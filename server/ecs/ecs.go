package ecs

import (
	"net"
	"sync"
	"sync/atomic"
)

// Entity is a uint64 for O(1) integer map lookup instead of string hashing.
type Entity uint64

// Components stored as inline values (not pointers) to avoid extra heap allocs.
// Exception: ConnectionComponent keeps net.Conn as an interface (already a pointer).

type PositionComponent struct {
	MapID int
	X     int
	Z     int
}

type ConnectionComponent struct {
	Conn net.Conn
}

type MetadataComponent struct {
	Name string
	Type string
}

type StatsComponent struct {
	Level int
	XP    uint64
	HP    int
	MaxHP int
	MP    int
	MaxMP int
	Dam   int
}

type ItemTemplateComponent struct {
	TemplateID uint64
}

// PartyComponent represents a player party/team entity.
// The entity owning this component is the party itself (not a player).
// MemberIDs tracks all current members in insertion order.
// PartyMemberComponent on each member carries a back-reference to this party entity.
type PartyComponent struct {
	LeaderID  Entity
	TeamName  string
	MemberIDs []Entity
}

// Clone thực hiện DEEP COPY danh sách thành viên tổ đội bên trong.
func (c PartyComponent) Clone() PartyComponent {
	return PartyComponent{
		LeaderID:  c.LeaderID,
		TeamName:  c.TeamName,
		MemberIDs: append([]Entity(nil), c.MemberIDs...),
	}
}

// PartyMemberComponent is attached to each player who belongs to a party.
type PartyMemberComponent struct {
	PartyID Entity
}

const chunkSize = 1024

var entitySlicePool = sync.Pool{
	New: func() any {
		s := make([]Entity, 0, 1024)
		return &s
	},
}

// ComponentStore implements a thread-safe, high-performance, cache-friendly Sparse Set.
// It stores components contiguously in memory and provides O(1) lookups, insertions, and deletions.
type ComponentStore[T any] struct {
	mu     sync.RWMutex
	dense  []Entity
	sparse [][]int32 // Paged sparse array to avoid allocating huge flat slices for sparse Entity IDs
	values []T
}

func (s *ComponentStore[T]) Set(id Entity, val T) {
	s.mu.Lock()

	pageIndex := id / chunkSize
	if pageIndex >= Entity(len(s.sparse)) {
		newSparse := make([][]int32, pageIndex+1)
		copy(newSparse, s.sparse)
		s.sparse = newSparse
	}

	page := s.sparse[pageIndex]
	if page == nil {
		page = make([]int32, chunkSize)
		s.sparse[pageIndex] = page
	}

	offset := id % chunkSize
	idx := page[offset]

	// Overwrite if already exists
	if idx != 0 && idx-1 < int32(len(s.dense)) && s.dense[idx-1] == id {
		s.values[idx-1] = val
		s.mu.Unlock()
		return
	}

	// Add new
	newIdx := int32(len(s.dense)) + 1
	page[offset] = newIdx
	s.dense = append(s.dense, id)
	s.values = append(s.values, val)
	s.mu.Unlock()
}

func (s *ComponentStore[T]) Get(id Entity) (T, bool) {
	s.mu.RLock()

	pageIndex := id / chunkSize
	if pageIndex >= Entity(len(s.sparse)) {
		s.mu.RUnlock()
		var zero T
		return zero, false
	}
	page := s.sparse[pageIndex]
	if page == nil {
		s.mu.RUnlock()
		var zero T
		return zero, false
	}
	idx := page[id%chunkSize]
	if idx != 0 && idx-1 < int32(len(s.dense)) && s.dense[idx-1] == id {
		val := s.values[idx-1]
		s.mu.RUnlock()
		return val, true
	}
	s.mu.RUnlock()
	var zero T
	return zero, false
}

func (s *ComponentStore[T]) Delete(id Entity) {
	s.mu.Lock()

	pageIndex := id / chunkSize
	if pageIndex >= Entity(len(s.sparse)) {
		s.mu.Unlock()
		return
	}
	page := s.sparse[pageIndex]
	if page == nil {
		s.mu.Unlock()
		return
	}

	offset := id % chunkSize
	idx := page[offset]

	if idx == 0 || idx-1 >= int32(len(s.dense)) || s.dense[idx-1] != id {
		s.mu.Unlock()
		return
	}

	actualIdx := idx - 1
	lastIdx := int32(len(s.dense)) - 1

	if actualIdx != lastIdx {
		lastEntity := s.dense[lastIdx]
		s.dense[actualIdx] = lastEntity
		s.values[actualIdx] = s.values[lastIdx]

		lastPageIndex := lastEntity / chunkSize
		lastOffset := lastEntity % chunkSize
		s.sparse[lastPageIndex][lastOffset] = actualIdx + 1
	}

	page[offset] = 0
	s.dense = s.dense[:lastIdx]
	var zero T
	s.values[lastIdx] = zero
	s.values = s.values[:lastIdx]
	s.mu.Unlock()
}

