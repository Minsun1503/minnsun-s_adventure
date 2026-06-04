package aoi

import (
	"server/ecs"
	"server/peakgo/pool"
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

// AOIManager manages AOI watchers and computes enter/leave deltas.
type AOIManager struct {
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
	m.watchers[entity] = &Watcher{
		owner:    entity,
		radius:   radius,
		neighbor: make(neighborSet),
	}
}

// UnregisterWatcher removes an entity from AOI tracking.
func (m *AOIManager) UnregisterWatcher(entity ecs.Entity) {
	delete(m.watchers, entity)
}

// UpdateAll computes enter/leave events for all registered watchers.
// posGetter provides the position for each entity (decoupled from ecs.GlobalRegistry for testability).
// Each watcher's events are deep-copied from the pool before the pool slice is returned,
// so the caller owns the returned map and its slices fully.
func (m *AOIManager) UpdateAll(posGetter func(ecs.Entity) (ecs.PositionComponent, bool), query SpatialQueryFunc) map[ecs.Entity][]AOIEvent {
	results := make(map[ecs.Entity][]AOIEvent, len(m.watchers))
	for id, w := range m.watchers {
		pos, ok := posGetter(id)
		if !ok {
			continue
		}
		eventsPtr := m.updateOne(id, w, pos, query)
		if eventsPtr != nil && len(*eventsPtr) > 0 {
			// Deep copy: the caller owns these slices independently of the pool.
			events := make([]AOIEvent, len(*eventsPtr))
			copy(events, *eventsPtr)
			results[id] = events
		}
		if eventsPtr != nil {
			AOIEventPool.Put(eventsPtr)
		}
	}
	return results
}

// updateOne computes enter/leave events for a single watcher given its position.
// Returns a pointer to a pooled []AOIEvent slice, or nil if no changes.
// The caller MUST Put the returned pointer back into AOIEventPool after use.
func (m *AOIManager) updateOne(entity ecs.Entity, w *Watcher, pos ecs.PositionComponent, query SpatialQueryFunc) *[]AOIEvent {
	raw := query(pos, w.radius, entity)
	if raw == nil {
		return nil
	}
	defer EntityListPool.Put(raw)
	entries := *raw

	// Build current neighbor set
	current := make(neighborSet, len(entries))
	for _, e := range entries {
		current[e] = struct{}{}
	}

	// Detect leaves: in old set but not in new set
	var events *[]AOIEvent
	for old := range w.neighbor {
		if _, exists := current[old]; !exists {
			if events == nil {
				events = AOIEventPool.Get()
				*events = (*events)[:0]
			}
			*events = append(*events, AOIEvent{Type: EventLeave, Target: old})
		}
	}

	// Detect enters: in new set but not in old set
	for newID := range current {
		if _, exists := w.neighbor[newID]; !exists {
			if events == nil {
				events = AOIEventPool.Get()
				*events = (*events)[:0]
			}
			*events = append(*events, AOIEvent{Type: EventEnter, Target: newID})
		}
	}

	// Swap the neighbor set (reuse old map by clearing)
	for k := range w.neighbor {
		delete(w.neighbor, k)
	}
	for k := range current {
		w.neighbor[k] = struct{}{}
	}

	return events
}

// UpdateOne computes enter/leave events for a single entity given its position.
// Returns a pointer to a pooled []AOIEvent slice, or nil if no changes.
// The caller MUST Put the returned pointer back into AOIEventPool after use.
func (m *AOIManager) UpdateOne(entity ecs.Entity, pos ecs.PositionComponent, query SpatialQueryFunc) *[]AOIEvent {
	w, ok := m.watchers[entity]
	if !ok {
		return nil
	}
	return m.updateOne(entity, w, pos, query)
}

// WatcherCount returns the number of registered watchers.
func (m *AOIManager) WatcherCount() int {
	return len(m.watchers)
}
