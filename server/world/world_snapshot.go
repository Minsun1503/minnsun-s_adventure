// Package world provides world-level management for the game server.
//
// world_snapshot.go — Enhanced world snapshot with full state serialization
// including entities, inventory, party, and metadata for crash recovery.
package world

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"server/ecs"
	"server/logger"
	"server/peakgo/perf"
	"sync"
	"time"
)

// ─── Snapshot Data Structures ─────────────────────────────────────────────────
//
// WorldSnapshotData is a complete, serializable capture of the world state.
// It is written to disk and loaded on startup for crash recovery.

// SnapshotVersion is the format version for world snapshots.
// Increment when snapshot structure changes to handle migration.
const SnapshotVersion = 1

// SnapshotInterval is how often the periodic world snapshot runs.
// Default: 5 minutes (300 seconds).
const SnapshotInterval = 5 * time.Minute

// SnapshotMeta stores metadata about a snapshot file.
type SnapshotMeta struct {
	Version      int       `json:"version"`       // Snapshot format version
	Timestamp    time.Time `json:"timestamp"`     // When the snapshot was taken
	EntityCount  int       `json:"entity_count"`  // Number of entities in snapshot
	PlayerCount  int       `json:"player_count"`  // Number of player entities
	MonsterCount int       `json:"monster_count"` // Number of monster entities
	ServerUptime string    `json:"server_uptime"` // Server uptime at snapshot time
	SnapshotType string    `json:"snapshot_type"` // "periodic" or "shutdown"
}

// EntitySnapshotData holds all component data for a single entity.
type EntitySnapshotData struct {
	ID        ecs.Entity                 `json:"id"`
	Meta      ecs.MetadataComponent      `json:"meta"`
	Pos       ecs.PositionComponent      `json:"pos"`
	Stats     ecs.StatsComponent         `json:"stats"`
	Inventory *ecs.InventoryComponent    `json:"inventory,omitempty"`
	Equipment *ecs.EquipmentComponent    `json:"equipment,omitempty"`
	Effects   *ecs.EffectsComponent      `json:"effects,omitempty"`
	AI        *ecs.AIComponent           `json:"ai,omitempty"`
	Party     *ecs.PartyComponent        `json:"party,omitempty"`
	ItemTemp  *ecs.ItemTemplateComponent `json:"item_template,omitempty"`
	HasPos    bool                       `json:"-"`
	HasStats  bool                       `json:"-"`
}

// WorldSnapshotData is the complete serializable world state.
type WorldSnapshotData struct {
	Meta     SnapshotMeta         `json:"meta"`
	Entities []EntitySnapshotData `json:"entities"`
}

// ─── Snapshot File Management ─────────────────────────────────────────────────

// snapshotBaseDir is the directory for persistent snapshot files.
const snapshotBaseDir = "data/snapshots"

// snapshotFilePrefix is the prefix for snapshot files.
const snapshotFilePrefix = "world_snapshot_"

// maxSnapshots is the maximum number of snapshot files to retain.
const maxSnapshots = 3

// snapshotMu protects the snapshot file directory operations.
var snapshotMu sync.Mutex

// ensureSnapshotDir creates the snapshot directory if it doesn't exist.
func ensureSnapshotDir() error {
	return os.MkdirAll(snapshotBaseDir, 0755)
}

// snapshotFilename returns the path for a snapshot file with the given timestamp.
func snapshotFilename(ts time.Time) string {
	return filepath.Join(snapshotBaseDir,
		snapshotFilePrefix+ts.Format("2006-01-02_150405")+".snap")
}

// latestSnapshotFilename returns the path to the "latest" symlink file.
func latestSnapshotFilename() string {
	return filepath.Join(snapshotBaseDir, "latest.snap")
}

// ─── Periodic Snapshot ────────────────────────────────────────────────────────

// StartPeriodicSnapshot launches a background goroutine that runs
// periodic world snapshots. It serializes all active entity state
// (players, monsters, etc.) to disk as a complete WorldSnapshotData.
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
			takeWorldSnapshot("periodic")
			busy = false
		}
	}()
}

