package broadcast_test

import (
	"encoding/binary"
	"server/peakgo/broadcast"
	"testing"
)

// ─── GENERIC FRAMING CORRECTNESS ─────────────────────────────────────────────

// TestFrameLayout kiểm tra cấu trúc cơ bản của một gói tin nhị phân xem
// kích thước, độ dài vùng đệm (Length Prefix) và Opcode có chính xác không.
func TestFrameLayout(t *testing.T) {
	payload := []byte{0xAB, 0xCD}
	// Sử dụng opcode tùy ý 0x05 để test tính năng đóng khung thô
	pkt := broadcast.Frame(0x05, payload)

	// Tổng kích thước mong muốn: 2 (Length) + 1 (Opcode) + 2 (Payload) = 5 bytes
	if len(pkt) != 5 {
		t.Fatalf("expected packet length 5, got %d", len(pkt))
	}

	// Giá trị trường Length = Opcode (1 byte) + Payload (2 bytes) = 3
	lengthField := binary.BigEndian.Uint16(pkt[0:2])
	if lengthField != 3 {
		t.Fatalf("expected length field 3, got %d", lengthField)
	}

	// Xác thực Opcode ghi vào byte thứ 3
	if pkt[2] != 0x05 {
		t.Fatalf("expected opcode 0x05, got %02X", pkt[2])
	}

	// Xác thực nội dung payload được copy chính xác
	if pkt[3] != 0xAB || pkt[4] != 0xCD {
		t.Fatalf("payload mismatch: got %02X %02X", pkt[3], pkt[4])
	}
}

// TestFrameEmptyPayload đảm bảo hệ thống vẫn đóng khung an toàn khi payload trống (nil).
func TestFrameEmptyPayload(t *testing.T) {
	pkt := broadcast.Frame(0x07, nil)
	if len(pkt) != 3 {
		t.Fatalf("expected 3 bytes for empty payload, got %d", len(pkt))
	}

	// Trường length lúc này chỉ tính vùng Opcode (1 byte)
	lengthField := binary.BigEndian.Uint16(pkt[0:2])
	if lengthField != 1 {
		t.Fatalf("expected length field 1 (opcode only), got %d", lengthField)
	}
}

// TestFrameText xác thực tính năng đóng gói chuỗi ký tự UTF-8 thuần túy.
func TestFrameText(t *testing.T) {
	pkt := broadcast.FrameText(broadcast.OpcodeSuccess, "hello")

	// 2 (Length) + 1 (Opcode) + 5 (Kích thước chuỗi "hello") = 8 bytes
	if len(pkt) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(pkt))
	}
	if string(pkt[3:]) != "hello" {
		t.Fatalf("expected 'hello' in payload, got %q", string(pkt[3:]))
	}
}

// TestFrameIntoLayout gia cố kiểm tra toàn diện hàm FrameInto.
// Không chỉ kiểm tra len/cap, mà rà soát chi tiết từng byte nội dung đầu ra.
func TestFrameIntoLayout(t *testing.T) {
	dst := make([]byte, 0, 64)
	payload := []byte{0x01, 0x02, 0x03}
	result := broadcast.FrameInto(dst, 0x05, payload)

	// Kiểm tra kích thước tổng: 2 + 1 + 3 = 6 bytes
	if len(result) != 6 {
		t.Fatalf("expected 6 bytes, got %d", len(result))
	}

	// Đảm bảo không xảy ra cơ chế cấp phát lại (reallocate) mảng nhớ
	if cap(result) != 64 {
		t.Fatal("FrameInto reallocated — should reuse dst when cap is sufficient")
	}

	// Xác thực sâu nội dung từng byte dữ liệu bên trong buffer dùng chung
	lengthField := binary.BigEndian.Uint16(result[0:2])
	if lengthField != 4 { // Opcode(1) + Payload(3) = 4
		t.Fatalf("unexpected length field: got %d", lengthField)
	}
	if result[2] != 0x05 {
		t.Fatalf("unexpected opcode: got %02X", result[2])
	}
	if result[3] != 0x01 || result[4] != 0x02 || result[5] != 0x03 {
		t.Fatalf("unexpected data contents in dst array")
	}
}

