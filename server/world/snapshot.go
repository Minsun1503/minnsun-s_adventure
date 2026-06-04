package world

import (
	"server/db"
	"server/ecs"
	"server/logger"
	"time"
)

// SnapshotInterval is how often the periodic world snapshot runs.
// Default: 5 minutes (300 seconds).
const SnapshotInterval = 5 * time.Minute

// StartPeriodicSnapshot launches a background goroutine that runs
// periodic world snapshots. It serializes all active entity state
// (players, monsters, etc.) to the persistence layer through the
// existing SaveQueue mechanism.
//
// Snapshotting every entity is expensive — this runs on a 5-minute
// ticker and is NOT a hot-path operation. It ensures that in the
// event of a crash, the world can be recovered to a state no older
// than SnapshotInterval.
//
// If the previous snapshot cycle is still running when the next
// tick arrives, the new tick is skipped to avoid piling up.
func StartPeriodicSnapshot() {
	go func() {
		logger.Info("[SNAPSHOT] Periodic world snapshot started (interval: %v).", SnapshotInterval)

		ticker := time.NewTicker(SnapshotInterval)
		defer ticker.Stop()

		// Guard to prevent overlapping snapshot cycles.
		var busy bool

		for range ticker.C {
			if busy {
				logger.Warn("[SNAPSHOT] Previous snapshot still in progress — skipping this cycle.")
				continue
			}
			busy = true
			takeWorldSnapshot()
			busy = false
		}
	}()
}

// takeWorldSnapshot iterates all active entities in the ECS registry
// and queues each one for persistence via the existing SaveQueue.
//
// This only snapshots entities that have a Metadata component (i.e.
// active, alive entities). Ground items, expired objects, and other
// ephemeral entities are intentionally excluded — they are re-created
// from loot tables on boot.
func takeWorldSnapshot() {
	start := time.Now()

	// Collect all entity IDs first to avoid holding locks across the entire save.
	allEntities := ecs.GlobalRegistry.GetAllEntities()
	if len(allEntities) == 0 {
		logger.Info("[SNAPSHOT] No entities to snapshot.")
		return
	}

	snapshotCount := 0
	dropCount := 0

	for _, id := range allEntities {
		meta, ok := ecs.GlobalRegistry.GetMetadata(id)
		if !ok {
			continue
		}

		// Only persist players and monsters. Ground items and other
		// ephemeral entities are re-created from data on boot.
		switch meta.Type {
		case ecs.EntityPlayer:
			db.QueuePlayerSave(id)
			snapshotCount++
		case ecs.EntityMonster:
			// Monsters are persisted via the same snapshot mechanism.
			db.QueuePlayerSave(id)
			snapshotCount++
		default:
			dropCount++
		}
	}

	elapsed := time.Since(start)
	logger.Info("[SNAPSHOT] World snapshot complete: %d entities queued, %d skipped (%v).",
		snapshotCount, dropCount, elapsed)

	// Log a warning if snapshot took too long.
	if elapsed > 10*time.Second {
		logger.Warn("[PERF] World snapshot took %v — consider reducing entity count.", elapsed)
	}
}
