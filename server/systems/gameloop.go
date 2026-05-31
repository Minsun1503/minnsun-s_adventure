package systems

import (
	"fmt"
	"server/ecs"
	"time"
)

// StartGameLoop initializes the server's heartbeat clock.
// We set it to trigger exactly 4 times a second (250-millisecond ticks).
func StartGameLoop() {
	// A 250ms tick rate is perfect for classic 2D RPG logic loops
	ticker := time.NewTicker(250 * time.Millisecond)

	// Run the loop in a background thread so it doesn't block player connections
	go func() {
		fmt.Println("[ENGINE] Heartbeat game loop started at 4 ticks/sec.")

		for range ticker.C {
			// 1. Core check: Are there actually players on the map?
			if !HasActivePlayers() {
				// MAP SLEEP TRICK: If nobody is here, skip heavy math!
				continue
			}

			// 2. If the map is awake, execute entity simulation math
			// UpdateWorldEntitiesSystem()
		}
	}()
}

// HasActivePlayers checks if there are any active player entities registered in ECS.
//
// Returns:
//   - true if at least one entity has a MetadataComponent with type "player", false otherwise.
func HasActivePlayers() bool {
	registry := ecs.GlobalRegistry
	entities := registry.GetAllEntities()
	for _, entity := range entities {
		if meta := registry.GetMetadata(entity); meta != nil && meta.Type == "player" {
			return true
		}
	}
	return false
}

// UpdateWorldEntitiesSystem processes AI, monster logic, and status tickers.
func UpdateWorldEntitiesSystem() {
	// For this test lecture, let's just print out a heartbeat log line
	fmt.Println("[LOOP] Map is active. Processing AI behavior matrix...")
}
