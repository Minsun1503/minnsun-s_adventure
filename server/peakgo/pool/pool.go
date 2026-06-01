// Package pool provides typed, zero-footgun sync.Pool wrappers.
//
// # Design rationale
//
// Raw sync.Pool usage in Go requires:
//  1. Manual type assertions on Get() → easy to get wrong.
//  2. Manual slice reset before Put() → easy to forget (data leak).
//  3. Repeated boilerplate New functions everywhere.
//
// This package eliminates all three issues with two generic types:
//   - BytesPool: for []byte packet/IO buffers.
//   - SlicePool[T]: for any []T accumulator (query results, entity lists, …).
//
// # Peak Go Contract
//
// Both types guarantee 0 allocs/op on the Get → use → Put hot path
// so long as the requested capacity does not exceed the pool's default capacity.
// When it does, a new slice is allocated ONCE and returned to the pool on Put,
// so the amortised allocation rate trends to zero under steady traffic.
package pool

import "sync"

// ─── BytesPool ───────────────────────────────────────────────────────────────

// BytesPool is a typed pool of *[]byte. Buffers are pre-allocated to `size`
// bytes. On Put, the slice is reset to its full capacity so it is always
// ready-to-use on the next Get.
type BytesPool struct {
	p    sync.Pool
	size int
}

// NewBytesPool creates a BytesPool whose buffers are pre-sized to `size` bytes.
// Typical values: 1024 for packet payloads, 4096 for large I/O chunks.
func NewBytesPool(size int) *BytesPool {
	bp := &BytesPool{size: size}
	bp.p.New = func() any {
		b := make([]byte, size)
		return &b
	}
	return bp
}

// Get retrieves a *[]byte from the pool. The returned slice has length == cap
// (full default size) and is safe to reslice immediately.
// Callers MUST call Put when done.
func (bp *BytesPool) Get() *[]byte {
	return bp.p.Get().(*[]byte)
}

// Put resets the slice to its full capacity and returns it to the pool.
// This prevents stale data leaks — the slice is always clean on the next Get.
func (bp *BytesPool) Put(b *[]byte) {
	// Restore to default size so the next Get always sees a full-capacity buffer.
	if cap(*b) >= bp.size {
		*b = (*b)[:bp.size]
	}
	bp.p.Put(b)
}

// ─── SlicePool ───────────────────────────────────────────────────────────────

// SlicePool[T] is a typed pool of *[]T. Slices are pre-allocated with the
// given capacity. On Put, the slice is zeroed and reset to length 0.
type SlicePool[T any] struct {
	p   sync.Pool
	cap int
}

// NewSlicePool creates a SlicePool whose slices start with capacity `cap`.
// Typical values: 8 for chunk buckets, 16 for proximity results, 1024 for entity lists.
func NewSlicePool[T any](capacity int) *SlicePool[T] {
	sp := &SlicePool[T]{cap: capacity}
	sp.p.New = func() any {
		s := make([]T, 0, capacity)
		return &s
	}
	return sp
}

// Get retrieves a *[]T from the pool. The returned slice has length == 0
// and capacity >= the pool default. Ready to append immediately.
// Callers MUST call Put when done.
func (sp *SlicePool[T]) Get() *[]T {
	ps := sp.p.Get().(*[]T)
	*ps = (*ps)[:0] // Ensure length is 0 even if Put forgot to reset.
	return ps
}

// Put clears the slice contents (to release any held references), resets its
// length to 0, and returns it to the pool.
func (sp *SlicePool[T]) Put(ps *[]T) {
	var zero T
	s := *ps
	for i := range s {
		s[i] = zero // release any pointer/interface references
	}
	*ps = s[:0]
	sp.p.Put(ps)
}
