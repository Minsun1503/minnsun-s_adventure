package ecs

import (
	"net"
	"server/peakgo/astar"
	"server/peakgo/threat"
	"server/peakgo/timer" // Tích hợp chặt chẽ hệ thống TickTimer lõi
	"sync"
	"sync/atomic"
	"time"
)

// Entity đại diện cho mã định danh thực thể kiểu uint64 giúp tra cứu Map O(1).
type Entity uint64

// Entity 0 is reserved.
// SparseSet uses 0 as "not present" marker inside the dense/sparse vectors.
const InvalidEntity Entity = 0

// ─── ENTITY TYPE ENUM ─────────────────────────────────────────────────────────
// EntityType represents a strictly typed identifier for game entity categories.
// Replaces fragile raw strings to catch invalid type queries at compile time.
type EntityType uint8

const (
	EntityAny        EntityType = iota // Matches any entity type in the grid
	EntityPlayer                       // Matches "player" type entities
	EntityMonster                      // Matches "monster" type entities
	EntityGroundItem                   // Matches "ground_item" type entities
)

// String maps the strongly-typed EntityType enum back to the underlying
// string representation. Used for logging / debugging — NOT for hot-path comparison.
func (t EntityType) String() string {
	switch t {
	case EntityPlayer:
		return "player"
	case EntityMonster:
		return "monster"
	case EntityGroundItem:
		return "ground_item"
	default:
		return ""
	}
}

// ─── COMPONENT DEFINITIONS ───────────────────────────────────────────────────
// Các Component được lưu trữ dưới dạng inline value (không dùng con trỏ)
// để tối ưu bộ nhớ đệm CPU cache-locality và triệt tiêu áp lực dọn rác của GC.

type PositionComponent struct {
	MapID int
	X     int
	Z     int
}

type ConnectionComponent struct {
	Conn net.Conn // Interface bản chất là con trỏ, giữ nguyên.
}

type MetadataComponent struct {
	Name string
	Type EntityType
}

type StatsComponent struct {
	Level        int
	XP           uint64
	HP           int
	MaxHP        int
	MP           int
	MaxMP        int
	Dam          int
	Attack       int
	MagicAttack  int
	Defense      int
	MagicDefense int
	HitRate      int
	DodgeRate    int
	CritRate     int
	CritDamage   int
}

type ItemTemplateComponent struct {
	TemplateID uint64
}

// AIState định danh các trạng thái của cỗ máy FSM quái vật.
type AIState uint8

const (
	AIStateIdle AIState = iota
	AIStateRoaming
	AIStateChasing
	AIStateAttacking
	AIStateReturning
	AIStateTransferring // Entity is being transferred between maps (frozen)
)

func (s AIState) String() string {
	switch s {
	case AIStateIdle:
		return "Idle"
	case AIStateRoaming:
		return "Roaming"
	case AIStateChasing:
		return "Chasing"
	case AIStateAttacking:
		return "Attacking"
	case AIStateReturning:
		return "Returning"
	case AIStateTransferring:
		return "Transferring"
	default:
		return "Unknown"
	}
}

// AIComponent lưu giữ trạng thái bộ đếm thời gian và chỉ số tư duy của Quái vật.
type AIComponent struct {
	State       AIState
	TargetID    Entity
	SpawnX      int
	SpawnZ      int
	SpawnRadius int
	AggroRadius float64
	LeashRadius int
	MeleeRange  int
	AttackTimer timer.TickTimer
	IdleTimer   timer.TickTimer
	PathTimer   timer.TickTimer
	RoamTargetX int
	RoamTargetZ int

	// ThreatTable tracks aggro values (peakgo/threat). Pointer for lazy init.
	// Only monsters chasing/attacking players need this — idle monsters waste nothing.
	ThreatTable *threat.ThreatTable

	// PathCache is a reusable A* pathfinder for this monster.
	// Created on first path request and recycled across ticks.
	// Pointer for lazy init — idle monsters waste nothing.
	PathCache *astar.PathCache

	// CurrentPath stores the last computed A* path so the monster can
	// step through waypoints one-by-one across multiple ticks.
	// Value type with fixed-size array — zero alloc once embedded.
	CurrentPath astar.PathResult

	// PathFollowIdx is the current waypoint index in CurrentPath.
	// -1 means no active path; 0+ means stepping toward CurrentPath.Points[followIdx].
	PathFollowIdx int

	// PathGoalX, PathGoalZ store the destination of the current A* path
	// so we can detect when the monster needs to re-path.
	PathGoalX int
	PathGoalZ int

	// PathMapID stores which map the current path was computed for.
	PathMapID int
}

