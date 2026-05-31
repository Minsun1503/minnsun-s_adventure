package game

import (
	"fmt"
	"math/rand"
	"server/ecs"
	"server/models"
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
	registry := ecs.GlobalRegistry

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

	// ← NEW: range check using spatial system
	if !world.IsInRange(attackerID, targetID, meleeRange) {
		targetMeta, _ := ecs.GlobalRegistry.GetMetadata(targetID)
		return CombatResult{}, fmt.Sprintf(
			"%s is out of melee range (%.0f units).\r\n", targetMeta.Name, meleeRange,
		)
	}

	// --- Damage calculation (extend here: crit, defense, buffs) ---
	damage := calculateDamage(attackerStats, targetStats)

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
		DeathSystem(targetID, attackerID, targetMeta, attackerMeta)

		// Roll loot and spawn items on the ground if the killed target is a monster and attacker is a player
		if targetMeta.Type == "monster" && attackerMeta.Type == "player" {
			monsterTemplateID := uint64(1) // Example fallback lookup
			droppedItems := RollLoot(monsterTemplateID)

			if len(droppedItems) > 0 {
				targetPos, hasPos := registry.GetPosition(targetID)
				if hasPos {
					for _, itemID := range droppedItems {
						// Scatter: generate a small random offset between -1 and +1 grid tiles
						offsetX := rand.Intn(3) - 1 // yields -1, 0, or 1
						offsetZ := rand.Intn(3) - 1 // yields -1, 0, or 1

						dropX := targetPos.X + offsetX
						dropZ := targetPos.Z + offsetZ

						// Clamp to map boundaries (0-100)
						if dropX < 0 {
							dropX = 0
						}
						if dropX > 100 {
							dropX = 100
						}
						if dropZ < 0 {
							dropZ = 0
						}
						if dropZ > 100 {
							dropZ = 100
						}

						SpawnItemOnGround(itemID, targetPos.MapID, dropX, dropZ)
					}
				}
			}
		}
	} else {
		// Target survived — broadcast the hit to everyone on the map.
		broadcastHit(result)
	}

	return result, ""
}

// calculateDamage computes raw damage dealt.
// Isolated here so future systems (armor, critical hits, elemental resist)
// only need to modify this one function.
//
// Current formula: flat attacker damage.
// Future hooks: subtract target defense, multiply by crit multiplier, etc.
func calculateDamage(attacker, _ ecs.StatsComponent) int {
	dmg := attacker.Dam
	if dmg <= 0 {
		dmg = 1 // minimum 1 damage; prevent zero-damage stalemates
	}
	return dmg
}

// DamageSystem applies a damage value to a target entity using the
// copy-modify-overwrite pattern required by inline ECS values.
// It does NOT check for death — that is AttackSystem's responsibility.
//
// Parameters:
//   - targetID: entity to damage.
//   - amount:   damage points to subtract from HP.
//
// Returns the target's HP after damage (may be negative).
func DamageSystem(targetID ecs.Entity, amount int) int {
	stats, ok := ecs.GlobalRegistry.GetStats(targetID)
	if !ok {
		return 0
	}
	stats.HP -= amount                           // MODIFY
	ecs.GlobalRegistry.SetStats(targetID, stats) // OVERWRITE
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
//   - targetMeta:  pre-fetched metadata (entity is about to be removed).
//   - killerMeta:  attacker's metadata for the kill broadcast message.
func DeathSystem(targetID, killerID ecs.Entity, targetMeta, killerMeta ecs.MetadataComponent) {
	registry := ecs.GlobalRegistry

	var killMsg string
	if killerMeta.Type == "monster" {
		stats, _ := registry.GetStats(killerID)
		killMsg = fmt.Sprintf("[DEATH] %s (#%d) struck Player %s for %d damage and DEFEATED them!\r\n",
			killerMeta.Name, killerID, targetMeta.Name, stats.Dam)
	} else {
		killMsg = fmt.Sprintf("[COMBAT] %s was slain by %s!\r\n",
			targetMeta.Name, killerMeta.Name)
	}
	targetPos, _ := registry.GetPosition(targetID)

	// If the killer is in a party, notify the whole party instead of just the map.
	if partyID := GetPlayerPartyID(killerID); partyID != 0 {
		BroadcastToParty(partyID, killMsg)
	} else {
		protocol.BroadcastToMap(targetPos.MapID, killMsg)
	}

	// ← NEW: remove từ spatial grid trước khi ECS cleanup
	world.GlobalSpatialGrid.RemoveEntity(targetID)

	if targetMeta.Type == "player" {
		conn, ok := registry.GetConnection(targetID)
		if ok && conn.Conn != nil {
			conn.Conn.Close()
		}
	} else {
		if t, found := models.GetTemplateByName(targetMeta.Name); found {
			spawnX, spawnZ := t.SpawnX, t.SpawnZ
			if ai, hasAI := registry.GetAI(targetID); hasAI {
				spawnX, spawnZ = ai.SpawnX, ai.SpawnZ
			}
			GlobalRespawnManager.ScheduleMonsterRespawn(
				t.ID, targetPos.MapID, spawnX, spawnZ, 15*time.Second,
			)

			// Distribute XP if killer was a player
			if killerMeta.Type == "player" {
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
	var msg string
	attackerMeta, ok := ecs.GlobalRegistry.GetMetadata(r.AttackerID)
	if ok && attackerMeta.Type == "monster" {
		msg = fmt.Sprintf("[COMBAT] %s (#%d) hit Player %s for %d damage! (Player HP: %d)\r\n",
			r.AttackerName, r.AttackerID, r.TargetName, r.Damage, r.TargetHP)
	} else {
		msg = fmt.Sprintf(
			"[COMBAT] %s hit %s for %d damage! (%s HP: %d)\r\n",
			r.AttackerName, r.TargetName, r.Damage, r.TargetName, r.TargetHP,
		)
	}
	attackerPos, _ := ecs.GlobalRegistry.GetPosition(r.AttackerID)
	protocol.BroadcastToMap(attackerPos.MapID, msg)
	fmt.Printf("[HIT] %s → %s | dmg=%d hp_left=%d\n",
		r.AttackerName, r.TargetName, r.Damage, r.TargetHP)
}
