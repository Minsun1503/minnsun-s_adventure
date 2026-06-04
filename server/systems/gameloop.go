package systems

import (
	"server/ecs"
	"server/game"
	"server/logger"
	"server/peakgo/loggate"
	"server/world"
	"time"
)

// globalTick is the monotonically advancing atomic tick counter.
// Each tick from the heartbeat Ticker increments this counter by 1.
// Systems consume this value instead of calling time.Now() for drift-free
// epoch-relative scheduling (e.g. AI roaming, effect expiry, respawn).
var globalTick uint64

// StartGameLoop launches the 250ms heartbeat Ticker and blocks forever
// on ticker.C. This function is designed to run as a goroutine.
//
// Zero-Syscall hot-path: All time.Now() and time.Since() calls are
// eliminated from the main loop. The Ticker itself is the sole timing
// authority — OS scheduler jitter and clock drift are absorbed by Go's
// runtime, which silently drops stalled ticks without compounding delay.
func StartGameLoop() {
	// Register default map tick for map 1 (the primary game map).
	// Additional maps can be registered at boot time via world.RegisterMapTick.
	world.RegisterMapTick(1, func(mapID int, tick uint64) {
		// Query entities on this map and process AI.
		ecs.GlobalRegistry.QueryPositionStats(func(id ecs.Entity, pos ecs.PositionComponent, stats ecs.StatsComponent) bool {
			if pos.MapID != mapID {
				return true
			}
			if meta, ok := ecs.GlobalRegistry.GetMetadata(id); ok && meta.Type == ecs.EntityMonster {
				game.TickAI(id)
			}
			return true
		})
	})

	ticker := time.NewTicker(250 * time.Millisecond)
	go func() {
		logger.Info("[ENGINE] Heartbeat game loop started at 4 ticks/sec.")
		for range ticker.C {
			globalTick++
			tickWorld(globalTick)
		}
	}()
}

// tickWorld does a single metadata scan per tick.
// hasPlayers and monster processing happen in the same pass — no double scan.
func tickWorld(tick uint64) {
	hasPlayers := false

	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		switch snap.Meta.Type {
		case ecs.EntityPlayer:
			hasPlayers = true
		case ecs.EntityMonster:
			debugLogMonsterState(snap)
		}
		return true
	})

	// Process floor items lifecycle expiration countdown
	game.RunGroundItemDecaySystem()

	// Process monster respawn queue
	game.GlobalRespawnManager.RunRespawnSystem()

	// Purge expired party invitations to prevent memory leaks.
	game.GlobalInviteCache.PurgeExpired()

	// Process active status effects, buffs, and DoTs
	game.RunStatusEffectsSystem()

	// Only run AI ticks when at least one player is online.
	// MAP SLEEP TRICK: zero players → no AI computation at all.
	if !hasPlayers {
		return
	}

	// Dispatch per-map ticks using the world partition system.
	// Registered map tick functions handle their own entity filtering.
	// Currently only map 1 is registered; future maps will be added
	// via world.RegisterMapTick at boot time.
	world.TickMap(1, tick)
}

// debugLogMonsterState logs active monster state at DEBUG level only.
// In production (debug=false), loggate.Debugf is a guaranteed no-op.
func debugLogMonsterState(snap ecs.EntitySnapshot) {
	if !snap.HasPos || !snap.HasStats {
		return
	}
	loggate.Debugf("[MONITOR] ID: %d | %s | Pos: (%d, %d) | HP: %d",
		snap.ID, snap.Meta.Name, snap.Pos.X, snap.Pos.Z, snap.Stats.HP)
}

// UpdateWorldEntitiesSystem — kept for external callers, now zero double-lookup.
func UpdateWorldEntitiesSystem() {
	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		if snap.Meta.Type != ecs.EntityMonster || !snap.HasPos || !snap.HasStats {
			return true
		}
		debugLogMonsterState(snap)
		return true
	})

	// Process floor items lifecycle expiration countdown
	game.RunGroundItemDecaySystem()
}

// CurrentTick returns the global monotonic tick counter.
// Subsystems can compare saved tick values against this to implement
// timer-free, drift-free expiration logic (e.g. "is 5 ticks have passed").
func CurrentTick() uint64 {
	return globalTick
}
