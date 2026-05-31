package models

// SkillTemplate defines the static, read-only configuration rules for an ability.
type SkillTemplate struct {
	ID       uint64
	Name     string
	ManaCost int
	DamMult  float64 // Damage multiplier (e.g., 2.5 means 250% base damage strike)
}

// Global registry holding all skill configurations in memory
var SkillRegistry = make(map[uint64]SkillTemplate)

// InitializeSkillRegistry populates available abilities
func InitializeSkillRegistry() {
	SkillRegistry[1] = SkillTemplate{
		ID:       1,
		Name:     "Fireball",
		ManaCost: 20,
		DamMult:  2.5,
	}
	SkillRegistry[2] = SkillTemplate{
		ID:       2,
		Name:     "Thunderclap",
		ManaCost: 35,
		DamMult:  4.0,
	}
}