// takeWorldSnapshot captures the complete world state and writes it to disk.
func takeWorldSnapshot(snapshotType string) {
	start := time.Now()

	// Collect all entities
	allEntities := ecs.DefaultRegistry.GetAllEntities()
	if len(allEntities) == 0 {
		logger.Info("[SNAPSHOT] No entities to snapshot.")
		return
	}

	var entities []EntitySnapshotData
	playerCount := 0
	monsterCount := 0

	for _, id := range allEntities {
		meta, ok := ecs.DefaultRegistry.GetMetadata(id)
		if !ok {
			continue
		}

		// Skip bot players from world snapshot
		if meta.Type == ecs.EntityPlayer && len(meta.Name) >= 3 && meta.Name[:3] == "bot" {
			continue
		}

		pos, hasPos := ecs.DefaultRegistry.GetPosition(id)
		stats, hasStats := ecs.DefaultRegistry.GetStats(id)

		data := EntitySnapshotData{
			ID:       id,
			Meta:     meta,
			Pos:      pos,
			Stats:    stats,
			HasPos:   hasPos,
			HasStats: hasStats,
		}

		// Capture optional components (only if present)
		if inv, ok := ecs.DefaultRegistry.GetInventory(id); ok {
			clone := inv.Clone()
			data.Inventory = &clone
		}
		if eq, ok := ecs.DefaultRegistry.GetEquipment(id); ok {
			data.Equipment = &eq
		}
		if eff, ok := ecs.DefaultRegistry.GetEffects(id); ok {
			clone := eff.Clone()
			data.Effects = &clone
		}
		if ai, ok := ecs.DefaultRegistry.GetAI(id); ok {
			data.AI = &ai
		}
		if party, ok := ecs.DefaultRegistry.GetParty(id); ok {
			clone := party.Clone()
			data.Party = &clone
		}
		if itemTemp, ok := ecs.DefaultRegistry.GetItemTemplate(id); ok {
			data.ItemTemp = &itemTemp
		}

		entities = append(entities, data)

		switch meta.Type {
		case ecs.EntityPlayer:
			playerCount++
		case ecs.EntityMonster:
			monsterCount++
		}
	}

	// Build the snapshot
	snapshot := WorldSnapshotData{
		Meta: SnapshotMeta{
			Version:      SnapshotVersion,
			Timestamp:    start,
			EntityCount:  len(entities),
			PlayerCount:  playerCount,
			MonsterCount: monsterCount,
			ServerUptime: fmt.Sprintf("%.0f", time.Since(start).Seconds()),
			SnapshotType: snapshotType,
		},
		Entities: entities,
	}

	// Write to disk
	if err := writeSnapshotToDisk(&snapshot); err != nil {
		logger.Error("[SNAPSHOT] Failed to write snapshot: %v", err)
		return
	}

	// Cleanup old snapshots
	cleanupOldSnapshots()

	elapsed := time.Since(start)
	logger.Info("[SNAPSHOT] %s snapshot complete: %d entities (%d players, %d monsters) in %v.",
		snapshotType, len(entities), playerCount, monsterCount, elapsed)

	if elapsed > 10*time.Second {
		logger.Warn("[PERF] World snapshot took %v — consider reducing entity count.", elapsed)
	}

	// Record performance metric
	perf.GlobalTickMonitor.RecordTick(elapsed)
}

// writeSnapshotToDisk serializes and writes a world snapshot to a file.
func writeSnapshotToDisk(snapshot *WorldSnapshotData) error {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	if err := ensureSnapshotDir(); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	filename := snapshotFilename(snapshot.Meta.Timestamp)
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create snapshot file %s: %w", filename, err)
	}
	defer f.Close()

	enc := gob.NewEncoder(f)
	if err := enc.Encode(snapshot); err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	// Write a "latest" symlink file that points to this snapshot
	// On Windows we write a marker file instead of a symlink
	if err := writeLatestMarker(snapshot.Meta.Timestamp); err != nil {
		logger.Warn("[SNAPSHOT] Failed to write latest marker: %v", err)
	}

	logger.Info("[SNAPSHOT] Saved snapshot to %s (%d entities).", filename, snapshot.Meta.EntityCount)
	return nil
}

