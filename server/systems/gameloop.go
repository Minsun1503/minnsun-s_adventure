package systems

import (
	"server/ecs"
	"server/game"
	"server/logger"
	"server/peakgo/loggate"
	"time"
)

func StartGameLoop() {
	ticker := time.NewTicker(250 * time.Millisecond)
	go func() {
		logger.Info("[ENGINE] Heartbeat game loop started at 4 ticks/sec.")
		for range ticker.C {
			start := time.Now()
			tickWorld()
			if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
				logger.Warn("[PERF] Game loop tick overran: %v (budget: 50ms)", elapsed)
			}
		}
	}()
}

// tickWorld does a single metadata scan per tick.
// hasPlayers and monster processing happen in the same pass — no double scan.
func tickWorld() {
	hasPlayers := false

	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		switch snap.Meta.Type {
		case "player":
			hasPlayers = true
		case "monster":
			processMonster(snap)
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

// processMonster logs active monster state at DEBUG level only.
// In production (debug=false), loggate.Debugf is a guaranteed no-op.
func processMonster(snap ecs.EntitySnapshot) {
	if !snap.HasPos || !snap.HasStats {
		return
	}
	loggate.Debugf("[MONITOR] ID: %d | %s | Pos: (%d, %d) | HP: %d",
		snap.ID, snap.Meta.Name, snap.Pos.X, snap.Pos.Z, snap.Stats.HP)
}

// UpdateWorldEntitiesSystem — kept for external callers, now zero double-lookup.
func UpdateWorldEntitiesSystem() {
	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		if snap.Meta.Type != "monster" || !snap.HasPos || !snap.HasStats {
			return true
		}
		processMonster(snap)
		return true
	})

	// Process floor items lifecycle expiration countdown
	game.RunGroundItemDecaySystem()
}
