package systems

import (
	"server/ecs"
	"server/game"
	"server/logger"
	"server/peakgo/loggate"
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

	// Tick every monster's AI state machine.
	// RangeAI is separate from RangeSnapshots to avoid a second
	// metadata scan — AI and snapshot passes are decoupled by design.
	ecs.GlobalRegistry.RangeAI(func(id ecs.Entity, _ ecs.AIComponent) bool {
		game.TickAI(id)
		return true
	})
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
