// Package game - CombatAccumulator moved to server/ecs/combat_accumulator.go.
// This file exists only to prevent import cycle errors during migration.
package game

import "server/ecs"

// NewCombatAccumulator is a convenience alias for ecs.NewCombatAccumulator.
// Deprecated: use ecs.NewCombatAccumulator directly.
func NewCombatAccumulator() *ecs.CombatAccumulator {
	return ecs.NewCombatAccumulator()
}
