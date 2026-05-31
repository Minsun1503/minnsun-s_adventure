package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)

func main() {
	err := LoadMonster()
	if err != nil {
		fmt.Println("CRITICAL SERVER BOOT ERROR:", err)
		return
	}

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
		newPlayer := NewPlayerBlueprint(conn)

		WorldPlayer[newPlayer.ID] = newPlayer
		fmt.Println("New player connected", newPlayer.ID)
		BroadcastToAll(fmt.Sprintf("Player %s has logged into the game!\r\n", newPlayer.Name))

		go handleClient(conn, newPlayer)
	}

}

func handleClient(conn net.Conn, p *Player) {
	defer func() {
		delete(WorldPlayer, p.ID)
		BroadcastToAll(fmt.Sprintf("Player %s has logged out the game!\r\n", p.Name))
		conn.Close()
	}()

	send_notice_to_player(fmt.Sprintf("Welcome to the Realm, %s!\r\nYour Spawn Position is X: %s, Z: %s\r\n", p.Name, p.X, p.Z), conn)
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
				BroadcastToAll(fmt.Sprintf("Player %s want to move to position: X = %s, Z = %s\r\n", p.Name, x_cor, y_cor))
			} else {
				send_notice_to_player("Invalid command, doesn't have 3 parts.\r\n", conn)
			}
		} else if strings.HasPrefix(message, "Exit") {
			fmt.Printf("player %s send close command, shutting down server \n", conn.RemoteAddr())
			break
		} else {
			fmt.Println("Nomarl chat")
		}
	}

	if err := scan.Err(); err != nil {
		fmt.Printf("An error ocurred with player %s: %v\n", conn.RemoteAddr(), err)
	}
	fmt.Printf("player %s disconnected, shuting down\n", conn.RemoteAddr())
}

func send_notice_to_player(message string, conn net.Conn) {
	conn.Write([]byte(message))
}
