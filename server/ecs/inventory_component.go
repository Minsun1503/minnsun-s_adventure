package ecs

// InventoryComponent stores items by tracking template IDs and counts.
// By storing data as a flat map inside an inline struct, it fits perfectly
// into your high-performance TypedSyncMap.
type InventoryComponent struct {
	Items map[uint64]int // Maps ItemTemplateID -> Quantity owned
}

// Helper methods on Registry for InventoryComponent:

// Add these helper methods inside your ecs.go file:
func (r *Registry) SetInventory(id Entity, comp InventoryComponent) {
	r.inventories.Set(id, comp)
}

func (r *Registry) GetInventory(id Entity) (InventoryComponent, bool) {
	return r.inventories.Get(id)
}
