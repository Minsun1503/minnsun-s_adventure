package game

import (
	"fmt"
	"math/rand"
	"server/ecs"
	"server/world"
)

// TickAI is the per-entity entry point called once per game loop tick.
// It reads the current AIComponent, runs the state machine,
// and writes back the mutated copy via SetAI (copy-modify-overwrite).
//
// All world mutations (position, HP) go through existing systems:
//   - Movement → MovementSystem
//   - Damage   → AttackSystem
//
// AI never writes ECS directly except for its own AIComponent.
func TickAI(id ecs.Entity) {
	ai, ok := ecs.GlobalRegistry.GetAI(id)
	if !ok {
		return
	}

	// Advance tick counters unconditionally.
	ai.AttackTick++
	ai.IdleTick++

	switch ai.State {
	case ecs.AIStateIdle:
		ai = tickIdle(id, ai)
	case ecs.AIStateRoaming:
		ai = tickRoaming(id, ai)
	case ecs.AIStateChasing:
		ai = tickChasing(id, ai)
	case ecs.AIStateAttacking:
		ai = tickAttacking(id, ai)
	case ecs.AIStateReturning:
		ai = tickReturning(id, ai)
	}

	// Write back the mutated AI state (copy-modify-overwrite).
	ecs.GlobalRegistry.SetAI(id, ai)
}

// --- State handlers ---
// Each handler receives the current AIComponent by value, mutates it locally,
// and returns the new value. No direct ECS writes except through systems.

func tickIdle(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if target, found := findNearestPlayer(id, ai.AggroRadius); found {
		ai.TargetID = target
		ai.State = ecs.AIStateChasing
		logStateChange(id, ecs.AIStateIdle, ecs.AIStateChasing)
		return ai
	}

	if ai.IdleTick >= ai.IdleDuration {
		ai.IdleTick = 0
		ai.State = ecs.AIStateRoaming
		logStateChange(id, ecs.AIStateIdle, ecs.AIStateRoaming)
	}

	return ai
}

func tickRoaming(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if target, found := findNearestPlayer(id, ai.AggroRadius); found {
		ai.TargetID = target
		ai.State = ecs.AIStateChasing
		logStateChange(id, ecs.AIStateRoaming, ecs.AIStateChasing)
		return ai
	}

	pos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		ai.State = ecs.AIStateIdle
		return ai
	}

	nextX, nextZ, moved := roamStep(pos, ai)
	if !moved {
		ai.State = ecs.AIStateIdle
		logStateChange(id, ecs.AIStateRoaming, ecs.AIStateIdle)
		return ai
	}

	MovementSystem(id, nextX, nextZ)
	return ai
}

func tickChasing(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if ai.TargetID == 0 {
		ai.State = ecs.AIStateReturning
		return ai
	}

	targetPos, ok := ecs.GlobalRegistry.GetPosition(ai.TargetID)
	if !ok {
		ai.TargetID = 0
		ai.State = ecs.AIStateReturning
		logStateChange(id, ecs.AIStateChasing, ecs.AIStateReturning)
		return ai
	}

	myPos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		return ai
	}

	if distanceSq(myPos, targetPos) > ai.LeashRadius*ai.LeashRadius {
		ai.TargetID = 0
		ai.State = ecs.AIStateReturning
		logStateChange(id, ecs.AIStateChasing, ecs.AIStateReturning)
		return ai
	}

	if distanceSq(myPos, targetPos) <= ai.MeleeRange*ai.MeleeRange {
		ai.State = ecs.AIStateAttacking
		logStateChange(id, ecs.AIStateChasing, ecs.AIStateAttacking)
		return ai
	}

	nextX, nextZ := world.FindPath(myPos, targetPos)
	MovementSystem(id, nextX, nextZ)
	return ai
}

