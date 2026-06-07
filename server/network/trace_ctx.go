// Package network — Trace Context Helpers
//
// trace_ctx.go provides traceID generation and propagation utilities for
// distributed tracing across the network layer, handler layer, and DB layer.
//
// # Design Decision: Explicit Parameter vs Thread-Local
//
// We use an explicit traceID string parameter passed through handler signatures
// rather than a goroutine-local storage (sync.Map[goroutineID → traceID]).
// This is:
//   - Zero magic — no implicit state, easy to reason about
//   - Zero alloc — no sync.Map lookup per call
//   - Testable — no hidden global state
//   - Compatible with all goroutine spawning patterns
//
// The packet binary format reserves the first 4 bytes as an optional trace_id
// hint from the client. If the client omits it, the server generates one.
package network

import (
	"fmt"

	"server/peakgo/rng"
)

// generateTraceID creates a short hex trace identifier.
// Format: 8 lowercase hex characters (e.g., "a1b2c3d4").
// Uses the pooled RNG from peakgo/rng for zero heap allocation.
func generateTraceID() string {
	r := rng.Borrow()
	tid := fmt.Sprintf("%08x", r.Uint32())
	rng.Return(r)
	return tid
}
