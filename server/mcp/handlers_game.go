package mcp

import (
	"encoding/json"
	"fmt"
	"server/ecs"
	"server/game"
	"server/models"
	"server/protocol"
	"server/systems"
)

func init() {
	// ─── Game State Tools ──────────────────────────────────────────────────────────

	Register("game_online_players", func(req Request) Response {
		var players []EntityInfo
		ecs.DefaultRegistry.RangeMetadata(func(id ecs.Entity, meta ecs.MetadataComponent) bool {
			if meta.Type == ecs.EntityPlayer {
				info := buildEntityInfo(id)
				if _, hasConn := ecs.DefaultRegistry.GetConnection(id); hasConn {
					info.Type = "online"
				}
				players = append(players, info)
			}
			return true
		})
		if players == nil {
			players = []EntityInfo{}
		}
		return rpcResult(req.ID, map[string]any{
			"count":   len(players),
			"players": players,
		})
	})

	Register("game_player_detail", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		info := buildEntityInfo(id)

		// Inventory
		invMap := make(map[string]any)
		if inv, ok := ecs.DefaultRegistry.GetInventory(id); ok && len(inv.Items) > 0 {
			items := make([]map[string]any, 0)
			for tid, qty := range inv.Items {
				items = append(items, map[string]any{
					"template_id": tid,
					"quantity":    qty,
				})
			}
			invMap["items"] = items
		} else {
			invMap["items"] = []any{}
		}

		// Party info
		partyInfo := map[string]any{}
		if pm, ok := ecs.DefaultRegistry.GetPartyMember(id); ok {
			partyInfo["party_id"] = uint64(pm.PartyID)
			if party, ok := ecs.DefaultRegistry.GetParty(pm.PartyID); ok {
				partyInfo["team_name"] = party.TeamName
				members := make([]uint64, len(party.MemberIDs))
				for i, m := range party.MemberIDs {
					members[i] = uint64(m)
				}
				partyInfo["members"] = members
			}
		}

		return rpcResult(req.ID, map[string]any{
			"entity":    info,
			"inventory": invMap,
			"party":     partyInfo,
		})
	})

	Register("game_monster_list", func(req Request) Response {
		var monsters []map[string]any
		ecs.DefaultRegistry.RangeAI(func(id ecs.Entity, ai ecs.AIComponent) bool {
			info := buildEntityInfo(id)
			monsters = append(monsters, map[string]any{
				"id":       uint64(id),
				"name":     info.Name,
				"ai_state": ai.State.String(),
				"position": map[string]int{
					"map_id": info.MapID,
					"x":      info.X,
					"z":      info.Z,
				},
				"stats": map[string]int{
					"hp":     info.HP,
					"max_hp": info.MaxHP,
					"damage": info.Damage,
				},
			})
			return true
		})
		if monsters == nil {
			monsters = []map[string]any{}
		}
		return rpcResult(req.ID, map[string]any{
			"count":    len(monsters),
			"monsters": monsters,
		})
	})

	Register("game_item_registry", func(req Request) Response {
		items := make([]map[string]any, 0)
		for _, t := range game.ItemRegistry {
			items = append(items, map[string]any{
				"id":          t.ID,
				"name":        t.Name,
				"description": t.Description,
				"slot_type":   t.SlotType,
				"heal_value":  t.HealValue,
				"bonus_dam":   t.BonusDam,
				"bonus_hp":    t.BonusHP,
			})
		}
		return rpcResult(req.ID, items)
	})

	Register("game_skill_registry", func(req Request) Response {
		skills := make([]map[string]any, 0)
		for id, s := range models.SkillRegistry {
			skills = append(skills, map[string]any{
				"id":          id,
				"name":        s.Name,
				"mana_cost":   s.ManaCost,
				"damage_mult": s.DamMult,
			})
		}
		return rpcResult(req.ID, skills)
	})

	Register("game_loot_table", func(req Request) Response {
		var p struct {
			MonsterTemplateID uint64 `json:"monster_template_id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.MonsterTemplateID == 0 {
			return rpcError(req.ID, ErrCodeInvalidParams, "monster_template_id is required")
		}

		drops, ok := game.MonsterLootTables[p.MonsterTemplateID]
		if !ok {
			return rpcResult(req.ID, map[string]any{
				"monster_template_id": p.MonsterTemplateID,
				"drops":               []any{},
			})
		}

		items := make([]map[string]any, len(drops))
		for i, d := range drops {
			name := fmt.Sprintf("Item #%d", d.ItemTemplateID)
			if t, exists := game.ItemRegistry[d.ItemTemplateID]; exists {
				name = t.Name
			}
			items[i] = map[string]any{
				"item_template_id": d.ItemTemplateID,
				"name":             name,
				"drop_chance":      d.DropChance,
			}
		}
		return rpcResult(req.ID, map[string]any{
			"monster_template_id": p.MonsterTemplateID,
			"drops":               items,
		})
	})

	Register("game_config", func(req Request) Response {
		return rpcResult(req.ID, map[string]any{
			"max_party_size":    4,
			"tick_rate_ms":      250,
			"max_move_per_tick": 2,
			"world_bounds":      "0-100",
			"maps":              []string{"Town", "Forest", "Dungeon"},
		})
	})

	// ─── Server Operations ─────────────────────────────────────────────────────────

	Register("server_stats", func(req Request) Response {
		entityCount := 0
		playerCount := 0
		monsterCount := 0
		ecs.DefaultRegistry.RangeMetadata(func(_ ecs.Entity, meta ecs.MetadataComponent) bool {
			entityCount++
			switch meta.Type {
			case ecs.EntityPlayer:
				playerCount++
			case ecs.EntityMonster:
				monsterCount++
			}
			return true
		})
		return rpcResult(req.ID, map[string]any{
			"total_entities": entityCount,
			"players":        playerCount,
			"monsters":       monsterCount,
			"tick":           systems.CurrentTick(),
		})
	})

	Register("server_broadcast", func(req Request) Response {
		var p struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Message == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "message is required")
		}
		systems.BroadcastSystem([]byte(p.Message + "\r\n"))
		return rpcResult(req.ID, map[string]string{"status": "broadcast sent"})
	})

	Register("server_broadcast_map", func(req Request) Response {
		var p struct {
			MapID   int    `json:"map_id"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Message == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "map_id and message are required")
		}
		protocol.BroadcastToMap(p.MapID, p.Message+"\r\n")
		return rpcResult(req.ID, map[string]string{"status": "broadcast sent to map"})
	})
}
