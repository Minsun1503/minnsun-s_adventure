package world

import (
	"server/ecs"
	"server/logger"
	"server/peakgo/loggate"
)

// ─── Region Streaming ────────────────────────────────────────────────────────
//
// Region Streaming optimizes large worlds by only activating systems for loaded
// regions (chunks) that have active players or monsters. Empty regions have their
// systems suspended to save CPU cycles.
//
// Design:
//   - Each MapWorker tracks which regions (chunks) are active via activeRegions map.
//   - When a player enters a chunk, the region is activated via WakeRegionForEntity.
//   - When a region becomes empty (all entities leave), SweepRegions deactivates it.
//   - A periodic "sweep" runs every N ticks to garbage-collect inactive regions.
//   - AI bucket filtering is done independently (world and systems share a constant).
//
// The region sweep is done by the MapWorker's SweepRegions method, which is
// called periodically by the map tick function.

// RegionSweepInterval defines how many ticks between region sweep passes.
// Default: every 10 ticks = every 2.5 seconds at 4 ticks/sec.
const RegionSweepInterval = 10

// AIUpdateBuckets defines how many frames to distribute monster AI updates across.
// Duplicated here to avoid circular import with systems package.
// Must be kept in sync with systems.AI_UPDATE_BUCKETS.
const AIUpdateBuckets = 4

// RegionStreamingTick is called by the map tick function to perform region
// streaming maintenance. It sweeps inactive regions and wakes regions for new
// entities.
//
// This function should be called at the end of each map tick.
func RegionStreamingTick(mw *MapWorker, tick uint64) {
	if tick%RegionSweepInterval == 0 {
		mw.SweepRegions()
	}
}

// ─── Region-Aware System Helpers ─────────────────────────────────────────────
// These helper functions let external systems (e.g., combat, movement) check
// if a position is in an active region before processing.

// ActiveRegionCount returns the number of active regions in this map worker.
func ActiveRegionCount(mw *MapWorker) int {
	if mw == nil {
		return 0
	}
	return len(mw.activeRegions)
}

// IsEntityInActiveRegion checks if an entity's position is in an active region
// on its map. Returns true if the region is active or if region streaming is
// not configured (default: process all entities).
func IsEntityInActiveRegion(mw *MapWorker, pos ecs.PositionComponent) bool {
	if mw == nil {
		return true // no map worker = process everything
	}
	key := worldToChunk(pos)
	return mw.IsRegionActive(key)
}

// WakeRegionFromPosition wakes the region for a given position if it's inactive.
// Called by movement systems when an entity moves into a new chunk.
func WakeRegionFromPosition(mw *MapWorker, pos ecs.PositionComponent) {
	if mw == nil {
		return
	}
	mw.WakeRegionForEntity(pos)
}

// LogRegionStats logs a summary of region activity for a map worker.
func LogRegionStats(mw *MapWorker) {
	if mw == nil || !loggate.DebugEnabled() {
		return
	}
	activeCount := len(mw.activeRegions)
	totalEntities := mw.GetEntityCount()
	logger.Debug("[REGION] Map %d: %d active regions, %d entities",
		mw.ID, activeCount, totalEntities)
}
