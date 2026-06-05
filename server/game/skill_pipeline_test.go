package game

import (
	"os"
	"server/ecs"
	"server/models"
	"server/peakgo/combat"
	"server/world"
	"testing"
)

func init() {
	combat.DisableRngForTesting = true
	models.InitializeSkillRegistry()

	// Load a mock monster template for combat testing
	jsonContent := `[
		{"id": 1, "name": "Orc", "hp": 100, "damage": 10, "spawn_x": 50, "spawn_z": 50, "roam_radius": 5, "aggro_radius": 6.0, "attack_cooldown": 4, "xp_reward": 50}
	]`
	tmpFile, err := os.CreateTemp("", "monster_templates_pipeline_test.json")
	if err == nil {
		defer os.Remove(tmpFile.Name())
		_, _ = tmpFile.WriteString(jsonContent)
		_ = tmpFile.Close()
		_, _ = models.LoadMonster(tmpFile.Name())
	}
}

func setupPipelineTestEntities(t testing.TB) (playerID, monsterID ecs.Entity) {
	t.Helper()
	registry := ecs.DefaultRegistry
	playerID = registry.NewEntity()
	monsterID = registry.NewEntity()

	registry.SetMetadata(playerID, ecs.MetadataComponent{Name: "Hero", Type: ecs.EntityPlayer})
	registry.SetStats(playerID, ecs.StatsComponent{
		Level: 1, HP: 100, MaxHP: 100, MP: 100, MaxMP: 100,
		XP: 0, Dam: 15, Attack: 15, MagicAttack: 15,
		HitRate: 800, DodgeRate: 100, CritRate: 50, CritDamage: 1500,
		Defense: 10, MagicDefense: 10,
	})
	registry.SetPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})
	world.GlobalSpatialGrid.UpdateEntityPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})

	registry.SetMetadata(monsterID, ecs.MetadataComponent{Name: "Orc", Type: ecs.EntityMonster})
	registry.SetStats(monsterID, ecs.StatsComponent{
		Level: 1, HP: 100, MaxHP: 100, MP: 50, MaxMP: 50,
		Dam: 5, Attack: 5, MagicAttack: 3,
		HitRate: 700, DodgeRate: 80, CritRate: 30, CritDamage: 1500,
		Defense: 5, MagicDefense: 3,
	})
	registry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})

	registry.SetAI(monsterID, ecs.AIComponent{
		State:       ecs.AIStateIdle,
		SpawnX:      11,
		SpawnZ:      11,
		SpawnRadius: 5,
		AggroRadius: 8.0,
		LeashRadius: 15,
		MeleeRange:  2,
	})

	return
}

func cleanupPipelineEntities(playerID, monsterID ecs.Entity) {
	world.GlobalSpatialGrid.RemoveEntity(playerID)
	world.GlobalSpatialGrid.RemoveEntity(monsterID)
	ecs.DefaultRegistry.RemoveEntity(playerID)
	ecs.DefaultRegistry.RemoveEntity(monsterID)
}

func TestPipelineSelfAttack(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	pipeline := NewSkillPipeline()
	_, errMsg := pipeline.Execute(playerID, playerID, 0)
	if errMsg == "" {
		t.Fatal("Expected error when attacking self, got none")
	}
}

func TestPipelineOutOfRange(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	// Move monster out of range
	ecs.DefaultRegistry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 100, Z: 100})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 100, Z: 100})

	pipeline := NewSkillPipeline()
	_, errMsg := pipeline.Execute(playerID, monsterID, 0)
	if errMsg == "" {
		t.Fatal("Expected error when target out of range, got none")
	}
}

func TestPipelineValidAttack(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	pipeline := NewSkillPipeline()
	result, errMsg := pipeline.Execute(playerID, monsterID, 0)
	if errMsg != "" {
		t.Fatalf("Expected valid attack to succeed, got error: %s", errMsg)
	}
	if !result.Hit || result.Damage < 1 {
		t.Fatalf("Expected hit with damage > 0, got hit=%t damage=%d", result.Hit, result.Damage)
	}

	// Verify HP reduced
	stats, ok := ecs.DefaultRegistry.GetStats(monsterID)
	if !ok || stats.HP >= 100 {
		t.Fatalf("Expected HP reduced, got stats ok=%t HP=%d", ok, stats.HP)
	}
}

