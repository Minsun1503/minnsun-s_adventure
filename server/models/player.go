package models

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"server/ecs"
	"server/state"
	"strings"
	"time"
)

// ActivePlayers maps player network addresses (IP:port) to their ecs.Entity ID.
var ActivePlayers = &state.TypedSyncMap[string, ecs.Entity]{}

// savedPlayerData holds loaded DB state for a returning player.
type savedPlayerData struct {
	Pos       ecs.PositionComponent
	Stats     ecs.StatsComponent
	Equipment ecs.EquipmentComponent
	Inventory map[uint64]int
}

// ValidateUsername checks that a username meets the server rules.
// Requirements: 3-16 characters, alphanumeric only (A-Z, a-z, 0-9).
func ValidateUsername(name string) bool {
	if len(name) < 3 || len(name) > 16 {
		return false
	}
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')) {
			return false
		}
	}
	return true
}

// SanitizeUsername trims whitespace and converts to a canonical form.
func SanitizeUsername(name string) string {
	return strings.TrimSpace(name)
}

// loadSavedPlayerState queries the DB for an existing character by name.
// Returns nil if no saved state exists (brand new player).
func loadSavedPlayerState(name string) *savedPlayerData {
	if DBEngine == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Step 1: Find the character by unique name.
	var oldCharID uint64
	err := DBEngine.QueryRowContext(ctx,
		"SELECT id FROM characters WHERE name = ?", name,
	).Scan(&oldCharID)
	if err == sql.ErrNoRows {
		return nil // New player — no saved state
	}
	if err != nil {
		fmt.Printf("[LOAD] DB lookup error for %s: %v\n", name, err)
		return nil
	}

	// Step 2: Load dynamic state (position, stats, equipment).
	var mapID, x, z, hp, maxHP, damage int
	var weaponID, armorID uint64
	err = DBEngine.QueryRowContext(ctx,
		`SELECT map_id, x, z, hp, max_hp, damage, weapon_id, armor_id
		 FROM character_states WHERE character_id = ?`,
		oldCharID,
	).Scan(&mapID, &x, &z, &hp, &maxHP, &damage, &weaponID, &armorID)
	if err != nil && err != sql.ErrNoRows {
		fmt.Printf("[LOAD] State lookup error for %s (id %d): %v\n", name, oldCharID, err)
	}

	// Step 3: Load inventory items.
	inventory := make(map[uint64]int)
	rows, err := DBEngine.QueryContext(ctx,
		"SELECT item_template_id, quantity FROM character_inventory WHERE character_id = ?",
		oldCharID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var itemID uint64
			var qty int
			if err := rows.Scan(&itemID, &qty); err == nil {
				inventory[itemID] = qty
			}
		}
	}

	fmt.Printf("[LOAD] Recovered state for %s (old id %d): map=%d pos=(%d,%d) hp=%d/%d atk=%d weapon=%d armor=%d items=%d\n",
		name, oldCharID, mapID, x, z, hp, maxHP, damage, weaponID, armorID, len(inventory))

	return &savedPlayerData{
		Pos:       ecs.PositionComponent{MapID: mapID, X: x, Z: z},
		Stats:     ecs.StatsComponent{HP: hp, MaxHP: maxHP, Dam: damage},
		Equipment: ecs.EquipmentComponent{WeaponID: weaponID, ArmorID: armorID},
		Inventory: inventory,
	}
}

// CreatePlayerEntity registers a player entity in the ECS registry by username.
// If the username already exists in the database, their saved position, stats,
// equipment, and inventory are restored. Otherwise, a fresh character with
// default stats is created and persisted.
//
// Parameters:
//   - conn:     The live TCP socket connection of the player client.
//   - username: The player's chosen username (already validated and sanitized).
//
// Returns:
//   - The ecs.Entity ID registered in the ECS registry.
//   - An error if database operations fail.
func CreatePlayerEntity(conn net.Conn, username string) (ecs.Entity, error) {
	playerAddress := conn.RemoteAddr().String()

	// Phase 1: Load saved state if this username has been here before.
	saved := loadSavedPlayerState(username)

	// Phase 2: Generate a fresh entity ID for this session.
	entityID := ecs.GlobalRegistry.NewEntity()

	// Phase 3: Persist to database.
	if DBEngine != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		tx, err := DBEngine.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("DB transaction start failed: %w", err)
		}
		defer tx.Rollback()

		// Delete old rows for this username (FK ON DELETE CASCADE cleans child tables).
		if saved != nil {
			if _, err := tx.ExecContext(ctx,
				"DELETE FROM characters WHERE name = ?", username,
			); err != nil {
				return 0, fmt.Errorf("DB delete old character failed: %w", err)
			}
		}

		// Insert the new character row.
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO characters (id, name) VALUES (?, ?)", entityID, username,
		); err != nil {
			return 0, fmt.Errorf("DB insert character failed: %w", err)
		}

		// Insert or restore dynamic state.
		if saved != nil {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, weapon_id, armor_id)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				entityID,
				saved.Pos.MapID, saved.Pos.X, saved.Pos.Z,
				saved.Stats.HP, saved.Stats.MaxHP, saved.Stats.Dam,
				saved.Equipment.WeaponID, saved.Equipment.ArmorID,
			); err != nil {
				return 0, fmt.Errorf("DB insert character_states failed: %w", err)
			}

			for itemID, qty := range saved.Inventory {
				if _, err := tx.ExecContext(ctx,
					"INSERT INTO character_inventory (character_id, item_template_id, quantity) VALUES (?, ?, ?)",
					entityID, itemID, qty,
				); err != nil {
					return 0, fmt.Errorf("DB insert character_inventory failed: %w", err)
				}
			}
		} else {
			// Brand new player — insert default state.
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, weapon_id, armor_id)
				 VALUES (?, 1, 0, 0, 100, 100, 15, 0, 0)`,
				entityID,
			); err != nil {
				return 0, fmt.Errorf("DB insert default character_states failed: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("DB commit failed: %w", err)
		}
	}

	// Phase 4: Register ECS components.
	if saved != nil {
		ecs.GlobalRegistry.SetPosition(entityID, saved.Pos)
		ecs.GlobalRegistry.SetStats(entityID, saved.Stats)
		ecs.GlobalRegistry.SetEquipment(entityID, saved.Equipment)

		if len(saved.Inventory) > 0 {
			ecs.GlobalRegistry.SetInventory(entityID, ecs.InventoryComponent{
				Items: saved.Inventory,
			})
		}
	} else {
		ecs.GlobalRegistry.SetPosition(entityID, ecs.PositionComponent{MapID: 1, X: 0, Z: 0})
		ecs.GlobalRegistry.SetStats(entityID, ecs.StatsComponent{HP: 100, MaxHP: 100, Dam: 15})
		ecs.GlobalRegistry.SetEquipment(entityID, ecs.EquipmentComponent{WeaponID: 0, ArmorID: 0})
	}

	ecs.GlobalRegistry.SetConnection(entityID, ecs.ConnectionComponent{Conn: conn})
	ecs.GlobalRegistry.SetMetadata(entityID, ecs.MetadataComponent{Name: username, Type: "player"})

	// Track active player mapping.
	ActivePlayers.Set(playerAddress, entityID)

	return entityID, nil
}
