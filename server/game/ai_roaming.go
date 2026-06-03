package game

import (
	"server/ecs"
	"server/peakgo/gmath"
	"server/peakgo/rng"
	"server/peakgo/spatial"
	"server/world"
)

// TickAI là điểm vào duy nhất của mỗi thực thể AI quái vật tại mỗi vòng lặp game loop.
// Toàn bộ logic đếm thời gian được nén gọn qua các lệnh Advance() của TickTimer.
func TickAI(id ecs.Entity) {
	ai, ok := ecs.GlobalRegistry.GetAI(id)
	if !ok {
		return
	}

	// Tịnh tiến toàn bộ bộ đếm thời gian của thực thể lên 1 tick một cách đồng bộ
	ai.AttackTimer.Advance()
	ai.IdleTimer.Advance()
	ai.PathTimer.Advance()

	switch ai.State {
	case ecs.AIStateIdle:
		ai = tickIdle(id, ai)
	case ecs.AIStateRoaming:
		ai = tickRoaming(id, ai)
	case ecs.AIStateChasing:
		ai = tickChasing(id, ai)
	case ecs.AIStateAttacking:
		ai = tickAttacking(id, ai)
	case ecs.AIStateReturning:
		ai = tickReturning(id, ai)
	}

	// Ghi đè trạng thái đột biến ngược trở lại Registry (Copy-Modify-Overwrite)
	ecs.GlobalRegistry.SetAI(id, ai)
}

// ─── STATE HANDLERS ──────────────────────────────────────────────────────────

func tickIdle(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	// 1. Check threat-based aggro: if a player has accumulated threat, chase them
	if ai.ThreatTable != nil && ai.ThreatTable.Len() > 0 {
		if topPlayerID, topThreat := ai.ThreatTable.Top(); topThreat > 0 {
			if targetPos, ok := ecs.GlobalRegistry.GetPosition(ecs.Entity(topPlayerID)); ok {
				if myPos, ok := ecs.GlobalRegistry.GetPosition(id); ok {
					if gmath.InRangeInt(myPos.X, myPos.Z, targetPos.X, targetPos.Z, int(ai.AggroRadius)) {
						ai.TargetID = ecs.Entity(topPlayerID)
						MonsterFSMSend(id, &ai, MonsterEvAggro)
						return ai
					}
				}
			}
		}
	}

	// 2. Fallback: nearest player in aggro range
	if target, found := spatial.GetNearestPlayer(id, ai.AggroRadius); found {
		ai.TargetID = target.ID
		MonsterFSMSend(id, &ai, MonsterEvAggro)
		return ai
	}

	// Tận dụng TickTimer: Tự động báo khi đủ thời gian nghỉ ngơi
	if ai.IdleTimer.Ready() {
		ai.IdleTimer.Reset()

		// Tìm kiếm một điểm đích di chuyển Roaming ngẫu nhiên xung quanh khu vực Spawn
		nextX, nextZ, found := pickRoamTarget(id, ai)
		if found {
			ai.RoamTargetX = nextX
			ai.RoamTargetZ = nextZ
			MonsterFSMSend(id, &ai, MonsterEvTick)
		}
	}

	return ai
}

func tickRoaming(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if target, found := spatial.GetNearestPlayer(id, ai.AggroRadius); found {
		ai.TargetID = target.ID
		MonsterFSMSend(id, &ai, MonsterEvAggro)
		return ai
	}

	pos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		MonsterFSMSend(id, &ai, MonsterEvPathDone)
		return ai
	}

	// Nếu đã chạm chân tới điểm đích Roaming mục tiêu -> Quay về trạng thái Idle nghỉ ngơi
	if pos.X == ai.RoamTargetX && pos.Z == ai.RoamTargetZ {
		MonsterFSMSend(id, &ai, MonsterEvPathDone)
		ai.IdleTimer.Reset()
		return ai
	}

	// Đóng băng Pathfinding (Throttling): Chỉ cho phép AI tìm đường 4 tick/lần (1 giây/lần)
	if ai.PathTimer.Ready() {
		ai.PathTimer.Reset()

		targetPos := ecs.PositionComponent{X: ai.RoamTargetX, Z: ai.RoamTargetZ, MapID: pos.MapID}
		nextX, nextZ := world.FindPath(pos, targetPos)

		// Nếu bị kẹt đường hoặc tile mục tiêu bị chặn đột ngột -> Hủy roam, đứng nghỉ
		if (nextX == pos.X && nextZ == pos.Z) || world.IsTileBlocked(pos.MapID, nextX, nextZ) {
			MonsterFSMSend(id, &ai, MonsterEvPathDone)
			ai.IdleTimer.Reset()
			return ai
		}

		MovementSystem(id, nextX, nextZ)
	}

	return ai
}

