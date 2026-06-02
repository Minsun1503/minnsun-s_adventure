package mcp

import (
	"encoding/json"
	"server/ecs"
	"server/world"
)

func init() {
	// ─── World / Spatial Tools ────────────────────────────────────────────────────

	Register("world_chunk_info", func(req Request) Response {
		var p struct {
			MapID  int `json:"map_id"`
			ChunkX int `json:"chunk_x"`
			ChunkZ int `json:"chunk_z"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, "missing or invalid parameters")
		}

		key := world.ChunkKey{MapID: p.MapID, X: p.ChunkX, Z: p.ChunkZ}
		candidates := world.GlobalSpatialGrid.QueryChunkByKey(key, 0)
		if candidates == nil || len(*candidates) == 0 {
			return rpcResult(req.ID, map[string]any{
				"chunk":    key,
				"entities": []any{},
			})
		}

		var entities []EntityInfo
		for _, c := range *candidates {
			entities = append(entities, buildEntityInfo(c.ID))
		}
		world.FreeQueryCandidates(candidates)

		return rpcResult(req.ID, map[string]any{
			"chunk":    key,
			"count":    len(entities),
			"entities": entities,
		})
	})

	Register("world_query_radius", func(req Request) Response {
		var p struct {
			EntityID uint64  `json:"entity_id"`
			Radius   float64 `json:"radius"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, "missing or invalid parameters")
		}
		if p.EntityID == 0 {
			return rpcError(req.ID, ErrCodeInvalidParams, "entity_id is required")
		}
		if p.Radius <= 0 {
			p.Radius = 10.0
		}

		pos, ok := ecs.GlobalRegistry.GetPosition(ecs.Entity(p.EntityID))
		if !ok {
			return rpcError(req.ID, ErrCodeInternal, "entity position not found")
		}

		candidates := world.GlobalSpatialGrid.QueryRadius(pos, p.Radius, ecs.Entity(p.EntityID))
		if candidates == nil || len(*candidates) == 0 {
			return rpcResult(req.ID, map[string]any{
				"origin_id": p.EntityID,
				"radius":    p.Radius,
				"count":     0,
				"entities":  []any{},
			})
		}

		var entities []EntityInfo
		for _, c := range *candidates {
			entities = append(entities, buildEntityInfo(c.ID))
		}
		world.FreeQueryCandidates(candidates)

		return rpcResult(req.ID, map[string]any{
			"origin_id": p.EntityID,
			"radius":    p.Radius,
			"count":     len(entities),
			"entities":  entities,
		})
	})

	Register("world_collision_map", func(req Request) Response {
		var p struct {
			MapID int `json:"map_id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			p.MapID = 1
		}

		// Render a text-based collision map (101x101 chars is too large, show summary).
		blockedCount := 0
		for x := 0; x < 101; x++ {
			for z := 0; z < 101; z++ {
				if world.IsTileBlocked(p.MapID, x, z) {
					blockedCount++
				}
			}
		}

		return rpcResult(req.ID, map[string]any{
			"map_id":        p.MapID,
			"grid_size":     "101x101",
			"blocked_tiles": blockedCount,
			"open_tiles":    101*101 - blockedCount,
		})
	})

	Register("world_grid_stats", func(req Request) Response {
		stats := world.GlobalSpatialGrid.DebugStats()
		return rpcResult(req.ID, map[string]string{
			"stats": stats,
		})
	})
}
