package codec_test

import (
	"math"
	"server/peakgo/codec"
	"testing"
)

// ─── PRIMITIVE ROUND-TRIP & BOUNDARY TESTS ───────────────────────────────────

func TestUint8RoundTrip(t *testing.T) {
	buf := make([]byte, 1)
	boundaries := []uint8{0, 42, 128, math.MaxUint8}

	for _, want := range boundaries {
		codec.WriteUint8(buf, want)
		if got := codec.ReadUint8(buf); got != want {
			t.Fatalf("Uint8 mismatch: got %d want %d", got, want)
		}
	}
}

func TestInt16RoundTrip(t *testing.T) {
	buf := make([]byte, 2)
	boundaries := []int16{math.MinInt16, -256, 0, 42, math.MaxInt16}

	for _, want := range boundaries {
		codec.WriteInt16(buf, want)
		if got := codec.ReadInt16(buf); got != want {
			t.Fatalf("Int16 mismatch: got %d want %d", got, want)
		}
	}
}

func TestUint16RoundTrip(t *testing.T) {
	buf := make([]byte, 2)
	boundaries := []uint16{0, 1024, 0xBEEF, math.MaxUint16}

	for _, want := range boundaries {
		codec.WriteUint16(buf, want)
		if got := codec.ReadUint16(buf); got != want {
			t.Fatalf("Uint16 mismatch: got %X want %X", got, want)
		}
	}
}

func TestInt32RoundTrip(t *testing.T) {
	buf := make([]byte, 4)
	boundaries := []int32{math.MinInt32, -42, 0, 500000, math.MaxInt32}

	for _, want := range boundaries {
		codec.WriteInt32(buf, want)
		if got := codec.ReadInt32(buf); got != want {
			t.Fatalf("Int32 mismatch: got %d want %d", got, want)
		}
	}
}

func TestUint32RoundTrip(t *testing.T) {
	buf := make([]byte, 4)
	boundaries := []uint32{0, 123456, 0xDEADBEEF, math.MaxUint32}

	for _, want := range boundaries {
		codec.WriteUint32(buf, want)
		if got := codec.ReadUint32(buf); got != want {
			t.Fatalf("Uint32 mismatch: got %d want %d", got, want)
		}
	}
}

func TestInt64RoundTrip(t *testing.T) {
	buf := make([]byte, 8)
	boundaries := []int64{math.MinInt64, -999999, 0, 1234567890, math.MaxInt64}

	for _, want := range boundaries {
		codec.WriteInt64(buf, want)
		if got := codec.ReadInt64(buf); got != want {
			t.Fatalf("Int64 mismatch: got %d want %d", got, want)
		}
	}
}

func TestUint64RoundTrip(t *testing.T) {
	buf := make([]byte, 8)
	boundaries := []uint64{0, 0xDEADC0DE, 0xDEADBEEFCAFEBABE, math.MaxUint64}

	for _, want := range boundaries {
		codec.WriteUint64(buf, want)
		if got := codec.ReadUint64(buf); got != want {
			t.Fatalf("Uint64 mismatch: got %X want %X", got, want)
		}
	}
}

func TestFloat32RoundTrip(t *testing.T) {
	buf := make([]byte, 4)
	boundaries := []float32{-math.MaxFloat32, -123.456, 0.0, 3.14159, math.MaxFloat32}

	for _, want := range boundaries {
		codec.WriteFloat32(buf, want)
		if got := codec.ReadFloat32(buf); got != want {
			t.Fatalf("Float32 mismatch: got %f want %f", got, want)
		}
	}
}

// ─── STRING HELPERS CORRECTNESS TESTS ────────────────────────────────────────

func TestReadStringLen16Valid(t *testing.T) {
	// Khởi tạo buffer chứa: [Length 2-bytes BE] + [Chuỗi Text UTF-8]
	message := "Minnsun's Adventure Realtime Game Server"
	buf := make([]byte, 2+len(message))
	codec.WriteUint16(buf[0:2], uint16(len(message)))
	copy(buf[2:], message)

	got, ok := codec.ReadStringLen16(buf)
	if !ok {
		t.Fatal("expected ReadStringLen16 to parse valid buffer successfully")
	}
	if got != message {
		t.Fatalf("string data corruption: got %q, want %q", got, message)
	}
}

func TestReadStringLen16InvalidPayload(t *testing.T) {
	// Case 1: Buffer quá ngắn, không đọc nổi trường miêu tả Length Prefix (2 bytes)
	bufShort := []byte{0x00}
	if _, ok := codec.ReadStringLen16(bufShort); ok {
		t.Fatal("expected failure when buffer cannot hold length prefix descriptor")
	}

	// Case 2: Độ dài khai báo lớn hơn dung lượng thực tế của mảng byte dữ liệu
	bufMissingBody := []byte{0x00, 0x10, 0x41, 0x42} // Khai báo dài 16 bytes nhưng thực tế có 2 bytes body
	if _, ok := codec.ReadStringLen16(bufMissingBody); ok {
		t.Fatal("expected failure when packet data body is truncated/incomplete")
	}
}

// ─── COMPOSITE COMPONENT READERS TESTS ───────────────────────────────────────

