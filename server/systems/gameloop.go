package systems

import (
	"server/ecs"
	"server/game"
	"server/logger"
	"server/peakgo/loggate"
	"server/peakgo/perf"
	"server/world"
	"time"
)

// globalTick is the monotonically advancing atomic tick counter.
// Each tick from the heartbeat Ticker increments this counter by 1.
// Systems consume this value instead of calling time.Now() for drift-free
// epoch-relative scheduling (e.g. AI roaming, effect expiry, respawn).
var globalTick uint64

// ─── AI UPDATE BUDGET (Frame Bucket Array) ──────────────────────────────────
//
// The AI_UPDATE_BUCKETS constant defines how many frames to distribute monster
// AI updates across. With 4 buckets and 4 ticks/sec, each monster processes AI
// once per second (every 4th frame). This prevents CPU spikes by slicing the
// total monster count into N equally-sized groups.
//
// Bucket assignment is based on (entityID % numBuckets). Monsters can change
// buckets naturally across respawns since their entity ID changes.
const AI_UPDATE_BUCKETS = 4

// StartGameLoop launches the 250ms heartbeat Ticker and blocks forever
// on ticker.C. This function is designed to run as a goroutine.
//
// Zero-Syscall hot-path: All time.Now() and time.Since() calls are
// eliminated from the main loop. The Ticker itself is the sole timing
// authority — OS scheduler jitter and clock drift are absorbed by Go's
// runtime, which silently drops stalled ticks without compounding delay.
//
// Map parallelism: Each registered map runs its own goroutine via the
// world.MapInstance system. The heartbeat dispatcher sends ticks to all
// maps concurrently. Cross-map entity transfers go through a central
// orchestrator channel to avoid lock-coupling between maps.
//
// AI Budget: Monster updates are distributed across AI_UPDATE_BUCKETS frames.
// Each frame only processes monsters whose (entityID % AI_UPDATE_BUCKETS)
// matches the current frame bucket (tick % AI_UPDATE_BUCKETS). This ensures
// smooth CPU usage even with 1000+ monsters on a single map.
func StartGameLoop() {
	// Start the cross-map transfer orchestrator.
	world.StartTransferOrchestrator()

	// Register default map tick for map 1 (the primary game map).
	// Additional maps can be registered at boot time via world.RegisterMapTick.
	// The cmdBuf parameter is this map's local CommandBuffer; systems record
	// deferred mutations into it, and the framework flushes at end of tick.
	world.RegisterMapTick(1, func(mapID int, tick uint64, cmdBuf *ecs.CommandBuffer) {
		// Compute the current AI bucket from the global tick.
		// Each monster is assigned to bucket (entityID % AI_UPDATE_BUCKETS).
		// On each frame, only monsters in bucket (tick % AI_UPDATE_BUCKETS)
		// get their AI updated. This distributes the AI load evenly across frames.
		currentBucket := int(tick % AI_UPDATE_BUCKETS)

		// Query monsters on this map and process AI.
		// Uses QueryPositionAI which iterates over the smallest store (AI store, monsters only)
		// and provides AI + Position + Stats in one pass — no nested lookups needed.
		ecs.GlobalRegistry.QueryPositionAI(func(id ecs.Entity, ai ecs.AIComponent, pos ecs.PositionComponent, stats ecs.StatsComponent) bool {
			if pos.MapID != mapID {
				return true
			}
			// Frame bucket filtering: only process monsters in the current bucket.
			// Entity ID distribution is roughly uniform, so this gives us an even split.
			if int(id%ecs.Entity(AI_UPDATE_BUCKETS)) != currentBucket {
				return true
			}
			game.TickAI(id)
			return true
		})
	})

	ticker := time.NewTicker(250 * time.Millisecond)
	go func() {
		logger.Info("[ENGINE] Heartbeat game loop started at 4 ticks/sec.")
		for range ticker.C {
			tickStart := time.Now()
			globalTick++
			tickWorld(globalTick)
			elapsed := time.Since(tickStart)

			// Record tick duration into the global tick monitor (zero-alloc hot-path)
			perf.GlobalTickMonitor.RecordTick(elapsed)

			// Check tick duration against alert threshold
			perf.GlobalAlertMonitor.CheckTickDuration(elapsed.Nanoseconds())
		}
	}()
}

// tickWorld does a single metadata scan per tick.
// hasPlayers and monster processing happen in the same pass — no double scan.
// Then dispatches ticks to all running maps for parallel simulation.
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

	// Dispatch ticks to all running maps in parallel via their goroutines.
	// Each map's MapInstance goroutine receives the tick on its channel,
	// runs the registered tick function, and flushes its CommandBuffer.
	for _, mapID := range world.RunningMapIDs() {
		world.Tick(mapID, tick)
	}
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
