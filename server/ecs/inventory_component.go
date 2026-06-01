package ecs

type InventoryComponent struct {
	Items map[uint64]int // Maps ItemTemplateID -> Quantity owned
}

// Clone thực hiện DEEP COPY dữ liệu map bên trong.
// Bắt buộc gọi trước khi chỉnh sửa linh kiện từ bất kỳ System nào.
func (c InventoryComponent) Clone() InventoryComponent {
	if c.Items == nil {
		return InventoryComponent{Items: make(map[uint64]int)}
	}
	clone := make(map[uint64]int, len(c.Items))
	for k, v := range c.Items {
		clone[k] = v
	}
	return InventoryComponent{Items: clone}
}

func (r *Registry) SetInventory(id Entity, comp InventoryComponent) {
	r.inventories.Set(id, comp)
}

func (r *Registry) GetInventory(id Entity) (InventoryComponent, bool) {
	return r.inventories.Get(id)
}
