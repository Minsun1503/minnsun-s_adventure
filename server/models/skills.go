package models

import "server/peakgo/skilldata"

// DamageTypeEnum matches peakgo/combat DamageType uint8 constants.
type DamageTypeEnum uint8

const (
	DamagePhysical DamageTypeEnum = 0
	DamageMagical  DamageTypeEnum = 1
	DamagePure     DamageTypeEnum = 2
)

// ElementEnum matches peakgo/combat Element uint8 constants.
type ElementEnum uint8

const (
	ElementNone  ElementEnum = 0
	ElementFire  ElementEnum = 1
	ElementWater ElementEnum = 2
	ElementWind  ElementEnum = 3
	ElementEarth ElementEnum = 4
	ElementLight ElementEnum = 5
	ElementDark  ElementEnum = 6
)

// SkillDefinition defines the static, read-only configuration rules for an ability.
type SkillDefinition struct {
	ID            uint64
	Name          string
	ManaCost      int
	HPCost        int
	DamMult       float64 // Damage multiplier (e.g., 2.5 means 250% base damage strike)
	CastRange     float64 // Max range in world units (0 = melee range)
	DamageType    DamageTypeEnum
	Element       ElementEnum
	IsGuaranteed  bool // Bypasses hit/dodge check
	HealValue     int  // If > 0, heals caster instead of dealing damage
	CooldownTicks int  // Cooldown in ticks (0 = no cooldown)
	Description   string
}

// ─── Skill Graph (peakgo/skilldata) ──────────────────────────────────────────

// GlobalSkillGraph is the authoritative skill tree for the entire server.
// Built alongside SkillRegistry for backward compatibility, but powers
// the new skill-tree queries (prerequisites, level gating, class filtering).
var GlobalSkillGraph *skilldata.SkillGraph

// Global registry holding all skill configurations in memory
var SkillRegistry = make(map[uint64]SkillDefinition)

// InitializeSkillRegistry populates available abilities
func InitializeSkillRegistry() {
	// ── Legacy Skill Registry ────────────────────────────────────────────
	SkillRegistry[1] = SkillDefinition{
		ID:            1,
		Name:          "Fireball",
		ManaCost:      20,
		DamMult:       2.5,
		CastRange:     8.0,
		DamageType:    DamageMagical,
		Element:       ElementFire,
		IsGuaranteed:  false,
		CooldownTicks: 4,
		Description:   "Launches a fireball dealing 2.5x magical damage.",
	}
	SkillRegistry[2] = SkillDefinition{
		ID:            2,
		Name:          "Thunderclap",
		ManaCost:      35,
		DamMult:       4.0,
		CastRange:     6.0,
		DamageType:    DamageMagical,
		Element:       ElementWind,
		IsGuaranteed:  false,
		CooldownTicks: 8,
		Description:   "Calls down thunder dealing 4x magical damage.",
	}
	SkillRegistry[3] = SkillDefinition{
		ID:            3,
		Name:          "Power Strike",
		ManaCost:      10,
		DamMult:       1.5,
		CastRange:     5.0,
		DamageType:    DamagePhysical,
		Element:       ElementNone,
		IsGuaranteed:  false,
		CooldownTicks: 2,
		Description:   "A powerful melee strike dealing 1.5x physical damage.",
	}

	// ── Skill Graph (peakgo/skilldata) ───────────────────────────────────
	// Build the skill tree alongside the legacy registry.
	// Prerequisites: Power Strike (3) → Fireball (1) → Thunderclap (2)
	GlobalSkillGraph = skilldata.NewSkillGraph()
	_ = GlobalSkillGraph.RegisterSkill(skilldata.SkillEntry{
		ID: 1, Name: "Fireball",
		Class:         skilldata.SkillCast,
		Target:        skilldata.TargetEnemy,
		ManaCost:      20,
		RequiredLevel: 3,
		Range:         8.0,
		DamageMult:    2.5,
		Prerequisites: []int32{3}, // Power Strike first
	})
	_ = GlobalSkillGraph.RegisterSkill(skilldata.SkillEntry{
		ID: 2, Name: "Thunderclap",
		Class:         skilldata.SkillCast,
		Target:        skilldata.TargetEnemy,
		ManaCost:      35,
		RequiredLevel: 6,
		Range:         6.0,
		DamageMult:    4.0,
		Prerequisites: []int32{1}, // Fireball first
	})
	_ = GlobalSkillGraph.RegisterSkill(skilldata.SkillEntry{
		ID: 3, Name: "Power Strike",
		Class:         skilldata.SkillInstant,
		Target:        skilldata.TargetEnemy,
		ManaCost:      10,
		RequiredLevel: 1,
		Range:         5.0,
		DamageMult:    1.5,
	})
}

// SkillGraphGetPrereqs returns the full prerequisite chain for a skill ID.
// Wraps peakgo/skilldata.SkillGraph.GetPrerequisiteChain.
func SkillGraphGetPrereqs(skillID uint64) []int32 {
	if GlobalSkillGraph == nil {
		return nil
	}
	return GlobalSkillGraph.GetPrerequisiteChain(int32(skillID))
}

// SkillGraphCanLearn checks if a skill is learnable given known skills and player level.
func SkillGraphCanLearn(skillID uint64, knownSkills []int32, playerLevel int32) (bool, error) {
	if GlobalSkillGraph == nil {
		return false, skilldata.ErrSkillNotFound
	}
	return GlobalSkillGraph.CanLearn(int32(skillID), knownSkills, playerLevel)
}
