package systems

import (
	"fmt"
	"math"
	"server/ecs"
	"sync"
)

// chunkSize defines how many world units fit in one spatial partition cell.
// With a 100×100 map and chunkSize=20, we get a 5×5 grid of 25 chunks.
// Tune this value: smaller = more chunks = faster proximity but more memory;
// larger = fewer chunks = less memory but more entities per bucket.
const chunkSize = 20

// ChunkKey identifies a single spatial partition cell by its grid coordinates.
// Derived from world position: ChunkKey{X: pos.X / chunkSize, Z: pos.Z / chunkSize}
type ChunkKey struct {
	MapID int
	X     int
	Z     int
}

// chunkEntry holds an entity and its precise world position inside the chunk.
// Storing position here avoids a second ECS lookup during proximity filtering.
type chunkEntry struct {
	ID  ecs.Entity
	Pos ecs.PositionComponent
}

// SpatialGrid is the authoritative spatial partition registry.
// It maintains a map of ChunkKey → set of entities in that chunk,
// and a reverse map of Entity → current ChunkKey for O(1) move/remove.
//
// Two separate mutexes:
//   - chunkMu: guards chunks (written on every move).
//   - indexMu: guards entityIndex (written on every move/remove).
//
// Splitting the locks reduces contention: reads on different chunks
// don't block each other, and index lookups don't block chunk scans.
type SpatialGrid struct {
	chunkMu sync.RWMutex
	chunks  map[ChunkKey]map[ecs.Entity]chunkEntry

	indexMu     sync.RWMutex
	entityIndex map[ecs.Entity]ChunkKey // reverse lookup: entity → its current chunk
}

// GlobalSpatialGrid is the singleton spatial registry used by all systems.
var GlobalSpatialGrid = newSpatialGrid()

func newSpatialGrid() *SpatialGrid {
	return &SpatialGrid{
		chunks:      make(map[ChunkKey]map[ecs.Entity]chunkEntry),
		entityIndex: make(map[ecs.Entity]ChunkKey),
	}
}

// worldToChunk converts a world-space position into its ChunkKey.
func worldToChunk(pos ecs.PositionComponent) ChunkKey {
	return ChunkKey{
		MapID: pos.MapID,
		X:     int(math.Floor(float64(pos.X) / chunkSize)),
		Z:     int(math.Floor(float64(pos.Z) / chunkSize)),
	}
}

// --- Write operations ---

// UpdateEntityPosition is the single write path for spatial data.
// Called by MovementSystem after a confirmed ECS position update.
//
// It handles three cases transparently:
//  1. New entity (not yet in grid): insert into correct chunk.
//  2. Same chunk (didn't cross a boundary): update position in-place.
//  3. New chunk (crossed boundary): remove from old chunk, insert into new.
func (g *SpatialGrid) UpdateEntityPosition(id ecs.Entity, pos ecs.PositionComponent) {
	newChunk := worldToChunk(pos)
	entry := chunkEntry{ID: id, Pos: pos}

	// Read current chunk under read lock first (fast path: same-chunk update).
	g.indexMu.RLock()
	oldChunk, existed := g.entityIndex[id]
	g.indexMu.RUnlock()

	if existed && oldChunk == newChunk {
		// Same chunk: only update the position entry, no chunk reassignment needed.
		g.chunkMu.Lock()
		if g.chunks[newChunk] != nil {
			g.chunks[newChunk][id] = entry
		}
		g.chunkMu.Unlock()
		return
	}

	// Different chunk or new entity: full update under write lock.
	g.chunkMu.Lock()
	if existed {
		// Remove from old chunk bucket.
		delete(g.chunks[oldChunk], id)
		if len(g.chunks[oldChunk]) == 0 {
			delete(g.chunks, oldChunk) // reclaim empty bucket
		}
	}
	// Insert into new chunk bucket.
	if g.chunks[newChunk] == nil {
		g.chunks[newChunk] = make(map[ecs.Entity]chunkEntry)
	}
	g.chunks[newChunk][id] = entry
	g.chunkMu.Unlock()

	// Update reverse index.
	g.indexMu.Lock()
	g.entityIndex[id] = newChunk
	g.indexMu.Unlock()
}