func tickAttacking(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if ai.TargetID == 0 {
		ai.State = ecs.AIStateReturning
		return ai
	}

	targetPos, targetOk := ecs.GlobalRegistry.GetPosition(ai.TargetID)
	myPos, myOk := ecs.GlobalRegistry.GetPosition(id)
	if !targetOk || !myOk {
		ai.TargetID = 0
		ai.State = ecs.AIStateReturning
		logStateChange(id, ecs.AIStateAttacking, ecs.AIStateReturning)
		return ai
	}

	if distanceSq(myPos, targetPos) > ai.MeleeRange*ai.MeleeRange {
		ai.State = ecs.AIStateChasing
		logStateChange(id, ecs.AIStateAttacking, ecs.AIStateChasing)
		return ai
	}

	if ai.AttackTick < ai.AttackCooldown {
		return ai
	}
	ai.AttackTick = 0

	result, errMsg := AttackSystem(id, ai.TargetID)
	if errMsg != "" {
		ai.TargetID = 0
		ai.State = ecs.AIStateReturning
		return ai
	}

	if result.Killed {
		ai.TargetID = 0
		ai.State = ecs.AIStateReturning
		logStateChange(id, ecs.AIStateAttacking, ecs.AIStateReturning)
	}

	return ai
}

func tickReturning(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	pos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		return ai
	}

	spawnPos := ecs.PositionComponent{X: ai.SpawnX, Z: ai.SpawnZ}

	if distanceSq(pos, spawnPos) <= 1 {
		ai.State = ecs.AIStateIdle
		ai.IdleTick = 0
		logStateChange(id, ecs.AIStateReturning, ecs.AIStateIdle)
		return ai
	}

	nextX, nextZ := world.FindPath(pos, spawnPos)
	MovementSystem(id, nextX, nextZ)
	return ai
}

// --- Spatial helpers ---

func findNearestPlayer(monsterID ecs.Entity, aggroRadius float64) (ecs.Entity, bool) {
	players := world.GetNearbyPlayers(monsterID, aggroRadius)
	if len(players) == 0 {
		return 0, false
	}

	myPos, ok := ecs.GlobalRegistry.GetPosition(monsterID)
	if !ok {
		return players[0].ID, true
	}

	nearest := players[0]
	nearestDSq := distanceSqPos(myPos, players[0].Pos)
	for _, p := range players[1:] {
		dsq := distanceSqPos(myPos, p.Pos)
		if dsq < nearestDSq {
			nearest = p
			nearestDSq = dsq
		}
	}
	return nearest.ID, true
}

func roamStep(pos ecs.PositionComponent, ai ecs.AIComponent) (int, int, bool) {
	const attempts = 8

	for i := 0; i < attempts; i++ {
		dx := rand.Intn(5) - 2
		dz := rand.Intn(5) - 2
		if dx == 0 && dz == 0 {
			continue
		}

		nx := pos.X + dx
		nz := pos.Z + dz

		ddx := float64(nx - ai.SpawnX)
		ddz := float64(nz - ai.SpawnZ)
		if ddx*ddx+ddz*ddz > float64(ai.SpawnRadius*ai.SpawnRadius) {
			continue
		}

		if nx < 0 || nx > 100 || nz < 0 || nz > 100 {
			continue
		}

		if world.IsTileBlocked(pos.MapID, nx, nz) {
			continue
		}

		return nx, nz, true
	}
	return 0, 0, false
}

func distanceSq(a, b ecs.PositionComponent) float64 {
	dx := float64(a.X - b.X)
	dz := float64(a.Z - b.Z)
	return dx*dx + dz*dz
}

func distanceSqPos(a ecs.PositionComponent, b ecs.PositionComponent) float64 {
	return distanceSq(a, b)
}

func logStateChange(id ecs.Entity, from, to ecs.AIState) {
	meta, ok := ecs.GlobalRegistry.GetMetadata(id)
	name := fmt.Sprintf("entity_%d", id)
	if ok {
		name = meta.Name
	}
	fmt.Printf("[AI] %s: %s → %s\n", name, from, to)
}
