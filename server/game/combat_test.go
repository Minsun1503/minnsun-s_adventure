package game

import (
	"os"
	"server/ecs"
	"server/models"
	"server/world"
	"testing"
)

func init() {
	// Load a mock monster template for combat testing
	jsonContent := `[
		{"id": 1, "name": "Orc", "hp": 100, "damage": 10, "spawn_x": 50, "spawn_z": 50, "roam_radius": 5, "aggro_radius": 6.0, "attack_cooldown": 4, "xp_reward": 50}
	]`
	tmpFile, err := os.CreateTemp("", "monster_templates_test.json")
	if err == nil {
		defer os.Remove(tmpFile.Name())
		_, _ = tmpFile.WriteString(jsonContent)
		_ = tmpFile.Close()
		_, _ = models.LoadMonster(tmpFile.Name())
	}
}

func TestAttackSystemValidations(t *testing.T) {
	registry := ecs.GlobalRegistry
	playerID := registry.NewEntity()
	monsterID := registry.NewEntity()

	// 1. Setup stats & metadata
	registry.SetMetadata(playerID, ecs.MetadataComponent{Name: "Hero", Type: "player"})
	registry.SetStats(playerID, ecs.StatsComponent{Level: 1, HP: 100, MaxHP: 100, XP: 0, Dam: 15})
	registry.SetPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})
	world.GlobalSpatialGrid.UpdateEntityPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})

	registry.SetMetadata(monsterID, ecs.MetadataComponent{Name: "Orc", Type: "monster"})
	registry.SetStats(monsterID, ecs.StatsComponent{Level: 1, HP: 100, MaxHP: 100, Dam: 5})
	registry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11}) // Melee range
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})

	// 2. Test self-attack
	_, errStr := AttackSystem(playerID, playerID)
	if errStr == "" {
		t.Error("Expected error when attacking self, got none")
	}

	// 3. Test out-of-range attack
	registry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 30, Z: 30}) // Out of range
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 30, Z: 30})

	_, errStr = AttackSystem(playerID, monsterID)
	if errStr == "" {
		t.Error("Expected error when target is out of range, got none")
	}

	// 4. Test valid attack range
	registry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 12, Z: 12}) // Back in range (distance sqrt(8) ~ 2.8 <= 5.0)
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 12, Z: 12})

	result, errStr := AttackSystem(playerID, monsterID)
	if errStr != "" {
		t.Errorf("Expected valid attack to succeed, got error: %s", errStr)
	}
	if !result.Hit || result.Damage != 15 {
		t.Errorf("Expected hit with 15 damage, got hit=%t damage=%d", result.Hit, result.Damage)
	}

	// Verify remaining HP
	stats, ok := registry.GetStats(monsterID)
	if !ok || stats.HP != 85 {
		t.Errorf("Expected target HP to be 85, got stats ok=%t HP=%d", ok, stats.HP)
	}
}

func TestAttackSystemKillingBlow(t *testing.T) {
	registry := ecs.GlobalRegistry
	playerID := registry.NewEntity()
	monsterID := registry.NewEntity()

	// Initialize entities
	registry.SetMetadata(playerID, ecs.MetadataComponent{Name: "Hero", Type: "player"})
	registry.SetStats(playerID, ecs.StatsComponent{Level: 1, HP: 100, MaxHP: 100, XP: 0, Dam: 15})
	registry.SetPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})
	world.GlobalSpatialGrid.UpdateEntityPosition(playerID, ecs.PositionComponent{MapID: 1, X: 10, Z: 10})

	registry.SetMetadata(monsterID, ecs.MetadataComponent{Name: "Orc", Type: "monster"})
	registry.SetStats(monsterID, ecs.StatsComponent{Level: 1, HP: 10, MaxHP: 10, Dam: 5}) // 10 HP, player deals 15
	registry.SetPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})
	world.GlobalSpatialGrid.UpdateEntityPosition(monsterID, ecs.PositionComponent{MapID: 1, X: 11, Z: 11})

	// Reset respawn queue size
	GlobalRespawnManager.mu.Lock()
	initialEvents := len(GlobalRespawnManager.events)
	GlobalRespawnManager.mu.Unlock()

	// Kill monster
	result, errStr := AttackSystem(playerID, monsterID)
	if errStr != "" {
		t.Fatalf("Expected valid killing blow to succeed, got error: %s", errStr)
	}
	if !result.Killed {
		t.Error("Expected CombatResult to indicate target was killed")
	}

	// Verify monster is removed from registry
	_, ok := registry.GetMetadata(monsterID)
	if ok {
		t.Error("Expected monster metadata to be deleted from registry")
	}

	// Verify monster is removed from spatial grid
	_, ok = world.GlobalSpatialGrid.GetEntityChunk(monsterID)
	if ok {
		t.Error("Expected monster to be removed from spatial grid")
	}

	// Verify XP was rewarded (Orc template gives 50 XP)
	playerStats, _ := registry.GetStats(playerID)
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
