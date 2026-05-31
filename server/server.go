package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"

	"server/models"
	"server/network"
	"server/state"
)

func main() {
	monsters, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		fmt.Println("CRITICAL SERVER BOOT ERROR:", err)
		return
	}

	for i := range monsters {
		template := &monsters[i]
		state.MonsterRegistry[template.ID] = template
	}

	fmt.Printf("[Engine] Successfully loaded %d monster templates into memory.\n", len(state.MonsterRegistry))

	lis, err := net.Listen("tcp", ":1503")
	if err != nil {
		fmt.Println("error occurred", err)
		return
	}
	defer lis.Close()
	fmt.Println("server started on port", lis.Addr())

	for {
		fmt.Println("waiting to player")
		conn, err := lis.Accept()
		if err != nil {
			fmt.Println("An error occurred", err)
			return
		}
		newPlayer := models.NewPlayer(conn)

		state.WorldPlayer[newPlayer.ID] = newPlayer
		fmt.Println("New player connected", newPlayer.ID)
		network.BroadcastToAll(fmt.Sprintf("Player %s has logged into the game!\r\n", newPlayer.Name))

		go handleClient(conn, newPlayer)
	}
}

func handleClient(conn net.Conn, p *models.Player) {
	defer func() {
		delete(state.WorldPlayer, p.ID)
		network.BroadcastToAll(fmt.Sprintf("Player %s has logged out the game!\r\n", p.Name))
		conn.Close()
	}()

	network.SendNoticeToPlayer(fmt.Sprintf("Welcome to the Realm, %s!\r\nYour Spawn Position is X: %s, Z: %s\r\n", p.Name, p.X, p.Z), conn)
	scan := bufio.NewScanner(conn)

	for scan.Scan() {
		message := scan.Text()
		length := len(message)
		conn.Write([]byte("Server send: " + message + "\r\n"))

		fmt.Println("Message Received", length, message)

		if strings.HasPrefix(message, "m") {
			mpart := strings.Split(message, " ")

			if len(mpart) == 3 {
				x_cor := mpart[1]
				y_cor := mpart[2]
				network.BroadcastToAll(fmt.Sprintf("Player %s want to move to position: X = %s, Z = %s\r\n", p.Name, x_cor, y_cor))
			} else {
				network.SendNoticeToPlayer("Invalid command, doesn't have 3 parts.\r\n", conn)
			}
		} else if strings.HasPrefix(message, "quit") {
			fmt.Printf("player %s send close command, shutting down server \n", conn.RemoteAddr())
			break
		} else if strings.HasPrefix(message, "info") {
			parts := strings.Split(message, " ")
			if len(parts) == 2 {
				targetID := parts[1]
				
				var foundMonster *models.Monster
				if targetID == "1" {
					foundMonster = state.MonsterRegistry[1]
				}
				if targetID == "2" {
					foundMonster = state.MonsterRegistry[2]
				}
				if targetID == "3" {
					foundMonster = state.MonsterRegistry[3]
				}

				if foundMonster != nil {
					network.SendNoticeToPlayer(fmt.Sprintf("👾 %s Stats -> HP: %d, ATK: %d\r\n", foundMonster.Name, foundMonster.HP, foundMonster.Dam), conn)
				} else {
					network.SendNoticeToPlayer("Monster ID not found in database.\r\n", conn)
				}
			} else {
				network.SendNoticeToPlayer("Syntax error. Use: INFO [1-3]\r\n", conn)
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