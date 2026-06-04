package main

import (
	"fmt"
	"net"
	"time"

	"server/db"
	"server/ecs"
	"server/game"
	"server/logger"
	"server/mcp"
	"server/models"
	"server/peakgo/broadcast"
	"server/peakgo/codec"
	"server/peakgo/loggate"
	"server/peakgo/netio"
	"server/protocol"
	"server/systems"
	"server/transport"
	"server/world"
)

// packetPool is the shared payload buffer pool for all client connections.
// Exposed via netio.DefaultPool; defined here for documentation locality.
var packetPool = netio.DefaultPool

var opcodeNames = map[byte]string{
	1: "MOVE", 2: "INV", 3: "USE", 4: "WARP", 5: "ATTACK",
	6: "INFO", 7: "QUIT", 8: "PICKUP", 9: "EQUIP",
	10: "LOGIN", 11: "REGISTER", 12: "PARTY_CREATE",
	13: "PARTY_INVITE", 14: "PARTY_JOIN",
	15: "TRADE_INIT", 16: "TRADE_OFFER", 17: "TRADE_CONFIRM", 18: "TRADE_CANCEL",
	19: "SKILL_CAST", 20: "CHAT", 21: "HEARTBEAT",
}

// opcodeNameOf returns a human-readable name for a binary opcode byte.
// Used by the network packet debug middleware in handleBinaryPacket.
func opcodeNameOf(op byte) string {
	if s, ok := opcodeNames[op]; ok {
		return s
	}
	return "UNKNOWN"
}

// LoginQueue is a buffered channel that holds incoming TCP connections waiting to log in.
var LoginQueue = make(chan net.Conn, 1000)

// StartLoginWorkerPool spins up a pool of background worker goroutines to process connections.
func StartLoginWorkerPool(workerCount int) {
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			logger.Info("[BOOT] Connection login worker #%d active.", workerID)
			for conn := range LoginQueue {
				processLogin(conn)
			}
		}(i)
	}
}

// packetAuth holds parsed username/password from a LOGIN or REGISTER packet.
type packetAuth struct {
	username string
	password string
}

// parseAuthPayload extracts username and password from a binary auth packet payload.
// Format: [UsernameLen uint8][Username UTF-8][PasswordLen uint8][Password UTF-8]
// Returns empty packetAuth and false on any parse error.
func parseAuthPayload(payload []byte) (packetAuth, bool) {
	if len(payload) < 2 {
		return packetAuth{}, false
	}

	usernameLen := int(payload[0])
	if usernameLen == 0 || usernameLen > len(payload)-1 {
		return packetAuth{}, false
	}
	pos := 1 + usernameLen

	if pos >= len(payload) {
		return packetAuth{}, false
	}
	passwordLen := int(payload[pos])
	pos++
	if passwordLen == 0 || pos+passwordLen > len(payload) {
		return packetAuth{}, false
	}

	username := models.SanitizeUsername(string(payload[1 : 1+usernameLen]))
	password := string(payload[pos : pos+passwordLen])

	return packetAuth{username: username, password: password}, true
}

