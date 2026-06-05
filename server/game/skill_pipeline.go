package game

import (
	"fmt"
	"server/ecs"
	"server/models"
	"server/peakgo/combat"
	"server/peakgo/loggate"
	"server/peakgo/threat"
	"server/protocol"
	"server/world"
)

// ─── SkillPipelineStage ────────────────────────────────────────────────────────
//
// SkillPipelineStage is a single step in the combat resolution chain.
// Each stage transforms the context and either passes it forward or
// short-circuits with an error.
type SkillPipelineStage int

const (
	StageTargetSelection SkillPipelineStage = iota
	StageRangeCheck
	StageResourceCheck
	StageDamageCalculation
	StageEffectApplication
	StageBroadcast
	StagePostProcess
)

// ─── CombatContext ─────────────────────────────────────────────────────────────
//
// CombatContext carries all state needed through the skill pipeline stages.
// It is a value type — passed by pointer only inside the pipeline for
// mutation, never escaped to heap.
type CombatContext struct {
	Stage    SkillPipelineStage
	Attacker ecs.Entity
	Target   ecs.Entity
	SkillID  uint64
	Skill    *models.SkillDefinition
	IsPlayer bool // Is attacker a player?

	// Resolved stats
	AttackerStats ecs.StatsComponent
	TargetStats   ecs.StatsComponent
	AttackerMeta  ecs.MetadataComponent
	TargetMeta    ecs.MetadataComponent
	AttackerPos   ecs.PositionComponent

	// Combat results
	CombatResult combat.CombatResult
	Damage       int
	RemainingHP  int
	Killed       bool

	// Feedback
	ErrorMessage     string
	SuccessMessage   string
	PersonalFeedback string
}

// ─── SkillPipeline ─────────────────────────────────────────────────────────────
//
// SkillPipeline is a reusable combat resolution pipeline.
// It replaces monolithic AttackSystem / HandleSkillCastingSystem with
// modular stages that can be individually tested and extended.
type SkillPipeline struct{}

// NewSkillPipeline creates a new pipeline instance.
// Stateless — can be shared across goroutines.
func NewSkillPipeline() *SkillPipeline {
	return &SkillPipeline{}
}

// Execute runs the full combat pipeline for an attack.
// Returns a CombatResult (the game-level result for routing) and error string.
func (sp *SkillPipeline) Execute(attackerID, targetID ecs.Entity, skillID uint64) (CombatResult, string) {
	ctx := CombatContext{
		Stage:    StageTargetSelection,
		Attacker: attackerID,
		Target:   targetID,
		SkillID:  skillID,
	}

	return sp.run(&ctx)
}

// run executes the pipeline stages in sequence.
func (sp *SkillPipeline) run(ctx *CombatContext) (CombatResult, string) {
	registry := ecs.DefaultRegistry

	// ── Stage 0: Target Selection & Validation ─────────────────────────────
	if ctx.Attacker == ctx.Target {
		return CombatResult{}, "You cannot target yourself.\r\n"
	}

	// Load skill definition if provided
	if ctx.SkillID != 0 {
		skill, exists := models.SkillRegistry[ctx.SkillID]
		if !exists {
			return CombatResult{}, "Error: That skill does not exist in the server registry!\r\n"
		}
		ctx.Skill = &skill
	}

	// Load attacker data
	attackerStats, ok := registry.GetStats(ctx.Attacker)
	if !ok {
		return CombatResult{}, "Error: attacker stats not found.\r\n"
	}
	ctx.AttackerStats = attackerStats

	attackerMeta, ok := registry.GetMetadata(ctx.Attacker)
	if !ok {
		return CombatResult{}, "Error: attacker metadata not found.\r\n"
	}
	ctx.AttackerMeta = attackerMeta
	ctx.IsPlayer = attackerMeta.Type == ecs.EntityPlayer

	// Load target data
	targetStats, ok := registry.GetStats(ctx.Target)
	if !ok {
		return CombatResult{}, fmt.Sprintf("Target entity %d not found.\r\n", ctx.Target)
	}
	ctx.TargetStats = targetStats

	targetMeta, ok := registry.GetMetadata(ctx.Target)
	if !ok {
		return CombatResult{}, fmt.Sprintf("Target entity %d has no metadata.\r\n", ctx.Target)
	}
	ctx.TargetMeta = targetMeta

	if targetStats.HP <= 0 {
		return CombatResult{}, fmt.Sprintf("%s is already dead.\r\n", targetMeta.Name)
	}

	// ── Stage 1: Range Check ───────────────────────────────────────────────
	sp.stageRangeCheck(ctx)
	if ctx.ErrorMessage != "" {
		return CombatResult{}, ctx.ErrorMessage
	}

	// ── Stage 2: Resource Check (skill MP cost) ─────────────────────────────
	sp.stageResourceCheck(ctx)
	if ctx.ErrorMessage != "" {
		return CombatResult{}, ctx.ErrorMessage
	}

	// ── Stage 3: Damage Calculation ─────────────────────────────────────────
	sp.stageDamageCalculation(ctx)
	if ctx.ErrorMessage != "" {
		return CombatResult{}, ctx.ErrorMessage
	}

	// ── Stage 4: Effect Application (apply damage to target) ────────────────
	sp.stageEffectApplication(ctx)

	// ── Stage 5: Broadcast ──────────────────────────────────────────────────
	result := sp.stageBroadcast(ctx)

	// ── Stage 6: Post Process (death, loot, XP, respawn) ────────────────────
	sp.stagePostProcess(ctx)

	return result, ""
}

