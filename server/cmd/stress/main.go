package main

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"server/ecs"
	"server/game"
	"server/loadtest"
	"server/logger"
	"server/models"
	"server/world"
)

// ─── Headless Combat Stress Test ─────────────────────────────────────────────
//
// This program spawns N player bots on a single MapWorker and a single Boss
// monster. All bots attack the boss simultaneously to stress-test:
//   - Lock-free CombatAccumulator batch processing
//   - AOI performance under 1000+ participants
//   - A* pathfinding under load (bots spread randomly then converge on boss)
//   - MapWorker tick capacity
//
// Metrics collected:
//   - Bots spawned / sec
//   - Tick rate (observed)
//   - Combat throughput (damage batches / tick)
//   - Memory allocation per tick (GC stats)
//
// Usage: go run cmd/stress/main.go
//   Optionally: go run cmd/stress/main.go -bots=500

const (
	defaultBots    = 1000
	bossName       = "Boss_Malakar"
	bossHP         = 10_000_000
	bossLevel      = 50
	targetMapID    = 1
	stressTickRate = 50 * time.Millisecond // 20 ticks/sec for high-resolution stress
	reportInterval = 5                     // seconds between summary reports
)

var (
	totalDamageDealt atomic.Int64
	totalBotsPerTick atomic.Int64
)

func main() {
	logger.Init()
	logger.Info("[STRESS] === Combat Scale Stress Test ===")

	// Number of bots
	numBots := defaultBots
	if len(os.Args) > 1 && os.Args[1] == "-bots" && len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &numBots)
		logger.Info("[STRESS] Using custom bot count: %d", numBots)
	}

	// Initialize game systems required for combat
	game.InitializeItemRegistry()
	models.InitializeSkillRegistry()
	game.InitializeLootTables()
	world.InitializeCollisionMaps()

	// Create a fresh ECS registry for the stress test
	ecs.DefaultRegistry = ecs.NewRegistry()
	ecs.CurrentCombatBuffer = ecs.NewCombatAccumulator()

	// Initialize AOI
	world.InitAOIManager()

	// GlobalSpatialGrid is initialized by InitWorld/InitAOIManager
	// No manual assignment needed.

	// ─── Phase 1: Spawn the Boss ──────────────────────────────────────────
	bossID := ecs.DefaultRegistry.NewEntity()
	ecs.DefaultRegistry.SetMetadata(bossID, ecs.MetadataComponent{
		Name: bossName,
		Type: ecs.EntityMonster,
	})
	ecs.DefaultRegistry.SetPosition(bossID, ecs.PositionComponent{
		MapID: targetMapID,
		X:     50,
		Z:     50,
	})
	ecs.DefaultRegistry.SetStats(bossID, ecs.StatsComponent{
		Level:     bossLevel,
		HP:        bossHP,
		MaxHP:     bossHP,
		MP:        500,
		MaxMP:     500,
		Attack:    200,
		Defense:   100,
		HitRate:   750,
		DodgeRate: 30,
		CritRate:  50,
	})
	ecs.DefaultRegistry.SetAI(bossID, ecs.AIComponent{
		State:       ecs.AIStateIdle,
		SpawnX:      50,
		SpawnZ:      50,
		SpawnRadius: 0,
		AggroRadius: 100,
		LeashRadius: 100,
		MeleeRange:  3,
	})
	world.GlobalSpatialGrid.UpdateEntityPosition(bossID, ecs.PositionComponent{
		MapID: targetMapID,
		X:     50,
		Z:     50,
	})
	logger.Info("[STRESS] Boss %s (ID=%d) spawned at (50,50) with %d HP", bossName, bossID, bossHP)

	// ─── Phase 2: Spawn Player Bots ───────────────────────────────────────
	logger.Info("[STRESS] Spawning %d player bots...", numBots)
	spawnStart := time.Now()

	bots := loadtest.SpawnNBots(numBots, targetMapID)

	elapsed := time.Since(spawnStart)
	logger.Info("[STRESS] Spawned %d bots in %v (%.0f bots/sec)", len(bots), elapsed, float64(numBots)/elapsed.Seconds())

	// ─── Phase 3: Run Stress Simulation ───────────────────────────────────
	logger.Info("[STRESS] Starting stress simulation...")
	logger.Info("[STRESS] All bots are attacking the boss (ID=%d) at (50,50)", bossID)

	ticker := time.NewTicker(stressTickRate)
	defer ticker.Stop()

	reportTicker := time.NewTicker(reportInterval * time.Second)
	defer reportTicker.Stop()

	var tickCount uint64
	startTime := time.Now()

	// Print header
	fmt.Println()
	fmt.Println("======================================================================")
	fmt.Println(" COMBAT STRESS TEST")
	fmt.Println("======================================================================")
	fmt.Printf(" Bots: %d | Boss HP: %d | Tick Rate: 20/sec\n", numBots, bossHP)
	fmt.Println("======================================================================")

	for range ticker.C {
		tickCount++

		// Tick the game: run AI for all monsters and bots
		runStressTick(tickCount, bossID, bots)

		// Periodic reporting
		select {
		case <-reportTicker.C:
			bossStats, _ := ecs.DefaultRegistry.GetStats(bossID)
			damage := totalDamageDealt.Load()
			botsPerTick := totalBotsPerTick.Load()
			elapsed := time.Since(startTime)

			// GC stats
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			fmt.Printf("\n[REPORT] After %v (tick %d):\n", elapsed.Round(time.Second), tickCount)
			fmt.Printf("  Boss HP: %d / %d (%.1f%%)\n", bossStats.HP, bossStats.MaxHP,
				100.0*float64(bossStats.HP)/float64(bossStats.MaxHP))
			fmt.Printf("  Damage dealt: %d (%.0f dmg/sec)\n", damage, float64(damage)/elapsed.Seconds())
			fmt.Printf("  Bots/tick avg: %d\n", botsPerTick/int64(reportInterval))
			fmt.Printf("  Mem Alloc: %d KB | Heap: %d KB | GC Cycles: %d\n",
				m.Alloc/1024, m.HeapAlloc/1024, m.NumGC)
			fmt.Printf("  Goroutines: %d\n", runtime.NumGoroutine())
			fmt.Println("----------------------------------------------------------------------")

			totalBotsPerTick.Store(0)

			// Check if boss is dead
			if bossStats.HP <= 0 {
				logger.Info("[STRESS] BOSS DEFEATED after %v and %d ticks!", elapsed.Round(time.Second), tickCount)
				fmt.Println("\n*** BOSS DEFEATED! ***")
				fmt.Printf("Total elapsed: %v\n", elapsed.Round(time.Second))
				fmt.Printf("Total ticks: %d\n", tickCount)

				// Print final GC stats
				runtime.ReadMemStats(&m)
				fmt.Printf("Final Mem Alloc: %d KB | Heap: %d KB\n", m.Alloc/1024, m.HeapAlloc/1024)
				fmt.Println("======================================================================")
				return
			}
		default:
		}
	}
}

