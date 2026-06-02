package ecs

import (
	"server/peakgo/timer"
)

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
	State       AIState
	TargetID    Entity
	SpawnX      int
	SpawnZ      int
	SpawnRadius int
	AggroRadius float64
	LeashRadius int // Đổi sang int để chạy hệ số nguyên siêu tốc
	MeleeRange  int // Đổi sang int để chạy hệ số nguyên siêu tốc

	// --- Timers của PeakGo ---
	AttackTimer timer.TickTimer // Thay thế hoàn toàn AttackTick + AttackCooldown
	IdleTimer   timer.TickTimer // Thay thế hoàn toàn IdleTick + IdleDuration
	PathTimer   timer.TickTimer // Chặn đứng việc gọi FindPath mọi tick (Ví dụ: 4 tick/lần = 1 giây)

	// --- Điểm neo mục tiêu di chuyển ---
	RoamTargetX int
	RoamTargetZ int
}
