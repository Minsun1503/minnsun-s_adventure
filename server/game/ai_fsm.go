package game

import (
	"fmt"
	"sync"

	"server/ecs"
	"server/peakgo/fsm"
	"server/peakgo/loggate"
)

// ──────────────────────────────────────────────
// Monster FSM — Event enum
// ──────────────────────────────────────────────

type MonsterEvent uint8

const (
	MonsterEvTick         MonsterEvent = iota // Tick timer expired
	MonsterEvAggro                            // Player detected in aggro range
	MonsterEvLostTarget                       // Target lost (out of leash / dead)
	MonsterEvInRange                          // Target in melee range
	MonsterEvOutOfRange                       // Target out of melee range
	MonsterEvPathDone                         // Roam/return destination reached
	MonsterEvAtSpawn                          // Returned to spawn point
	MonsterEvThreatSwitch                     // Higher-threat player exists
)

// String implements fmt.Stringer.
func (e MonsterEvent) String() string {
	switch e {
	case MonsterEvTick:
		return "Tick"
	case MonsterEvAggro:
		return "Aggro"
	case MonsterEvLostTarget:
		return "LostTarget"
	case MonsterEvInRange:
		return "InRange"
	case MonsterEvOutOfRange:
		return "OutOfRange"
	case MonsterEvPathDone:
		return "PathDone"
	case MonsterEvAtSpawn:
		return "AtSpawn"
	case MonsterEvThreatSwitch:
		return "ThreatSwitch"
	default:
		return fmt.Sprintf("Event(%d)", e)
	}
}

// ──────────────────────────────────────────────
// FSM instance pool (per-entity)
// ──────────────────────────────────────────────

var (
	monsterFSMDef   = buildMonsterFSMDef()
	monsterFSMStore = make(map[ecs.Entity]*fsm.FSM[ecs.AIState, MonsterEvent])
	monsterFSMMu    sync.RWMutex
)

// GetOrCreateMonsterFSM returns the FSM for the given entity, creating one if missing.
func GetOrCreateMonsterFSM(id ecs.Entity) *fsm.FSM[ecs.AIState, MonsterEvent] {
	monsterFSMMu.RLock()
	inst, ok := monsterFSMStore[id]
	monsterFSMMu.RUnlock()
	if ok {
		return inst
	}

	monsterFSMMu.Lock()
	defer monsterFSMMu.Unlock()

	// Double-check after acquiring write lock
	if inst, ok := monsterFSMStore[id]; ok {
		return inst
	}

	newFSM := fsm.NewFSM(monsterFSMDef)
	monsterFSMStore[id] = &newFSM
	return monsterFSMStore[id]
}

// RemoveMonsterFSM cleans up FSM for a removed entity.
func RemoveMonsterFSM(id ecs.Entity) {
	monsterFSMMu.Lock()
	delete(monsterFSMStore, id)
	monsterFSMMu.Unlock()
}

// MonsterFSMSend attempts to send an event to the entity's FSM.
// Returns true if the transition was valid and executed.
func MonsterFSMSend(id ecs.Entity, ai *ecs.AIComponent, ev MonsterEvent) bool {
	inst := GetOrCreateMonsterFSM(id)

	oldState := inst.Current
	newState, ok := inst.Send(ev)
	if !ok {
		return false
	}

	// Sync the AI component state with the FSM state
	ai.State = newState

	// Log state changes
	if newState != oldState {
		name := fmt.Sprintf("entity_%d", id)
		if meta, ok := ecs.DefaultRegistry.GetMetadata(id); ok {
			name = meta.Name
		}
		loggate.Debugf("[AI·FSM] %s: %s --[%s]--> %s", name, oldState, ev, newState)
	}

	return true
}

// ──────────────────────────────────────────────
// FSM definition builder — singleton
// ──────────────────────────────────────────────

func buildMonsterFSMDef() *fsm.FSMDef[ecs.AIState, MonsterEvent] {
	def := fsm.NewFSMDef(ecs.AIStateIdle, []fsm.TransitionRule[ecs.AIState, MonsterEvent]{
		// Idle → Roaming: tick expired, move to roam point
		{From: ecs.AIStateIdle, Event: MonsterEvTick, To: ecs.AIStateRoaming},
		// Idle → Chasing: aggro detected
		{From: ecs.AIStateIdle, Event: MonsterEvAggro, To: ecs.AIStateChasing},

		// Roaming → Chasing: player spotted
		{From: ecs.AIStateRoaming, Event: MonsterEvAggro, To: ecs.AIStateChasing},
		// Roaming → Idle: arrived at roam target or stuck
		{From: ecs.AIStateRoaming, Event: MonsterEvPathDone, To: ecs.AIStateIdle},

		// Chasing → Attacking: target in melee range
		{From: ecs.AIStateChasing, Event: MonsterEvInRange, To: ecs.AIStateAttacking},
		// Chasing → Returning: target lost or beyond leash
		{From: ecs.AIStateChasing, Event: MonsterEvLostTarget, To: ecs.AIStateReturning},
		// Chasing → Chasing (threat switch): handled by force-setting target
		{From: ecs.AIStateChasing, Event: MonsterEvThreatSwitch, To: ecs.AIStateChasing},

		// Attacking → Chasing: target out of melee range
		{From: ecs.AIStateAttacking, Event: MonsterEvOutOfRange, To: ecs.AIStateChasing},
		// Attacking → Returning: target died
		{From: ecs.AIStateAttacking, Event: MonsterEvLostTarget, To: ecs.AIStateReturning},

		// Returning → Idle: arrived at spawn
		{From: ecs.AIStateReturning, Event: MonsterEvAtSpawn, To: ecs.AIStateIdle},
		// Returning → Chasing: aggro (player follows monster back)
		{From: ecs.AIStateReturning, Event: MonsterEvAggro, To: ecs.AIStateChasing},
	})

	return def
}
