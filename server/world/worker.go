package world

import (
	"server/ecs"
	"server/logger"
	"server/peakgo/aoi"
)

// ─── MapWorker ──────────────────────────────────────────────────────────────
//
// MapWorker is an isolated game map with its own ECS Registry, SpatialGrid,
// AOI manager, and CommandBuffer. Each MapWorker runs its own tick loop,
// enabling true multi-core parallel simulation across maps.
//
// Design decisions:
//   - Each MapWorker owns a full ecs.Registry (not a subset) to keep the
//     codebase simple and avoid partial-store bugs. The trade-off is slightly
//     more memory per map, which is negligible for an MMORPG (typically <10
//     maps). The benefit is that all ECS query functions (QueryPositionAI,
//     QueryPositionStats, etc.) work identically on per-map registries.
//   - The central World holds the global entity ID (nextID) counter.
//     Entity IDs are unique across all maps — a monster on Map 1 will never
//     collide with a player on Map 2.
//   - Cross-map entity transfer uses serialization/deserialization of
//     component snapshots pushed through a central orchestrator channel.
//   - No archetype ECS, no microservices — just N isolated registries
//     running on N goroutines with a shared orchestrator.

// MapWorker holds the per-map ECS state and systems.
type MapWorker struct {
	ID int

	// Registry is this map's ECS component store. All entities on this map
	// are registered here. The central World's nextID counter ensures global
	// uniqueness of entity IDs across all maps.
	Registry *ecs.Registry

	// SpatialGrid is this map's spatial partition for proximity queries.
	SpatialGrid *SpatialGrid

	// AOIManager tracks watcher neighborhoods for this map.
	AOIManager *aoi.AOIManager

	// CmdBuf is this map's command buffer for deferred ECS mutations.
	CmdBuf *ecs.CommandBuffer

	// tickFn is the main simulation function for this map, registered at boot.
	tickFn MapTickFn

	// activeRegions tracks which spatial regions are currently active.
	// An empty region has its systems suspended to save CPU.
	activeRegions map[ChunkKey]bool

	// regionWakeBuffer buffers entities that entered an inactive region
	// during the tick, so the region can be woken on the next cycle.
	regionWakeBuffer []ChunkKey
}

// NewMapWorker creates a new MapWorker with fresh ECS, spatial, and AOI state.
func NewMapWorker(mapID int, fn MapTickFn) *MapWorker {
	mw := &MapWorker{
		ID:               mapID,
		Registry:         ecs.NewRegistry(),
		SpatialGrid:      newSpatialGrid(),
		AOIManager:       aoi.NewAOIManager(),
		CmdBuf:           ecs.NewCommandBuffer(),
		tickFn:           fn,
		activeRegions:    make(map[ChunkKey]bool),
		regionWakeBuffer: make([]ChunkKey, 0, 8),
	}
	return mw
}

// ─── Per-Map AOI Operations ─────────────────────────────────────────────────

// RegisterPlayerAOI registers a player entity as an AOI watcher on this map.
func (mw *MapWorker) RegisterPlayerAOI(entity ecs.Entity) {
	mw.AOIManager.RegisterWatcher(entity, WatcherRadius)
}

// UnregisterPlayerAOI removes a player from this map's AOI watcher set.
func (mw *MapWorker) UnregisterPlayerAOI(entity ecs.Entity) {
	mw.AOIManager.UnregisterWatcher(entity)
}

// ProcessAOIEvents updates the AOI watcher for a single entity on this map,
// producing enter/leave events and sending corresponding packets.
func (mw *MapWorker) ProcessAOIEvents(entity ecs.Entity, pos ecs.PositionComponent) {
	eventsPtr := mw.AOIManager.UpdateOne(entity, pos, func(origin ecs.PositionComponent, worldRadius float64, excludeID ecs.Entity) *[]ecs.Entity {
		return aoiSpatialQueryFromGrid(mw.SpatialGrid, origin, worldRadius, excludeID)
	})
	if eventsPtr == nil || len(*eventsPtr) == 0 {
		if eventsPtr != nil {
			aoi.AOIEventPool.Put(eventsPtr)
		}
		return
	}
	defer aoi.AOIEventPool.Put(eventsPtr)

	// Get the watcher's connection (only players have connections)
	watcherConn, hasConn := mw.Registry.GetConnection(entity)
	if !hasConn || watcherConn.Conn == nil {
		return
	}

	for _, ev := range *eventsPtr {
		switch ev.Type {
		case aoi.EventEnter:
			sendSpawnToFrom(watcherConn.Conn, ev.Target, mw.Registry)
		case aoi.EventLeave:
			sendDespawnTo(watcherConn.Conn, ev.Target)
		}
	}
}

