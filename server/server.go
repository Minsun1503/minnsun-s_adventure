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
	monsters, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		fmt.Println("CRITICAL SERVER BOOT ERROR:", err)
		return
	}
	for _, t := range monsters {
		models.CreateMonsterEntity(t)
	}
	fmt.Printf("[BOOT] Registered %d monster templates into ECS.\n", len(monsters))

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
		// Re-fetch name in case it was mutated during the session (e.g. rename system).
		name := snap.Meta.Name
		if live, ok := ecs.GlobalRegistry.GetMetadata(playerEntity); ok {
			name = live.Name
		}
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

	parts := strings.Fields(message) // Fields splits on any whitespace, handles extra spaces
	if len(parts) == 0 {
		return
	}

	switch parts[0] {

	case "m", "move":
		if len(parts) != 3 {
			systems.SendNoticeSystem(playerEntity, []byte("Usage: m <x> <z>\r\n"))
			return
		}
		xVal, errX := strconv.Atoi(parts[1])
		zVal, errZ := strconv.Atoi(parts[2])
		if errX != nil || errZ != nil {
			systems.SendNoticeSystem(playerEntity, []byte("Coordinates must be integers.\r\n"))
			return
		}
		systems.MovementSystem(playerEntity, xVal, zVal)

	case "info":
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

	case "quit":
		fmt.Printf("[QUIT] %s requested disconnect.\n", conn.RemoteAddr())
		conn.Close() // triggers scan.Scan() to return false → handleClient cleans up

	default:
		// Normal chat — echo back, could be routed to a ChatSystem later.
		conn.Write([]byte("Server echo: " + message + "\r\n"))
	}
}
