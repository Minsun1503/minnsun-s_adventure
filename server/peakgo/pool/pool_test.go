package pool_test

import (
	"server/peakgo/pool"
	"testing"
)

// ─── BytesPool benchmarks ─────────────────────────────────────────────────────

func BenchmarkBytesPoolGetPut(b *testing.B) {
	p := pool.NewBytesPool(1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := p.Get()
		*buf = (*buf)[:256] // simulate partial use
		p.Put(buf)
	}
}

func BenchmarkBytesPoolGetPutBaseline(b *testing.B) {
	// Baseline: allocate fresh slice each iteration (the bad old way).
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]byte, 256)
		_ = buf
	}
}

// ─── SlicePool benchmarks ─────────────────────────────────────────────────────

type testEntry struct{ A, B int }

func BenchmarkSlicePoolGetPut(b *testing.B) {
	p := pool.NewSlicePool[testEntry](16)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps := p.Get()
		*ps = append(*ps, testEntry{1, 2}, testEntry{3, 4})
		p.Put(ps)
	}
}

func BenchmarkSlicePoolGetPutBaseline(b *testing.B) {
	// Baseline: allocate fresh slice each iteration.
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := make([]testEntry, 0, 16)
		s = append(s, testEntry{1, 2}, testEntry{3, 4})
		_ = s
	}
}

// ─── Correctness tests ────────────────────────────────────────────────────────

func TestBytesPoolResetOnPut(t *testing.T) {
	p := pool.NewBytesPool(64)
	buf := p.Get()
	*buf = (*buf)[:10] // shorten it
	p.Put(buf)

	buf2 := p.Get()
	if len(*buf2) != 64 {
		t.Fatalf("expected len 64 after Put/Get cycle, got %d", len(*buf2))
	}
}

func TestSlicePoolLenZeroOnGet(t *testing.T) {
	p := pool.NewSlicePool[int](8)
	ps := p.Get()
	*ps = append(*ps, 1, 2, 3)
	p.Put(ps)

	ps2 := p.Get()
	if len(*ps2) != 0 {
		t.Fatalf("expected len 0 after Put/Get cycle, got %d", len(*ps2))
	}
}
