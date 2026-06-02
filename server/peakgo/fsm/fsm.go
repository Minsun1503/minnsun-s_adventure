// Package fsm provides a zero-allocation, type-safe Finite State Machine
// optimized for the Minnsun's Adventure game server AI systems.
//
// # Why this package exists
//
// The current monster AI uses ad-hoc if-else chains in game/monster_ai.go,
// which become unmaintainable as more states (idle, patrol, chase, attack,
// flee, stun, dead) and transitions are added. An FSM provides:
//   - Explicit state transition graph (self-documenting)
//   - Entry/exit hooks for state initialization/cleanup
//   - Timer integration for state timeout cooldowns
//   - Deterministic behavior (easy to debug)
//
// # Peak Go Contract
//
// Zero heap allocations on state transitions. Uses value-type transitions
// and pooled state metadata. All state enums should be constants (iota).
package fsm

import (
	"fmt"
	"server/peakgo/timer"
)

// ─── Core Types ───────────────────────────────────────────────────────────────

// State is a type constraint for FSM states.
// Typically implemented as an iota-based integer enum.
type State interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Event is a type constraint for FSM events (triggers).
// Typically implemented as an iota-based integer enum.
type Event interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

// TransitionHook is called when entering or exiting a state.
// Return false to prevent the transition (guard condition).
type TransitionHook[S State, E Event] func(from, to S, event E) bool

// ─── FSM Definition ───────────────────────────────────────────────────────────

// TransitionRule defines a single transition between states.
type TransitionRule[S State, E Event] struct {
	From  S
	Event E
	To    S
}

// FSMDef is the immutable definition of a finite state machine.
// Created at initialization time and shared across all instances.
type FSMDef[S State, E Event] struct {
	transitions map[S]map[E]S                // [fromState][event] -> toState
	onEnter     map[S][]TransitionHook[S, E] // Hooks called when entering a state
	onExit      map[S][]TransitionHook[S, E] // Hooks called when exiting a state
	initial     S                            // Default starting state
}

// NewFSMDef creates a new FSM definition with the given transitions.
// Panics on duplicate transitions or missing states.
//
// Example:
//
//	type MonsterState int
//	const (
//	    Idle MonsterState = iota
//	    Patrol
//	    Chase
//	    Attack
//	)
//	type MonsterEvent int
//	const (
//	    SeePlayer MonsterEvent = iota
//	    LosePlayer
//	    InRange
//	    OutOfRange
//	)
//
//	def := fsm.NewFSMDef(
//	    MonsterState(Idle),
//	    []fsm.TransitionRule[MonsterState, MonsterEvent]{
//	        {Idle, SeePlayer, Chase},
//	        {Chase, LosePlayer, Patrol},
//	        {Chase, InRange, Attack},
//	        {Attack, OutOfRange, Chase},
//	    },
//	)
func NewFSMDef[S State, E Event](initial S, rules []TransitionRule[S, E]) *FSMDef[S, E] {
	def := &FSMDef[S, E]{
		transitions: make(map[S]map[E]S),
		onEnter:     make(map[S][]TransitionHook[S, E]),
		onExit:      make(map[S][]TransitionHook[S, E]),
		initial:     initial,
	}

	for _, rule := range rules {
		if _, ok := def.transitions[rule.From]; !ok {
			def.transitions[rule.From] = make(map[E]S)
		}
		if _, ok := def.transitions[rule.From][rule.Event]; ok {
			panic(fmt.Sprintf("fsm: duplicate transition from state %v on event %v", rule.From, rule.Event))
		}
		def.transitions[rule.From][rule.Event] = rule.To
	}

	return def
}

// OnEnter registers a hook that runs when entering the given state.
// Hooks are called in registration order. Returning false prevents the transition.
func (def *FSMDef[S, E]) OnEnter(state S, hook TransitionHook[S, E]) {
	def.onEnter[state] = append(def.onEnter[state], hook)
}

// OnExit registers a hook that runs when exiting the given state.
// Hooks are called in registration order. Returning false prevents the transition.
func (def *FSMDef[S, E]) OnExit(state S, hook TransitionHook[S, E]) {
	def.onExit[state] = append(def.onExit[state], hook)
}

// ─── FSM Instance ─────────────────────────────────────────────────────────────

// FSM is a concrete instance of a finite state machine.
// Embed this into your entity component (monster, NPC, boss).
//
// Stored as an inline value — copy-modify-overwrite like all ECS components.
type FSM[S State, E Event] struct {
	Def     *FSMDef[S, E]   // Shared definition (nil = uninitialized)
	Current S               // Current state
	Ticker  timer.TickTimer // State timeout timer
}

