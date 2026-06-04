package world

import (
	"server/ecs"
	"server/logger"
)

// ─── Cross-Map Entity Transfer ──────────────────────────────────────────────

// TransferRequest represents a request to move an entity from one map to another.
// Pushed to the central orchestrator channel by the source MapInstance.
type TransferRequest struct {
	EntityID ecs.Entity
	FromMap  int
	ToMap    int
}

// ─── MapInstance ──────────────────────────────────────────────────────────────
//
// MapInstance represents an isolated game map with its own spatial grid,
// entity population, command buffer, and per-tick processing.
// Each map runs its own tick goroutine, enabling parallel simulation for
// multi-map servers.
//
// Cross-map entity transfers use a central orchestrator channel to avoid
// direct lock-coupling and deadlocks between maps. When an entity leaves
// Map A for Map B, Map A pushes a TransferRequest to the orchestrator,
// which processes the transfer outside any map's tick.

// MapInstance holds the per-map state and runs its own tick goroutine.
type MapInstance struct {
	ID int

	// CmdBuf is this map's command buffer for deferred ECS mutations.
	// All systems on this map record commands here; Flush is called at
	// the end of each tick.
	CmdBuf *ecs.CommandBuffer

	// tickFn is the main simulation function for this map, registered at boot.
	tickFn MapTickFn

	// tickChan receives tick signals from the heartbeat dispatcher.
	tickChan chan uint64

	// stop signals the goroutine to shut down cleanly.
	stop chan struct{}
}

// MapTickFn is the per-map tick function signature.
type MapTickFn func(mapID int, tick uint64, cmdBuf *ecs.CommandBuffer)

// ─── Global State ────────────────────────────────────────────────────────────

// instances holds all active map instances, indexed by MapID.
var instances = make(map[int]*MapInstance)

// transferChan is the central orchestrator channel for cross-map entity transfers.
// Maps push TransferRequest into this channel; the orchestrator goroutine
// processes them outside any map's tick to avoid deadlocks.
var transferChan = make(chan TransferRequest, 256)

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// StartInstance launches a MapInstance in its own goroutine.
// It registers the tick function and starts consuming ticks from tickChan.
// The goroutine runs until StopInstance is called.
func StartInstance(mapID int, fn MapTickFn) {
	inst := &MapInstance{
		ID:       mapID,
		CmdBuf:   ecs.NewCommandBuffer(),
		tickFn:   fn,
		tickChan: make(chan uint64, 4), // small buffer absorbs tick jitter
		stop:     make(chan struct{}),
	}
	instances[mapID] = inst
	logger.Info("[WORLD] Started map instance %d", mapID)

	go inst.run()
}

// StopInstance signals a MapInstance goroutine to shut down gracefully.
func StopInstance(mapID int) {
	if inst, ok := instances[mapID]; ok {
		close(inst.stop)
		delete(instances, mapID)
		logger.Info("[WORLD] Stopped map instance %d", mapID)
	}
}

// run is the main loop for a MapInstance goroutine.
// It consumes ticks from tickChan, runs the tick function, flushes the
// command buffer, and repeats until stopped.
func (m *MapInstance) run() {
	for {
		select {
		case <-m.stop:
			// Free pooled slices before exiting
			m.CmdBuf.Free()
			return
		case tick := <-m.tickChan:
			// Run the registered tick function with this map's command buffer
			m.tickFn(m.ID, tick, m.CmdBuf)

			// Flush all deferred commands to the ECS registry and spatial grid
			m.CmdBuf.Flush(GlobalSpatialGrid)
		}
	}
}

// Tick sends a tick signal to the MapInstance goroutine.
// Returns false if the map is not running.
func Tick(mapID int, tick uint64) bool {
	if inst, ok := instances[mapID]; ok {
		select {
		case inst.tickChan <- tick:
		default:
			// Channel full — drop tick silently (map is overloaded).
			// Go's runtime silently skips stalled ticks on the Ticker side,
			// so this naturally backpressures without compounding delay.
			logger.Warn("[WORLD] Map %d tick channel full, dropping tick %d", mapID, tick)
		}
		return true
	}
	return false
}

// ─── Map Query ───────────────────────────────────────────────────────────────

// IsMapRunning returns true if the given map ID has a running instance.
func IsMapRunning(mapID int) bool {
	_, ok := instances[mapID]
	return ok
}

// RunningMapIDs returns a slice of all currently running map IDs.
func RunningMapIDs() []int {
	ids := make([]int, 0, len(instances))
	for id := range instances {
		ids = append(ids, id)
	}
	return ids
}

// ─── Cross-Map Transfer ──────────────────────────────────────────────────────

// RequestTransfer enqueues a cross-map entity transfer.
// Called by a MapInstance during its tick when an entity moves to another map.
// The transfer is processed asynchronously by the orchestrator.
func RequestTransfer(entityID ecs.Entity, fromMap, toMap int) {
	transferChan <- TransferRequest{
		EntityID: entityID,
		FromMap:  fromMap,
		ToMap:    toMap,
	}
}

// StartTransferOrchestrator launches the goroutine that processes cross-map
// entity transfers. Must be called once at server boot.
//
// The orchestrator reads TransferRequest from the central channel and
// updates the entity's MapID in the ECS registry. Since it runs in its own
// goroutine, it avoids any lock-coupling with the map tick loops.
func StartTransferOrchestrator() {
	logger.Info("[WORLD] Cross-map transfer orchestrator started")
	go func() {
		for req := range transferChan {
			processTransfer(req)
		}
	}()
}

// processTransfer updates the entity's MapID in the ECS registry.
// The entity's PositionComponent is updated to reflect the new map,
// and the spatial grid is notified of the position change.
func processTransfer(req TransferRequest) {
	pos, ok := ecs.GlobalRegistry.GetPosition(req.EntityID)
	if !ok {
		logger.Warn("[WORLD] Transfer: entity %d not found (from map %d to %d)",
			req.EntityID, req.FromMap, req.ToMap)
		return
	}
	pos.MapID = req.ToMap
	ecs.GlobalRegistry.SetPosition(req.EntityID, pos)
	GlobalSpatialGrid.UpdateEntityPosition(req.EntityID, pos)
	logger.Debug("[WORLD] Transferred entity %d from map %d to map %d",
		req.EntityID, req.FromMap, req.ToMap)
}

// ─── Legacy Compatibility ────────────────────────────────────────────────────
//
// The following functions maintain backward compatibility with the old
// single-threaded tick dispatch model. They delegate to the new MapInstance
// system internally.
//
// These will be removed in a future cleanup pass once all callers are migrated.

// RegisterMapTick registers a tick function for the given map ID and starts
// the map instance's goroutine. This is the boot-time entry point used by
// systems.StartGameLoop.
//
// Old signature: func(mapID int, tick uint64)
// New signature: func(mapID int, tick uint64, cmdBuf *ecs.CommandBuffer)
func RegisterMapTick(mapID int, fn MapTickFn) {
	StartInstance(mapID, fn)
}

// TickMap sends a tick to the given map ID via the new MapInstance system.
// Returns immediately if the map is not running (idle).
//
// Deprecated: use world.Tick(mapID, tick) directly.
func TickMap(mapID int, tick uint64) {
	Tick(mapID, tick)
}
