package game

import (
	"fmt"
	"server/ecs"
	"server/protocol"
	"server/world"
)

// HandleItemPickupSystem processes a player's PICKUP command for a ground item entity.
// It verifies spatial ranges and securely transfers the ground item to the player's backpack.
func HandleItemPickupSystem(playerID ecs.Entity, itemEntityID ecs.Entity) (string, bool) {
	// 1. FETCH ITEM METADATA: Verify this entity is actually a piece of ground floor loot
	itemMeta, hasMeta := ecs.GlobalRegistry.GetMetadata(itemEntityID)
	if !hasMeta || itemMeta.Type != "ground_item" {
		return "Error: That item is either not on the ground or has already vanished!\r\n", false
	}

	// 2. SPATIAL PROXIMITY SECURITY GATE
	// Items must be close enough to pick up. We use a range threshold of 5.0 units.
	const pickupRange = 5.0
	if !world.IsInRange(playerID, itemEntityID, pickupRange) {
		return "Pickup Denied: You are standing too far away from this item!\r\n", false
	}

	// 3. ATOMIC PURGE STEP (The Anti-Duplication Lock)
	// We extract the item's position and template ID, then immediately delete it.
	// This is transaction-safe since registry map lookups are safe.
	itemPos, hasPos := ecs.GlobalRegistry.GetPosition(itemEntityID)
	if !hasPos {
		return "Error: Item state has already been modified by another player.\r\n", false
	}

	// Query template component before purging the entity
	templateComp, hasTemplate := ecs.GlobalRegistry.GetItemTemplate(itemEntityID)
	var resolvedTemplateID uint64 = 101 // Fallback default if not found
	if hasTemplate {
		resolvedTemplateID = templateComp.TemplateID
	}

	// Remove the item from both spatial grid and registry immediately to lock it to this routine
	world.GlobalSpatialGrid.RemoveEntity(itemEntityID)
	ecs.GlobalRegistry.RemoveEntity(itemEntityID)

	// 4. COPY-MODIFY-OVERWRITE: Insert item token into player's inventory table row
	inv, hasInv := ecs.GlobalRegistry.GetInventory(playerID)
	if !hasInv {
		inv = ecs.InventoryComponent{Items: make(map[uint64]int)}
	}

	inv.Items[resolvedTemplateID]++
	ecs.GlobalRegistry.SetInventory(playerID, inv)

	// 5. LOCAL COMMUNICATION PACKET (No emojis)
	playerMeta, _ := ecs.GlobalRegistry.GetMetadata(playerID)
	successBroadcast := fmt.Sprintf("[LOOT]: Player %s picked up a %s from the floor.\r\n",
		playerMeta.Name, itemMeta.Name)

	// Notify all local area map witnesses that the item has been picked up
	protocol.BroadcastToMap(itemPos.MapID, successBroadcast)

	personalFeedback := fmt.Sprintf("You successfully stowed %s in your backpack!\r\n", itemMeta.Name)
	return personalFeedback, true
}
