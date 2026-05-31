package game

import (
	"encoding/binary"
	"fmt"
	"server/ecs"
)

// HandleItemUsageSystem processes a USE [item_id] binary command packet.
// It coordinates safe updates between the inventory tables and combat stats tables.
// Payload layout: [ItemID (uint64 - BE)] (total 8 bytes)
func HandleItemUsageSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	if len(payload) != 8 {
		return "Error: Invalid item usage payload length. Expected 8 bytes.\r\n", false
	}

	// 1. Decode Item ID from binary payload
	itemID := binary.BigEndian.Uint64(payload[0:8])

	// 2. VERIFY STATIC ITEM CONFIGURATION
	itemTemplate, itemExists := ItemRegistry[itemID]
	if !itemExists {
		return "Error: This item template does not exist in the server data table.\r\n", false
	}
	if itemTemplate.HealValue <= 0 {
		return fmt.Sprintf("Error: %s is a crafting material and cannot be consumed!\r\n", itemTemplate.Name), false
	}

	// 3. COPY & VALIDATE PLAYER INVENTORY BAG
	inv, hasInv := ecs.GlobalRegistry.GetInventory(playerID)
	if !hasInv || inv.Items[itemID] <= 0 {
		return fmt.Sprintf("Error: You do not own any %s!\r\n", itemTemplate.Name), false
	}

	// 4. COPY & VALIDATE PLAYER STATS PROFILE
	stats, hasStats := ecs.GlobalRegistry.GetStats(playerID)
	if !hasStats {
		return "Error: Your character stats profile was not found.\r\n", false
	}
	if stats.HP <= 0 {
		return "You cannot use items while you are dead!\r\n", false
	}
	if stats.HP >= stats.MaxHP {
		return fmt.Sprintf("You are already at full health! (%d/%d HP)\r\n", stats.HP, stats.MaxHP), false
	}

	// 5. MODIFY STEP: Deduct inventory quantity & calculate recovery numbers
	inv.Items[itemID]--

	oldHP := stats.HP
	stats.HP += itemTemplate.HealValue
	if stats.HP > stats.MaxHP {
		stats.HP = stats.MaxHP // Clamp to boundary limits
	}
	actualHealed := stats.HP - oldHP

	// 6. OVERWRITE STEP: Commit both updated value copies back to database tables
	ecs.GlobalRegistry.SetInventory(playerID, inv)
	ecs.GlobalRegistry.SetStats(playerID, stats)

	meta, _ := ecs.GlobalRegistry.GetMetadata(playerID)
	successMsg := fmt.Sprintf("[CONSUME] Player %s drank %s! Restored +%d HP. (Vitals: %d/%d HP)\r\n",
		meta.Name, itemTemplate.Name, actualHealed, stats.HP, stats.MaxHP)

	return successMsg, true
}