type InventoryComponent struct {
	Items map[uint64]int // Maps ItemTemplateID -> Quantity owned
}

// Clone thực hiện DEEP COPY dữ liệu map bên trong.
// Bắt buộc gọi trước khi chỉnh sửa linh kiện từ bất kỳ System nào.
func (c InventoryComponent) Clone() InventoryComponent {
	if c.Items == nil {
		return InventoryComponent{Items: make(map[uint64]int)}
	}
	clone := make(map[uint64]int, len(c.Items))
	for k, v := range c.Items {
		clone[k] = v
	}
	return InventoryComponent{Items: clone}
}

type LifetimeComponent struct {
	SpawnedAt time.Time     // The exact moment the item hit the floor
	Duration  time.Duration // How long it is allowed to live (e.g., 60 * time.Second)
}

// ActiveEffect represents a single temporary modifier layer running on an entity.
type ActiveEffect struct {
	Type         string        // "poison", "burn", "haste_buff"
	Value        int           // The damage or stat modifier amount
	Duration     time.Duration // Total remaining time
	LastTickTime time.Time     // Last time a DoT damage was applied
}

// EffectsComponent is mapped directly to an entity row anchor inside the TypedSyncMap
type EffectsComponent struct {
	ActiveList []ActiveEffect
}

// Clone thực hiện DEEP COPY danh sách hiệu ứng bên trong.
func (c EffectsComponent) Clone() EffectsComponent {
	if c.ActiveList == nil {
		return EffectsComponent{ActiveList: nil}
	}
	return EffectsComponent{
		ActiveList: append([]ActiveEffect(nil), c.ActiveList...),
	}
}

type EquipmentComponent struct {
	WeaponID uint64
	ArmorID  uint64
}

type PartyComponent struct {
	LeaderID  Entity
	TeamName  string
	MemberIDs []Entity
}

func (c PartyComponent) Clone() PartyComponent {
	return PartyComponent{
		LeaderID:  c.LeaderID,
		TeamName:  c.TeamName,
		MemberIDs: append([]Entity(nil), c.MemberIDs...), // Deep copy mảng tránh gãy vùng nhớ.
	}
}

type PartyMemberComponent struct {
	PartyID Entity
}

// ─── SPARSE SET COMPONENT STORE ARCHITECTURE ─────────────────────────────────

const chunkSize = 1024

var entitySlicePool = sync.Pool{
	New: func() any {
		s := make([]Entity, 0, 1024)
		return &s
	},
}

type ComponentStore[T any] struct {
	mu     sync.RWMutex
	dense  []Entity
	sparse [][]int32 // Paged sparse array chống phình bộ nhớ phân mảnh.
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

	if idx != 0 && idx-1 < int32(len(s.dense)) && s.dense[idx-1] == id {
		s.values[idx-1] = val
		s.mu.Unlock()
		return
	}

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

// Range là hàm siêu tốc tối ưu cho Hot-Path ĐỌC dữ liệu (99% chu kỳ game loop).
// Đã sửa: Giữ RLock duy nhất cho toàn bộ chu trình duyệt mảng dense/values song song,
// triệt tiêu hoàn toàn gánh nặng lock/unlock liên tục trên từng thực thể độc lập.
// Giao kèo: Lập trình viên KHÔNG ĐƯỢC phép thêm/xóa component của store này trong hàm callback f.
func (s *ComponentStore[T]) Range(f func(key Entity, value T) bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i, id := range s.dense {
		if !f(id, s.values[i]) {
			break
		}
	}
}

// RangeMutate chuyên dụng cho các vòng lặp cần chỉnh sửa cấu trúc (Mutation).
// Thích hợp khi callback f cần gọi Set() hoặc Delete() trực tiếp lên chính store này.
// Tận dụng mảng đệm mượn từ Pool để triệt tiêu chi phí Allocation vùng nhớ rác.
func (s *ComponentStore[T]) RangeMutate(f func(key Entity) bool) {
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
		// Re-fetch động bảo vệ an toàn cho bộ nhớ nếu vòng lặp trước đã thực hiện lệnh xóa
		if _, ok := s.Get(id); ok {
			if !f(id) {
				break
			}
		}
	}

	*pBuf = buf
	entitySlicePool.Put(pBuf)
}

