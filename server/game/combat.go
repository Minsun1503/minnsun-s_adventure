package game

import (
	"fmt"
	"server/ecs"
	"server/models"
	"server/peakgo/broadcast"
	"server/peakgo/combat"
	"server/peakgo/eventbus"
	"server/peakgo/gmath"
	"server/peakgo/loggate"
	"server/peakgo/rng"
	"server/peakgo/threat"
	"server/protocol"
	"server/world"
	"time"
)

// CombatResult is returned by AttackSystem to handleCommand.
// It carries enough information to route the response without
// handleCommand needing to know anything about ECS internals.
type CombatResult struct {
	Hit          bool // false = attack was rejected before landing
	Killed       bool // true = target HP reached 0
	Damage       int  // actual damage dealt
	AttackerID   ecs.Entity
	TargetID     ecs.Entity
	AttackerName string
	TargetName   string
	TargetHP     int // remaining HP after hit (0 if killed)
}

const meleeRange = 5.0 // world units; tune per game design

// AttackSystem is the entry point for all combat interactions.
// It validates both parties, calls DamageSystem, then routes to
// DeathSystem or a hit broadcast depending on the outcome.
//
// Parameters:
//   - attackerID: entity initiating the attack.
//   - targetID:   entity receiving the attack.
//
// Returns a CombatResult and an error string if the attack was rejected.
// On rejection, error string is ready to send directly to the attacker.
func AttackSystem(attackerID, targetID ecs.Entity) (CombatResult, string) {
	registry := ecs.DefaultRegistry

	// --- Attacker validation ---
	if attackerID == targetID {
		return CombatResult{}, "You cannot attack yourself.\r\n"
	}

	attackerStats, ok := registry.GetStats(attackerID)
	if !ok {
		return CombatResult{}, "Error: attacker stats not found.\r\n"
	}
	attackerMeta, ok := registry.GetMetadata(attackerID)
	if !ok {
		return CombatResult{}, "Error: attacker metadata not found.\r\n"
	}

	// --- Target validation ---
	targetStats, ok := registry.GetStats(targetID)
	if !ok {
		return CombatResult{}, fmt.Sprintf("Target entity %d not found.\r\n", targetID)
	}
	targetMeta, ok := registry.GetMetadata(targetID)
	if !ok {
		return CombatResult{}, fmt.Sprintf("Target entity %d has no metadata.\r\n", targetID)
	}
	if targetStats.HP <= 0 {
		return CombatResult{}, fmt.Sprintf("%s is already dead.\r\n", targetMeta.Name)
	}

	// ← NEW: prevent attacking entities that are currently transferring between maps
	if ai, hasAI := registry.GetAI(targetID); hasAI && ai.State == ecs.AIStateTransferring {
		return CombatResult{}, fmt.Sprintf("%s is currently invulnerable (transferring).\r\n", targetMeta.Name)
	}

	// ← NEW: range check using spatial system
	if !world.IsInRange(attackerID, targetID, meleeRange) {
		targetMeta, _ := ecs.DefaultRegistry.GetMetadata(targetID)
		return CombatResult{}, fmt.Sprintf(
			"%s is out of melee range (%.0f units).\r\n", targetMeta.Name, meleeRange,
		)
	}

	// --- Damage calculation using peakgo/combat (crit, defense, dodge) ---
	aCombat := statsToCombatStats(attackerStats)
	tCombat := statsToCombatStats(targetStats)
	mods := combat.DamageModifiers{
		DamageType: combat.DamagePhysical,
		Element:    combat.ElementNone,
	}
	cr := combat.ResolvePhysical(&aCombat, &tCombat, mods)
	damage := cr.DamageDealt

	// Record threat when a player attacks a monster
	if attackerMeta.Type == ecs.EntityPlayer && targetMeta.Type == ecs.EntityMonster {
		if ai, hasAI := ecs.DefaultRegistry.GetAI(targetID); hasAI {
			if ai.ThreatTable == nil {
				ai.ThreatTable = threat.NewThreatTable()
				ai.ThreatTable.SetDecayRate(threat.DefaultThreatDecay)
			}
			ai.ThreatTable.Add(uint64(attackerID), int64(damage))
			ecs.DefaultRegistry.SetAI(targetID, ai)
		}
	}

	// --- Apply damage via DamageSystem (copy-modify-overwrite) ---
	remaining := DamageSystem(targetID, damage)

	result := CombatResult{
		Hit:          true,
		Damage:       damage,
		AttackerID:   attackerID,
		TargetID:     targetID,
		AttackerName: attackerMeta.Name,
		TargetName:   targetMeta.Name,
		TargetHP:     remaining,
	}

	if remaining <= 0 {
		result.Killed = true
		DeathSystem(targetID, attackerID, targetMeta, attackerMeta, damage)

		// Roll loot and spawn items on the ground if the killed target is a monster and attacker is a player.
		if targetMeta.Type == ecs.EntityMonster && attackerMeta.Type == ecs.EntityPlayer {
			// Resolve the monster's template ID from its name so every monster
			// rolls its own loot table instead of always falling back to template 1.
			if tmpl, found := models.GetTemplateByName(targetMeta.Name); found {
				monsterTemplateID := uint64(tmpl.ID)
				droppedItems := RollLoot(monsterTemplateID)

				if len(droppedItems) > 0 {
					targetPos, hasPos := registry.GetPosition(targetID)
					if hasPos {
						for _, itemID := range droppedItems {
							// rng.Intn: pooled RNG — no mutex contention, 0 allocs.
							offsetX := rng.Intn(3) - 1 // yields -1, 0, or 1
							offsetZ := rng.Intn(3) - 1

							// gmath.ClampPos: replaces 8 lines of manual if/else clamping.
							dropX, dropZ := gmath.ClampPos(
								targetPos.X+offsetX,
								targetPos.Z+offsetZ,
								0, 100,
							)

							SpawnItemOnGround(itemID, targetPos.MapID, dropX, dropZ)
						}
					}
				}
			}
		}
	} else {
		// Target survived — broadcast the hit to everyone on the map.
		if ecs.CurrentCombatBuffer == nil {
			broadcastHit(result)
		}
	}

	return result, ""
}

