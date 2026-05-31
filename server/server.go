package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"server/ecs"
	"server/models"
	"server/systems"
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
func processLogin(conn net.Conn) {
	playerEntity, err := models.CreatePlayerEntity(conn)
	if err != nil {
		fmt.Println("[CONNECT] Error registering character in DB:", err)
		systems.SendErrorPacket(conn, systems.ErrCodeDatabaseError, "Temporary server database issue. Please try again later.")
		conn.Close()
		return
	}

	// Single snapshot lookup — covers name + spawn position in one call.
	snap, ok := ecs.GlobalRegistry.GetSnapshot(playerEntity)
	if !ok {
		// Entity registration failed somehow; drop the connection cleanly.
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
	systems.InitializeItemRegistry()
	systems.InitializeLootTables()
	systems.InitializeCollisionMaps()

	models.InitializeDatabase("root:root@tcp(127.0.0.1:3306)/?parseTime=true")
	systems.StartSaveWorkerEngine()

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
			systems.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
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
			systems.SendErrorPacket(conn, systems.ErrCodeServerFull, "Server login queue is full. Please try again later.")
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
		systems.QueuePlayerSave(playerEntity)
		systems.GlobalSpatialGrid.RemoveEntity(playerEntity)
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
	case systems.OpcodeC2SMove: // MOVE
		errMsg, ok := systems.HandlePlayerMovementSystem(playerEntity, payload)
		if !ok {
			systems.SendNoticeSystem(playerEntity, []byte(errMsg))
		}

	case systems.OpcodeC2SInv: // INV
		if len(payload) != 0 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: INV packet payload must be empty.\r\n"))
			return
		}
		inventoryTextPacket := systems.RunInventoryQuerySystem(playerEntity)
		conn.Write([]byte(inventoryTextPacket))

	case systems.OpcodeC2SUse: // USE
		noticePacket, success := systems.HandleItemUsageSystem(playerEntity, payload)
		if success {
			systems.BroadcastSystem([]byte(noticePacket))
		} else {
			systems.SendNoticeSystem(playerEntity, []byte(noticePacket))
		}

	case systems.OpcodeC2SWarp: // WARP
		systemFeedback, _ := systems.HandleWarpSystem(playerEntity, payload)
		systems.SendNoticeSystem(playerEntity, []byte(systemFeedback))

	case systems.OpcodeC2SAttack: // ATTACK
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid ATTACK payload length. Expected 8 bytes.\r\n"))
			return
		}
		targetID := ecs.Entity(binary.BigEndian.Uint64(payload[0:8]))

		result, errMsg := systems.AttackSystem(playerEntity, targetID)
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

	case systems.OpcodeC2SInfo: // INFO
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

	case systems.OpcodeC2SQuit: // QUIT
		if len(payload) != 0 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: QUIT packet payload must be empty.\r\n"))
			return
		}
		fmt.Printf("[QUIT] %s requested disconnect.\n", conn.RemoteAddr())
		conn.Close()

	case systems.OpcodeC2SPickup: // PICKUP
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid PICKUP payload length. Expected 8 bytes.\r\n"))
			return
		}
		itemEntityID := ecs.Entity(binary.BigEndian.Uint64(payload[0:8]))
		personalFeedback, _ := systems.HandleItemPickupSystem(playerEntity, itemEntityID)
		systems.SendNoticeSystem(playerEntity, []byte(personalFeedback))

	case systems.OpcodeC2SEquip: // EQUIP
		if len(payload) != 8 {
			systems.SendNoticeSystem(playerEntity, []byte("Error: Invalid EQUIP payload length. Expected 8 bytes.\r\n"))
			return
		}
		itemID := binary.BigEndian.Uint64(payload[0:8])
		feedback, success := systems.HandleEquipmentSystem(playerEntity, itemID)
		if success {
			pos, _ := ecs.GlobalRegistry.GetPosition(playerEntity)
			systems.BroadcastToMap(pos.MapID, feedback)
		} else {
			systems.SendNoticeSystem(playerEntity, []byte(feedback))
		}

	default:
		systems.SendNoticeSystem(playerEntity, []byte(fmt.Sprintf("Error: Unknown opcode %d\r\n", opcode)))
	}
}
