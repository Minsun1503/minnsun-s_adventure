package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"server/db"
	"server/ecs"
	"server/game"
	"server/models"
	"server/protocol"
	"server/systems"
	"server/world"
)

// LoginQueue is a buffered channel that holds incoming TCP connections waiting to log in.
var LoginQueue = make(chan net.Conn, 1000)

// StartLoginWorkerPool spins up a pool of background worker goroutines to process connections.
func StartLoginWorkerPool(workerCount int) {
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			fmt.Printf("[BOOT] Connection login worker #%d active.\n", workerID)
			for conn := range LoginQueue {
				processLogin(conn)
			}
		}(i)
	}
}

// processLogin manages the database registration and player entity creation.
// It reads the first packet which MUST be a LOGIN packet (opcode 10) containing
// the player's chosen username.
func processLogin(conn net.Conn) {
	// Set a 5-second read deadline to prevent login worker starvation DoS attacks
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Read packet length (2 bytes Big-Endian).
	var length uint16
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Failed to read login packet length.")
		conn.Close()
		return
	}
	if length == 0 || length > 256 {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Invalid login packet length.")
		conn.Close()
		return
	}

	// Read the full packet payload.
	packetBytes := make([]byte, length)
	if _, err := io.ReadFull(conn, packetBytes); err != nil {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Failed to read login packet payload.")
		conn.Close()
		return
	}

	// Validate opcode.
	if packetBytes[0] != protocol.OpcodeC2SLogin {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Expected LOGIN packet as first message.")
		conn.Close()
		return
	}

	// Payload format: [UsernameLen uint8][Username UTF-8 bytes]
	payload := packetBytes[1:]
	if len(payload) < 1 {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Login packet too short.")
		conn.Close()
		return
	}

	usernameLen := int(payload[0])
	if usernameLen == 0 || usernameLen > len(payload)-1 {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Invalid username length in login packet.")
		conn.Close()
		return
	}

	username := models.SanitizeUsername(string(payload[1 : 1+usernameLen]))
	if !models.ValidateUsername(username) {
		protocol.SendErrorPacket(conn, protocol.ErrCodeInternalError, "Username must be 3-16 alphanumeric characters.")
		conn.Close()
		return
	}

	// Create or load the player entity using the validated username.
	playerEntity, err := models.CreatePlayerEntity(conn, username)
	if err != nil {
		fmt.Println("[CONNECT] Error registering character in DB:", err)
		protocol.SendErrorPacket(conn, protocol.ErrCodeDatabaseError, "Temporary server database issue. Please try again later.")
		conn.Close()
		return
	}

	// Reset read deadline to infinity for the persistent client loop.
	_ = conn.SetReadDeadline(time.Time{})

	// Single snapshot lookup — covers name + spawn position in one call.
	snap, ok := ecs.GlobalRegistry.GetSnapshot(playerEntity)
	if !ok {
		conn.Close()
		return
	}

	fmt.Printf("[CONNECT] %s (entity %d)\n", snap.Meta.Name, playerEntity)
	systems.BroadcastSystem(
		[]byte(fmt.Sprintf("Player %s has logged into the game!\r\n", snap.Meta.Name)),
	)

	go handleClient(conn, playerEntity, snap)
}

