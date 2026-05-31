package systems

// ItemTemplate defines the static, read-only stats for an item configuration.
type ItemTemplate struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HealValue   int
	SlotType    string // "weapon", "armor", or "consumable"
	BonusDam    int    // Attack power added when equipped
	BonusHP     int    // Max HP added when equipped
}

// Global cache holding all loaded item definitions in memory
var ItemRegistry = make(map[uint64]ItemTemplate)

// InitializeItemRegistry populates our items database with names and icons
func InitializeItemRegistry() {
	ItemRegistry[101] = ItemTemplate{
		ID:          101,
		Name:        "Red Potion",
		Description: "Restores 50 Vitality HP instantly.",
		HealValue:   50,
		SlotType:    "consumable",
	}
	ItemRegistry[202] = ItemTemplate{
		ID:          202,
		Name:        "Boar Tusk",
		Description: "A sharp tusk used for crafting equipment.",
		HealValue:   0,
		SlotType:    "consumable",
	}
	ItemRegistry[303] = ItemTemplate{
		ID:          303,
		Name:        "Iron Sword",
		Description: "A forged blade that increases strike force.",
		SlotType:    "weapon",
		BonusDam:    15, // +15 damage
	}
	ItemRegistry[404] = ItemTemplate{
		ID:          404,
		Name:        "Leather Armor",
		Description: "Simple leather armor that protects the body.",
		SlotType:    "armor",
		BonusHP:     30, // +30 max HP
	}
}
