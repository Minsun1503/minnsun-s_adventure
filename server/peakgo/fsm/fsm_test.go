package fsm

import (
	"testing"
)

// ─── Test Types ───────────────────────────────────────────────────────────────

type MonsterState int

const (
	Idle MonsterState = iota
	Patrol
	Chase
	Attack
	Flee
	Dead
)

type MonsterEvent int

const (
	SeePlayer MonsterEvent = iota
	LosePlayer
	InRange
	OutOfRange
	Hurt
	Healed
	Killed
	Respawn
)

// ─── Test Setup ───────────────────────────────────────────────────────────────

func newMonsterFSMDef() *FSMDef[MonsterState, MonsterEvent] {
	return NewFSMDef(Idle, []TransitionRule[MonsterState, MonsterEvent]{
		{Idle, SeePlayer, Chase},
		{Chase, LosePlayer, Patrol},
		{Chase, InRange, Attack},
		{Attack, OutOfRange, Chase},
		{Attack, Hurt, Flee},
		{Flee, Healed, Chase},
		{Flee, LosePlayer, Patrol},
		{Patrol, SeePlayer, Chase},
		{Idle, Killed, Dead},
		{Chase, Killed, Dead},
		{Attack, Killed, Dead},
		{Dead, Respawn, Idle},
	})
}

// ─── Basic Tests ──────────────────────────────────────────────────────────────

func TestFSMInitialState(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	if fsm.Current != Idle {
		t.Fatalf("expected initial state Idle, got %v", fsm.Current)
	}
}

func TestFSMIs(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	if !fsm.Is(Idle) {
		t.Fatal("expected Is(Idle) to be true")
	}
	if fsm.Is(Chase) {
		t.Fatal("expected Is(Chase) to be false initially")
	}
}

func TestFSMCan(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	if !fsm.Can(SeePlayer) {
		t.Fatal("expected Can(SeePlayer) from Idle")
	}
	if fsm.Can(LosePlayer) {
		t.Fatal("expected not Can(LosePlayer) from Idle")
	}
	if !fsm.Can(Killed) {
		t.Fatal("expected Can(Killed) from Idle")
	}
}

func TestFSMSendValid(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	prev, ok := fsm.Send(SeePlayer)
	if !ok {
		t.Fatal("expected Send(SeePlayer) to be accepted")
	}
	if prev != Idle {
		t.Fatalf("expected previous state Idle, got %v", prev)
	}
	if fsm.Current != Chase {
		t.Fatalf("expected current state Chase, got %v", fsm.Current)
	}
}

func TestFSMSendInvalid(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	_, ok := fsm.Send(LosePlayer) // Not valid from Idle
	if ok {
		t.Fatal("expected Send(LosePlayer) from Idle to be rejected")
	}
	if fsm.Current != Idle {
		t.Fatalf("expected current state still Idle, got %v", fsm.Current)
	}
}

func TestFSMTransitionChain(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	// Follow a full combat cycle
	transitions := []struct {
		event MonsterEvent
		want  MonsterState
	}{
		{SeePlayer, Chase},
		{InRange, Attack},
		{Hurt, Flee},
		{Healed, Chase},
		{LosePlayer, Patrol},
		{SeePlayer, Chase},
		{Killed, Dead},
		{Respawn, Idle},
	}

	for _, tt := range transitions {
		_, ok := fsm.Send(tt.event)
		if !ok {
			t.Fatalf("expected Send(%v) to be accepted, current state %v", tt.event, fsm.Current)
		}
		if fsm.Current != tt.want {
			t.Fatalf("expected state %v after %v, got %v", tt.want, tt.event, fsm.Current)
		}
	}
}

// ─── Hook Tests ───────────────────────────────────────────────────────────────

func TestFSMOnEnterHook(t *testing.T) {
	def := newMonsterFSMDef()
	enteredAttack := false

	def.OnEnter(Attack, func(from, to MonsterState, event MonsterEvent) bool {
		enteredAttack = true
		return true
	})

	fsm := NewFSM(def)
	fsm.Send(SeePlayer) // Idle -> Chase
	fsm.Send(InRange)   // Chase -> Attack

	if !enteredAttack {
		t.Fatal("expected OnEnter hook for Attack to be called")
	}
}