// processLogin handles client authentication (LOGIN or REGISTER).
// The first packet MUST be opcode 10 (LOGIN) or 11 (REGISTER).
func processLogin(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// ReadHeader: zero-alloc, no reflection — see peakgo/netio.
	length, err := netio.ReadHeader(conn)
	if err != nil {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Failed to read auth packet length.")
		conn.Close()
		return
	}
	if length == 0 || length > 256 {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Invalid auth packet length.")
		conn.Close()
		return
	}

	// ReadPayload: pooled buffer — zero heap allocation on steady-state path.
	pBuf, err := netio.ReadPayload(conn, packetPool, length)
	if err != nil {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Failed to read auth packet payload.")
		conn.Close()
		return
	}
	packetBytes := (*pBuf)[:length]
	defer packetPool.Put(pBuf)

	opcode := packetBytes[0]
	payload := packetBytes[1:]

	switch opcode {

	case protocol.OpcodeC2SLogin: // LOGIN
		auth, ok := parseAuthPayload(payload)
		if !ok {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Invalid LOGIN packet payload.")
			conn.Close()
			return
		}
		if !models.ValidateUsername(auth.username) {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Username must be 3-16 alphanumeric characters.")
			conn.Close()
			return
		}
		if !models.ValidatePassword(auth.password) {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Password must be at least 6 characters.")
			conn.Close()
			return
		}

		// In dev_mode (no DB), skip credential checks and create a fresh player entity.
		devMode := models.DBEngine == nil
		if !devMode {
			// Look up stored credentials.
			_, storedHash, found := models.LookupCredentials(auth.username)
			if !found {
				protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Account does not exist. Please register first.")
				conn.Close()
				return
			}
			if !models.CheckPasswordHash(auth.password, storedHash) {
				protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Invalid username or password.")
				conn.Close()
				return
			}
		}

		// Auth passed — create ECS entity from saved DB state.
		playerEntity, err := models.CreatePlayerEntity(conn, auth.username)
		if devMode && err == nil {
			// In dev_mode: ensure the entity has a valid metadata name
			// (CreatePlayerEntity already sets the default Position/Stats/Equipment)
			logger.Info("[DEV] Skipped DB auth for %s — dev_mode active", auth.username)
		}
		if err != nil {
			logger.Error("[CONNECT] Error loading character from DB: %v", err)
			protocol.SendErrorPacket(conn, protocol.ErrCodeDatabaseError, "Failed to load character data. Please try again.")
			conn.Close()
			return
		}

		_ = conn.SetReadDeadline(time.Time{})

		snap, ok := ecs.GlobalRegistry.GetSnapshot(playerEntity)
		if !ok {
			conn.Close()
			return
		}

		logger.Info("[CONNECT] %s (entity %d) from %s", snap.Meta.Name, playerEntity, conn.RemoteAddr())

		// Send the player's own SpawnEntity so the client can create their character.
		selfSpawn := broadcast.BuildSpawnEntity(broadcast.SpawnPayload{
			EntityID: uint64(playerEntity),
			Type:     uint8(snap.Meta.Type),
			MapID:    int32(snap.Pos.MapID),
			X:        int32(snap.Pos.X),
			Z:        int32(snap.Pos.Z),
			Name:     snap.Meta.Name,
		})
		_ = netio.WritePacket(conn, selfSpawn)

		// Send the player's own StatsSync so the client can initialize the HUD.
		selfStats := broadcast.BuildStatsSync(broadcast.StatsSyncPayload{
			EntityID: uint64(playerEntity),
			HP:       int32(snap.Stats.HP),
			MaxHP:    int32(snap.Stats.MaxHP),
			MP:       int32(snap.Stats.MP),
			MaxMP:    int32(snap.Stats.MaxMP),
			Dam:      int32(snap.Stats.Dam),
			Level:    int32(snap.Stats.Level),
		})
		_ = netio.WritePacket(conn, selfStats)

		systems.BroadcastSystem(
			[]byte(fmt.Sprintf("Player %s has logged into the game!\r\n", snap.Meta.Name)),
		)
		// Register the player as an AOI watcher for delta enter/leave broadcasts.
		world.RegisterPlayerAOI(playerEntity)
		go handleClient(conn, playerEntity, snap)

	case protocol.OpcodeC2SRegister: // REGISTER
		auth, ok := parseAuthPayload(payload)
		if !ok {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Invalid REGISTER packet payload.")
			conn.Close()
			return
		}
		if !models.ValidateUsername(auth.username) {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Username must be 3-16 alphanumeric characters.")
			conn.Close()
			return
		}
		if !models.ValidatePassword(auth.password) {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Password must be at least 6 characters.")
			conn.Close()
			return
		}

		hashed, err := models.HashPassword(auth.password)
		if err != nil {
			logger.Error("[REGISTER] bcrypt hash error: %v", err)
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Server error. Please try again.")
			conn.Close()
			return
		}

		err = models.RegisterNewAccount(auth.username, hashed)
		if err != nil {
			protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Username already exists.")
			conn.Close()
			return
		}

		// Registration successful — notify client and close.
		// Client must now send a LOGIN packet to enter the game.
		protocol.SendSuccessPacket(conn, "Account registered successfully! Please log in.")
		conn.Close()

	default:
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Expected LOGIN or REGISTER packet as first message.")
		conn.Close()
	}
}

