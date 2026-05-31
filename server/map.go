package main

import "fmt"

// create a book that store a infomation of any player
var WorldPlayer = make(map[string]*Player)

func BroadcastToAll(message string) {
	for _, p := range WorldPlayer {
		p.Conn.Write([]byte(message))
	}
	fmt.Println("[BROADCAST] " + message)
}
