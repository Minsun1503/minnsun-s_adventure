package models

import (
	"net"
	"server/ecs"
)

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
	entityID := ecs.Entity(playerAddress)

	ecs.GlobalRegistry.RegisterEntity(entityID)
	ecs.GlobalRegistry.SetPosition(entityID, &ecs.PositionComponent{X: 0, Z: 0})
	ecs.GlobalRegistry.SetConnection(entityID, &ecs.ConnectionComponent{Conn: conn})
	ecs.GlobalRegistry.SetMetadata(entityID, &ecs.MetadataComponent{Name: guestName, Type: "player"})

	return entityID
}