func TestReadMovePayloadValid(t *testing.T) {
	buf := make([]byte, 8)
	codec.WriteInt32(buf[0:4], 37)
	codec.WriteInt32(buf[4:8], -12)

	p, ok := codec.ReadMovePayload(buf)
	if !ok {
		t.Fatal("ReadMovePayload returned ok=false on valid 8-byte payload")
	}
	if p.X != 37 || p.Z != -12 {
		t.Fatalf("ReadMovePayload data mismatch: got (%d, %d), want (37, -12)", p.X, p.Z)
	}
}

func TestReadMovePayloadInvalidLength(t *testing.T) {
	buf := make([]byte, 4) // Kích thước sai (MOVE bắt buộc phải là 8 bytes)
	if _, ok := codec.ReadMovePayload(buf); ok {
		t.Fatal("ReadMovePayload must fail when incoming byte slice length is not exactly 8")
	}
}

func TestReadAttackPayloadValid(t *testing.T) {
	buf := make([]byte, 8)
	codec.WriteUint64(buf, 9999)

	p, ok := codec.ReadAttackPayload(buf)
	if !ok {
		t.Fatal("ReadAttackPayload returned ok=false on valid 8-byte payload")
	}
	if p.TargetID != 9999 {
		t.Fatalf("ReadAttackPayload mismatch: got %d want 9999", p.TargetID)
	}
}

func TestReadAttackPayloadInvalidLength(t *testing.T) {
	buf := make([]byte, 7) // Thiếu 1 byte so với chuẩn của ATTACK packet
	if _, ok := codec.ReadAttackPayload(buf); ok {
		t.Fatal("ReadAttackPayload must fail when incoming byte slice length is not exactly 8")
	}
}

// ─── STRICT ZERO-ALLOCATION ASSERTIONS (AllocsPerRun) ───────────────────────

// TestCodecZeroAllocations ép chặt hợp đồng vận hành: Tuyệt đối không phát sinh
// rò rỉ bộ nhớ hoặc cấp phát Heap ngầm trong quá trình biến đổi nhị phân.
func TestCodecZeroAllocations(t *testing.T) {
	buf := make([]byte, 8)

	// Kiểm tra các Composite Hot-Path Readers
	allocs := testing.AllocsPerRun(1000, func() {
		_, _ = codec.ReadMovePayload(buf)
	})
	if allocs != 0 {
		t.Fatalf("ReadMovePayload violated zero-alloc contract: got %f allocations", allocs)
	}

	allocs = testing.AllocsPerRun(1000, func() {
		_, _ = codec.ReadAttackPayload(buf)
	})
	if allocs != 0 {
		t.Fatalf("ReadAttackPayload violated zero-alloc contract: got %f allocations", allocs)
	}

	// Kiểm tra các Primitive Readers/Writers thô
	allocs = testing.AllocsPerRun(1000, func() {
		codec.WriteFloat32(buf[0:4], 3.14)
		_ = codec.ReadFloat32(buf[0:4])
	})
	if allocs != 0 {
		t.Fatalf("Primitive operations leaked %f allocations to the heap", allocs)
	}
}

// ─── GRANULAR MICRO-BENCHMARKS ────────────────────────────────────────────────
//
// Đã tách biệt hoàn toàn theo yêu cầu của kỹ sư: Đo đạc độc lập chi phí của từng
// hàm riêng lẻ, không lồng ghép Đọc/Ghi chung một vòng lặp để số liệu chuẩn xác nhất.

func BenchmarkReadUint16(b *testing.B) {
	buf := []byte{0x00, 0xFF}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = codec.ReadUint16(buf)
	}
}

func BenchmarkWriteUint16(b *testing.B) {
	buf := make([]byte, 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		codec.WriteUint16(buf, 0xBEEF)
	}
}

func BenchmarkReadUint32(b *testing.B) {
	buf := []byte{0x00, 0x00, 0xFF, 0xFF}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = codec.ReadUint32(buf)
	}
}

func BenchmarkWriteUint32(b *testing.B) {
	buf := make([]byte, 4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		codec.WriteUint32(buf, 0xDEADBEEF)
	}
}

func BenchmarkReadUint64(b *testing.B) {
	buf := []byte{0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0xFF}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = codec.ReadUint64(buf)
	}
}

func BenchmarkWriteUint64(b *testing.B) {
	buf := make([]byte, 8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		codec.WriteUint64(buf, 0xDEADBEEFCAFEBABE)
	}
}

func BenchmarkReadMovePayload(b *testing.B) {
	buf := make([]byte, 8)
	codec.WriteInt32(buf[0:4], 50)
	codec.WriteInt32(buf[4:8], 75)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.ReadMovePayload(buf)
	}
}

func BenchmarkReadAttackPayload(b *testing.B) {
	buf := make([]byte, 8)
	codec.WriteUint64(buf, 42)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = codec.ReadAttackPayload(buf)
	}
}

// BenchmarkMarshalPeakGo measures the cost of encoding a MovePayload struct into a byte buffer.
// Simulates the hot-path Marshal operation used in packet construction.
func BenchmarkMarshalPeakGo(b *testing.B) {
	buf := make([]byte, 8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		codec.WriteInt32(buf[0:4], 50)
		codec.WriteInt32(buf[4:8], 75)
	}
}
