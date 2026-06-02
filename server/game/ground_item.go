package game

import (
	"fmt"
	"server/ecs"
	"server/protocol"
	"server/world"
	"time"
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
	itemEntity := ecs.GlobalRegistry.NewEntity()

	// 3. Populate spatial position columns and register with the Spatial Grid
	pos := ecs.PositionComponent{
		MapID: mapID,
		X:     x,
		Z:     z,
	}
	ecs.GlobalRegistry.SetPosition(itemEntity, pos)
	world.GlobalSpatialGrid.UpdateEntityPosition(itemEntity, pos)

	// 4. Populate metadata classification columns
	ecs.GlobalRegistry.SetMetadata(itemEntity, ecs.MetadataComponent{
		Name: name,
		Type: ecs.EntityGroundItem,
	})

	// 5. Populate ItemTemplateComponent so systems don't have to parse names
	ecs.GlobalRegistry.SetItemTemplate(itemEntity, ecs.ItemTemplateComponent{
		TemplateID: itemTemplateID,
	})

	// 6. Populate lifetime expiry columns (Set item to despawn in 60 seconds)
	ecs.GlobalRegistry.SetLifetime(itemEntity, ecs.LifetimeComponent{
		SpawnedAt: time.Now(),
		Duration:  60 * time.Second,
	})

	// 6. Broadcast packet notice to local area map witnesses only (no emojis)
	notice := fmt.Sprintf("[DROP]: A %s dropped on the ground at position (%d, %d) [ID: %d]\r\n",
		name, x, z, itemEntity)
	protocol.BroadcastToMap(mapID, notice)

	return itemEntity
}

// RunGroundItemDecaySystem scans all active ground items and purges expired floor loot.
// Uses RangeSnapshots for zero-extra-allocation scanning per architectural rules.
func RunGroundItemDecaySystem() {
	now := time.Now()

	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		// 1. Only process ground items with a lifetime component
		if snap.Meta.Type != ecs.EntityGroundItem {
			return true // Skip players, permanent monsters, etc.
		}

		lifetime, hasLifetime := ecs.GlobalRegistry.GetLifetime(snap.ID)
		if !hasLifetime {
			return true
		}

		// 2. Evaluate if the expiry threshold duration has been crossed
		if now.After(lifetime.SpawnedAt.Add(lifetime.Duration)) {
			// Fetch data for the exit notification before clearing columns
			pos, posOk := ecs.GlobalRegistry.GetPosition(snap.ID)
			meta, metaOk := ecs.GlobalRegistry.GetMetadata(snap.ID)

			if posOk && metaOk {
				decayNotice := fmt.Sprintf("[DECAY]: The %s sitting at (%d, %d) faded away into dust.\r\n",
					meta.Name, pos.X, pos.Z)
				protocol.BroadcastToMap(pos.MapID, decayNotice)
			}

			// 3. PURGE TRANSACTION: Clean up spatial grid and parallel memory tables completely
			world.GlobalSpatialGrid.RemoveEntity(snap.ID)
			ecs.GlobalRegistry.RemoveEntity(snap.ID)
		}

		return true
	})
}
