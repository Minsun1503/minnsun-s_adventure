package db

import (
	"context"
	"server/ecs"
	"server/logger"
	"server/models"
	"strings"
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

// Global thread-safe buffered stream channel for saving snapshots
var SaveQueue = make(chan SaveSnapshot, 1000)

// StartSaveWorkerEngine spins up the background worker channel monitor loop
func StartSaveWorkerEngine() {
	go func() {
		logger.Info("[PERSISTENCE] Asynchronous DB Save Worker thread active.")
		for snapshot := range SaveQueue {
			executeWriteToSQL(snapshot)
		}
	}()
}

// QueuePlayerSave captures a fast inline memory snapshot and pushes it to the worker buffer thread.
//
// NOTE: This snapshot is not atomic across all components.
// A torn read (components from different game ticks) is acceptable
// given the 250ms tick rate. The worst case is a 1-tick-stale save.
func QueuePlayerSave(playerID ecs.Entity) {
	meta, _ := ecs.GlobalRegistry.GetMetadata(playerID)
	pos, _ := ecs.GlobalRegistry.GetPosition(playerID)
	stats, _ := ecs.GlobalRegistry.GetStats(playerID)

	inv, hasInv := ecs.GlobalRegistry.GetInventory(playerID)
	var invCopy = make(map[uint64]int)
	if hasInv {
		// Snapshot deep copy of the map reference to prevent multi-thread data race conflicts
		for k, v := range inv.Items {
			invCopy[k] = v
		}
	}

	eq, hasEq := ecs.GlobalRegistry.GetEquipment(playerID)
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

	// Non-blocking push down the channel tube!
	select {
	case SaveQueue <- snapshot:
	default:
		queueLen := len(SaveQueue)
		logger.Warn("[SAVE ALERT] Queue full (%d/1000)! DROP: entity %d (%s). DATA LOSS!",
			queueLen, playerID, meta.Name)
	}
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
	dynamicUpsert := `INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, level, xp, weapon_id, armor_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			map_id=VALUES(map_id), x=VALUES(x), z=VALUES(z), hp=VALUES(hp), max_hp=VALUES(max_hp), damage=VALUES(damage),
			level=VALUES(level), xp=VALUES(xp), weapon_id=VALUES(weapon_id), armor_id=VALUES(armor_id);`

	_, err = tx.ExecContext(ctx, dynamicUpsert, snap.EntityID, snap.Pos.MapID, snap.Pos.X, snap.Pos.Z, snap.Stats.HP, snap.Stats.MaxHP, snap.Stats.Dam, snap.Stats.Level, snap.Stats.XP, snap.Equipment.WeaponID, snap.Equipment.ArmorID)
	if err != nil {
		return err
	}

	// 2. Sync inventory bag matrices via Batch Upsert
	if len(snap.Inventory) > 0 {
		query, args := buildBatchInventoryUpsert(snap.EntityID, snap.Inventory)
		if query != "" {
			_, err = tx.ExecContext(ctx, query, args...)
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
