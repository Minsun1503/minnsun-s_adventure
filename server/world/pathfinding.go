package world

import (
	"server/ecs"
	"sync"
)

type point struct {
	x, z int
}

// pathfindContext holds reusable buffers to eliminate pathfinding heap allocations.
type pathfindContext struct {
	queue   []point
	parent  map[point]point
	visited map[point]bool
}

// Global pool of pathfinding contexts
var contextPool = sync.Pool{
	New: func() interface{} {
		return &pathfindContext{
			queue:   make([]point, 0, 400),
			parent:  make(map[point]point, 400),
			visited: make(map[point]bool, 400),
		}
	},
}

// FindPath executes a BFS search to find the next step (X, Z) towards the target position.
// It avoids blocked tiles and uses a sync.Pool memory context to avoid heap allocations.
func FindPath(from, to ecs.PositionComponent) (int, int) {
	if from.X == to.X && from.Z == to.Z {
		return from.X, from.Z
	}

	// If the target tile itself is blocked, pathfinding is impossible. Fallback immediately.
	if IsTileBlocked(from.MapID, to.X, to.Z) {
		return StepToward(from, to)
	}

	// Acquire context from the pool
	ctx := contextPool.Get().(*pathfindContext)
	defer func() {
		// Reset structures and return to the pool to prevent memory leaks
		clear(ctx.parent)
		clear(ctx.visited)
		ctx.queue = ctx.queue[:0]
		contextPool.Put(ctx)
	}()

	// Initialize pathfinding search root
	ctx.queue = append(ctx.queue, point{x: from.X, z: from.Z})
	ctx.visited[point{x: from.X, z: from.Z}] = true

	// 8-Directional movement vectors (includes diagonals)
	dirs := []point{
		{x: 0, z: 1},   // North
		{x: 1, z: 1},   // North-East
		{x: 1, z: 0},   // East
		{x: 1, z: -1},  // South-East
		{x: 0, z: -1},  // South
		{x: -1, z: -1}, // South-West
		{x: -1, z: 0},  // West
		{x: -1, z: 1},  // North-West
	}

	found := false
	var targetPt point

	// Search limit to protect CPU tick rate (max 400 node expansions)
	maxSearched := 400
	searchedCount := 0

	for len(ctx.queue) > 0 && searchedCount < maxSearched {
		curr := ctx.queue[0]
		ctx.queue = ctx.queue[1:]
		searchedCount++

		if curr.x == to.X && curr.z == to.Z {
			found = true
			targetPt = curr
			break
		}

		for _, d := range dirs {
			next := point{x: curr.x + d.x, z: curr.z + d.z}

			// Boundary checks
			if next.x < 0 || next.x > 100 || next.z < 0 || next.z > 100 {
				continue
			}

			// Obstacle checks
			if IsTileBlocked(from.MapID, next.x, next.z) {
				continue
			}

			if !ctx.visited[next] {
				ctx.visited[next] = true
				ctx.parent[next] = curr
				ctx.queue = append(ctx.queue, next)
			}
		}
	}

	if found {
		// Traverse parents backwards to find the first step from origin
		curr := targetPt
		for {
			p, ok := ctx.parent[curr]
			if !ok {
				break
			}
			if p.x == from.X && p.z == from.Z {
				return curr.x, curr.z
			}
			curr = p
		}
	}

	// Fallback to standard straight step if path is blocked or target unreachable
	return StepToward(from, to)
}

// StepToward returns a position one unit closer to the target.
// Moves on the axis with the larger delta first (Chebyshev step).
func StepToward(from, to ecs.PositionComponent) (int, int) {
	nx, nz := from.X, from.Z

	if from.X < to.X {
		nx++
	} else if from.X > to.X {
		nx--
	}

	if from.Z < to.Z {
		nz++
	} else if from.Z > to.Z {
		nz--
	}

	return nx, nz
}
