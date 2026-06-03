package game

import (
	"fmt"
	"server/ecs"
	"server/protocol"
	"time"
)

// RunStatusEffectsSystem iterates through all entities to tick down active buffs or process DoTs.
func RunStatusEffectsSystem() {
	now := time.Now()
	tickInterval := 250 * time.Millisecond

	ecs.GlobalRegistry.RangeEffects(func(id ecs.Entity, effComp ecs.EffectsComponent) bool {
		if len(effComp.ActiveList) == 0 {
			return true
		}

		effComp = effComp.Clone()

		var activeRemaining []ecs.ActiveEffect
		forceStatRecalc := false

		// Fetch metadata for logging purposes
		meta, _ := ecs.GlobalRegistry.GetMetadata(id)
		pos, posOk := ecs.GlobalRegistry.GetPosition(id)

		for _, effect := range effComp.ActiveList {
			// 1. Tick down total lifespan duration frames
			effect.Duration -= tickInterval

			if effect.Duration <= 0 {
				// Effect expired!
				if effect.Type == "haste_buff" {
					forceStatRecalc = true
				}
				if posOk {
					protocol.BroadcastToNeighbors(pos, []byte(fmt.Sprintf("[STATUS] The %s effect worn off from %s (#%d).\r\n", effect.Type, meta.Name, id)), id)
				}
				continue // Skip appending to remaining active list
			}

			// 2. PROCESS DAMAGE OVER TIME (DoT) TICKERS (Downsample to 1-second cycles)
			if effect.Type == "poison" || effect.Type == "burn" {
				if now.Sub(effect.LastTickTime) >= 1*time.Second {
					// COPY STATS OUT
					stats, hasStats := ecs.GlobalRegistry.GetStats(id)
					if hasStats && stats.HP > 0 {
						// MODIFY
						stats.HP -= effect.Value
						if stats.HP < 0 {
							stats.HP = 0
						}

						// OVERWRITE
						ecs.GlobalRegistry.SetStats(id, stats)
						effect.LastTickTime = now

						if posOk {
							protocol.BroadcastToNeighbors(pos, []byte(fmt.Sprintf("[STATUS] %s (#%d) suffered -%d %s damage! (HP: %d/%d)\r\n",
								meta.Name, id, effect.Value, effect.Type, stats.HP, stats.MaxHP)), id)
						}

						// Handle death transition logic safely if DoT dealt a killing blow
						if stats.HP == 0 {
							if posOk {
								protocol.BroadcastToNeighbors(pos, []byte(fmt.Sprintf("[DEATH] %s (#%d) succumbed to %s.\r\n", meta.Name, id, effect.Type)), id)
							}

							DeathSystem(id, 0, meta, ecs.MetadataComponent{Name: effect.Type, Type: ecs.EntityMonster}) // status_effect not in EntityType enum; fallback to monster for logging
							continue                                                                                    // Stop processing an already erased row anchor
						}
					}
				}
			}

			// Still active, retain in list
			activeRemaining = append(activeRemaining, effect)
		}

		// 3. Commit remaining active statuses back to data grids
		effComp.ActiveList = activeRemaining
		ecs.GlobalRegistry.SetEffects(id, effComp)

		// If a major buff expired, re-run aggregation logic immediately to strip bonus points
		if forceStatRecalc && meta.Type == ecs.EntityPlayer {
			RecalculateActiveStats(id)
		}

		return true
	})
}
