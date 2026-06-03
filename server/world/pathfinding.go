package world

import (
	"server/ecs"
	"server/peakgo/astar"
)

// isWalkableDefault always returns true (no tile-based blocking in current map).
// In future, this should query the map's collision grid.
func isWalkableDefault(x, z int) bool {
	_ = x
	_ = z
	return true
}

// FindPath finds a complete path from one position to another.
// Returns the x,z of the first step toward the target, or (0,0) if no path exists.
// Kept for backward compatibility with the old BFS API.
func FindPath(from, to ecs.PositionComponent) (int, int) {
	result := astar.FindPath(from.X, from.Z, to.X, to.Z, isWalkableDefault, astar.MaxPathNodes)
	if !result.Found || result.Len == 0 {
		return 0, 0
	}
	return int(result.Points[0].X), int(result.Points[0].Z)
}

// StepToward computes the next single step from 'from' toward 'to'.
// This is the primary function used by AI movement systems.
func StepToward(from, to ecs.PositionComponent) (int, int) {
	result := astar.FindPath(from.X, from.Z, to.X, to.Z, isWalkableDefault, astar.MaxPathNodes)
	if !result.Found || result.Len == 0 {
		return from.X, from.Z
	}
	// Points[0] is the current position (start). Points[1] is the next step.
	if result.Len >= 2 {
		return int(result.Points[1].X), int(result.Points[1].Z)
	}
	return from.X, from.Z
}
