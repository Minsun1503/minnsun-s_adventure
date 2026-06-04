package world

import (
	"server/logger"
)

// ─── MapInstance ──────────────────────────────────────────────────────────────
//
// MapInstance represents an isolated game map with its own spatial grid,
// entity population, and per-tick processing. Each map runs its own tick
// goroutine, enabling parallel simulation for multi-map servers.
//
// Current implementation wraps the global tick loop (single-threaded main
// loop) and routes entities by MapID. Future work: break out per-map
// goroutines for truly parallel map simulation.

// MapTickFn is the per-map tick function signature.
type MapTickFn func(mapID int, tick uint64)

// mapTickFns stores per-map tick functions registered at boot time.
// Indexed by MapID. Only maps with a registered function receive ticks.
var mapTickFns = make(map[int]MapTickFn)

// RegisterMapTick registers a tick function for the given map ID.
// Called during server boot from systems.StartGameLoop.
// If no function is registered for a map, the map is considered idle
// and receives no tick processing.
func RegisterMapTick(mapID int, fn MapTickFn) {
	mapTickFns[mapID] = fn
	logger.Info("[WORLD] Registered tick function for map %d", mapID)
}

// TickMap calls the registered tick function for the given map.
// Returns immediately if no function is registered (map is idle).
func TickMap(mapID int, tick uint64) {
	if fn, ok := mapTickFns[mapID]; ok {
		fn(mapID, tick)
	}
}

// ─── Future: Parallel Map Ticks ──────────────────────────────────────────────
//
// To enable parallel per-map ticks, replace the global systems.StartGameLoop()
// with N goroutines, one per MapInstance:
//
//	type MapInstance struct {
//	    ID          int
//	    Grid        *SpatialGrid
//	    tickChannel chan uint64
//	}
//
// Each MapInstance runs in its own goroutine, consuming ticks from a channel
// or Ticker. The world package provides the routing infrastructure; game
// systems register MapTickFn callbacks at boot time.
