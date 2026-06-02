package rng_test

import (
	"math/rand"
	"server/peakgo/rng"
	"sync"
	"testing"
)

// ─── BENCHMARK SINK VARIABLES ────────────────────────────────────────────────
//
// Sử dụng các biến Package-Level toàn cục làm đích đến (Sink) cho dữ liệu đầu ra.
// Điều này ngăn chặn hoàn toàn việc Go Compiler tự động tối ưu hóa và xóa bỏ
// khối lệnh chạy thử nghiệm (Dead-code elimination) trong vòng lặp Benchmark.
var (
	sinkInt   int
	sinkFloat float64
	sinkBool  bool
)

// ─── FUNCTIONAL CORRECTNESS TESTS ───────────────────────────────────────────

func TestIntnRange(t *testing.T) {
	for i := 0; i < 1000; i++ {
		v := rng.Intn(10)
		if v < 0 || v >= 10 {
			t.Fatalf("Intn(10) out of half-open interval [0,10): got %d", v)
		}
	}
}

func TestFloat64Range(t *testing.T) {
	for i := 0; i < 1000; i++ {
		v := rng.Float64()
		if v < 0.0 || v >= 1.0 {
			t.Fatalf("Float64() out of half-open interval [0.0,1.0): got %f", v)
		}
	}
}

func TestIntnRangeLoHi(t *testing.T) {
	for i := 0; i < 1000; i++ {
		v := rng.IntnRange(5, 10)
		if v < 5 || v >= 10 {
			t.Fatalf("IntnRange(5,10) out of half-open interval [5,10): got %d", v)
		}
	}
}

// TestChanceAPI xác thực tính chính xác của hàm ngữ nghĩa tỷ lệ xác suất mới bổ sung.
func TestChanceAPI(t *testing.T) {
	// Khóa điều kiện biên tuyệt đối
	if !rng.Chance(1.0) {
		t.Fatal("Chance(1.0) must always evaluate to true")
	}
	if rng.Chance(0.0) {
		t.Fatal("Chance(0.0) must always evaluate to false")
	}

	// Kiểm tra tính phân phối tương đối (gần đúng) của tỷ lệ 50% qua 1000 lượt roll
	hits := 0
	for i := 0; i < 1000; i++ {
		if rng.Chance(0.5) {
			hits++
		}
	}
	// Biên dao động an toàn cho 1000 mẫu thử tỷ lệ 50% là trong khoảng [400, 600]
	if hits < 400 || hits > 600 {
		t.Fatalf("Chance(0.5) sampling bias suspected: got %d hits out of 1000", hits)
	}
}

// TestBorrowReturnBulk xác thực giải pháp tối ưu hóa vòng lặp lớn thông qua mượn RNG trực tiếp.
func TestBorrowReturnBulk(t *testing.T) {
	r := rng.Borrow()
	if r == nil {
		t.Fatal("expected valid *rand.Rand generator instance from pool, got nil")
	}
	defer rng.Return(r)

	// Xả hàng loạt lệnh sinh số ngẫu nhiên trên thực thể mượn mà không dính chi phí Get/Put
	for i := 0; i < 100; i++ {
		v := r.Intn(100)
		if v < 0 || v >= 100 {
			t.Fatalf("borrowed instance data corruption at index %d: value %d", i, v)
		}
	}
}

// TestShuffle bổ sung bài kiểm thử bị thiếu cho thuật toán xáo trộn Fisher-Yates (Shuffle).
// Đảm bảo cấu trúc mảng sau xáo trộn giữ nguyên độ dài và không làm đột biến/mất mát phần tử.
func TestShuffle(t *testing.T) {
	s := []int{10, 20, 30, 40, 50, 60, 70, 80, 90}

	// Sao chép một bản mẫu thô để đối chiếu dữ liệu gốc
	orig := append([]int(nil), s...)

	rng.Shuffle(len(s), func(i, j int) {
		s[i], s[j] = s[j], s[i]
	})

	if len(s) != len(orig) {
		t.Fatalf("Shuffle modified slice length: got %d, want %d", len(s), len(orig))
	}

	// Xác thực tính toàn vẹn phần tử: Đảm bảo không có phần tử nào bị ghi đè trùng lặp hoặc biến mất
	counts := make(map[int]int)
	for _, v := range s {
		counts[v]++
	}
	for _, v := range orig {
		counts[v]--
		if counts[v] < 0 {
			t.Fatalf("Shuffle corrupted slice data. Element %d mismatch or duplicated ngầm", v)
		}
	}
}

