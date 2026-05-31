// Package logger — ECS Entity Tracer
//
// EntityTracer maintains a thread-safe watch-list of ECS entity IDs.
// When an entity is registered in the watch-list, any system can call
// TraceEvent to emit a structured trace entry exclusively for that entity.
//
// Trace entries are forwarded to logger.Debug, which means they respect
// the global DebugMode flag (silent when debug=false in config.json).
//
// Example output:
//
//	[TRACE] Entity#42 | HP_CHANGE    | HP: 100 → 75 (hit by Slime for 25 damage)
//	[TRACE] Entity#42 | POSITION     | (15, 30) → (16, 30)
//	[TRACE] Entity#42 | ITEM_PICKUP  | Iron Sword (template 1001)
package logger

import (
	"fmt"
	"sync"
)

// ─── Entity Tracer ───────────────────────────────────────────────────────────

// EntityTracer holds the watch-list of entity IDs to trace.
type EntityTracer struct {
	watched sync.Map // map[uint64]bool
}

// GlobalEntityTracer is the singleton tracer used by all systems.
var GlobalEntityTracer = &EntityTracer{}

// Watch registers an entity ID for detailed event tracing.
// Subsequent calls to TraceEvent for this ID will produce log output.
func (t *EntityTracer) Watch(entityID uint64) {
	t.watched.Store(entityID, true)
	Debug("[TRACER] Now watching Entity#%d", entityID)
}

// Unwatch removes an entity ID from the watch-list.
func (t *EntityTracer) Unwatch(entityID uint64) {
	t.watched.Delete(entityID)
	Debug("[TRACER] Stopped watching Entity#%d", entityID)
}

// IsWatched reports whether an entity ID is currently being traced.
func (t *EntityTracer) IsWatched(entityID uint64) bool {
	_, ok := t.watched.Load(entityID)
	return ok
}

// TraceEvent emits a structured trace line for the given entity.
// It is a no-op when:
//   - The entity is not in the watch-list.
//   - DebugMode is disabled (debug=false in config.json).
//
// Parameters:
//   - entityID: The ECS entity ID to trace.
//   - event:    A short event tag, e.g. "HP_CHANGE", "POSITION", "ITEM_PICKUP".
//   - format:   Printf-style format string for event detail.
//   - args:     Format arguments.
func (t *EntityTracer) TraceEvent(entityID uint64, event, format string, args ...any) {
	if !t.IsWatched(entityID) {
		return
	}
	detail := fmt.Sprintf(format, args...)
	Debug("[TRACE] Entity#%d | %-14s | %s", entityID, event, detail)
}

// WatchCount returns the number of entities currently being traced.
// Useful for admin status queries.
func (t *EntityTracer) WatchCount() int {
	count := 0
	t.watched.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
