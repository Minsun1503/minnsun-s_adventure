package auth

import (
	"fmt"
	"net"
	"server/ecs"
	"server/logger"
	"server/models"
	"server/network"
	"server/peakgo/broadcast"
	"server/peakgo/netio"
	"server/protocol"
	"server/systems"
	"server/world"
	"time"
)

// LoginQueue is a buffered channel that holds incoming TCP connections waiting to log in.
var LoginQueue = make(chan net.Conn, 1000)

// packetAuth holds parsed username/password from a LOGIN or REGISTER packet.
type packetAuth struct {
	username string
	password string
}

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
	pBuf, err := netio.ReadPayload(conn, netio.DefaultPool, length)
	if err != nil {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Failed to read auth packet payload.")
		conn.Close()
		return
	}
	packetBytes := (*pBuf)[:length]
	defer netio.DefaultPool.Put(pBuf)

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

		// Send S2CSuccess with EntityID so client can set LocalPlayerID from a trusted source.
		successWithID := broadcast.BuildSuccessWithEntityID(
			uint64(playerEntity),
			"Login successful.",
		)
		_ = netio.WritePacket(conn, successWithID)

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
		go network.HandleClient(conn, playerEntity, snap)
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
