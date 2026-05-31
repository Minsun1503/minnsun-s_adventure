package world

// Max map scale dimensions matching grid constraints
const MapWidth = 101
const MapHeight = 101

// MapCollisionGrid represents the passability matrix for a specific map layer.
type MapCollisionGrid [MapWidth][MapHeight]bool

// Global cache holding the physical collision maps for the entire server
var ServerCollisionData = make(map[int]*MapCollisionGrid)

// InitializeCollisionMaps hooks up static barriers for testing.
// In production, this data grid will be parsed out of your external JSON/binary level files.
func InitializeCollisionMaps() {
	// Allocate a fresh empty grid for Map #1 (Starting Town)
	townGrid := &MapCollisionGrid{}

	// Place a solid horizontal brick wall line at Z coordinate row 15
	// stretching from X: 10 to X: 20
	for x := 10; x <= 20; x++ {
		townGrid[x][15] = true // Mark these coordinate indexes as blocked
	}

	// Place an isolated boulder obstacle object right in the town square
	townGrid[50][50] = true

	// Register the populated matrix into our global memory stack
	ServerCollisionData[1] = townGrid
}

// IsTileBlocked verifies if a target position contains a solid spatial barrier
func IsTileBlocked(mapID int, targetX, targetZ int) bool {
	grid, exists := ServerCollisionData[mapID]
	if !exists {
		return false // If a map has no registered grid matrix, default to open passability
	}

	// Double-check array boundary bounds to prevent index panic faults
	if targetX < 0 || targetX >= MapWidth || targetZ < 0 || targetZ >= MapHeight {
		return true // Treat out-of-bounds cells as completely blocked
	}

	return grid[targetX][targetZ]
}
