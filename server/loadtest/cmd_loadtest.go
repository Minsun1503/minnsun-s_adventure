package loadtest

import (
	"fmt"
	"server/ecs"
	"server/logger"
	"server/peakgo/perf"
	"time"
)

// RunDefaultLoadTest runs the load test with default parameters
// and returns a summary string. This is the recommended entry point
// for quick load testing from server startup.
//
// Default test:
//   - 500 player bots on map 1
//   - 5000 monsters on map 1 (uses the existing monster pool + new ones)
//   - Movement + Combat enabled
//   - Runs for 60 seconds
//   - Reports every 5 seconds
func RunDefaultLoadTest() string {
	cfg := DefaultConfig()
	cfg.BotCount = 500
	cfg.MonsterCount = 5000
	cfg.Duration = 60 * time.Second

	return RunLoadTest(cfg)
}

// RunLoadTest runs the load test with the given config and returns
// the final report string. Blocks until the test completes.
func RunLoadTest(cfg Config) string {
	logger.Info("[LOADTEST] ═══════════════════════════════════════════")
	logger.Info("[LOADTEST]  Load Test Framework v1.0")
	logger.Info("[LOADTEST] ═══════════════════════════════════════════")
	logger.Info("[LOADTEST] Config: %d bots, %d monsters, combat=%v, duration=%v",
		cfg.BotCount, cfg.MonsterCount, cfg.EnableCombat, cfg.Duration)

	harness := NewHarness(cfg)

	// Timed phase 1: Spawn monsters first
	monsterStart := time.Now()
	harness.SpawnMonsters(cfg.MonsterCount, cfg.MapID)
	logger.Info("[LOADTEST] Monster spawn took %v", time.Since(monsterStart))

	// Timed phase 2: Spawn player bots
	botStart := time.Now()
	harness.SpawnBots(cfg.BotCount, cfg.MapID)
	logger.Info("[LOADTEST] Bot spawn took %v", time.Since(botStart))

	// Phase 3: Start simulation
	entityCount := ecs.DefaultRegistry.TotalEntityIDs()
	logger.Info("[LOADTEST] Total ECS entities after spawn: %d", entityCount)

	harness.Start()
	logger.Info("[LOADTEST] Simulation running for %v...", cfg.Duration)

	// Block until harness completes
	harness.Wait()

	report := harness.Report()
	logger.Info("[LOADTEST] %s", report)

	// Final diagnostics
	collect := perf.Collect(perf.GlobalTickMonitor, perf.GlobalPacketMonitor, perf.GlobalMemMonitor)
	logger.Info("[LOADTEST] ─── Final Server Diagnostics ───")
	logger.Info("[LOADTEST] Tick: min=%v max=%v avg=%v count=%d",
		collect.TickMin, collect.TickMax, collect.TickAvg, collect.TickCount)
	logger.Info("[LOADTEST] Memory: Alloc=%d MB, HeapObjects=%d, Goroutines=%d",
		collect.Alloc/1_000_000, collect.HeapObjects, collect.Goroutines)
	logger.Info("[LOADTEST] Network: PacketsIn=%d, PacketsOut=%d, BytesIn=%d, BytesOut=%d",
		collect.PacketsIn, collect.PacketsOut, collect.BytesIn/1024, collect.BytesOut/1024)
	logger.Info("[LOADTEST] ═══════════════════════════════════════════")

	return fmt.Sprintf("[LOADTEST] Complete: %d bots + %d monsters ran for %v. %s",
		cfg.BotCount, cfg.MonsterCount, cfg.Duration, report)
}