// ─── ENTITY RECYCLING ────────────────────────────────────────────────────────
//
// Entity IDs are recycled after destruction to prevent unbounded growth during
// long server uptimes. The recycledIDs queue stores IDs of destroyed entities
// and hands them out before consuming fresh IDs from the atomic counter.
//
// The recycling pool is a lock-free CAS queue (built on a simple slice with a
// mutex) since the volume is low — entity destrucations are rare compared to
// movement ticks. The sync.Pool pattern is unnecessary here; a slice-based
// freelist with a single mutex is simpler and sufficient.

// entityFreelist is a pooled slice of recycled entity IDs protected by a mutex.
type entityFreelist struct {
	mu  sync.Mutex
	ids []Entity
}

var recycledEntities entityFreelist

// recycleEntityID adds a destroyed entity's ID back to the freelist for reuse.
// Safe for concurrent callers (e.g. map goroutines recycling cross-map entities).
func recycleEntityID(id Entity) {
	recycledEntities.mu.Lock()
	recycledEntities.ids = append(recycledEntities.ids, id)
	recycledEntities.mu.Unlock()
}

// PopRecycledEntityID returns a recycled entity ID if one is available, or 0.
// Exported for use by the world package's entity ID allocation.
func PopRecycledEntityID() Entity {
	recycledEntities.mu.Lock()
	if len(recycledEntities.ids) == 0 {
		recycledEntities.mu.Unlock()
		return 0
	}
	last := len(recycledEntities.ids) - 1
	id := recycledEntities.ids[last]
	recycledEntities.ids = recycledEntities.ids[:last]
	recycledEntities.mu.Unlock()
	return id
}

// ─── MASTER REGISTRY SYSTEM ──────────────────────────────────────────────────

type Registry struct {
	nextID        atomic.Uint64
	positions     ComponentStore[PositionComponent]
	conns         ComponentStore[ConnectionComponent]
	metadata      ComponentStore[MetadataComponent]
	stats         ComponentStore[StatsComponent]
	ai            ComponentStore[AIComponent] // Quản lý AI Quái vật.
	inventories   ComponentStore[InventoryComponent]
	lifetimes     ComponentStore[LifetimeComponent]
	itemTemplates ComponentStore[ItemTemplateComponent]
	equipment     ComponentStore[EquipmentComponent]
	parties       ComponentStore[PartyComponent]
	partyMembers  ComponentStore[PartyMemberComponent]
	effects       ComponentStore[EffectsComponent]
}

// DefaultRegistry is the transitional global registry pointer set by the game loop.
// Systems that have not yet been refactored to receive an explicit *Registry parameter
// read from this pointer. The game loop (systems/gameloop.go) sets this to the
// current MapWorker's registry before each per-map tick.
//
// During the full DI migration, this will be removed and all game systems will
// accept *Registry as a function parameter.
//
// init() initializes DefaultRegistry with a fresh Registry at package load so
// tests and standalone code that haven't gone through the game loop still work.
// Production code (perMapTick in systems/gameloop.go) overrides this with the
// appropriate MapWorker.Registry on each tick.
var DefaultRegistry *Registry

func init() {
	DefaultRegistry = NewRegistry()
}

// NewRegistry creates a new empty Registry with fresh component stores.
func NewRegistry() *Registry {
	return &Registry{}
}

// NewEntity returns a recycled entity ID if one is available, otherwise
// allocates a fresh ID from the atomic counter. This prevents unbounded
// growth of the entity ID space during long server uptimes.
func (r *Registry) NewEntity() Entity {
	if recycled := PopRecycledEntityID(); recycled != 0 {
		return recycled
	}
	return Entity(r.nextID.Add(1))
}