func (s *ComponentStore[T]) Range(f func(key Entity, value T) bool) {
	s.mu.RLock()
	count := len(s.dense)
	if count == 0 {
		s.mu.RUnlock()
		return
	}

	pBuf := entitySlicePool.Get().(*[]Entity)
	buf := *pBuf
	if cap(buf) < count {
		buf = make([]Entity, count)
	} else {
		buf = buf[:count]
	}
	copy(buf, s.dense)
	s.mu.RUnlock()

	for _, id := range buf {
		val, ok := s.Get(id)
		if ok {
			if !f(id, val) {
				break
			}
		}
	}

	*pBuf = buf
	entitySlicePool.Put(pBuf)
}

// Registry uses paged Sparse Set per component type for peak cache-locality and O(1) performance.
// ID generation uses an atomic counter, making RegisterEntity lock-free.
type Registry struct {
	nextID        atomic.Uint64
	positions     ComponentStore[PositionComponent]
	conns         ComponentStore[ConnectionComponent]
	metadata      ComponentStore[MetadataComponent]
	stats         ComponentStore[StatsComponent]
	ai            ComponentStore[AIComponent]
	inventories   ComponentStore[InventoryComponent]
	lifetimes     ComponentStore[LifetimeComponent]
	itemTemplates ComponentStore[ItemTemplateComponent]
	equipment     ComponentStore[EquipmentComponent]
	parties       ComponentStore[PartyComponent]
	partyMembers  ComponentStore[PartyMemberComponent]
	effects       ComponentStore[EffectsComponent]
}

var GlobalRegistry = &Registry{}

// NewEntity generates a new unique Entity ID atomically — no lock needed.
func (r *Registry) NewEntity() Entity {
	return Entity(r.nextID.Add(1))
}

// RemoveEntity deletes all components sequentially.
func (r *Registry) RemoveEntity(id Entity) {
	r.positions.Delete(id)
	r.conns.Delete(id)
	r.metadata.Delete(id)
	r.stats.Delete(id)
	r.ai.Delete(id)
	r.inventories.Delete(id)
	r.lifetimes.Delete(id)
	r.itemTemplates.Delete(id)
	r.equipment.Delete(id)
	r.parties.Delete(id)
	r.partyMembers.Delete(id)
	r.effects.Delete(id)
}

// RangeMetadata iterates all entities that have a MetadataComponent.
func (r *Registry) RangeMetadata(f func(id Entity, meta MetadataComponent) bool) {
	r.metadata.Range(f)
}

// SetNextID sets the internal atomic entity ID counter.
func (r *Registry) SetNextID(val uint64) {
	r.nextID.Store(val)
}

// RangeEffects iterates all entities that have an EffectsComponent.
func (r *Registry) RangeEffects(f func(id Entity, comp EffectsComponent) bool) {
	r.effects.Range(f)
}

func (r *Registry) SetEffects(id Entity, comp EffectsComponent) {
	r.effects.Set(id, comp)
}

func (r *Registry) GetEffects(id Entity) (EffectsComponent, bool) {
	return r.effects.Get(id)
}

func (r *Registry) DeleteEffects(id Entity) {
	r.effects.Delete(id)
}

func (r *Registry) SetLifetime(id Entity, comp LifetimeComponent) {
	r.lifetimes.Set(id, comp)
}

func (r *Registry) GetLifetime(id Entity) (LifetimeComponent, bool) {
	return r.lifetimes.Get(id)
}

func (r *Registry) SetItemTemplate(id Entity, comp ItemTemplateComponent) {
	r.itemTemplates.Set(id, comp)
}

func (r *Registry) GetItemTemplate(id Entity) (ItemTemplateComponent, bool) {
	return r.itemTemplates.Get(id)
}

func (r *Registry) SetEquipment(id Entity, comp EquipmentComponent) {
	r.equipment.Set(id, comp)
}

func (r *Registry) GetEquipment(id Entity) (EquipmentComponent, bool) {
	return r.equipment.Get(id)
}

func (r *Registry) SetPosition(id Entity, comp PositionComponent) {
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

// ─── Party Component helpers ─────────────────────────────────────────────

func (r *Registry) SetParty(id Entity, comp PartyComponent) {
	r.parties.Set(id, comp)
}

func (r *Registry) GetParty(id Entity) (PartyComponent, bool) {
	return r.parties.Get(id)
}

func (r *Registry) DeleteParty(id Entity) {
	r.parties.Delete(id)
}

func (r *Registry) SetPartyMember(id Entity, comp PartyMemberComponent) {
	r.partyMembers.Set(id, comp)
}

func (r *Registry) GetPartyMember(id Entity) (PartyMemberComponent, bool) {
	return r.partyMembers.Get(id)
}

func (r *Registry) DeletePartyMember(id Entity) {
	r.partyMembers.Delete(id)
}

// RangePartyMembers iterates all entities that have a PartyMemberComponent.
func (r *Registry) RangePartyMembers(f func(id Entity, pm PartyMemberComponent) bool) {
	r.partyMembers.Range(f)
}

// GetAllEntities collects all entities that have at least a MetadataComponent.
func (r *Registry) GetAllEntities() []Entity {
	var list []Entity
	r.metadata.Range(func(key Entity, _ MetadataComponent) bool {
		list = append(list, key)
		return true
	})
	return list
}

// EntitySnapshot holds a pre-fetched view of all components for one entity.
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
func (r *Registry) RangeConnections(f func(id Entity, conn ConnectionComponent) bool) {
	r.conns.Range(f)
}

// GetSnapshot returns a fully pre-fetched EntitySnapshot for a single entity.
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
