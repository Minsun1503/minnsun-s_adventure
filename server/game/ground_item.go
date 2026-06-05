package game

import (
	"fmt"
	"server/ecs"
	"server/protocol"
	"server/world"
)

// SpawnItemOnGround creates a live ground item entity in the ECS register.
func SpawnItemOnGround(itemTemplateID uint64, mapID int, x int, z int) ecs.Entity {
	// 1. Resolve item name details from the static ItemRegistry configuration
	itemTemplate, exists := ItemRegistry[itemTemplateID]
	if !exists {
		// Spawn Guard: refuse to spawn invalid templates to prevent world state corruption
		return 0
	}
	name := itemTemplate.Name

	// 2. Generate a lock-free atomic entity row ID
	itemEntity := ecs.DefaultRegistry.NewEntity()

	// 3. Populate spatial position columns and register with the Spatial Grid
	pos := ecs.PositionComponent{
		MapID: mapID,
		X:     x,
		Z:     z,
	}
	ecs.DefaultRegistry.SetPosition(itemEntity, pos)
	world.GlobalSpatialGrid.UpdateEntityPosition(itemEntity, pos)

	// 4. Populate metadata classification columns
	ecs.DefaultRegistry.SetMetadata(itemEntity, ecs.MetadataComponent{
		Name: name,
		Type: ecs.EntityGroundItem,
	})

	// 5. Populate ItemTemplateComponent so systems don't have to parse names
	ecs.DefaultRegistry.SetItemTemplate(itemEntity, ecs.ItemTemplateComponent{
		TemplateID: itemTemplateID,
	})

	// 6. Populate lifetime expiry columns (Set item to despawn in 240 ticks = 60 seconds at 4 ticks/sec)
	ecs.DefaultRegistry.SetLifetime(itemEntity, ecs.LifetimeComponent{
		SpawnedTick: GetCurrentTick(),
		Duration:    240, // 240 ticks = 60 seconds
	})

	// 6. Broadcast packet notice to nearby players using AOI-aware neighbor broadcast.
	notice := fmt.Sprintf("[DROP]: A %s dropped on the ground at position (%d, %d) [ID: %d]\r\n",
		name, x, z, itemEntity)
	protocol.BroadcastToNeighbors(pos, []byte(notice), itemEntity)

	return itemEntity
}

// RunGroundItemDecaySystem scans all active ground items and purges expired floor loot.
// Uses tick-based timing via GetCurrentTick() instead of time.Now().
func RunGroundItemDecaySystem() {
	currentTick := GetCurrentTick()

	ecs.DefaultRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		// 1. Only process ground items with a lifetime component
		if snap.Meta.Type != ecs.EntityGroundItem {
			return true // Skip players, permanent monsters, etc.
		}

		lifetime, hasLifetime := ecs.DefaultRegistry.GetLifetime(snap.ID)
		if !hasLifetime {
			return true
		}

		// 2. Evaluate if the expiry threshold duration has been crossed using tick comparison
		if currentTick >= lifetime.SpawnedTick+lifetime.Duration {
			// Fetch data for the exit notification before clearing columns
			pos, posOk := ecs.DefaultRegistry.GetPosition(snap.ID)
			meta, metaOk := ecs.DefaultRegistry.GetMetadata(snap.ID)

			if posOk && metaOk {
				decayNotice := fmt.Sprintf("[DECAY]: The %s sitting at (%d, %d) faded away into dust.\r\n",
					meta.Name, pos.X, pos.Z)
				protocol.BroadcastToNeighbors(pos, []byte(decayNotice), snap.ID)
			}

			// 3. PURGE TRANSACTION: Clean up spatial grid and parallel memory tables completely
			world.GlobalSpatialGrid.RemoveEntity(snap.ID)
			ecs.DefaultRegistry.RemoveEntity(snap.ID)
		}

		return true
	})
}
