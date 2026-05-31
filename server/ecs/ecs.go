package ecs

import (
	"net"
	"server/state"
	"sync"
	"sync/atomic"
)

// Entity is a uint64 for O(1) integer map lookup instead of string hashing.
type Entity uint64

// Components stored as inline values (not pointers) to avoid extra heap allocs.
// Exception: ConnectionComponent keeps net.Conn as an interface (already a pointer).

type PositionComponent struct {
	X int
	Z int
}

type ConnectionComponent struct {
	Conn net.Conn
}

type MetadataComponent struct {
	Name string
	Type string
}

type StatsComponent struct {
	HP  int
	Dam int
}

// Registry uses sync.Map per component type.
// The separate "entities" SafeMap is eliminated — presence is implicit via component maps.
// ID generation uses an atomic counter, making RegisterEntity lock-free.
type Registry struct {
	nextID    atomic.Uint64
	positions state.TypedSyncMap[Entity, PositionComponent] // inline value, no pointer
	conns     state.TypedSyncMap[Entity, ConnectionComponent]
	metadata  state.TypedSyncMap[Entity, MetadataComponent] // inline value, no pointer
	stats     state.TypedSyncMap[Entity, StatsComponent]    // inline value, no pointer
}

var GlobalRegistry = &Registry{}

// NewEntity generates a new unique Entity ID atomically — no lock needed.
func (r *Registry) NewEntity() Entity {
	return Entity(r.nextID.Add(1))
}

// RemoveEntity deletes all components in parallel using a WaitGroup.
// Previous: 5 sequential lock acquisitions.
// Now: 4 concurrent sync.Map deletes.
func (r *Registry) RemoveEntity(id Entity) {
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { r.positions.Delete(id); wg.Done() }()
	go func() { r.conns.Delete(id); wg.Done() }()
	go func() { r.metadata.Delete(id); wg.Done() }()
	go func() { r.stats.Delete(id); wg.Done() }()
	wg.Wait()
}

func (r *Registry) SetPosition(id Entity, comp PositionComponent) { // no pointer param
	r.positions.Set(id, comp)
}

func (r *Registry) GetPosition(id Entity) (PositionComponent, bool) {
	return r.positions.Get(id)
}

func (r *Registry) SetConnection(id Entity, comp ConnectionComponent) {
	r.conns.Set(id, comp)
}

func (r *Registry) GetConnection(id Entity) (ConnectionComponent, bool) {
	return r.conns.Get(id)
}

func (r *Registry) SetMetadata(id Entity, comp MetadataComponent) {
	r.metadata.Set(id, comp)
}

func (r *Registry) GetMetadata(id Entity) (MetadataComponent, bool) {
	return r.metadata.Get(id)
}

func (r *Registry) SetStats(id Entity, comp StatsComponent) {
	r.stats.Set(id, comp)
}

func (r *Registry) GetStats(id Entity) (StatsComponent, bool) {
	return r.stats.Get(id)
}

// GetAllEntities collects all entities that have at least a MetadataComponent.
// Adjust to whichever component is "required" for a valid entity in your design.
func (r *Registry) GetAllEntities() []Entity {
	var list []Entity
	r.metadata.Range(func(key Entity, _ MetadataComponent) bool {
		list = append(list, key)
		return true
	})
	return list
}

// EntitySnapshot holds a pre-fetched view of all components for one entity.
// Populated in a single-pass range so callers never need follow-up lookups.
type EntitySnapshot struct {
	ID       Entity
	Meta     MetadataComponent
	Pos      PositionComponent
	Stats    StatsComponent
	HasPos   bool
	HasStats bool
}

// RangeSnapshots iterates all entities that have a MetadataComponent and yields
// an EntitySnapshot with Position and Stats pre-fetched — zero follow-up lookups.
// The callback returns false to stop early (same contract as RangeMetadata).
func (r *Registry) RangeSnapshots(f func(snap EntitySnapshot) bool) {
	r.metadata.Range(func(id Entity, meta MetadataComponent) bool {
		pos, hasPos := r.positions.Get(id)
		stats, hasStats := r.stats.Get(id)
		return f(EntitySnapshot{
			ID:       id,
			Meta:     meta,
			Pos:      pos,
			Stats:    stats,
			HasPos:   hasPos,
			HasStats: hasStats,
		})
	})
}

// RangeConnections iterates all entities that have a ConnectionComponent.
// Dùng cho broadcast mà không cần build slice trung gian.
func (r *Registry) RangeConnections(f func(id Entity, conn ConnectionComponent) bool) {
	r.conns.Range(f)
}

// GetSnapshot returns a fully pre-fetched EntitySnapshot for a single entity.
// Eliminates the pattern of calling GetMetadata + GetPosition + GetStats separately.
func (r *Registry) GetSnapshot(id Entity) (EntitySnapshot, bool) {
	meta, ok := r.metadata.Get(id)
	if !ok {
		return EntitySnapshot{}, false
	}
	pos, hasPos := r.positions.Get(id)
	stats, hasStats := r.stats.Get(id)
	return EntitySnapshot{
		ID:       id,
		Meta:     meta,
		Pos:      pos,
		Stats:    stats,
		HasPos:   hasPos,
		HasStats: hasStats,
	}, true
}