func main() {
	logger.Init() // Must be first: reads data/config.json, starts async log worker

	game.InitializeItemRegistry()
	game.InitializeLootTables()
	world.InitializeCollisionMaps()
	models.InitializeSkillRegistry()

	// Sync item registry to MCP for inventory display.
	for id, t := range game.ItemRegistry {
		mcp.ItemRegistryGlobal[id] = struct {
			ID        uint64
			Name      string
			HealValue int
			SlotType  string
			BonusDam  int
			BonusHP   int
		}{
			ID: t.ID, Name: t.Name,
			HealValue: t.HealValue,
			SlotType:  t.SlotType,
			BonusDam:  t.BonusDam,
			BonusHP:   t.BonusHP,
		}
	}

	models.InitializeDatabase("root:root@tcp(127.0.0.1:3306)/?parseTime=true")
	db.StartSaveWorkerEngine()

	// Initialize the ECS Entity ID counter to the maximum character ID in the DB to avoid session ID collisions.
	// Skip if DB is not available (dev_mode).
	if models.DBEngine != nil {
		var maxID uint64
		if err := models.DBEngine.QueryRow("SELECT COALESCE(MAX(id), 0) FROM characters").Scan(&maxID); err == nil {
			ecs.GlobalRegistry.SetNextID(maxID)
		} else {
			logger.Error("[BOOT] Failed to scan max character ID: %v", err)
		}
	} else {
		logger.Info("[BOOT] No DB available — ECS ID counter starts from default (dev_mode).")
	}

	templates, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		logger.Error("CRITICAL SERVER BOOT ERROR: %v", err)
		return
	}
	logger.Info("[BOOT] Loaded %d monster templates.", len(templates))

	spawned := 0
	for _, t := range templates {
		id, err := models.SpawnFromDefaultPosition(t.ID)
		if err != nil {
			logger.Warn("[BOOT] Failed to spawn template %d (%s): %v", t.ID, t.Name, err)
			continue
		}
		if pos, ok := ecs.GlobalRegistry.GetPosition(id); ok {
			world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
		}
		spawned++
	}
	logger.Info("[BOOT] Spawned %d live monster instances into ECS.", spawned)

	lis, err := net.Listen("tcp", ":1503")
	if err != nil {
		logger.Error("[BOOT] Failed to bind port: %v", err)
		return
	}
	defer lis.Close()
	logger.Info("[BOOT] Server listening on %s", lis.Addr())

	world.InitAOIManager()

	systems.StartGameLoop()
	StartLoginWorkerPool(4) // Start 4 connection login workers to process db queue

	// Start the MCP JSON-RPC HTTP server for AI agent inspection.
	// Uses port 8080 by default; configure via MCP_PORT env var or data/config.json.
	mcp.Start(mcp.Config{Port: 8080})
	logger.Info("[BOOT] MCP admin interface available on http://localhost:8080/mcp")

	// Start the WebSocket listener for WebGL clients on port 8081.
	// Runs in its own goroutine — does not block the TCP accept loop.
	go transport.StartWebSocketListener(":8081", LoginQueue)
	logger.Info("[BOOT] WebSocket transport listening on ws://localhost:8081/ws")

	for {
		conn, err := lis.Accept()
		if err != nil {
			logger.Error("[ACCEPT] Error: %v", err)
			return
		}

		select {
		case LoginQueue <- conn:
		default:
			// Queue full! Tell the client and drop connection cleanly.
			logger.Warn("[ACCEPT] Login queue full — dropping connection from %s", conn.RemoteAddr())
			protocol.SendErrorPacket(conn, protocol.ErrCodeServerFull, "Server login queue is full. Please try again later.")
			conn.Close()
		}
	}
}

// handleClient manages the lifecycle of a single connected player.
// snap is passed in from main so we avoid a redundant ECS lookup at goroutine start.
//
// Parameters:
//   - conn:         TCP socket for this player.
//   - playerEntity: ECS entity ID.
//   - snap:         Pre-fetched snapshot (name + spawn position already resolved).
func handleClient(conn net.Conn, playerEntity ecs.Entity, snap ecs.EntitySnapshot) {
	// Deferred cleanup: broadcast logout, remove from ECS, close socket.
	defer func() {
		name := snap.Meta.Name
		if live, ok := ecs.GlobalRegistry.GetMetadata(playerEntity); ok {
			name = live.Name
		}
		game.GlobalTradeRegistry.CancelTradeSession(playerEntity)
		game.RemovePlayerFromParty(playerEntity)
		db.QueuePlayerSave(playerEntity)
		world.UnregisterPlayerAOI(playerEntity)
		world.GlobalSpatialGrid.RemoveEntity(playerEntity)
		ecs.GlobalRegistry.RemoveEntity(playerEntity)
		models.ActivePlayers.Delete(conn.RemoteAddr().String())
		logger.Info("[DISCONNECT] %s (%s)", name, conn.RemoteAddr())
		systems.BroadcastSystem(
			[]byte(fmt.Sprintf("Player %s has logged out the game!\r\n", name)),
		)
		conn.Close()
	}()

	// Greet the player with their name and spawn position — both already in snap.
	spawnMsg := fmt.Sprintf(
		"Welcome to the Realm, %s!\r\nYour Spawn Position is X: %d, Z: %d\r\n",
		snap.Meta.Name, snap.Pos.X, snap.Pos.Z,
	)
	systems.SendNoticeSystem(playerEntity, []byte(spawnMsg))

	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))

	for {
		// Zero-alloc header read: stack [2]byte + BigEndian.Uint16, no reflection.
		length, err := netio.ReadHeader(conn)
		if err != nil {
			break // Disconnected or read error
		}
		if length == 0 {
			continue
		}

		// Pooled payload read: no heap allocation on steady-state path.
		pBuf, err := netio.ReadPayload(conn, packetPool, length)
		if err != nil {
			break // Disconnected or read error
		}

		buf := (*pBuf)[:length]
		opcode := codec.ReadUint8(buf)
		payload := buf[1:]

		handleBinaryPacket(conn, playerEntity, opcode, payload)

		packetPool.Put(pBuf)
		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	}
}

