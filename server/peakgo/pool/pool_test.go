package pool_test

import (
	"server/peakgo/pool"
	"testing"
)

// ─── BENCHMARK SINK VARIABLES ────────────────────────────────────────────────
//
// Sử dụng các biến Package-Level toàn cục làm đích đến (Sink) cho dữ liệu đầu ra.
// Điều này chặn đứng hoàn toàn việc Go Compiler tự động tối ưu hóa và xóa bỏ
// khối lệnh chạy thử nghiệm (Dead-code elimination) khi không dùng biến.
var (
	sinkBytes []byte
	sinkSlice any
)

type testEntry struct{ A, B int }

// ─── FUNCTIONAL CORRECTNESS TESTS ───────────────────────────────────────────

// TestBytesPoolResetOnPut xác thực chu trình Get/Put cơ bản của BytesPool,
// đảm bảo độ dài (length) luôn được trả về mức mặc định sau khi quay lại pool.
func TestBytesPoolResetOnPut(t *testing.T) {
	p := pool.NewBytesPool(64)
	buf := p.Get()
	*buf = (*buf)[:10] // Cố tình gọt ngắn dung lượng khi sử dụng
	p.Put(buf)

	buf2 := p.Get()
	if len(*buf2) != 64 {
		t.Fatalf("expected len 64 after Put/Get cycle, got %d", len(*buf2))
	}
}

// TestBytesPoolOversizedBuffer bổ sung kiểm thử hàng rào chống nhiễm bẩn RAM (Pool Pollution).
// Các bộ đệm bị phình to quá 4 lần kích thước nền bắt buộc phải bị loại bỏ hoàn toàn khỏi Pool.
func TestBytesPoolOversizedBuffer(t *testing.T) {
	baselineSize := 64
	p := pool.NewBytesPool(baselineSize)

	buf := p.Get()
	// Mở rộng bộ đệm vượt ngưỡng an toàn (64 * 4 = 256 bytes)
	*buf = make([]byte, 300)
	p.Put(buf) // Hàm Put bắt buộc phải từ chối và thả nổi cho GC tự dọn dẹp

	// Lấy một thực thể mới, Pool do đã drop mảng 300 bytes kia nên bắt buộc
	// phải cấp phát một mảng mới tinh có kích thước trả về chuẩn là 64.
	buf2 := p.Get()
	if cap(*buf2) > baselineSize*4 {
		t.Fatalf("expected oversized buffer to be dropped, but pool retained cap %d", cap(*buf2))
	}
}

// TestSlicePoolLenZeroOnGet đảm bảo lát cắt cấu trúc lấy từ SlicePool
// luôn ở trạng thái sạch sẽ có độ dài bằng 0 để sẵn sàng cho lệnh append.
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

// TestSlicePoolOversizedSlice xác thực tính năng chống phình mảng backing array của SlicePool.
func TestSlicePoolOversizedSlice(t *testing.T) {
	baselineCap := 4
	p := pool.NewSlicePool[int](baselineCap)

	ps := p.Get()
	// Append liên tục làm phình dung lượng vượt ngưỡng an toàn (4 * 4 = 16)
	for i := 0; i < 20; i++ {
		*ps = append(*ps, i)
	}
	p.Put(ps) // Kích hoạt cơ chế drop bỏ mảng khổng lồ

	ps2 := p.Get()
	if cap(*ps2) > baselineCap*4 {
		t.Fatalf("expected oversized slice to be dropped, but pool retained cap %d", cap(*ps2))
	}
}

// TestSlicePoolClearsReferences bổ sung bài kiểm thử chí mạng chống rò rỉ bộ nhớ ngầm (GC Leaks).
// Đảm bảo toàn bộ các con trỏ hoặc interface lưu trữ cũ phải bị xóa sạch (zeroed) khi trả về pool,
// nếu không bộ gom rác của Go vẫn sẽ hiểu lầm là đối tượng đang sống và không chịu giải phóng RAM.
func TestSlicePoolClearsReferences(t *testing.T) {
	type dummyObject struct {
		Ptr *int
	}

	p := pool.NewSlicePool[dummyObject](4)
	ps := p.Get()

	liveValue := 100
	*ps = append(*ps, dummyObject{Ptr: &liveValue})
	p.Put(ps) // Lệnh Put bắt buộc phải quét sạch và đưa phần tử thứ 0 về nil

	// Tiến hành bẫy thử nghiệm: Lấy thực thể ra, ép reslice vượt độ dài len=0
	// để đọc lén dữ liệu "thây ma" (zombie data) nằm trong vùng nhớ đệm backing array.
	ps2 := p.Get()
	zombieSlice := (*ps2)[:1]
	if zombieSlice[0].Ptr != nil {
		t.Fatal("expected stale object pointers to be zeroed out in Put to prevent phantom GC reference leaks")
	}
}

