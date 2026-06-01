package game

import (
	"fmt"
	"server/ecs"
	"server/peakgo/gmath"
	"server/peakgo/loggate"
	"server/peakgo/rng"
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

	if gmath.DistanceSq(myPos.X, myPos.Z, targetPos.X, targetPos.Z) > ai.LeashRadius*ai.LeashRadius {
		ai.TargetID = 0
		ai.State = ecs.AIStateReturning
		logStateChange(id, ecs.AIStateChasing, ecs.AIStateReturning)
		return ai
	}

	if gmath.DistanceSq(myPos.X, myPos.Z, targetPos.X, targetPos.Z) <= ai.MeleeRange*ai.MeleeRange {
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

	if gmath.DistanceSq(myPos.X, myPos.Z, targetPos.X, targetPos.Z) > ai.MeleeRange*ai.MeleeRange {
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

	if gmath.DistanceSq(pos.X, pos.Z, ai.SpawnX, ai.SpawnZ) <= 1 {
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
	defer world.FreeNearbyPlayers(players)

	myPos, ok := ecs.GlobalRegistry.GetPosition(monsterID)
	if !ok {
		return players[0].ID, true
	}

	nearest := players[0]
	nearestDSq := gmath.DistanceSq(myPos.X, myPos.Z, players[0].Pos.X, players[0].Pos.Z)
	for _, p := range players[1:] {
		dsq := gmath.DistanceSq(myPos.X, myPos.Z, p.Pos.X, p.Pos.Z)
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
		// rng.Intn: pooled RNG, 0 allocs vs global rand mutex contention.
		dx := rng.Intn(5) - 2 // [-2, +2]
		dz := rng.Intn(5) - 2
		if dx == 0 && dz == 0 {
			continue
		}

		nx := pos.X + dx
		nz := pos.Z + dz

		// gmath.DistanceSq: no-sqrt spawn radius check.
		if gmath.DistanceSq(nx, nz, ai.SpawnX, ai.SpawnZ) > float64(ai.SpawnRadius*ai.SpawnRadius) {
			continue
		}

		// gmath.InBounds: single call replaces 4 comparisons.
		if !gmath.InBounds(nx, nz, 0, 100) {
			continue
		}

		if world.IsTileBlocked(pos.MapID, nx, nz) {
			continue
		}

		return nx, nz, true
	}
	return 0, 0, false
}

// distanceSq and distanceSqPos have been replaced by gmath.DistanceSq.
// Kept as comment for history: func distanceSq(a, b ecs.PositionComponent) float64

func logStateChange(id ecs.Entity, from, to ecs.AIState) {
	// loggate.Debugf encapsulates the IsDebug() guard — 0 allocs in production.
	name := fmt.Sprintf("entity_%d", id)
	if meta, ok := ecs.GlobalRegistry.GetMetadata(id); ok {
		name = meta.Name
	}
	loggate.Debugf("[AI] %s: %s → %s", name, from, to)
}
