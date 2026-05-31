package game

import (
	"fmt"
	"math"
	"server/ecs"
	"server/protocol"
)

// CalculateXPThreshold evaluates the exponential progression curve equation.
// Formula: 100 * (Level ^ 1.8)
func CalculateXPThreshold(level int) uint64 {
	if level <= 0 {
		return 0
	}
	baseModifier := 100.0
	exponentCurve := 1.8

	totalRequired := baseModifier * math.Pow(float64(level), exponentCurve)
	return uint64(math.Floor(totalRequired))
}

// AddExperienceSystem safely injects XP into a player row, running cascading level checking steps.
func AddExperienceSystem(playerID ecs.Entity, xpGained uint64) {
	// 1. COPY: Pull active stat columns out of our lock-free database
	stats, hasStats := ecs.GlobalRegistry.GetStats(playerID)
	if !hasStats {
		return
	}

	meta, _ := ecs.GlobalRegistry.GetMetadata(playerID)
	
	// Add the incoming bounty to the pool
	stats.XP += xpGained
	didLevelUp := false

	// 2. THE LEVEL CASCADE LOOP: Handle single or multiple level jumps atomically
	for {
		nextLevelNeeded := CalculateXPThreshold(stats.Level)
		if stats.XP >= nextLevelNeeded {
			// Deduct the threshold expense from their pool
			stats.XP -= nextLevelNeeded
			stats.Level++
			didLevelUp = true
			
			// 3. STAT SCALING ALGORITHM: Apply stat mutations per level gained
			stats.MaxHP += 25 // Grant +25 max health allocation pool per level
			stats.MaxMP += 10 // Grant +10 max mana pool per level
			stats.Dam += 3    // Grant +3 base strike damage power per level
		} else {
			break // Exp total is verified within limits, break simulation loop
		}
	}

	// 4. OVERWRITE: If variables mutated, force synchronize health points and rewrite tables
	if didLevelUp {
		stats.HP = stats.MaxHP // Fully heal player on level up as a design reward
		stats.MP = stats.MaxMP // Refill mana on level up as a design reward
		
		levelNotice := fmt.Sprintf("[LEVEL UP] Player %s ascended to Level %d! MaxHP: %d | MaxMP: %d | Base Damage: %d\r\n", 
			meta.Name, stats.Level, stats.MaxHP, stats.MaxMP, stats.Dam)
		
		// Broadcast the event notification to local area map witnesses only
		pos, _ := ecs.GlobalRegistry.GetPosition(playerID)
		protocol.BroadcastToMap(pos.MapID, levelNotice)
	} else {
		// Send a small personal ticker notice update instead
		SendNoticeSystem(playerID, []byte(fmt.Sprintf("+%d XP gained.\r\n", xpGained)))
	}

	// Save structural modifications back lock-free
	ecs.GlobalRegistry.SetStats(playerID, stats)
}