// stageRangeCheck validates attack range.
func (sp *SkillPipeline) stageRangeCheck(ctx *CombatContext) {
	const meleeRange = 5.0
	attackRange := meleeRange
	if ctx.Skill != nil {
		attackRange = ctx.Skill.CastRange
	}
	if !world.IsInRange(ctx.Attacker, ctx.Target, meleeRange) && !world.IsInRange(ctx.Attacker, ctx.Target, attackRange) {
		if ctx.Skill != nil {
			ctx.ErrorMessage = fmt.Sprintf("Target out of range (need %.0f units).\r\n", attackRange)
		} else {
			ctx.ErrorMessage = fmt.Sprintf("Target is out of melee range (%.0f units).\r\n", meleeRange)
		}
	}
}

// stageResourceCheck verifies and consumes MP/HP costs.
func (sp *SkillPipeline) stageResourceCheck(ctx *CombatContext) {
	if ctx.Skill == nil {
		return // Auto-attack has no resource cost
	}

	if ctx.AttackerStats.MP < ctx.Skill.ManaCost {
		ctx.ErrorMessage = fmt.Sprintf("Mana Insufficient! Required: %d MP | You have: %d MP\r\n",
			ctx.Skill.ManaCost, ctx.AttackerStats.MP)
		return
	}
	if ctx.Skill.HPCost > 0 && ctx.AttackerStats.HP <= ctx.Skill.HPCost {
		ctx.ErrorMessage = "HP Insufficient for this skill!\r\n"
		return
	}

	// Consume resources (copy-modify-overwrite)
	stats := ctx.AttackerStats
	stats.MP -= ctx.Skill.ManaCost
	stats.HP -= ctx.Skill.HPCost
	ecs.DefaultRegistry.SetStats(ctx.Attacker, stats)
	ctx.AttackerStats = stats
}

// stageDamageCalculation resolves damage using peakgo/combat.
func (sp *SkillPipeline) stageDamageCalculation(ctx *CombatContext) {
	aCombat := statsToCombatStats(ctx.AttackerStats)
	tCombat := statsToCombatStats(ctx.TargetStats)

	var mods combat.DamageModifiers

	if ctx.Skill != nil {
		mods = combat.DamageModifiers{
			DamageType:      combat.DamageType(ctx.Skill.DamageType),
			Element:         combat.Element(ctx.Skill.Element),
			SkillMultiplier: int(ctx.Skill.DamMult * 1000), // Skill uses float → per-mille
			IsGuaranteed:    ctx.Skill.IsGuaranteed,
		}
	} else {
		mods = combat.DamageModifiers{
			DamageType: combat.DamagePhysical,
			Element:    combat.ElementNone,
		}
	}

	var cr combat.CombatResult
	switch mods.DamageType {
	case combat.DamagePhysical:
		cr = combat.ResolvePhysical(&aCombat, &tCombat, mods)
	case combat.DamageMagical:
		cr = combat.ResolveMagical(&aCombat, &tCombat, mods)
	case combat.DamagePure:
		cr = combat.ResolvePure(&aCombat, mods)
	default:
		cr = combat.ResolvePhysical(&aCombat, &tCombat, mods)
	}

	ctx.CombatResult = cr
	ctx.Damage = cr.DamageDealt

	// Record threat when a player attacks a monster
	if ctx.IsPlayer && ctx.TargetMeta.Type == ecs.EntityMonster {
		if ai, hasAI := ecs.DefaultRegistry.GetAI(ctx.Target); hasAI {
			if ai.ThreatTable == nil {
				ai.ThreatTable = threat.NewThreatTable()
				ai.ThreatTable.SetDecayRate(threat.DefaultThreatDecay)
			}
			ai.ThreatTable.Add(uint64(ctx.Attacker), int64(ctx.Damage))
			ecs.DefaultRegistry.SetAI(ctx.Target, ai)
		}
	}
}