// handleBinaryPacket parses and dispatches a single binary packet from a player.
//
// Parameters:
//   - conn:         Player's TCP socket (used for direct response transmission).
//   - playerEntity: ECS entity ID of the sending player.
//   - opcode:       Operation code byte.
//   - payload:      Raw packet payload bytes.
func handleBinaryPacket(conn net.Conn, playerEntity ecs.Entity, opcode byte, payload []byte) {
	// Network packet trace middleware: only active when debug=true in config.json.
	// loggate.Debugf gates on logger.IsDebug() before formatting — 0 allocs in production.
	loggate.Debugf("[NET RX] Conn: %s | Opcode: %d (%s) | Payload: %d bytes | Hex: [% X]",
		conn.RemoteAddr(), opcode, opcodeNameOf(opcode), len(payload), payload)

	switch opcode {
	case protocol.OpcodeC2SMove: // MOVE
		errMsg, ok := game.HandlePlayerMovementSystem(playerEntity, payload)
		if !ok {
			systems.SendNoticeSystem(playerEntity, []byte(errMsg))
		}

	case protocol.OpcodeC2SInv: // INV
		if len(payload) != 0 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: INV packet payload must be empty.\r\n"))
			return
		}
		inventoryTextPacket := game.RunInventoryQuerySystem(playerEntity)
		frame := broadcast.BuildNotice(broadcast.NoticePayload{Message: inventoryTextPacket})
		conn.Write(frame)

	case protocol.OpcodeC2SUse: // USE
		noticePacket, success := game.HandleItemUsageSystem(playerEntity, payload)
		if success {
			systems.BroadcastSystem([]byte(noticePacket))
		} else {
			systems.SendNoticeSystem(playerEntity, []byte(noticePacket))
		}

	case protocol.OpcodeC2SWarp: // WARP
		systemFeedback, _ := world.HandleWarpSystem(playerEntity, payload)
		systems.SendNoticeSystem(playerEntity, []byte(systemFeedback))

	case protocol.OpcodeC2SAttack: // ATTACK
		atk, ok := codec.ReadAttackPayload(payload)
		if !ok {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid ATTACK payload length. Expected 8 bytes.\r\n"))
			return
		}
		targetID := ecs.Entity(atk.TargetID)

		result, errMsg := game.AttackSystem(playerEntity, targetID)
		if errMsg != "" {
			systems.SendNoticeSystem(playerEntity, []byte(errMsg))
			return
		}

		if result.Killed {
			systems.SendNoticeSystem(playerEntity,
				[]byte(fmt.Sprintf("You killed %s!\r\n", result.TargetName)),
			)
		} else {
			systems.SendNoticeSystem(playerEntity,
				[]byte(fmt.Sprintf(
					"You hit %s for %d damage. %s has %d HP remaining.\r\n",
					result.TargetName, result.Damage,
					result.TargetName, result.TargetHP,
				)),
			)
		}

	case protocol.OpcodeC2SInfo: // INFO
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid INFO payload length. Expected 8 bytes.\r\n"))
			return
		}
		targetID := ecs.Entity(codec.ReadUint64(payload[0:8]))
		text, err := systems.GetInfoSystem(targetID)
		if err != nil {
			systems.SendNoticeSystem(playerEntity, []byte("Entity not found.\r\n"))
			return
		}
		systems.SendNoticeSystem(playerEntity, []byte(text))

	case protocol.OpcodeC2SQuit: // QUIT
		if len(payload) != 0 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: QUIT packet payload must be empty.\r\n"))
			return
		}
		logger.Info("[QUIT] %s requested graceful disconnect.", conn.RemoteAddr())
		conn.Close()

	case protocol.OpcodeC2SPickup: // PICKUP
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PICKUP payload length. Expected 8 bytes.\r\n"))
			return
		}
		itemEntityID := ecs.Entity(codec.ReadUint64(payload[0:8]))
		personalFeedback, _ := game.HandleItemPickupSystem(playerEntity, itemEntityID)
		systems.SendNoticeSystem(playerEntity, []byte(personalFeedback))

	case protocol.OpcodeC2SEquip: // EQUIP
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid EQUIP payload length. Expected 8 bytes.\r\n"))
			return
		}
		itemID := codec.ReadUint64(payload[0:8])
		feedback, success := game.HandleEquipmentSystem(playerEntity, itemID)
		if success {
			pos, _ := ecs.GlobalRegistry.GetPosition(playerEntity)
			frame := broadcast.BuildNotice(broadcast.NoticePayload{Message: feedback})
			protocol.BroadcastToNeighbors(pos, frame, playerEntity)
		} else {
			systems.SendNoticeSystem(playerEntity, []byte(feedback))
		}

	case protocol.OpcodeC2SPartyCreate: // PARTY CREATE
		if len(payload) < 1 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PARTY CREATE payload.\r\n"))
			return
		}
		teamNameLen := int(payload[0])
		if teamNameLen == 0 || teamNameLen > len(payload)-1 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid team name length.\r\n"))
			return
		}
		teamName := string(payload[1 : 1+teamNameLen])
		response := game.CreatePartySystem(playerEntity, teamName)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2SPartyInvite: // PARTY INVITE
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PARTY INVITE payload length. Expected 8 bytes.\r\n"))
			return
		}
		targetID := ecs.Entity(codec.ReadUint64(payload[0:8]))
		response, _ := game.SendPartyInviteSystem(playerEntity, targetID)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2SPartyJoin: // PARTY JOIN
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PARTY JOIN payload length. Expected 8 bytes.\r\n"))
			return
		}
		partyID := ecs.Entity(codec.ReadUint64(payload[0:8]))
		response, _ := game.AcceptPartyInviteSystem(playerEntity, partyID)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2STradeInit: // TRADE INIT
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid TRADE INIT payload length. Expected 8 bytes.\r\n"))
			return
		}
		targetID := ecs.Entity(codec.ReadUint64(payload[0:8]))
		response, _ := game.GlobalTradeRegistry.InitializeTradeSession(playerEntity, targetID)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2STradeOffer: // TRADE OFFER
		if len(payload) != 12 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid TRADE OFFER payload length. Expected 12 bytes.\r\n"))
			return
		}
		itemID := codec.ReadUint64(payload[0:8])
		qty := int(codec.ReadInt32(payload[8:12]))
		response, _ := game.GlobalTradeRegistry.OfferItemToTrade(playerEntity, itemID, qty)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2STradeConfirm: // TRADE CONFIRM
		response, _ := game.GlobalTradeRegistry.LockTradeStage(playerEntity)
		if response != "" {
			systems.SendNoticeSystem(playerEntity, []byte(response))
		}

	case protocol.OpcodeC2STradeCancel: // TRADE CANCEL
		response, _ := game.GlobalTradeRegistry.CancelTradeSession(playerEntity)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2SSkillCast: // SKILL CAST
		if len(payload) != 16 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid SKILL CAST payload length. Expected 16 bytes.\r\n"))
			return
		}
		skillID := codec.ReadUint64(payload[0:8])
		targetID := ecs.Entity(codec.ReadUint64(payload[8:16]))
		response, _ := game.HandleSkillCastingSystem(playerEntity, skillID, targetID)
		systems.SendNoticeSystem(playerEntity, []byte(response))

	case protocol.OpcodeC2SChat: // CHAT
		msg := string(payload)
		game.RouteChatMessage(playerEntity, msg)

	case protocol.OpcodeC2SHeartbeat: // HEARTBEAT
		// Reset read deadline — this is the primary purpose of heartbeat.
		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		// Pong back to client.
		pong := [4]byte{0, 1, protocol.OpcodeS2CHeartbeat}
		_ = netio.WritePacket(conn, pong[:3])

	default:
		loggate.Warnf("[NET] Unknown opcode %d from %s", opcode, conn.RemoteAddr())
		systems.SendNoticeSystem(playerEntity, []byte(fmt.Sprintf("Error: Unknown opcode %d\r\n", opcode)))
	}
}
