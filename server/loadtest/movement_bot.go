package loadtest

import (
	"server/ecs"
	"server/game"
	"server/peakgo/gmath"
	"server/peakgo/rng"
	"server/world"
)

// ─── Movement Bot ─────────────────────────────────────────────────────────────
//
// MovementBot drives random walk behavior for player bots.
// Each tick, a bot picks a random direction within bounds and tries to move.
// Monsters on the same map will naturally detect and react to these bots
// via the existing spatial/aggro system.

// TickMovementBots processes all alive player bots and moves them
// randomly within the map bounds. Called once per game tick from
// the load test harness.
//
// Returns the number of bots that successfully moved this tick.
func TickMovementBots() int {
	moved := 0
	for _, bot := range playerBots {
		if !bot.Alive {
			continue
		}

		// Every bot moves on every tick with a random step.
		dx := rng.Intn(3) - 1 // -1, 0, or 1
		dz := rng.Intn(3) - 1
		if dx == 0 && dz == 0 {
			continue // skip this tick to avoid flooding movement system
		}

		newX := bot.X + dx
		newZ := bot.Z + dz

		// Clamp to map bounds [1, 99]
		if !gmath.InBounds(newX, newZ, 1, 99) {
			continue
		}

		// Collision check
		if world.IsTileBlocked(bot.MapID, newX, newZ) {
			continue
		}

		// Apply movement via the real MovementSystem (anti-cheat included)
		if game.MovementSystem(bot.ID, newX, newZ) {
			bot.X = newX
			bot.Z = newZ
			moved++
		}
	}
	return moved
}

// TeleportBot instantly moves a bot to a new position (bypasses anti-cheat).
// Used for bot respawning after death or repositioning.
func TeleportBot(bot *PlayerBotState, x, z int) {
	pos := ecs.PositionComponent{MapID: bot.MapID, X: x, Z: z}
	ecs.DefaultRegistry.SetPosition(bot.ID, pos)
	world.GlobalSpatialGrid.UpdateEntityPosition(bot.ID, pos)
	bot.X = x
	bot.Z = z
}
