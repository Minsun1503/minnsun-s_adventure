package gmath_test

import (
	"math"
	"server/peakgo/gmath"
	"testing"
)

// ─── DistanceSq correctness ───────────────────────────────────────────────────

func TestDistanceSqSamePoint(t *testing.T) {
	if got := gmath.DistanceSq(5, 5, 5, 5); got != 0 {
		t.Fatalf("same point: expected 0, got %f", got)
	}
}

func TestDistanceSqKnownValues(t *testing.T) {
	// (0,0) to (3,4) → 3² + 4² = 25
	if got := gmath.DistanceSq(0, 0, 3, 4); got != 25 {
		t.Fatalf("3-4-5 triangle: expected 25, got %f", got)
	}
}

func TestDistanceSqSymmetric(t *testing.T) {
	a := gmath.DistanceSq(1, 2, 7, 9)
	b := gmath.DistanceSq(7, 9, 1, 2)
	if a != b {
		t.Fatalf("DistanceSq not symmetric: %f vs %f", a, b)
	}
}

// ─── InRange correctness ──────────────────────────────────────────────────────

func TestInRange(t *testing.T) {
	// distance = sqrt(3² + 4²) = 5.0 exactly
	if !gmath.InRange(0, 0, 3, 4, 5.0) {
		t.Fatal("InRange(0,0,3,4,5.0) should be true (exactly on boundary)")
	}
	if gmath.InRange(0, 0, 3, 4, 4.9) {
		t.Fatal("InRange(0,0,3,4,4.9) should be false (just outside)")
	}
}

// ─── InBounds correctness ─────────────────────────────────────────────────────

func TestInBounds(t *testing.T) {
	if !gmath.InBounds(50, 50, 0, 100) {
		t.Fatal("(50,50) should be in [0,100]")
	}
	if !gmath.InBounds(0, 0, 0, 100) {
		t.Fatal("corners (0,0) should be in [0,100]")
	}
	if !gmath.InBounds(100, 100, 0, 100) {
		t.Fatal("corners (100,100) should be in [0,100]")
	}
	if gmath.InBounds(-1, 50, 0, 100) {
		t.Fatal("(-1,50) should be out of [0,100]")
	}
	if gmath.InBounds(50, 101, 0, 100) {
		t.Fatal("(50,101) should be out of [0,100]")
	}
}

// ─── Clamp correctness ────────────────────────────────────────────────────────

func TestClamp(t *testing.T) {
	if gmath.Clamp(-5, 0, 100) != 0 {
		t.Fatal("Clamp(-5, 0, 100) should be 0")
	}
	if gmath.Clamp(150, 0, 100) != 100 {
		t.Fatal("Clamp(150, 0, 100) should be 100")
	}
	if gmath.Clamp(50, 0, 100) != 50 {
		t.Fatal("Clamp(50, 0, 100) should be 50 (unchanged)")
	}
}

func TestClampPos(t *testing.T) {
	x, z := gmath.ClampPos(-3, 105, 0, 100)
	if x != 0 || z != 100 {
		t.Fatalf("ClampPos(-3, 105, 0, 100): got (%d, %d), want (0, 100)", x, z)
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkDistanceSq(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gmath.DistanceSq(10, 20, 73, 85)
	}
}

func BenchmarkInRange(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gmath.InRange(10, 20, 73, 85, 5.0)
	}
}

func BenchmarkInBounds(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = gmath.InBounds(50, 77, 0, 100)
	}
}

func BenchmarkClampPos(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gmath.ClampPos(-3, 105, 0, 100)
	}
}

// BenchmarkVsStdlib verifies gmath.DistanceSq is comparable to the naive
// math.Sqrt approach but without the sqrt overhead.
func BenchmarkDistanceSqVsSqrt(b *testing.B) {
	b.Run("DistanceSq_no_sqrt", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = gmath.DistanceSq(10, 20, 73, 85) <= 5.0*5.0
		}
	})
	b.Run("math_Sqrt_baseline", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			dx := float64(10 - 73)
			dz := float64(20 - 85)
			_ = math.Sqrt(dx*dx+dz*dz) <= 5.0
		}
	})
}
