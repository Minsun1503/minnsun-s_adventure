package game

import (
	"server/peakgo/rng"
)

// LootDropItem defines an item ID and its fractional probability chance.
type LootDropItem struct {
	ItemTemplateID uint64
	DropChance     float64 // Value between 0.0 and 1.0 (e.g., 0.50 = 50% drop rate)
}

// Global Registry holding loot drops per monster template ID
var MonsterLootTables = make(map[uint64][]LootDropItem)

// InitializeLootTables sets up basic item rewards for your database
func InitializeLootTables() {
	// Monster Template 1 (Slime) drops Item 101 (Red Potion) at a 70% chance
	MonsterLootTables[1] = []LootDropItem{
		{ItemTemplateID: 101, DropChance: 0.70},
	}
	// Monster Template 2 (Wild Boar) drops Item 101 (Red Potion) at 40% and Item 202 (Boar Tusk) at 20%
	MonsterLootTables[2] = []LootDropItem{
		{ItemTemplateID: 101, DropChance: 0.40},
		{ItemTemplateID: 202, DropChance: 0.20},
	}
}

// RollLoot evaluates a monster's drop table and returns a list of won item IDs.
//
// # Migration note
//
// Previously this function created rand.New(rand.NewSource(time.Now().UnixNano()))
// on every call — a heap allocation per monster kill. Under high kill rates this
// generated GC pressure.
//
// rng.Float64() draws from a pooled *rand.Rand: 0 allocs, no mutex contention.
func RollLoot(monsterTemplateID uint64) []uint64 {
	var rolledItems []uint64

	table, exists := MonsterLootTables[monsterTemplateID]
	if !exists {
		return rolledItems
	}

	for _, drop := range table {
		// rng.Float64(): pooled RNG — replaces rand.New(rand.NewSource(...)).Float64()
		// which allocated a new *rand.Rand on the heap every invocation.
		if rng.Float64() <= drop.DropChance {
			rolledItems = append(rolledItems, drop.ItemTemplateID)
		}
	}

	return rolledItems
}
