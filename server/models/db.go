package models

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// DBEngine encapsulates our active database pool handle
var DBEngine *sql.DB

// InitializeDatabase opens a connection to MySQL, creates the database if it doesn't exist,
// selects it, and sets up relational tables.
func InitializeDatabase(dsn string) {
	var err error
	// Connect to MySQL server first (without database name)
	DBEngine, err = sql.Open("mysql", dsn)
	if err != nil {
		panic(fmt.Sprintf("SQL Connection Fault: %v", err))
	}

	// Verify server connection
	err = DBEngine.Ping()
	if err != nil {
		panic(fmt.Sprintf("SQL Server Ping Fault: %v", err))
	}

	// Create database if not exists
	_, err = DBEngine.Exec("CREATE DATABASE IF NOT EXISTS minnsun_adventure")
	if err != nil {
		panic(fmt.Sprintf("Failed to create database: %v", err))
	}

	// Use database
	_, err = DBEngine.Exec("USE minnsun_adventure")
	if err != nil {
		panic(fmt.Sprintf("Failed to select database: %v", err))
	}

	// Create character structural base schemas
	createStaticTable := `
	CREATE TABLE IF NOT EXISTS characters (
		id BIGINT PRIMARY KEY,
		name VARCHAR(255),
		password_hash VARCHAR(255) NOT NULL DEFAULT '',
		UNIQUE KEY idx_char_name (name)
	);`

	createDynamicTable := `
	CREATE TABLE IF NOT EXISTS character_states (
		character_id BIGINT PRIMARY KEY,
		map_id INT,
		x INT,
		z INT,
		hp INT,
		max_hp INT,
		damage INT,
		level INT NOT NULL DEFAULT 1,
		xp BIGINT UNSIGNED NOT NULL DEFAULT 0,
		mp INT NOT NULL DEFAULT 100,
		max_mp INT NOT NULL DEFAULT 100,
		weapon_id BIGINT,
		armor_id BIGINT,
		FOREIGN KEY (character_id) REFERENCES characters (id) ON DELETE CASCADE
	);`

	createInventoryTable := `
	CREATE TABLE IF NOT EXISTS character_inventory (
		character_id BIGINT,
		item_template_id BIGINT,
		quantity INT,
		PRIMARY KEY(character_id, item_template_id),
		FOREIGN KEY(character_id) REFERENCES characters (id) ON DELETE CASCADE
	);`

	_, err = DBEngine.Exec(createStaticTable)
	if err != nil {
		panic(fmt.Sprintf("Schema Compilation Error (static): %v", err))
	}

	_, err = DBEngine.Exec(createDynamicTable)
	if err != nil {
		panic(fmt.Sprintf("Schema Compilation Error (dynamic): %v", err))
	}

	_, err = DBEngine.Exec(createInventoryTable)
	if err != nil {
		panic(fmt.Sprintf("Schema Compilation Error (inventory): %v", err))
	}

	// Ensure password_hash column exists for pre-existing installations.
	_, err = DBEngine.Exec("ALTER TABLE characters ADD COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT ''")
	if err != nil {
		_ = err
	}

	// Ensure level and xp columns exist for pre-existing installations.
	_, _ = DBEngine.Exec("ALTER TABLE character_states ADD COLUMN level INT NOT NULL DEFAULT 1")
	_, _ = DBEngine.Exec("ALTER TABLE character_states ADD COLUMN xp BIGINT UNSIGNED NOT NULL DEFAULT 0")
	_, _ = DBEngine.Exec("ALTER TABLE character_states ADD COLUMN mp INT NOT NULL DEFAULT 100")
	_, _ = DBEngine.Exec("ALTER TABLE character_states ADD COLUMN max_mp INT NOT NULL DEFAULT 100")

	fmt.Println("[DATABASE] Relational system matrices initialized and ready.")
}
