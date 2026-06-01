package ecs

import (
	"sync"
	"testing"
)

func TestRegistryInventoryRace(t *testing.T) {
	registry := &Registry{}
	entityID := registry.NewEntity()

	// Initial inventory setup
	initialInv := InventoryComponent{
		Items: map[uint64]int{
			1: 100,
			2: 200,
		},
	}
	registry.SetInventory(entityID, initialInv)

	var wg sync.WaitGroup
	numReaders := 50
	numWriters := 50
	iterations := 500

	wg.Add(numReaders + numWriters)

	// Spin up 50 reader Goroutines
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				inv, ok := registry.GetInventory(entityID)
				if ok {
					// Read from the map
					_ = inv.Items[1]
					_ = inv.Items[2]
				}
			}
		}()
	}

	// Spin up 50 writer Goroutines that use CoW (Clone -> Mutate -> Set)
	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				inv, ok := registry.GetInventory(entityID)
				if ok {
					// CoW Pattern
					clone := inv.Clone()
					clone.Items[1] = writerID*iterations + j
					clone.Items[2] = writerID*iterations - j
					registry.SetInventory(entityID, clone)
				}
			}
		}(i)
	}

	wg.Wait()
}
