package game

import (
	"fmt"
	"server/ecs"
	"server/protocol"
)

// RunStatusEffectsSystem iterates through all entities to tick down active buffs or process DoTs.
// Uses tick-based timing via CurrentTick() and WallTimer for delay tracking.
func RunStatusEffectsSystem() {
	tickInterval := uint64(1) // 1 tick per call (called every game loop tick)
	_ = tickInterval

	ecs.DefaultRegistry.RangeEffects(func(id ecs.Entity, effComp ecs.EffectsComponent) bool {
		if len(effComp.ActiveList) == 0 {
			return true
		}

		effComp = effComp.Clone()

		var activeRemaining []ecs.ActiveEffect
		forceStatRecalc := false

		meta, _ := ecs.DefaultRegistry.GetMetadata(id)
		pos, posOk := ecs.DefaultRegistry.GetPosition(id)

		for _, effect := range effComp.ActiveList {
			// 1. Tick down total lifespan duration using tick-based decrement
			// Convert to ticks: decrement by 1 tick per 250ms game loop
			if effect.DurationTicks > 0 {
				effect.DurationTicks--
			}

			if effect.DurationTicks <= 0 {
				// Effect expired!
				if effect.Type == "haste_buff" {
					forceStatRecalc = true
				}
				if posOk {
					protocol.BroadcastToNeighbors(pos, []byte(fmt.Sprintf("[STATUS] The %s effect worn off from %s (#%d).\r\n", effect.Type, meta.Name, id)), id)
				}
				continue // Skip appending to remaining active list
			}

			// 2. PROCESS DAMAGE OVER TIME (DoT) TICKERS (every ~4 ticks = ~1 second)
			if effect.Type == "poison" || effect.Type == "burn" {
				// Tick-based DoT: fire every 4 ticks (1 second at 250ms/tick)
				effect.DoTTickAccum++

				if effect.DoTTickAccum >= 4 {
					effect.DoTTickAccum = 0

					// COPY STATS OUT
					stats, hasStats := ecs.DefaultRegistry.GetStats(id)
					if hasStats && stats.HP > 0 {
						// MODIFY
						stats.HP -= effect.Value
						if stats.HP < 0 {
							stats.HP = 0
						}

						// OVERWRITE
						ecs.DefaultRegistry.SetStats(id, stats)

						if posOk {
							protocol.BroadcastToNeighbors(pos, []byte(fmt.Sprintf("[STATUS] %s (#%d) suffered -%d %s damage! (HP: %d/%d)\r\n",
								meta.Name, id, effect.Value, effect.Type, stats.HP, stats.MaxHP)), id)
						}

						// Handle death transition logic safely if DoT dealt a killing blow
						if stats.HP == 0 {
							if posOk {
								protocol.BroadcastToNeighbors(pos, []byte(fmt.Sprintf("[DEATH] %s (#%d) succumbed to %s.\r\n", meta.Name, id, effect.Type)), id)
							}

							DeathSystem(id, 0, meta, ecs.MetadataComponent{Name: effect.Type, Type: ecs.EntityMonster}, effect.Value) // status_effect not in EntityType enum; fallback to monster for logging
							continue                                                                                                  // Stop processing an already erased row anchor
						}
					}
				}
			}

			// Still active, retain in list
			activeRemaining = append(activeRemaining, effect)
		}

		// 3. Commit remaining active statuses back to data grids
		effComp.ActiveList = activeRemaining
		ecs.DefaultRegistry.SetEffects(id, effComp)

		// If a major buff expired, re-run aggregation logic immediately to strip bonus points
		if forceStatRecalc && meta.Type == ecs.EntityPlayer {
			RecalculateActiveStats(id)
		}

		return true
	})
}

// ─── TICK-BASED DURATION HELPERS ────────────────────────────────────────────

// TicksFromSeconds converts seconds to ticks at 4 ticks/sec.
// floor(seconds) * 4 gives the approximate tick count.
func TicksFromSeconds(seconds int) uint64 {
	return uint64(seconds) * 4
}

// AddEffectWithTicks applies an effect with tick-based duration instead of time.Duration.
// This replaces the old time.Now() based effect scheduling for zero-syscall timing.
func AddEffectWithTicks(entity ecs.Entity, effectType string, value int, durationTicks int) {
	effComp, ok := ecs.DefaultRegistry.GetEffects(entity)
	if !ok {
		return
	}

	effComp = effComp.Clone()
	effComp.ActiveList = append(effComp.ActiveList, ecs.ActiveEffect{
		Type:          effectType,
		Value:         value,
		DurationTicks: durationTicks,
		DoTTickAccum:  0,
	})
	ecs.DefaultRegistry.SetEffects(entity, effComp)
}
