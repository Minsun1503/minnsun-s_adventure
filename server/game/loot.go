package game

import (
	"math/rand"
	"time"
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

// RollLoot evaluates a monster's drop table and returns a list of won item IDs
func RollLoot(monsterTemplateID uint64) []uint64 {
	randGen := rand.New(rand.NewSource(time.Now().UnixNano()))
	var rolledItems []uint64

	table, exists := MonsterLootTables[monsterTemplateID]
	if !exists {
		return rolledItems
	}

	for _, drop := range table {
		// Roll a decimal between 0.0 and 1.0
		if randGen.Float64() <= drop.DropChance {
			rolledItems = append(rolledItems, drop.ItemTemplateID)
		}
	}

	return rolledItems
}
