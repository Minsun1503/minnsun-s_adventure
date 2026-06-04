package loadtest

import (
	"server/ecs"
	"server/game"
	"server/peakgo/loggate"
	"server/world"
)

// ─── Combat Bot ───────────────────────────────────────────────────────────────
//
// CombatBot drives player bots to find and attack nearby monsters.
// Each tick, a bot scans its spatial neighborhood for monster entities
// and attacks the closest one using the real game.AttackSystem.

const combatBotSearchRadius = 60.0 // same as AOI watcher radius

// TickCombatBots processes all alive player bots and makes them attack
// the nearest monster. Bots with no monsters nearby simply idle.
//
// Returns the number of successful attack attempts this tick.
func TickCombatBots() int {
	attacks := 0
	for _, bot := range playerBots {
		if !bot.Alive {
			continue
		}

		// If the bot already has a target, check if it's still alive.
		if bot.TargetID != 0 {
			if targetStats, ok := ecs.GlobalRegistry.GetStats(bot.TargetID); ok && targetStats.HP > 0 {
				// Target still alive — keep attacking.
				_, errMsg := game.AttackSystem(bot.ID, bot.TargetID)
				if errMsg == "" {
					attacks++
					continue
				}
				// Attack failed (target out of range, etc.) — fall through to find new target.
			}
			bot.TargetID = 0 // target dead or invalid
		}

		// Scan for a new monster target in the spatial grid.
		nearby := world.GetNearbyMonsters(bot.ID, combatBotSearchRadius)
		if len(nearby) == 0 {
			continue
		}

		// Attack the first (closest) monster found.
		target := nearby[0]
		bot.TargetID = target.ID

		_, errMsg := game.AttackSystem(bot.ID, target.ID)
		if errMsg == "" {
			attacks++
			if loggate.DebugEnabled() {
				loggate.Debugf("[LOADTEST] Bot %d attacks monster %d", bot.ID, target.ID)
			}
		} else {
			bot.TargetID = 0
		}
		world.FreeNearbyMonsters(nearby)
	}
	return attacks
}

// BotAttackCount returns how many bots currently have an active target.
func BotAttackCount() int {
	count := 0
	for _, bot := range playerBots {
		if bot.Alive && bot.TargetID != 0 {
			count++
		}
	}
	return count
}