// ─── STRICT ZERO-ALLOCATION CONTRACTS (AllocsPerRun) ───────────────────────

// TestPoolZeroAllocations cưỡng ép hợp đồng tối ưu RAM của framework,
// cấm phát sinh bất kỳ một lượt cấp phát mới nào trên Heap trong chu trình tuần hoàn kín.
func TestPoolZeroAllocations(t *testing.T) {
	bp := pool.NewBytesPool(128)
	allocs := testing.AllocsPerRun(500, func() {
		buf := bp.Get()
		*buf = (*buf)[:32]
		bp.Put(buf)
	})
	if allocs > 0 {
		t.Fatalf("BytesPool violated zero-allocation contract: got %f allocs", allocs)
	}

	sp := pool.NewSlicePool[testEntry](32)
	allocs = testing.AllocsPerRun(500, func() {
		ps := sp.Get()
		*ps = append(*ps, testEntry{1, 1}, testEntry{2, 2})
		sp.Put(ps)
	})
	if allocs > 0 {
		t.Fatalf("SlicePool violated zero-allocation contract: got %f allocs", allocs)
	}
}

// ─── BYTESPOOL PERFORMANCE BENCHMARKS ────────────────────────────────────────

func BenchmarkBytesPoolGetPut(b *testing.B) {
	p := pool.NewBytesPool(1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := p.Get()
		*buf = (*buf)[:256]
		p.Put(buf)
	}
}

func BenchmarkBytesPoolGetPutBaseline(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Đã sửa: Gán vào biến Sink toàn cục chống compiler triệt tiêu mã lệnh thừa
		buf := make([]byte, 256)
		sinkBytes = buf
	}
}

// ─── SLICEPOOL PERFORMANCE BENCHMARKS ────────────────────────────────────────

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
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := make([]testEntry, 0, 16)
		s = append(s, testEntry{1, 2}, testEntry{3, 4})
		sinkSlice = s
	}
}

// ─── GRANULAR SCALE & WORKLOAD SCALING BENCHMARKS ────────────────────────────
//
// Bổ sung các bài đo tải quy mô lớn theo yêu cầu của kỹ sư để giám sát chặt chẽ
// chi phí CPU của vòng lặp xóa sạch dữ liệu (Zeroing loop) tương ứng với các kích thước map khác nhau.

func BenchmarkSlicePoolRecycleCycle_Scale32(b *testing.B) {
	p := pool.NewSlicePool[testEntry](32)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps := p.Get()
		for j := 0; j < 32; j++ {
			*ps = append(*ps, testEntry{j, j})
		}
		p.Put(ps)
	}
}

func BenchmarkSlicePoolRecycleCycle_Scale128(b *testing.B) {
	p := pool.NewSlicePool[testEntry](128)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps := p.Get()
		for j := 0; j < 128; j++ {
			*ps = append(*ps, testEntry{j, j})
		}
		p.Put(ps)
	}
}

func BenchmarkSlicePoolRecycleCycle_Scale1024(b *testing.B) {
	p := pool.NewSlicePool[testEntry](1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps := p.Get()
		for j := 0; j < 1024; j++ {
			*ps = append(*ps, testEntry{j, j})
		}
		p.Put(ps)
	}
}

// BenchmarkBytesPoolGetPutPeakGo measures Get/Put cycle with 1024-byte fixed buffer.
func BenchmarkBytesPoolGetPutPeakGo(b *testing.B) {
	p := pool.NewBytesPool(1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := p.Get()
		p.Put(buf)
	}
}
