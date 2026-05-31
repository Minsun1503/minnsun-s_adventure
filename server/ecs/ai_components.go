package ecs

// AIState represents the current behavioral state of a monster entity.
type AIState uint8

const (
	AIStateIdle      AIState = iota // at spawn, waiting for idle timer
	AIStateRoaming                  // random walk within spawn radius
	AIStateChasing                  // pursuing a player target
	AIStateAttacking                // target in melee range, firing AttackSystem
	AIStateReturning                // leash triggered, walking back to spawn
)

func (s AIState) String() string {
	switch s {
	case AIStateIdle:
		return "Idle"
	case AIStateRoaming:
		return "Roaming"
	case AIStateChasing:
		return "Chasing"
	case AIStateAttacking:
		return "Attacking"
	case AIStateReturning:
		return "Returning"
	default:
		return "Unknown"
	}
}

// AIComponent holds all mutable AI runtime state for a monster entity.
// Stored as an inline value in the ECS registry — copy-modify-overwrite on every mutation.
//
// Design rule: AIComponent owns behavioral state only.
// Position and stats are owned by their respective components and
// mutated exclusively through MovementSystem and DamageSystem.
type AIComponent struct {
	State          AIState
	SpawnX         int     // world X at spawn time — leash anchor point
	SpawnZ         int     // world Z at spawn time
	SpawnRadius    int     // max roam distance from spawn (world units)
	AggroRadius    float64 // distance at which monster notices players
	LeashRadius    float64 // distance at which monster gives up the chase
	MeleeRange     float64 // distance at which monster can strike
	TargetID       Entity  // current chase/attack target (0 = no target)
	AttackTick     int     // counts game ticks since last attack
	AttackCooldown int     // ticks required between attacks (e.g. 4 = 1 atk/sec at 250ms)
	IdleTick       int     // counts ticks spent idle before roaming begins
	IdleDuration   int     // ticks before idle → roaming transition
}
