package game

import (
	"encoding/binary"
	"fmt"
	"net"
	"server/ecs"
	"server/logger"
	"server/protocol"
	"server/world"
	"time"
)

// HandlePlayerMovementSystem parses a binary payload containing target X and Z coordinates.
// Payload layout: [X (int32 - BE)] [Z (int32 - BE)] (total 8 bytes)
//
// Returns:
//   - (errorMsg, false) if parsing fails before reaching MovementSystem.
//   - ("", true)        if MovementSystem was invoked.
func HandlePlayerMovementSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	if len(payload) != 8 {
		return "Error: Invalid movement payload length. Expected 8 bytes.\r\n", false
	}

	targetX := int(int32(binary.BigEndian.Uint32(payload[0:4])))
	targetZ := int(int32(binary.BigEndian.Uint32(payload[4:8])))

	MovementSystem(playerID, targetX, targetZ)
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
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(data); err != nil {
		c.Close()
	}
}

// MovementSystem validates and commits a position update.
func MovementSystem(entity ecs.Entity, x, z int) bool {
	if x < 0 || x > 100 || z < 0 || z > 100 {
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
	logger.Debug("[MOVEMENT] %s → (%d, %d) on Map %d", meta.Name, x, z, pos.MapID)
	return true
}