// TestFrameIntoGrowsWhenNeeded kiểm tra xem FrameInto có tự động kích hoạt
// cơ chế tạo mảng mới an toàn khi bộ đệm dst truyền vào bị thiếu dung lượng hay không.
func TestFrameIntoGrowsWhenNeeded(t *testing.T) {
	dst := make([]byte, 0, 2) // Quá nhỏ để chứa gói tin
	payload := []byte{0x01, 0x02, 0x03}
	result := broadcast.FrameInto(dst, 0x05, payload)
	if len(result) != 6 {
		t.Fatalf("expected 6 bytes after grow, got %d", len(result))
	}
}

// ─── S2C TYPED BUILDERS CORRECTNESS (BuildXxx) ───────────────────────────────

func TestBuildError(t *testing.T) {
	pkt := broadcast.BuildError(2, "db error")
	if len(pkt) < 3 {
		t.Fatal("packet too short")
	}
	// Đã sửa: Sử dụng hằng số Opcode tập trung để tránh magic number
	if pkt[2] != broadcast.OpcodeError {
		t.Fatalf("expected error opcode, got %02X", pkt[2])
	}
	errCode := binary.BigEndian.Uint16(pkt[3:5])
	if errCode != 2 {
		t.Fatalf("expected error code 2, got %d", errCode)
	}
}

// TestBuildSuccess gia cố kiểm thử chiều sâu nội dung payload theo yêu cầu của kỹ sư.
func TestBuildSuccess(t *testing.T) {
	pkt := broadcast.BuildSuccess("registered!")

	if pkt[2] != broadcast.OpcodeSuccess {
		t.Fatalf("unexpected opcode, got %02X", pkt[2])
	}

	// Đọc trường độ dài chuỗi Text (2 bytes đầu của Payload)
	msgLen := binary.BigEndian.Uint16(pkt[3:5])
	if int(msgLen) != len("registered!") {
		t.Fatalf("unexpected message length: expected %d, got %d", len("registered!"), msgLen)
	}

	// Đọc chuỗi text phía sau trường độ dài
	if string(pkt[5:]) != "registered!" {
		t.Fatalf("unexpected message content: got %q", string(pkt[5:]))
	}
}

func TestBuildSpawnEntity(t *testing.T) {
	p := broadcast.SpawnPayload{
		EntityID: 42,
		Type:     1, // Monster
		MapID:    1,
		X:        30,
		Z:        40,
		Name:     "Bandit",
	}
	pkt := broadcast.BuildSpawnEntity(p)
	if pkt[2] != broadcast.OpcodeSpawnEntity {
		t.Fatalf("expected spawn opcode, got %02X", pkt[2])
	}
	entityID := binary.BigEndian.Uint64(pkt[3:11])
	if entityID != 42 {
		t.Fatalf("expected EntityID 42, got %d", entityID)
	}
}

// TestBuildDespawnEntity bổ sung bài test bị thiếu cho hàm DespawnEntity.
func TestBuildDespawnEntity(t *testing.T) {
	p := broadcast.DespawnPayload{
		EntityID: 999,
	}
	pkt := broadcast.BuildDespawnEntity(p)

	if pkt[2] != broadcast.OpcodeDespawnEntity {
		t.Fatalf("unexpected despawn opcode, got %02X", pkt[2])
	}

	id := binary.BigEndian.Uint64(pkt[3:11])
	if id != 999 {
		t.Fatalf("unexpected entity id, expected 999 got %d", id)
	}
}

func TestBuildPositionSync(t *testing.T) {
	p := broadcast.PositionSyncPayload{EntityID: 7, X: 15, Z: 25}
	pkt := broadcast.BuildPositionSync(p)
	if pkt[2] != broadcast.OpcodePositionSync {
		t.Fatalf("expected position sync opcode, got %02X", pkt[2])
	}
}

func TestBuildStatsSync(t *testing.T) {
	p := broadcast.StatsSyncPayload{EntityID: 3, HP: 80, MaxHP: 100}
	pkt := broadcast.BuildStatsSync(p)
	if pkt[2] != broadcast.OpcodeStatsSync {
		t.Fatalf("expected stats sync opcode, got %02X", pkt[2])
	}
}

