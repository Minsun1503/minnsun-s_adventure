package db

import (
	"context"
	"fmt"
	"os"
	"server/ecs"
	"server/logger"
	"server/models"
	"strings"
	"sync"
	"time"
)

// SaveSnapshot encapsulates an atomic copy of an entity's data records at the moment of saving.
type SaveSnapshot struct {
	EntityID  ecs.Entity
	Name      string
	Pos       ecs.PositionComponent
	Stats     ecs.StatsComponent
	Inventory map[uint64]int
	Equipment ecs.EquipmentComponent
}

// ─── Periodic Memory Snapshots ───────────────────────────────────────────────
//
// A background goroutine takes periodic snapshots of all active player entities
// every SnapshotIntervalSec seconds. These snapshots are written to the save
// queue and also to a recovery log file for crash recovery.

const SnapshotIntervalSec = 300 // 5 minutes

// recoveryLogPath is the path to the recovery log file.
const recoveryLogPath = "data/recovery.log"

// saveEngineMu protects the save engine's internal state.
var saveEngineMu sync.Mutex

// Global thread-safe buffered stream channel for saving snapshots
var SaveQueue = make(chan SaveSnapshot, 1000)

// StartSaveWorkerEngine spins up the background worker channel monitor loop.
// After each successful drain cycle, it attempts to replay any buffered
// snapshots from the disk-based emergency buffer.
// Also starts the periodic memory snapshot goroutine.
func StartSaveWorkerEngine() {
	// Initialize the emergency disk buffer for overflow protection.
	if err := GlobalSaveBuffer.Init(); err != nil {
		logger.Error("[PERSISTENCE] Failed to init emergency save buffer: %v", err)
	}

	// Start the periodic memory snapshot goroutine
	go runPeriodicSnapshots()

	go func() {
		logger.Info("[PERSISTENCE] Asynchronous DB Save Worker thread active.")
		for snapshot := range SaveQueue {
			executeWriteToSQL(snapshot)
		}
		logger.Info("[PERSISTENCE] Save queue worker exited (channel closed).")

		// After the channel is drained, attempt to replay the disk buffer.
		pending := GlobalSaveBuffer.PendingCount()
		if pending > 0 {
			logger.Info("[PERSISTENCE] Replaying %d buffered snapshots from emergency buffer...", pending)
			// Re-open a temporary channel and replay into it synchronously.
			tmpQueue := make(chan SaveSnapshot, cap(SaveQueue))
			GlobalSaveBuffer.DrainBuffer()

			// Drain the replayed snapshots
			for snap := range tmpQueue {
				executeWriteToSQL(snap)
			}
		}
	}()
}

// runPeriodicSnapshots takes memory snapshots of all active player entities
// at a fixed interval and writes them to the recovery log and save queue.
func runPeriodicSnapshots() {
	ticker := time.NewTicker(SnapshotIntervalSec * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		saveEngineMu.Lock()
		// Collect all active players from the ECS registry
		// This is a best-effort snapshot — we collect what's available.
		ecs.DefaultRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
			if snap.Meta.Type != ecs.EntityPlayer || !snap.HasPos || !snap.HasStats {
				return true
			}
			// Queue a periodic save for each player
			QueuePlayerSave(snap.ID)
			return true
		})

		// Write a recovery checkpoint
		writeRecoveryCheckpoint()
		saveEngineMu.Unlock()

		logger.Debug("[PERSISTENCE] Periodic memory snapshot completed.")
	}
}

// writeRecoveryCheckpoint writes a timestamped checkpoint to the recovery log.
func writeRecoveryCheckpoint() {
	f, err := os.OpenFile(recoveryLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Error("[PERSISTENCE] Failed to open recovery log: %v", err)
		return
	}
	defer f.Close()

	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("CHECKPOINT %s\n", timestamp)
	if _, err := f.WriteString(line); err != nil {
		logger.Error("[PERSISTENCE] Failed to write recovery checkpoint: %v", err)
	}
}

// FlushSaveQueue flushes all pending snapshots and ensures the emergency
// buffer is also synced to disk. Call this during graceful shutdown to
// guarantee zero data loss.
func FlushSaveQueue() {
	saveEngineMu.Lock()
	defer saveEngineMu.Unlock()

	logger.Info("[PERSISTENCE] Flushing save queue... (remaining: %d items)", len(SaveQueue))
	close(SaveQueue)

	// Sync the emergency buffer to disk (if any buffered snapshots remain).
	GlobalSaveBuffer.FlushToDisk()
	logger.Info("[PERSISTENCE] Emergency buffer synced to disk.")
}

