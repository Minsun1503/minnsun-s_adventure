package models

import (
	"encoding/json"
	"fmt"
	"os"
	"server/ecs"
	"server/logger"
	"server/peakgo/timer" // Tích hợp hệ thống quản lý thời gian vòng lặp lõi
	"sync"
)

// MonsterTemplate định nghĩa cấu trúc dữ liệu tĩnh (Read-Only) được tải lên từ file cấu hình JSON.
type MonsterTemplate struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	HP             int     `json:"hp"`
	Dam            int     `json:"damage"`
	MapID          int     `json:"map_id"` // Bản đồ mặc định, tự động quy về 1 nếu trống
	SpawnX         int     `json:"spawn_x"`
	SpawnZ         int     `json:"spawn_z"`
	RoamRadius     int     `json:"roam_radius"`
	AggroRadius    float64 `json:"aggro_radius"`
	AttackCooldown int     `json:"attack_cooldown"`
	XPReward       uint64  `json:"xp_reward"`

	// Đã sửa: Đưa tầm đánh vào cấu hình JSON theo khuyến nghị của kỹ sư trưởng.
	// Sử dụng kiểu int để đồng bộ với hệ thống gmath tính toán khoảng cách số nguyên siêu tốc.
	MeleeRange int `json:"melee_range"`
}

// templateStore đóng vai trò là registry trong bộ nhớ RAM lưu trữ các bản mẫu quái vật tĩnh.
var (
	templateStore   = make(map[int]MonsterTemplate)
	templateStoreMu sync.RWMutex
)

// LoadMonster đọc file JSON cấu hình quái vật và nạp vào templateStore khi server khởi động.
func LoadMonster(filePath string) ([]MonsterTemplate, error) {
	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read monster config: %w", err)
	}

	var list []MonsterTemplate
	if err := json.Unmarshal(fileBytes, &list); err != nil {
		return nil, fmt.Errorf("failed to parse monster config: %w", err)
	}

	templateStoreMu.Lock()
	for i := range list {
		// Thiết lập giá trị mặc định nếu file JSON bỏ trống trường dữ liệu
		if list[i].MapID == 0 {
			list[i].MapID = 1
		}
		// Dự phòng: Nếu tầm đánh trong cấu hình bằng 0, tự động đưa về mốc mặc định là 2 ô
		if list[i].MeleeRange == 0 {
			list[i].MeleeRange = 2
		}
		templateStore[list[i].ID] = list[i]
	}
	templateStoreMu.Unlock()

	return list, nil
}

// GetTemplate truy xuất bản mẫu quái vật tĩnh từ ID cấu hình.
func GetTemplate(templateID int) (MonsterTemplate, bool) {
	templateStoreMu.RLock()
	defer templateStoreMu.RUnlock()
	t, ok := templateStore[templateID]
	return t, ok
}

// SpawnMonsterFromTemplate khởi tạo một thực thể quái vật sống (Live Entity) vào thế giới game.
//
// Đã sửa: Đồng bộ hoàn toàn dữ liệu với hợp đồng AI mới (Chuyển đổi bán kính xích quái sang số nguyên int,
// nạp cấu hình MeleeRange động từ Template và khởi tạo các bộ đếm TickTimer bảo vệ CPU).
func SpawnMonsterFromTemplate(templateID, mapID, spawnX, spawnZ int) (ecs.Entity, error) {
	t, ok := GetTemplate(templateID)
	if !ok {
		return 0, fmt.Errorf("template ID %d not found — did you call LoadMonster?", templateID)
	}

	// Sinh ID thực thể duy nhất từ bộ đếm nguyên tử của ECS Registry
	id := ecs.GlobalRegistry.NewEntity()

	ecs.GlobalRegistry.SetMetadata(id, ecs.MetadataComponent{
		Name: t.Name,
		Type: ecs.EntityMonster,
	})

	spawnPos := ecs.PositionComponent{MapID: mapID, X: spawnX, Z: spawnZ}
	ecs.GlobalRegistry.SetPosition(id, spawnPos)

	ecs.GlobalRegistry.SetStats(id, ecs.StatsComponent{
		HP:    t.HP,
		MaxHP: t.HP,
		Dam:   t.Dam,
	})

	// Tối ưu hóa: Tính toán tầm xích quái (Leash Radius = Bán kính kích động * 2)
	// và ép về kiểu int thô để phục vụ hàm gmath.InRangeInt trên hot-path.
	leashRadius := int(t.AggroRadius * 2.0)

	// Khởi tạo cấu hình FSM AI đồng bộ 100% với kiến trúc ai_roaming.go mới
	ecs.GlobalRegistry.SetAI(id, ecs.AIComponent{
		State:       ecs.AIStateIdle,
		SpawnX:      spawnX,
		SpawnZ:      spawnZ,
		SpawnRadius: t.RoamRadius,
		AggroRadius: t.AggroRadius,
		LeashRadius: leashRadius,  // Đã đồng bộ sang int
		MeleeRange:  t.MeleeRange, // Đã cấu hình động từ JSON template

		// Khởi tạo các cỗ máy đếm thời gian chuyên dụng của PeakGo thay vì cộng dồn biến int thủ công
		AttackTimer: timer.NewTickTimer(t.AttackCooldown),
		IdleTimer:   timer.NewTickTimer(8), // Thả lỏng quái đứng nghỉ 2 giây (8 ticks với tickrate 250ms)
		PathTimer:   timer.NewTickTimer(4), // Giới hạn chu kỳ tìm đường A* tối đa 1 giây/lần (4 ticks) để bảo vệ CPU
	})

	logger.Info("[SPAWN] %s (entity %d) at (%d, %d) | HP:%d ATK:%d aggro:%.0f leash:%d melee:%d",
		t.Name, id, spawnX, spawnZ, t.HP, t.Dam, t.AggroRadius, leashRadius, t.MeleeRange)

	return id, nil
}

// GetTemplateByName tìm kiếm bản mẫu quái vật dựa theo tên định danh.
func GetTemplateByName(name string) (MonsterTemplate, bool) {
	templateStoreMu.RLock()
	defer templateStoreMu.RUnlock()
	for _, t := range templateStore {
		if t.Name == name {
			return t, true
		}
	}
	return MonsterTemplate{}, false
}

// SpawnFromDefaultPosition tạo quái vật tại vị trí tọa độ mặc định được khai báo sẵn trong file cấu hình.
func SpawnFromDefaultPosition(templateID int) (ecs.Entity, error) {
	t, ok := GetTemplate(templateID)
	if !ok {
		return 0, fmt.Errorf("template ID %d not found", templateID)
	}
	return SpawnMonsterFromTemplate(templateID, t.MapID, t.SpawnX, t.SpawnZ)
}
