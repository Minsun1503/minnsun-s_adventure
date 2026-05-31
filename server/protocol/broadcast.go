package protocol

import (
	"net"
	"server/ecs"
	"time"
)

// BroadcastToMap transits a network packet ONLY to player entities
// who currently occupy the exact same map zone index sector.
func BroadcastToMap(targetMapID int, textPacket string) {
	bytePayload := []byte(textPacket)

	// Interrogate all active client network connections lock-free
	ecs.GlobalRegistry.RangeConnections(func(playerID ecs.Entity, netComp ecs.ConnectionComponent) bool {
		if netComp.Conn == nil {
			return true // Skip broken sockets, continue scanning
		}

		// Pull the target client's spatial position record
		playerPos, posExists := ecs.GlobalRegistry.GetPosition(playerID)

		// ZONING FILTER: Only transit data bytes if player is on the same map grid!
		if posExists && playerPos.MapID == targetMapID {
			writeMapConn(netComp.Conn, bytePayload)
		}

		return true // Continue scanning the rest of the map connections
	})
}

// writeMapConn is the single write point for all outbound map TCP data.
func writeMapConn(c net.Conn, data []byte) {
	if c == nil {
		return
	}
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(data); err != nil {
		c.Close()
	}
}
