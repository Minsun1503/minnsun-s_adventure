package world

import (
	"fmt"
	"math"
	"server/ecs"
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
var chunkSlicePool = sync.Pool{
	New: func() any {
		s := make([]ChunkEntry, 0, 8)
		return &s
	},
}

// Memory pool for QueryRadius outputs to avoid heap allocations in the game loop.
var queryResultPool = sync.Pool{
	New: func() any {
		s := make([]ChunkEntry, 0, 16)
		return &s
	},
}

// FreeQueryCandidates recycles the slice returned by QueryRadius.
func FreeQueryCandidates(s []ChunkEntry) {
	if s == nil {
		return
	}
	s = s[:0]
	queryResultPool.Put(&s)
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
// It handles same-chunk updates in-place, and different-chunk movements
// using swap-and-slice removals combined with pooled slice allocations.
func (g *SpatialGrid) UpdateEntityPosition(id ecs.Entity, pos ecs.PositionComponent) {
	newChunk := worldToChunk(pos)
	entry := ChunkEntry{ID: id, Pos: pos}

	// Read current chunk under read lock first (fast path: same-chunk update).
	g.indexMu.RLock()
	oldChunk, existed := g.entityIndex[id]
	g.indexMu.RUnlock()

	if existed && oldChunk == newChunk {
		// Same chunk: only update the position entry in-place
		g.chunkMu.Lock()
		bucket := g.chunks[newChunk]
		for i, e := range bucket {
			if e.ID == id {
				bucket[i].Pos = pos
				break
			}
		}
		g.chunkMu.Unlock()
		return
	}

	// Different chunk or new entity: full update.
	g.chunkMu.Lock()
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
			oldBucket = oldBucket[:0]
			chunkSlicePool.Put(&oldBucket)
		} else {
			g.chunks[oldChunk] = oldBucket
		}
	}

	// Insert into new chunk bucket
	newBucket, ok := g.chunks[newChunk]
	if !ok {
		pSlice := chunkSlicePool.Get().(*[]ChunkEntry)
		newBucket = *pSlice
		newBucket = newBucket[:0]
	}
	newBucket = append(newBucket, entry)
	g.chunks[newChunk] = newBucket
	g.chunkMu.Unlock()

	// Update reverse index.
	g.indexMu.Lock()
	g.entityIndex[id] = newChunk
	g.indexMu.Unlock()
}

// RemoveEntity removes an entity from the spatial grid entirely.
func (g *SpatialGrid) RemoveEntity(id ecs.Entity) {
	g.indexMu.RLock()
	chunk, exists := g.entityIndex[id]
	g.indexMu.RUnlock()

	if !exists {
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
		bucket = bucket[:0]
		chunkSlicePool.Put(&bucket)
	} else {
		g.chunks[chunk] = bucket
	}
	g.chunkMu.Unlock()

	g.indexMu.Lock()
	delete(g.entityIndex, id)
	g.indexMu.Unlock()
}

// --- Query operations ---

// QueryRadius returns all entities within worldRadius world units of origin.
// Uses slice pooling to avoid allocating result slices on the heap.
func (g *SpatialGrid) QueryRadius(
	origin ecs.PositionComponent,
	worldRadius float64,
	excludeID ecs.Entity,
) []ChunkEntry {
	chunkRadius := int(math.Ceil(worldRadius / chunkSize))
	originChunk := worldToChunk(origin)

	radiusSq := worldRadius * worldRadius

	pSlice := queryResultPool.Get().(*[]ChunkEntry)
	results := *pSlice
	results = results[:0]

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
					results = append(results, entry)
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

// GetEntityChunk returns the current ChunkKey for an entity.
func (g *SpatialGrid) GetEntityChunk(id ecs.Entity) (ChunkKey, bool) {
	g.indexMu.RLock()
	defer g.indexMu.RUnlock()
	key, ok := g.entityIndex[id]
	return key, ok
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