func TestBuildChatMessage(t *testing.T) {
	p := broadcast.ChatPayload{Channel: 2, SenderName: "Hero", Message: "hello world"}
	pkt := broadcast.BuildChatMessage(p)
	if pkt[2] != broadcast.OpcodeChat {
		t.Fatalf("expected chat opcode, got %02X", pkt[2])
	}
}

// ─── S2C TYPED WRITERS CORRECTNESS (WriteXxx - Contents) ─────────────────────
//
// Đảm bảo viết đầy đủ Unit Test xác thực tính chính xác dữ liệu cho toàn bộ các hàm WriteXxx.

func TestWriteError(t *testing.T) {
	dst := make([]byte, 0, 128)
	result := broadcast.WriteError(dst, 500, "internal error")
	if result[2] != broadcast.OpcodeError {
		t.Fatalf("unexpected opcode: %02X", result[2])
	}
	errCode := binary.BigEndian.Uint16(result[3:5])
	if errCode != 500 {
		t.Fatalf("expected error code 500, got %d", errCode)
	}
}

func TestWriteSuccess(t *testing.T) {
	dst := make([]byte, 0, 128)
	result := broadcast.WriteSuccess(dst, "done")
	if result[2] != broadcast.OpcodeSuccess {
		t.Fatalf("unexpected opcode: %02X", result[2])
	}
	msgLen := binary.BigEndian.Uint16(result[3:5])
	if int(msgLen) != len("done") {
		t.Fatalf("length mismatch")
	}
}

func TestWriteSpawnEntity(t *testing.T) {
	dst := make([]byte, 0, 256)
	p := broadcast.SpawnPayload{EntityID: 10, Type: 0, MapID: 1, X: 5, Z: 5, Name: "Player"}
	result := broadcast.WriteSpawnEntity(dst, p)
	if result[2] != broadcast.OpcodeSpawnEntity {
		t.Fatalf("unexpected opcode: %02X", result[2])
	}
	id := binary.BigEndian.Uint64(result[3:11])
	if id != 10 {
		t.Fatalf("expected ID 10, got %d", id)
	}
}

func TestWriteDespawnEntity(t *testing.T) {
	dst := make([]byte, 0, 64)
	p := broadcast.DespawnPayload{EntityID: 55}
	result := broadcast.WriteDespawnEntity(dst, p)
	if result[2] != broadcast.OpcodeDespawnEntity {
		t.Fatalf("unexpected opcode: %02X", result[2])
	}
	id := binary.BigEndian.Uint64(result[3:11])
	if id != 55 {
		t.Fatalf("expected ID 55, got %d", id)
	}
}

func TestWritePositionSync(t *testing.T) {
	dst := make([]byte, 0, 128)
	p := broadcast.PositionSyncPayload{EntityID: 99, X: 100, Z: 200}
	result := broadcast.WritePositionSync(dst, p)

	if len(result) != 19 { // 2 + 1 + 16 = 19 bytes
		t.Fatalf("expected 19 bytes, got %d", len(result))
	}
	if result[2] != broadcast.OpcodePositionSync {
		t.Fatalf("expected opcode %02X, got %02X", broadcast.OpcodePositionSync, result[2])
	}
}

func TestWriteStatsSync(t *testing.T) {
	dst := make([]byte, 0, 128)
	p := broadcast.StatsSyncPayload{EntityID: 1, HP: 50, MaxHP: 100, MP: 20, MaxMP: 50, Dam: 10, Level: 1}
	result := broadcast.WriteStatsSync(dst, p)
	if result[2] != broadcast.OpcodeStatsSync {
		t.Fatalf("unexpected opcode: %02X", result[2])
	}
	hp := binary.BigEndian.Uint32(result[11:15])
	if int32(hp) != 50 {
		t.Fatalf("expected HP 50, got %d", hp)
	}
}

func TestWriteChatMessage(t *testing.T) {
	dst := make([]byte, 0, 256)
	p := broadcast.ChatPayload{Channel: 1, SenderName: "GM", Message: "Maintenance"}
	result := broadcast.WriteChatMessage(dst, p)

	if result[2] != broadcast.OpcodeChat {
		t.Fatalf("expected chat opcode, got %02X", result[2])
	}
	senderName := string(result[5 : 5+len(p.SenderName)])
	if senderName != p.SenderName {
		t.Fatalf("expected sender %q, got %q", p.SenderName, senderName)
	}
}

