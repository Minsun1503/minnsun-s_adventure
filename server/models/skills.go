package models

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

// Global registry holding all skill configurations in memory
var SkillRegistry = make(map[uint64]SkillDefinition)

// InitializeSkillRegistry populates available abilities
func InitializeSkillRegistry() {
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
}
