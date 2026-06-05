// Package network provides network-layer utilities for the game server.
//
// inspector_api.go — Admin/debug UI for viewing entity states, components,
// position, and AI. Extends the Metrics Panel HTTP Server with entity
// inspection endpoints.
package network

import (
	"net/http"
	"server/ecs"
)

// ─── Endpoint: /debug/entities ────────────────────────────────────────────────

// EntitiesResponse is the JSON structure for /debug/entities.
type EntitiesResponse struct {
	Entities []EntityDetail `json:"entities"`
	Total    int            `json:"total"`
}

// EntityDetail is a serializable summary of a single entity for the inspector.
type EntityDetail struct {
	ID      uint64 `json:"id"`
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	MapID   int    `json:"map_id,omitempty"`
	X       int    `json:"x,omitempty"`
	Z       int    `json:"z,omitempty"`
	HP      int    `json:"hp,omitempty"`
	MaxHP   int    `json:"max_hp,omitempty"`
	MP      int    `json:"mp,omitempty"`
	MaxMP   int    `json:"max_mp,omitempty"`
	Damage  int    `json:"damage,omitempty"`
	Level   int    `json:"level,omitempty"`
	XP      uint64 `json:"xp,omitempty"`
	Weapon  uint64 `json:"weapon_id,omitempty"`
	Armor   uint64 `json:"armor_id,omitempty"`
	AIState string `json:"ai_state,omitempty"`
}

// handleDebugEntities returns a JSON array of all live entity snapshots.
func (as *AdminServer) handleDebugEntities(w http.ResponseWriter, r *http.Request) {
	// Support single entity lookup via ?id=X
	idStr := r.URL.Query().Get("id")
	if idStr != "" {
		as.handleDebugEntityByID(w, r)
		return
	}

	var details []EntityDetail
	ecs.DefaultRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		detail := entityDetailFromSnapshot(snap)
		details = append(details, detail)
		return true
	})

	if details == nil {
		details = []EntityDetail{} // ensure JSON array, not null
	}

	resp := EntitiesResponse{
		Entities: details,
		Total:    len(details),
	}
	writeJSON(w, resp)
}

// handleDebugEntityByID returns a single entity detail for ?id=X.
func (as *AdminServer) handleDebugEntityByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	var entityID uint64
	for _, c := range idStr {
		if c < '0' || c > '9' {
			http.Error(w, `{"error":"invalid id: must be a positive integer"}`, http.StatusBadRequest)
			return
		}
		entityID = entityID*10 + uint64(c-'0')
	}
	if entityID == 0 {
		http.Error(w, `{"error":"invalid id: must be a positive integer"}`, http.StatusBadRequest)
		return
	}

	snap, ok := ecs.DefaultRegistry.GetSnapshot(ecs.Entity(entityID))
	if !ok {
		http.Error(w, `{"error":"entity not found"}`, http.StatusNotFound)
		return
	}

	detail := entityDetailFromSnapshot(snap)

	// Add extra component details for single-entity view
	type entityFullDetail struct {
		EntityDetail
		HasPosition bool `json:"has_position"`
		HasStats    bool `json:"has_stats"`
		HasAI       bool `json:"has_ai"`
		HasEquip    bool `json:"has_equipment"`
		HasInv      bool `json:"has_inventory"`
		HasEffects  bool `json:"has_effects"`
	}

	full := entityFullDetail{
		EntityDetail: detail,
	}
	full.HasPosition = snap.HasPos
	full.HasStats = snap.HasStats

	if _, ok := ecs.DefaultRegistry.GetAI(ecs.Entity(entityID)); ok {
		full.HasAI = true
	}
	if _, ok := ecs.DefaultRegistry.GetEquipment(ecs.Entity(entityID)); ok {
		full.HasEquip = true
	}
	if _, ok := ecs.DefaultRegistry.GetInventory(ecs.Entity(entityID)); ok {
		full.HasInv = true
	}
	if _, ok := ecs.DefaultRegistry.GetEffects(ecs.Entity(entityID)); ok {
		full.HasEffects = true
	}

	writeJSON(w, full)
}

// entityDetailFromSnapshot builds an EntityDetail from an EntitySnapshot.
func entityDetailFromSnapshot(snap ecs.EntitySnapshot) EntityDetail {
	detail := EntityDetail{
		ID:   uint64(snap.ID),
		Name: snap.Meta.Name,
		Type: snap.Meta.Type.String(),
	}
	if snap.HasPos {
		detail.MapID = snap.Pos.MapID
		detail.X = snap.Pos.X
		detail.Z = snap.Pos.Z
	}
	if snap.HasStats {
		detail.HP = snap.Stats.HP
		detail.MaxHP = snap.Stats.MaxHP
		detail.MP = snap.Stats.MP
		detail.MaxMP = snap.Stats.MaxMP
		detail.Damage = snap.Stats.Dam
		detail.Level = snap.Stats.Level
		detail.XP = snap.Stats.XP
	}
	// Get optional components
	if eq, ok := ecs.DefaultRegistry.GetEquipment(snap.ID); ok {
		detail.Weapon = eq.WeaponID
		detail.Armor = eq.ArmorID
	}
	if ai, ok := ecs.DefaultRegistry.GetAI(snap.ID); ok {
		detail.AIState = ai.State.String()
	}
	return detail
}
