package systems

import (
	"server/ecs"
)

// RecalculateActiveStats aggregates base attributes and equipment modifications
// to update the final authoritative StatsComponent row inline.
func RecalculateActiveStats(playerID ecs.Entity) {
	// 1. Establish hardcoded default base stats for a level 1 player character
	// Matching default initialization inside models/player.go: MaxHP: 100, Dam: 15
	baseMaxHP := 100
	baseDamage := 15

	// 2. Fetch equipment component layout data columns
	eq, hasEq := ecs.GlobalRegistry.GetEquipment(playerID)
	
	// If equipment row exists, accumulate bonus values from registries
	if hasEq {
		if weapon, exists := ItemRegistry[eq.WeaponID]; exists {
			baseDamage += weapon.BonusDam
		}
		if armor, exists := ItemRegistry[eq.ArmorID]; exists {
			baseMaxHP += armor.BonusHP
		}
	}

	// 3. COPY existing stats to preserve current dynamic health status tracking
	currentStats, hasStats := ecs.GlobalRegistry.GetStats(playerID)
	if !hasStats {
		// If brand new, spawn fresh structure blocks
		currentStats = ecs.StatsComponent{HP: baseMaxHP, MaxHP: baseMaxHP, Dam: baseDamage}
	}

	// 4. MODIFY: Overwrite upper limitations and active attributes safely
	currentStats.MaxHP = baseMaxHP
	currentStats.Dam = baseDamage

	// Guardrail: If item swaps lowered MaxHP below current health pools, clamp it
	if currentStats.HP > currentStats.MaxHP {
		currentStats.HP = currentStats.MaxHP
	}

	// 5. OVERWRITE: Force-push values back into your lock-free database columns
	ecs.GlobalRegistry.SetStats(playerID, currentStats)
}
