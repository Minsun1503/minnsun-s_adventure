// Package combat provides a zero-allocation combat resolution engine
// for the Minnsun's Adventure 2.5D MMORPG server.
//
// # Why this package exists
//
// Combat logic involves damage formulas, hit/miss/dodge/crit resolution,
// elemental modifiers, and status effect applications. Centralizing this
// into a single package ensures consistency, testability, and zero-alloc
// hot-path performance.
//
// # Peak Go Contract
//
// Every resolve function produces zero heap allocations. Results are
// returned as inline value types. All lookup tables use pre-computed
// arrays indexed by level delta.
package combat

import (
	"server/peakgo/rng"
)

// ─── Combatant Stats ──────────────────────────────────────────────────────────

// Stats represents the combat-relevant attributes of an entity.
// Embedded into ECS components for each player/monster.
// All values are inline — no heap allocation.
type Stats struct {
	Level            int
	MaxHP, CurrentHP int
	MaxMP, CurrentMP int
	Attack           int // Physical attack power
	MagicAttack      int // Magical attack power
	Defense          int // Physical defense
	MagicDefense     int // Magical defense
	HitRate          int // Base hit chance (0-1000, per-mille)
	DodgeRate        int // Base dodge chance (0-1000, per-mille)
	CritRate         int // Base crit chance (0-1000, per-mille)
	CritDamage       int // Crit damage multiplier (1000 = 100%, 1500 = 150%)
}

// NewStats creates default stats for a given level.
func NewStats(level int) Stats {
	return Stats{
		Level:        level,
		Attack:       10 + level*2,
		MagicAttack:  10 + level*2,
		Defense:      5 + level,
		MagicDefense: 5 + level,
		HitRate:      850,  // 85% base
		DodgeRate:    50,   // 5% base
		CritRate:     50,   // 5% base
		CritDamage:   1500, // 150% crit damage
	}
}

// ─── Damage Types ─────────────────────────────────────────────────────────────

// DamageType classifies the source of damage.
type DamageType uint8

const (
	DamagePhysical DamageType = iota
	DamageMagical
	DamagePure // True damage (ignores all defense)
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
// 1000 = normal, 2000 = 2x weak, 500 = 0.5x resist, 0 = immune
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
// Returns value in per-mille (1000 = 100% normal damage).
func GetElementEffectiveness(attacker, defender Element) int {
	return int(elementEffectiveness[attacker][defender])
}

// ─── Status Effects ───────────────────────────────────────────────────────────

// StatusEffect represents a temporary combat condition.
type StatusEffect uint8

const (
	StatusNone    StatusEffect = iota
	StatusStun                 // Cannot act
	StatusBurn                 // Take damage over time (fire)
	StatusPoison               // Take damage over time (toxic)
	StatusSlow                 // Reduced movement speed
	StatusFreeze               // Cannot move or act
	StatusSilence              // Cannot cast magic
	StatusBerserk              // Increased attack, decreased defense
)

// StatusEffectInstance is a concrete active status effect on an entity.
type StatusEffectInstance struct {
	Type           StatusEffect
	Stacks         int // Number of stacks (for stacking effects)
	RemainingTicks int // Remaining duration in game ticks
}

// ─── Resolution ───────────────────────────────────────────────────────────────

// DamageModifiers carries optional parameters for a damage calculation.
// Zero value = default behavior.
type DamageModifiers struct {
	IsCrit          bool       // Force critical hit (default: calculated)
	IsGuaranteed    bool       // Ignore dodge/block (default: calculated)
	DamageType      DamageType // Override damage type (default: physical)
	Element         Element    // Element of the attack
	SkillMultiplier int        // Per-mille multiplier for skill (1000 = 100%)
}

// CombatResult holds the complete result of a single attack resolution.
// Value type — zero heap allocation.
type CombatResult struct {
	Hit           bool
	Dodged        bool
	IsCrit        bool
	DamageDealt   int
	RawDamage     int                  // Before defense reduction
	Mitigated     int                  // Amount reduced by defense
	ElementMulti  int                  // Element effectiveness multiplier (per-mille)
	StatusApplied StatusEffectInstance // Status effect applied (if any)
}

// ResolvePhysical resolves a physical attack from attacker to defender.
// Zero alloc per call. Results are returned as a value type.
//
// Formula:
//
//	raw = AttackerATK * skillMulti / 1000
//	mitigated = DefenderDEF * 0.5 (half DEF applies to physical)
//	final = max(1, raw - mitigated) * elementMulti / 1000
//	crit = roll crit, if crit: final = final * critDamage / 1000
//
// Returns CombatResult with all fields populated.
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

	// Element multiplier
	result.ElementMulti = GetElementEffectiveness(modifiers.Element, ElementNone)
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

// ResolveMagical resolves a magical attack from attacker to defender.
// Similar to physical but uses MagicAttack vs MagicDefense.
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

	// Element multiplier
	result.ElementMulti = GetElementEffectiveness(modifiers.Element, ElementNone)
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

// ResolvePure resolves true damage that ignores all defenses.
// Uses only skill multiplier and element modifier.
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

	result.ElementMulti = GetElementEffectiveness(modifiers.Element, ElementNone)
	result.DamageDealt = (result.RawDamage * result.ElementMulti) / 1000
	if result.DamageDealt < 1 {
		result.DamageDealt = 1
	}

	return result
}

// ─── Damage Over Time ─────────────────────────────────────────────────────────

// DoTInstance represents a damage-over-time effect applied to an entity.
type DoTInstance struct {
	DamagePerTick  int
	RemainingTicks int
	TotalTicks     int
	Element        Element
	SourceEntityID uint64 // For kill credit
}

// Tick returns the damage for this tick and decrements the remaining ticks.
// Returns 0 if the DoT has expired.
func (dot *DoTInstance) Tick() int {
	if dot.RemainingTicks <= 0 {
		return 0
	}
	dot.RemainingTicks--
	return dot.DamagePerTick
}

// Expired reports whether this DoT has completed.
func (dot *DoTInstance) Expired() bool {
	return dot.RemainingTicks <= 0
}

// ─── Healing ──────────────────────────────────────────────────────────────────

// HealResult holds the result of a healing action.
type HealResult struct {
	Amount   int
	Overheal int
	IsCrit   bool
}

// CalculateHealing computes healing from source to target.
// Formula: baseHeal * skillMulti / 1000, optionally crit.
func CalculateHealing(sourceLevel, baseHeal, skillMulti int, canCrit bool) HealResult {
	if skillMulti <= 0 {
		skillMulti = 1000
	}

	amount := (baseHeal * skillMulti) / 1000
	if amount < 1 {
		amount = 1
	}

	result := HealResult{Amount: amount}

	if canCrit {
		critRoll := rng.Intn(1000)
		if critRoll < 50 { // 5% heal crit chance
			result.IsCrit = true
			result.Amount = (result.Amount * 1500) / 1000 // 150% heal crit
		}
	}

	return result
}

// ApplyHealing applies healing to a target's HP, respecting max HP.
// Returns the actual amount healed and any overheal.
func ApplyHealing(currentHP, maxHP int, heal HealResult) (newHP int, overheal int) {
	newHP = currentHP + heal.Amount
	if newHP > maxHP {
		overheal = newHP - maxHP
		newHP = maxHP
	}
	return newHP, overheal
}
