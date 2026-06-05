package spatial_test

import (
	"server/ecs"
	"server/peakgo/spatial"
	"server/world"
	"testing"
)

// ─── UTILITY HELPERS ─────────────────────────────────────────────────────────

// cleanupEntities gom toàn bộ logic dọn dẹp state vào một nơi duy nhất.
// Đảm bảo xóa sạch sẽ cả ở SpatialGrid lẫn ECS Registry, chống rò rỉ bộ nhớ khi chạy test/benchmark.
func cleanupEntities(ids ...ecs.Entity) {
	for _, id := range ids {
		world.GlobalSpatialGrid.RemoveEntity(id)
		ecs.DefaultRegistry.RemoveEntity(id)
	}
}

// setupEntity là helper khởi tạo nhanh một thực thể với vị trí không gian cụ thể.
// Nhận vào testing.TB để hỗ trợ báo lỗi chính xác ngữ cảnh cả trong Test lẫn Benchmark.
func setupEntity(t testing.TB, name string, typ ecs.EntityType, mapID, x, z int) ecs.Entity {
	id := ecs.DefaultRegistry.NewEntity()

	ecs.DefaultRegistry.SetMetadata(id, ecs.MetadataComponent{Name: name, Type: typ})

	pos := ecs.PositionComponent{MapID: mapID, X: x, Z: z}
	ecs.DefaultRegistry.SetPosition(id, pos)
	world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
	return id
}

// ─── CORRECTNESS TESTS ───────────────────────────────────────────────────────

func TestGetNearestPlayerFindsClosest(t *testing.T) {
	world.InitializeCollisionMaps()

	monster := setupEntity(t, "Orc", ecs.EntityMonster, 1, 50, 50)
	near := setupEntity(t, "NearPlayer", ecs.EntityPlayer, 1, 52, 50) // khoảng cách = 2
	far := setupEntity(t, "FarPlayer", ecs.EntityPlayer, 1, 60, 50)   // khoảng cách = 10
	defer cleanupEntities(monster, near, far)

	result, found := spatial.GetNearestPlayer(monster, 15.0)
	if !found {
		t.Fatal("expected to find a nearby player")
	}
	if result.ID != near {
		t.Errorf("expected nearest player to be NearPlayer (id %d), got id %d", near, result.ID)
	}
}

// TestGetNearestMonster bổ sung bài kiểm thử bị thiếu cho hàm GetNearestMonster.
func TestGetNearestMonster(t *testing.T) {
	world.InitializeCollisionMaps()

	player := setupEntity(t, "Hero", ecs.EntityPlayer, 1, 10, 10)
	nearMob := setupEntity(t, "Slime", ecs.EntityMonster, 1, 11, 10) // khoảng cách = 1
	farMob := setupEntity(t, "Dragon", ecs.EntityMonster, 1, 25, 10) // khoảng cách = 15
	defer cleanupEntities(player, nearMob, farMob)

	result, found := spatial.GetNearestMonster(player, 5.0)
	if !found {
		t.Fatal("expected to find a nearby monster")
	}
	if result.ID != nearMob {
		t.Errorf("expected nearest monster to be Slime, got %v", result.ID)
	}
}

func TestGetNearestReturnsNothingOutOfRange(t *testing.T) {
	monster := setupEntity(t, "Orc2", ecs.EntityMonster, 1, 10, 10)
	player := setupEntity(t, "Player2", ecs.EntityPlayer, 1, 90, 90) // Quá xa
	defer cleanupEntities(monster, player)

	_, found := spatial.GetNearestPlayer(monster, 5.0)
	if found {
		t.Error("expected no player within radius 5 when player is at (90,90)")
	}
}

func TestCountInRadiusAndSemanticWrappers(t *testing.T) {
	origin := setupEntity(t, "Origin", ecs.EntityMonster, 1, 30, 30)
	p1 := setupEntity(t, "P1", ecs.EntityPlayer, 1, 31, 30)  // khoảng cách = 1
	p2 := setupEntity(t, "P2", ecs.EntityPlayer, 1, 32, 30)  // khoảng cách = 2
	m1 := setupEntity(t, "M1", ecs.EntityMonster, 1, 33, 30) // khoảng cách = 3
	defer cleanupEntities(origin, p1, p2, m1)

	// Đã sửa: Kiểm tra song song cả hàm Generic và hàm ngữ nghĩa (Semantic API) mới
	playerCount := spatial.CountInRadius(origin, 5.0, ecs.EntityPlayer)
	if playerCount != 2 {
		t.Errorf("expected 2 players, got %d", playerCount)
	}
	if spatial.CountPlayersInRadius(origin, 5.0) != 2 {
		t.Error("semantic CountPlayersInRadius failed")
	}

	monsterCount := spatial.CountInRadius(origin, 5.0, ecs.EntityMonster)
	if monsterCount != 1 { // Không tính chính nó (originID bị loại trừ tự động từ grid)
		t.Errorf("expected 1 monster, got %d", monsterCount)
	}
	if spatial.CountMonstersInRadius(origin, 5.0) != 1 {
		t.Error("semantic CountMonstersInRadius failed")
	}

	allCount := spatial.CountInRadius(origin, 5.0, ecs.EntityAny)
	if allCount != 3 {
		t.Errorf("expected 3 total entities, got %d", allCount)
	}
}

