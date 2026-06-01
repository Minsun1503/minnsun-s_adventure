package game

import (
	"fmt"
	"net"
	"server/ecs"
	"server/peakgo/codec"
	"server/peakgo/gmath"
	"server/peakgo/loggate"
	"server/peakgo/netio"
	"server/protocol"
	"server/world"
)

// HandlePlayerMovementSystem parses a binary payload containing target X and Z coordinates.
// Payload layout: [X (int32 - BE)] [Z (int32 - BE)] (total 8 bytes)
//
// Returns:
//   - (errorMsg, false) if parsing fails before reaching MovementSystem.
//   - ("", true)        if MovementSystem was invoked.
func HandlePlayerMovementSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	// codec.ReadMovePayload: typed hot-path decoder — validates length + decodes in one call.
	p, ok := codec.ReadMovePayload(payload)
	if !ok {
		return "Error: Invalid movement payload length. Expected 8 bytes.\r\n", false
	}

	MovementSystem(playerID, p.X, p.Z)
	return "", true
}

// SendNoticeSystem sends a direct payload to a single entity's connection.
func SendNoticeSystem(entity ecs.Entity, data []byte) {
	conn, ok := ecs.GlobalRegistry.GetConnection(entity)
	if ok && conn.Conn != nil {
		writeConn(conn.Conn, data)
	}
}

// writeConn is the single write point for all outbound TCP data.
func writeConn(c net.Conn, data []byte) {
	if c == nil {
		return
	}

	if err := netio.WritePacket(c, data); err != nil {
		c.Close()
	}
}

// MovementSystem validates and commits a position update.
func MovementSystem(entity ecs.Entity, x, z int) bool {
	// gmath.InBounds: single call replaces 4 comparisons (x<0||x>100||z<0||z>100).
	if !gmath.InBounds(x, z, 0, 100) {
		SendNoticeSystem(entity, []byte("Movement rejected! Out of bounds.\r\n"))
		return false
	}
	registry := ecs.GlobalRegistry
	pos, ok := registry.GetPosition(entity)
	if !ok {
		return false
	}

	if world.IsTileBlocked(pos.MapID, x, z) {
		SendNoticeSystem(entity, []byte(fmt.Sprintf("Collision Alert: Path is blocked by a solid obstacle at coordinate (%d, %d)!\r\n", x, z)))
		return false
	}

	pos.X = x
	pos.Z = z
	registry.SetPosition(entity, pos)

	world.GlobalSpatialGrid.UpdateEntityPosition(entity, pos)

	meta, metaOk := registry.GetMetadata(entity)
	if !metaOk {
		return false
	}
	msg := fmt.Sprintf("Player %s moved to position: X=%d, Z=%d\r\n", meta.Name, x, z)
	protocol.BroadcastToMap(pos.MapID, msg)
	loggate.Debugf("[MOVEMENT] %s → (%d, %d) on Map %d", meta.Name, x, z, pos.MapID)
	return true
}