// writeLatestMarker writes a marker file pointing to the latest snapshot.
func writeLatestMarker(ts time.Time) error {
	markerPath := latestSnapshotFilename()
	markerContent := ts.Format("2006-01-02_150405")
	return os.WriteFile(markerPath, []byte(markerContent), 0644)
}

// readLatestMarker reads the latest snapshot marker file and returns the timestamp.
func readLatestMarker() (string, error) {
	markerPath := latestSnapshotFilename()
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// cleanupOldSnapshots removes old snapshot files, keeping only the most recent ones.
func cleanupOldSnapshots() {
	entries, err := os.ReadDir(snapshotBaseDir)
	if err != nil {
		return
	}

	// Collect snapshot files
	var snapFiles []os.FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(entry.Name()) < len(snapshotFilePrefix) || entry.Name()[:len(snapshotFilePrefix)] != snapshotFilePrefix {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		snapFiles = append(snapFiles, info)
	}

	// Sort by modification time (oldest first) - simple bubble sort
	for i := 0; i < len(snapFiles); i++ {
		for j := i + 1; j < len(snapFiles); j++ {
			if snapFiles[j].ModTime().Before(snapFiles[i].ModTime()) {
				snapFiles[i], snapFiles[j] = snapFiles[j], snapFiles[i]
			}
		}
	}

	// Remove excess files
	if len(snapFiles) > maxSnapshots {
		toRemove := len(snapFiles) - maxSnapshots
		for i := 0; i < toRemove; i++ {
			path := filepath.Join(snapshotBaseDir, snapFiles[i].Name())
			os.Remove(path)
			logger.Debug("[SNAPSHOT] Removed old snapshot: %s", snapFiles[i].Name())
		}
	}
}

// ─── Snapshot Loading (Crash Recovery) ────────────────────────────────────────

// LoadLatestSnapshot loads the most recent world snapshot from disk.
// Returns nil if no snapshot is available.
func LoadLatestSnapshot() *WorldSnapshotData {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	marker, err := readLatestMarker()
	if err != nil {
		logger.Info("[RECOVERY] No world snapshot marker found — no recovery data available.")
		return nil
	}

	filename := filepath.Join(snapshotBaseDir, snapshotFilePrefix+marker+".snap")
	f, err := os.Open(filename)
	if err != nil {
		logger.Warn("[RECOVERY] Latest snapshot file not found: %s — trying directory scan.", filename)

		// Fallback: find the most recent snapshot file
		return loadMostRecentSnapshot()
	}
	defer f.Close()

	dec := gob.NewDecoder(f)
	var snapshot WorldSnapshotData
	if err := dec.Decode(&snapshot); err != nil {
		logger.Error("[RECOVERY] Failed to decode snapshot %s: %v", filename, err)
		return nil
	}

	logger.Info("[RECOVERY] Loaded world snapshot from %s (version %d, %d entities, %s type).",
		filename, snapshot.Meta.Version, snapshot.Meta.EntityCount, snapshot.Meta.SnapshotType)
	return &snapshot
}

// loadMostRecentSnapshot finds and loads the most recent snapshot file.
func loadMostRecentSnapshot() *WorldSnapshotData {
	entries, err := os.ReadDir(snapshotBaseDir)
	if err != nil {
		logger.Info("[RECOVERY] No snapshot directory found.")
		return nil
	}

	var latestFile string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(entry.Name()) < len(snapshotFilePrefix) || entry.Name()[:len(snapshotFilePrefix)] != snapshotFilePrefix {
			continue
		}
		if entry.Name() == "latest.snap" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = entry.Name()
		}
	}

	if latestFile == "" {
		logger.Info("[RECOVERY] No snapshot files found.")
		return nil
	}

	filename := filepath.Join(snapshotBaseDir, latestFile)
	f, err := os.Open(filename)
	if err != nil {
		logger.Error("[RECOVERY] Failed to open snapshot %s: %v", filename, err)
		return nil
	}
	defer f.Close()

	dec := gob.NewDecoder(f)
	var snapshot WorldSnapshotData
	if err := dec.Decode(&snapshot); err != nil {
		logger.Error("[RECOVERY] Failed to decode snapshot %s: %v", filename, err)
		return nil
	}

	// Update the marker file
	writeLatestMarker(snapshot.Meta.Timestamp)
	logger.Info("[RECOVERY] Loaded world snapshot from %s (version %d, %d entities).",
		filename, snapshot.Meta.Version, snapshot.Meta.EntityCount)
	return &snapshot
}

