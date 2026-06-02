package broadcast_test

import (
	"encoding/binary"
	"server/peakgo/broadcast"
	"testing"
)

// ─── Frame correctness ────────────────────────────────────────────────────────

func TestFrameLayout(t *testing.T) {
	payload := []byte{0xAB, 0xCD}
	pkt := broadcast.Frame(0x05, payload)

	// Total length: 2 (length prefix) + 1 (opcode) + 2 (payload)
	if len(pkt) != 5 {
		t.Fatalf("expected packet length 5, got %d", len(pkt))
	}

	// Length field = opcode(1) + payload(2) = 3
	lengthField := binary.BigEndian.Uint16(pkt[0:2])
	if lengthField != 3 {
		t.Fatalf("expected length field 3, got %d", lengthField)
	}

	// Opcode
	if pkt[2] != 0x05 {
		t.Fatalf("expected opcode 0x05, got %02X", pkt[2])
	}

	// Payload bytes
	if pkt[3] != 0xAB || pkt[4] != 0xCD {
		t.Fatalf("payload mismatch: got %02X %02X", pkt[3], pkt[4])
	}
}

func TestFrameEmptyPayload(t *testing.T) {
	pkt := broadcast.Frame(0x07, nil)
	if len(pkt) != 3 {
		t.Fatalf("expected 3 bytes for empty payload, got %d", len(pkt))
	}
	lengthField := binary.BigEndian.Uint16(pkt[0:2])
	if lengthField != 1 {
		t.Fatalf("expected length field 1 (opcode only), got %d", lengthField)
	}
	if pkt[2] != 0x07 {
		t.Fatalf("expected opcode 0x07, got %02X", pkt[2])
	}
}

func TestFrameText(t *testing.T) {
	pkt := broadcast.FrameText(0x01, "hello")
	// 2 (len prefix) + 1 (opcode) + 5 (text)
	if len(pkt) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(pkt))
	}
	if string(pkt[3:]) != "hello" {
		t.Fatalf("expected 'hello' in payload, got %q", string(pkt[3:]))
	}
}

func TestFrameIntoNoAlloc(t *testing.T) {
	dst := make([]byte, 0, 64)
	payload := []byte{0x01, 0x02, 0x03}
	result := broadcast.FrameInto(dst, 0x05, payload)

	if len(result) != 6 { // 2+1+3
		t.Fatalf("expected 6 bytes, got %d", len(result))
	}
	// Verify same underlying array (no realloc)
	if cap(result) != 64 {
		t.Fatal("FrameInto reallocated — should reuse dst when cap is sufficient")
	}
}

func TestFrameIntoGrowsWhenNeeded(t *testing.T) {
	dst := make([]byte, 0, 2) // too small
	payload := []byte{0x01, 0x02, 0x03}
	result := broadcast.FrameInto(dst, 0x05, payload)
	if len(result) != 6 {
		t.Fatalf("expected 6 bytes after grow, got %d", len(result))
	}
}

// ─── Typed builders correctness ───────────────────────────────────────────────

func TestBuildError(t *testing.T) {
	pkt := broadcast.BuildError(2, "db error")
	if len(pkt) < 3 {
		t.Fatal("packet too short")
	}
	if pkt[2] != 0xFF {
		t.Fatalf("expected opcode 0xFF, got %02X", pkt[2])
	}
	// ErrorCode at payload[0:2] = 2
	errCode := binary.BigEndian.Uint16(pkt[3:5])
	if errCode != 2 {
		t.Fatalf("expected error code 2, got %d", errCode)
	}
}

func TestBuildSuccess(t *testing.T) {
	pkt := broadcast.BuildSuccess("registered!")
	if pkt[2] != 0x01 {
		t.Fatalf("expected opcode 0x01, got %02X", pkt[2])
	}
}

func TestBuildSpawnEntity(t *testing.T) {
	p := broadcast.SpawnPayload{
		EntityID: 42,
		Type:     1, // monster
		MapID:    1,
		X:        30,
		Z:        40,
		Name:     "Bandit",
	}
	pkt := broadcast.BuildSpawnEntity(p)
	if pkt[2] != 0x10 {
		t.Fatalf("expected opcode 0x10, got %02X", pkt[2])
	}
	entityID := binary.BigEndian.Uint64(pkt[3:11])
	if entityID != 42 {
		t.Fatalf("expected EntityID 42, got %d", entityID)
	}
}

