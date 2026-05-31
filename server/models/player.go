package models

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"server/ecs"
	"server/logger"
	"server/state"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ActivePlayers maps player network addresses (IP:port) to their ecs.Entity ID.
var ActivePlayers = &state.TypedSyncMap[string, ecs.Entity]{}

// savedPlayerData holds loaded DB state for a returning player.
type savedPlayerData struct {
	Pos          ecs.PositionComponent
	Stats        ecs.StatsComponent
	Equipment    ecs.EquipmentComponent
	Inventory    map[uint64]int
	PasswordHash string
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

// ValidatePassword checks that a password meets server rules.
// Requirements: minimum 6 characters.
func ValidatePassword(password string) bool {
	return len(password) >= 6
}

// HashPassword derives a bcrypt hash from a plaintext password.
// Cost factor 10 — good balance of security and server-side latency.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPasswordHash verifies a plaintext password against a bcrypt hash.
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
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
	var storedHash string
	err := DBEngine.QueryRowContext(ctx,
		"SELECT id, password_hash FROM characters WHERE name = ?", name,
	).Scan(&oldCharID, &storedHash)
	if err == sql.ErrNoRows {
		return nil // New player — no saved state
	}
	if err != nil {
		logger.Error("[LOAD] DB lookup error for %s: %v", name, err)
		return nil
	}

	// Step 2: Load dynamic state (position, stats, equipment).
	var mapID, x, z, hp, maxHP, damage, level int
	var xp uint64
	var weaponID, armorID uint64
	err = DBEngine.QueryRowContext(ctx,
		`SELECT map_id, x, z, hp, max_hp, damage, level, xp, weapon_id, armor_id
		 FROM character_states WHERE character_id = ?`,
		oldCharID,
	).Scan(&mapID, &x, &z, &hp, &maxHP, &damage, &level, &xp, &weaponID, &armorID)
	if err != nil && err != sql.ErrNoRows {
		logger.Error("[LOAD] State lookup error for %s (id %d): %v", name, oldCharID, err)
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

	logger.Info("[LOAD] Recovered state for %s (old id %d): map=%d pos=(%d,%d) hp=%d/%d lvl=%d xp=%d atk=%d weapon=%d armor=%d items=%d",
		name, oldCharID, mapID, x, z, hp, maxHP, level, xp, damage, weaponID, armorID, len(inventory))

	return &savedPlayerData{
		Pos:          ecs.PositionComponent{MapID: mapID, X: x, Z: z},
		Stats:        ecs.StatsComponent{Level: level, XP: xp, HP: hp, MaxHP: maxHP, Dam: damage},
		Equipment:    ecs.EquipmentComponent{WeaponID: weaponID, ArmorID: armorID},
		Inventory:    inventory,
		PasswordHash: storedHash,
	}
}

// LookupCredentials queries the DB for a user's stored password hash.
// Returns (oldCharID, passwordHash, found).
func LookupCredentials(username string) (uint64, string, bool) {
	if DBEngine == nil {
		return 0, "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var id uint64
	var hash string
	err := DBEngine.QueryRowContext(ctx,
		"SELECT id, password_hash FROM characters WHERE name = ?", username,
	).Scan(&id, &hash)
	if err == sql.ErrNoRows {
		return 0, "", false
	}
	if err != nil {
		logger.Error("[AUTH] DB lookup error for %s: %v", username, err)
		return 0, "", false
	}
	return id, hash, true
}

// RegisterNewAccount creates a database entry for a new player.
// Returns an error if the username is already taken or DB operations fail.
func RegisterNewAccount(username, passwordHash string) error {
	if DBEngine == nil {
		return fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Check if username already exists.
	var existing uint64
	err := DBEngine.QueryRowContext(ctx,
		"SELECT id FROM characters WHERE name = ?", username,
	).Scan(&existing)
	if err == nil {
		return fmt.Errorf("username already exists")
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("DB lookup failed: %w", err)
	}

	// Generate entity ID and insert.
	entityID := ecs.GlobalRegistry.NewEntity()

	tx, err := DBEngine.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("DB transaction start failed: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		"INSERT INTO characters (id, name, password_hash) VALUES (?, ?, ?)",
		entityID, username, passwordHash,
	); err != nil {
		return fmt.Errorf("DB insert character failed: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, level, xp, weapon_id, armor_id)
		 VALUES (?, 1, 0, 0, 100, 100, 15, 1, 0, 0, 0)`,
		entityID,
	); err != nil {
		return fmt.Errorf("DB insert default character_states failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("DB commit failed: %w", err)
	}

	logger.Info("[REGISTER] New account '%s' created (entity %d)", username, entityID)
	return nil
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

		// Insert the new character row, preserving the password hash.
		passHash := ""
		if saved != nil {
			passHash = saved.PasswordHash
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO characters (id, name, password_hash) VALUES (?, ?, ?)", entityID, username, passHash,
		); err != nil {
			return 0, fmt.Errorf("DB insert character failed: %w", err)
		}

		// Insert or restore dynamic state.
		if saved != nil {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, level, xp, weapon_id, armor_id)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				entityID,
				saved.Pos.MapID, saved.Pos.X, saved.Pos.Z,
				saved.Stats.HP, saved.Stats.MaxHP, saved.Stats.Dam,
				saved.Stats.Level, saved.Stats.XP,
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
				`INSERT INTO character_states (character_id, map_id, x, z, hp, max_hp, damage, level, xp, weapon_id, armor_id)
				 VALUES (?, 1, 0, 0, 100, 100, 15, 1, 0, 0, 0)`,
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
		ecs.GlobalRegistry.SetStats(entityID, ecs.StatsComponent{Level: 1, XP: 0, HP: 100, MaxHP: 100, Dam: 15})
		ecs.GlobalRegistry.SetEquipment(entityID, ecs.EquipmentComponent{WeaponID: 0, ArmorID: 0})
	}

	ecs.GlobalRegistry.SetConnection(entityID, ecs.ConnectionComponent{Conn: conn})
	ecs.GlobalRegistry.SetMetadata(entityID, ecs.MetadataComponent{Name: username, Type: "player"})

	// Track active player mapping.
	ActivePlayers.Set(playerAddress, entityID)

	return entityID, nil
}