// RestoreWorldFromSnapshot applies a snapshot to the ECS registry.
// This restores all entities, their positions, stats, inventory, and other components.
func RestoreWorldFromSnapshot(snapshot *WorldSnapshotData) int {
	if snapshot == nil {
		logger.Info("[RECOVERY] No snapshot to restore.")
		return 0
	}

	restored := 0
	for i := range snapshot.Entities {
		entity := snapshot.Entities[i]

		// Restore metadata (always present)
		ecs.DefaultRegistry.SetMetadata(entity.ID, entity.Meta)

		// Restore position
		if entity.HasPos {
			ecs.DefaultRegistry.SetPosition(entity.ID, entity.Pos)
		}

		// Restore stats
		if entity.HasStats {
			ecs.DefaultRegistry.SetStats(entity.ID, entity.Stats)
		}

		// Restore optional components
		if entity.Inventory != nil {
			ecs.DefaultRegistry.SetInventory(entity.ID, *entity.Inventory)
		}
		if entity.Equipment != nil {
			ecs.DefaultRegistry.SetEquipment(entity.ID, *entity.Equipment)
		}
		if entity.Effects != nil {
			ecs.DefaultRegistry.SetEffects(entity.ID, *entity.Effects)
		}
		if entity.AI != nil {
			ecs.DefaultRegistry.SetAI(entity.ID, *entity.AI)
		}
		if entity.Party != nil {
			ecs.DefaultRegistry.SetParty(entity.ID, *entity.Party)
		}
		if entity.ItemTemp != nil {
			ecs.DefaultRegistry.SetItemTemplate(entity.ID, *entity.ItemTemp)
		}

		restored++
	}

	logger.Info("[RECOVERY] Restored %d entities from world snapshot (taken at %s).",
		restored, snapshot.Meta.Timestamp.Format(time.RFC3339))
	return restored
}

// TakeShutdownSnapshot takes a snapshot specifically for graceful shutdown.
// This is called by the shutdown procedure.
func TakeShutdownSnapshot() {
	logger.Info("[SHUTDOWN] Taking pre-shutdown world snapshot...")
	takeWorldSnapshot("shutdown")
}

// SnapshotMetrics returns diagnostic info about the snapshot system.
type SnapshotMetrics struct {
	LastSnapshotTime string `json:"last_snapshot_time"`
	SnapshotCount    int    `json:"snapshot_count"`
	PendingBuffer    int    `json:"pending_buffer"`
}

// GetSnapshotMetrics returns current snapshot system metrics.
func GetSnapshotMetrics() SnapshotMetrics {
	snapshotMu.Lock()
	defer snapshotMu.Unlock()

	entries, _ := os.ReadDir(snapshotBaseDir)
	count := 0
	lastTime := "never"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(entry.Name()) >= len(snapshotFilePrefix) && entry.Name()[:len(snapshotFilePrefix)] == snapshotFilePrefix {
			count++
		}
		if entry.Name() == "latest.snap" {
			data, _ := os.ReadFile(filepath.Join(snapshotBaseDir, entry.Name()))
			if len(data) > 0 {
				lastTime = string(data)
			}
		}
	}

	return SnapshotMetrics{
		LastSnapshotTime: lastTime,
		SnapshotCount:    count,
	}
}

// init registers types with gob for snapshot serialization.
func init() {
	gob.Register(ecs.PositionComponent{})
	gob.Register(ecs.MetadataComponent{})
	gob.Register(ecs.StatsComponent{})
	gob.Register(ecs.InventoryComponent{})
	gob.Register(ecs.EquipmentComponent{})
	gob.Register(ecs.EffectsComponent{})
	gob.Register(ecs.AIComponent{})
	gob.Register(ecs.PartyComponent{})
	gob.Register(ecs.ItemTemplateComponent{})
	gob.Register(EntitySnapshotData{})
	gob.Register(WorldSnapshotData{})
}
