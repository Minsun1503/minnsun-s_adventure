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

// PartyMemberComponent is attached to each player who belongs to a party.
type PartyMemberComponent struct {
	PartyID Entity
}

// Registry uses sync.Map per component type.
// The separate "entities" SafeMap is eliminated — presence is implicit via component maps.
// ID generation uses an atomic counter, making RegisterEntity lock-free.
type Registry struct {
	nextID        atomic.Uint64
	positions     state.TypedSyncMap[Entity, PositionComponent] // inline value, no pointer
	conns         state.TypedSyncMap[Entity, ConnectionComponent]
	metadata      state.TypedSyncMap[Entity, MetadataComponent] // inline value, no pointer
	stats         state.TypedSyncMap[Entity, StatsComponent]    // inline value, no pointer
	ai            state.TypedSyncMap[Entity, AIComponent]       // inline value, no pointer
	inventories   state.TypedSyncMap[Entity, InventoryComponent]
	lifetimes     state.TypedSyncMap[Entity, LifetimeComponent]
	itemTemplates state.TypedSyncMap[Entity, ItemTemplateComponent]
	equipment     state.TypedSyncMap[Entity, EquipmentComponent]
	parties       state.TypedSyncMap[Entity, PartyComponent]
	partyMembers  state.TypedSyncMap[Entity, PartyMemberComponent]
	effects       state.TypedSyncMap[Entity, EffectsComponent]
}

var GlobalRegistry = &Registry{}

// NewEntity generates a new unique Entity ID atomically — no lock needed.
func (r *Registry) NewEntity() Entity {
	return Entity(r.nextID.Add(1))
}

// RemoveEntity deletes all components in parallel using a WaitGroup.
// Previous: 5 sequential lock acquisitions.
// Now: 9 concurrent sync.Map deletes.
func (r *Registry) RemoveEntity(id Entity) {
	var wg sync.WaitGroup
	wg.Add(12)
	go func() { r.positions.Delete(id); wg.Done() }()
	go func() { r.conns.Delete(id); wg.Done() }()
	go func() { r.metadata.Delete(id); wg.Done() }()
	go func() { r.stats.Delete(id); wg.Done() }()
	go func() { r.ai.Delete(id); wg.Done() }()
	go func() { r.inventories.Delete(id); wg.Done() }()
	go func() { r.lifetimes.Delete(id); wg.Done() }()
	go func() { r.itemTemplates.Delete(id); wg.Done() }()
	go func() { r.equipment.Delete(id); wg.Done() }()
	go func() { r.parties.Delete(id); wg.Done() }()
	go func() { r.partyMembers.Delete(id); wg.Done() }()
	go func() { r.effects.Delete(id); wg.Done() }()
	wg.Wait()
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
