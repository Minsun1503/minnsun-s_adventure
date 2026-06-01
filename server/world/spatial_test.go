package world

import (
	"server/ecs"
	"testing"
)

func TestSpatialGridUpdateAndQuery(t *testing.T) {
	grid := newSpatialGrid()

	entity1 := ecs.Entity(1)
	entity2 := ecs.Entity(2)
	entity3 := ecs.Entity(3)

	// 1. Insert entities into grid
	pos1 := ecs.PositionComponent{MapID: 1, X: 5, Z: 5}   // Chunk (0,0)
	pos2 := ecs.PositionComponent{MapID: 1, X: 8, Z: 8}   // Chunk (0,0)
	pos3 := ecs.PositionComponent{MapID: 1, X: 45, Z: 45} // Chunk (2,2)

	grid.UpdateEntityPosition(entity1, pos1)
	grid.UpdateEntityPosition(entity2, pos2)
	grid.UpdateEntityPosition(entity3, pos3)

	// Verify reverse indexes
	chk1, ok := grid.GetEntityChunk(entity1)
	if !ok || chk1.X != 0 || chk1.Z != 0 {
		t.Errorf("Expected entity1 in chunk (0,0), got ok=%t chk=(%d,%d)", ok, chk1.X, chk1.Z)
	}

	chk3, ok := grid.GetEntityChunk(entity3)
	if !ok || chk3.X != 2 || chk3.Z != 2 {
		t.Errorf("Expected entity3 in chunk (2,2), got ok=%t chk=(%d,%d)", ok, chk3.X, chk3.Z)
	}

	// 2. Query Radius
	// Query around entity1 (5, 5) with radius 5 (should find entity2 at 8,8 because distance is sqrt(3^2 + 3^2) = 4.24 <= 5)
	results := grid.QueryRadius(pos1, 5.0, entity1)
	if len(results) != 1 {
		FreeQueryCandidates(results)
		t.Fatalf("Expected 1 result in radius, got %d", len(results))
	}
	if results[0].ID != entity2 {
		FreeQueryCandidates(results)
		t.Errorf("Expected entity2, got %d", results[0].ID)
	}
	FreeQueryCandidates(results)

	// Query around entity1 (5, 5) with radius 2 (should find nothing because entity2 is at distance 4.24)
	results = grid.QueryRadius(pos1, 2.0, entity1)
	if len(results) != 0 {
		FreeQueryCandidates(results)
		t.Errorf("Expected 0 results, got %d", len(results))
	}
	FreeQueryCandidates(results)

	// 3. Move entity2 to a different chunk (crossed boundary: 8,8 -> 25,25)
	pos2Moved := ecs.PositionComponent{MapID: 1, X: 25, Z: 25} // Chunk (1,1)
	grid.UpdateEntityPosition(entity2, pos2Moved)

	chk2, ok := grid.GetEntityChunk(entity2)
	if !ok || chk2.X != 1 || chk2.Z != 1 {
		t.Errorf("Expected entity2 in chunk (1,1), got ok=%t chk=(%d,%d)", ok, chk2.X, chk2.Z)
	}

	// Verify it's no longer in chunk (0,0)
	results = grid.QueryChunk(pos1, 0)
	if len(results) != 1 || results[0].ID != entity1 {
		t.Errorf("Expected only entity1 in chunk (0,0), got %v", results)
	}

	// 4. Remove entity1
	grid.RemoveEntity(entity1)
	_, ok = grid.GetEntityChunk(entity1)
	if ok {
		t.Error("Expected entity1 to be removed from reverse index")
	}

	results = grid.QueryChunk(pos1, 0)
	if len(results) != 0 {
		t.Errorf("Expected chunk (0,0) to be empty, got %v", results)
	}
}
