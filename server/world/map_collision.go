package world

import (
	"server/peakgo/aabb"
	"server/peakgo/astar"
	"server/peakgo/nav"
)

// Max map scale dimensions matching grid constraints
const MapWidth = 101
const MapHeight = 101

// ─── Tile-Based Collision (Legacy Grid) ───────────────────────────────────────

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

	// Blocked tile near (6,6) spawn zone for movement test collision checks
	townGrid[7][6] = true

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

// ─── AABB Entity Collision (peakgo/aabb) ─────────────────────────────────────

// EntityHitbox defines an entity's axis-aligned bounding box for collision detection.
// Uses peakgo/aabb.Box for tile-level queries.
type EntityHitbox struct {
	EntityID uint64
	Box      aabb.Box
}

// EntityCollider maintains entity-level AABB collision state for a map.
// Used for skill AoE target validation, entity overlap checks, and
// movement collision against other entities (not tiles).
type EntityCollider struct {
	hitboxes map[uint64]aabb.Box
}

// NewEntityCollider creates an entity-level collider for a map.
func NewEntityCollider() *EntityCollider {
	return &EntityCollider{
		hitboxes: make(map[uint64]aabb.Box),
	}
}

// SetEntityHitbox registers or updates an entity's AABB hitbox.
func (ec *EntityCollider) SetEntityHitbox(entityID uint64, centerX, centerZ int, halfWidth, halfDepth int) {
	ec.hitboxes[entityID] = aabb.Box{
		MinX: centerX - halfWidth,
		MaxX: centerX + halfWidth,
		MinZ: centerZ - halfDepth,
		MaxZ: centerZ + halfDepth,
	}
}

// RemoveEntityHitbox removes an entity's hitbox.
func (ec *EntityCollider) RemoveEntityHitbox(entityID uint64) {
	delete(ec.hitboxes, entityID)
}

// CheckEntityCollision checks if entity a collides with entity b.
func (ec *EntityCollider) CheckEntityCollision(aID, bID uint64) bool {
	aBox, okA := ec.hitboxes[aID]
	bBox, okB := ec.hitboxes[bID]
	if !okA || !okB {
		return false
	}
	return aBox.Overlaps(bBox)
}

// QueryEntitiesAtPoint returns all entity IDs whose AABB contains the given point.
func (ec *EntityCollider) QueryEntitiesAtPoint(x, z int) []uint64 {
	var result []uint64
	for id, box := range ec.hitboxes {
		if x >= box.MinX && x <= box.MaxX && z >= box.MinZ && z <= box.MaxZ {
			result = append(result, id)
		}
	}
	return result
}

// EntityCount returns the number of registered entities.
func (ec *EntityCollider) EntityCount() int {
	return len(ec.hitboxes)
}

// IsWalkableForMap returns a walkability checker bound to a specific map ID.
// Uses the collision grid data to determine if a tile is passable.
func IsWalkableForMap(mapID int) astar.IsWalkable {
	return func(x, z int) bool {
		return !IsTileBlocked(mapID, x, z)
	}
}

// ─── NavMesh (peakgo/nav) ───────────────────────────────────────────────────

// GlobalNavMesh is the singleton navigation mesh used by all AI pathfinding.
// It wraps peakgo/nav.NavMesh with zone/portal awareness and multi-map support.
// Populated from collision grid data in InitNavMesh().
var GlobalNavMesh *nav.NavMesh

// InitNavMesh creates the global NavMesh and feeds collision data from
// ServerCollisionData into walkable zones. Each map gets a default ZoneNormal
// covering its entire bounds, so AI pathfinding automatically respects tile
// collisions and restricted zones.
func InitNavMesh() {
	GlobalNavMesh = nav.NewNavMesh()

	// Register a normal walkable zone for each collision map.
	// The zone covers the full map bounds. Inside each zone, individual
	// blocked tiles (walls, boulders) are handled by the walkability
	// closure passed to astar.FindPathWithCache (which delegates to
	// IsTileBlocked). Zones add semantic awareness: safe zones, dungeons,
	// and restricted areas can be defined as separate Zone entries.
	for mapID, grid := range ServerCollisionData {
		// Register a ZoneNormal covering the full map.
		GlobalNavMesh.RegisterZone(nav.Zone{
			ID:          mapID,
			MapID:       mapID,
			MinX:        0,
			MinZ:        0,
			MaxX:        int32(MapWidth - 1),
			MaxZ:        int32(MapHeight - 1),
			Type:        nav.ZoneNormal,
			LinkedZones: nil,
		})

		// Log zone registration for diagnostics
		_ = grid // grid reference kept for future per-zone customization
	}

	// Portal registration can be extended here as more maps are added.
	// Example:
	//   GlobalNavMesh.RegisterPortal(nav.Portal{
	//       ID:       1,
	//       SrcMapID: 1,
	//       DstMapID: 2,
	//       SrcX:     99, SrcZ: 50,
	//       DstX:    0, DstZ: 50,
	//   })
}

// Global entity colliders per map (lazy initialized on use).
var globalEntityColliders = make(map[int]*EntityCollider)

// GetEntityCollider returns (or creates) the EntityCollider for a map.
func GetEntityCollider(mapID int) *EntityCollider {
	if ec, ok := globalEntityColliders[mapID]; ok {
		return ec
	}
	ec := NewEntityCollider()
	globalEntityColliders[mapID] = ec
	return ec
}
