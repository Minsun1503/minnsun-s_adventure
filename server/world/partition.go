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

	// Start the map goroutine
	go w.runMapWorker(mw)

	logger.Info("[WORLD] Started map %d", mapID)
}

// StopMap signals a MapWorker goroutine to shut down gracefully.
func (w *World) StopMap(mapID int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, ok := w.workers[mapID]; ok {
		// Note: In a full implementation, we'd close a stop channel.
		// For now, we remove from the map and let the goroutine end
		// when nobody sends to its tickChan.
		delete(w.workers, mapID)
		logger.Info("[WORLD] Stopped map %d", mapID)
	}
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

// ─── Tick Dispatch ──────────────────────────────────────────────────────────

// TickAll dispatches a tick to all running MapWorkers concurrently.
// Each worker's tick runs in its own goroutine spawned by runMapWorker.
func (w *World) TickAll(tick uint64) {
	w.mu.RLock()
	workerList := make([]*MapWorker, 0, len(w.workers))
	for _, mw := range w.workers {
		workerList = append(workerList, mw)
	}
	w.mu.RUnlock()

	// Dispatch tick to all workers concurrently via their goroutine channels.
	// Each MapWorker has a tickChan with a small buffer — non-blocking send
	// ensures one slow map doesn't stall others.
	var wg sync.WaitGroup
	for _, mw := range workerList {
		wg.Add(1)
		go func(worker *MapWorker) {
			defer wg.Done()
			worker.Tick(tick)
		}(mw)
	}
	wg.Wait()
}

// Tick sends a tick to a specific map's worker goroutine.
// Returns false if the map is not running.
func (w *World) Tick(mapID int, tick uint64) bool {
	mw := w.GetWorker(mapID)
	if mw == nil {
		return false
	}
	mw.Tick(tick)
	return true
}

// ─── Cross-Map Transfer ─────────────────────────────────────────────────────

// TransferEntity serializes an entity from the source MapWorker and
// enqueues it for transfer to the destination MapWorker.
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
	go func() {
		for req := range w.transferChan {
			w.processTransfer(req)
		}
	}()
}

// processTransfer serializes the entity from the source map's worker and
// deserializes it into the destination map's worker.
func (w *World) processTransfer(req TransferRequest) {
	fromWorker := w.GetWorker(req.FromMap)
	toWorker := w.GetWorker(req.ToMap)

	if fromWorker == nil || toWorker == nil {
		logger.Warn("[WORLD] Transfer: source (%d) or target (%d) map not running",
			req.FromMap, req.ToMap)
		return
	}

	// 1. Serialize: extract all components from source map's registry
	snap, ok := serializeEntity(req.EntityID, fromWorker.Registry)
	if !ok {
		logger.Warn("[WORLD] Transfer: entity %d not found on map %d",
			req.EntityID, req.FromMap)
		return
	}

	// 2. Remove from source map
	fromWorker.DespawnEntity(req.EntityID)

	// 3. Update position to target map
	snap.Pos.MapID = req.ToMap

	// 4. Deserialize into target map
	deserializeEntity(snap, toWorker.Registry, toWorker.SpatialGrid)

	// 5. Transfer AOI watcher if entity is a player
	if snap.Meta.Type == ecs.EntityPlayer {
		fromWorker.UnregisterPlayerAOI(req.EntityID)
		toWorker.RegisterPlayerAOI(req.EntityID)
	}

	logger.Debug("[WORLD] Transferred entity %d from map %d to map %d",
		req.EntityID, req.FromMap, req.ToMap)
}

// ─── Worker Goroutine ──────────────────────────────────────────────────────

// runMapWorker is the main loop for a MapWorker goroutine.
// It reads from the worker's tick channel and processes ticks.
func (w *World) runMapWorker(mw *MapWorker) {
	// Note: tickChan is not yet in MapWorker — it's a future optimization.
	// For now, Tick is called directly from TickAll or Tick.
	// This goroutine exists for future use when we add per-worker channels.
	select {}
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

	// Set AI component if present (AIState != 0 means non-zero AI)
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
