package systems

import (
	"fmt"
	"server/ecs"
)

// ProximityResult holds a nearby entity with its resolved components.
// Returned by proximity queries so callers don't need follow-up ECS lookups.
type ProximityResult struct {
	ID    ecs.Entity
	Pos   ecs.PositionComponent
	Meta  ecs.MetadataComponent
	Stats ecs.StatsComponent
}

// GetNearbyEntities returns all entities within worldRadius of the given entity,
// with components pre-resolved. Entities missing metadata or stats are silently skipped.
//
// Parameters:
//   - originID:    the querying entity (excluded from results).
//   - worldRadius: search radius in world units.
func GetNearbyEntities(originID ecs.Entity, worldRadius float64) []ProximityResult {
	pos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return nil
	}

	candidates := GlobalSpatialGrid.QueryRadius(pos, worldRadius, originID)
	results := make([]ProximityResult, 0, len(candidates))

	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		stats, hasStats := ecs.GlobalRegistry.GetStats(c.ID)
		if !hasMeta || !hasStats {
			continue
		}
		results = append(results, ProximityResult{
			ID:    c.ID,
			Pos:   c.Pos,
			Meta:  meta,
			Stats: stats,
		})
	}
	return results
}

// GetNearbyPlayers filters GetNearbyEntities to player-type entities only.
// Used by monster AI to find aggro targets.
func GetNearbyPlayers(originID ecs.Entity, worldRadius float64) []ProximityResult {
	nearby := GetNearbyEntities(originID, worldRadius)
	players := nearby[:0] // reuse backing array, no alloc
	for _, r := range nearby {
		if r.Meta.Type == "player" {
			players = append(players, r)
		}
	}
	return players
}

// GetNearbyMonsters filters to monster-type entities only.
// Used by player attack commands to validate target is in range.
func GetNearbyMonsters(originID ecs.Entity, worldRadius float64) []ProximityResult {
	nearby := GetNearbyEntities(originID, worldRadius)
	monsters := nearby[:0]
	for _, r := range nearby {
		if r.Meta.Type == "monster" {
			monsters = append(monsters, r)
		}
	}
	return monsters
}

// IsInRange checks whether a specific target entity is within worldRadius
// of the origin entity. Used by AttackSystem for melee range validation.
func IsInRange(originID, targetID ecs.Entity, worldRadius float64) bool {
	originPos, ok1 := ecs.GlobalRegistry.GetPosition(originID)
	targetPos, ok2 := ecs.GlobalRegistry.GetPosition(targetID)
	if !ok1 || !ok2 {
		return false
	}

	dx := float64(originPos.X - targetPos.X)
	dz := float64(originPos.Z - targetPos.Z)
	return dx*dx+dz*dz <= worldRadius*worldRadius
}

// NearbyEntitiesSystem is the game-loop facing system.
// Called from UpdateWorldEntitiesSystem per monster tick to find aggro targets.
// Logs nearby player count for monitoring; replace with AI logic later.
func NearbyEntitiesSystem(monsterID ecs.Entity, aggroRadius float64) {
	meta, ok := ecs.GlobalRegistry.GetMetadata(monsterID)
	if !ok {
		return
	}

	nearby := GetNearbyPlayers(monsterID, aggroRadius)
	if len(nearby) == 0 {
		return
	}

	fmt.Printf("[AGGRO] %s detects %d player(s) within %.0f units\n",
		meta.Name, len(nearby), aggroRadius)

	for _, p := range nearby {
		fmt.Printf("  → %s at (%d, %d) HP:%d\n",
			p.Meta.Name, p.Pos.X, p.Pos.Z, p.Stats.HP)
	}
}