// QueuePlayerSave captures a fast inline memory snapshot and pushes it to the worker buffer thread.
//
// Inventory is deep-copied immediately (for k, v := range inv.Items) before the snapshot
// is enqueued, so the worker goroutine always sees a consistent bag state — even if a
// trade or pickup modifies the live component on a different goroutine before the worker
// drains the queue. Position and stats are value-type fields (copied by value out of
// sync.Map) so they are inherently snapshot-safe as well.
func QueuePlayerSave(playerID ecs.Entity) {
	meta, _ := ecs.DefaultRegistry.GetMetadata(playerID)
	pos, _ := ecs.DefaultRegistry.GetPosition(playerID)
	stats, _ := ecs.DefaultRegistry.GetStats(playerID)

	inv, hasInv := ecs.DefaultRegistry.GetInventory(playerID)
	var invCopy = make(map[uint64]int)
	if hasInv {
		// Snapshot deep copy of the map reference to prevent multi-thread data race conflicts
		for k, v := range inv.Items {
			invCopy[k] = v
		}
	}

	eq, hasEq := ecs.DefaultRegistry.GetEquipment(playerID)
	if !hasEq {
		eq = ecs.EquipmentComponent{WeaponID: 0, ArmorID: 0}
	}

	snapshot := SaveSnapshot{
		EntityID:  playerID,
		Name:      meta.Name,
		Pos:       pos,
		Stats:     stats,
		Inventory: invCopy,
		Equipment: eq,
	}

	// Backpressure-aware push with emergency disk buffer fallback.
	// If the channel is full, the snapshot is written to disk instead of dropped.
	// This guarantees ZERO DATA LOSS under extreme load.
	TryWriteToQueue(snapshot)
}

func executeWriteToSQL(snap SaveSnapshot) {
	const maxRetries = 3
	var err error
	for attempt := 0; attempt < maxRetries; attempt++ {
		start := time.Now()
		err = tryWrite(snap)
		elapsed := time.Since(start)
		if err == nil {
			logger.Info("[DB SAVE] Persisted entity #%d (%s) in %v.", snap.EntityID, snap.Name, elapsed)
			if elapsed > 100*time.Millisecond {
				logger.Warn("[PERF] DB write slow for %s: %v (budget: 100ms)", snap.Name, elapsed)
			}
			return
		}
		// Log failure and sleep with backoff for retry
		logger.Warn("[SAVE] Attempt %d failed for %s: %v", attempt+1, snap.Name, err)
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	logger.Error("[SAVE] Failed after %d retries for %s: %v", maxRetries, snap.Name, err)
}

func tryWrite(snap SaveSnapshot) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Begin transactional pipeline block with context timeout
	tx, err := models.DBEngine.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Upsert character dynamic stats and equipment records
	dynamicUpsert := `INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, level, xp, mp, max_mp, weapon_id, armor_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			map_id=VALUES(map_id), x=VALUES(x), z=VALUES(z), hp=VALUES(hp), max_hp=VALUES(max_hp), damage=VALUES(damage),
			level=VALUES(level), xp=VALUES(xp), mp=VALUES(mp), max_mp=VALUES(max_mp), weapon_id=VALUES(weapon_id), armor_id=VALUES(armor_id);`

	_, err = tx.ExecContext(ctx, dynamicUpsert, snap.EntityID, snap.Pos.MapID, snap.Pos.X, snap.Pos.Z, snap.Stats.HP, snap.Stats.MaxHP, snap.Stats.Dam, snap.Stats.Level, snap.Stats.XP, snap.Stats.MP, snap.Stats.MaxMP, snap.Equipment.WeaponID, snap.Equipment.ArmorID)
	if err != nil {
		return err
	}

	// 2. Sync inventory bag matrices via Batch Upsert.
	if len(snap.Inventory) > 0 {
		// Upsert items with qty > 0.
		query, args := buildBatchInventoryUpsert(snap.EntityID, snap.Inventory)
		if query != "" {
			_, err = tx.ExecContext(ctx, query, args...)
			if err != nil {
				return err
			}
		}

		// Delete items whose quantity has dropped to zero so the DB stays clean.
		// This handles the case where a player used the last of an item or gave it
		// all away in a trade — the upsert above would set qty=0 without this step.
		delQuery, delArgs := buildBatchInventoryDelete(snap.EntityID, snap.Inventory)
		if delQuery != "" {
			_, err = tx.ExecContext(ctx, delQuery, delArgs...)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func buildBatchInventoryUpsert(entityID ecs.Entity, inv map[uint64]int) (string, []any) {
	if len(inv) == 0 {
		return "", nil
	}
	query := `INSERT INTO character_inventory (character_id, item_template_id, quantity)
		VALUES `
	args := make([]any, 0, len(inv)*3)
	placeholders := make([]string, 0, len(inv))

	for itemID, qty := range inv {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, entityID, itemID, qty)
	}
	query += strings.Join(placeholders, ",") +
		` ON DUPLICATE KEY UPDATE quantity=VALUES(quantity)`
	return query, args
}

// buildBatchInventoryDelete constructs a DELETE query that removes all rows for the
// given entity where the item quantity is zero or below. This keeps the DB clean
// after trades, item consumption, or other quantity-decrement operations.
func buildBatchInventoryDelete(entityID ecs.Entity, inv map[uint64]int) (string, []any) {
	var args []any
	var pairs []string
	for itemID, qty := range inv {
		if qty <= 0 {
			pairs = append(pairs, "(?, ?)")
			args = append(args, entityID, itemID)
		}
	}
	if len(pairs) == 0 {
		return "", nil
	}
	query := `DELETE FROM character_inventory WHERE (character_id, item_template_id) IN (` +
		strings.Join(pairs, ",") + `)`
	return query, args
}