// calculateDamage computes raw damage dealt.
// Isolated here so future systems (armor, critical hits, elemental resist)
// only need to modify this one function.
//
// Current formula: flat attacker damage.
// Future hooks: subtract target defense, multiply by crit multiplier, etc.
// statsToCombatStats maps ecs.StatsComponent → peakgo/combat.Stats
func statsToCombatStats(s ecs.StatsComponent) combat.Stats {
	// Use Dam as Attack if Attack is not set (backward compat)
	atk := s.Attack
	if atk == 0 && s.Dam > 0 {
		atk = s.Dam
	}
	return combat.Stats{
		Level:        s.Level,
		MaxHP:        s.MaxHP,
		CurrentHP:    s.HP,
		MaxMP:        s.MaxMP,
		CurrentMP:    s.MP,
		Attack:       atk,
		MagicAttack:  s.MagicAttack,
		Defense:      s.Defense,
		MagicDefense: s.MagicDefense,
		HitRate:      s.HitRate,
		DodgeRate:    s.DodgeRate,
		CritRate:     s.CritRate,
		CritDamage:   s.CritDamage,
	}
}

// DamageSystem applies a damage value to a target entity.
//
// If a CombatAccumulator is currently installed (via ecs.CurrentCombatBuffer),
// the damage is buffered and NOT applied immediately — it will be flushed at
// the end of the current map tick, coalescing all hits on the same target
// into a single HP write and broadcast.
//
// If no CombatAccumulator is installed (legacy mode), damage is applied
// immediately using the old copy-modify-overwrite pattern.
//
// Parameters:
//   - targetID: entity to damage.
//   - amount:   damage points to subtract from HP.
//
// Returns the target's HP after damage (may be negative, or 0 if buffered).
func DamageSystem(targetID ecs.Entity, amount int) int {
	// Route through CombatAccumulator if active (1000-vs-1 boss storm path)
	if ecs.CurrentCombatBuffer != nil {
		ecs.CurrentCombatBuffer.AddDamage(targetID, 0, amount, 0)
		// Return current HP without modification — the real HP change
		// happens during CombatAccumulator.Flush at tick end.
		if stats, ok := ecs.DefaultRegistry.GetStats(targetID); ok {
			return stats.HP
		}
		return 0
	}

	// Legacy immediate-damage path (no accumulator installed)
	stats, ok := ecs.DefaultRegistry.GetStats(targetID)
	if !ok {
		return 0
	}
	stats.HP -= amount                           // MODIFY
	ecs.DefaultRegistry.SetStats(targetID, stats) // OVERWRITE
	return stats.HP
}