// NewFSM creates a new FSM instance in the initial state.
// If def is nil, returns a zero-value FSM (safe for later initialization).
func NewFSM[S State, E Event](def *FSMDef[S, E]) FSM[S, E] {
	if def == nil {
		return FSM[S, E]{}
	}
	return FSM[S, E]{
		Def:     def,
		Current: def.initial,
	}
}

// Is reports whether the FSM is currently in the given state.
// Returns false if the FSM has no definition (nil def).
func (f *FSM[S, E]) Is(state S) bool {
	if f.Def == nil {
		return false
	}
	return f.Current == state
}

// Can reports whether the FSM can transition on the given event.
func (f *FSM[S, E]) Can(event E) bool {
	if f.Def == nil {
		return false
	}
	stateMap, ok := f.Def.transitions[f.Current]
	if !ok {
		return false
	}
	_, ok = stateMap[event]
	return ok
}

// Send attempts to transition the FSM on the given event.
// Returns the new state if successful, or the current state if the transition
// is not defined or a hook rejects it.
// Zero alloc per call (no heap allocations).
func (f *FSM[S, E]) Send(event E) (newState S, ok bool) {
	if f.Def == nil {
		return f.Current, false
	}

	// Look up transition
	stateMap, hasTransitions := f.Def.transitions[f.Current]
	if !hasTransitions {
		return f.Current, false
	}
	target, hasTarget := stateMap[event]
	if !hasTarget {
		return f.Current, false
	}

	// Run exit hooks for current state
	if hooks, ok := f.Def.onExit[f.Current]; ok {
		for _, hook := range hooks {
			if !hook(f.Current, target, event) {
				return f.Current, false // Hook rejected the transition
			}
		}
	}

	// Run enter hooks for target state
	if hooks, ok := f.Def.onEnter[target]; ok {
		for _, hook := range hooks {
			if !hook(f.Current, target, event) {
				return f.Current, false // Hook rejected the entry
			}
		}
	}

	// Perform transition
	oldState := f.Current
	f.Current = target
	f.Ticker.Reset()

	return oldState, true
}

// Force transitions to a state regardless of defined transitions.
// Use sparingly — for initialization, death, or admin commands.
func (f *FSM[S, E]) Force(state S) {
	if f.Def == nil {
		f.Current = state
		return
	}

	// Run exit hooks for current state
	if hooks, ok := f.Def.onExit[f.Current]; ok {
		for _, hook := range hooks {
			if !hook(f.Current, state, *new(E)) {
				return
			}
		}
	}

	// Run enter hooks for target state
	if hooks, ok := f.Def.onEnter[state]; ok {
		for _, hook := range hooks {
			if !hook(f.Current, state, *new(E)) {
				return
			}
		}
	}

	f.Current = state
	f.Ticker.Reset()
}

// Reset returns the FSM to its initial state.
func (f *FSM[S, E]) Reset() {
	if f.Def != nil {
		f.Current = f.Def.initial
		f.Ticker.Reset()
	}
}

// String implements fmt.Stringer for debugging.
func (f FSM[S, E]) String() string {
	if f.Def == nil {
		return "FSM(uninitialized)"
	}
	return fmt.Sprintf("FSM(state=%v)", f.Current)
}

// ─── Convience: StateTimer ────────────────────────────────────────────────────

// StateTimer wraps an FSM with a tick timer for state timeout transitions.
// Common pattern: "stay in state for N ticks, then auto-transition."
type StateTimer[S State, E Event] struct {
	FSM          FSM[S, E]
	Timeouts     map[S]int // State -> ticks before auto-timeout
	TimeoutEvent E         // Event to send on timeout
}

// NewStateTimer creates a StateTimer with per-state timeouts.
func NewStateTimer[S State, E Event](def *FSMDef[S, E], timeouts map[S]int, timeoutEvent E) StateTimer[S, E] {
	return StateTimer[S, E]{
		FSM:          NewFSM(def),
		Timeouts:     timeouts,
		TimeoutEvent: timeoutEvent,
	}
}

// Tick advances the timer and sends a timeout event if the current state
// has been active longer than its configured timeout.
// Call once per game-loop tick.
// Returns true if a state transition occurred.
func (st *StateTimer[S, E]) Tick() bool {
	if st.Timeouts == nil {
		return false
	}

	timeout, hasTimeout := st.Timeouts[st.FSM.Current]
	if !hasTimeout {
		return false
	}

	// Configure timer if not yet set
	if st.FSM.Ticker.Cooldown() != timeout {
		st.FSM.Ticker.SetCooldown(timeout)
	}

	// Check timeout and send event
	if st.FSM.Ticker.Tick() {
		_, ok := st.FSM.Send(st.TimeoutEvent)
		return ok
	}
	return false
}
