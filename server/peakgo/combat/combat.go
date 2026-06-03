package combat

import (
	"server/peakgo/rng"
)

// ─── Stats ──────────────────────────────────────────────────────────────────

// Stats holds the combat-relevant attributes of an entity (player or monster).
type Stats struct {
	Level        int
	MaxHP        int
	CurrentHP    int
	MaxMP        int
	CurrentMP    int
	Attack       int
	MagicAttack  int
	Defense      int
	MagicDefense int
	HitRate      int
	DodgeRate    int
	CritRate     int
	CritDamage   int
}

// NewStats creates a Stats with default per-level values.
func NewStats(level int) Stats {
	baseHP := 100 + (level-1)*25
	baseMP := 50 + (level-1)*10

	return Stats{
		Level:        level,
		MaxHP:        baseHP,
		CurrentHP:    baseHP,
		MaxMP:        baseMP,
		CurrentMP:    baseMP,
		Attack:       10 + level*2,
		MagicAttack:  10 + level*2,
		Defense:      5 + level,
		MagicDefense: 5 + level,
		HitRate:      850,
		DodgeRate:    50,
		CritRate:     50,
		CritDamage:   1500,
	}
}

// ─── Damage Type ────────────────────────────────────────────────────────────

type DamageType uint8

const (
	DamagePhysical DamageType = iota
	DamageMagical
	DamagePure
)

// ─── Element Types ────────────────────────────────────────────────────────────

// Element represents an elemental affinity for attacks and resistances.
type Element uint8

const (
	ElementNone Element = iota
	ElementFire
	ElementWater
	ElementWind
	ElementEarth
	ElementLight
	ElementDark
)

// Element effectiveness table: [attacker][defender] -> multiplier (per-mille)
var elementEffectiveness = [7][7]int16{
	// None  Fire  Water Wind  Earth Light Dark
	{1000, 1000, 1000, 1000, 1000, 1000, 1000}, // None
	{1000, 500, 2000, 500, 1000, 1000, 1000},   // Fire -> Water weak, Wind resist
	{1000, 500, 500, 2000, 1000, 1000, 1000},   // Water -> Wind weak, Fire resist
	{1000, 2000, 500, 500, 1000, 1000, 1000},   // Wind -> Fire weak, Water resist
	{1000, 1000, 1000, 1000, 500, 1000, 2000},  // Earth -> Dark weak, Earth resist
	{1000, 1000, 1000, 1000, 2000, 1000, 2000}, // Light -> Earth weak, Dark weak
	{1000, 1000, 1000, 1000, 500, 500, 1000},   // Dark -> Light resist, Earth resist
}

// GetElementEffectiveness returns the multiplier for attacker element vs defender element.
func GetElementEffectiveness(attacker, defender Element) int {
	return int(elementEffectiveness[attacker][defender])
}

// ─── Status Effects ─────────────────────────────────────────────────────────

type StatusEffect uint8

const (
	StatusNone StatusEffect = iota
	StatusStun
	StatusBurn
	StatusPoison
	StatusSlow
	StatusFreeze
	StatusSilence
	StatusBerserk
)

// StatusEffectInstance represents a single active status effect on an entity.
type StatusEffectInstance struct {
	Type           StatusEffect
	Stacks         int
	RemainingTicks int
}

// ─── Damage Modifiers & Combat Result ───────────────────────────────────────

// DamageModifiers provides parameters for damage resolution.
type DamageModifiers struct {
	IsCrit          bool // Force critical hit (default: calculated)
	IsGuaranteed    bool // Bypasses hit/dodge check
	DamageType      DamageType
	Element         Element // Element of the attack
	DefenderElement Element // Element of the defender (used for effectiveness calc)
	SkillMultiplier int     // Per-mille multiplier for skill (1000 = 100%)
}

// CombatResult holds the resolved outcome of a damage calculation.
type CombatResult struct {
	Hit           bool
	Dodged        bool
	IsCrit        bool
	DamageDealt   int
	RawDamage     int
	Mitigated     int
	ElementMulti  int // Element effectiveness multiplier (per-mille)
	StatusApplied StatusEffectInstance
}

// ─── Damage Resolution ──────────────────────────────────────────────────────

// ResolvePhysical resolves physical damage between attacker and defender.
func ResolvePhysical(attacker, defender *Stats, modifiers DamageModifiers) CombatResult {
	var result CombatResult

	// Hit check
	if !modifiers.IsGuaranteed {
		hitChance := attacker.HitRate - defender.DodgeRate
		if hitChance < 100 {
			hitChance = 100 // Minimum 10% hit chance
		}
		if hitChance > 995 {
			hitChance = 995 // Maximum 99.5%
		}
		if rng.Intn(1000) >= hitChance {
			result.Dodged = true
			return result
		}
	}

	result.Hit = true

	// Raw damage
	skillMulti := modifiers.SkillMultiplier
	if skillMulti <= 0 {
		skillMulti = 1000 // Default auto-attack
	}
	result.RawDamage = (attacker.Attack * skillMulti) / 1000
	if result.RawDamage < 1 {
		result.RawDamage = 1
	}

	// Defense mitigation (physical defense applies at 50%)
	result.Mitigated = defender.Defense * 50 / 100
	if result.Mitigated > result.RawDamage {
		result.Mitigated = result.RawDamage
	}
	afterDef := result.RawDamage - result.Mitigated

	// Element multiplier (uses defender's element for effectiveness)
	result.ElementMulti = GetElementEffectiveness(modifiers.Element, modifiers.DefenderElement)
	afterElement := (afterDef * result.ElementMulti) / 1000
	if afterElement < 1 {
		afterElement = 1
	}

	// Crit check
	isCrit := modifiers.IsCrit
	if !isCrit && result.Hit {
		critRoll := rng.Intn(1000)
		isCrit = critRoll < attacker.CritRate
	}

	if isCrit {
		result.IsCrit = true
		result.DamageDealt = (afterElement * attacker.CritDamage) / 1000
	} else {
		result.DamageDealt = afterElement
	}

	if result.DamageDealt < 1 {
		result.DamageDealt = 1
	}

	return result
}

