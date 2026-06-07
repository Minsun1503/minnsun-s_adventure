package config

import (
	"encoding/json"
	"log"
	"os"
	"sync/atomic"
	"time"
)

// GameConfig holds all tunable game parameters.
// Read via GetConfig() for zero-alloc atomic access.
// Write via Reload() which does an atomic pointer swap.
type GameConfig struct {
	// Logger
	Debug    bool   `json:"debug"`
	LogDir   string `json:"log_dir"`
	LogMaxMB int    `json:"log_max_mb"`

	// Map & Movement
	MapBoundsMinX   int32 `json:"map_bounds_min_x"`
	MapBoundsMaxX   int32 `json:"map_bounds_max_x"`
	MapBoundsMinZ   int32 `json:"map_bounds_min_z"`
	MapBoundsMaxZ   int32 `json:"map_bounds_max_z"`
	MaxMoveDistance int32 `json:"max_move_distance"`
	ChunkSize       int   `json:"chunk_size"`

	// Combat
	MeleeRange       float64 `json:"melee_range"`
	CastRange        float64 `json:"cast_range"`
	AggroRadius      float64 `json:"aggro_radius"`
	LeashRadius      int     `json:"leash_radius"`
	AttackCooldownMS int     `json:"attack_cooldown_ms"`

	// AOI / Broadcast
	BroadcastAOIRadius float64 `json:"broadcast_aoi_radius"`

	// Pathfinding
	BFSMaxNodes int `json:"bfs_max_nodes"`

	// Tick
	TickRateMS      int           `json:"tick_rate_ms"`
	TickRateDur     time.Duration `json:"-"`                 // computed
	LogMetricsEvery int           `json:"log_metrics_every"` // in ticks

	// Rate Limiter
	RateLimitMaxTokens     int32 `json:"rate_limit_max_tokens"`
	RateLimitRefillPerTick int32 `json:"rate_limit_refill_per_tick"`

	// DB / Snapshot
	DBSaveIntervalMS int `json:"db_save_interval_ms"`

	// Respawn
	RespawnDelayMS  int `json:"respawn_delay_ms"`
	GroundItemTTLMS int `json:"ground_item_ttl_ms"`
}

// DefaultConfig returns a sensible default config matching the current hardcoded values.
func DefaultConfig() *GameConfig {
	return &GameConfig{
		Debug:                  false,
		LogDir:                 "logs",
		LogMaxMB:               10,
		MapBoundsMinX:          0,
		MapBoundsMaxX:          1000,
		MapBoundsMinZ:          0,
		MapBoundsMaxZ:          1000,
		MaxMoveDistance:        2,
		ChunkSize:              10,
		MeleeRange:             5.0,
		CastRange:              6.0,
		AggroRadius:            12.0,
		LeashRadius:            20,
		AttackCooldownMS:       1000,
		BroadcastAOIRadius:     60.0,
		BFSMaxNodes:            400,
		TickRateMS:             250,
		LogMetricsEvery:        40, // every ~10s at 250ms/tick
		RateLimitMaxTokens:     60,
		RateLimitRefillPerTick: 2,
		DBSaveIntervalMS:       15000,
		RespawnDelayMS:         3000,
		GroundItemTTLMS:        60000,
	}
}

// ConfigManager provides atomic hot-reload access to GameConfig.
type ConfigManager struct {
	config  atomic.Pointer[GameConfig]
	path    string
	modTime time.Time
}

// NewConfigManager creates a ConfigManager loading from path (JSON file).
// If path is empty or file doesn't exist, falls back to DefaultConfig.
func NewConfigManager(path string) *ConfigManager {
	cm := &ConfigManager{path: path}
	cfg := DefaultConfig()
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(data, cfg); err != nil {
				log.Printf("[config] failed to parse %s: %v, using defaults", path, err)
			}
		} else {
			log.Printf("[config] no config file at %s, using defaults", path)
		}
	}
	cfg.TickRateDur = time.Duration(cfg.TickRateMS) * time.Millisecond
	cm.config.Store(cfg)
	return cm
}

// Get returns the current config via atomic load (zero-alloc).
func (cm *ConfigManager) Get() *GameConfig {
	return cm.config.Load()
}

// Reload re-reads the config file and atomically swaps the pointer.
func (cm *ConfigManager) Reload() error {
	if cm.path == "" {
		return nil
	}
	data, err := os.ReadFile(cm.path)
	if err != nil {
		return err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return err
	}
	cfg.TickRateDur = time.Duration(cfg.TickRateMS) * time.Millisecond
	cm.config.Store(cfg)
	log.Printf("[config] hot-reloaded from %s", cm.path)
	return nil
}

// Watch periodically checks the config file for changes and hot-reloads.
func (cm *ConfigManager) Watch(interval time.Duration, stop <-chan struct{}) {
	if cm.path == "" {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			info, err := os.Stat(cm.path)
			if err != nil {
				continue
			}
			if info.ModTime().After(cm.modTime) {
				cm.modTime = info.ModTime()
				_ = cm.Reload()
			}
		}
	}
}

// GlobalConfigManager is the package-level singleton.
var GlobalConfigManager *ConfigManager

// C is a shorthand for GlobalConfigManager.Get()
// Safe to call before InitConfig — returns defaults if not initialized.
func C() *GameConfig {
	if GlobalConfigManager == nil {
		return DefaultConfig()
	}
	return GlobalConfigManager.Get()
}

// InitConfig initializes the global config manager.
func InitConfig(path string) *ConfigManager {
	GlobalConfigManager = NewConfigManager(path)
	return GlobalConfigManager
}
