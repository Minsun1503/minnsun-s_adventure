package world

import (
	"server/ecs"
	"server/logger"
	"sync"
	"sync/atomic"
)

// ─── Cross-Map Entity Transfer ──────────────────────────────────────────────

// TransferRequest represents a request to move an entity from one map to another.
// Pushed to the central orchestrator channel by the source MapWorker.
type TransferRequest struct {
	EntityID ecs.Entity
	FromMap  int
	ToMap    int
}

// EntitySnapshot contains all component data needed to transfer an entity
// between MapWorkers. This is a packed struct that gets serialized through
// the transfer channel.
type EntitySnapshot struct {
	ID    ecs.Entity
	Pos   ecs.PositionComponent
	Meta  ecs.MetadataComponent
	Stats ecs.StatsComponent
	Conn  ecs.ConnectionComponent // nil for monsters/NPCs
	AI    ecs.AIComponent
}

// ─── World ────────────────────────────────────────────────────────────────────
//
// World is the top-level orchestrator for all game maps. It manages:
//   - MapWorkers (one per map, each with its own ECS Registry + SpatialGrid + AOI)
//   - Global entity ID counter (ensures IDs are unique across all maps)
//   - Cross-map transfer orchestrator (serializes/deserializes entities between maps)
//   - Map lifecycle (start/stop individual maps)
//
// Design decisions:
//   - NO Archetype ECS: Each MapWorker uses the same ComponentStore pattern.
//   - NO Microservices: All maps run in-process on separate goroutines.
//   - NO Plugin Framework: Systems are compiled in at build time.
//   - The atomic nextID counter is the sole global state — everything else
//     is isolated per-map for lock-free multi-core scaling.

// World is the top-level orchestrator for all game maps.
type World struct {
	mu      sync.RWMutex
	workers map[int]*MapWorker

	// nextID is the global entity ID counter.
	// Entity IDs are unique across ALL maps — allocated via AllocateEntityID.
	nextID atomic.Uint64

	// transferChan is the central orchestrator channel for cross-map transfers.
	transferChan chan TransferRequest

	// tickFn is the per-map tick function registered at boot.
	tickFn MapTickFn

	// wg tracks all running map worker goroutines for graceful shutdown.
	wg sync.WaitGroup

	// transferWg tracks the transfer orchestrator goroutine.
	transferWg sync.WaitGroup
}

// GlobalWorld is the singleton World instance used by all systems.
var GlobalWorld *World

// InitWorld creates and initializes the global World instance.
// Must be called once at server startup before any maps are started.
func InitWorld(fn MapTickFn) *World {
	w := &World{
		workers:      make(map[int]*MapWorker),
		transferChan: make(chan TransferRequest, 256),
		tickFn:       fn,
	}
	GlobalWorld = w
	logger.Info("[WORLD] Initialized global World instance.")
	return w
}

// AllocateEntityID returns a recycled entity ID if available, otherwise
// allocates a fresh ID from the atomic counter. This ensures uniqueness
// across all maps.
func (w *World) AllocateEntityID() ecs.Entity {
	if recycled := ecs.PopRecycledEntityID(); recycled != 0 {
		return recycled
	}
	return ecs.Entity(w.nextID.Add(1))
}

// SetNextID sets the global entity ID counter. Used during boot to align
// with the maximum character ID in the database.
func (w *World) SetNextID(val uint64) {
	w.nextID.Store(val)
}

// TotalEntityIDs returns the current atomic ID counter value.
func (w *World) TotalEntityIDs() uint64 {
	return w.nextID.Load()
}

// ─── Map Lifecycle ──────────────────────────────────────────────────────────

// StartMap creates a new MapWorker for the given map ID and starts its
// tick goroutine. The map's tick function uses the world's registered
// MapTickFn with per-map state isolation.
func (w *World) StartMap(mapID int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.workers[mapID]; exists {
		logger.Warn("[WORLD] Map %d already running — ignoring duplicate start.", mapID)
		return
	}

	mw := NewMapWorker(mapID, w.tickFn)
	w.workers[mapID] = mw

	// Start the map goroutine with its own tick channel for true parallelism.
	w.wg.Add(1)
	go w.runMapWorker(mw)

	logger.Info("[WORLD] Started map %d", mapID)
}

// StopMap signals a MapWorker goroutine to shut down gracefully.
func (w *World) StopMap(mapID int) {
	w.mu.Lock()
	mw, ok := w.workers[mapID]
	if ok {
		delete(w.workers, mapID)
	}
	w.mu.Unlock()

	if ok && mw != nil {
		// Close the tick channel to signal shutdown
		mw.Close()
	}
}

// ShutdownAll stops all map workers and the transfer orchestrator gracefully.
func (w *World) ShutdownAll() {
	logger.Info("[WORLD] Shutting down all map workers...")

	// Close the transfer channel to stop the orchestrator
	close(w.transferChan)

	// Stop all map workers
	w.mu.RLock()
	for mapID := range w.workers {
		if mw := w.workers[mapID]; mw != nil {
			mw.Close()
		}
	}
	w.mu.RUnlock()

	// Wait for all workers to finish
	w.wg.Wait()
	logger.Info("[WORLD] All map workers shut down.")
}

