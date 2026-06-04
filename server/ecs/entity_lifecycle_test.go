package ecs

import (
	"testing"
)

// TestEntityLifecycle verifies that RemoveEntity cleans up all component stores
// and no component references leak after deletion.
func TestEntityLifecycle(t *testing.T) {
	reg := &Registry{}

	// Create an entity and populate all component stores.
	eid := reg.NewEntity()
	reg.SetPosition(eid, PositionComponent{MapID: 1, X: 100, Z: 100})
	reg.SetConnection(eid, ConnectionComponent{})
	reg.SetMetadata(eid, MetadataComponent{Name: "test_entity", Type: EntityPlayer})
	reg.SetStats(eid, StatsComponent{HP: 100, MaxHP: 100})
	reg.SetAI(eid, AIComponent{State: AIStateIdle})
	reg.SetInventory(eid, InventoryComponent{Items: map[uint64]int{1: 5}})
	reg.SetLifetime(eid, LifetimeComponent{})
	reg.SetItemTemplate(eid, ItemTemplateComponent{TemplateID: 1})
	reg.SetEquipment(eid, EquipmentComponent{WeaponID: 1})
	reg.SetParty(eid, PartyComponent{LeaderID: eid, TeamName: "test", MemberIDs: []Entity{eid}})
	reg.SetPartyMember(eid, PartyMemberComponent{PartyID: eid})
	reg.SetEffects(eid, EffectsComponent{ActiveList: []ActiveEffect{{Type: "poison", Value: 10}}})

	// Verify all components exist.
	t.Run("all_components_present_before_removal", func(t *testing.T) {
		if _, ok := reg.GetPosition(eid); !ok {
			t.Error("position should exist")
		}
		if _, ok := reg.GetConnection(eid); !ok {
			t.Error("connection should exist")
		}
		if _, ok := reg.GetMetadata(eid); !ok {
			t.Error("metadata should exist")
		}
		if _, ok := reg.GetStats(eid); !ok {
			t.Error("stats should exist")
		}
		if _, ok := reg.GetAI(eid); !ok {
			t.Error("AI should exist")
		}
		if _, ok := reg.GetInventory(eid); !ok {
			t.Error("inventory should exist")
		}
		if _, ok := reg.GetLifetime(eid); !ok {
			t.Error("lifetime should exist")
		}
		if _, ok := reg.GetItemTemplate(eid); !ok {
			t.Error("item template should exist")
		}
		if _, ok := reg.GetEquipment(eid); !ok {
			t.Error("equipment should exist")
		}
		if _, ok := reg.GetParty(eid); !ok {
			t.Error("party should exist")
		}
		if _, ok := reg.GetPartyMember(eid); !ok {
			t.Error("party member should exist")
		}
		if _, ok := reg.GetEffects(eid); !ok {
			t.Error("effects should exist")
		}
	})

	// Remove the entity.
	reg.RemoveEntity(eid)

	// Verify ALL component stores return ok=false.
	t.Run("no_components_after_removal", func(t *testing.T) {
		stores := []struct {
			name string
			ok   bool
		}{
			{"position", func() bool { _, ok := reg.GetPosition(eid); return ok }()},
			{"connection", func() bool { _, ok := reg.GetConnection(eid); return ok }()},
			{"metadata", func() bool { _, ok := reg.GetMetadata(eid); return ok }()},
			{"stats", func() bool { _, ok := reg.GetStats(eid); return ok }()},
			{"AI", func() bool { _, ok := reg.GetAI(eid); return ok }()},
			{"inventory", func() bool { _, ok := reg.GetInventory(eid); return ok }()},
			{"lifetime", func() bool { _, ok := reg.GetLifetime(eid); return ok }()},
			{"item_template", func() bool { _, ok := reg.GetItemTemplate(eid); return ok }()},
			{"equipment", func() bool { _, ok := reg.GetEquipment(eid); return ok }()},
			{"party", func() bool { _, ok := reg.GetParty(eid); return ok }()},
			{"party_member", func() bool { _, ok := reg.GetPartyMember(eid); return ok }()},
			{"effects", func() bool { _, ok := reg.GetEffects(eid); return ok }()},
		}

		for _, s := range stores {
			if s.ok {
				t.Errorf("component store %s should return ok=false after RemoveEntity", s.name)
			}
		}
	})

	// Verify the entity ID counter is NOT rolled back (by design — ID is never reused).
	t.Run("next_id_preserved", func(t *testing.T) {
		nextID := reg.NewEntity()
		if nextID <= eid {
			t.Errorf("new entity ID %d should be > removed ID %d (no ID reuse)", nextID, eid)
		}
	})
}
