package game

import (
	"fmt"
	"server/ecs"
)

// HandleEquipmentSystem processes equipping an item from the player's inventory onto their active slots.
// It verifies inventory ownership, maps the item to the weapon or armor slot, and triggers RecalculateActiveStats.
func HandleEquipmentSystem(playerID ecs.Entity, itemID uint64) (string, bool) {
	// 1. Verify item exists and is actually equippable gear configuration
	item, exists := ItemRegistry[itemID]
	if !exists || (item.SlotType != "weapon" && item.SlotType != "armor") {
		return "Error: This item cannot be equipped!\r\n", false
	}

	// 2. Verify player actually owns the target piece inside their inventory component
	inv, hasInv := ecs.GlobalRegistry.GetInventory(playerID)
	if !hasInv || inv.Items[itemID] <= 0 {
		return fmt.Sprintf("Error: You do not own any %s!\r\n", item.Name), false
	}

	// 3. COPY: Pull active equipment layout rows
	eq, _ := ecs.GlobalRegistry.GetEquipment(playerID)

	// 4. MODIFY: Assign the template ID to the matching slot channel
	if item.SlotType == "weapon" {
		eq.WeaponID = itemID
	} else if item.SlotType == "armor" {
		eq.ArmorID = itemID
	}

	// 5. OVERWRITE: Push modified data structs back lock-free
	ecs.GlobalRegistry.SetEquipment(playerID, eq)

	// 6. AGGREGATION LOOP STEP: Trigger calculations to rebuild combat attributes
	RecalculateActiveStats(playerID)

	meta, _ := ecs.GlobalRegistry.GetMetadata(playerID)
	notice := fmt.Sprintf("[GEAR]: Player %s equipped %s! Stats successfully calculated.\r\n", meta.Name, item.Name)
	return notice, true
}
