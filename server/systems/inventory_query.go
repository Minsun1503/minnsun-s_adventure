package systems

import (
	"fmt"
	"server/ecs"
	"strings"
)

// RunInventoryQuerySystem processes the "I" command for a specific player.
// It inspects their inline inventory row and resolves names from the item registry.
func RunInventoryQuerySystem(playerID ecs.Entity) string {
	// 1. Fetch the inline value copy from your TypedSyncMap table
	inv, hasInv := ecs.GlobalRegistry.GetInventory(playerID)

	// Boundary check: If no inventory component is assigned or it's completely empty
	if !hasInv || len(inv.Items) == 0 {
		return "[INVENTORY]: Your backpack is completely empty!\r\n"
	}

	var sb strings.Builder
	sb.WriteString("═══ YOUR BACKPACK BAG ═══\r\n")

	// 2. Loop through the player's items map row
	for itemID, count := range inv.Items {
		if count <= 0 {
			continue
		}

		// Resolve the static display name using your ItemRegistry map
		itemTemplate, exists := ItemRegistry[itemID]

		if exists {
			sb.WriteString(fmt.Sprintf(" • %s x%d (%s)\r\n",
				itemTemplate.Name, count, itemTemplate.Description))
		} else {
			// Fallback string if the item template definition doesn't exist in memory
			sb.WriteString(fmt.Sprintf(" • Unknown Item ID #%d x%d\r\n", itemID, count))
		}
	}
	sb.WriteString("════════════════════════════\r\n")

	return sb.String()
}
