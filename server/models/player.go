package models

import "net"

//create player struct
type Player struct {
	ID   string
	Name string
	X    string
	Z    string
	Conn net.Conn
}

// NewPlayer builds an authoritative character data structure in RAM.
// It uses the player's network socket address to generate a unique ID
// and a temporary guest nickname based on the last 4 digits of their port.
//
// Inputs:
//   - conn: The live net.Conn TCP socket link for the player.
//
// Returns:
//   - A memory pointer (*Player) pointing directly to the new character house.
func NewPlayer(conn net.Conn) *Player {
	playerAddress := conn.RemoteAddr().String()
	guestName := "Guest_" + playerAddress[len(playerAddress)-4:]

	return &Player{
		ID:   playerAddress,
		Name: guestName,
		X:    "0",
		Z:    "0",
		Conn: conn,
	}
}