// ─── STRICT ZERO-ALLOCATION ASSERTIONS (AllocsPerRun) ───────────────────────
//
// Sử dụng công cụ testing.AllocsPerRun của hệ thống Go core để cưỡng ép
// kiểm tra việc tối ưu hóa bộ nhớ RAM. Cấm phát sinh allocation trên hot-path.

func TestWriteErrorZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 512)
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WriteError(dst[:0], 404, "Not Found")
	})
	if allocs != 0 {
		t.Fatalf("WriteError leaked %f allocations per operation", allocs)
	}
}

func TestWriteSuccessZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 512)
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WriteSuccess(dst[:0], "Success")
	})
	if allocs != 0 {
		t.Fatalf("WriteSuccess leaked %f allocations per operation", allocs)
	}
}

func TestWriteSpawnEntityZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 512)
	p := broadcast.SpawnPayload{EntityID: 1, Type: 1, MapID: 100, X: 50, Z: 50, Name: "Mob"}
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WriteSpawnEntity(dst[:0], p)
	})
	if allocs != 0 {
		t.Fatalf("WriteSpawnEntity leaked %f allocations per operation", allocs)
	}
}

func TestWriteDespawnEntityZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 256)
	p := broadcast.DespawnPayload{EntityID: 123}
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WriteDespawnEntity(dst[:0], p)
	})
	if allocs != 0 {
		t.Fatalf("WriteDespawnEntity leaked %f allocations per operation", allocs)
	}
}

func TestWritePositionSyncZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 256)
	p := broadcast.PositionSyncPayload{EntityID: 1, X: 10, Z: 20}
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WritePositionSync(dst[:0], p)
	})
	if allocs != 0 {
		t.Fatalf("WritePositionSync leaked %f allocations per operation", allocs)
	}
}

func TestWriteStatsSyncZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 512)
	p := broadcast.StatsSyncPayload{EntityID: 5, HP: 80, MaxHP: 100, MP: 50, MaxMP: 50, Dam: 12, Level: 10}
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WriteStatsSync(dst[:0], p)
	})
	if allocs != 0 {
		t.Fatalf("WriteStatsSync leaked %f allocations per operation", allocs)
	}
}

func TestWriteChatMessageZeroAlloc(t *testing.T) {
	dst := make([]byte, 0, 1024)
	p := broadcast.ChatPayload{Channel: 0, SenderName: "Player", Message: "Hello Game"}
	allocs := testing.AllocsPerRun(500, func() {
		dst = broadcast.WriteChatMessage(dst[:0], p)
	})
	if allocs != 0 {
		t.Fatalf("WriteChatMessage leaked %f allocations per operation", allocs)
	}
}

// ─── BENCHMARKS ───────────────────────────────────────────────────────────────

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

func BenchmarkWritePositionSync(b *testing.B) {
	dst := make([]byte, 0, 512)
	p := broadcast.PositionSyncPayload{EntityID: 42, X: 30, Z: 50}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = broadcast.WritePositionSync(dst[:0], p)
	}
}

// BenchmarkWriteStatsSync bổ sung bài đo kiểm hiệu năng hot-path cho StatsSync.
func BenchmarkWriteStatsSync(b *testing.B) {
	dst := make([]byte, 0, 512)
	p := broadcast.StatsSyncPayload{EntityID: 9, HP: 500, MaxHP: 500, MP: 100, MaxMP: 100, Dam: 88, Level: 50}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = broadcast.WriteStatsSync(dst[:0], p)
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

// BenchmarkBroadcastToNeighborsPeakGo measures the core packet encoding used by
// BroadcastToNeighbors hot-path (WritePositionSync with pre-allocated dst buffer).
func BenchmarkBroadcastToNeighborsPeakGo(b *testing.B) {
	dst := make([]byte, 0, 512)
	p := broadcast.PositionSyncPayload{EntityID: 42, X: 30, Z: 50}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = broadcast.WritePositionSync(dst[:0], p)
	}
}
