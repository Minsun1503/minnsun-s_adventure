package world

import (
	"server/ecs"
	"server/peakgo/astar"
)

// IsWalkableForMap returns a walkability checker bound to a specific map ID.
// Replaces the old isWalkableDefault which hardcoded map ID 1.
func IsWalkableForMap(mapID int) astar.IsWalkable {
	return func(x, z int) bool {
		return !IsTileBlocked(mapID, x, z)
	}
}

// FindPath finds a complete path from one position to another.
// Returns the x,z of the first step toward the target, or (0,0) if no path exists.
// Kept for backward compatibility with the old BFS API.
func FindPath(from, to ecs.PositionComponent) (int, int) {
	walkable := IsWalkableForMap(from.MapID)
	result := astar.FindPath(from.X, from.Z, to.X, to.Z, walkable, astar.MaxPathNodes)
	if !result.Found || result.Len == 0 {
		return 0, 0
	}
	return int(result.Points[0].X), int(result.Points[0].Z)
}

// StepToward computes the next single step from 'from' toward 'to'.
// This is the primary function used by AI movement systems.
// Accepts an optional *astar.PathCache for zero-alloc hot-path reuse.
// If pc is nil, a fresh PathResult is allocated (fallback path).
func StepToward(from, to ecs.PositionComponent, pc *astar.PathCache) (int, int) {
	walkable := IsWalkableForMap(from.MapID)
	var result astar.PathResult
	if pc != nil {
		result = astar.FindPathWithCache(pc, from.X, from.Z, to.X, to.Z, walkable, astar.MaxPathNodes)
	} else {
		result = astar.FindPath(from.X, from.Z, to.X, to.Z, walkable, astar.MaxPathNodes)
	}
	if !result.Found || result.Len == 0 {
		return from.X, from.Z
	}
	// Points[0] is the current position (start). Points[1] is the next step.
	if result.Len >= 2 {
		return int(result.Points[1].X), int(result.Points[1].Z)
	}
	return from.X, from.Z
}