// ─── Entity Lifecycle ────────────────────────────────────────────────────────

// SpawnEntity creates an entity on this map with the given components.
// The entity ID is pre-allocated (typically via World.AllocateEntityID).
func (mw *MapWorker) SpawnEntity(id ecs.Entity, pos ecs.PositionComponent, meta ecs.MetadataComponent, stats ecs.StatsComponent) {
	mw.Registry.SetPosition(id, pos)
	mw.Registry.SetMetadata(id, meta)
	mw.Registry.SetStats(id, stats)
	mw.SpatialGrid.UpdateEntityPosition(id, pos)
}

// DespawnEntity removes an entity from this map and recycles its ID.
func (mw *MapWorker) DespawnEntity(id ecs.Entity) {
	mw.SpatialGrid.RemoveEntity(id)
	mw.Registry.RemoveEntity(id)
}

// GetEntityCount returns the number of active entities on this map.
func (mw *MapWorker) GetEntityCount() int {
	return len(mw.Registry.GetAllEntities())
}

// ─── Region Suspension ───────────────────────────────────────────────────────

// ActivateRegion marks a chunk region as active (systems will run).
func (mw *MapWorker) ActivateRegion(key ChunkKey) {
	if !mw.activeRegions[key] {
		mw.activeRegions[key] = true
		logger.Debug("[MAP %d] Region (%d,%d) activated", mw.ID, key.X, key.Z)
	}
}

// DeactivateRegion marks a chunk region as inactive (systems will be skipped).
func (mw *MapWorker) DeactivateRegion(key ChunkKey) {
	if mw.activeRegions[key] {
		delete(mw.activeRegions, key)
		logger.Debug("[MAP %d] Region (%d,%d) deactivated", mw.ID, key.X, key.Z)
	}
}

// IsRegionActive returns true if the chunk region is active.
func (mw *MapWorker) IsRegionActive(key ChunkKey) bool {
	return mw.activeRegions[key]
}

// WakeRegionForEntity buffers a chunk key for activation on the next tick.
// Called when a player/monster enters a previously empty chunk.
func (mw *MapWorker) WakeRegionForEntity(pos ecs.PositionComponent) {
	key := worldToChunk(pos)
	if !mw.activeRegions[key] {
		mw.regionWakeBuffer = append(mw.regionWakeBuffer, key)
	}
}

// FlushRegionWakeBuffer activates all pending regions from the buffer.
func (mw *MapWorker) FlushRegionWakeBuffer() {
	for _, key := range mw.regionWakeBuffer {
		mw.ActivateRegion(key)
	}
	mw.regionWakeBuffer = mw.regionWakeBuffer[:0]
}

// SweepRegions deactivates regions that have no entities.
// Called periodically (not every tick) to avoid useless scanning.
func (mw *MapWorker) SweepRegions() {
	// Collect regions that still have entities
	populated := make(map[ChunkKey]bool)
	mw.SpatialGrid.ForEachEntityChunk(func(id ecs.Entity, key ChunkKey) bool {
		populated[key] = true
		return true
	})

	// Deactivate regions that are no longer populated
	for key := range mw.activeRegions {
		if !populated[key] {
			mw.DeactivateRegion(key)
		}
	}
}

// ─── Worker Lifecycle ────────────────────────────────────────────────────────

// Tick runs one simulation tick on this map worker.
// It calls the registered tick function with per-map state, then flushes
// the command buffer to this map's own registry and spatial grid.
func (mw *MapWorker) Tick(tick uint64) {
	// Run the registered tick function
	mw.tickFn(mw.ID, tick, mw.CmdBuf)

	// Flush commands to this map's own registry and spatial grid
	mw.CmdBuf.Flush(mw.SpatialGrid)

	// Process region wake buffer
	mw.FlushRegionWakeBuffer()
}

// Free releases pooled resources held by this worker.
func (mw *MapWorker) Free() {
	mw.CmdBuf.Free()
}
