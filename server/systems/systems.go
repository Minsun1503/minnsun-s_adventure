package systems

import (
	"fmt"
	"server/ecs"
)

// BroadcastSystem sends a message to all entities containing a ConnectionComponent.
//
// Parameters:
//   - message: The text string to send.
func BroadcastSystem(message string) {
	registry := ecs.GlobalRegistry
	entities := registry.GetAllEntities()

	for _, entity := range entities {
		connComp := registry.GetConnection(entity)
		if connComp != nil && connComp.Conn != nil {
			connComp.Conn.Write([]byte(message))
		}
	}
	fmt.Print("[BROADCAST-SYSTEM] " + message)
}

// SendNoticeSystem sends a direct message to a single connection entity.
//
// Parameters:
//   - entity: The target entity ID.
//   - message: The message to send.
func SendNoticeSystem(entity ecs.Entity, message string) {
	registry := ecs.GlobalRegistry
	connComp := registry.GetConnection(entity)
	if connComp != nil && connComp.Conn != nil {
		connComp.Conn.Write([]byte(message))
	}
}

// MovementSystem updates the position component of a given entity after verifying map boundaries.
// It acts as the game logic system enforcing movement constraints.
//
// Parameters:
//   - entity: The entity ID to move.
//   - x: New X coordinate (int).
//   - z: New Z coordinate (int).
func MovementSystem(entity ecs.Entity, x, z int) {
	// SECURITY GUARDRAIL: Define map boundary lines (e.g., a map size of 100x100)
	if x < 0 || x > 100 || z < 0 || z > 100 {
		SendNoticeSystem(entity, "Movement rejected! Out of bounds.\r\n")
		return
	}

	registry := ecs.GlobalRegistry
	posComp := registry.GetPosition(entity)
	if posComp != nil {
		posComp.X = x
		posComp.Z = z

		// Broadcast the movement event to everyone
		if meta := registry.GetMetadata(entity); meta != nil {
			BroadcastSystem(fmt.Sprintf("Player %s want to move to position: X = %d, Z = %d\r\n", meta.Name, x, z))
		}
	}
}

// GetInfoSystem retrieves and formats stats for a given target entity (typically a monster).
//
// Parameters:
//   - target: The entity ID to query.
//
// Returns:
//   - A formatted string containing name, HP, and Damage.
//   - An error if the target entity or its components are not found.
func GetInfoSystem(target ecs.Entity) (string, error) {
	registry := ecs.GlobalRegistry
	meta := registry.GetMetadata(target)
	stats := registry.GetStats(target)

	if meta == nil || stats == nil {
		return "", fmt.Errorf("entity or required components not found")
	}

	return fmt.Sprintf("%s Stats -> HP: %d, ATK: %d\r\n", meta.Name, stats.HP, stats.Dam), nil
}