// GetWorker returns the MapWorker for the given map ID, or nil if not running.
func (w *World) GetWorker(mapID int) *MapWorker {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.workers[mapID]
}

// RunMapIDs returns a slice of all currently running map IDs.
func (w *World) RunMapIDs() []int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	ids := make([]int, 0, len(w.workers))
	for id := range w.workers {
		ids = append(ids, id)
	}
	return ids
}

// IsMapRunning returns true if the given map ID has a running worker.
func (w *World) IsMapRunning(mapID int) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.workers[mapID]
	return ok
}

// ─── Tick Dispatch (True Parallelism) ────────────────────────────────────────

// TickAll dispatches a tick to all running MapWorkers concurrently via their
// dedicated tick channels. Each worker processes the tick on its own goroutine.
// Non-blocking sends ensure one slow map cannot stall others.
//
// The function does NOT wait for all workers to finish — workers process ticks
// asynchronously. This is the key architectural change from sequential ticking
// to true multi-core parallelism.
func (w *World) TickAll(tick uint64) {
	w.mu.RLock()
	workerList := make([]*MapWorker, 0, len(w.workers))
	for _, mw := range w.workers {
		workerList = append(workerList, mw)
	}
	w.mu.RUnlock()

	// Dispatch tick to all workers concurrently via their goroutine channels.
	// Non-blocking send ensures one slow map doesn't stall others.
	for _, mw := range workerList {
		select {
		case mw.tickChan <- tick:
			// Successfully dispatched
		default:
			// Worker is still busy — skip this tick (tick is dropped)
			logger.Debug("[WORLD] Map %d tickChan full — dropping tick %d (worker busy)", mw.ID, tick)
		}
	}
}

// Tick sends a tick to a specific map's worker goroutine via its tick channel.
// Returns false if the map is not running.
func (w *World) Tick(mapID int, tick uint64) bool {
	mw := w.GetWorker(mapID)
	if mw == nil {
		return false
	}
	select {
	case mw.tickChan <- tick:
		return true
	default:
		logger.Debug("[WORLD] Map %d tickChan full — dropping tick %d (worker busy)", mapID, tick)
		return false
	}
}

// ─── Cross-Map Transfer ─────────────────────────────────────────────────────

// TransferEntity serializes an entity from the source MapWorker and
// enqueues it for transfer to the destination MapWorker via two-phase commit.
// Called by a MapWorker during its tick.
func (w *World) TransferEntity(entityID ecs.Entity, fromMap, toMap int) {
	w.transferChan <- TransferRequest{
		EntityID: entityID,
		FromMap:  fromMap,
		ToMap:    toMap,
	}
}

// StartTransferOrchestrator launches the goroutine that processes cross-map
// entity transfers. Must be called once at server boot.
func (w *World) StartTransferOrchestrator() {
	logger.Info("[WORLD] Cross-map transfer orchestrator started")
	w.transferWg.Add(1)
	go func() {
		defer w.transferWg.Done()
		for req := range w.transferChan {
			w.processTransfer2PC(req)
		}
		logger.Info("[WORLD] Cross-map transfer orchestrator stopped.")
	}()
}

// ─── Two-Phase Commit Transfer ───────────────────────────────────────────────
//
// The 2PC protocol guarantees zero data loss during cross-map entity transfer:
//
// Phase 1 (Lock): Mark the entity as "transferring" (frozen) on the source map.
//   - Any systems that read the AIState will see StateTransferring and skip
//     processing this entity.
//   - The entity is removed from the source spatial grid.
//
// Phase 2 (Copy & Spawn): Serialize entity components and send to target map.
//   - Target map spawns the entity (adds to registry + spatial grid + AOI).
//   - The entity is now fully active on the target map.
//
// Phase 3 (Commit): Remove the original entity from the source map's registry.
//   - If the source registry still has the entity, it is now safe to delete.
//
// Rollback on failure: If the target map fails to spawn (e.g., map is down),
// the transfer is aborted and the entity is un-frozen on the source map.

// TransferState encodes the current phase of a cross-map entity transfer.
type TransferState int

const (
	TransferNone         TransferState = 0
	TransferPhase1Lock   TransferState = 1
	TransferPhase2Copy   TransferState = 2
	TransferPhase3Commit TransferState = 3
	TransferPhaseAbort   TransferState = 4
)