func main() {
	game.InitializeItemRegistry()
	game.InitializeLootTables()
	world.InitializeCollisionMaps()

	models.InitializeDatabase("root:root@tcp(127.0.0.1:3306)/?parseTime=true")
	db.StartSaveWorkerEngine()

	templates, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		fmt.Println("CRITICAL SERVER BOOT ERROR:", err)
		return
	}
	fmt.Printf("[BOOT] Loaded %d monster templates.\n", len(templates))

	spawned := 0
	for _, t := range templates {
		id, err := models.SpawnFromDefaultPosition(t.ID)
		if err != nil {
			fmt.Printf("[BOOT] Failed to spawn template %d (%s): %v\n", t.ID, t.Name, err)
			continue
		}
		if pos, ok := ecs.GlobalRegistry.GetPosition(id); ok {
			world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
		}
		spawned++
	}
	fmt.Printf("[BOOT] Spawned %d live monster instances into ECS.\n", spawned)

	lis, err := net.Listen("tcp", ":1503")
	if err != nil {
		fmt.Println("[BOOT] Failed to bind port:", err)
		return
	}
	defer lis.Close()
	fmt.Println("[BOOT] Server listening on", lis.Addr())

	systems.StartGameLoop()
	StartLoginWorkerPool(4) // Start 4 connection login workers to process db queue

	for {
		conn, err := lis.Accept()
		if err != nil {
			fmt.Println("[ACCEPT] Error:", err)
			return
		}

		select {
		case LoginQueue <- conn:
		default:
			// Queue full! Tell the client and drop connection cleanly.
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
		db.QueuePlayerSave(playerEntity)
		world.GlobalSpatialGrid.RemoveEntity(playerEntity)
		ecs.GlobalRegistry.RemoveEntity(playerEntity)
		models.ActivePlayers.Delete(conn.RemoteAddr().String())
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

	for {
		var length uint16
		if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
			break // Disconnected
		}

		if length == 0 {
			continue
		}

		packetBytes := make([]byte, length)
		if _, err := io.ReadFull(conn, packetBytes); err != nil {
			break // Disconnected or read error
		}

		opcode := packetBytes[0]
		payload := packetBytes[1:]

		handleBinaryPacket(conn, playerEntity, opcode, payload)
	}
	fmt.Printf("[DISCONNECT] %s (%s)\n", snap.Meta.Name, conn.RemoteAddr())
}

// handleBinaryPacket parses and dispatches a single binary packet from a player.
//
// Parameters:
//   - conn:         Player's TCP socket (used for direct response transmission).
//   - playerEntity: ECS entity ID of the sending player.
//   - opcode:       Operation code byte.
//   - payload:      Raw packet payload bytes.
func handleBinaryPacket(conn net.Conn, playerEntity ecs.Entity, opcode byte, payload []byte) {
	fmt.Printf("[CMD] %s: Opcode %d, Payload len %d\n", conn.RemoteAddr(), opcode, len(payload))

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
		conn.Write([]byte(inventoryTextPacket))

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
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid ATTACK payload length. Expected 8 bytes.\r\n"))
			return
		}
		targetID := ecs.Entity(binary.BigEndian.Uint64(payload[0:8]))

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
		targetID := ecs.Entity(binary.BigEndian.Uint64(payload[0:8]))
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
		fmt.Printf("[QUIT] %s requested disconnect.\n", conn.RemoteAddr())
		conn.Close()

	case protocol.OpcodeC2SPickup: // PICKUP
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PICKUP payload length. Expected 8 bytes.\r\n"))
			return
		}
		itemEntityID := ecs.Entity(binary.BigEndian.Uint64(payload[0:8]))
		personalFeedback, _ := game.HandleItemPickupSystem(playerEntity, itemEntityID)
		systems.SendNoticeSystem(playerEntity, []byte(personalFeedback))

	case protocol.OpcodeC2SEquip: // EQUIP
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid EQUIP payload length. Expected 8 bytes.\r\n"))
			return
		}
		itemID := binary.BigEndian.Uint64(payload[0:8])
		feedback, success := game.HandleEquipmentSystem(playerEntity, itemID)
		if success {
			pos, _ := ecs.GlobalRegistry.GetPosition(playerEntity)
			protocol.BroadcastToMap(pos.MapID, feedback)
		} else {
			systems.SendNoticeSystem(playerEntity, []byte(feedback))
		}

	default:
		systems.SendNoticeSystem(playerEntity, []byte(fmt.Sprintf("Error: Unknown opcode %d\r\n", opcode)))
	}
}
