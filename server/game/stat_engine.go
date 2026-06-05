package game

import (
	"server/ecs"
)

// RecalculateActiveStats aggregates base attributes and equipment modifications
// to update the final authoritative StatsComponent row inline.
func RecalculateActiveStats(playerID ecs.Entity) {
	currentStats, hasStats := ecs.DefaultRegistry.GetStats(playerID)

	baseMaxHP := 100
	baseDamage := 15

	if hasStats && currentStats.Level > 1 {
		baseMaxHP = 100 + (currentStats.Level-1)*50
		baseDamage = 15 + (currentStats.Level-1)*10
	} else if !hasStats {
		currentStats = ecs.StatsComponent{Level: 1, HP: baseMaxHP, MaxHP: baseMaxHP, Dam: baseDamage}
	}

	// 2. Fetch equipment component layout data columns
	eq, hasEq := ecs.DefaultRegistry.GetEquipment(playerID)

	// If equipment row exists, accumulate bonus values from registries
	if hasEq {
		if weapon, exists := ItemRegistry[eq.WeaponID]; exists {
			baseDamage += weapon.BonusDam
		}
		if armor, exists := ItemRegistry[eq.ArmorID]; exists {
			baseMaxHP += armor.BonusHP
		}
	}

	// 3. Overwrite upper limitations and active attributes safely
	currentStats.MaxHP = baseMaxHP
	currentStats.Dam = baseDamage
	currentStats.Attack = baseDamage

	// Guardrail: If item swaps lowered MaxHP below current health pools, clamp it
	if currentStats.HP > currentStats.MaxHP {
		currentStats.HP = currentStats.MaxHP
	}

	// 4. OVERWRITE: Force-push values back into your lock-free database columns
	ecs.DefaultRegistry.SetStats(playerID, currentStats)
}
