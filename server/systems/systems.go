package systems

import (
	"fmt"
	"net"
	"server/ecs"
	"time"
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
	// Set a 5-second write deadline to avoid blocking indefinitely on slow clients.
	_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(data); err != nil {
		// Close the socket on write failure to trigger deferred disconnect cleanup in handleClient.
		c.Close()
	}
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

	// NEW CELL COLLISION GATE: Verify spatial passability status
	if IsTileBlocked(pos.MapID, x, z) {
		SendNoticeSystem(entity, []byte(fmt.Sprintf("Collision Alert: Path is blocked by a solid obstacle at coordinate (%d, %d)!\r\n", x, z)))
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
	msg := fmt.Sprintf("Player %s moved to position: X=%d, Z=%d\r\n", meta.Name, x, z)
	BroadcastToMap(pos.MapID, msg)
	fmt.Printf("[MOVEMENT] %s → (%d, %d) on Map %d\n", meta.Name, x, z, pos.MapID)
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