func TestIsAnyInRadiusAndSemanticWrappers(t *testing.T) {
	origin := setupEntity(t, "Check", ecs.EntityMonster, 1, 20, 20)
	player := setupEntity(t, "Nearby", ecs.EntityPlayer, 1, 21, 20)
	defer cleanupEntities(origin, player)

	if !spatial.IsAnyInRadius(origin, 3.0, ecs.EntityPlayer) {
		t.Error("expected player within radius 3")
	}
	if !spatial.HasPlayerInRadius(origin, 3.0) {
		t.Error("semantic HasPlayerInRadius failed")
	}

	if spatial.IsAnyInRadius(origin, 0.5, ecs.EntityPlayer) {
		t.Error("expected no player within radius 0.5")
	}
}

// TestFilterInRadius bổ sung bài kiểm thử hoàn chỉnh cho hàm FilterInRadius công khai.
func TestFilterInRadius(t *testing.T) {
	origin := setupEntity(t, "Center", ecs.EntityPlayer, 1, 0, 0)
	m1 := setupEntity(t, "Mob1", ecs.EntityMonster, 1, 2, 2)
	m2 := setupEntity(t, "Mob2", ecs.EntityMonster, 1, 3, 3)
	p1 := setupEntity(t, "Friend", ecs.EntityPlayer, 1, 1, 1)
	defer cleanupEntities(origin, m1, m2, p1)

	// Lọc danh sách Monster
	var targets []ecs.Entity
	targets = spatial.FilterInRadius(origin, 5.0, ecs.EntityMonster, targets)
	if len(targets) != 2 {
		t.Fatalf("expected 2 monsters in slice, got %d", len(targets))
	}

	// Đảm bảo kết quả chứa đúng thực thể
	if targets[0] != m1 && targets[1] != m1 {
		t.Error("missing Mob1 in filtered results")
	}
}

// TestIsInRadius kiểm tra tính năng đo lường nhanh quan hệ khoảng cách Boolean giữa 2 mục tiêu.
func TestIsInRadius(t *testing.T) {
	a := setupEntity(t, "A", ecs.EntityPlayer, 1, 10, 10)
	b := setupEntity(t, "B", ecs.EntityMonster, 1, 13, 14) // khoảng cách hình học = 5
	defer cleanupEntities(a, b)

	if !spatial.IsInRadius(a, b, 5.1) {
		t.Error("expected entities to be in radius 5.1")
	}
	if spatial.IsInRadius(a, b, 4.9) {
		t.Error("entities should not be within radius 4.9")
	}
}

func TestDistanceBetween(t *testing.T) {
	a := setupEntity(t, "A", ecs.EntityPlayer, 1, 0, 0)
	b := setupEntity(t, "B", ecs.EntityPlayer, 1, 3, 4) // tam giác Ai Cập 3-4-5 -> bình phương = 25
	defer cleanupEntities(a, b)

	dsq, ok := spatial.DistanceBetween(a, b)
	if !ok {
		t.Fatal("DistanceBetween returned ok=false")
	}
	if dsq != 25 {
		t.Fatalf("expected 25 (3-4-5 triangle), got %d", dsq)
	}
}

func TestSameMap(t *testing.T) {
	a := setupEntity(t, "MapA1", ecs.EntityPlayer, 1, 5, 5)
	b := setupEntity(t, "MapA2", ecs.EntityPlayer, 1, 6, 6)
	c := setupEntity(t, "MapB1", ecs.EntityPlayer, 2, 5, 5)
	defer cleanupEntities(a, b, c)

	if !spatial.SameMap(a, b) {
		t.Error("a and b should be on the same map")
	}
	if spatial.SameMap(a, c) {
		t.Error("a and c should be on different maps")
	}
}

// ─── EDGE CASES (ROBUSTNESS TESTS) ───────────────────────────────────────────

