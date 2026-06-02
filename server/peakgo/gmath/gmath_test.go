package gmath_test

import (
	"math"
	"server/peakgo/gmath"
	"testing"
)

// ─── BENCHMARK SINK VARIABLES ────────────────────────────────────────────────
//
// Sử dụng các biến Package-Level toàn cục làm đích đến (Sink) cho dữ liệu đầu ra.
// Điều này ngăn chặn hoàn toàn việc Go Compiler tự động tối ưu hóa và xóa bỏ
// khối lệnh chạy thử nghiệm (Dead-code elimination), giúp Benchmark phản ánh đúng thực tế.
var (
	sinkInt  int
	sinkBool bool
)

// ─── BASIC MATH UTILITIES TESTS ──────────────────────────────────────────────

func TestAbs(t *testing.T) {
	if got := gmath.Abs(-10); got != 10 {
		t.Fatalf("Abs(-10): expected 10, got %d", got)
	}
	if got := gmath.Abs(0); got != 0 {
		t.Fatalf("Abs(0): expected 0, got %d", got)
	}
	if got := gmath.Abs(5); got != 5 {
		t.Fatalf("Abs(5): expected 5, got %d", got)
	}
}

// ─── DISTANCES CORRECTNESS TESTS ─────────────────────────────────────────────

func TestDistanceSqSamePoint(t *testing.T) {
	// Đã sửa: Sử dụng định dạng chuỗi %d vì gmath.DistanceSq hiện tại trả về kiểu int
	if got := gmath.DistanceSq(5, 5, 5, 5); got != 0 {
		t.Fatalf("same point: expected 0, got %d", got)
	}
}

func TestDistanceSqKnownValues(t *testing.T) {
	// (0,0) tới (3,4) → 3² + 4² = 25
	if got := gmath.DistanceSq(0, 0, 3, 4); got != 25 {
		t.Fatalf("3-4-5 triangle: expected 25, got %d", got)
	}
}

func TestDistanceSqSymmetric(t *testing.T) {
	a := gmath.DistanceSq(1, 2, 7, 9)
	b := gmath.DistanceSq(7, 9, 1, 2)
	if a != b {
		t.Fatalf("DistanceSq not symmetric: %d vs %d", a, b)
	}
}

// TestDistanceSqNegativeCoordinates bổ sung bài kiểm thử với hệ tọa độ âm
// nhằm phòng ngừa các lỗi tràn số hoặc tính toán sai dấu khi làm AI/Pathfinding.
func TestDistanceSqNegativeCoordinates(t *testing.T) {
	// (-5, -5) tới (5, 5) -> dx = -10, dz = -10 -> (-10)² + (-10)² = 200
	got := gmath.DistanceSq(-5, -5, 5, 5)
	if got != 200 {
		t.Fatalf("expected 200 for negative coordinates, got %d", got)
	}
}

// TestManhattanDistance xác thực thuật toán khoảng cách Manhattan thô dùng cho A* Pathfinding.
func TestManhattanDistance(t *testing.T) {
	// (0,0) tới (3,4) -> |0-3| + |0-4| = 7
	if got := gmath.ManhattanDistance(0, 0, 3, 4); got != 7 {
		t.Fatalf("expected Manhattan distance 7, got %d", got)
	}

	// Kiểm tra trường hợp giá trị âm
	if got := gmath.ManhattanDistance(-1, -2, 2, 3); got != 8 { // |-1-2| + |-2-3| = 3 + 5 = 8
		t.Fatalf("expected Manhattan distance 8 for negative bounds, got %d", got)
	}
}

// ─── RANGE PROXIMITY CORRECTNESS TESTS ───────────────────────────────────────

func TestInRange(t *testing.T) {
	// Khoảng cách hình học = sqrt(3² + 4²) = 5.0 chính xác trên biên
	if !gmath.InRange(0, 0, 3, 4, 5.0) {
		t.Fatal("InRange(0,0,3,4,5.0) should be true (exactly on boundary)")
	}
	if gmath.InRange(0, 0, 3, 4, 4.9) {
		t.Fatal("InRange(0,0,3,4,4.9) should be false (just outside)")
	}
}

