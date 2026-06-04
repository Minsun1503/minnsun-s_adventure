package game

import (
	"server/ecs"
	"server/world"
	"testing"
)

// setupCombatBenchEntities creates a player and monster at melee range for benchmarking.
func setupCombatBenchEntities(b *testing.B) (ecs.Entity, ecs.Entity) {
	b.Helper()
	registry := ecs.GlobalRegistry
	playerID := registry.NewEntity()
	monsterID := registry.NewEntity()

	registry.SetMetadata(playerID, ecs.MetadataComponent{Name: "BenchHero", Type: ecs.EntityPlayer})
	registry.SetStats(playerID, ecs.StatsComponent{
		Level: 50, HP: 5000, MaxHP: 5000, XP: 0,
		Dam: 200, Attack: 200, MagicAttack: 150,
		Defense: 100, MagicDefense: 80,
		HitRate: 850, DodgeRate: 50, CritRate: 100, CritDamage: 1500,
	})
	registry.SetPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})
	world.GlobalSpatialGrid.UpdateEntityPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})

	registry.SetMetadata(monsterID, ecs.MetadataComponent{Name: "BenchOrc", Type: ecs.EntityMonster})
	registry.SetStats(monsterID, ecs.StatsComponent{
		Level: 50, HP: 10000, MaxHP: 10000,
		Dam: 100, Attack: 100, Defense: 80,
		HitRate: 700, DodgeRate: 80, CritRate: 50, CritDamage: 1500,
	})
	registry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})

	// Set AI for threat tracking
	registry.SetAI(monsterID, ecs.AIComponent{
		State:       ecs.AIStateIdle,
		SpawnX:      11,
		SpawnZ:      11,
		SpawnRadius: 5,
		AggroRadius: 8.0,
		LeashRadius: 15,
		MeleeRange:  2,
	})

	return playerID, monsterID
}

// cleanupCombatBenchEntities removes both entities after benchmarking.
func cleanupCombatBenchEntities(playerID, monsterID ecs.Entity) {
	world.GlobalSpatialGrid.RemoveEntity(playerID)
	world.GlobalSpatialGrid.RemoveEntity(monsterID)
	ecs.GlobalRegistry.RemoveEntity(playerID)
	ecs.GlobalRegistry.RemoveEntity(monsterID)
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkAttackSystem(b *testing.B) {
	playerID, monsterID := setupCombatBenchEntities(b)
	defer cleanupCombatBenchEntities(playerID, monsterID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AttackSystem(playerID, monsterID)
	}
}

func BenchmarkAttackSystemMiss(b *testing.B) {
	// Monster out of range — test the rejection fast-path
	playerID, monsterID := setupCombatBenchEntities(b)
	defer cleanupCombatBenchEntities(playerID, monsterID)

	// Move monster out of range
	ecs.GlobalRegistry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 100, Z: 100})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 100, Z: 100})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AttackSystem(playerID, monsterID)
	}
}

func BenchmarkDamageSystem(b *testing.B) {
	registry := ecs.GlobalRegistry
	monsterID := registry.NewEntity()
	registry.SetStats(monsterID, ecs.StatsComponent{HP: 10000, MaxHP: 10000})
	defer registry.RemoveEntity(monsterID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DamageSystem(monsterID, 100)
	}
}

func BenchmarkDeathSystemMonster(b *testing.B) {
	playerID, monsterID := setupCombatBenchEntities(b)
	defer cleanupCombatBenchEntities(playerID, monsterID)

	// Set monster HP to 1 so it dies
	stats, _ := ecs.GlobalRegistry.GetStats(monsterID)
	stats.HP = 1
	ecs.GlobalRegistry.SetStats(monsterID, stats)

	targetMeta, _ := ecs.GlobalRegistry.GetMetadata(monsterID)
	attackerMeta, _ := ecs.GlobalRegistry.GetMetadata(playerID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DeathSystem(monsterID, playerID, targetMeta, attackerMeta, 9999)
	}
}

func BenchmarkStatsToCombatStats(b *testing.B) {
	s := ecs.StatsComponent{
		Level: 50, HP: 5000, MaxHP: 5000, Dam: 200,
		Attack: 200, MagicAttack: 150, Defense: 100,
		MagicDefense: 80, HitRate: 850, DodgeRate: 50,
		CritRate: 100, CritDamage: 1500,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statsToCombatStats(s)
	}
}

// ─── Memory allocation benchmarks ─────────────────────────────────────────────

func BenchmarkAttackSystemAllocs(b *testing.B) {
	playerID, monsterID := setupCombatBenchEntities(b)
	defer cleanupCombatBenchEntities(playerID, monsterID)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AttackSystem(playerID, monsterID)
	}
}

func BenchmarkDamageSystemAllocs(b *testing.B) {
	registry := ecs.GlobalRegistry
	monsterID := registry.NewEntity()
	registry.SetStats(monsterID, ecs.StatsComponent{HP: 10000, MaxHP: 10000})
	defer registry.RemoveEntity(monsterID)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DamageSystem(monsterID, 100)
	}
}