// RemoveEntity cleans up all components for an entity and recycles its ID.
// Before deleting each component, it explicitly nils out any slice/map fields
// and closes pooled resources (ThreatTable, PathCache) to prevent memory leaks
// when entities are despawned. This ensures GC can reclaim internal data even
// if the dense array slot is later swapped.
//
// Callers MUST also remove the entity from the SpatialGrid separately
// (via the CommandBuffer's Destroy path or a direct grid.RemoveEntity call).
//
// Cảnh báo Concurrency: Hàm này không mang tính Transaction nguyên tử liên vùng.
// Hoạt động an toàn tuyệt đối dưới cấu trúc vòng lặp game loop đơn luồng (Single-threaded worker framework).
func (r *Registry) RemoveEntity(id Entity) {
	// ── AI Component Cleanup ─────────────────────────────────────────────────
	// Close pooled ThreatTable and release PathCache references before deletion
	// so GC can reclaim them even if the dense array slot is swapped.
	if ai, ok := r.ai.Get(id); ok {
		if ai.ThreatTable != nil {
			ai.ThreatTable.Close()
			ai.ThreatTable = nil
		}
		if ai.PathCache != nil {
			ai.PathCache.Reset() // Release internal state
			ai.PathCache = nil
		}
		ai.CurrentPath = astar.PathResult{}
		ai.PathFollowIdx = -1
		r.ai.Set(id, ai)
	}

	// ── Effects Component Cleanup ────────────────────────────────────────────
	// Nil the ActiveList slice so the underlying backing array (potentially large)
	// can be reclaimed by the GC.
	if effects, ok := r.effects.Get(id); ok {
		effects.ActiveList = nil
		r.effects.Set(id, effects)
	}

	// ── Inventory Component Cleanup ──────────────────────────────────────────
	// Nil the Items map so the bucket entries and hash table memory are freed.
	if inv, ok := r.inventories.Get(id); ok {
		inv.Items = nil
		r.inventories.Set(id, inv)
	}

	// ── Party Component Cleanup ──────────────────────────────────────────────
	// Nil the MemberIDs slice to release the backing array.
	if party, ok := r.parties.Get(id); ok {
		party.MemberIDs = nil
		r.parties.Set(id, party)
	}

	// ── Delete All Components ─────────────────────────────────────────────────
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

	// Recycled the entity ID so it can be reused by a future NewEntity call.
	recycleEntityID(id)
}

// TotalEntityIDs returns the current atomic ID counter value.
// Useful for diagnostics and monitoring entity ID growth.
func (r *Registry) TotalEntityIDs() uint64 {
	return r.nextID.Load()
}

// RecycledEntityCount returns the number of recycled IDs currently in the freelist.
func RecycledEntityCount() int {
	recycledEntities.mu.Lock()
	n := len(recycledEntities.ids)
	recycledEntities.mu.Unlock()
	return n
}

// ─── AI COMPONENT STORE PUBLIC APIS ──────────────────────────────────────────
// Đã sửa: Bổ sung trọn bộ API cầu nối AI cho Registry để hệ thống Quái vật compile thành công 100%.

func (r *Registry) SetAI(id Entity, comp AIComponent)                 { r.ai.Set(id, comp) }
func (r *Registry) GetAI(id Entity) (AIComponent, bool)               { return r.ai.Get(id) }
func (r *Registry) DeleteAI(id Entity)                                { r.ai.Delete(id) }
func (r *Registry) RangeAI(f func(id Entity, value AIComponent) bool) { r.ai.Range(f) }

// ─── STANDARD COMPONENT REGISTRY CORE APIS ───────────────────────────────────

func (r *Registry) SetPosition(id Entity, comp PositionComponent)   { r.positions.Set(id, comp) }
func (r *Registry) GetPosition(id Entity) (PositionComponent, bool) { return r.positions.Get(id) }

func (r *Registry) SetConnection(id Entity, comp ConnectionComponent)   { r.conns.Set(id, comp) }
func (r *Registry) GetConnection(id Entity) (ConnectionComponent, bool) { return r.conns.Get(id) }

func (r *Registry) SetMetadata(id Entity, comp MetadataComponent)                { r.metadata.Set(id, comp) }
func (r *Registry) GetMetadata(id Entity) (MetadataComponent, bool)              { return r.metadata.Get(id) }
func (r *Registry) RangeMetadata(f func(id Entity, meta MetadataComponent) bool) { r.metadata.Range(f) }

func (r *Registry) SetStats(id Entity, comp StatsComponent)   { r.stats.Set(id, comp) }
func (r *Registry) GetStats(id Entity) (StatsComponent, bool) { return r.stats.Get(id) }