// RemoveEntity removes an entity from the spatial grid entirely.
// Must be called by DeathSystem and disconnect cleanup before ECS RemoveEntity.
func (g *SpatialGrid) RemoveEntity(id ecs.Entity) {
	g.indexMu.RLock()
	chunk, exists := g.entityIndex[id]
	g.indexMu.RUnlock()

	if !exists {
		return
	}

	g.chunkMu.Lock()
	delete(g.chunks[chunk], id)
	if len(g.chunks[chunk]) == 0 {
		delete(g.chunks, chunk)
	}
	g.chunkMu.Unlock()

	g.indexMu.Lock()
	delete(g.entityIndex, id)
	g.indexMu.Unlock()
}

// --- Query operations ---

// QueryRadius returns all entities within worldRadius world units of origin.
// It scans only the chunks that the radius circle can overlap — typically
// 4–9 chunks for a radius smaller than 2×chunkSize.
//
// The result slice is allocated once with a capacity hint to avoid
// repeated growing during append.
//
// Parameters:
//   - origin:      world position of the querying entity.
//   - worldRadius: search radius in world units.
//   - excludeID:   entity to exclude from results (usually the querier itself).
//
// Returns a slice of entities within the radius, sorted by nothing (unordered).
func (g *SpatialGrid) QueryRadius(
	origin ecs.PositionComponent,
	worldRadius float64,
	excludeID ecs.Entity,
) []chunkEntry {
	// Determine which chunk range to scan.
	chunkRadius := int(math.Ceil(worldRadius / chunkSize))
	originChunk := worldToChunk(origin)

	radiusSq := worldRadius * worldRadius
	results := make([]chunkEntry, 0, 16) // pre-alloc; 16 is a reasonable nearby-entity count

	g.chunkMu.RLock()
	defer g.chunkMu.RUnlock()

	for dz := -chunkRadius; dz <= chunkRadius; dz++ {
		for dx := -chunkRadius; dx <= chunkRadius; dx++ {
			key := ChunkKey{MapID: originChunk.MapID, X: originChunk.X + dx, Z: originChunk.Z + dz}
			bucket, ok := g.chunks[key]
			if !ok {
				continue
			}
			for _, entry := range bucket {
				if entry.ID == excludeID {
					continue
				}
				// Precise Euclidean check within the candidate chunks.
				ddx := float64(entry.Pos.X - origin.X)
				ddz := float64(entry.Pos.Z - origin.Z)
				if ddx*ddx+ddz*ddz <= radiusSq {
					results = append(results, entry)
				}
			}
		}
	}

	return results
}

// QueryChunk returns all entities in the exact chunk containing pos.
// Cheaper than QueryRadius when you only need same-cell occupants
// (e.g. standing on the same tile triggers a pickup).
func (g *SpatialGrid) QueryChunk(pos ecs.PositionComponent, excludeID ecs.Entity) []chunkEntry {
	key := worldToChunk(pos)
	results := make([]chunkEntry, 0, 8)

	g.chunkMu.RLock()
	defer g.chunkMu.RUnlock()

	for _, entry := range g.chunks[key] {
		if entry.ID != excludeID {
			results = append(results, entry)
		}
	}
	return results
}

// GetEntityChunk returns the current ChunkKey for an entity.
// Used by systems that need to know where an entity is without a full position lookup.
func (g *SpatialGrid) GetEntityChunk(id ecs.Entity) (ChunkKey, bool) {
	g.indexMu.RLock()
	defer g.indexMu.RUnlock()
	key, ok := g.entityIndex[id]
	return key, ok
}

// DebugStats returns a snapshot of grid occupancy for logging/monitoring.
func (g *SpatialGrid) DebugStats() string {
	g.chunkMu.RLock()
	defer g.chunkMu.RUnlock()
	g.indexMu.RLock()
	defer g.indexMu.RUnlock()
	return fmt.Sprintf("SpatialGrid: %d active chunks, %d indexed entities",
		len(g.chunks), len(g.entityIndex))
}
