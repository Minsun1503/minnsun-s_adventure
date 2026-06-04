package world

import (
	"fmt"
	"math"
	"server/ecs"
	"server/peakgo/pool"
	"sync"
)

// chunkSize defines how many world units fit in one spatial partition cell.
// With a 100×100 map and chunkSize=20, we get a 5×5 grid of 25 chunks.
const chunkSize = 20

// ChunkKey identifies a single spatial partition cell by its grid coordinates.
type ChunkKey struct {
	MapID int
	X     int
	Z     int
}

// ChunkEntry holds an entity and its precise world position inside the chunk.
type ChunkEntry struct {
	ID  ecs.Entity
	Pos ecs.PositionComponent
}

// Memory pool for chunk bucket slices to completely avoid slice allocation churn during movement.
var chunkSlicePool = pool.NewSlicePool[ChunkEntry](8)

// Memory pool for QueryRadius outputs to avoid heap allocations in the game loop.
var queryResultPool = pool.NewSlicePool[ChunkEntry](256)

// FreeQueryCandidates recycles the slice returned by QueryRadius.
func FreeQueryCandidates(s *[]ChunkEntry) {
	if s == nil {
		return
	}
	queryResultPool.Put(s)
}

// SpatialGrid is the authoritative spatial partition registry.
// It maintains a map of ChunkKey → slice of entities in that chunk,
// and a reverse map of Entity → current ChunkKey for O(1) move/remove.
type SpatialGrid struct {
	chunkMu sync.RWMutex
	chunks  map[ChunkKey][]ChunkEntry

	indexMu     sync.RWMutex
	entityIndex map[ecs.Entity]ChunkKey // reverse lookup: entity → its current chunk
}

// GlobalSpatialGrid is the singleton spatial registry used by all systems.
var GlobalSpatialGrid = newSpatialGrid()

func newSpatialGrid() *SpatialGrid {
	return &SpatialGrid{
		chunks:      make(map[ChunkKey][]ChunkEntry),
		entityIndex: make(map[ecs.Entity]ChunkKey),
	}
}

// WorldToChunk converts a world-space position into its ChunkKey (exported).
func WorldToChunk(pos ecs.PositionComponent) ChunkKey {
	return ChunkKey{
		MapID: pos.MapID,
		X:     int(math.Floor(float64(pos.X) / chunkSize)),
		Z:     int(math.Floor(float64(pos.Z) / chunkSize)),
	}
}

// worldToChunk converts a world-space position into its ChunkKey (unexported).
func worldToChunk(pos ecs.PositionComponent) ChunkKey {
	return ChunkKey{
		MapID: pos.MapID,
		X:     int(math.Floor(float64(pos.X) / chunkSize)),
		Z:     int(math.Floor(float64(pos.Z) / chunkSize)),
	}
}

// --- Write operations ---

// UpdateEntityPosition is the single write path for spatial data.
// It handles same-chunk updates in-place, and different-chunk movements
// using swap-and-slice removals combined with pooled slice allocations.
//
// Lock ordering: indexMu.Lock() → chunkMu.Lock() → chunkMu.Unlock() → indexMu.Unlock().
// Both index and chunk buckets are updated atomically as a transaction.
func (g *SpatialGrid) UpdateEntityPosition(id ecs.Entity, pos ecs.PositionComponent) {
	newChunk := worldToChunk(pos)
	entry := ChunkEntry{ID: id, Pos: pos}

	g.indexMu.Lock()
	g.chunkMu.Lock()

	oldChunk, existed := g.entityIndex[id]

	if existed && oldChunk == newChunk {
		// Same chunk: only update the position entry in-place
		bucket := g.chunks[newChunk]
		for i, e := range bucket {
			if e.ID == id {
				bucket[i].Pos = pos
				break
			}
		}
		g.chunkMu.Unlock()
		g.indexMu.Unlock()
		return
	}

	if existed {
		// Remove from old chunk bucket
		oldBucket := g.chunks[oldChunk]
		for i, e := range oldBucket {
			if e.ID == id {
				lastIdx := len(oldBucket) - 1
				if i != lastIdx {
					oldBucket[i] = oldBucket[lastIdx]
				}
				oldBucket = oldBucket[:lastIdx]
				break
			}
		}
		if len(oldBucket) == 0 {
			delete(g.chunks, oldChunk)
			chunkSlicePool.Put(&oldBucket)
		} else {
			g.chunks[oldChunk] = oldBucket
		}
	}

	// Insert into new chunk bucket
	newBucket, ok := g.chunks[newChunk]
	if !ok {
		newBucket = *chunkSlicePool.Get()
	}
	newBucket = append(newBucket, entry)
	g.chunks[newChunk] = newBucket

	// Update reverse index atomically with chunk state.
	g.entityIndex[id] = newChunk

	g.chunkMu.Unlock()
	g.indexMu.Unlock()
}

