// Package spatial provides high-level query helpers built on top of
// world.SpatialGrid and peakgo/gmath.
//
// # Why this package exists
//
// world.SpatialGrid is a low-level data structure — it returns raw ChunkEntry
// slices and requires callers to manually resolve ECS components. Every system
// that needs "who is near entity X" must safely query, iterate, filter by type,
// and remember to free query candidates to avoid GC pressure.
//
// This package wraps that sequence into named, typed functions so new systems
// only think in terms of game concepts ("find the nearest monster") rather
// than spatial grid mechanics.
//
// # Peak Go contract
//
// All functions delegate to pooled spatial grid queries.
// No additional heap allocations beyond what the grid already amortises.
package spatial

import (
	"math"
	"server/ecs"
	"server/peakgo/gmath"
	"server/world"
)

// ─── Entity Type Enum ────────────────────────────────────────────────────────

// EntityType represents a strictly typed identifier for game entity categories.
// Replaces fragile raw strings to catch invalid type queries at compile time.
type EntityType uint8

const (
	EntityAny        EntityType = iota // Matches any entity type in the grid
	EntityPlayer                       // Matches "player" type entities
	EntityMonster                      // Matches "monster" type entities
	EntityGroundItem                   // Matches "ground_item" type entities
)

// String maps the strongly-typed EntityType enum back to the underlying
// ECS Metadata string representation. Acts as an isolated compatibility layer.
func (t EntityType) String() string {
	switch t {
	case EntityPlayer:
		return "player"
	case EntityMonster:
		return "monster"
	case EntityGroundItem:
		return "ground_item"
	default:
		return ""
	}
}

// ─── Query Results Structures ────────────────────────────────────────────────

// NearestResult holds the closest entity found by a query, with its
// pre-resolved position to avoid a follow-up expensive ECS lookup.
type NearestResult struct {
	ID  ecs.Entity
	Pos ecs.PositionComponent
}

// ─── High-Level Typed Semantic API ───────────────────────────────────────────
//
// These functions provide a highly readable, self-documenting API for common
// gameplay queries, short-circuiting internals wherever possible.

// GetNearestPlayer returns the closest player-type entity within worldRadius of originID.
func GetNearestPlayer(originID ecs.Entity, worldRadius float64) (NearestResult, bool) {
	return getNearestByType(originID, worldRadius, EntityPlayer)
}

// GetNearestMonster returns the closest monster-type entity within worldRadius of originID.
func GetNearestMonster(originID ecs.Entity, worldRadius float64) (NearestResult, bool) {
	return getNearestByType(originID, worldRadius, EntityMonster)
}

// CountPlayersInRadius counts the total number of players within range.
func CountPlayersInRadius(originID ecs.Entity, worldRadius float64) int {
	return CountInRadius(originID, worldRadius, EntityPlayer)
}

// CountMonstersInRadius counts the total number of monsters within range.
func CountMonstersInRadius(originID ecs.Entity, worldRadius float64) int {
	return CountInRadius(originID, worldRadius, EntityMonster)
}

// HasPlayerInRadius reports whether at least one player exists within range.
func HasPlayerInRadius(originID ecs.Entity, worldRadius float64) bool {
	return IsAnyInRadius(originID, worldRadius, EntityPlayer)
}

// HasMonsterInRadius reports whether at least one monster exists within range.
func HasMonsterInRadius(originID ecs.Entity, worldRadius float64) bool {
	return IsAnyInRadius(originID, worldRadius, EntityMonster)
}

// ─── Generic Core Spatial Queries ────────────────────────────────────────────

// CountInRadius returns the number of entities of the given type within worldRadius of originID.
// Useful for area-of-effect skills and aggro checks without allocating an expensive list.
func CountInRadius(originID ecs.Entity, worldRadius float64, entityType EntityType) int {
	_, candidates, ok := queryRadius(originID, worldRadius)
	if !ok {
		return 0
	}
	defer world.FreeQueryCandidates(candidates)

	if entityType == EntityAny {
		return len(candidates)
	}

	targetStr := entityType.String()
	count := 0
	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if hasMeta && meta.Type == targetStr {
			count++
		}
	}
	return count
}

