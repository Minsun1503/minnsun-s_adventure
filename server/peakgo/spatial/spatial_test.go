package spatial_test

import (
	"server/ecs"
	"server/peakgo/spatial"
	"server/world"
	"testing"
)

func setupEntity(name, typ string, mapID, x, z int) ecs.Entity {
	id := ecs.GlobalRegistry.NewEntity()
	ecs.GlobalRegistry.SetMetadata(id, ecs.MetadataComponent{Name: name, Type: typ})
	pos := ecs.PositionComponent{MapID: mapID, X: x, Z: z}
	ecs.GlobalRegistry.SetPosition(id, pos)
	world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
	return id
}

func TestGetNearestPlayerFindsClosest(t *testing.T) {
	world.InitializeCollisionMaps()

	monster := setupEntity("Orc", "monster", 1, 50, 50)
	near := setupEntity("NearPlayer", "player", 1, 52, 50) // distance=2
	far := setupEntity("FarPlayer", "player", 1, 60, 50)   // distance=10

	result, found := spatial.GetNearestPlayer(monster, 15.0)
	if !found {
		t.Fatal("expected to find a nearby player")
	}
	if result.ID != near {
		t.Errorf("expected nearest player to be NearPlayer (id %d), got id %d", near, result.ID)
	}

	// Cleanup
	world.GlobalSpatialGrid.RemoveEntity(monster)
	world.GlobalSpatialGrid.RemoveEntity(near)
	world.GlobalSpatialGrid.RemoveEntity(far)
	ecs.GlobalRegistry.RemoveEntity(monster)
	ecs.GlobalRegistry.RemoveEntity(near)
	ecs.GlobalRegistry.RemoveEntity(far)
}

func TestGetNearestPlayerReturnsNothingOutOfRange(t *testing.T) {
	monster := setupEntity("Orc2", "monster", 1, 10, 10)
	player := setupEntity("Player2", "player", 1, 90, 90) // far away

	_, found := spatial.GetNearestPlayer(monster, 5.0)
	if found {
		t.Error("expected no player within radius 5 when player is at (90,90)")
	}

	world.GlobalSpatialGrid.RemoveEntity(monster)
	world.GlobalSpatialGrid.RemoveEntity(player)
	ecs.GlobalRegistry.RemoveEntity(monster)
	ecs.GlobalRegistry.RemoveEntity(player)
}

func TestCountInRadius(t *testing.T) {
	origin := setupEntity("Origin", "monster", 1, 30, 30)
	p1 := setupEntity("P1", "player", 1, 31, 30)  // distance=1
	p2 := setupEntity("P2", "player", 1, 32, 30)  // distance=2
	m1 := setupEntity("M1", "monster", 1, 33, 30) // distance=3

	playerCount := spatial.CountInRadius(origin, 5.0, "player")
	if playerCount != 2 {
		t.Errorf("expected 2 players, got %d", playerCount)
	}

	monsterCount := spatial.CountInRadius(origin, 5.0, "monster")
	if monsterCount != 1 {
		t.Errorf("expected 1 monster (excluding origin), got %d", monsterCount)
	}

	allCount := spatial.CountInRadius(origin, 5.0, "")
	if allCount != 3 {
		t.Errorf("expected 3 total entities, got %d", allCount)
	}

	world.GlobalSpatialGrid.RemoveEntity(origin)
	world.GlobalSpatialGrid.RemoveEntity(p1)
	world.GlobalSpatialGrid.RemoveEntity(p2)
	world.GlobalSpatialGrid.RemoveEntity(m1)
	ecs.GlobalRegistry.RemoveEntity(origin)
	ecs.GlobalRegistry.RemoveEntity(p1)
	ecs.GlobalRegistry.RemoveEntity(p2)
	ecs.GlobalRegistry.RemoveEntity(m1)
}

func TestIsAnyInRadius(t *testing.T) {
	origin := setupEntity("Check", "monster", 1, 20, 20)
	player := setupEntity("Nearby", "player", 1, 21, 20)

	if !spatial.IsAnyInRadius(origin, 3.0, "player") {
		t.Error("expected player within radius 3")
	}
	if spatial.IsAnyInRadius(origin, 0.5, "player") {
		t.Error("expected no player within radius 0.5")
	}

	world.GlobalSpatialGrid.RemoveEntity(origin)
	world.GlobalSpatialGrid.RemoveEntity(player)
	ecs.GlobalRegistry.RemoveEntity(origin)
	ecs.GlobalRegistry.RemoveEntity(player)
}

func TestDistanceBetween(t *testing.T) {
	a := setupEntity("A", "player", 1, 0, 0)
	b := setupEntity("B", "player", 1, 3, 4) // distance = 5, sq = 25

	dsq, ok := spatial.DistanceBetween(a, b)
	if !ok {
		t.Fatal("DistanceBetween returned ok=false")
	}
	if dsq != 25 {
		t.Fatalf("expected 25 (3-4-5 triangle), got %f", dsq)
	}

	world.GlobalSpatialGrid.RemoveEntity(a)
	world.GlobalSpatialGrid.RemoveEntity(b)
	ecs.GlobalRegistry.RemoveEntity(a)
	ecs.GlobalRegistry.RemoveEntity(b)
}

func TestSameMap(t *testing.T) {
	a := setupEntity("MapA1", "player", 1, 5, 5)
	b := setupEntity("MapA2", "player", 1, 6, 6)
	c := setupEntity("MapB1", "player", 2, 5, 5)

	if !spatial.SameMap(a, b) {
		t.Error("a and b should be on the same map")
	}
	if spatial.SameMap(a, c) {
		t.Error("a and c should be on different maps")
	}

	world.GlobalSpatialGrid.RemoveEntity(a)
	world.GlobalSpatialGrid.RemoveEntity(b)
	world.GlobalSpatialGrid.RemoveEntity(c)
	ecs.GlobalRegistry.RemoveEntity(a)
	ecs.GlobalRegistry.RemoveEntity(b)
	ecs.GlobalRegistry.RemoveEntity(c)
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkGetNearestPlayer(b *testing.B) {
	world.InitializeCollisionMaps()

	monster := setupEntity("BenchOrc", "monster", 1, 50, 50)
	players := make([]ecs.Entity, 10)
	for i := range players {
		players[i] = setupEntity("P", "player", 1, 50+i, 50)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = spatial.GetNearestPlayer(monster, 20.0)
	}

	world.GlobalSpatialGrid.RemoveEntity(monster)
	for _, p := range players {
		world.GlobalSpatialGrid.RemoveEntity(p)
	}
}

func BenchmarkCountInRadius(b *testing.B) {
	origin := setupEntity("BenchOrigin", "monster", 1, 50, 50)
	for i := 0; i < 20; i++ {
		e := setupEntity("P", "player", 1, 50+i%10, 50+i/10)
		_ = e
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = spatial.CountInRadius(origin, 15.0, "player")
	}
}
