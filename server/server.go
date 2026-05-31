package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"

	"server/ecs"
	"server/models"
	"server/systems"
)

func main() {
	templates, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		fmt.Println("CRITICAL SERVER BOOT ERROR:", err)
		return
	}
	fmt.Printf("[BOOT] Loaded %d monster templates.\n", len(templates))

	spawned := 0
	for _, t := range templates {
		if _, err := models.SpawnFromDefaultPosition(t.ID); err != nil {
			fmt.Printf("[BOOT] Failed to spawn template %d (%s): %v\n", t.ID, t.Name, err)
			continue
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

	for {
		conn, err := lis.Accept()
		if err != nil {
			fmt.Println("[ACCEPT] Error:", err)
			return
		}

		playerEntity := models.CreatePlayerEntity(conn)

		// Single snapshot lookup — covers name + spawn position in one call.
		snap, ok := ecs.GlobalRegistry.GetSnapshot(playerEntity)
		if !ok {
			// Entity registration failed somehow; drop the connection cleanly.
			conn.Close()
			continue
		}

		fmt.Printf("[CONNECT] %s (entity %d)\n", snap.Meta.Name, playerEntity)
		systems.BroadcastSystem(
			[]byte(fmt.Sprintf("Player %s has logged into the game!\r\n", snap.Meta.Name)),
		)

		go handleClient(conn, playerEntity, snap)
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
		systems.GlobalSpatialGrid.RemoveEntity(playerEntity) // ← NEW
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

	scan := bufio.NewScanner(conn)
	for scan.Scan() {
		handleCommand(conn, playerEntity, scan.Text())
	}

	if err := scan.Err(); err != nil {
		fmt.Printf("[IO] Player %s read error: %v\n", conn.RemoteAddr(), err)
	}
	fmt.Printf("[DISCONNECT] %s (%s)\n", snap.Meta.Name, conn.RemoteAddr())
}

// handleCommand parses and dispatches a single line of input from a player.
// Extracted from handleClient to keep the read-loop body flat and easy to extend.
//
// Parameters:
//   - conn:         Player's TCP socket (used only for the raw echo reply).
//   - playerEntity: ECS entity ID of the sending player.
//   - message:      Raw text line received from the client.
func handleCommand(conn net.Conn, playerEntity ecs.Entity, message string) {
	fmt.Printf("[CMD] %s: %q\n", conn.RemoteAddr(), message)

	parts := strings.Fields(message)
	if len(parts) == 0 {
		return
	}

	switch strings.ToUpper(parts[0]) { // ToUpper một lần ở đây — tất cả case đều uppercase

	case "M", "MOVE":
		// Delegate hoàn toàn sang HandlePlayerMovementSystem.
		// Không parse tọa độ ở đây nữa — đó là trách nhiệm của movement system.
		errMsg, ok := systems.HandlePlayerMovementSystem(playerEntity, message)
		if !ok {
			systems.SendNoticeSystem(playerEntity, []byte(errMsg))
		}
		// ok == true: MovementSystem đã tự broadcast, không cần làm gì thêm.

	case "INFO":
		if len(parts) != 2 {
			systems.SendNoticeSystem(playerEntity, []byte("Usage: info <entity_id>\r\n"))
			return
		}
		id, err := strconv.Atoi(parts[1])
		if err != nil {
			systems.SendNoticeSystem(playerEntity, []byte("Entity ID must be a number.\r\n"))
			return
		}
		text, err := systems.GetInfoSystem(ecs.Entity(id))
		if err != nil {
			systems.SendNoticeSystem(playerEntity, []byte("Entity not found.\r\n"))
			return
		}
		systems.SendNoticeSystem(playerEntity, []byte(text))

	case "ATTACK", "ATK":
		// Syntax: attack <entity_id>
		if len(parts) != 2 {
			systems.SendNoticeSystem(playerEntity, []byte("Usage: attack <entity_id>\r\n"))
			return
		}
		id, err := strconv.Atoi(parts[1])
		if err != nil {
			systems.SendNoticeSystem(playerEntity, []byte("Entity ID must be a number.\r\n"))
			return
		}

		result, errMsg := systems.AttackSystem(playerEntity, ecs.Entity(id))
		if errMsg != "" {
			// Attack rejected before landing — notify attacker only.
			systems.SendNoticeSystem(playerEntity, []byte(errMsg))
			return
		}

		// Attack landed — AttackSystem already broadcast the outcome.
		// Send a personal confirmation to the attacker with full detail.
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

	case "QUIT":
		fmt.Printf("[QUIT] %s requested disconnect.\n", conn.RemoteAddr())
		conn.Close()

	default:
		conn.Write([]byte("Server echo: " + message + "\r\n"))
	}
}