// stageEffectApplication applies damage to the target.
//
// If a CombatAccumulator is active (ecs.CurrentCombatBuffer != nil), damage
// is buffered and the killed flag is set to false because the real HP
// subtraction and death check happen during the tick-end Flush.
// When no accumulator is active, damage is applied immediately (legacy path).
func (sp *SkillPipeline) stageEffectApplication(ctx *CombatContext) {
	ctx.RemainingHP = DamageSystem(ctx.Target, ctx.Damage)

	// When the accumulator is active, the killed check happens during Flush.
	// We optimistically report the hit as non-lethal so the per-hit broadcast
	// is NOT sent — the consolidated StatsSync from Flush replaces it.
	if ecs.CurrentCombatBuffer != nil {
		ctx.Killed = false // deferred to accumulator flush
	} else {
		ctx.Killed = ctx.RemainingHP <= 0
	}

	if ctx.Skill != nil && ctx.Skill.HealValue > 0 {
		// Apply heal to caster
		healResult := combat.CalculateHealing(ctx.AttackerStats.Level, ctx.Skill.HealValue, 1000, false)
		newHP, _ := combat.ApplyHealing(ctx.AttackerStats.HP, ctx.AttackerStats.MaxHP, healResult)
		stats := ctx.AttackerStats
		stats.HP = newHP
		ecs.DefaultRegistry.SetStats(ctx.Attacker, stats)
		ctx.AttackerStats = stats
	}
}

// stageBroadcast sends hit/kill notification to neighbors.
//
// When CombatAccumulator is active, per-hit broadcasts are skipped entirely
// because the accumulator flush sends one consolidated StatsSync per target.
func (sp *SkillPipeline) stageBroadcast(ctx *CombatContext) CombatResult {
	result := CombatResult{
		Hit:          true,
		Damage:       ctx.Damage,
		AttackerID:   ctx.Attacker,
		TargetID:     ctx.Target,
		AttackerName: ctx.AttackerMeta.Name,
		TargetName:   ctx.TargetMeta.Name,
		TargetHP:     ctx.RemainingHP,
		Killed:       ctx.Killed,
	}

	// When accumulator is active, skip per-hit broadcasts
	if ecs.CurrentCombatBuffer != nil {
		return result
	}

	if !ctx.Killed {
		broadcastHit(result)
	} else {
		// Broadcast death differently — send kill frame
		if ctx.IsPlayer && ctx.TargetMeta.Type == ecs.EntityMonster {
			msg := fmt.Sprintf("[COMBAT] %s has slain %s!\r\n", ctx.AttackerMeta.Name, ctx.TargetMeta.Name)
			if ctx.Skill != nil {
				msg = fmt.Sprintf("[SPELL] %s unleashed %s on %s dealing %d damage!\r\n",
					ctx.AttackerMeta.Name, ctx.Skill.Name, ctx.TargetMeta.Name, ctx.Damage)
			}
			pos, _ := ecs.DefaultRegistry.GetPosition(ctx.Attacker)
			protocol.BroadcastToNeighbors(pos, []byte(msg), ctx.Attacker)
		}
	}

	return result
}

// stagePostProcess handles death cleanup, loot, XP, and respawn scheduling.
//
// When CombatAccumulator is active, death is deferred to the tick-end Flush.
// The hit damage and threat are added to the buffer so the flush callback
// can resolve the top damager as the killer.
func (sp *SkillPipeline) stagePostProcess(ctx *CombatContext) {
	if !ctx.Killed {
		return
	}

	// When accumulator is active, death is deferred until the accumulator flush
	// at map tick end. We skip the immediate DeathSystem call.
	if ecs.CurrentCombatBuffer == nil {
		DeathSystem(ctx.Target, ctx.Attacker, ctx.TargetMeta, ctx.AttackerMeta, ctx.Damage)
	} else {
		// Add the killer as threat so the accumulator flush can resolve
		// the top damager for death attribution.
		ecs.CurrentCombatBuffer.AddDamage(ctx.Target, ctx.Attacker, ctx.Damage, float64(ctx.Damage))
	}

	// Personal feedback for skills (works in both paths)
	if ctx.Skill != nil && ctx.IsPlayer {
		ctx.PersonalFeedback = fmt.Sprintf("Spent -%d MP. (Current: %d/%d MP)\r\n",
			ctx.Skill.ManaCost, ctx.AttackerStats.MP, ctx.AttackerStats.MaxMP)
	}

	if loggate.DebugEnabled() {
		loggate.Debugf("[PIPELINE] %s → %s | skill=%d dmg=%d killed=%v",
			ctx.AttackerMeta.Name, ctx.TargetMeta.Name, ctx.SkillID, ctx.Damage, ctx.Killed)
	}
}
