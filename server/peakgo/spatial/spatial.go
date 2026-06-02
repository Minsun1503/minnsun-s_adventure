// Package spatial provides high-level query helpers built on top of
// world.SpatialGrid and peakgo/gmath.
//
// # Why this package exists
//
// world.SpatialGrid is a low-level data structure — it returns raw ChunkEntry
// slices and requires callers to manually resolve ECS components. Every system
// that needs "who is near entity X" must:
//
//  1. Call GlobalSpatialGrid.QueryRadius(pos, radius, excludeID)
//  2. Iterate candidates
//  3. Resolve MetadataComponent (filter by type)
//  4. Resolve StatsComponent, PositionComponent
//  5. Remember to call FreeQueryCandidates to avoid GC pressure
//
// This package wraps that sequence into named, typed functions so new systems
// only think in terms of game concepts ("find the nearest monster") rather
// than spatial grid mechanics.
//
// # Relationship to world/proximity.go
//
// world/proximity.go already has GetNearbyPlayers and GetNearbyMonsters.
// This package sits one layer higher:
//   - proximity.go    → typed slices of ProximityResult (resolved components)
//   - spatial helpers → single-entity answers (GetNearest, CountInRadius, IsAnyInRange)
//
// # Peak Go contract
//
// All functions delegate to pooled spatial grid queries.
// No additional heap allocations beyond what the grid already amortises.
package spatial

import (
	"server/ecs"
	"server/peakgo/gmath"
	"server/world"
)

// NearestResult holds the closest entity found by a query, with its
// pre-resolved position to avoid a follow-up ECS lookup.
type NearestResult struct {
	ID  ecs.Entity
	Pos ecs.PositionComponent
}

// GetNearestPlayer returns the closest player-type entity within worldRadius
// of originID, excluding originID itself.
//
// Returns (result, true) if at least one player is found.
// Returns (zero, false) if no players are within range.
//
// The returned position is the snapshot stored in the spatial grid at query
// time — it may be up to one tick stale.
func GetNearestPlayer(originID ecs.Entity, worldRadius float64) (NearestResult, bool) {
	return getNearestByType(originID, worldRadius, "player")
}

// GetNearestMonster returns the closest monster-type entity within worldRadius
// of originID, excluding originID itself.
//
// Returns (result, true) if at least one monster is found.
// Returns (zero, false) if no monsters are within range.
func GetNearestMonster(originID ecs.Entity, worldRadius float64) (NearestResult, bool) {
	return getNearestByType(originID, worldRadius, "monster")
}

// CountInRadius returns the number of entities of the given type within
// worldRadius of originID.
//
// entityType: "player", "monster", "ground_item", or "" for all types.
// Useful for area-of-effect skills and aggro checks without allocating a full list.
func CountInRadius(originID ecs.Entity, worldRadius float64, entityType string) int {
	originPos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return 0
	}

	candidates := world.GlobalSpatialGrid.QueryRadius(originPos, worldRadius, originID)
	if len(candidates) == 0 {
		world.FreeQueryCandidates(candidates)
		return 0
	}
	defer world.FreeQueryCandidates(candidates)

	if entityType == "" {
		return len(candidates)
	}

	count := 0
	for _, c := range candidates {
		meta, ok := ecs.GlobalRegistry.GetMetadata(c.ID)
		if ok && meta.Type == entityType {
			count++
		}
	}
	return count
}

// IsAnyInRadius reports whether at least one entity of the given type exists
// within worldRadius of originID. Short-circuits on the first match — faster
// than CountInRadius when you only need a boolean answer.
//
// entityType: "player", "monster", "ground_item", or "" for any type.
func IsAnyInRadius(originID ecs.Entity, worldRadius float64, entityType string) bool {
	originPos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return false
	}

	candidates := world.GlobalSpatialGrid.QueryRadius(originPos, worldRadius, originID)
	if len(candidates) == 0 {
		world.FreeQueryCandidates(candidates)
		return false
	}
	defer world.FreeQueryCandidates(candidates)

	if entityType == "" {
		return len(candidates) > 0
	}

	for _, c := range candidates {
		meta, ok := ecs.GlobalRegistry.GetMetadata(c.ID)
		if ok && meta.Type == entityType {
			return true
		}
	}
	return false
}

// FilterInRadius returns all entity IDs of the given type within worldRadius
// of originID, appended into dst (pass nil to allocate fresh).
//
// The caller owns the returned slice. Use when you need the full list but
// don't need resolved components (cheaper than world.GetNearbyPlayers).
//
// entityType: "player", "monster", "ground_item", or "" for all types.
func FilterInRadius(originID ecs.Entity, worldRadius float64, entityType string, dst []ecs.Entity) []ecs.Entity {
	originPos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return dst
	}

	candidates := world.GlobalSpatialGrid.QueryRadius(originPos, worldRadius, originID)
	if len(candidates) == 0 {
		world.FreeQueryCandidates(candidates)
		return dst
	}
	defer world.FreeQueryCandidates(candidates)

	for _, c := range candidates {
		if entityType == "" {
			dst = append(dst, c.ID)
			continue
		}
		meta, ok := ecs.GlobalRegistry.GetMetadata(c.ID)
		if ok && meta.Type == entityType {
			dst = append(dst, c.ID)
		}
	}
	return dst
}

// DistanceBetween returns the squared Euclidean distance between two entities.
// Returns (-1, false) if either entity's position cannot be resolved.
//
// Returns squared distance — compare against radius*radius to avoid sqrt.
// Use gmath.DistanceSq directly if you already have both positions.
func DistanceBetween(aID, bID ecs.Entity) (float64, bool) {
	aPos, okA := ecs.GlobalRegistry.GetPosition(aID)
	bPos, okB := ecs.GlobalRegistry.GetPosition(bID)
	if !okA || !okB {
		return -1, false
	}
	return gmath.DistanceSq(aPos.X, aPos.Z, bPos.X, bPos.Z), true
}

// SameMap reports whether two entities are on the same map.
// Returns false if either entity's position cannot be resolved.
// Useful as a fast pre-check before more expensive spatial queries.
func SameMap(aID, bID ecs.Entity) bool {
	aPos, okA := ecs.GlobalRegistry.GetPosition(aID)
	bPos, okB := ecs.GlobalRegistry.GetPosition(bID)
	return okA && okB && aPos.MapID == bPos.MapID
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

func getNearestByType(originID ecs.Entity, worldRadius float64, entityType string) (NearestResult, bool) {
	originPos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return NearestResult{}, false
	}

	candidates := world.GlobalSpatialGrid.QueryRadius(originPos, worldRadius, originID)
	if len(candidates) == 0 {
		world.FreeQueryCandidates(candidates)
		return NearestResult{}, false
	}
	defer world.FreeQueryCandidates(candidates)

	var nearest NearestResult
	nearestDSq := float64(-1)
	found := false

	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if !hasMeta || meta.Type != entityType {
			continue
		}
		dsq := gmath.DistanceSq(originPos.X, originPos.Z, c.Pos.X, c.Pos.Z)
		if !found || dsq < nearestDSq {
			nearest = NearestResult{ID: c.ID, Pos: c.Pos}
			nearestDSq = dsq
			found = true
		}
	}

	return nearest, found
}
