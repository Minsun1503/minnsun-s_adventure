package aoi

import (
	"server/ecs"
	"server/peakgo/perf"
	"server/peakgo/pool"
	"sync"
)

// EventType describes what happened to a neighbor within an entity's AOI.
type EventType uint8

const (
	EventNone  EventType = 0
	EventEnter EventType = 1
	EventLeave EventType = 2
)

// AOIEvent represents a single enter/leave delta for one entity.
type AOIEvent struct {
	Type   EventType
	Target ecs.Entity
}

// MaxAOIWatchers is the hard limit on the number of entities returned per AOI update.
// When 500 players stand on the same tile, only the 50 closest are tracked.
// This prevents eventsPtr from containing 500+ events per player, keeping tick rate
// under 50ms even in worst-case stacking scenarios.
const MaxAOIWatchers = 50

// EntityListPool recycles *[]ecs.Entity slices for AOI spatial queries.
// Used by aoiSpatialQuery in world/aoi_bridge.go to avoid per-frame slice allocation.
var EntityListPool = pool.NewSlicePool[ecs.Entity](32)

// AOIEventPool recycles *[]AOIEvent slices for enter/leave delta results.
// Callers of UpdateOne must Put the returned pointer back after processing.
var AOIEventPool = pool.NewSlicePool[AOIEvent](32)

// neighborSet is a small set of entity IDs for fast lookup.
type neighborSet map[ecs.Entity]struct{}

// Watcher tracks the current neighbor set for one entity.
type Watcher struct {
	owner    ecs.Entity
	radius   float64
	neighbor neighborSet
}

// SpatialQueryFunc queries the spatial grid for entities within radius of origin.
type SpatialQueryFunc func(origin ecs.PositionComponent, worldRadius float64, excludeID ecs.Entity) *[]ecs.Entity

// AOIEventCallback is called for each enter/leave event detected during UpdateAll.
// Returning false stops further processing for the current watcher.
type AOIEventCallback func(watcher ecs.Entity, event AOIEvent) bool

// AOIManager manages AOI watchers and computes enter/leave deltas.
type AOIManager struct {
	mu       sync.RWMutex
	watchers map[ecs.Entity]*Watcher
}

// NewAOIManager creates a new AOI manager.
func NewAOIManager() *AOIManager {
	return &AOIManager{
		watchers: make(map[ecs.Entity]*Watcher),
	}
}

// RegisterWatcher adds an entity to be tracked for AOI changes.
func (m *AOIManager) RegisterWatcher(entity ecs.Entity, radius float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers[entity] = &Watcher{
		owner:    entity,
		radius:   radius,
		neighbor: make(neighborSet),
	}
}

// UnregisterWatcher removes an entity from AOI tracking.
func (m *AOIManager) UnregisterWatcher(entity ecs.Entity) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.watchers, entity)
}

// UpdateAll computes enter/leave events for all registered watchers and calls onEvent for each.
// This callback-based approach eliminates all per-frame map and slice allocations (0 B/op, 0 allocs/op).
// posGetter provides the position for each entity (decoupled from ecs.DefaultRegistry for testability).
// Return false from onEvent to stop processing events for the current watcher early.
func (m *AOIManager) UpdateAll(
	posGetter func(ecs.Entity) (ecs.PositionComponent, bool),
	query SpatialQueryFunc,
	onEvent AOIEventCallback,
) {
	m.mu.RLock()
	// Tạm thời clone ra danh sách keys để nhả lock sớm nhất có thể
	// tránh block các goroutine network đang cố gắng Unregister khi player disconnect
	// Dùng pool.SlicePool để tái sử dụng slice thay vì make mới mỗi tick
	pooled := EntityListPool.Get()
	watchersToUpdate := *pooled
	if cap(watchersToUpdate) < len(m.watchers) {
		EntityListPool.Put(pooled) // return old slice before growing
		grown := make([]ecs.Entity, 0, len(m.watchers))
		pooled = &grown // track new slice for deferred Put
		watchersToUpdate = grown
	}
	watchersToUpdate = watchersToUpdate[:0]
	for id := range m.watchers {
		watchersToUpdate = append(watchersToUpdate, id)
	}
	m.mu.RUnlock()

	// Release the pooled slice back after processing all watchers.
	// This must happen AFTER the for loop to avoid aliasing with query results
	// that also use EntityListPool inside updateOne.
	defer EntityListPool.Put(pooled)

	for _, id := range watchersToUpdate {
		m.mu.RLock()
		w, exists := m.watchers[id]
		m.mu.RUnlock()
		if !exists {
			continue
		}

		pos, ok := posGetter(id)
		if !ok {
			continue
		}
		m.updateOne(id, w, pos, query, onEvent)
	}
}