func (r *Registry) SetLifetime(id Entity, comp LifetimeComponent)   { r.lifetimes.Set(id, comp) }
func (r *Registry) GetLifetime(id Entity) (LifetimeComponent, bool) { return r.lifetimes.Get(id) }

func (r *Registry) SetInventory(id Entity, comp InventoryComponent)   { r.inventories.Set(id, comp) }
func (r *Registry) GetInventory(id Entity) (InventoryComponent, bool) { return r.inventories.Get(id) }

func (r *Registry) SetItemTemplate(id Entity, comp ItemTemplateComponent) {
	r.itemTemplates.Set(id, comp)
}
func (r *Registry) GetItemTemplate(id Entity) (ItemTemplateComponent, bool) {
	return r.itemTemplates.Get(id)
}

func (r *Registry) SetEquipment(id Entity, comp EquipmentComponent)   { r.equipment.Set(id, comp) }
func (r *Registry) GetEquipment(id Entity) (EquipmentComponent, bool) { return r.equipment.Get(id) }

func (r *Registry) SetEffects(id Entity, comp EffectsComponent)                { r.effects.Set(id, comp) }
func (r *Registry) GetEffects(id Entity) (EffectsComponent, bool)              { return r.effects.Get(id) }
func (r *Registry) DeleteEffects(id Entity)                                    { r.effects.Delete(id) }
func (r *Registry) RangeEffects(f func(id Entity, comp EffectsComponent) bool) { r.effects.Range(f) }

func (r *Registry) SetParty(id Entity, comp PartyComponent)   { r.parties.Set(id, comp) }
func (r *Registry) GetParty(id Entity) (PartyComponent, bool) { return r.parties.Get(id) }
func (r *Registry) DeleteParty(id Entity)                     { r.parties.Delete(id) }

func (r *Registry) SetPartyMember(id Entity, comp PartyMemberComponent) { r.partyMembers.Set(id, comp) }
func (r *Registry) GetPartyMember(id Entity) (PartyMemberComponent, bool) {
	return r.partyMembers.Get(id)
}
func (r *Registry) DeletePartyMember(id Entity) { r.partyMembers.Delete(id) }
func (r *Registry) RangePartyMembers(f func(id Entity, pm PartyMemberComponent) bool) {
	r.partyMembers.Range(f)
}

func (r *Registry) RangeConnections(f func(id Entity, conn ConnectionComponent) bool) {
	r.conns.Range(f)
}

// ─── COMPONENT MUTATION POLICY ───────────────────────────────────────────────
//
// All Component values in the ECS are stored inline (not as pointers) inside
// ComponentStore[T]. This ensures CPU cache-locality and zero GC pressure on
// the hot path.
//
// Read policy (hot-path safe):
//   - Use Get() to obtain a read-only copy of the component value.
//   - The returned value is a stack-allocated copy — the caller may read it
//     freely without locks after Get() returns.
//
// Write policy (Copy-Modify-Override):
//   1. Read:   val, ok := registry.GetComponent(id)
//   2. Modify: val.Field = newValue  (mutate the local copy)
//   3. Write:  registry.SetComponent(id, val)
//
// NEVER:
//   - Store pointers to component values (use inline structs only).
//   - Return pointers to internal ComponentStore.values elements.
//   - Mutate a component value obtained from Get() and expect it to persist
//     without calling Set().
//
// For slices and maps inside components (e.g. InventoryComponent.Items,
// PartyComponent.MemberIDs, EffectsComponent.ActiveList), use the Clone()
// method provided on the struct BEFORE mutation. This prevents accidental
// sharing of slice/map headers between the stored value and the working copy.
//
// ─── QUERY LAYER: COMBINED COMPONENT LOOKUPS ─────────────────────────────────

// QueryPositionStats iterates over all entities that have both a Position and
// Stats component. Uses the smaller store (positions) as the driver and looks
// up the other (stats) via O(1) Get. This avoids two separate Range passes
// when both components are needed together (e.g. AOI broadcast, combat tick).
//
// The callback receives the entity ID, its PositionComponent and StatsComponent.
// Return false from the callback to stop iteration early.
func (r *Registry) QueryPositionStats(f func(id Entity, pos PositionComponent, stats StatsComponent) bool) {
	r.positions.Range(func(id Entity, pos PositionComponent) bool {
		if stats, ok := r.stats.Get(id); ok {
			return f(id, pos, stats)
		}
		return true // continue iteration
	})
}

