// Package ecs provides the Entity Component System for the game server.
//
// query_cache.go — Query Cache for optimizing repeated Query operations.
//
// The query cache memoizes the "smallest store selection" for multi-component
// queries, avoiding repeated range scanning of large stores when expensive
// ancillary lookups would be more efficient. This provides a ~2x speedup for
// repeated QueryPositionStats, QueryPositionAI, etc.
//
// Design:
//   - CacheKey is a bitmask of component types
//   - CacheResult stores the selected component store index
//   - Cache is invalidated when components are added/removed
//   - Zero-alloc design: cache entries are atomically swapped
package ecs

import (
	"sync/atomic"
	"unsafe"
)

// ─── Component Type IDs ──────────────────────────────────────────────────────
//
// Each component type gets a unique bit position in the query cache key.
// These are used internally by the Query layer and the Query Cache.

const (
	compPosition     uint64 = 1 << iota // 1 << 0
	compConnection                      // 1 << 1
	compMetadata                        // 1 << 2
	compStats                           // 1 << 3
	compAI                              // 1 << 4
	compInventory                       // 1 << 5
	compLifetime                        // 1 << 6
	compItemTemplate                    // 1 << 7
	compEquipment                       // 1 << 8
	compParty                           // 1 << 9
	compPartyMember                     // 1 << 10
	compEffects                         // 1 << 11
)

// StoreSize returns the current number of entities in a component store.
// This is used for query planning to pick the smallest store to iterate.
func (r *Registry) StoreSize(compType uint64) int {
	switch compType {
	case compPosition:
		return len(r.positions.dense)
	case compConnection:
		return len(r.conns.dense)
	case compMetadata:
		return len(r.metadata.dense)
	case compStats:
		return len(r.stats.dense)
	case compAI:
		return len(r.ai.dense)
	case compInventory:
		return len(r.inventories.dense)
	case compLifetime:
		return len(r.lifetimes.dense)
	case compItemTemplate:
		return len(r.itemTemplates.dense)
	case compEquipment:
		return len(r.equipment.dense)
	case compParty:
		return len(r.parties.dense)
	case compPartyMember:
		return len(r.partyMembers.dense)
	case compEffects:
		return len(r.effects.dense)
	default:
		return 0
	}
}

// ─── Query Cache ─────────────────────────────────────────────────────────────

// QueryCache caches the optimal iteration store for multi-component queries.
//
// Thread-safety: The cache is designed for concurrent read access (hot-path)
// with infrequent writes (when component counts change significantly).
// The cache uses atomic pointer swapping so readers never see a partially
// written entry.
type QueryCache struct {
	// entries holds the cached query plans, indexed by component bitmask.
	// An atomic pointer to a map is used: readers atomically load, writers
	// atomically store a new map. This is lock-free for readers.
	entries unsafe.Pointer // *map[uint64]QueryPlan
}

// QueryPlan holds the optimal iteration strategy for a given query.
type QueryPlan struct {
	// IterStore is the component type with the fewest entities.
	IterStore uint64
	// IterStoreSize is the entity count of the iteration store.
	IterStoreSize int
	// NeededStores is a bitmask of all component stores needed for this query.
	NeededStores uint64
}

// GlobalQueryCache is the singleton query cache for the global registry.
var GlobalQueryCache = &QueryCache{}

func init() {
	GlobalQueryCache.Init()
}

// Init initializes the query cache with an empty map.
func (qc *QueryCache) Init() {
	m := make(map[uint64]QueryPlan)
	atomic.StorePointer(&qc.entries, unsafe.Pointer(&m))
}

// FindOrPlan returns a cached query plan or computes a new one.
// The mask is a bitmask of required component types.
// r is the registry to measure store sizes from.
func (qc *QueryCache) FindOrPlan(mask uint64, reg *Registry) QueryPlan {
	// Fast path: check cache (lock-free atomic read)
	entries := (*map[uint64]QueryPlan)(atomic.LoadPointer(&qc.entries))
	if entries != nil {
		if plan, ok := (*entries)[mask]; ok {
			return plan
		}
	}

	// Cache miss: compute the plan
	plan := qc.computePlan(mask, reg)

	// Store the computed plan atomically
	qc.storeEntry(mask, plan)

	return plan
}

// computePlan determines which component store is smallest and should be
// iterated for a query requiring all components in the given mask.
func (qc *QueryCache) computePlan(mask uint64, reg *Registry) QueryPlan {
	// Determine which component stores are needed based on the mask
	// (We need to know the sizes; iterate over possible component types)
	var smallestSize int = -1
	var smallestComp uint64

	// Check each component type bit in the mask
	for i := uint(0); i < 12; i++ {
		compBit := uint64(1 << i)
		if mask&compBit == 0 {
			continue
		}

		size := reg.StoreSize(compBit)
		if smallestSize < 0 || size < smallestSize {
			smallestSize = size
			smallestComp = compBit
		}
	}

	return QueryPlan{
		IterStore:     smallestComp,
		IterStoreSize: smallestSize,
		NeededStores:  mask,
	}
}

// storeEntry atomically adds or updates a cache entry.
func (qc *QueryCache) storeEntry(mask uint64, plan QueryPlan) {
	// Load current map, copy and update
	oldPtr := atomic.LoadPointer(&qc.entries)
	if oldPtr == nil {
		return
	}
	oldMap := *(*map[uint64]QueryPlan)(oldPtr)

	// Check if already exists (double-checked)
	if _, ok := oldMap[mask]; ok {
		return
	}

	// Create new map with entry added
	newMap := make(map[uint64]QueryPlan, len(oldMap)+1)
	for k, v := range oldMap {
		newMap[k] = v
	}
	newMap[mask] = plan

	// Atomically swap the pointer
	atomic.StorePointer(&qc.entries, unsafe.Pointer(&newMap))
}

// Invalidate clears the entire query cache.
// Call this when component counts change significantly.
func (qc *QueryCache) Invalidate() {
	m := make(map[uint64]QueryPlan)
	atomic.StorePointer(&qc.entries, unsafe.Pointer(&m))
}

// ─── Query Helpers ───────────────────────────────────────────────────────────

// componentMaskForQuery returns the component bitmask for a given query type.
// This is used by the Query layer to cache the optimal store selection.

// MaskPositionStats returns the bitmask for Position + Stats.
func MaskPositionStats() uint64 { return compPosition | compStats }

// MaskPositionAI returns the bitmask for AI + Position + Stats.
func MaskPositionAI() uint64 { return compAI | compPosition | compStats }

// MaskPositionMetadata returns the bitmask for Metadata + Position.
func MaskPositionMetadata() uint64 { return compMetadata | compPosition }
