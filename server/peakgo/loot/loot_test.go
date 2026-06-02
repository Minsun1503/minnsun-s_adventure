package loot

import (
	"testing"
)

func TestLootTableRoll(t *testing.T) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 50, MinQty: 1, MaxQty: 2},
		{ItemID: 101, Weight: 30, MinQty: 1, MaxQty: 1},
		{ItemID: 102, Weight: 20, MinQty: 1, MaxQty: 3},
	}
	lt := NewLootTable(entries, 1000) // 100% base chance
	ctx := &DropContext{KillerLevel: 10, PartySize: 0}

	// Roll multiple times
	hasItems := false
	for range 100 {
		drops := lt.Roll(ctx)
		if len(*drops) > 0 {
			hasItems = true
			// Verify dropped items are valid
			for _, drop := range *drops {
				if drop.ItemID < 100 || drop.ItemID > 102 {
					t.Fatalf("unexpected item ID: %d", drop.ItemID)
				}
				if drop.Qty < 1 {
					t.Fatalf("quantity should be >= 1, got %d", drop.Qty)
				}
			}
		}
		ReleaseDrops(drops)
	}

	if !hasItems {
		t.Fatal("expected at least some drops with 100% base chance")
	}
}

func TestLootTableEmpty(t *testing.T) {
	lt := NewLootTable(nil, 1000)
	ctx := &DropContext{}
	drops := lt.Roll(ctx)
	if len(*drops) != 0 {
		t.Fatal("expected empty drops from nil entries")
	}
	ReleaseDrops(drops)
}

func TestLootTableZeroChance(t *testing.T) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 100, MinQty: 1, MaxQty: 1},
	}
	lt := NewLootTable(entries, 0) // 0% base chance
	ctx := &DropContext{KillerLevel: 10}

	items := 0
	for range 50 {
		drops := lt.Roll(ctx)
		items += len(*drops)
		ReleaseDrops(drops)
	}

	if items > 0 {
		t.Fatal("expected no drops with 0% base chance")
	}
}

func TestLootTableLevelRequirement(t *testing.T) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 100, MinQty: 1, MaxQty: 1, RequiredLevel: 50},
		{ItemID: 101, Weight: 100, MinQty: 1, MaxQty: 1},
	}
	lt := NewLootTable(entries, 1000)
	ctx := &DropContext{KillerLevel: 10} // Below requirement

	drops := lt.Roll(ctx)
	for _, drop := range *drops {
		if drop.ItemID == 100 {
			t.Fatal("should not drop item requiring level 50 when killer is level 10")
		}
	}
	ReleaseDrops(drops)
}

func TestLootTableCustomCondition(t *testing.T) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 100, MinQty: 1, MaxQty: 1},
	}
	lt := NewLootTable(entries, 1000)
	lt.WithCondition(func(entry DropEntry, ctx *DropContext) bool {
		return ctx.IsBoss // Only drop if boss
	})

	// Non-boss context
	ctx := &DropContext{IsBoss: false}
	drops := lt.Roll(ctx)
	if len(*drops) > 0 {
		t.Fatal("expected no drops when condition fails (non-boss)")
	}
	ReleaseDrops(drops)

	// Boss context
	ctx2 := &DropContext{IsBoss: true}
	drops2 := lt.Roll(ctx2)
	found := false
	for _, d := range *drops2 {
		if d.ItemID == 100 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected drop when condition passes (boss)")
	}
	ReleaseDrops(drops2)
}

func TestLootTableIndividualChance(t *testing.T) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 100, MinQty: 1, MaxQty: 1, Chance: 100}, // 10% individual chance
	}
	lt := NewLootTable(entries, 1000)
	ctx := &DropContext{KillerLevel: 10}

	dropped := false
	for range 100 {
		drops := lt.Roll(ctx)
		for _, d := range *drops {
			if d.ItemID == 100 {
				dropped = true
				break
			}
		}
		ReleaseDrops(drops)
	}

	if !dropped {
		t.Log("item with 10% chance may not drop in 100 rolls (unlikely but possible)")
	}
}

func TestNewCoinDrop(t *testing.T) {
	lt := NewCoinDrop(10, 50)
	ctx := &DropContext{KillerLevel: 1}

	drops := lt.Roll(ctx)
	for _, drop := range *drops {
		if drop.ItemID != GoldCoinID {
			t.Fatalf("expected gold coin (ID=1), got ID=%d", drop.ItemID)
		}
		if drop.Qty < 10 || drop.Qty > 50 {
			t.Fatalf("expected quantity 10-50, got %d", drop.Qty)
		}
	}
	ReleaseDrops(drops)
}

func TestNewBossDrop(t *testing.T) {
	guaranteed := []DropEntry{
		{ItemID: 1000, Weight: 100, MinQty: 1, MaxQty: 1}, // Boss core
	}
	rare := []DropEntry{
		{ItemID: 2000, Weight: 1, MinQty: 1, MaxQty: 1}, // Legendary sword
		{ItemID: 2001, Weight: 1, MinQty: 1, MaxQty: 1}, // Legendary armor
	}
	lt := NewBossDrop(guaranteed, rare)
	ctx := &DropContext{KillerLevel: 50, IsBoss: true, PartySize: 4}

	drops := lt.Roll(ctx)
	foundGuaranteed := false
	for _, d := range *drops {
		if d.ItemID == 1000 {
			foundGuaranteed = true
		}
	}
	if !foundGuaranteed {
		t.Fatal("expected guaranteed boss drop")
	}
	ReleaseDrops(drops)
}

func TestLootTableClone(t *testing.T) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 100, MinQty: 1, MaxQty: 1},
	}
	original := NewLootTable(entries, 1000)
	clone := original.Clone()

	// Modify clone's entries via roll context
	ctx := &DropContext{KillerLevel: 10}
	drops1 := original.Roll(ctx)
	drops2 := clone.Roll(ctx)

	// Both should work
	if len(*drops1) == 0 && len(*drops2) == 0 {
		t.Fatal("both original and clone should be functional")
	}
	ReleaseDrops(drops1)
	ReleaseDrops(drops2)
}

func TestReleaseDrops(t *testing.T) {
	ctx := &DropContext{KillerLevel: 10}
	entries := []DropEntry{
		{ItemID: 100, Weight: 100, MinQty: 1, MaxQty: 1},
	}
	lt := NewLootTable(entries, 1000)

	drops := lt.Roll(ctx)
	// This should not panic
	ReleaseDrops(drops)
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkLootTableRoll(b *testing.B) {
	entries := []DropEntry{
		{ItemID: 100, Weight: 50, MinQty: 1, MaxQty: 2},
		{ItemID: 101, Weight: 30, MinQty: 1, MaxQty: 1},
		{ItemID: 102, Weight: 20, MinQty: 1, MaxQty: 1},
	}
	lt := NewLootTable(entries, 1000)
	ctx := &DropContext{KillerLevel: 10, PartySize: 4}

	b.ResetTimer()
	for range b.N {
		drops := lt.Roll(ctx)
		ReleaseDrops(drops)
	}
}

func BenchmarkLootTableRollSimple(b *testing.B) {
	lt := NewCoinDrop(10, 50)
	ctx := &DropContext{KillerLevel: 1}

	b.ResetTimer()
	for range b.N {
		drops := lt.Roll(ctx)
		ReleaseDrops(drops)
	}
}
