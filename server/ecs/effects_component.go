package ecs

import "time"

// ActiveEffect represents a single temporary modifier layer running on an entity.
type ActiveEffect struct {
	Type         string        // "poison", "burn", "haste_buff"
	Value        int           // The damage or stat modifier amount
	Duration     time.Duration // Total remaining time
	LastTickTime time.Time     // Last time a DoT damage was applied
}

// EffectsComponent is mapped directly to an entity row anchor inside the TypedSyncMap
type EffectsComponent struct {
	ActiveList []ActiveEffect
}

// Clone thực hiện DEEP COPY danh sách hiệu ứng bên trong.
func (c EffectsComponent) Clone() EffectsComponent {
	if c.ActiveList == nil {
		return EffectsComponent{ActiveList: nil}
	}
	return EffectsComponent{
		ActiveList: append([]ActiveEffect(nil), c.ActiveList...),
	}
}
