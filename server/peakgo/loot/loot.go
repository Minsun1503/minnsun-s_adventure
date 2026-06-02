// Package loot provides a zero-allocation, weighted random loot table engine
// for the Minnsun's Adventure 2.5D MMORPG server.
//
// # Why this package exists
//
// Monster drops, treasure chests, and reward systems need configurable,
// weighted random selection with quantity ranges. This package provides
// a fast, testable loot table engine using the pooled rng package.
//
// # Peak Go Contract
//
// Zero heap allocations per roll. Loot tables are pre-computed at server
// startup. Results use pooled slices from pool.SlicePool.
package loot

import (
	"server/peakgo/pool"
	"server/peakgo/rng"
)

// ─── Drop Entry ───────────────────────────────────────────────────────────────

// DropEntry defines a single item that can be dropped from a loot table.
// All fields are inline value types — zero heap overhead.
type DropEntry struct {
	ItemID        uint64 // Template ID of the item
	Weight        int    // Relative weight for random selection (higher = more common)
	MinQty        int    // Minimum quantity on drop
	MaxQty        int    // Maximum quantity on drop (inclusive)
	RequiredLevel int    // Minimum player level for this drop to be eligible
	Chance        int    // Per-mille drop chance override (0 = use table default, 1000 = always)
}

// Drop represents a single dropped item with its quantity.
// Value type — zero heap allocation.
type Drop struct {
	ItemID uint64
	Qty    int
}

// ─── Drop Condition ───────────────────────────────────────────────────────────

// ConditionFunc is a function that determines if a drop entry is eligible.
// Returns true if the drop should be considered.
type ConditionFunc func(entry DropEntry, context *DropContext) bool

// DropContext carries contextual information for condition evaluation.
// Value type — pass by pointer for mutation in game systems.
type DropContext struct {
	KillerLevel int
	KillerClass int // 0 = warrior, 1 = mage, 2 = archer, etc.
	PartySize   int
	IsBoss      bool
	WorldLevel  int // Average world level for dynamic scaling
}

// ─── Loot Table ───────────────────────────────────────────────────────────────

// MaxLootEntries is the maximum number of drop entries in a single loot table.
const MaxLootEntries = 64

// LootTable is a collection of drop entries with configured probabilities.
// Created at server startup and shared across all monster spawns.
type LootTable struct {
	entries     []DropEntry
	totalWeight int
	baseChance  int // Per-mille base drop chance (1000 = always drop at least one)
	customCond  ConditionFunc

	// scratch buffer for rollSingle: avoids allocations by reusing a fixed-size array.
	// Populated with eligible entries per roll.
	scratch  [MaxLootEntries]DropEntry
	scratchW [MaxLootEntries]int // paralllel weight buffer
}

// NewLootTable creates a new loot table with the given entries.
// The baseChance is per-mille (e.g., 500 = 50% chance to drop).
func NewLootTable(entries []DropEntry, baseChance int) *LootTable {
	lt := &LootTable{
		entries:    entries,
		baseChance: baseChance,
	}
	for _, e := range entries {
		lt.totalWeight += e.Weight
	}
	return lt
}

// WithCondition attaches a custom condition function to this loot table.
func (lt *LootTable) WithCondition(cond ConditionFunc) *LootTable {
	lt.customCond = cond
	return lt
}

// Clone creates a copy of this loot table for modification.
func (lt *LootTable) Clone() *LootTable {
	clone := &LootTable{
		entries:     make([]DropEntry, len(lt.entries)),
		totalWeight: lt.totalWeight,
		baseChance:  lt.baseChance,
	}
	copy(clone.entries, lt.entries)
	return clone
}

// referenced slice pools for roll results
var dropPool = pool.NewSlicePool[Drop](4) // Most monsters drop 1-4 items

// ─── Rolling ──────────────────────────────────────────────────────────────────

