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

// main is the main entry point to bootstrap and run the game server.
// The boot sequence consists of:
//  1. Loading the template monsters from the JSON config file and registering them as entities in ECS.
//  2. Listening for incoming TCP socket connections on port `:1503`.
//  3. Starting the ticking server heartbeat clock (Game Loop).
//  4. Waiting for new player clients. When a player connects, the server registers a new Player entity in ECS
//     and spins up a dedicated goroutine (`handleClient`) to handle their requests.
func main() {
	monsters, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		fmt.Println("CRITICAL SERVER BOOT ERROR:", err)
		return
	}

	for i := range monsters {
		models.CreateMonsterEntity(monsters[i])
	}

	fmt.Printf("[Engine] Successfully loaded %d monster templates into ECS Registry.\n", len(monsters))

	lis, err := net.Listen("tcp", ":1503")
	if err != nil {
		fmt.Println("error occurred", err)
		return
	}
	defer lis.Close()
	fmt.Println("server started on port", lis.Addr())

	// Start the background game loop
	systems.StartGameLoop()

	for {
		fmt.Println("waiting to player")
		conn, err := lis.Accept()
		if err != nil {
			fmt.Println("An error occurred", err)
			return
		}
		
		newPlayerEntity := models.CreatePlayerEntity(conn)
		
		playerName := ""
		if meta := ecs.GlobalRegistry.GetMetadata(newPlayerEntity); meta != nil {
			playerName = meta.Name
		}
		
		fmt.Println("New player connected", newPlayerEntity)
		systems.BroadcastSystem(fmt.Sprintf("Player %s has logged into the game!\r\n", playerName))

		go handleClient(conn, newPlayerEntity)
	}
}

// handleClient manages the lifecycle and handles incoming network commands for a specific player client.
// It executes asynchronously as a dedicated goroutine per connected player.
// When the player disconnects (or sends a quit command), it removes the entity from ECS, broadcasts logout notice, and closes socket.
//
// Parameters:
//   - conn: The TCP network socket link (net.Conn) of the player client.
//   - playerEntity: The ecs.Entity representing the connected player character.
func handleClient(conn net.Conn, playerEntity ecs.Entity) {
	defer func() {
		playerName := ""
		if meta := ecs.GlobalRegistry.GetMetadata(playerEntity); meta != nil {
			playerName = meta.Name
		}
		ecs.GlobalRegistry.RemoveEntity(playerEntity)
		systems.BroadcastSystem(fmt.Sprintf("Player %s has logged out the game!\r\n", playerName))
		conn.Close()
	}()

	playerName := ""
	x, z := 0, 0
	if meta := ecs.GlobalRegistry.GetMetadata(playerEntity); meta != nil {
		playerName = meta.Name
	}
	if pos := ecs.GlobalRegistry.GetPosition(playerEntity); pos != nil {
		x = pos.X
		z = pos.Z
	}

	systems.SendNoticeSystem(playerEntity, fmt.Sprintf("Welcome to the Realm, %s!\r\nYour Spawn Position is X: %d, Z: %d\r\n", playerName, x, z))
	scan := bufio.NewScanner(conn)

	for scan.Scan() {
		message := scan.Text()
		length := len(message)
		conn.Write([]byte("Server send: " + message + "\r\n"))

		fmt.Println("Message Received", length, message)

		if strings.HasPrefix(message, "m") {
			mpart := strings.Split(message, " ")

			if len(mpart) == 3 {
				xVal, errX := strconv.Atoi(mpart[1])
				zVal, errZ := strconv.Atoi(mpart[2])
				if errX == nil && errZ == nil {
					systems.MovementSystem(playerEntity, xVal, zVal)
				} else {
					systems.SendNoticeSystem(playerEntity, "Coordinates must be numbers.\r\n")
				}
			} else {
				systems.SendNoticeSystem(playerEntity, "Invalid command, doesn't have 3 parts.\r\n")
			}
		} else if strings.HasPrefix(message, "quit") {
			fmt.Printf("player %s send close command, shutting down server \n", conn.RemoteAddr())
			break
		} else if strings.HasPrefix(message, "info") {
			parts := strings.Split(message, " ")
			if len(parts) == 2 {
				targetID := parts[1]
				
				infoText, err := systems.GetInfoSystem(ecs.Entity(targetID))
				if err == nil {
					systems.SendNoticeSystem(playerEntity, infoText)
				} else {
					systems.SendNoticeSystem(playerEntity, "Monster ID not found in database.\r\n")
				}
			} else {
				systems.SendNoticeSystem(playerEntity, "Syntax error. Use: INFO [1-3]\r\n")
			}
		} else {
			fmt.Println("Normal chat")
		}
	}

	if err := scan.Err(); err != nil {
		fmt.Printf("An error ocurred with player %s: %v\n", conn.RemoteAddr(), err)
	}
	fmt.Printf("player %s disconnected, shuting down\n", conn.RemoteAddr())
}