// TestEdgeCasesNonExistentEntities kiểm thử khả năng phòng vệ của thư viện
// khi truyền vào các Entity ID rác hoặc đã bị hủy. Hệ thống phải trả về giá trị mặc định an toàn.
func TestEdgeCasesNonExistentEntities(t *testing.T) {
	invalidID := ecs.Entity(999999)

	if _, found := spatial.GetNearestPlayer(invalidID, 10.0); found {
		t.Error("GetNearestPlayer should fail for non-existent origin")
	}

	if count := spatial.CountInRadius(invalidID, 10.0, ecs.EntityAny); count != 0 {
		t.Errorf("CountInRadius should return 0, got %d", count)
	}

	if any := spatial.IsAnyInRadius(invalidID, 10.0, ecs.EntityAny); any {
		t.Error("IsAnyInRadius should return false")
	}

	if _, ok := spatial.DistanceBetween(invalidID, invalidID); ok {
		t.Error("DistanceBetween should report failure (false) for invalid IDs")
	}
}

// ─── ZERO-ALLOCATION GUARANTEE CONTRACTS ─────────────────────────────────────

// TestSpatialZeroAllocations khóa chặt cam kết hiệu năng: Không phát sinh thêm
// bất kỳ một lượt cấp phát bộ nhớ RAM nào trên Heap khi vận hành các hàm tiện ích không gian.
func TestSpatialZeroAllocations(t *testing.T) {
	origin := setupEntity(t, "Center", ecs.EntityPlayer, 1, 10, 10)
	target := setupEntity(t, "Target", ecs.EntityMonster, 1, 11, 11)
	defer cleanupEntities(origin, target)

	// Đảm bảo SpatialGrid nội bộ đã được khởi động và phân bố vùng đệm
	_ = spatial.CountInRadius(origin, 10.0, ecs.EntityMonster)

	allocs := testing.AllocsPerRun(1000, func() {
		_ = spatial.CountInRadius(origin, 10.0, ecs.EntityMonster)
	})
	if allocs > 0 {
		t.Fatalf("CountInRadius violated zero-alloc contract: got %f allocations", allocs)
	}

	allocs = testing.AllocsPerRun(1000, func() {
		_, _ = spatial.GetNearestMonster(origin, 10.0)
	})
	if allocs > 0 {
		t.Fatalf("GetNearestMonster violated zero-alloc contract: got %f allocations", allocs)
	}
}

// ─── WORKLOAD SCALING BENCHMARKS ───────────────────────────────────────────────

// runSpatialBenchmark là hàm lõi để thiết lập ma trận kiểm thử tải thực tế cho MMORPG.
func runSpatialBenchmark(b *testing.B, entityCount int) {
	world.InitializeCollisionMaps()

	monster := setupEntity(b, "BenchOrc", ecs.EntityMonster, 1, 50, 50)
	players := make([]ecs.Entity, entityCount)
	for i := range players {
		// Phân bố người chơi rải rác xung quanh vị trí quái vật
		players[i] = setupEntity(b, "P", ecs.EntityPlayer, 1, 50+(i%20), 50+(i/20))
	}

	// Đã sửa: Sử dụng cơ chế dọn dẹp state sạch hoàn toàn cả registry lẫn grid
	defer func() {
		cleanupEntities(monster)
		cleanupEntities(players...)
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = spatial.CountInRadius(monster, 20.0, ecs.EntityPlayer)
	}
}

func BenchmarkCountInRadius_Scale10(b *testing.B) {
	runSpatialBenchmark(b, 10)
}

func BenchmarkCountInRadius_Scale100(b *testing.B) {
	runSpatialBenchmark(b, 100)
}

func BenchmarkCountInRadius_Scale500(b *testing.B) {
	runSpatialBenchmark(b, 500)
}

func BenchmarkGetNearestMonster_Scale100(b *testing.B) {
	world.InitializeCollisionMaps()
	player := setupEntity(b, "Hero", ecs.EntityPlayer, 1, 50, 50)
	monsters := make([]ecs.Entity, 100)
	for i := range monsters {
		monsters[i] = setupEntity(b, "M", ecs.EntityMonster, 1, 50+(i%10), 50+(i/10))
	}
	defer func() {
		cleanupEntities(player)
		cleanupEntities(monsters...)
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = spatial.GetNearestMonster(player, 30.0)
	}
}

// BenchmarkCountInRadiusPeakGo measures CountInRadius hot-path with fixed 10-player setup.
func BenchmarkCountInRadiusPeakGo(b *testing.B) {
	world.InitializeCollisionMaps()
	monster := setupEntity(b, "BenchOrc", ecs.EntityMonster, 1, 50, 50)
	players := make([]ecs.Entity, 10)
	for i := range players {
		players[i] = setupEntity(b, "P", ecs.EntityPlayer, 1, 50+(i%5), 50+(i/5))
	}
	b.Cleanup(func() {
		cleanupEntities(monster)
		cleanupEntities(players...)
	})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = spatial.CountInRadius(monster, 20.0, ecs.EntityPlayer)
	}
}