// Roll performs a loot table roll and returns the dropped items.
// Uses the provided context for conditional filtering.
// The returned slice should be Put back to dropPool when done.
func (lt *LootTable) Roll(ctx *DropContext) *[]Drop {
	result := dropPool.Get()

	// Base chance check: does this monster drop anything at all?
	if lt.baseChance < 1000 && rng.Intn(1000) >= lt.baseChance {
		return result // Empty drop
	}

	// Determine number of drops (1-3 based on luck/party)
	numDrops := 1
	if ctx.PartySize > 0 {
		numDrops = 1 + rng.Intn(ctx.PartySize) // 1 + 0..partySize-1
	}
	if numDrops > 3 {
		numDrops = 3 // Cap at 3 drops per monster
	}

	for range numDrops {
		drop := lt.rollSingle(ctx)
		if drop.ItemID != 0 {
			*result = append(*result, drop)
		}
	}

	return result
}

// rollSingle picks one item from the loot table using weighted random selection.
// Zero heap alloc: uses the pre-allocated scratch buffer on LootTable.
func (lt *LootTable) rollSingle(ctx *DropContext) Drop {
	if lt.totalWeight <= 0 {
		return Drop{}
	}

	// Filter eligible entries into pre-allocated scratch buffer
	eligible := lt.scratch[:0]
	weightBuf := lt.scratchW[:0]
	var eligibleWeight int

	for _, entry := range lt.entries {
		// Level check
		if entry.RequiredLevel > 0 && ctx.KillerLevel < entry.RequiredLevel {
			continue
		}

		// Custom condition check
		if lt.customCond != nil && !lt.customCond(entry, ctx) {
			continue
		}

		// Individual chance check
		if entry.Chance > 0 && entry.Chance < 1000 {
			if rng.Intn(1000) >= entry.Chance {
				continue
			}
		}

		eligible = append(eligible, entry)
		weightBuf = append(weightBuf, entry.Weight)
		eligibleWeight += entry.Weight
	}

	if len(eligible) == 0 || eligibleWeight <= 0 {
		return Drop{}
	}

	// Weighted random selection using parallel weight buffer
	roll := rng.Intn(eligibleWeight)
	cumulative := 0
	for i := range eligible {
		cumulative += weightBuf[i]
		if roll < cumulative {
			// Calculate quantity
			qty := eligible[i].MinQty
			if eligible[i].MaxQty > eligible[i].MinQty {
				qty += rng.Intn(eligible[i].MaxQty - eligible[i].MinQty + 1)
			}
			return Drop{ItemID: eligible[i].ItemID, Qty: qty}
		}
	}

	return Drop{}
}

// ─── Convenience: Common Loot Tables ──────────────────────────────────────────

// NewCoinDrop creates a simple coin/gold drop table.
func NewCoinDrop(minCoins, maxCoins int) *LootTable {
	return NewLootTable([]DropEntry{
		{ItemID: 1, Weight: 100, MinQty: minCoins, MaxQty: maxCoins},
	}, 900) // 90% base chance
}

// NewMonsterDrop creates a loot table for standard monsters.
// commonDropChance is per-mille for common items.
func NewMonsterDrop(common, uncommon []DropEntry, commonDropChance int) *LootTable {
	entries := make([]DropEntry, 0, len(common)+len(uncommon))
	entries = append(entries, common...)
	entries = append(entries, uncommon...)
	return NewLootTable(entries, commonDropChance)
}

// NewBossDrop creates a loot table for bosses (guaranteed rare drops).
func NewBossDrop(guaranteed, rare []DropEntry) *LootTable {
	entries := make([]DropEntry, 0, len(guaranteed)+len(rare))
	entries = append(entries, guaranteed...)
	entries = append(entries, rare...)
	return NewLootTable(entries, 1000) // 100% base chance
}

// ─── Drop Pool Management ─────────────────────────────────────────────────────

// ReleaseDrops returns a drop result slice to the pool.
func ReleaseDrops(drops *[]Drop) {
	dropPool.Put(drops)
}

// ─── Gold Coin Constants ──────────────────────────────────────────────────────

const (
	GoldCoinID   uint64 = 1
	SilverCoinID uint64 = 2
	CopperCoinID uint64 = 3
)

// NewCurrencyDrop creates a currency drop with gold, silver, and copper.
func NewCurrencyDrop(goldMin, goldMax, silverMin, silverMax int) *LootTable {
	entries := []DropEntry{
		{ItemID: GoldCoinID, Weight: 100, MinQty: goldMin, MaxQty: goldMax},
		{ItemID: SilverCoinID, Weight: 300, MinQty: silverMin, MaxQty: silverMax},
	}
	return NewLootTable(entries, 800) // 80% chance for currency
}