func tickChasing(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if ai.TargetID == 0 {
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
		return ai
	}

	// Decay threat each tick so aggro naturally fades
	if ai.ThreatTable != nil {
		ai.ThreatTable.Decay()
		// Switch target if another player has higher threat
		if topID, topThreat := ai.ThreatTable.Top(); topThreat > 0 && ecs.Entity(topID) != ai.TargetID {
			if _, ok := ecs.GlobalRegistry.GetPosition(ecs.Entity(topID)); ok {
				ai.TargetID = ecs.Entity(topID)
			}
		}
	}

	targetPos, ok := ecs.GlobalRegistry.GetPosition(ai.TargetID)
	if !ok {
		ai.TargetID = 0
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
		return ai
	}

	myPos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		return ai
	}

	// ĐÃ VÁ LỖI CHÍ MẠNG (Leash Check): Kiểm tra khoảng cách giữa vị trí hiện tại của quái
	// với điểm xuất phát ban đầu (ai.SpawnX, ai.SpawnZ) thay vì đuổi theo mục tiêu vô hạn!
	// Chạy bằng InRangeInt hệ số nguyên, không dính bất kỳ một phép toán float nào.
	if !gmath.InRangeInt(myPos.X, myPos.Z, ai.SpawnX, ai.SpawnZ, ai.LeashRadius) {
		ai.TargetID = 0
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
		return ai
	}

	// Đug Đã Vá (Melee Range Check): Sử dụng InRangeInt hệ số nguyên sạch sẽ, triệt tiêu magic code
	if gmath.InRangeInt(myPos.X, myPos.Z, targetPos.X, targetPos.Z, ai.MeleeRange) {
		MonsterFSMSend(id, &ai, MonsterEvInRange)
		return ai
	}

	// Chặn nghẽn CPU: Chỉ thực thi thuật toán tìm đường đuổi theo Player theo chu kỳ xả Timer
	if ai.PathTimer.Ready() {
		ai.PathTimer.Reset()
		nextX, nextZ := world.FindPath(myPos, targetPos)
		MovementSystem(id, nextX, nextZ)
	}

	return ai
}

func tickAttacking(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	if ai.TargetID == 0 {
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
		return ai
	}

	targetPos, targetOk := ecs.GlobalRegistry.GetPosition(ai.TargetID)
	myPos, myOk := ecs.GlobalRegistry.GetPosition(id)
	if !targetOk || !myOk {
		ai.TargetID = 0
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
		return ai
	}

	// Nếu người chơi dùng tốc biến hoặc chạy giật lùi thoát khỏi tầm đánh -> Quay lại trạng thái Chasing
	if !gmath.InRangeInt(myPos.X, myPos.Z, targetPos.X, targetPos.Z, ai.MeleeRange) {
		MonsterFSMSend(id, &ai, MonsterEvOutOfRange)
		return ai
	}

	// Sử dụng cấu trúc hợp đồng Ready() nguyên bản của TickTimer
	if !ai.AttackTimer.Ready() {
		return ai
	}

	// Decay threat before attacking so past damagers can overtake
	if ai.ThreatTable != nil {
		ai.ThreatTable.Decay()
	}

	result, errMsg := AttackSystem(id, ai.TargetID)
	if errMsg != "" {
		if ai.ThreatTable != nil {
			ai.ThreatTable.Remove(uint64(ai.TargetID))
		}
		ai.TargetID = 0
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
		return ai
	}

	ai.AttackTimer.Reset() // Reset hồi chiêu đòn đánh về 0 ngay sau khi tung chiêu

	if result.Killed {
		if ai.ThreatTable != nil {
			ai.ThreatTable.Remove(uint64(ai.TargetID))
		}
		ai.TargetID = 0
		MonsterFSMSend(id, &ai, MonsterEvLostTarget)
	}

	return ai
}

