// Package pool provides typed, zero-footgun, high-performance sync.Pool wrappers
// tailored for the Minnsun's Adventure game server infrastructure.
//
// # Why this package exists
//
// Raw sync.Pool usage in standard Go has three major maintainability issues:
//  1. Requires manual interface type assertions on every Get() invocation.
//  2. Requires rigid clean-up/reset logic before Put(), which is prone to human error.
//  3. Leads to repeated boilerplate New allocation functions spread across subsystems.
//
// This package components encapsulate type safety and memory management inside
// two highly optimized generic constructs:
//   - BytesPool: Custom tailored for []byte packet network and serialization buffers.
//   - SlicePool[T]: For recycling dynamic arrays like ECS entities, chunk entries, or proximity queries.
//
// # Peak Go Contract & Memory Anti-Pollution
//
// Both pool primitives guarantee exactly 0 allocs/op on the hot-path lifecycle.
// To prevent permanent RAM bloating (Pool Pollution) after heavy traffic spikes
// or oversized packet handling, these pools automatically discard any buffers that
// expanded beyond 4x their baseline capacity, letting the Go Garbage Collector (GC)
// cleanly reclaim the transient spikes.
package pool

import "sync"

// ─── BytesPool ───────────────────────────────────────────────────────────────

// BytesPool represents a strongly-typed memory pool managing pointers to byte slices (*[]byte).
// This architectural choice prevents copying slice headers during pool exchanges.
type BytesPool struct {
	p    sync.Pool
	size int
}

// NewBytesPool instantiates a BytesPool where all items are pre-allocated to exactly `size` bytes.
// Standard configurations: 1024 for game packets, 4096 for massive system file I/O operations.
func NewBytesPool(size int) *BytesPool {
	bp := &BytesPool{size: size}
	bp.p.New = func() any {
		b := make([]byte, size)
		return &b
	}
	return bp
}

// Get fetches a *[]byte container from the internal pool.
//
// Optimized Self-Normalization: This method automatically normalizes the slice length
// back to the pool default size. It guarantees that the caller always receives a standard
// ready-to-use capacity block, protecting against corrupt implementations from faulty Put calls.
func (bp *BytesPool) Get() *[]byte {
	pb := bp.p.Get().(*[]byte)
	*pb = (*pb)[:bp.size] // Enforces local reset consistency
	return pb
}

// Put restores the buffer slice length back to the pool default size and recycles it.
//
// Memory Anti-Pollution Guard: If an individual buffer's capacity expanded beyond 4 times
// its initial designated size, it is permanently banned from re-entering the pool. This prevents
// giant transient arrays from nesting in RAM indefinitely.
func (bp *BytesPool) Put(b *[]byte) {
	// Defensive check: Drop oversized spike buffers to allow proper heap reclamation
	if cap(*b) > bp.size*4 {
		return
	}

	*b = (*b)[:bp.size] // Restores the buffer to a reusable length state
	bp.p.Put(b)
}

// ─── SlicePool ───────────────────────────────────────────────────────────────

// SlicePool[T] handles a typed, generic collection pool managing pointers to arrays (*[]T).
// Highly versatile for recycling intensive runtime vectors like ecs.Entity or world.ChunkEntry.
type SlicePool[T any] struct {
	p   sync.Pool
	cap int
}

// NewSlicePool creates a typed SlicePool whose arrays initialized with a baseline target capacity.
// Standard configurations: 8 for chunk buckets, 16 for proximity filters, 1024 for dense entity lists.
func NewSlicePool[T any](capacity int) *SlicePool[T] {
	sp := &SlicePool[T]{cap: capacity}
	sp.p.New = func() any {
		s := make([]T, 0, capacity)
		return &s
	}
	return sp
}

// Get retrieves a *[]T from the pool.
//
// The returned slice pointer is automatically truncated to length == 0 while preserving
// its underlying capacity matrix, making it immediately available for fast append operations.
func (sp *SlicePool[T]) Get() *[]T {
	ps := sp.p.Get().(*[]T)
	*ps = (*ps)[:0] // Self-normalization to ensure pristine append-ready state
	return ps
}

// Put safely clears active internal reference values, resets lengths, and returns the slice container.
//
// GC Leak Protection: This method completely zeros out the active backing array elements.
// If [T] contains pointer references or composite interfaces, failure to clear them means the
// runtime GC will still see live root links, causing silent severe memory leaks despite length resets.
func (sp *SlicePool[T]) Put(ps *[]T) {
	// Memory Anti-Pollution Guard: Do not re-pool dynamic arrays that over-expanded during a spike.
	if cap(*ps) > sp.cap*4 {
		return
	}

	var zero T
	s := *ps
	// Strict zeroing loop: Erases any underlying live links to prevent phantom GC reference leaks
	for i := range s {
		s[i] = zero
	}

	*ps = s[:0] // Reset length while retaining the backing array bounds
	sp.p.Put(ps)
}
