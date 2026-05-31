package models

import (
	"context"
	"net"
	"server/ecs"
	"server/state"
	"time"
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
//   - An error if database insertion fails.
func CreatePlayerEntity(conn net.Conn) (ecs.Entity, error) {
	playerAddress := conn.RemoteAddr().String()
	guestName := "Guest_" + playerAddress[len(playerAddress)-4:]
	
	// Create a new entity ID atomically
	entityID := ecs.GlobalRegistry.NewEntity()

	// Save static player info (ID & Name) to database once at login/creation
	if DBEngine != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := DBEngine.ExecContext(ctx, "INSERT INTO characters (id, name) VALUES (?, ?) ON DUPLICATE KEY UPDATE name=VALUES(name)", entityID, guestName); err != nil {
			return 0, err
		}
	}

	// Set inline component values (not pointers)
	ecs.GlobalRegistry.SetPosition(entityID, ecs.PositionComponent{MapID: 1, X: 0, Z: 0})
	ecs.GlobalRegistry.SetConnection(entityID, ecs.ConnectionComponent{Conn: conn})
	ecs.GlobalRegistry.SetMetadata(entityID, ecs.MetadataComponent{Name: guestName, Type: "player"})
	ecs.GlobalRegistry.SetStats(entityID, ecs.StatsComponent{HP: 100, MaxHP: 100, Dam: 15})
	ecs.GlobalRegistry.SetEquipment(entityID, ecs.EquipmentComponent{WeaponID: 0, ArmorID: 0})

	// Track active player mapping
	ActivePlayers.Set(playerAddress, entityID)

	return entityID, nil
}