// QueryPositionAI iterates over all entities that have an AI component
// (i.e. monsters) and looks up their Position + Stats components in O(1).
// The AI store is the smallest store (only monsters), making this the most
// efficient way to process monster AI ticks.
//
// The callback receives entity ID, AIComponent, PositionComponent, StatsComponent.
// Return false to stop iteration early.
func (r *Registry) QueryPositionAI(f func(id Entity, ai AIComponent, pos PositionComponent, stats StatsComponent) bool) {
	r.ai.Range(func(id Entity, ai AIComponent) bool {
		pos, okPos := r.positions.Get(id)
		if !okPos {
			return true // skip entities without position (shouldn't happen but be safe)
		}
		stats, okStats := r.stats.Get(id)
		if !okStats {
			return true // skip entities without stats
		}
		return f(id, ai, pos, stats)
	})
}

// QueryPositionMetadata iterates over all entities that have Metadata
// and looks up their Position in O(1). This is useful for the initial
// snapshot pass in the game loop (hasPlayers check, monster state logging).
//
// The callback receives entity ID, MetadataComponent, PositionComponent.
// Return false to stop iteration early.
func (r *Registry) QueryPositionMetadata(f func(id Entity, meta MetadataComponent, pos PositionComponent) bool) {
	r.metadata.Range(func(id Entity, meta MetadataComponent) bool {
		pos, ok := r.positions.Get(id)
		if !ok {
			return true // skip entities without position
		}
		return f(id, meta, pos)
	})
}

// QueryMonsterCombat iterates over AI store (monsters only) to process
// combat-relevant iterations (AI + Position + Stats). This is the most
// targeted query for the map tick when we need to run monster AI ticks
// and perform combat logic.
//
// The callback receives entity ID, AIComponent, PositionComponent, StatsComponent.
// Return false to stop iteration early.
func (r *Registry) QueryMonsterCombat(f func(id Entity, ai AIComponent, pos PositionComponent, stats StatsComponent) bool) {
	// Identical implementation to QueryPositionAI but semantically distinct.
	// QueryPositionAI is for general AI ticking (roaming, pathfinding).
	// QueryMonsterCombat is for combat-specific passes (damage, threat, aggro range).
	// If performance of combined pass is ever needed, the caller merges both into
	// a single QueryPositionAI pass — these aliases exist to make intent explicit.
	r.ai.Range(func(id Entity, ai AIComponent) bool {
		pos, okPos := r.positions.Get(id)
		if !okPos {
			return true
		}
		stats, okStats := r.stats.Get(id)
		if !okStats {
			return true
		}
		return f(id, ai, pos, stats)
	})
}

// ─── DIAGNOSTIC & SURVEY LAYER APIS ──────────────────────────────────────────

// GetAllEntities thu thập toàn bộ ID thực thể đang hoạt động trong game.
// Cảnh báo Hiệu năng: Hàm này liên tục thực hiện lệnh append gây phình Heap bộ nhớ rác.
// Chỉ định tuyến sử dụng cho mục đích Debug, Admin Command hoặc lưu dữ liệu (Save Game). Tuyệt đối cấm gọi trên Hot-Path game loop!
func (r *Registry) GetAllEntities() []Entity {
	var list []Entity
	r.metadata.Range(func(key Entity, _ MetadataComponent) bool {
		list = append(list, key)
		return true
	})
	return list
}

// EntitySnapshot đại diện cho một góc nhìn tổng hợp nhanh về trạng thái thực thể.
type EntitySnapshot struct {
	ID       Entity
	Meta     MetadataComponent
	Pos      PositionComponent
	Stats    StatsComponent
	HasPos   bool
	HasStats bool
}

// RangeSnapshots thực hiện kết xuất lát cắt trạng thái thực thể diện rộng.
// Chú thích thiết kế: Hàm này thực hiện nhiều lệnh tra cứu riêng lẻ, dữ liệu tổng hợp không mang tính
// nguyên tử tuyệt đối trừ khi toàn bộ tiến trình game loop được bảo vệ bởi một khóa Master Lock bên ngoài.
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

// Sắp xếp dữ liệu kiểm soát thủ công ID counter nếu cần thiết (ví dụ: hot-reload)
func (r *Registry) SetNextID(val uint64) { r.nextID.Store(val) }
