package ecs

import (
	"math/rand/v2"
	"testing"
)

// BenchmarkWorldScale measures ECS registry throughput under load.
// Simulation: 100 & 1000 player entities + 500 & 5000 monster entities.
// Metrics: ns/op, B/op, allocs/op.
//
// This benchmark tests the core ECS hot-path operations that dominate
// the game loop: QueryPositionStats (combined pos+stats range) and
// RangeAI (monster AI iteration). All allocations from ECS Range are
// expected to be zero.
func BenchmarkWorldScale(b *testing.B) {
	scenarios := []struct {
		name     string
		players  int
		monsters int
	}{
		{"Small-100p-500m", 100, 500},
		{"Large-1000p-5000m", 1000, 5000},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			// Fresh registry for each sub-benchmark.
			reg := &Registry{}

			// Spawn players.
			for i := 0; i < s.players; i++ {
				eid := reg.NewEntity()
				reg.SetMetadata(eid, MetadataComponent{Name: "bench_player", Type: EntityPlayer})
				reg.SetPosition(eid, PositionComponent{X: rand.IntN(500), Z: rand.IntN(500), MapID: 1})
				reg.SetStats(eid, StatsComponent{HP: 100, MaxHP: 100, MP: 50, MaxMP: 50})
			}

			// Spawn monsters.
			for i := 0; i < s.monsters; i++ {
				eid := reg.NewEntity()
				reg.SetMetadata(eid, MetadataComponent{Name: "bench_monster", Type: EntityMonster})
				reg.SetPosition(eid, PositionComponent{X: rand.IntN(500), Z: rand.IntN(500), MapID: 1})
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

			// Reset timer — the setup allocations are excluded from metrics.
			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Simulate one game loop tick: QueryPositionStats (hot-path).
				reg.QueryPositionStats(func(id Entity, pos PositionComponent, stats StatsComponent) bool {
					_ = id
					_ = pos
					_ = stats
					return true
				})

				// Process AI for monsters.
				reg.RangeAI(func(id Entity, _ AIComponent) bool {
					_ = id
					return true
				})
			}
		})
	}
}
