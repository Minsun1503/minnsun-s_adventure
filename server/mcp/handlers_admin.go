package mcp

import (
	"encoding/json"
	"fmt"
	"server/ecs"
	"server/game"
	"server/logger"
	"server/world"
)

func init() {
	// ─── Admin / Debug Tools ───────────────────────────────────────────────────────

	Register("admin_kick_player", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		connComp, ok := ecs.GlobalRegistry.GetConnection(id)
		if !ok || connComp.Conn == nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("entity %d has no active connection", id))
		}
		meta, _ := ecs.GlobalRegistry.GetMetadata(id)
		name := meta.Name
		if name == "" {
			name = fmt.Sprintf("entity_%d", id)
		}
		connComp.Conn.Close()
		return rpcResult(req.ID, map[string]string{
			"status": fmt.Sprintf("kicked player %s (%d)", name, id),
		})
	})

	Register("admin_teleport", func(req Request) Response {
		var p struct {
			EntityID uint64 `json:"entity_id"`
			MapID    int    `json:"map_id"`
			X        int    `json:"x"`
			Z        int    `json:"z"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.EntityID == 0 {
			return rpcError(req.ID, ErrCodeInvalidParams, "entity_id, map_id, x, z are required")
		}
		msg, ok := world.ExecuteMapTransfer(ecs.Entity(p.EntityID), p.MapID, p.X, p.Z)
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, msg)
		}
		return rpcResult(req.ID, map[string]string{"status": msg})
	})

	Register("admin_set_debug", func(req Request) Response {
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, "'enabled' boolean is required")
		}
		logger.SetDebugMode(p.Enabled)
		return rpcResult(req.ID, map[string]any{
			"debug_enabled": p.Enabled,
		})
	})

	Register("admin_watch_entity", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		logger.GlobalEntityTracer.Watch(uint64(id))
		return rpcResult(req.ID, map[string]string{
			"status": fmt.Sprintf("watching entity %d", id),
		})
	})

	Register("admin_unwatch_entity", func(req Request) Response {
		id, err := parseEntityParam(req.Params)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}
		logger.GlobalEntityTracer.Unwatch(uint64(id))
		return rpcResult(req.ID, map[string]string{
			"status": fmt.Sprintf("stopped watching entity %d", id),
		})
	})

	Register("admin_give_item", func(req Request) Response {
		var p struct {
			PlayerID       uint64 `json:"player_id"`
			ItemTemplateID uint64 `json:"item_template_id"`
			Quantity       int    `json:"quantity"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.PlayerID == 0 || p.ItemTemplateID == 0 {
			return rpcError(req.ID, ErrCodeInvalidParams, "player_id, item_template_id, quantity are required")
		}
		if p.Quantity <= 0 {
			p.Quantity = 1
		}

		inv, ok := ecs.GlobalRegistry.GetInventory(ecs.Entity(p.PlayerID))
		if !ok {
			inv = ecs.InventoryComponent{Items: make(map[uint64]int)}
		} else {
			inv = inv.Clone()
		}
		inv.Items[p.ItemTemplateID] += p.Quantity
		ecs.GlobalRegistry.SetInventory(ecs.Entity(p.PlayerID), inv)

		return rpcResult(req.ID, map[string]any{
			"status":        fmt.Sprintf("gave item %d x%d to player %d", p.ItemTemplateID, p.Quantity, p.PlayerID),
			"new_total_qty": inv.Items[p.ItemTemplateID],
		})
	})

	Register("admin_set_stat", func(req Request) Response {
		var p struct {
			PlayerID uint64 `json:"player_id"`
			Field    string `json:"field"`
			Value    int    `json:"value"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.PlayerID == 0 || p.Field == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "player_id, field, value are required")
		}

		stats, ok := ecs.GlobalRegistry.GetStats(ecs.Entity(p.PlayerID))
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, "player stats not found")
		}

		switch p.Field {
		case "hp":
			stats.HP = p.Value
		case "max_hp":
			stats.MaxHP = p.Value
		case "mp":
			stats.MP = p.Value
		case "max_mp":
			stats.MaxMP = p.Value
		case "damage":
			stats.Dam = p.Value
		case "level":
			stats.Level = p.Value
		case "xp":
			stats.XP = uint64(p.Value)
		default:
			return rpcError(req.ID, ErrCodeInvalidParams, fmt.Sprintf("unknown field '%s'. valid fields: hp, max_hp, mp, max_mp, damage, level, xp", p.Field))
		}
		ecs.GlobalRegistry.SetStats(ecs.Entity(p.PlayerID), stats)

		return rpcResult(req.ID, map[string]any{
			"status": fmt.Sprintf("set %s = %d for player %d", p.Field, p.Value, p.PlayerID),
			"stats":  stats,
		})
	})

	Register("admin_spawn_item_ground", func(req Request) Response {
		var p struct {
			ItemTemplateID uint64 `json:"item_template_id"`
			MapID          int    `json:"map_id"`
			X              int    `json:"x"`
			Z              int    `json:"z"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ItemTemplateID == 0 {
			return rpcError(req.ID, ErrCodeInvalidParams, "item_template_id, map_id, x, z are required")
		}
		if p.MapID == 0 {
			p.MapID = 1
		}

		eid := game.SpawnItemOnGround(p.ItemTemplateID, p.MapID, p.X, p.Z)
		if eid == 0 {
			return rpcError(req.ID, ErrCodeInternal, "failed to spawn item on ground")
		}
		return rpcResult(req.ID, buildEntityInfo(eid))
	})

	// Wire up ItemRegistryGlobal from game.ItemRegistry for inventory display.
	// This runs at init() time — after game.InitializeItemRegistry() in main().
	// Since init() runs before main(), we populate ItemRegistryGlobal lazily
	// via a deferred function or direct call at boot. For now, it's populated
	// by the integration point in server.go.
}
