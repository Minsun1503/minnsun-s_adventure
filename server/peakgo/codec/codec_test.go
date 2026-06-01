package codec_test

import (
	"server/peakgo/codec"
	"testing"
)

// ─── Round-trip correctness ───────────────────────────────────────────────────

func TestPrimitiveRoundTrip(t *testing.T) {
	buf := make([]byte, 8)

	// uint16
	codec.WriteUint16(buf, 0xBEEF)
	if got := codec.ReadUint16(buf); got != 0xBEEF {
		t.Fatalf("uint16 round-trip: got %X want %X", got, 0xBEEF)
	}

	// int32
	codec.WriteInt32(buf, -42)
	if got := codec.ReadInt32(buf); got != -42 {
		t.Fatalf("int32 round-trip: got %d want -42", got)
	}

	// uint64
	codec.WriteUint64(buf, 0xDEADBEEFCAFEBABE)
	if got := codec.ReadUint64(buf); got != 0xDEADBEEFCAFEBABE {
		t.Fatalf("uint64 round-trip: got %X", got)
	}
}

func TestReadMovePayload(t *testing.T) {
	buf := make([]byte, 8)
	codec.WriteInt32(buf[0:4], 37)
	codec.WriteInt32(buf[4:8], -12)

	p, ok := codec.ReadMovePayload(buf)
	if !ok {
		t.Fatal("ReadMovePayload returned ok=false on valid 8-byte payload")
	}
	if p.X != 37 || p.Z != -12 {
		t.Fatalf("ReadMovePayload: got (%d, %d), want (37, -12)", p.X, p.Z)
	}

	_, bad := codec.ReadMovePayload(buf[:4])
	if bad {
		t.Fatal("ReadMovePayload should return ok=false for wrong-length payload")
	}
}

func TestReadAttackPayload(t *testing.T) {
	buf := make([]byte, 8)
	codec.WriteUint64(buf, 9999)

	p, ok := codec.ReadAttackPayload(buf)
	if !ok {
		t.Fatal("ReadAttackPayload returned ok=false on valid 8-byte payload")
	}
	if p.TargetID != 9999 {
		t.Fatalf("ReadAttackPayload: got %d want 9999", p.TargetID)
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

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

func BenchmarkWriteReadUint64(b *testing.B) {
	buf := make([]byte, 8)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		codec.WriteUint64(buf, 0xDEADBEEF)
		_ = codec.ReadUint64(buf)
	}
}