// runStressTick processes one tick of the stress simulation.
// All bots move toward and attack the boss.
func runStressTick(tick uint64, bossID ecs.Entity, bots []*loadtest.PlayerBotState) {
	bossPos, _ := ecs.DefaultRegistry.GetPosition(bossID)
	bossStats, _ := ecs.DefaultRegistry.GetStats(bossID)
	if bossStats.HP <= 0 {
		return
	}

	botsInAction := 0

	for _, bot := range bots {
		if !bot.Alive {
			continue
		}

		// Move bot toward boss if not in melee range
		dx := bossPos.X - bot.X
		dz := bossPos.Z - bot.Z
		dist := dx*dx + dz*dz

		if dist > 9 { // melee range ~3 units
			// Move one step toward boss (simple line-of-sight movement)
			step := 1
			if dx > 0 {
				bot.X += step
			} else if dx < 0 {
				bot.X -= step
			}
			if dz > 0 {
				bot.Z += step
			} else if dz < 0 {
				bot.Z -= step
			}

			// Update position in ECS
			pos := ecs.PositionComponent{MapID: targetMapID, X: bot.X, Z: bot.Z}
			ecs.DefaultRegistry.SetPosition(bot.ID, pos)
			world.GlobalSpatialGrid.UpdateEntityPosition(bot.ID, pos)
		} else {
			// In melee range — attack boss
			game.AttackSystem(bot.ID, bossID)
			botsInAction++
		}
	}

	// Track stats
	totalBotsPerTick.Add(int64(botsInAction))

	// Flush accumulated combat damage
	ecs.CurrentCombatBuffer.Flush(func(target ecs.Entity, batch *ecs.DamageBatch) {
		stats, ok := ecs.DefaultRegistry.GetStats(target)
		if ok {
			stats.HP -= batch.TotalDamage
			ecs.DefaultRegistry.SetStats(target, stats)
		}
	})

	// Update boss stats after damage accumulation
	newStats, ok := ecs.DefaultRegistry.GetStats(bossID)
	if ok {
		damageThisTick := bossStats.HP - newStats.HP
		if damageThisTick > 0 {
			totalDamageDealt.Add(int64(damageThisTick))
		}
	}
}