func tickReturning(id ecs.Entity, ai ecs.AIComponent) ecs.AIComponent {
	pos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		return ai
	}

	// Check aggro while returning: player may follow monster back
	if ai.ThreatTable != nil && ai.ThreatTable.Len() > 0 {
		if topPlayerID, topThreat := ai.ThreatTable.Top(); topThreat > 0 {
			if targetPos, ok := ecs.GlobalRegistry.GetPosition(ecs.Entity(topPlayerID)); ok {
				if gmath.InRangeInt(pos.X, pos.Z, targetPos.X, targetPos.Z, int(ai.AggroRadius)) {
					ai.TargetID = ecs.Entity(topPlayerID)
					MonsterFSMSend(id, &ai, MonsterEvAggro)
					return ai
				}
			}
		}
	}
	if target, found := spatial.GetNearestPlayer(id, ai.AggroRadius); found {
		ai.TargetID = target.ID
		MonsterFSMSend(id, &ai, MonsterEvAggro)
		return ai
	}

	// Đã lọt về điểm Neo Spawn cũ (bán kính <= 1 ô) -> Reset trạng thái đứng nghỉ Idle
	if gmath.InRangeInt(pos.X, pos.Z, ai.SpawnX, ai.SpawnZ, 1) {
		MonsterFSMSend(id, &ai, MonsterEvAtSpawn)
		ai.IdleTimer.Reset()
		return ai
	}

	// Tìm đường rút lui an toàn theo chu kỳ bảo vệ CPU
	if ai.PathTimer.Ready() {
		ai.PathTimer.Reset()
		spawnPos := ecs.PositionComponent{X: ai.SpawnX, Z: ai.SpawnZ, MapID: pos.MapID}
		nextX, nextZ := world.FindPath(pos, spawnPos)
		MovementSystem(id, nextX, nextZ)
	}

	return ai
}

// ─── INTERNAL HELPERS ────────────────────────────────────────────────────────

// transition quản lý tập trung việc chuyển đổi trạng thái AI.
// Gom toàn bộ logic gán biến và Debug Log rườm rà trước đây vào đúng 1 nguồn sự thật duy nhất.


// pickRoamTarget tính toán tọa độ di chuyển ngẫu nhiên tự nhiên quanh vùng spawn.
func pickRoamTarget(id ecs.Entity, ai ecs.AIComponent) (int, int, bool) {
	pos, ok := ecs.GlobalRegistry.GetPosition(id)
	if !ok {
		return 0, 0, false
	}
	const attempts = 8

	for i := 0; i < attempts; i++ {
		dx := rng.Intn(5) - 2 // Khoảng nhảy di chuyển [-2, +2]
		dz := rng.Intn(5) - 2
		if dx == 0 && dz == 0 {
			continue
		}

		nx := pos.X + dx
		nz := pos.Z + dz

		// Kiểm tra điểm đích mới có nằm ngoài phạm vi bán kính Spawn cho phép không
		if !gmath.InRangeInt(nx, nz, ai.SpawnX, ai.SpawnZ, ai.SpawnRadius) {
			continue
		}
		// Chặn cứng biên bản đồ game [0, 100] bằng một lệnh duy nhất
		if !gmath.InBounds(nx, nz, 0, 100) {
			continue
		}
		// Chặn di chuyển đè lên gạch/vật cản cấu trúc map
		if world.IsTileBlocked(pos.MapID, nx, nz) {
			continue
		}

		return nx, nz, true
	}
	return 0, 0, false
}
