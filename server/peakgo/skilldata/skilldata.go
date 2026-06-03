package skilldata

import "errors"

// Sentinel errors for skill graph operations.
var (
	ErrDuplicateSkill     = errors.New("skilldata: duplicate skill ID")
	ErrSkillNotFound      = errors.New("skilldata: skill not found")
	ErrLevelTooLow        = errors.New("skilldata: player level too low")
	ErrMissingPrerequisite = errors.New("skilldata: missing prerequisite")
)

// SkillClass defines how a skill is executed.
type SkillClass uint8

const (
	SkillInstant SkillClass = iota // instant cast, no animation
	SkillCast                      // has cast time, can be interrupted
	SkillChannel                   // channeled (tick-based effect over time)
	SkillPassive                   // always active, no manual activation
	SkillToggle                    // toggle on/off (e.g. aura)
)

// SkillTarget defines who/what the skill affects.
type SkillTarget uint8

const (
	TargetNone     SkillTarget = 0
	TargetSelf     SkillTarget = 1
	TargetEnemy    SkillTarget = 2
	TargetFriendly SkillTarget = 3
	TargetPoint    SkillTarget = 4 // ground target
	TargetArea     SkillTarget = 5 // AoE around self or target
)

// SkillEntry holds the full definition of a skill.
// All fields are flat and zero-alloc safe for hot-path reads.
type SkillEntry struct {
	ID            int32
	Name          string
	Class         SkillClass
	Target        SkillTarget
	CastTimeMS    int32
	CooldownMS    int32
	Range         float64
	ManaCost      int32
	RequiredLevel int32
	MaxRank       int32
	DamageMult    float64
	HealMult      float64
	Prerequisites []int32 // skill IDs required to unlock this
	Children      []int32 // skills that require this as prerequisite (for tree traversal)
}

// SkillGraph manages the skill tree and provides dependency/classification queries.
type SkillGraph struct {
	skills map[int32]*SkillEntry
}

// NewSkillGraph creates an empty SkillGraph.
func NewSkillGraph() *SkillGraph {
	return &SkillGraph{
		skills: make(map[int32]*SkillEntry),
	}
}

// RegisterSkill adds a skill to the graph. Returns error on duplicate ID.
func (sg *SkillGraph) RegisterSkill(s SkillEntry) error {
	if _, exists := sg.skills[s.ID]; exists {
		return ErrDuplicateSkill
	}
	entry := &SkillEntry{
		ID:            s.ID,
		Name:          s.Name,
		Class:         s.Class,
		Target:        s.Target,
		CastTimeMS:    s.CastTimeMS,
		CooldownMS:    s.CooldownMS,
		Range:         s.Range,
		ManaCost:      s.ManaCost,
		RequiredLevel: s.RequiredLevel,
		MaxRank:       s.MaxRank,
		DamageMult:    s.DamageMult,
		HealMult:      s.HealMult,
	}
	// Deep copy prerequisite slices
	if len(s.Prerequisites) > 0 {
		entry.Prerequisites = make([]int32, len(s.Prerequisites))
		copy(entry.Prerequisites, s.Prerequisites)
	}
	if len(s.Children) > 0 {
		entry.Children = make([]int32, len(s.Children))
		copy(entry.Children, s.Children)
	}

	sg.skills[s.ID] = entry

	// Register in parent's children list (bidirectional graph maintenance)
	for _, prereqID := range s.Prerequisites {
		if parent, exists := sg.skills[prereqID]; exists {
			parent.Children = append(parent.Children, s.ID)
		}
	}

	return nil
}

// GetSkill returns the skill entry by ID.
func (sg *SkillGraph) GetSkill(id int32) (*SkillEntry, bool) {
	s, ok := sg.skills[id]
	return s, ok
}

// CanLearn checks if a skill is learnable given the player's known skills and level.
func (sg *SkillGraph) CanLearn(skillID int32, knownSkills []int32, playerLevel int32) (bool, error) {
	skill, ok := sg.skills[skillID]
	if !ok {
		return false, ErrSkillNotFound
	}
	if playerLevel < skill.RequiredLevel {
		return false, ErrLevelTooLow
	}
	// Check all prerequisites are met
	knownSet := make(map[int32]struct{}, len(knownSkills))
	for _, id := range knownSkills {
		knownSet[id] = struct{}{}
	}
	for _, prereqID := range skill.Prerequisites {
		if _, has := knownSet[prereqID]; !has {
			return false, ErrMissingPrerequisite
		}
	}
	return true, nil
}

// GetPrerequisiteChain returns the full chain of prerequisites for a skill (DFS).
func (sg *SkillGraph) GetPrerequisiteChain(skillID int32) []int32 {
	result := make([]int32, 0, 8)
	visited := make(map[int32]struct{})
	sg.collectPrereqs(skillID, visited, &result)
	return result
}

func (sg *SkillGraph) collectPrereqs(id int32, visited map[int32]struct{}, result *[]int32) {
	if _, seen := visited[id]; seen {
		return
	}
	skill, ok := sg.skills[id]
	if !ok {
		return
	}
	visited[id] = struct{}{}
	for _, prereqID := range skill.Prerequisites {
		sg.collectPrereqs(prereqID, visited, result)
	}
	*result = append(*result, id)
}

// GetTree returns the full tree as a flat slice (all skills).
func (sg *SkillGraph) GetTree() []SkillEntry {
	result := make([]SkillEntry, 0, len(sg.skills))
	for _, s := range sg.skills {
		result = append(result, *s)
	}
	return result
}

// Count returns the number of registered skills.
func (sg *SkillGraph) Count() int {
	return len(sg.skills)
}

// GetSkillsByClass returns all skill IDs of a given class.
func (sg *SkillGraph) GetSkillsByClass(class SkillClass) []int32 {
	result := make([]int32, 0, len(sg.skills)/5)
	for id, s := range sg.skills {
		if s.Class == class {
			result = append(result, id)
		}
	}
	return result
}
