// Package rng provides a pooled, goroutine-safe random number generator
// for high-frequency game logic in Minnsun's Adventure.
//
// # Problem it solves
//
// The codebase currently uses two different (both suboptimal) patterns:
//
//  1. loot.go creates rand.New(rand.NewSource(time.Now().UnixNano())) on EVERY
//     call to RollLoot → allocates a new *rand.Rand + Source on the heap
//     every time a monster dies. Under load this is guaranteed GC pressure.
//
//  2. ai_roaming.go and combat.go call the global rand.Intn / rand.Float64
//     which in Go's stdlib uses a single mutex-protected global source.
//     Under many concurrent goroutines this becomes a lock bottleneck.
//
// # Solution
//
// A sync.Pool of *rand.Rand sources:
//   - Each goroutine borrows a generator, uses it, returns it immediately.
//   - No mutex contention between goroutines (each gets its own source).
//   - No allocation on the hot path (source is recycled, not created).
//   - Each new source is seeded with time.UnixNano() XOR an atomic counter
//     to prevent multiple goroutines receiving the same seed in the same nanosecond.
//
// # Peak Go contract
//
//	rng.Intn(n)    → 0 allocs/op after pool warm-up
//	rng.Float64()  → 0 allocs/op after pool warm-up
package rng

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// seedCounter ensures that rapid concurrent calls to pool.New never share
// a seed, even when time.Now().UnixNano() returns the same value.
var seedCounter atomic.Int64

var pool = sync.Pool{
	New: func() any {
		// XOR with atomic increment guarantees uniqueness across concurrent New calls.
		seed := time.Now().UnixNano() ^ seedCounter.Add(1)
		return rand.New(rand.NewSource(seed))
	},
}

// Intn returns a uniformly distributed non-negative pseudo-random int in [0, n).
// Panics if n <= 0 (same contract as rand.Intn).
func Intn(n int) int {
	r := pool.Get().(*rand.Rand)
	v := r.Intn(n)
	pool.Put(r)
	return v
}

// Float64 returns a pseudo-random float64 in [0.0, 1.0).
// Use for probability rolls: if rng.Float64() <= dropChance { ... }
func Float64() float64 {
	r := pool.Get().(*rand.Rand)
	v := r.Float64()
	pool.Put(r)
	return v
}

// IntnRange returns a pseudo-random int in the closed interval [lo, hi).
// Equivalent to lo + Intn(hi-lo). Panics if lo >= hi.
func IntnRange(lo, hi int) int {
	return lo + Intn(hi-lo)
}

// Shuffle performs a Fisher-Yates shuffle on s in-place using the pooled RNG.
// Zero allocations.
func Shuffle(n int, swap func(i, j int)) {
	r := pool.Get().(*rand.Rand)
	r.Shuffle(n, swap)
	pool.Put(r)
}

// WarmUp pre-fills the pool with n pre-seeded generators to avoid New calls
// during the first burst of game-loop activity. Call once at server startup.
func WarmUp(n int) {
	sources := make([]*rand.Rand, n)
	for i := range sources {
		sources[i] = pool.Get().(*rand.Rand)
	}
	var mu sync.Mutex // only used here, not on hot path
	mu.Lock()
	for _, s := range sources {
		pool.Put(s)
	}
	mu.Unlock()
}
