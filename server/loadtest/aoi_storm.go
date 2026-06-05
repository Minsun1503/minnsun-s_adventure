package loadtest

import (
	"fmt"
	"server/ecs"
	"server/logger"
	"server/peakgo/perf"
	"server/world"
	"time"
)

// ─── AOI Storm Test ───────────────────────────────────────────────────────────
//
// aoi_storm.go spawns 500 PlayerBots on the exact same (X: 50, Z: 50) tile to
// stress-test the AOI system's worst-case culling. Without MaxAOIWatchers, each
// bot would emit 500 enter/leave events per tick, causing CPU spikes > 200ms.
//
// With MaxAOIWatchers (50), each bot sees only the 50 closest entities.
// The test verifies that tick rate remains under 50ms even with 500 stacked bots.
//
// Target: Tick rate remains under 50ms even if 500 players are stacked.

// AOIStormConfig configures the AOI storm load test.
type AOIStormConfig struct {
	BotCount       int
	MapID          int
	StackX         int // all bots spawn at this X coordinate
	StackZ         int // all bots spawn at this Z coordinate
	TickInterval   time.Duration
	ReportInterval time.Duration
	Duration       time.Duration // 0 = manual stop
}

// DefaultAOIStormConfig returns a default config: 500 bots stacked on (50, 50).
func DefaultAOIStormConfig() AOIStormConfig {
	return AOIStormConfig{
		BotCount:       500,
		MapID:          1,
		StackX:         50,
		StackZ:         50,
		TickInterval:   250 * time.Millisecond,
		ReportInterval: 5 * time.Second,
		Duration:       60 * time.Second,
	}
}

// AOIStormHarness orchestrates the AOI storm load test.
type AOIStormHarness struct {
	cfg  AOIStormConfig
	stop chan struct{}
	done chan struct{}
	bots []*PlayerBotState
}

// NewAOIStormHarness creates a new AOI storm harness.
func NewAOIStormHarness(cfg AOIStormConfig) *AOIStormHarness {
	return &AOIStormHarness{
		cfg:  cfg,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
}

// SpawnBots creates N player bots all on the exact same tile.
// All bots share the same (StackX, StackZ) position so the spatial grid
// sees 500 entities in the same chunk, triggering the worst-case AOI path.
func (h *AOIStormHarness) SpawnBots() {
	logger.Info("[AOI STORM] Spawning %d bots at (%d, %d) on map %d...",
		h.cfg.BotCount, h.cfg.StackX, h.cfg.StackZ, h.cfg.MapID)

	bots := make([]*PlayerBotState, 0, h.cfg.BotCount)
	for i := 0; i < h.cfg.BotCount; i++ {
		name := fmt.Sprintf("StormBot_%d", i)
		bot := SpawnPlayerBot(name, h.cfg.MapID, h.cfg.StackX, h.cfg.StackZ)
		bots = append(bots, bot)
	}
	h.bots = bots

	logger.Info("[AOI STORM] Spawned %d bots. Entity range: %d - %d",
		len(bots), bots[0].ID, bots[len(bots)-1].ID)
}

// Cleanup removes all bots from the ECS registry and spatial grid.
func (h *AOIStormHarness) Cleanup() {
	logger.Info("[AOI STORM] Cleaning up %d bots...", len(h.bots))
	for _, bot := range h.bots {
		if bot.Alive {
			DespawnPlayerBot(bot)
		}
	}
	h.bots = nil
	logger.Info("[AOI STORM] Cleanup complete.")
}

// Start begins background tick processing. Does not block.
// Runs AOI updates on each tick to measure worst-case performance.
func (h *AOIStormHarness) Start() {
	logger.Info("[AOI STORM] Starting AOI storm test: %d bots stacked on (%d,%d), duration=%v",
		len(h.bots), h.cfg.StackX, h.cfg.StackZ, h.cfg.Duration)
	go h.run()
}

// run is the main harness goroutine.
func (h *AOIStormHarness) run() {
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
			logger.Info("[AOI STORM] Duration elapsed (%v). Stopping.", h.cfg.Duration)
			close(h.done)
			return
		case <-reportTicker.C:
			h.printReport()
		case <-ticker.C:
			h.tickAOIStorm()
		}
	}
}

// tickAOIStorm runs one tick of the AOI storm simulation.
// For each bot, it triggers an AOI update using the global spatial grid.
// This is the worst-case: 500 stacked bots each computing enter/leave
// against 499 other bots in the same chunk.
func (h *AOIStormHarness) tickAOIStorm() {
	tickStart := time.Now()

	// Process AOI for each bot. With MaxAOIWatchers=50, each bot should
	// only see the 50 closest entities (out of 500), keeping tick time low.
	for _, bot := range h.bots {
		if !bot.Alive {
			continue
		}
		pos := ecs.PositionComponent{
			MapID: bot.MapID,
			X:     bot.X,
			Z:     bot.Z,
		}
		// Trigger AOI update using the world-level ProcessAOIEvents
		// which uses GlobalAOIManager and GlobalSpatialGrid.
		world.ProcessAOIEvents(bot.ID, pos)
	}

	elapsed := time.Since(tickStart)
	elapsedNS := elapsed.Nanoseconds()

	// Record tick duration
	perf.GlobalTickMonitor.RecordTick(elapsed)
	perf.GlobalAlertMonitor.CheckTickDuration(elapsedNS)

	logger.Debug("[AOI STORM] Tick took %v (%.2fms)", elapsed, float64(elapsedNS)/1e6)
}

// printReport logs current metrics.
func (h *AOIStormHarness) printReport() {
	alive := 0
	for _, bot := range h.bots {
		if bot.Alive {
			alive++
		}
	}
	tickAvg := perf.GlobalTickMonitor.Avg()
	tickMax := perf.GlobalTickMonitor.Max()

	logger.Info("[AOI STORM] ─── Report ───")
	logger.Info("[AOI STORM] Alive bots: %d / %d", alive, len(h.bots))
	logger.Info("[AOI STORM] Server Avg tick: %v | Server Max tick: %v", tickAvg, tickMax)
	if tickMax > 50*time.Millisecond {
		logger.Warn("[AOI STORM] ⚠ Max tick exceeds 50ms threshold!")
	} else {
		logger.Info("[AOI STORM] ✓ Max tick within 50ms threshold.")
	}
}

// Wait blocks until the harness goroutine exits.
func (h *AOIStormHarness) Wait() {
	<-h.done
}

// Report returns the final metrics summary.
func (h *AOIStormHarness) Report() string {
	tickAvg := perf.GlobalTickMonitor.Avg()
	tickMax := perf.GlobalTickMonitor.Max()
	status := "PASS"
	if tickMax > 50*time.Millisecond {
		status = "FAIL"
	}
	return fmt.Sprintf("[AOI STORM] %s | %d bots stacked at (%d,%d) | Avg tick: %v | Max tick: %v",
		status, len(h.bots), h.cfg.StackX, h.cfg.StackZ, tickAvg, tickMax)
}
