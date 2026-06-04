package game

import (
	"server/ecs"
)

// HandleSkillCastingSystem processes a player's skill casting logic using the SkillPipeline.
// This replaces the old procedural path with a modular pipeline.
func HandleSkillCastingSystem(casterID ecs.Entity, skillID uint64, targetID ecs.Entity) (string, bool) {
	pipeline := NewSkillPipeline()
	_, errMsg := pipeline.Execute(casterID, targetID, skillID)
	if errMsg != "" {
		return errMsg, false
	}

	// Read back the personal feedback from the context
	// The pipeline already handles all the damage, death, loot, XP, broadcast.
	// Return success with minimal feedback.
	return "", true
}