// updateOne computes enter/leave events for a single watcher given its position.
// Events are dispatched directly via onEvent callback, eliminating allocation.
//
// Note: worst-case AOI culling (sort by distance, keep closest MaxAOIWatchers)
// is handled upstream in the bridge layer (world/aoi_bridge.go's aoiSpatialQuery
// and aoiSpatialQueryFromGrid) where entity positions are available from the
// spatial grid's ChunkEntry results. By the time entities reach this function,
// the count is already ≤ MaxAOIWatchers.
func (m *AOIManager) updateOne(entity ecs.Entity, w *Watcher, pos ecs.PositionComponent, query SpatialQueryFunc, onEvent AOIEventCallback) {
	raw := query(pos, w.radius, entity)
	perf.GlobalPacketMonitor.RecordAoiQuery()
	if raw == nil {
		return
	}
	defer EntityListPool.Put(raw)
	entries := *raw

	// Detect leaves: in old set but not in new set (linear search on entries slice)
	for old := range w.neighbor {
		if !sliceContainsEntity(entries, old) {
			if !onEvent(entity, AOIEvent{Type: EventLeave, Target: old}) {
				return
			}
			delete(w.neighbor, old)
		}
	}

	// Detect enters: in new set but not in old set (map lookup on w.neighbor)
	for _, newID := range entries {
		if _, exists := w.neighbor[newID]; !exists {
			if !onEvent(entity, AOIEvent{Type: EventEnter, Target: newID}) {
				return
			}
			w.neighbor[newID] = struct{}{}
		}
	}
}

// sliceContainsEntity performs a linear search for target in the slice.
// Used instead of allocation-heavy map to check existence in small neighbor lists (5-30 entities).
func sliceContainsEntity(slice []ecs.Entity, target ecs.Entity) bool {
	for _, e := range slice {
		if e == target {
			return true
		}
	}
	return false
}

// UpdateOne computes enter/leave events for a single entity given its position.
// Returns a pointer to a pooled []AOIEvent slice, or nil if no changes.
// The caller MUST Put the returned pointer back into AOIEventPool after use.
//
// Note: worst-case AOI culling (sort by distance, keep closest MaxAOIWatchers)
// is handled upstream in the bridge layer where entity positions are available.
//
// Deprecated: Prefer UpdateAll with callback for zero-allocation hot-path.
func (m *AOIManager) UpdateOne(entity ecs.Entity, pos ecs.PositionComponent, query SpatialQueryFunc) *[]AOIEvent {
	m.mu.RLock()
	w, ok := m.watchers[entity]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	raw := query(pos, w.radius, entity)
	if raw == nil {
		return nil
	}
	defer EntityListPool.Put(raw)
	entries := *raw

	var events *[]AOIEvent

	// Detect leaves: in old set but not in new set
	for old := range w.neighbor {
		if !sliceContainsEntity(entries, old) {
			if events == nil {
				events = AOIEventPool.Get()
				*events = (*events)[:0]
			}
			*events = append(*events, AOIEvent{Type: EventLeave, Target: old})
			delete(w.neighbor, old)
		}
	}

	// Detect enters: in new set but not in old set
	for _, newID := range entries {
		if _, exists := w.neighbor[newID]; !exists {
			if events == nil {
				events = AOIEventPool.Get()
				*events = (*events)[:0]
			}
			*events = append(*events, AOIEvent{Type: EventEnter, Target: newID})
			w.neighbor[newID] = struct{}{}
		}
	}

	return events
}

// WatcherCount returns the number of registered watchers.
func (m *AOIManager) WatcherCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.watchers)
}