// DeathSystem handles cleanup when a target's HP reaches zero.
// Responsibilities:
//  1. Broadcast the kill event to the whole map.
//  2. Remove the entity from ECS (releases all components).
//  3. If target was a player, their connection cleanup is handled
//     by the deferred block in handleClient — DeathSystem only
//     closes the connection to trigger that path.
//
// Parameters:
//   - targetID:    entity that died.
//   - killerID:    entity that killed the target (may be 0 for environmental/status effect deaths).
//   - targetMeta:  pre-fetched metadata (entity is about to be removed).
//   - killerMeta:  attacker's metadata for the kill broadcast message.
func DeathSystem(targetID, killerID ecs.Entity, targetMeta, killerMeta ecs.MetadataComponent, damage int) {
	registry := ecs.DefaultRegistry

	var killMsg string
	if killerMeta.Type == ecs.EntityMonster {
		killMsg = fmt.Sprintf("[DEATH] %s (#%d) struck Player %s for %d damage and DEFEATED them!\r\n",
			killerMeta.Name, killerID, targetMeta.Name, damage)
	} else {
		killMsg = fmt.Sprintf("[COMBAT] %s was slain by %s!\r\n",
			targetMeta.Name, killerMeta.Name)
	}
	deathFrame := broadcast.BuildNotice(broadcast.NoticePayload{Message: killMsg})
	targetPos, _ := registry.GetPosition(targetID)

	// If the killer is in a party, notify the whole party instead of just the map.
	if partyID := GetPlayerPartyID(killerID); partyID != 0 {
		BroadcastToPartyBinary(partyID, deathFrame)
	} else {
		protocol.BroadcastToNeighbors(targetPos, deathFrame, killerID)
	}

	// ← NEW: remove từ spatial grid trước khi ECS cleanup
	world.GlobalSpatialGrid.RemoveEntity(targetID)

	if targetMeta.Type == ecs.EntityPlayer {
		// Publish player death event
		eventbus.GlobalBus.Publish(eventbus.TopicPlayerDeath, eventbus.PlayerDeathEvent{
			PlayerID:   uint64(targetID),
			KillerID:   uint64(killerID),
			PlayerName: targetMeta.Name,
			MapID:      targetPos.MapID,
		})

		conn, ok := registry.GetConnection(targetID)
		if ok && conn.Conn != nil {
			conn.Conn.Close()
		}
	} else {
		// ── Threat Table Cleanup ─────────────────────────────────────────────
		// Release threat table memory when a monster dies to prevent aggro
		// memory leak. The ThreatTable holds a pooled slice and max-heap array
		// that must be returned via Close().
		if ai, hasAI := registry.GetAI(targetID); hasAI && ai.ThreatTable != nil {
			ai.ThreatTable.Close()
			ai.ThreatTable = nil
			registry.SetAI(targetID, ai)
		}

		if t, found := models.GetTemplateByName(targetMeta.Name); found {
			spawnX, spawnZ := t.SpawnX, t.SpawnZ
			if ai, hasAI := registry.GetAI(targetID); hasAI {
				spawnX, spawnZ = ai.SpawnX, ai.SpawnZ
			}
			GlobalRespawnManager.ScheduleMonsterRespawn(
				t.ID, targetPos.MapID, spawnX, spawnZ, 15*time.Second,
			)

			// Publish monster death event
			if killerMeta.Type == ecs.EntityPlayer {
				eventbus.GlobalBus.Publish(eventbus.TopicMonsterDeath, eventbus.MonsterDeathEvent{
					MonsterID:   uint64(targetID),
					KillerID:    uint64(killerID),
					MonsterName: targetMeta.Name,
					MapID:       targetPos.MapID,
					SpawnX:      spawnX,
					SpawnZ:      spawnZ,
					XPReward:    t.XPReward,
					TemplateID:  t.ID,
				})
			}

			// Distribute XP if killer was a player
			if killerMeta.Type == ecs.EntityPlayer {
				xpBounty := t.XPReward
				if xpBounty > 0 {
					if partyID := GetPlayerPartyID(killerID); partyID != 0 {
						if party, exists := registry.GetParty(partyID); exists && len(party.MemberIDs) > 0 {
							share := xpBounty / uint64(len(party.MemberIDs))
							if share == 0 {
								share = 1
							}
							for _, memberID := range party.MemberIDs {
								AddExperienceSystem(memberID, share)
							}
						}
					} else {
						AddExperienceSystem(killerID, xpBounty)
					}
				}
			}
		}
		registry.RemoveEntity(targetID)
	}
}

// broadcastHit sends a hit notification to all connected clients.
// Called only when the target survived (HP > 0 after hit).
func broadcastHit(r CombatResult) {
	killed := uint8(0)
	if r.Killed {
		killed = 1
	}
	payload := broadcast.CombatHitPayload{
		AttackerID: uint64(r.AttackerID),
		TargetID:   uint64(r.TargetID),
		Damage:     int32(r.Damage),
		TargetHP:   int32(r.TargetHP),
		Killed:     killed,
	}
	frame := broadcast.BuildCombatHit(payload)
	attackerPos, _ := ecs.DefaultRegistry.GetPosition(r.AttackerID)
	protocol.BroadcastToNeighbors(attackerPos, frame, r.AttackerID)
	loggate.Debugf("[HIT] %s → %s | dmg=%d hp_left=%d",
		r.AttackerName, r.TargetName, r.Damage, r.TargetHP)
}
