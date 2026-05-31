package db

import (
	"context"
	"fmt"
	"server/ecs"
	"server/models"
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
		fmt.Println("[PERSISTENCE] Asynchronous DB Save Worker thread active.")
		for snapshot := range SaveQueue {
			executeWriteToSQL(snapshot)
		}
	}()
}

// QueuePlayerSave captures a fast inline memory snapshot and pushes it to the worker buffer thread
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

	// Non-blocking push down the channel tube! Lasts less than a microsecond.
	select {
	case SaveQueue <- snapshot:
	default:
		fmt.Printf("Alert: Save worker queue buffer full! Dropping save row payload for %s\n", meta.Name)
	}
}

func executeWriteToSQL(snap SaveSnapshot) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Begin transactional pipeline block with context timeout
	tx, err := models.DBEngine.BeginTx(ctx, nil)
	if err != nil {
		fmt.Printf("Error: DB Transaction Fail: %v\n", err)
		return
	}
	defer tx.Rollback()

	// 1. Upsert character dynamic stats and equipment records
	dynamicUpsert := `INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, weapon_id, armor_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			map_id=VALUES(map_id), x=VALUES(x), z=VALUES(z), hp=VALUES(hp), max_hp=VALUES(max_hp), damage=VALUES(damage),
			weapon_id=VALUES(weapon_id), armor_id=VALUES(armor_id);`

	_, err = tx.ExecContext(ctx, dynamicUpsert, snap.EntityID, snap.Pos.MapID, snap.Pos.X, snap.Pos.Z, snap.Stats.HP, snap.Stats.MaxHP, snap.Stats.Dam, snap.Equipment.WeaponID, snap.Equipment.ArmorID)
	if err != nil {
		fmt.Printf("Error: Failed SQL dynamic write: %v\n", err)
		return
	}

	// 2. Sync inventory bag matrices
	for itemID, qty := range snap.Inventory {
		invUpsert := `INSERT INTO character_inventory (character_id, item_template_id, quantity)
			VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE quantity=VALUES(quantity);`
		_, err = tx.ExecContext(ctx, invUpsert, snap.EntityID, itemID, qty)
		if err != nil {
			fmt.Printf("Error: Failed SQL inventory write: %v\n", err)
			return
		}
	}

	// Commit data to disk sequentially
	if err := tx.Commit(); err == nil {
		fmt.Printf("DB Persisted: Safe-write operation completed for entity row #%d (%s).\n", snap.EntityID, snap.Name)
	}
}