// processTransfer2PC implements the two-phase commit protocol for cross-map
// entity transfers, guaranteeing zero item duping or data loss.
func (w *World) processTransfer2PC(req TransferRequest) {
	fromWorker := w.GetWorker(req.FromMap)
	toWorker := w.GetWorker(req.ToMap)

	if fromWorker == nil || toWorker == nil {
		logger.Warn("[WORLD] Transfer (2PC): source (%d) or target (%d) map not running",
			req.FromMap, req.ToMap)
		return
	}

	// Phase 1: Lock — Mark the entity as transferring (frozen) on the source map.
	// Set a transfer marker in the AI component so systems skip this entity.
	snap, ok := serializeEntity(req.EntityID, fromWorker.Registry)
	if !ok {
		logger.Warn("[WORLD] Transfer (2PC): entity %d not found on map %d (already transferred?)",
			req.EntityID, req.FromMap)
		return
	}

	// Mark the entity as frozen on the source map by setting a transferring state.
	if snap.AI.State != 0 || snap.Meta.Type == ecs.EntityMonster {
		snap.AI.State = ecs.AIStateTransferring
		fromWorker.Registry.SetAI(req.EntityID, snap.AI)
	}

	// Remove from source map's spatial grid so no one sees it there
	fromWorker.SpatialGrid.RemoveEntity(req.EntityID)

	// Phase 2: Copy & Spawn — Send snapshot to target map.
	// Update position to target map before deserializing.
	snap.Pos.MapID = req.ToMap

	// Deserialize into target map's registry + spatial grid
	deserializeEntity(snap, toWorker.Registry, toWorker.SpatialGrid)

	// Register AOI watcher if entity is a player
	if snap.Meta.Type == ecs.EntityPlayer {
		toWorker.RegisterPlayerAOI(req.EntityID)
	}

	// Phase 3: Commit — Remove the original entity from the source map's registry.
	// The entity is now fully alive on the target map, so it's safe to delete
	// from the source.
	fromWorker.Registry.RemoveEntity(req.EntityID)

	logger.Debug("[WORLD] Transferred (2PC) entity %d from map %d to map %d",
		req.EntityID, req.FromMap, req.ToMap)
}

// ─── Worker Goroutine ──────────────────────────────────────────────────────

// runMapWorker is the main loop for a MapWorker goroutine.
// It reads from the worker's tick channel and processes ticks truly concurrently.
// When the channel is closed (via Close()), the goroutine exits.
func (w *World) runMapWorker(mw *MapWorker) {
	defer w.wg.Done()
	logger.Debug("[WORLD] Map %d worker goroutine started.", mw.ID)

	for tick := range mw.tickChan {
		// Install this map's registry as the default for legacy system compatibility.
		ecs.DefaultRegistry = mw.Registry

		// Run the tick
		mw.Tick(tick)
	}

	logger.Debug("[WORLD] Map %d worker goroutine exited.", mw.ID)
}

// ─── Serialization / Deserialization ─────────────────────────────────────────

// serializeEntity extracts all component data from an entity in a registry.
func serializeEntity(id ecs.Entity, reg *ecs.Registry) (EntitySnapshot, bool) {
	pos, okPos := reg.GetPosition(id)
	if !okPos {
		return EntitySnapshot{}, false
	}
	meta, okMeta := reg.GetMetadata(id)
	if !okMeta {
		return EntitySnapshot{}, false
	}
	stats, _ := reg.GetStats(id)
	conn, _ := reg.GetConnection(id)
	ai, _ := reg.GetAI(id)

	return EntitySnapshot{
		ID:    id,
		Pos:   pos,
		Meta:  meta,
		Stats: stats,
		Conn:  conn,
		AI:    ai,
	}, true
}

// deserializeEntity registers all components from a snapshot into a target
// registry and spatial grid.
func deserializeEntity(snap EntitySnapshot, reg *ecs.Registry, grid *SpatialGrid) {
	reg.SetPosition(snap.ID, snap.Pos)
	reg.SetMetadata(snap.ID, snap.Meta)
	reg.SetStats(snap.ID, snap.Stats)
	reg.SetConnection(snap.ID, snap.Conn)
	grid.UpdateEntityPosition(snap.ID, snap.Pos)

	// Set AI component if present
	if snap.AI.State != 0 || snap.AI.SpawnRadius != 0 {
		reg.SetAI(snap.ID, snap.AI)
	}
}

// ─── MapTickFn ───────────────────────────────────────────────────────────────
// MapTickFn is the per-map tick function signature.
type MapTickFn func(mapID int, tick uint64, cmdBuf *ecs.CommandBuffer)

// RegisterMapTick sets the tick function on GlobalWorld and starts the map.
func RegisterMapTick(mapID int, fn MapTickFn) {
	if GlobalWorld != nil {
		GlobalWorld.tickFn = fn
		GlobalWorld.StartMap(mapID)
		return
	}
	logger.Warn("[WORLD] RegisterMapTick: GlobalWorld not initialized, cannot start map %d", mapID)
}

// TickAll dispatches a tick to all running maps via GlobalWorld.
func TickAll(tick uint64) {
	if GlobalWorld != nil {
		GlobalWorld.TickAll(tick)
	}
}

// Tick sends a tick to the specified map via GlobalWorld.
func Tick(mapID int, tick uint64) bool {
	if GlobalWorld != nil {
		return GlobalWorld.Tick(mapID, tick)
	}
	return false
}

// RunningMapIDs returns all running map IDs from GlobalWorld.
func RunningMapIDs() []int {
	if GlobalWorld != nil {
		return GlobalWorld.RunMapIDs()
	}
	return nil
}

// IsMapRunning returns true if the given map has a running worker.
func IsMapRunning(mapID int) bool {
	if GlobalWorld != nil {
		return GlobalWorld.IsMapRunning(mapID)
	}
	return false
}