// TestConcurrentSafety xác thực tính an toàn đa luồng (Thread-Safety).
// Được thiết kế bẫy kiểm tra Data Race khi hàng loạt Goroutines cùng xả lệnh sinh số ngẫu nhiên liên tục.
func TestConcurrentSafety(t *testing.T) {
	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = rng.Intn(100)
				_ = rng.Float64()
				_ = rng.Chance(0.15)
			}
		}()
	}
	wg.Wait()
}

// TestDistributionBias giám sát chất lượng phân phối ngẫu nhiên của Seed để tránh bug trùng lặp seed.
func TestDistributionBias(t *testing.T) {
	const buckets = 10
	counts := make([]int, buckets)
	const N = 10000
	for i := 0; i < N; i++ {
		v := rng.Float64()
		idx := int(v * float64(buckets))
		if idx == buckets {
			idx = buckets - 1
		}
		counts[idx]++
	}

	expected := N / buckets
	tolerance := expected * 30 / 100
	for i, c := range counts {
		if c < expected-tolerance || c > expected+tolerance {
			t.Fatalf("bucket %d has %d samples (expected ~%d ±%d) — possible seeding bias", i, c, expected, tolerance)
		}
	}
}

// ─── STRICT ZERO-ALLOCATION CONTRACTS (AllocsPerRun) ───────────────────────

// TestRngZeroAllocations chứng minh cam kết hiệu năng tuyệt đối của hệ thống:
// Không được phép sinh bất kỳ một lượt cấp phát Heap (RAM) rác nào trên hot-path.
func TestRngZeroAllocations(t *testing.T) {
	// Kích hoạt nóng làm ấm Pool trước
	_ = rng.Intn(10)

	allocs := testing.AllocsPerRun(1000, func() {
		_ = rng.Intn(100)
		_ = rng.Float64()
		_ = rng.Chance(0.5)
		_ = rng.IntnRange(5, 15)
	})
	if allocs > 0 {
		t.Fatalf("Rng standard helpers violated zero-alloc contract: got %f heap allocations", allocs)
	}

	allocs = testing.AllocsPerRun(1000, func() {
		r := rng.Borrow()
		_ = r.Intn(10)
		_ = r.Float64()
		rng.Return(r)
	})
	if allocs > 0 {
		t.Fatalf("Rng Borrow/Return lifecycle leaked %f heap allocations", allocs)
	}
}

// ─── GRANULAR PERFORMANCE BENCHMARKS ─────────────────────────────────────────

func BenchmarkIntn(b *testing.B) {
	rng.WarmUp(8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Đã sửa: Gán vào biến Sink toàn cục chống compiler triệt tiêu mã lệnh thừa
		sinkInt = rng.Intn(100)
	}
}

func BenchmarkFloat64(b *testing.B) {
	rng.WarmUp(8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkFloat = rng.Float64()
	}
}

func BenchmarkChance(b *testing.B) {
	rng.WarmUp(8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBool = rng.Chance(0.25)
	}
}

// BenchmarkShuffle bổ sung bài kiểm thử hiệu năng xáo trộn vật phẩm/quái vật trên hot-path.
func BenchmarkShuffle(b *testing.B) {
	s := make([]int, 32) // Quy mô mảng Drop đồ hoặc Roaming quái tiêu chuẩn (32 phần tử)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rng.Shuffle(len(s), func(i, j int) {
			s[i], s[j] = s[j], s[i]
		})
	}
}

// ─── PARALLEL CONCURRENT WORKLOAD BENCHMARKS ─────────────────────────────────
//
// Hai bài đo tải song song này chính là đòn quyết định chứng minh sức mạnh của mô hình
// sync.Pool RNG đối chọi trực tiếp với cơ chế Khóa Mutex toàn cục của thư viện Go chuẩn.

func BenchmarkIntnConcurrent_PooledRng(b *testing.B) {
	rng.WarmUp(8)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = rng.Intn(100)
		}
	})
}

func BenchmarkIntnConcurrent_StdlibGlobalRandBaseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Sử dụng hàm sinh số của thư viện chuẩn Go ( math/rand toàn cục )
			// Ở môi trường chịu tải đa luồng cao, bài này sẽ bị nghẽn khóa (Mutex lock contention) rất nặng
			_ = rand.Intn(100)
		}
	})
}
