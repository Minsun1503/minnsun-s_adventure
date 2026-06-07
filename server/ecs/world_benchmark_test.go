package ecs

import (
	"runtime"
	"server/peakgo/rng"
	"testing"
)

// BenchmarkWorldScale measures ECS registry throughput under load.
// Metrics: ns/op, B/op, allocs/op plus runtime.ReadMemStats and NumGoroutine.
//
// Three load profiles:
//   - Small-100p-500m:   100 players,   500 monsters  (small active server)
//   - Medium-500p-3000m:  500 players,  3000 monsters  (medium community server)
//   - Large-1000p-10000m: 1000 players, 10000 monsters (MMO launch target)
//
// Each profile runs a simulated game loop tick:
//   - QueryPositionStats (pos+stats combined range — represents AOI/combat tick)
//   - QueryPositionAI     (monster AI iteration — the hot-path AI pass)
//   - QueryPositionMetadata (initial snapshot — hasPlayers check)
//
// It also captures runtime.ReadMemStats and runtime.NumGoroutine() before
// and after each sub-benchmark to detect heap growth and goroutine leaks.
func BenchmarkWorldScale(b *testing.B) {
	scenarios := []struct {
		name     string
		players  int
		monsters int
	}{
		{"Small-100p-500m", 100, 500},
		{"Medium-500p-3000m", 500, 3000},
		{"Large-1000p-10000m", 1000, 10000},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			// Fresh registry for each sub-benchmark.
			reg := &Registry{}

			// Capture pre-setup memory/goroutine stats.
			var memBefore runtime.MemStats
			runtime.ReadMemStats(&memBefore)
			goBefore := runtime.NumGoroutine()

			// Spawn players.
			for i := 0; i < s.players; i++ {
				eid := reg.NewEntity()
				reg.SetMetadata(eid, MetadataComponent{Name: "bench_player", Type: EntityPlayer})
				reg.SetPosition(eid, PositionComponent{X: rng.Intn(500), Z: rng.Intn(500), MapID: 1})
				reg.SetStats(eid, StatsComponent{HP: 100, MaxHP: 100, MP: 50, MaxMP: 50})
			}

			// Spawn monsters.
			for i := 0; i < s.monsters; i++ {
				eid := reg.NewEntity()
				reg.SetMetadata(eid, MetadataComponent{Name: "bench_monster", Type: EntityMonster})
				reg.SetPosition(eid, PositionComponent{X: rng.Intn(500), Z: rng.Intn(500), MapID: 1})
				reg.SetStats(eid, StatsComponent{HP: 50, MaxHP: 50})
				reg.SetAI(eid, AIComponent{
					State:       AIStateIdle,
					SpawnX:      250,
					SpawnZ:      250,
					SpawnRadius: 100,
					AggroRadius: 80,
					LeashRadius: 200,
					MeleeRange:  2,
				})
			}

			// Capture post-setup memory/goroutine stats.
			var memAfterSetup runtime.MemStats
			runtime.ReadMemStats(&memAfterSetup)
			goAfterSetup := runtime.NumGoroutine()

			// Reset timer — the setup allocations are excluded from metrics.
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// 1. Simulate initial snapshot pass (hasPlayers + monster state logging).
				reg.QueryPositionMetadata(func(id Entity, meta MetadataComponent, pos PositionComponent) bool {
					_ = id
					_ = meta
					_ = pos
					return true
				})

				// 2. Simulate one game loop tick: QueryPositionStats (AOI/combat hot-path).
				reg.QueryPositionStats(func(id Entity, pos PositionComponent, stats StatsComponent) bool {
					_ = id
					_ = pos
					_ = stats
					return true
				})

				// 3. Process AI for monsters via optimized QueryPositionAI.
				reg.QueryPositionAI(func(id Entity, ai AIComponent, pos PositionComponent, stats StatsComponent) bool {
					_ = id
					_ = ai
					_ = pos
					_ = stats
					return true
				})
			}

			// Capture post-benchmark memory/goroutine stats.
			var memAfter runtime.MemStats
			runtime.ReadMemStats(&memAfter)
			goAfter := runtime.NumGoroutine()

			// Report memory growth (HeapInuse delta in MB) and goroutine delta.
			heapSetupMB := float64(memAfterSetup.HeapInuse-memBefore.HeapInuse) / 1024 / 1024
			heapGrowthMB := float64(memAfter.HeapInuse-memAfterSetup.HeapInuse) / 1024 / 1024
			goroutineLeak := goAfter - goAfterSetup

			b.ReportMetric(heapSetupMB, "heap_setup_mb")
			b.ReportMetric(heapGrowthMB, "heap_growth_mb")
			b.ReportMetric(float64(goBefore), "go_before")
			b.ReportMetric(float64(goAfterSetup), "go_setup")
			b.ReportMetric(float64(goroutineLeak), "go_leak")
		})
	}
}
