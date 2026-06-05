package mcp

import (
	"encoding/json"
	"fmt"
	"server/ecs"
	"server/models"
	"server/world"
)

func init() {
	// ─── ECS Entity Tools ──────────────────────────────────────────────────────────

	Register("ecs_list_entities", func(req Request) Response {
		var p struct {
			Type string `json:"type"` // "player", "monster", "ground_item", or "" for all
		}
		if len(req.Params) > 0 {
			json.Unmarshal(req.Params, &p)
		}

		var entities []EntityInfo
		ecs.DefaultRegistry.RangeMetadata(func(id ecs.Entity, meta ecs.MetadataComponent) bool {
			if p.Type == "" || meta.Type.String() == p.Type {
				info := buildEntityInfo(id)
				entities = append(entities, info)
			}
			return true
		})

		if entities == nil {
			entities = []EntityInfo{}
		}
		return rpcResult(req.ID, map[string]any{
			"count":    len(entities),
			"entities": entities,
		})
	})

	Register("ecs_get_entity", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}

		info := buildEntityInfo(id)
		if info.Name == "" && info.Type == "" {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("entity %d not found", id))
		}
		return rpcResult(req.ID, info)
	})

	Register("ecs_get_stats", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		stats, ok := ecs.DefaultRegistry.GetStats(id)
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("stats for entity %d not found", id))
		}
		return rpcResult(req.ID, stats)
	})

	Register("ecs_get_position", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		pos, ok := ecs.DefaultRegistry.GetPosition(id)
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("position for entity %d not found", id))
		}
		return rpcResult(req.ID, pos)
	})

	Register("ecs_get_inventory", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		inv, ok := ecs.DefaultRegistry.GetInventory(id)
		if !ok || len(inv.Items) == 0 {
			return rpcResult(req.ID, map[string]any{
				"entity_id": uint64(id),
				"items":     []map[string]any{},
			})
		}
		var items []map[string]any
		for templateID, qty := range inv.Items {
			itemName := fmt.Sprintf("Item #%d", templateID)
			if t, exists := ItemRegistryGlobal[templateID]; exists {
				itemName = t.Name
			}
			items = append(items, map[string]any{
				"template_id": templateID,
				"name":        itemName,
				"quantity":    qty,
			})
		}
		return rpcResult(req.ID, map[string]any{
			"entity_id": uint64(id),
			"items":     items,
		})
	})

	Register("ecs_get_ai", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		ai, ok := ecs.DefaultRegistry.GetAI(id)
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("AI for entity %d not found", id))
		}
		return rpcResult(req.ID, map[string]any{
			"state":               ai.State.String(),
			"state_code":          int(ai.State),
			"target_id":           uint64(ai.TargetID),
			"spawn_x":             ai.SpawnX,
			"spawn_z":             ai.SpawnZ,
			"spawn_radius":        ai.SpawnRadius,
			"aggro_radius":        ai.AggroRadius,
			"leash_radius":        ai.LeashRadius,
			"melee_range":         ai.MeleeRange,
			"attack_cooldown":     ai.AttackTimer.GetCooldown(),
			"idle_cooldown":       ai.IdleTimer.GetCooldown(),
		})
	})

	Register("ecs_get_equipment", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		eq, ok := ecs.DefaultRegistry.GetEquipment(id)
		if !ok {
			return rpcResult(req.ID, map[string]any{
				"entity_id": uint64(id),
				"weapon_id": 0,
				"armor_id":  0,
			})
		}
		return rpcResult(req.ID, eq)
	})

	Register("ecs_get_party", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		party, ok := ecs.DefaultRegistry.GetParty(id)
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("party for entity %d not found", id))
		}
		members := make([]uint64, len(party.MemberIDs))
		for i, m := range party.MemberIDs {
			members[i] = uint64(m)
		}
		return rpcResult(req.ID, map[string]any{
			"party_id":   uint64(id),
			"leader_id":  uint64(party.LeaderID),
			"team_name":  party.TeamName,
			"member_ids": members,
		})
	})

	Register("ecs_get_effects", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		eff, ok := ecs.DefaultRegistry.GetEffects(id)
		if !ok || len(eff.ActiveList) == 0 {
			return rpcResult(req.ID, map[string]any{
				"entity_id": uint64(id),
				"effects":   []any{},
			})
		}
		return rpcResult(req.ID, map[string]any{
			"entity_id": uint64(id),
			"effects":   eff.ActiveList,
		})
	})

	Register("ecs_spawn_monster", func(req Request) Response {
		var p struct {
			TemplateID int `json:"template_id"`
			MapID      int `json:"map_id,omitempty"`
			X          int `json:"x,omitempty"`
			Z          int `json:"z,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, "missing or invalid parameters")
		}
		if p.TemplateID <= 0 {
			return rpcError(req.ID, ErrCodeInvalidParams, "template_id must be a positive integer")
		}
		if p.MapID == 0 {
			p.MapID = 1
		}

		id, err := models.SpawnMonsterFromTemplate(p.TemplateID, p.MapID, p.X, p.Z)
		if err != nil {
			return rpcError(req.ID, ErrCodeInternal, err.Error())
		}

		if pos, ok := ecs.DefaultRegistry.GetPosition(id); ok {
			world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
		}

		return rpcResult(req.ID, buildEntityInfo(id))
	})

	Register("ecs_remove_entity", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		if meta, ok := ecs.DefaultRegistry.GetMetadata(id); ok && meta.Type == ecs.EntityPlayer {
			return rpcError(req.ID, ErrCodeInternal, "cannot remove a player entity via MCP; use admin_kick_player instead")
		}
		world.GlobalSpatialGrid.RemoveEntity(id)
		ecs.DefaultRegistry.RemoveEntity(id)
		return rpcResult(req.ID, map[string]string{
			"status": fmt.Sprintf("entity %d removed", id),
		})
	})
}

// ItemRegistryGlobal is a reference to game.ItemRegistry set at boot.
var ItemRegistryGlobal = make(map[uint64]struct {
	ID        uint64
	Name      string
	HealValue int
	SlotType  string
	BonusDam  int
	BonusHP   int
})