func TestBuildPositionSync(t *testing.T) {
	p := broadcast.PositionSyncPayload{EntityID: 7, X: 15, Z: 25}
	pkt := broadcast.BuildPositionSync(p)
	if pkt[2] != 0x12 {
		t.Fatalf("expected opcode 0x12, got %02X", pkt[2])
	}
}

func TestBuildStatsSync(t *testing.T) {
	p := broadcast.StatsSyncPayload{EntityID: 3, HP: 80, MaxHP: 100}
	pkt := broadcast.BuildStatsSync(p)
	if pkt[2] != 0x13 {
		t.Fatalf("expected opcode 0x13, got %02X", pkt[2])
	}
}

func TestBuildChatMessage(t *testing.T) {
	p := broadcast.ChatPayload{Channel: 2, SenderName: "Hero", Message: "hello world"}
	pkt := broadcast.BuildChatMessage(p)
	if pkt[2] != 0x15 {
		t.Fatalf("expected opcode 0x15, got %02X", pkt[2])
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkFrame(b *testing.B) {
	payload := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = broadcast.Frame(0x05, payload)
	}
}

func BenchmarkFrameIntoNoAlloc(b *testing.B) {
	dst := make([]byte, 0, 1024)
	payload := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = broadcast.FrameInto(dst[:0], 0x05, payload)
	}
}

func BenchmarkBuildPositionSync(b *testing.B) {
	p := broadcast.PositionSyncPayload{EntityID: 42, X: 30, Z: 50}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = broadcast.BuildPositionSync(p)
	}
}

func BenchmarkBuildStatsSync(b *testing.B) {
	p := broadcast.StatsSyncPayload{EntityID: 5, HP: 80, MaxHP: 100, MP: 60, MaxMP: 100, Dam: 25, Level: 3}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = broadcast.BuildStatsSync(p)
	}
}

// ─── Typed zero-alloc writers correctness ─────────────────────────────────────

func TestWritePositionSync(t *testing.T) {
	dst := make([]byte, 0, 128)
	p := broadcast.PositionSyncPayload{EntityID: 99, X: 100, Z: 200}

	result := broadcast.WritePositionSync(dst, p)

	// 2 (len) + 1 (opcode) + 16 (payload) = 19
	if len(result) != 19 {
		t.Fatalf("expected 19 bytes, got %d", len(result))
	}
	if result[2] != 0x12 {
		t.Fatalf("expected opcode 0x12, got %02X", result[2])
	}
	if cap(result) != 128 {
		t.Fatal("WritePositionSync reallocated memory layout")
	}
}

func TestWriteChatMessage(t *testing.T) {
	dst := make([]byte, 0, 256)
	p := broadcast.ChatPayload{
		Channel:    1,
		SenderName: "GM",
		Message:    "Server maintenance soon",
	}

	result := broadcast.WriteChatMessage(dst, p)

	if result[2] != 0x15 {
		t.Fatalf("expected opcode 0x15, got %02X", result[2])
	}

	// Kiểm tra xem dữ liệu string có được ghi đúng vào buffer không
	// offset header (3) + channel (1) + senderLen (1) = 5
	senderName := string(result[5 : 5+len(p.SenderName)])
	if senderName != p.SenderName {
		t.Fatalf("expected sender %q, got %q", p.SenderName, senderName)
	}
}

// ─── Zero-Alloc Benchmarks (Crucial for Hot-Path contract) ────────────────────

func BenchmarkWritePositionSync(b *testing.B) {
	dst := make([]byte, 0, 512)
	p := broadcast.PositionSyncPayload{EntityID: 42, X: 30, Z: 50}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Trả len về 0 trước mỗi lần chạy nhưng giữ nguyên cap (capacity)
		dst = broadcast.WritePositionSync(dst[:0], p)
	}
}

func BenchmarkWriteChatMessage(b *testing.B) {
	dst := make([]byte, 0, 1024)
	p := broadcast.ChatPayload{
		Channel:    0,
		SenderName: "PlayerOne",
		Message:    "Need party for dungeon run now!",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = broadcast.WriteChatMessage(dst[:0], p)
	}
}
