package models

import (
	"net"
	"server/ecs"
	"server/state"
)

// ActivePlayers maps player network addresses (IP:port) to their ecs.Entity ID.
var ActivePlayers = &state.TypedSyncMap[string, ecs.Entity]{}

// CreatePlayerEntity registers a new player entity in the ECS registry and initializes its components.
//
// Parameters:
//   - conn: The live TCP socket connection of the player client.
//
// Returns:
//   - The newly registered ecs.Entity ID.
func CreatePlayerEntity(conn net.Conn) ecs.Entity {
	playerAddress := conn.RemoteAddr().String()
	guestName := "Guest_" + playerAddress[len(playerAddress)-4:]
	
	// Create a new entity ID atomically
	entityID := ecs.GlobalRegistry.NewEntity()

	// Set inline component values (not pointers)
	ecs.GlobalRegistry.SetPosition(entityID, ecs.PositionComponent{X: 0, Z: 0})
	ecs.GlobalRegistry.SetConnection(entityID, ecs.ConnectionComponent{Conn: conn})
	ecs.GlobalRegistry.SetMetadata(entityID, ecs.MetadataComponent{Name: guestName, Type: "player"})

	// Track active player mapping
	ActivePlayers.Set(playerAddress, entityID)

	return entityID
}
