package world

import (
	"server/ecs"
	"server/peakgo/gmath"
	"server/peakgo/loggate"
	"sync"
)

// ProximityResult holds a nearby entity with its resolved components.
// Returned by proximity queries so callers don't need follow-up ECS lookups.
type ProximityResult struct {
	ID    ecs.Entity
	Pos   ecs.PositionComponent
	Meta  ecs.MetadataComponent
	Stats ecs.StatsComponent
}

var proximityPool = sync.Pool{
	New: func() any {
		s := make([]ProximityResult, 0, 16)
		return &s
	},
}

// FreeNearbyPlayers returns a pooled slice to the pool.
func FreeNearbyPlayers(s []ProximityResult) {
	if s == nil {
		return
	}
	s = s[:0]
	proximityPool.Put(&s)
}

// GetNearbyPlayers filters spatial grid candidates to player-type entities only,
// resolving components with zero allocations via slice pooling.
// Callers must call FreeNearbyPlayers(slice) when done to recycle memory.
func GetNearbyPlayers(originID ecs.Entity, worldRadius float64) []ProximityResult {
	pos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return nil
	}

	candidates := GlobalSpatialGrid.QueryRadius(pos, worldRadius, originID)
	if len(candidates) == 0 {
		return nil
	}

	pSlice := proximityPool.Get().(*[]ProximityResult)
	results := *pSlice
	results = results[:0]

	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if !hasMeta || meta.Type != "player" {
			continue // Filter non-players early
		}
		stats, hasStats := ecs.GlobalRegistry.GetStats(c.ID)
		if !hasStats {
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

// GetNearbyMonsters filters spatial grid candidates to monster-type entities only,
// resolving components with zero allocations via slice pooling.
// Callers must call FreeNearbyPlayers(slice) when done to recycle memory.
func GetNearbyMonsters(originID ecs.Entity, worldRadius float64) []ProximityResult {
	pos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return nil
	}

	candidates := GlobalSpatialGrid.QueryRadius(pos, worldRadius, originID)
	if len(candidates) == 0 {
		return nil
	}

	pSlice := proximityPool.Get().(*[]ProximityResult)
	results := *pSlice
	results = results[:0]

	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if !hasMeta || meta.Type != "monster" {
			continue // Filter non-monsters early
		}
		stats, hasStats := ecs.GlobalRegistry.GetStats(c.ID)
		if !hasStats {
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

// IsInRange checks whether a specific target entity is within worldRadius
// of the origin entity. Used by AttackSystem for melee range validation.
func IsInRange(originID, targetID ecs.Entity, worldRadius float64) bool {
	originPos, ok1 := ecs.GlobalRegistry.GetPosition(originID)
	targetPos, ok2 := ecs.GlobalRegistry.GetPosition(targetID)
	if !ok1 || !ok2 {
		return false
	}

	return gmath.InRange(originPos.X, originPos.Z, targetPos.X, targetPos.Z, worldRadius)
}

// NearbyEntitiesSystem is the game-loop facing system.
// Called from UpdateWorldEntitiesSystem per monster tick to find aggro targets.
func NearbyEntitiesSystem(monsterID ecs.Entity, aggroRadius float64) {
	meta, ok := ecs.GlobalRegistry.GetMetadata(monsterID)
	if !ok {
		return
	}

	nearby := GetNearbyPlayers(monsterID, aggroRadius)
	if len(nearby) == 0 {
		return
	}
	defer FreeNearbyPlayers(nearby)

	// loggate.Debugf: 0 allocs in production; full aggro trace in debug mode.
	loggate.Debugf("[AGGRO] %s detects %d player(s) within %.0f units",
		meta.Name, len(nearby), aggroRadius)

	for _, p := range nearby {
		loggate.Debugf("  → %s at (%d, %d) HP:%d",
			p.Meta.Name, p.Pos.X, p.Pos.Z, p.Stats.HP)
	}
}
