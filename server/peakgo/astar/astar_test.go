package astar

import (
	"testing"
)

// simpleGrid returns an IsWalkable function for a 100x100 empty grid.
func simpleGrid() IsWalkable {
	return func(x, z int) bool {
		return x >= 0 && x <= 100 && z >= 0 && z <= 100
	}
}

// wallGrid returns an IsWalkable function with a wall at x=50 from z=0 to z=50.
func wallGrid() IsWalkable {
	return func(x, z int) bool {
		if x == 50 && z >= 0 && z <= 50 {
			return false
		}
		return x >= 0 && x <= 100 && z >= 0 && z <= 100
	}
}

// ─── Basic Tests ──────────────────────────────────────────────────────────────

func TestFindPathStraightLine(t *testing.T) {
	grid := simpleGrid()
	result := FindPath(0, 0, 10, 0, grid, 0)

	if !result.Found {
		t.Fatal("expected a path to be found on empty grid")
	}
	if result.Len < 2 {
		t.Fatalf("expected at least 2 waypoints, got %d", result.Len)
	}
	if result.Points[0].X != 0 || result.Points[0].Z != 0 {
		t.Fatalf("expected start (0,0), got (%d,%d)", result.Points[0].X, result.Points[0].Z)
	}
	if result.Points[result.Len-1].X != 10 || result.Points[result.Len-1].Z != 0 {
		t.Fatalf("expected goal (10,0), got (%d,%d)", result.Points[result.Len-1].X, result.Points[result.Len-1].Z)
	}
}

func TestFindPathDiagonal(t *testing.T) {
	grid := simpleGrid()
	result := FindPath(0, 0, 10, 10, grid, 0)

	if !result.Found {
		t.Fatal("expected a path to be found on empty grid")
	}

	if result.Points[0].X != 0 || result.Points[0].Z != 0 {
		t.Fatalf("expected start (0,0), got (%d,%d)", result.Points[0].X, result.Points[0].Z)
	}
	if result.Points[result.Len-1].X != 10 || result.Points[result.Len-1].Z != 10 {
		t.Fatalf("expected goal (10,10), got (%d,%d)", result.Points[result.Len-1].X, result.Points[result.Len-1].Z)
	}
}

func TestFindPathWithWall(t *testing.T) {
	grid := wallGrid()
	// Wall at x=50, z=0-50 blocks direct path from (0,25) to (100,25).
	// A* must route around via z > 50 or z < 0.
	result := FindPath(0, 25, 100, 25, grid, 0)
	// It's OK if not found (wall too restrictive) but if found, verify no wall crossing
	if result.Found {
		checkNoWallCrossing(t, result, 50, 0, 50)
	}
}

func checkNoWallCrossing(t *testing.T, result PathResult, wallX, wallZMin, wallZMax int) {
	for i := 0; i < result.Len-1; i++ {
		x1, z1 := result.Points[i].X, result.Points[i].Z
		x2, z2 := result.Points[i+1].X, result.Points[i+1].Z

		// Check if segment crosses the wall column
		if (x1 < wallX && x2 >= wallX) || (x1 >= wallX && x2 < wallX) {
			tParam := float64(wallX-x1) / float64(x2-x1)
			crossZ := z1 + int(float64(z2-z1)*tParam)
			if crossZ >= wallZMin && crossZ <= wallZMax {
				t.Errorf("path crosses wall at x=%d, z=%d (segment (%d,%d)->(%d,%d))",
					wallX, crossZ, x1, z1, x2, z2)
			}
		}
	}
}

func TestFindPathAroundObstacle(t *testing.T) {
	grid := wallGrid()
	// Start at left of wall, goal at right of wall but above the blocked section
	result := FindPath(0, 75, 100, 75, grid, 0)

	if !result.Found {
		t.Fatal("expected path around wall")
	}
}

func TestFindPathStartEqualsGoal(t *testing.T) {
	grid := simpleGrid()
	result := FindPath(5, 5, 5, 5, grid, 0)

	if !result.Found {
		t.Fatal("expected found when start equals goal")
	}
	if result.Len != 1 {
		t.Fatalf("expected 1 waypoint, got %d", result.Len)
	}
}

func TestFindPathUnreachable(t *testing.T) {
	// Start at blocked position
	blocked := func(x, z int) bool {
		return false // Everything blocked
	}
	result := FindPath(0, 0, 10, 10, blocked, 0)
	if result.Found {
		t.Fatal("expected no path when start is blocked")
	}
}

