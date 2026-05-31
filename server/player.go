package main

import "net"

type Player struct {
	ID   string
	Name string
	X    string
	Z    string
	Conn net.Conn
}

func NewPlayerBlueprint(conn net.Conn) *Player {
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