func TestFSMOnExitHook(t *testing.T) {
	def := newMonsterFSMDef()
	exitedChase := false

	def.OnExit(Chase, func(from, to MonsterState, event MonsterEvent) bool {
		exitedChase = true
		return true
	})

	fsm := NewFSM(def)
	fsm.Send(SeePlayer) // Idle -> Chase
	fsm.Send(InRange)   // Chase -> Attack

	if !exitedChase {
		t.Fatal("expected OnExit hook for Chase to be called")
	}
}

func TestFSMGuardRejectsTransition(t *testing.T) {
	def := newMonsterFSMDef()
	def.OnEnter(Chase, func(from, to MonsterState, event MonsterEvent) bool {
		return false // Always reject entering Chase
	})

	fsm := NewFSM(def)
	_, ok := fsm.Send(SeePlayer) // Idle -> Chase (rejected)
	if ok {
		t.Fatal("expected transition to be rejected by guard")
	}
	if fsm.Current != Idle {
		t.Fatalf("expected current state still Idle after rejected transition, got %v", fsm.Current)
	}
}

// ─── Force & Reset Tests ──────────────────────────────────────────────────────

func TestFSMForce(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	fsm.Force(Attack)
	if fsm.Current != Attack {
		t.Fatalf("expected Force to Attack, got %v", fsm.Current)
	}
}

func TestFSMReset(t *testing.T) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)

	fsm.Send(SeePlayer)
	fsm.Send(InRange)
	if fsm.Current != Attack {
		t.Fatalf("expected Attack state, got %v", fsm.Current)
	}

	fsm.Reset()
	if fsm.Current != Idle {
		t.Fatalf("expected Reset to Idle, got %v", fsm.Current)
	}
}

// ─── StateTimer Tests ─────────────────────────────────────────────────────────

func TestStateTimerTick(t *testing.T) {
	def := newMonsterFSMDef()
	timeouts := map[MonsterState]int{
		Idle: 10, // 10 ticks idle -> timeout
	}

	st := NewStateTimer(def, timeouts, SeePlayer)

	// Should timeout after 10 ticks
	transitioned := false
	for range 15 {
		if st.Tick() {
			transitioned = true
			break
		}
	}

	if !transitioned {
		t.Fatal("expected StateTimer to transition on timeout")
	}
}

func TestStateTimerNoTimeout(t *testing.T) {
	def := newMonsterFSMDef()
	timeouts := map[MonsterState]int{}

	st := NewStateTimer(def, timeouts, SeePlayer)

	for range 10 {
		if st.Tick() {
			t.Fatal("expected no transition (no timeouts configured)")
		}
	}
}

// ─── Panic Tests ──────────────────────────────────────────────────────────────

func TestFSMDefDuplicateTransitionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate transition")
		}
	}()

	_ = NewFSMDef[MonsterState, MonsterEvent](Idle, []TransitionRule[MonsterState, MonsterEvent]{
		{Idle, SeePlayer, Chase},
		{Idle, SeePlayer, Attack}, // Duplicate!
	})
}

// ─── Nil Def Tests ────────────────────────────────────────────────────────────

func TestFSMNilDef(t *testing.T) {
	var fsm FSM[MonsterState, MonsterEvent]
	if fsm.Is(Idle) {
		t.Fatal("expected Is to return false with nil def")
	}
	_, ok := fsm.Send(SeePlayer)
	if ok {
		t.Fatal("expected Send to fail with nil def")
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkFSMSend(b *testing.B) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)
	b.ResetTimer()
	for range b.N {
		fsm.Send(SeePlayer)
		fsm.Reset()
	}
}

func BenchmarkFSMCan(b *testing.B) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)
	b.ResetTimer()
	for range b.N {
		fsm.Can(SeePlayer)
	}
}

func BenchmarkFSMIs(b *testing.B) {
	def := newMonsterFSMDef()
	fsm := NewFSM(def)
	b.ResetTimer()
	for range b.N {
		fsm.Is(Idle)
	}
}

func BenchmarkStateTimerTick(b *testing.B) {
	def := newMonsterFSMDef()
	timeouts := map[MonsterState]int{Idle: 5}
	st := NewStateTimer(def, timeouts, SeePlayer)
	b.ResetTimer()
	for range b.N {
		st.Tick()
	}
}
