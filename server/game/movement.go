package game

import (
	"server/ecs"
	"server/peakgo/anticheat"
	"server/peakgo/broadcast"
	"server/peakgo/codec"
	"server/peakgo/config"
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

	// After successful movement, process AOI events to detect enter/leave neighbors.
	if pos, ok := ecs.DefaultRegistry.GetPosition(playerID); ok {
		world.ProcessAOIEvents(playerID, pos)
	}
	return "", true
}

// SendNoticeSystem đẩy trực tiếp mảng byte thông báo xuống cổng kết nối socket của thực thể.
func SendNoticeSystem(entity ecs.Entity, data []byte) {
	conn, ok := ecs.DefaultRegistry.GetConnection(entity)
	if ok && conn.Writer != nil {
		frame := broadcast.BuildNotice(broadcast.NoticePayload{Message: string(data)})
		conn.Writer.Send(frame)
	}
}

func SendNoticeBinary(entity ecs.Entity, frame []byte) {
	conn, ok := ecs.DefaultRegistry.GetConnection(entity)
	if ok && conn.Writer != nil {
		conn.Writer.Send(frame)
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

	registry := ecs.DefaultRegistry
	pos, ok := registry.GetPosition(entity)
	if !ok {
		return false
	}

	// Hàng rào bảo vệ 2 (VÁ LỖI CHÍ MẠNG): Chống dịch chuyển tức thời (Anti-Teleport Hack).
	// Sử dụng anticheat.Validator per-connection để kiểm tra khoảng cách di chuyển.
	// Lấy maxMoveDistance từ config (hot-reload) thay vì hardcode.
	cfg := config.C()
	maxMoveDist := int(cfg.MaxMoveDistance)

	if connComp, ok := registry.GetConnection(entity); ok {
		if v, ok2 := connComp.Validator.(*anticheat.Validator); ok2 && v != nil {
			newPos := ecs.PositionComponent{MapID: pos.MapID, X: x, Z: z}
			if !v.ValidateMovement(pos, newPos, maxMoveDist, GetCurrentTick()) {
				SendNoticeSystem(entity, staticAntiCheatNotice)
				return false
			}
		} else {
			// Fallback: no validator (e.g. monster) — use simple distance check
			if !gmath.InRangeInt(pos.X, pos.Z, x, z, maxMoveDist) {
				SendNoticeSystem(entity, staticAntiCheatNotice)
				return false
			}
		}
	} else {
		// No connection — use simple distance check (monster AI pathfinding)
		if !gmath.InRangeInt(pos.X, pos.Z, x, z, maxMoveDist) {
			return false
		}
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
	pBuf := netio.DefaultPool.Get()
	defer netio.DefaultPool.Put(pBuf)

	payload := broadcast.PositionSyncPayload{
		EntityID: uint64(entity),
		X:        int32(pos.X),
		Z:        int32(pos.Z),
	}
	buf := broadcast.WritePositionSync((*pBuf)[:0], payload)

	protocol.BroadcastToNeighbors(pos, buf, entity)

	if loggate.DebugEnabled() {
		if meta, ok := ecs.DefaultRegistry.GetMetadata(entity); ok {
			loggate.Debugf("[MOVEMENT] %s → (%d, %d) on Map %d", meta.Name, pos.X, pos.Z, pos.MapID)
		}
	}
}
