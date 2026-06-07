// Package world provides the thin bridge between the world package internals
// and the peakgo/spatial grid engine.
//
// This file provides backward-compatible type aliases and utility functions
// so that existing systems in the world package continue to compile
// without modification. The actual spatial engine lives in peakgo/spatial.
package world

import (
	"math"

	"server/ecs"
	"server/peakgo/spatial"
)

// SpatialGrid is an alias for spatial.SpatialGrid.
// The concrete type has moved to peakgo/spatial/grid.go.
type SpatialGrid = spatial.SpatialGrid

// ChunkKey is an alias for spatial.ChunkKey.
type ChunkKey = spatial.ChunkKey

// ChunkEntry is an alias for spatial.ChunkEntry.
type ChunkEntry = spatial.ChunkEntry

// FreeQueryCandidates delegates to spatial.FreeQueryCandidates.
func FreeQueryCandidates(s *[]ChunkEntry) {
	spatial.FreeQueryCandidates(s)
}

// GlobalSpatialGrid is the singleton spatial registry used by all systems.
// It is an alias for spatial.GlobalGrid to maintain backward compatibility.
var GlobalSpatialGrid = spatial.GlobalGrid

// WatcherRadius is the visibility radius (game units) used when registering watchers.
const WatcherRadius = 60.0

// newSpatialGrid creates a new SpatialGrid instance (delegates to spatial.NewGrid).
func newSpatialGrid() *SpatialGrid {
	return spatial.NewGrid()
}

// worldToChunk converts a world-space position into its ChunkKey (unexported helper).
func worldToChunk(pos ecs.PositionComponent) ChunkKey {
	return ChunkKey{
		MapID: pos.MapID,
		X:     int(math.Floor(float64(pos.X) / 20)),
		Z:     int(math.Floor(float64(pos.Z) / 20)),
	}
}

// ─── Bridge Functions for game package compatibility ─────────────────────────
//
// These thin wrappers delegate to the corresponding functions in peakgo/spatial
// so that existing game code (combat.go, movement.go, pickup.go, skill_pipeline.go)
// continues to compile without import changes.

// IsInRange delegates to spatial.IsInRange.
func IsInRange(originID, targetID ecs.Entity, worldRadius float64) bool {
	return spatial.IsInRange(originID, targetID, worldRadius)
}

func ProcessAOIEvents(entity ecs.Entity, pos ecs.PositionComponent) {
	mw := GlobalWorld.GetWorker(pos.MapID)
	if mw != nil {
		mw.ProcessAOIEvents(entity, pos)
	}
}

// RegisterPlayerAOI registers a player entity as an AOI watcher on the global AOI manager.
func RegisterPlayerAOI(entity ecs.Entity) {
	if pos, ok := ecs.DefaultRegistry.GetPosition(entity); ok {
		if mw := GlobalWorld.GetWorker(pos.MapID); mw != nil {
			mw.RegisterPlayerAOI(entity)
		}
	}
}

// UnregisterPlayerAOI removes a player from the global AOI watcher set.
func UnregisterPlayerAOI(entity ecs.Entity) {
	if pos, ok := ecs.DefaultRegistry.GetPosition(entity); ok {
		if mw := GlobalWorld.GetWorker(pos.MapID); mw != nil {
			mw.UnregisterPlayerAOI(entity)
		}
	}
}

// InitAOIManager is a no-op stub that replaces the deleted GlobalAOIManager setup.
// AOI managers are now created per-map in NewMapWorker.
// This function is kept for backward compatibility with server.go boot sequence.
func InitAOIManager() {
	// No-op: AOI managers are now created per-map in NewMapWorker.
}
