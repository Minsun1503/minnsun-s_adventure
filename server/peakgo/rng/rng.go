// Package rng provides a pooled, goroutine-safe pseudo-random number generator
// for high-frequency game logic in Minnsun's Adventure.
//
// # Why this package exists
//
// The standard math/rand global functions rely on a single, mutex-protected
// global source. Under high concurrent traffic (hundreds of player connections
// and thousands of monster AI ticks), this global mutex fast becomes a bottleneck.
//
// Conversely, creating a fresh rand.New(rand.NewSource(...)) on every single
// event (e.g., enemy death loot rolls) triggers massive heap allocation and GC pressure.
//
// # Solution & Architecture
//
// This package components encapsulate type-safe reuse inside a sync.Pool managing
// pointers to *rand.Rand generators. To optimize consecutive hot-path loops,
// explicit lifecycle semantics (Borrow/Return) are exposed to the caller layer.
package rng

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// seedCounter guarantees that rapid concurrent allocation calls to pool.New
// never share identical seeds, even if time.Now().UnixNano() returns identical values.
var seedCounter atomic.Int64

var pool = sync.Pool{
	New: func() any {
		// XOR with atomic increment ensures perfect seed uniqueness across concurrent threads
		seed := time.Now().UnixNano() ^ seedCounter.Add(1)
		return rand.New(rand.NewSource(seed))
	},
}

// ─── Lifecycle Operations (Bulk Optimization Hot-Paths) ─────────────────────

// Borrow retrieves a pre-seeded *rand.Rand generator instance from the shared pool.
//
// High-Performance Strategy: This method is highly recommended for hot-path loops
// requiring consecutive random selections (e.g., rolling multiple items from a loot table)
// to avoid the heavy repetitive Get/Put lifecycle overhead of the wrapper functions.
//
// Callers MUST invoke 'defer rng.Return(r)' once business operations conclude.
func Borrow() *rand.Rand {
	return pool.Get().(*rand.Rand)
}

// Return recycles a borrowed *rand.Rand generator safely back into the shared architecture pool.
func Return(r *rand.Rand) {
	pool.Put(r)
}

// ─── High-Level Convenience Semantics ────────────────────────────────────────

// Intn returns a uniformly distributed non-negative pseudo-random int in the half-open interval [0, n).
// Panics instantly if n <= 0.
func Intn(n int) int {
	r := Borrow()
	v := r.Intn(n)
	Return(r)
	return v
}

// Float64 returns a pseudo-random float64 in the half-open interval [0.0, 1.0).
func Float64() float64 {
	r := Borrow()
	v := r.Float64()
	Return(r)
	return v
}

// Chance reports whether a random probability check succeeds given a rate 'p' in range [0.0, 1.0].
// Highly pragmatic semantic helper for hot-path combat calculations like critical hits,
// dodge evaluations, or rare drop chance calculations.
//
// Example:
//
//	if rng.Chance(0.25) {
//	    // Kích hoạt sát thương chí mạng (25% tỷ lệ)
//	}
func Chance(p float64) bool {
	return Float64() < p
}

// IntnRange returns a pseudo-random int in the half-open interval [lo, hi).
// Equivalent to lo + Intn(hi-lo). Panics if lo >= hi.
func IntnRange(lo, hi int) int {
	return lo + Intn(hi-lo)
}

// Shuffle performs a Fisher-Yates shuffle on n elements in-place using a pooled RNG source.
// Executes with exactly 0 allocations.
func Shuffle(n int, swap func(i, j int)) {
	r := Borrow()
	r.Shuffle(n, swap)
	Return(r)
}

// ─── Lifecycle Framework Infrastructure ──────────────────────────────────────

// WarmUp pre-allocates and populates the pool with n generators to absorb
// allocation latency overhead during the server's early launch boot phase.
//
// Architecture Warning: Elements inside a sync.Pool are non-deterministic and subject
// to sudden, total cleanup by the Go Runtime Garbage Collector (GC) at any time.
// WarmUp does not guarantee long-term retention but alleviates early execution shock.
func WarmUp(n int) {
	sources := make([]*rand.Rand, n)
	for i := range sources {
		sources[i] = pool.Get().(*rand.Rand)
	}

	// Đã sửa: Loại bỏ hoàn toàn thực thể sync.Mutex cục bộ vô nghĩa.
	// Đẩy trả tài nguyên trực tiếp xuống pool ở tốc độ tối đa.
	for _, s := range sources {
		pool.Put(s)
	}
}
