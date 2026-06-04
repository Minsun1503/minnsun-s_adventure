package aoi

import (
	"server/ecs"
	"testing"
)

// positions is a test stub that returns positions for entities.
var testPositions = map[ecs.Entity]ecs.PositionComponent{
	1: {MapID: 1, X: 10, Z: 10},
	2: {MapID: 1, X: 12, Z: 12},
	3: {MapID: 1, X: 50, Z: 50},
}

func testPosGetter(id ecs.Entity) (ecs.PositionComponent, bool) {
	p, ok := testPositions[id]
	return p, ok
}

// fakeSpatialRadiusFactory returns a query function that returns known neighbor sets.
func fakeSpatialRadiusFactory(neighbors map[ecs.Entity][]ecs.Entity) SpatialQueryFunc {
	return func(pos ecs.PositionComponent, radius float64, excludeID ecs.Entity) *[]ecs.Entity {
		n := neighbors[excludeID]
		ps := EntityListPool.Get()
		*ps = append(*ps, n...)
		return ps
	}
}

func TestRegisterAndUpdate(t *testing.T) {
	m := NewAOIManager()
	e1 := ecs.Entity(1)
	e2 := ecs.Entity(2)

	// Register watchers
	m.RegisterWatcher(e1, 60.0)
	m.RegisterWatcher(e2, 60.0)

	// Initial state: e1 sees e2, e2 sees e1
	neighbors := map[ecs.Entity][]ecs.Entity{
		e1: {e2},
		e2: {e1},
	}
	results := m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors))

	// Both should have enter events for each other
	e1Events := results[e1]
	if len(e1Events) != 1 || e1Events[0].Type != EventEnter || e1Events[0].Target != e2 {
		t.Fatalf("expected e1 to have Enter for e2, got %+v", e1Events)
	}
	e2Events := results[e2]
	if len(e2Events) != 1 || e2Events[0].Type != EventEnter || e2Events[0].Target != e1 {
		t.Fatalf("expected e2 to have Enter for e1, got %+v", e2Events)
	}
}

func TestLeaveEvent(t *testing.T) {
	m := NewAOIManager()
	e1 := ecs.Entity(1)
	e2 := ecs.Entity(2)

	m.RegisterWatcher(e1, 60.0)
	m.RegisterWatcher(e2, 60.0)

	// First tick: both see each other
	neighbors := map[ecs.Entity][]ecs.Entity{
		e1: {e2},
		e2: {e1},
	}
	m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors))

	// Second tick: e1 sees nothing, e2 sees nothing (e1 moved away)
	neighbors2 := map[ecs.Entity][]ecs.Entity{
		e1: {},
		e2: {},
	}
	results := m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors2))

	// Both should have leave events
	e1Events := results[e1]
	if len(e1Events) != 1 || e1Events[0].Type != EventLeave || e1Events[0].Target != e2 {
		t.Fatalf("expected e1 to have Leave for e2, got %+v", e1Events)
	}
	e2Events := results[e2]
	if len(e2Events) != 1 || e2Events[0].Type != EventLeave || e2Events[0].Target != e1 {
		t.Fatalf("expected e2 to have Leave for e1, got %+v", e2Events)
	}
}

func TestNoChange(t *testing.T) {
	m := NewAOIManager()
	e1 := ecs.Entity(1)
	e2 := ecs.Entity(2)

	m.RegisterWatcher(e1, 60.0)

	// Both ticks same neighbors
	neighbors := map[ecs.Entity][]ecs.Entity{
		e1: {e2},
	}
	m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors))
	results := m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors))

	if len(results) != 0 {
		t.Fatalf("expected no events when state unchanged, got %+v", results)
	}
}

func TestReenter(t *testing.T) {
	m := NewAOIManager()
	e1 := ecs.Entity(1)
	e2 := ecs.Entity(2)

	m.RegisterWatcher(e1, 60.0)

	// Tick 1: see e2
	neighbors1 := map[ecs.Entity][]ecs.Entity{e1: {e2}}
	m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors1))

	// Tick 2: e2 leaves
	neighbors2 := map[ecs.Entity][]ecs.Entity{e1: {}}
	m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors2))

	// Tick 3: e2 re-enters
	results := m.UpdateAll(testPosGetter, fakeSpatialRadiusFactory(neighbors1))

	e1Events := results[e1]
	if len(e1Events) != 1 || e1Events[0].Type != EventEnter || e1Events[0].Target != e2 {
		t.Fatalf("expected e1 to have Enter for e2 on re-enter, got %+v", results)
	}
}

func TestUnregister(t *testing.T) {
	m := NewAOIManager()
	e1 := ecs.Entity(1)

	m.RegisterWatcher(e1, 60.0)
	if m.WatcherCount() != 1 {
		t.Fatal("expected 1 watcher")
	}
	m.UnregisterWatcher(e1)
	if m.WatcherCount() != 0 {
		t.Fatal("expected 0 watchers after unregister")
	}
}

// BenchmarkUpdateAll ensures AOI delta computation is fast and zero-alloc.
func BenchmarkUpdateAll(b *testing.B) {
	m := NewAOIManager()
	const numEntities = 32

	// Register many entities
	for i := 0; i < numEntities; i++ {
		e := ecs.Entity(uint64(100 + i))
		m.RegisterWatcher(e, 60.0)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		results := m.UpdateAll(
			func(id ecs.Entity) (ecs.PositionComponent, bool) {
				return ecs.PositionComponent{MapID: 1, X: 10, Z: 10}, true
			},
			func(pos ecs.PositionComponent, radius float64, excludeID ecs.Entity) *[]ecs.Entity {
				ps := EntityListPool.Get()
				*ps = (*ps)[:0]
				return ps
			},
		)
		_ = results
	}
}