// ResolveMagical resolves magical damage between attacker and defender.
func ResolveMagical(attacker, defender *Stats, modifiers DamageModifiers) CombatResult {
	var result CombatResult

	// Hit check (magic has higher base hit rate)
	if !modifiers.IsGuaranteed {
		hitChance := attacker.HitRate + 50 - defender.DodgeRate/2
		if hitChance < 100 {
			hitChance = 100
		}
		if hitChance > 995 {
			hitChance = 995
		}
		if rng.Intn(1000) >= hitChance {
			result.Dodged = true
			return result
		}
	}

	result.Hit = true

	skillMulti := modifiers.SkillMultiplier
	if skillMulti <= 0 {
		skillMulti = 1000
	}
	result.RawDamage = (attacker.MagicAttack * skillMulti) / 1000
	if result.RawDamage < 1 {
		result.RawDamage = 1
	}

	// Magic defense applies at 75% for magical attacks
	result.Mitigated = defender.MagicDefense * 75 / 100
	if result.Mitigated > result.RawDamage {
		result.Mitigated = result.RawDamage
	}
	afterDef := result.RawDamage - result.Mitigated

	// Element multiplier (uses defender's element for effectiveness)
	result.ElementMulti = GetElementEffectiveness(modifiers.Element, modifiers.DefenderElement)
	afterElement := (afterDef * result.ElementMulti) / 1000
	if afterElement < 1 {
		afterElement = 1
	}

	// Crit check (magic crits are rarer)
	isCrit := modifiers.IsCrit
	if !isCrit && result.Hit {
		critRoll := rng.Intn(1000)
		isCrit = critRoll < attacker.CritRate/2
	}

	if isCrit {
		result.IsCrit = true
		result.DamageDealt = (afterElement * attacker.CritDamage) / 1000
	} else {
		result.DamageDealt = afterElement
	}

	if result.DamageDealt < 1 {
		result.DamageDealt = 1
	}

	return result
}

// ResolvePure resolves pure (unmitigated) damage. Pure damage ignores all defenses.
func ResolvePure(attacker *Stats, modifiers DamageModifiers) CombatResult {
	var result CombatResult
	result.Hit = true

	skillMulti := modifiers.SkillMultiplier
	if skillMulti <= 0 {
		skillMulti = 1000
	}

	result.RawDamage = (attacker.Attack * skillMulti) / 1000
	if result.RawDamage < 1 {
		result.RawDamage = 1
	}

	// Pure damage still uses element effectiveness but without defender element
	result.ElementMulti = GetElementEffectiveness(modifiers.Element, ElementNone)
	result.DamageDealt = (result.RawDamage * result.ElementMulti) / 1000
	if result.DamageDealt < 1 {
		result.DamageDealt = 1
	}

	return result
}

// ─── DoT (Damage over Time) ──────────────────────────────────────────────────

// DoTInstance handles damage-over-time effects (burn, poison).
type DoTInstance struct {
	DamagePerTick  int
	RemainingTicks int
	TotalTicks     int
	Element        Element
	SourceEntityID uint64
}

// Tick processes one tick of damage and returns the amount dealt.
func (d *DoTInstance) Tick() int {
	if d.RemainingTicks <= 0 {
		return 0
	}
	d.RemainingTicks--
	return d.DamagePerTick
}

// Expired checks whether the DoT effect has expired.
func (d *DoTInstance) Expired() bool {
	return d.RemainingTicks <= 0
}

// ─── Healing ────────────────────────────────────────────────────────────────

// HealResult holds the resolved outcome of a healing operation.
type HealResult struct {
	Amount   int
	Overheal int
	IsCrit   bool
}

// CalculateHealing computes a healing amount from source level, base healing,
// skill multiplier, and optional crit.
func CalculateHealing(sourceLevel, baseHeal, skillMulti int, canCrit bool) HealResult {
	if skillMulti <= 0 {
		skillMulti = 1000
	}

	heal := (baseHeal * skillMulti) / 1000
	if heal < 1 {
		heal = 1
	}

	var result HealResult
	result.Amount = heal

	if canCrit && rng.Intn(1000) < 50 { // 5% crit rate for heals
		result.IsCrit = true
		result.Amount = (heal * 1500) / 1000 // 1.5x crit multiplier
	}

	return result
}

// ApplyHealing applies a HealResult to current HP, clamping to max HP.
func ApplyHealing(currentHP, maxHP int, heal HealResult) (newHP, overheal int) {
	newHP = currentHP + heal.Amount
	if newHP > maxHP {
		overheal = newHP - maxHP
		newHP = maxHP
	}
	return newHP, overheal
}
