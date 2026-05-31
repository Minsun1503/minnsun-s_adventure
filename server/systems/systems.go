package systems

import (
	"fmt"
	"net"
	"server/ecs"
)

// BroadcastSystem sends a raw byte payload to every entity with an active connection.
// Single-pass over the connection map — no intermediate slice, no double lookup.
//
// Parameters:
//   - data: Pre-encoded byte payload to write.
func BroadcastSystem(data []byte) {
	ecs.GlobalRegistry.RangeConnections(func(_ ecs.Entity, conn ecs.ConnectionComponent) bool {
		writeConn(conn.Conn, data)
		return true
	})
}

// BroadcastExcept sends a payload to all connected entities except the excluded one.
// Useful for movement events: sender already knows their own position.
//
// Parameters:
//   - exclude: Entity ID to skip.
//   - data:    Pre-encoded byte payload.
func BroadcastExcept(exclude ecs.Entity, data []byte) {
	ecs.GlobalRegistry.RangeConnections(func(id ecs.Entity, conn ecs.ConnectionComponent) bool {
		if id != exclude {
			writeConn(conn.Conn, data)
		}
		return true
	})
}

// SendNoticeSystem sends a direct payload to a single entity's connection.
//
// Parameters:
//   - entity:  Target entity ID.
//   - data:    Pre-encoded byte payload.
func SendNoticeSystem(entity ecs.Entity, data []byte) {
	conn, ok := ecs.GlobalRegistry.GetConnection(entity)
	if ok && conn.Conn != nil {
		writeConn(conn.Conn, data)
	}
}

// writeConn is the single write point for all outbound TCP data.
// Centralizing here makes it easy to add error handling, metrics, or
// a write deadline later without touching every call site.
func writeConn(c net.Conn, data []byte) {
	if c == nil {
		return
	}
	// TODO: set c.SetWriteDeadline(time.Now().Add(writeTimeout)) khi production
	c.Write(data) //nolint:errcheck — disconnect handled by read-side goroutine
}

// MovementSystem validates and commits a position update.
// Returns true if the move was accepted and broadcast was sent.
// Returns false if boundary check failed (notice already sent to player).
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
	pos.X = x
	pos.Z = z
	registry.SetPosition(entity, pos)

	// ← NEW: sync spatial grid after confirmed ECS position write
	GlobalSpatialGrid.UpdateEntityPosition(entity, pos)

	meta, metaOk := registry.GetMetadata(entity)
	if !metaOk {
		return false
	}
	msg := fmt.Sprintf("Player %s moved to X=%d Z=%d\r\n", meta.Name, x, z)
	BroadcastExcept(entity, []byte(msg))
	fmt.Printf("[MOVEMENT] %s → (%d, %d)\n", meta.Name, x, z)
	return true
}

// GetInfoSystem retrieves formatted combat stats for a target entity.
//
// Parameters:
//   - target: Entity ID to query.
//
// Returns:
//   - Formatted stats string, or error if components are missing.
func GetInfoSystem(target ecs.Entity) (string, error) {
	registry := ecs.GlobalRegistry

	meta, metaOk := registry.GetMetadata(target)
	stats, statsOk := registry.GetStats(target)

	if !metaOk || !statsOk {
		return "", fmt.Errorf("entity %d: required components not found", target)
	}

	return fmt.Sprintf("%s → HP: %d  ATK: %d\r\n", meta.Name, stats.HP, stats.Dam), nil
}
