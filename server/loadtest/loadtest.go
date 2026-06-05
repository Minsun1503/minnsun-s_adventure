package loadtest

import (
	"fmt"
	"server/ecs"
	"server/logger"
	"server/models"
	"server/peakgo/perf"
	"sync/atomic"
	"time"
)

// ─── Load Test Harness ────────────────────────────────────────────────────────
//
// The LoadTest framework simulates 100-1000 concurrent players doing
// movement, combat, and loot collection on the server. It uses real
// game systems (MovementSystem, AttackSystem) rather than mock calls.
//
// All bots share the same ECS Registry and Spatial Grid as real players.
// This gives a realistic measurement of tick time, heap/GC pressure,
// AI load, and spatial query performance under load.

// Config holds load test parameters.
type Config struct {
	BotCount       int           // number of player bots to spawn
	MonsterCount   int           // number of extra monsters to spawn
	MapID          int           // which map to run on
	EnableCombat   bool          // whether bots attack monsters
	TickInterval   time.Duration // simulation tick rate
	ReportInterval time.Duration // how often to print metrics
	Duration       time.Duration // 0 = run until Stop()
}

// DefaultConfig returns a sensible default config for production-like testing.
func DefaultConfig() Config {
	return Config{
		BotCount:       100,
		MonsterCount:   0,
		MapID:          1,
		EnableCombat:   true,
		TickInterval:   250 * time.Millisecond,
		ReportInterval: 5 * time.Second,
		Duration:       60 * time.Second,
	}
}

// Harness orchestrates the load test lifecycle.
type Harness struct {
	cfg Config

	stop chan struct{}
	done chan struct{}

	// Metrics (atomic for safe concurrent access)
	peakTickTime   atomic.Int64 // nanoseconds
	totalTicks     atomic.Int64
	totalMovements atomic.Int64
	totalAttacks   atomic.Int64
}

// NewHarness creates a new load test harness with the given config.
func NewHarness(cfg Config) *Harness {
	return &Harness{
		cfg:  cfg,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// SpawnBots creates N player bots on the given map.
func (h *Harness) SpawnBots(n int, mapID int) {
	logger.Info("[LOADTEST] Spawning %d bots on map %d...", n, mapID)
	bots := SpawnNBots(n, mapID)
	if len(bots) > 0 {
		logger.Info("[LOADTEST] Spawned %d bots (entity range: %d - %d)",
			len(bots), bots[0].ID, bots[len(bots)-1].ID)
	}
}

// SpawnMonsters creates N extra monsters on the given map using
// monster template ID 1 at spread-out positions.
func (h *Harness) SpawnMonsters(n int, mapID int) {
	logger.Info("[LOADTEST] Spawning %d extra monsters on map %d...", n, mapID)
	spawned := 0
	for i := 0; i < n; i++ {
		x := 10 + (i % 80)
		z := 10 + (i/80)*3
		if x > 90 {
			x = 90
		}
		if z > 90 {
			z = 90
		}

		_, err := models.SpawnMonsterFromTemplate(1, mapID, x, z)
		if err != nil {
			continue
		}
		spawned++
	}
	logger.Info("[LOADTEST] Spawned %d monsters.", spawned)
}

// Start begins background tick processing. Does not block.
// The harness runs in its own goroutine until Stop() is called
// or the configured Duration elapses.
func (h *Harness) Start() {
	logger.Info("[LOADTEST] Starting load test: %d bots, combat=%v, duration=%v",
		ActiveBotCount(), h.cfg.EnableCombat, h.cfg.Duration)

	go h.run()
}

// run is the main harness goroutine.
func (h *Harness) run() {
	ticker := time.NewTicker(h.cfg.TickInterval)
	defer ticker.Stop()

	reportTicker := time.NewTicker(h.cfg.ReportInterval)
	defer reportTicker.Stop()

	var timeoutCh <-chan time.Time
	if h.cfg.Duration > 0 {
		timeout := time.NewTimer(h.cfg.Duration)
		defer timeout.Stop()
		timeoutCh = timeout.C
	}

	for {
		select {
		case <-h.stop:
			close(h.done)
			return
		case <-timeoutCh:
			logger.Info("[LOADTEST] Duration elapsed (%v). Stopping.", h.cfg.Duration)
			close(h.done)
			return
		case <-reportTicker.C:
			h.printReport()
		case <-ticker.C:
			h.tickHarness()
		}
	}
}

// tickHarness runs one iteration of the load simulation.
func (h *Harness) tickHarness() {
	tickStart := time.Now()

	// 1. Move all bots
	moved := TickMovementBots()
	h.totalMovements.Add(int64(moved))

	// 2. Optionally attack monsters
	if h.cfg.EnableCombat {
		attacks := TickCombatBots()
		h.totalAttacks.Add(int64(attacks))
	}

	// 3. Update metrics
	elapsed := time.Since(tickStart)
	elapsedNS := elapsed.Nanoseconds()
	h.totalTicks.Add(1)

	for {
		prev := h.peakTickTime.Load()
		if elapsedNS <= prev {
			break
		}
		if h.peakTickTime.CompareAndSwap(prev, elapsedNS) {
			break
		}
	}
}

// Stop signals the harness to stop. Blocks until the goroutine exits.
func (h *Harness) Stop() {
	select {
	case h.stop <- struct{}{}:
	default:
	}
	<-h.done
}

// Wait blocks until the harness goroutine exits.
// Useful when using a Duration-based test — the harness
// auto-stops when the duration elapses.
func (h *Harness) Wait() {
	<-h.done
}

// printReport logs a snapshot of current metrics.
func (h *Harness) printReport() {
	peak := time.Duration(h.peakTickTime.Load())
	totalT := h.totalTicks.Load()
	totalM := h.totalMovements.Load()
	totalA := h.totalAttacks.Load()
	active := ActiveBotCount()

	// Fetch server-wide diagnostics
	entityCount := ecs.DefaultRegistry.TotalEntityIDs()
	tickAvg := perf.GlobalTickMonitor.Avg()
	tickMax := perf.GlobalTickMonitor.Max()

	logger.Info("[LOADTEST] ─── Report ───")
	logger.Info("[LOADTEST] Active bots: %d | Total entities: %d", active, entityCount)
	logger.Info("[LOADTEST] Ticks: %d | Movements: %d | Attacks: %d", totalT, totalM, totalA)
	if totalT > 0 {
		logger.Info("[LOADTEST] Avg movements/tick: %d | Avg attacks/tick: %d",
			totalM/totalT, totalA/totalT)
	}
	logger.Info("[LOADTEST] Peak harness tick: %v | Server Avg tick: %v | Server Max tick: %v", peak, tickAvg, tickMax)
}

// Report returns the current metrics summary as a string.
func (h *Harness) Report() string {
	peak := time.Duration(h.peakTickTime.Load())
	return fmt.Sprintf("[LOADTEST] Final Report: %d ticks, %d movements, %d attacks, peak tick=%v",
		h.totalTicks.Load(), h.totalMovements.Load(), h.totalAttacks.Load(), peak)
}
