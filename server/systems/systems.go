package systems

import (
	"fmt"
	"net"
	"server/ecs"
	"server/game"
	"time"
)

// BroadcastSystem sends a raw byte payload to every entity with an active connection.
func BroadcastSystem(data []byte) {
	ecs.GlobalRegistry.RangeConnections(func(_ ecs.Entity, conn ecs.ConnectionComponent) bool {
		writeConn(conn.Conn, data)
		return true
	})
}

// BroadcastExcept sends a payload to all connected entities except the excluded one.
func BroadcastExcept(exclude ecs.Entity, data []byte) {
	ecs.GlobalRegistry.RangeConnections(func(id ecs.Entity, conn ecs.ConnectionComponent) bool {
		if id != exclude {
			writeConn(conn.Conn, data)
		}
		return true
	})
}

// SendNoticeSystem sends a direct payload to a single entity's connection.
func SendNoticeSystem(entity ecs.Entity, data []byte) {
	game.SendNoticeSystem(entity, data)
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

// GetInfoSystem retrieves formatted combat stats for a target entity.
func GetInfoSystem(target ecs.Entity) (string, error) {
	registry := ecs.GlobalRegistry

	meta, metaOk := registry.GetMetadata(target)
	stats, statsOk := registry.GetStats(target)

	if !metaOk || !statsOk {
		return "", fmt.Errorf("entity %d: required components not found", target)
	}

	return fmt.Sprintf("%s → HP: %d  ATK: %d\r\n", meta.Name, stats.HP, stats.Dam), nil
}