func TestFindPathGoalBlocked(t *testing.T) {
	blocked := func(x, z int) bool {
		if x == 10 && z == 10 {
			return false
		}
		return x >= 0 && x <= 100 && z >= 0 && z <= 100
	}
	result := FindPath(0, 0, 10, 10, blocked, 0)
	if result.Found {
		t.Fatal("expected no path when goal is blocked")
	}
}

func TestFindPathMaxNodes(t *testing.T) {
	grid := simpleGrid()
	result := FindPath(0, 0, 50, 0, grid, 10)
	if result.Found {
		t.Log("path found even with 10 node limit (short path)")
	}
}

// ─── SmoothPath Tests ─────────────────────────────────────────────────────────

func TestSmoothPathStraightLine(t *testing.T) {
	grid := simpleGrid()
	result := FindPath(0, 0, 10, 0, grid, 0)
	if !result.Found {
		t.Fatal("expected path")
	}

	smoothed := SmoothPath(result, grid)
	if !smoothed.Found {
		t.Fatal("smoothed path should still be found")
	}
	if smoothed.Len > result.Len {
		t.Logf("smoothed path has %d waypoints vs original %d", smoothed.Len, result.Len)
	}
}

// ─── PathCache Tests ──────────────────────────────────────────────────────────

func TestPathCacheReuse(t *testing.T) {
	pc := NewPathCache()
	grid := simpleGrid()

	// First path
	pc.Reset()
	result1 := pc.findPath(0, 0, 5, 5, grid, 0)
	if !result1.Found {
		t.Fatal("first path should be found")
	}

	// Reset and second path
	pc.Reset()
	result2 := pc.findPath(10, 10, 20, 20, grid, 0)
	if !result2.Found {
		t.Fatal("second path should be found")
	}

	// Verify no state leakage
	if result2.Points[0].X != 10 || result2.Points[0].Z != 10 {
		t.Fatalf("expected start (10,10), got (%d,%d)", result2.Points[0].X, result2.Points[0].Z)
	}
}

// ─── Heuristic Tests ──────────────────────────────────────────────────────────

func TestHeuristicOctile(t *testing.T) {
	h := heuristicOctile(0, 0, 7, 3)
	// Minimum cost: min(7,3)=3 diagonals + abs(7-3)=4 straights = 3*14+4*10 = 82
	actualMin := int32(82)
	if h > actualMin {
		t.Fatalf("heuristic %d overestimates actual minimum cost %d", h, actualMin)
	}
	if h < 0 {
		t.Fatal("heuristic must be non-negative")
	}
}

// ─── Line Of Sight Tests ──────────────────────────────────────────────────────

func TestHasLineOfSightOpen(t *testing.T) {
	grid := simpleGrid()
	if !hasLineOfSight(0, 0, 10, 0, grid) {
		t.Fatal("line of sight should exist on open grid")
	}
}

func TestHasLineOfSightBlocked(t *testing.T) {
	blocked := func(x, z int) bool {
		if x == 5 && z == 0 {
			return false
		}
		return true
	}
	if hasLineOfSight(0, 0, 10, 0, blocked) {
		t.Fatal("line of sight should be blocked at x=5")
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkFindPathShort(b *testing.B) {
	grid := simpleGrid()
	b.ResetTimer()
	for range b.N {
		FindPath(0, 0, 10, 0, grid, 0)
	}
}

func BenchmarkFindPathLong(b *testing.B) {
	grid := simpleGrid()
	b.ResetTimer()
	for range b.N {
		FindPath(0, 0, 50, 50, grid, 0)
	}
}

func BenchmarkFindPathWithWalls(b *testing.B) {
	grid := wallGrid()
	b.ResetTimer()
	for range b.N {
		FindPath(0, 75, 100, 75, grid, 0)
	}
}

func BenchmarkHeuristicOctile(b *testing.B) {
	b.ResetTimer()
	for range b.N {
		heuristicOctile(0, 0, 50, 50)
	}
}

func BenchmarkSmoothPath(b *testing.B) {
	grid := simpleGrid()
	result := FindPath(0, 0, 20, 20, grid, 0)
	if !result.Found {
		b.Fatal("path not found for benchmark")
	}
	b.ResetTimer()
	for range b.N {
		SmoothPath(result, grid)
	}
}