// RemoveEntity removes an entity from the spatial grid entirely.
//
// Lock ordering: indexMu.Lock() → chunkMu.Lock() → chunkMu.Unlock() → indexMu.Unlock().
// Both index and chunk buckets are updated atomically as a transaction.
func (g *SpatialGrid) RemoveEntity(id ecs.Entity) {
	g.indexMu.Lock()
	chunk, exists := g.entityIndex[id]
	if !exists {
		g.indexMu.Unlock()
		return
	}

	g.chunkMu.Lock()
	bucket := g.chunks[chunk]
	for i, e := range bucket {
		if e.ID == id {
			lastIdx := len(bucket) - 1
			if i != lastIdx {
				bucket[i] = bucket[lastIdx]
			}
			bucket = bucket[:lastIdx]
			break
		}
	}
	if len(bucket) == 0 {
		delete(g.chunks, chunk)
		chunkSlicePool.Put(&bucket)
	} else {
		g.chunks[chunk] = bucket
	}
	delete(g.entityIndex, id)
	g.chunkMu.Unlock()
	g.indexMu.Unlock()
}

// --- Query operations ---

// QueryRadius returns all entities within worldRadius world units of origin.
// Uses slice pooling to avoid allocating result slices on the heap.
// Returns *[]ChunkEntry (pointer to pooled slice) to prevent slice header escape
// across function return boundaries. Callers must dereference and FreeQueryCandidates.
func (g *SpatialGrid) QueryRadius(
	origin ecs.PositionComponent,
	worldRadius float64,
	excludeID ecs.Entity,
) *[]ChunkEntry {
	chunkRadius := int(math.Ceil(worldRadius / chunkSize))
	originChunk := worldToChunk(origin)

	radiusSq := worldRadius * worldRadius

	results := queryResultPool.Get()

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
				ddx := float64(entry.Pos.X - origin.X)
				ddz := float64(entry.Pos.Z - origin.Z)
				if ddx*ddx+ddz*ddz <= radiusSq {
					*results = append(*results, entry)
				}
			}
		}
	}

	return results
}

// QueryChunk returns all entities in the exact chunk containing pos.
func (g *SpatialGrid) QueryChunk(pos ecs.PositionComponent, excludeID ecs.Entity) []ChunkEntry {
	key := worldToChunk(pos)
	results := make([]ChunkEntry, 0, 8)

	g.chunkMu.RLock()
	defer g.chunkMu.RUnlock()

	for _, entry := range g.chunks[key] {
		if entry.ID != excludeID {
			results = append(results, entry)
		}
	}
	return results
}

// QueryChunkByKey returns all entities in the given chunk key, excluding excludeID.
// Uses pooled slice allocation to avoid heap churn.
// Returns *[]ChunkEntry (pointer to pooled slice) to maintain consistent pointer-semantics
// with QueryRadius. Callers must dereference and FreeQueryCandidates.
func (g *SpatialGrid) QueryChunkByKey(key ChunkKey, excludeID ecs.Entity) *[]ChunkEntry {
	results := queryResultPool.Get()

	g.chunkMu.RLock()
	defer g.chunkMu.RUnlock()

	for _, entry := range g.chunks[key] {
		if entry.ID != excludeID {
			*results = append(*results, entry)
		}
	}
	return results
}

// GetEntityChunk returns the current ChunkKey for an entity.
func (g *SpatialGrid) GetEntityChunk(id ecs.Entity) (ChunkKey, bool) {
	g.indexMu.RLock()
	defer g.indexMu.RUnlock()
	key, ok := g.entityIndex[id]
	return key, ok
}

// ForEachEntityChunk calls f for every entity in the grid with its current chunk key.
// Returns false from f to stop early. Safe for concurrent use with read-lock.
func (g *SpatialGrid) ForEachEntityChunk(f func(id ecs.Entity, key ChunkKey) bool) {
	g.indexMu.RLock()
	defer g.indexMu.RUnlock()
	for id, key := range g.entityIndex {
		if !f(id, key) {
			return
		}
	}
}

// DebugStats returns a snapshot of grid occupancy for logging/monitoring.
func (g *SpatialGrid) DebugStats() string {
	g.chunkMu.RLock()
	chunkCount := len(g.chunks)
	g.chunkMu.RUnlock()

	g.indexMu.RLock()
	entityCount := len(g.entityIndex)
	g.indexMu.RUnlock()

	return fmt.Sprintf("SpatialGrid: %d active chunks, %d indexed entities",
		chunkCount, entityCount)
}
