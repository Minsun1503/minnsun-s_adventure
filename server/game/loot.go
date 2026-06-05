package game

import (
	"server/peakgo/loot"
)

// ─── PeakGo Loot Table Registry ──────────────────────────────────────────────
//
// MonsterLootTables maps monster template IDs to pre-built peakgo/loot.LootTable
// instances. These are built once at server startup (InitializeLootTables) and
// reused across all monster kills — zero allocation per roll.
var MonsterLootTables = make(map[uint64]*loot.LootTable)

// LegacyDropItems converts old-format LootDropItem entries to peakgo/loot.DropEntry,
// enabling a smooth migration path without rewriting the entire loot configuration.
func legacyDropToEntry(items []LootDropItem) []loot.DropEntry {
	entries := make([]loot.DropEntry, 0, len(items))
	for _, item := range items {
		// Convert floating-point probability (0.0–1.0) to per-mille weight (0–1000).
		// A 70% drop chance becomes weight=700 per-mille.
		perMille := int(item.DropChance * 1000)
		if perMille < 1 {
			perMille = 1 // Minimum 0.1% to prevent zero-weight entries
		}
		entries = append(entries, loot.DropEntry{
			ItemID: item.ItemTemplateID,
			Weight: perMille,
			MinQty: 1,
			MaxQty: 1,
		})
	}
	return entries
}

// InitializeLootTables builds loot tables for all monster templates.
// Called once at server startup. Uses peakgo/loot.LootTable for zero-alloc
// weighted random selection via pool.SlicePool and peakgo/rng.
func InitializeLootTables() {
	// Monster Template 1 (Slime) — 70% chance for Red Potion (Item 101)
	MonsterLootTables[1] = loot.NewLootTable(
		legacyDropToEntry([]LootDropItem{
			{ItemTemplateID: 101, DropChance: 0.70},
		}),
		1000, // Always eligible for roll (individual item chance handles the 70%)
	)

	// Monster Template 2 (Wild Boar) — Red Potion 40%, Boar Tusk 20%
	MonsterLootTables[2] = loot.NewLootTable(
		legacyDropToEntry([]LootDropItem{
			{ItemTemplateID: 101, DropChance: 0.40},
			{ItemTemplateID: 202, DropChance: 0.20},
		}),
		1000,
	)
}

// ─── Legacy Support (backward compatible) ───────────────────────────────────
//
// LootDropItem is the legacy drop configuration format kept for backward
// compatibility. New code should use peakgo/loot.DropEntry directly.

// LootDropItem defines an item ID and its fractional probability chance.
type LootDropItem struct {
	ItemTemplateID uint64
	DropChance     float64
}

// RollLoot evaluates a monster's drop table using peakgo/loot.LootTable.
//
// Returns the same []uint64 format as the original API for backward compatibility.
// Under the hood, peakgo/loot uses pool.SlicePool[Drop] and peakgo/rng —
// zero heap allocations per roll, no mutex contention.
func RollLoot(monsterTemplateID uint64) []uint64 {
	table, exists := MonsterLootTables[monsterTemplateID]
	if !exists {
		return nil
	}

	// Create a minimal drop context (legacy path — no level/party info)
	ctx := &loot.DropContext{
		KillerLevel: 1,
		PartySize:   0,
	}

	drops := table.Roll(ctx)
	if len(*drops) == 0 {
		loot.ReleaseDrops(drops)
		return nil
	}

	// Convert peakgo/loot.Drop → []uint64 (backward compat format)
	result := make([]uint64, len(*drops))
	for i, d := range *drops {
		result[i] = d.ItemID
	}

	loot.ReleaseDrops(drops)
	return result
}
