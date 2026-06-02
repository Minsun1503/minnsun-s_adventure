package game

import (
	"net"
	"server/ecs"
	"server/peakgo/codec"
	"server/peakgo/gmath"
	"server/peakgo/loggate"
	"server/peakgo/netio"
	"server/protocol"
	"server/world"
)

// ─── STATIC NOTICE BUFFERS ───────────────────────────────────────────────────
//
// Phòng chống DoS & Spam: Đóng đinh sẵn các mảng thông báo va chạm hệ thống.
// Loại bỏ hoàn toàn việc gọi fmt.Sprintf sinh rác vùng nhớ khi người chơi cố tình spam di chuyển vào tường.
var (
	staticOutOfBoundsNotice = []byte("Movement rejected! Out of bounds.\r\n")
	staticCollisionNotice   = []byte("Collision Alert: Path is blocked by a solid obstacle!\r\n")
	staticAntiCheatNotice   = []byte("Movement rejected! Teleport/Speed hack detected.\r\n")
)

// ─── NETWORK GATEWAY INTERFACE LAYER ─────────────────────────────────────────

// HandlePlayerMovementSystem bóc tách payload nhị phân chứa tọa độ mục tiêu X và Z của Client.
// Layout gói tin Client gửi lên: [X (int32 - BE)] [Z (int32 - BE)] (Đúng chuẩn 8 bytes)
func HandlePlayerMovementSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	// Tận dụng hot-path decoder của codec: Xác thực độ dài và giải mã trọn vẹn trong một nốt nhạc
	p, ok := codec.ReadMovePayload(payload)
	if !ok {
		return "Error: Invalid movement payload length. Expected 8 bytes.\r\n", false
	}

	if !MovementSystem(playerID, int(p.X), int(p.Z)) {
		return "Error: Movement validation rejected.\r\n", false
	}
	return "", true
}

// SendNoticeSystem đẩy trực tiếp mảng byte thông báo xuống cổng kết nối socket của thực thể.
func SendNoticeSystem(entity ecs.Entity, data []byte) {
	conn, ok := ecs.GlobalRegistry.GetConnection(entity)
	if ok && conn.Conn != nil {
		writeConn(conn.Conn, data)
	}
}

// writeConn là điểm xả dữ liệu duy nhất cho toàn bộ luồng lưu lượng TCP outbound.
func writeConn(c net.Conn, data []byte) {
	if c == nil {
		return
	}
	// Sử dụng triệt để Netio đã tối ưu vòng lặp Partial Write và cấu hình Write Deadline bảo vệ Thread
	if err := netio.WritePacket(c, data); err != nil {
		c.Close()
	}
}

// ─── CORE BUSINESS MOVEMENT SYSTEM (ANTI-CHEAT & ZERO-ALLOC) ─────────────────

// MovementSystem chịu trách nhiệm thẩm định điều kiện, đồng bộ hóa tọa độ
// và phát sóng nhị phân trạng thái dịch chuyển của thực thể.
func MovementSystem(entity ecs.Entity, x, z int) bool {
	// Hàng rào bảo vệ 1: Kiểm thử biên bản đồ game [0, 100] siêu tốc qua gmath
	if !gmath.InBounds(x, z, 0, 100) {
		SendNoticeSystem(entity, staticOutOfBoundsNotice)
		return false
	}

	registry := ecs.GlobalRegistry
	pos, ok := registry.GetPosition(entity)
	if !ok {
		return false
	}

	// Hàng rào bảo vệ 2 (VÁ LỖI CHÍ MẠNG): Chống dịch chuyển tức thời (Anti-Teleport Hack).
	// Ép buộc khoảng cách di chuyển giữa tick cũ và tick mới không được vượt quá giới hạn cấu hình (Max 2 ô/tick).
	// Tận dụng hàm hệ số nguyên thuần túy gmath.InRangeInt, không dính bất kỳ một phép cast float nào.
	const MaxMoveDistance = 2
	if !gmath.InRangeInt(pos.X, pos.Z, x, z, MaxMoveDistance) {
		SendNoticeSystem(entity, staticAntiCheatNotice)
		return false
	}

	// Hàng rào bảo vệ 3: Kiểm tra va chạm vật cản địa hình từ dữ liệu bản đồ
	if world.IsTileBlocked(pos.MapID, x, z) {
		SendNoticeSystem(entity, staticCollisionNotice)
		return false
	}

	// Cập nhật trạng thái nguyên tử: Đồng bộ đồng thời cả RAM Registry lẫn Bản đồ không gian SpatialGrid
	pos.X = x
	pos.Z = z
	registry.SetPosition(entity, pos)
	world.GlobalSpatialGrid.UpdateEntityPosition(entity, pos)

	// Phát sóng gói tin dịch chuyển nhị phân tối ưu xuống toàn bộ bản đồ
	broadcastBinaryMovement(entity, pos)

	return true
}

// ─── PROTOCOL BINARY ENCODING HELPER ─────────────────────────────────────────

// broadcastBinaryMovement thực hiện đóng gói nhị phân không cấp phát và phát sóng diện rộng.
func broadcastBinaryMovement(entity ecs.Entity, pos ecs.PositionComponent) {
	// Bước đi tối cao: Mượn một bộ đệm nhị phân sạch từ kiến trúc lõi netio DefaultPool (1KB)
	pBuf := netio.DefaultPool.Get()
	defer netio.DefaultPool.Put(pBuf)

	// Khai báo kích thước chính xác cho khung gói tin phát sóng:
	// Khung packet = [Length uint16 (2B)] + [Opcode uint8 (1B)] + [Payload (16B)] -> Tổng 19 bytes.
	// Chi tiết Payload: [EntityID uint64 (8B)] + [X int32 (4B)] + [Z int32 (4B)]
	buf := (*pBuf)[:19]

	const OpcodePlayerMoved uint8 = 2 // Mã Opcode định danh sự kiện thực thể di chuyển

	// Ghi trực tiếp lên vùng nhớ con trỏ thông qua package codec, triệt tiêu overhead dịch chuỗi
	codec.WriteUint16(buf[0:2], 17) // Độ dài Payload = 1 byte Opcode + 16 bytes Data dữ liệu = 17
	codec.WriteUint8(buf[2:3], OpcodePlayerMoved)
	codec.WriteUint64(buf[3:11], uint64(entity))
	codec.WriteInt32(buf[11:15], int32(pos.X))
	codec.WriteInt32(buf[15:19], int32(pos.Z))

	// Phát sóng theo vùng AOI: chỉ gửi gói tin đến các thực thể lân cận, triệt tiêu O(N)
	protocol.BroadcastToNeighbors(pos, buf, entity)

	// Đạt mốc 0 alloc hoàn hảo cho hệ thống Debug: Chỉ truy vết Metadata khi chế độ Debug thực sự bật
	if loggate.DebugEnabled() {
		if meta, ok := ecs.GlobalRegistry.GetMetadata(entity); ok {
			loggate.Debugf("[MOVEMENT] %s → (%d, %d) on Map %d", meta.Name, pos.X, pos.Z, pos.MapID)
		}
	}
}