// TestInRangeInt xác thực hàm so sánh khoảng cách bằng số nguyên thô trên hot-path.
func TestInRangeInt(t *testing.T) {
	if !gmath.InRangeInt(0, 0, 3, 4, 5) {
		t.Fatal("InRangeInt should return true for exact boundary fit")
	}
	if gmath.InRangeInt(0, 0, 3, 4, 4) {
		t.Fatal("InRangeInt should return false for out of range bounds")
	}
}

// TestRangeProximityEdgeCases kiểm thử các trường hợp đặc biệt như bán kính bằng 0
// và tính chất đối xứng qua lại của thuật toán kiểm tra tầm đánh/aggro.
func TestRangeProximityEdgeCases(t *testing.T) {
	// Case 1: Bán kính bằng 0 (Chỉ đúng khi trùng khít vị trí tọa độ)
	if !gmath.InRange(5, 5, 5, 5, 0.0) {
		t.Fatal("same point with radius 0 should be true")
	}
	if gmath.InRange(5, 5, 6, 5, 0.0) {
		t.Fatal("different point with radius 0 should be false")
	}

	// Case 2: Tính đối xứng (Symmetric Check)
	a := gmath.InRange(10, 20, 45, 60, 100.0)
	b := gmath.InRange(45, 60, 10, 20, 100.0)
	if a != b {
		t.Fatal("InRange algorithm must be perfectly symmetric")
	}
}

// ─── MAP BOUNDS VALIDATION TESTS ─────────────────────────────────────────────

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

// ─── CLAMPING UTILITIES TESTS ────────────────────────────────────────────────

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

// ─── STRICT ZERO-ALLOCATION CONTRACTS (AllocsPerRun) ───────────────────────

func TestGmathZeroAllocations(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		_ = gmath.DistanceSq(-10, 20, 45, -90)
		_ = gmath.ManhattanDistance(10, 10, 20, 20)
		_ = gmath.InRange(5, 5, 10, 10, 15.5)
		_ = gmath.InRangeInt(5, 5, 10, 10, 15)
		_, _ = gmath.ClampPos(-5, 200, 0, 100)
	})

	if allocs > 0 {
		t.Fatalf("gmath operations violated zero-alloc contract: got %f allocations", allocs)
	}
}

// ─── GRANULAR MICRO-BENCHMARKS ────────────────────────────────────────────────

func BenchmarkDistanceSq(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Đẩy data vào Sink toàn cục để chặn đứng việc tối ưu hóa xóa code thừa
		sinkInt = gmath.DistanceSq(10, 20, 73, 85)
	}
}

func BenchmarkManhattanDistance(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = gmath.ManhattanDistance(10, 20, 73, 85)
	}
}

func BenchmarkInRange(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBool = gmath.InRange(10, 20, 73, 85, 5.0)
	}
}

func BenchmarkInRangeInt(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBool = gmath.InRangeInt(10, 20, 73, 85, 5)
	}
}

func BenchmarkInBounds(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBool = gmath.InBounds(50, 77, 0, 100)
	}
}

func BenchmarkClampPos(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt, _ = gmath.ClampPos(-3, 105, 0, 100)
	}
}

// BenchmarkDistanceSqVsSqrt kiểm nghiệm thực tế độ chênh lệch chi phí xử lý CPU
// giữa giải pháp so sánh số nguyên gmath và việc lạm dụng hàm toán học math.Sqrt của thư viện Go chuẩn.
func BenchmarkDistanceSqVsSqrt(b *testing.B) {
	b.Run("DistanceSq_pure_int", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sinkBool = gmath.DistanceSq(10, 20, 73, 85) <= 25
		}
	})
	b.Run("math_Sqrt_float_baseline", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			dx := float64(10 - 73)
			dz := float64(20 - 85)
			sinkBool = math.Sqrt(dx*dx+dz*dz) <= 5.0
		}
	})
}