// IsAnyInRadius reports whether at least one entity of the given type exists within worldRadius of originID.
// Short-circuits on the very first match — highly optimized for boolean conditions.
func IsAnyInRadius(originID ecs.Entity, worldRadius float64, entityType EntityType) bool {
	_, candidates, ok := queryRadius(originID, worldRadius)
	if !ok {
		return false
	}
	defer world.FreeQueryCandidates(candidates)

	if entityType == EntityAny {
		return len(candidates) > 0
	}

	targetStr := entityType.String()
	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if hasMeta && meta.Type == targetStr {
			return true
		}
	}
	return false
}

// FilterInRadius returns all entity IDs of the given type within worldRadius of originID,
// appended directly into dst (pass nil to allocate fresh).
func FilterInRadius(originID ecs.Entity, worldRadius float64, entityType EntityType, dst []ecs.Entity) []ecs.Entity {
	_, candidates, ok := queryRadius(originID, worldRadius)
	if !ok {
		return dst
	}
	defer world.FreeQueryCandidates(candidates)

	targetStr := entityType.String()
	for _, c := range candidates {
		if entityType == EntityAny {
			dst = append(dst, c.ID)
			continue
		}
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if hasMeta && meta.Type == targetStr {
			dst = append(dst, c.ID)
		}
	}
	return dst
}

// IsInRadius checks if entity bID is within a specific radius of entity aID.
// Highly pragmatic helper for fast proximity tests like monster aggro or interactions.
func IsInRadius(aID ecs.Entity, bID ecs.Entity, radius float64) bool {
	distSq, ok := DistanceBetween(aID, bID)
	if !ok {
		return false
	}
	return float64(distSq) <= (radius * radius)
}

// DistanceBetween returns the squared Euclidean distance between two entities using pure integers.
// Returns (0, false) if either entity's position cannot be resolved.
func DistanceBetween(aID, bID ecs.Entity) (int, bool) {
	aPos, okA := ecs.GlobalRegistry.GetPosition(aID)
	bPos, okB := ecs.GlobalRegistry.GetPosition(bID)
	if !okA || !okB {
		return 0, false
	}
	// Không còn ép kiểu float64, trả về int thô từ gmath giúp tối ưu CPU tối đa
	return gmath.DistanceSq(aPos.X, aPos.Z, bPos.X, bPos.Z), true
}

// SameMap reports whether two entities are on the same map zone identifier.
// Useful as a lightning-fast pre-check before expensive spatial grid math.
func SameMap(aID, bID ecs.Entity) bool {
	aPos, okA := ecs.GlobalRegistry.GetPosition(aID)
	bPos, okB := ecs.GlobalRegistry.GetPosition(bID)
	return okA && okB && aPos.MapID == bPos.MapID
}

// ─── Internal Helpers (Single Source of Truth) ───────────────────────────────

// queryRadius abstracts the repetitive boilerplate sequence required to query the spatial grid.
// Centralizes dependencies so changes to world.GlobalSpatialGrid only require editing this single block.
func queryRadius(originID ecs.Entity, worldRadius float64) (ecs.PositionComponent, []world.ChunkEntry, bool) {
	originPos, ok := ecs.GlobalRegistry.GetPosition(originID)
	if !ok {
		return ecs.PositionComponent{}, nil, false
	}

	candidates := world.GlobalSpatialGrid.QueryRadius(originPos, worldRadius, originID)
	if len(candidates) == 0 {
		world.FreeQueryCandidates(candidates) // Always clear slices cleanly
		return originPos, nil, false
	}

	return originPos, candidates, true
}

// getNearestByType extracts the closest match using math.MaxInt bounds for pristine syntax.
func getNearestByType(originID ecs.Entity, worldRadius float64, entityType EntityType) (NearestResult, bool) {
	originPos, candidates, ok := queryRadius(originID, worldRadius)
	if !ok {
		return NearestResult{}, false
	}
	defer world.FreeQueryCandidates(candidates)

	var nearest NearestResult
	nearestDSq := math.MaxInt // Optimized: Avoid tracking cumbersome found booleans
	targetStr := entityType.String()

	for _, c := range candidates {
		meta, hasMeta := ecs.GlobalRegistry.GetMetadata(c.ID)
		if !hasMeta || meta.Type != targetStr {
			continue
		}

		dsq := gmath.DistanceSq(originPos.X, originPos.Z, c.Pos.X, c.Pos.Z)
		if dsq < nearestDSq {
			nearest = NearestResult{ID: c.ID, Pos: c.Pos}
			nearestDSq = dsq
		}
	}

	// If nearestDSq was updated, we found a target
	if nearestDSq < math.MaxInt {
		return nearest, true
	}
	return NearestResult{}, false
}
