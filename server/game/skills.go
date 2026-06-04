package game

import (
	"fmt"
	"server/ecs"
	"server/models"
	"server/protocol"
	"server/world"
)

// HandleSkillCastingSystem processes a player's skill casting logic.
func HandleSkillCastingSystem(casterID ecs.Entity, skillID uint64, targetID ecs.Entity) (string, bool) {
	registry := ecs.GlobalRegistry

	if casterID == targetID {
		return "Error: You cannot cast spells on yourself.\r\n", false
	}

	// 1. VERIFY STATIC SKILL CONFIGURATION
	skill, skillExists := models.SkillRegistry[skillID]
	if !skillExists {
		return "Error: That skill does not exist in the server registry!\r\n", false
	}

	// 2. COPY & VALIDATE CASTER STATS (RESOURCE GATE CHECK)
	casterStats, hasCasterStats := registry.GetStats(casterID)
	if !hasCasterStats {
		return "Error: Your character stats profile was not found.\r\n", false
	}
	if casterStats.MP < skill.ManaCost {
		return fmt.Sprintf("Mana Insufficient! Required: %d MP | You have: %d MP\r\n", skill.ManaCost, casterStats.MP), false
	}

	// 3. COPY & VALIDATE TARGET DATA ROWS
	targetMeta, targetMetaOk := registry.GetMetadata(targetID)
	targetStats, targetStatsOk := registry.GetStats(targetID)
	if !targetMetaOk || !targetStatsOk || targetStats.HP <= 0 {
		return "Error: Target is invalid or already dead!\r\n", false
	}

	// Proximity check: cast range is 6.0 units
	const castRange = 6.0
	if !world.IsInRange(casterID, targetID, castRange) {
		return fmt.Sprintf("Range Fault: Target is out of spellcast range (%.0f units).\r\n", castRange), false
	}

	// 4. ATOMIC COST DEDUCTION & OVERWRITE (CASTER)
	casterStats.MP -= skill.ManaCost
	registry.SetStats(casterID, casterStats)

	// 5. MULTIPLIER DAMAGE PROJECTION & OVERWRITE (TARGET)
	damageCalculated := int(float64(casterStats.Dam) * skill.DamMult)
	remainingHP := DamageSystem(targetID, damageCalculated)

	casterMeta, _ := registry.GetMetadata(casterID)
	pos, _ := registry.GetPosition(casterID)

	// 6. NOTIFY/BROADCAST PIPELINE
	var combatNotice string
	if remainingHP <= 0 {
		combatNotice = fmt.Sprintf("[SPELL] Player %s unleashed %s on %s dealing %d damage and DEFEATED them!\r\n",
			casterMeta.Name, skill.Name, targetMeta.Name, damageCalculated)
		// Sync local map witnesses visually
		protocol.BroadcastToNeighbors(pos, []byte(combatNotice), casterID)

		DeathSystem(targetID, casterID, targetMeta, casterMeta, damageCalculated)
	} else {
		combatNotice = fmt.Sprintf("[SPELL] Player %s casted %s on %s for %d damage! (%s HP: %d)\r\n",
			casterMeta.Name, skill.Name, targetMeta.Name, damageCalculated, targetMeta.Name, remainingHP)
		// Sync local map witnesses visually
		protocol.BroadcastToNeighbors(pos, []byte(combatNotice), casterID)
	}

	personalFeedback := fmt.Sprintf("Spent -%d MP. (Current Reserves: %d/%d MP)\r\n", skill.ManaCost, casterStats.MP, casterStats.MaxMP)
	return personalFeedback, true
}