func TestPipelineKillingBlow(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	// Set monster HP to 1 so it dies
	stats, _ := ecs.DefaultRegistry.GetStats(monsterID)
	stats.HP = 1
	ecs.DefaultRegistry.SetStats(monsterID, stats)

	// Reset respawn queue size
	GlobalRespawnManager.mu.Lock()
	initialEvents := len(GlobalRespawnManager.events)
	GlobalRespawnManager.mu.Unlock()

	pipeline := NewSkillPipeline()
	result, errMsg := pipeline.Execute(playerID, monsterID, 0)
	if errMsg != "" {
		t.Fatalf("Expected killing blow to succeed, got error: %s", errMsg)
	}
	if !result.Killed {
		t.Fatal("Expected CombatResult to indicate target was killed")
	}

	// Verify monster removed from registry
	_, ok := ecs.DefaultRegistry.GetMetadata(monsterID)
	if ok {
		t.Error("Expected monster metadata to be deleted after kill")
	}

	// Verify XP rewarded
	playerStats, _ := ecs.DefaultRegistry.GetStats(playerID)
	if playerStats.XP != 50 {
		t.Errorf("Expected player to gain 50 XP, got %d", playerStats.XP)
	}

	// Verify respawn event scheduled
	GlobalRespawnManager.mu.Lock()
	postEvents := len(GlobalRespawnManager.events)
	GlobalRespawnManager.mu.Unlock()
	if postEvents != initialEvents+1 {
		t.Errorf("Expected 1 new respawn event, got initial=%d post=%d", initialEvents, postEvents)
	}
}

func TestPipelineSkillFireball(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	// Ensure monste is in skill cast range
	ecs.DefaultRegistry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 13, Z: 13})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 13, Z: 13})

	// Give player enough MP
	stats, _ := ecs.DefaultRegistry.GetStats(playerID)
	stats.MP = 50
	ecs.DefaultRegistry.SetStats(playerID, stats)

	pipeline := NewSkillPipeline()
	result, errMsg := pipeline.Execute(playerID, monsterID, 1) // Fireball
	if errMsg != "" {
		t.Fatalf("Expected Fireball to succeed, got error: %s", errMsg)
	}
	if !result.Hit || result.Damage < 1 {
		t.Fatalf("Expected hit with damage > 0 from Fireball, got hit=%t damage=%d", result.Hit, result.Damage)
	}

	// Verify MP consumed
	playerStats, _ := ecs.DefaultRegistry.GetStats(playerID)
	if playerStats.MP != 30 { // Started with 50, fireball costs 20
		t.Errorf("Expected MP to be 30 after Fireball, got %d", playerStats.MP)
	}
}

func TestPipelineSkillInsufficientMana(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	// Ensure monster is in range
	ecs.DefaultRegistry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 13, Z: 13})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 13, Z: 13})

	// Set MP to 0
	stats, _ := ecs.DefaultRegistry.GetStats(playerID)
	stats.MP = 0
	ecs.DefaultRegistry.SetStats(playerID, stats)

	pipeline := NewSkillPipeline()
	_, errMsg := pipeline.Execute(playerID, monsterID, 1) // Fireball costs 20 MP
	if errMsg == "" {
		t.Fatal("Expected error when casting with insufficient mana, got none")
	}
}

func TestPipelineInvalidSkill(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	pipeline := NewSkillPipeline()
	_, errMsg := pipeline.Execute(playerID, monsterID, 999) // Non-existent skill
	if errMsg == "" {
		t.Fatal("Expected error when casting invalid skill, got none")
	}
}

func TestPipelineThreatTracking(t *testing.T) {
	playerID, monsterID := setupPipelineTestEntities(t)
	defer cleanupPipelineEntities(playerID, monsterID)

	pipeline := NewSkillPipeline()
	_, errMsg := pipeline.Execute(playerID, monsterID, 0)
	if errMsg != "" {
		t.Fatalf("Expected attack to succeed, got error: %s", errMsg)
	}

	// Verify threat was recorded
	ai, hasAI := ecs.DefaultRegistry.GetAI(monsterID)
	if !hasAI {
		t.Fatal("Expected monster to have AI component")
	}
	if ai.ThreatTable == nil {
		t.Fatal("Expected threat table to be created")
	}
	if ai.ThreatTable.Len() == 0 {
		t.Fatal("Expected at least one threat entry")
	}

	topID, _ := ai.ThreatTable.Top()
	if topID != uint64(playerID) {
		t.Errorf("Expected player %d as top threat, got %d", playerID, topID)
	}
}

func BenchmarkPipelineExecute(b *testing.B) {
	playerID, monsterID := setupPipelineTestEntities(b)
	defer cleanupPipelineEntities(playerID, monsterID)

	pipeline := NewSkillPipeline()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipeline.Execute(playerID, monsterID, 0)
	}
}

func BenchmarkPipelineExecuteAllocs(b *testing.B) {
	playerID, monsterID := setupPipelineTestEntities(b)
	defer cleanupPipelineEntities(playerID, monsterID)

	pipeline := NewSkillPipeline()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipeline.Execute(playerID, monsterID, 0)
	}
}
