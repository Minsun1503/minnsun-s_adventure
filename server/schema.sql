-- Database creation
CREATE DATABASE IF NOT EXISTS minnsun_adventure;
USE minnsun_adventure;

-- Table to store static player character metadata (such as name)
CREATE TABLE IF NOT EXISTS characters (
    id BIGINT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE KEY idx_char_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Table to store dynamic player character statistics, positions, and equipment
CREATE TABLE IF NOT EXISTS character_states (
    character_id BIGINT PRIMARY KEY,
    map_id INT NOT NULL DEFAULT 1,
    x INT NOT NULL DEFAULT 0,
    z INT NOT NULL DEFAULT 0,
    hp INT NOT NULL DEFAULT 100,
    max_hp INT NOT NULL DEFAULT 100,
    damage INT NOT NULL DEFAULT 15,
    level INT NOT NULL DEFAULT 1,
    xp BIGINT UNSIGNED NOT NULL DEFAULT 0,
    mp INT NOT NULL DEFAULT 100,
    max_mp INT NOT NULL DEFAULT 100,
    weapon_id BIGINT DEFAULT 0,
    armor_id BIGINT DEFAULT 0,
    CONSTRAINT fk_states_character FOREIGN KEY (character_id)
        REFERENCES characters(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Table to store player inventory items
CREATE TABLE IF NOT EXISTS character_inventory (
    character_id BIGINT NOT NULL,
    item_template_id BIGINT NOT NULL,
    quantity INT NOT NULL DEFAULT 1,
    PRIMARY KEY (character_id, item_template_id),
    CONSTRAINT fk_inventory_character FOREIGN KEY (character_id) 
        REFERENCES characters(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
