package mcp

import (
	"encoding/json"
	"fmt"
	"server/models"
)

func init() {
	// ─── Code / DB Tool: db_get_schema ──────────────────────────────────────────

	Register("db_get_schema", func(req Request) Response {
		var p struct {
			TableName string `json:"table_name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.TableName == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "table_name is required")
		}

		if models.DBEngine == nil {
			return rpcError(req.ID, ErrCodeInternal, "database not initialized")
		}

		rows, err := models.DBEngine.Query(fmt.Sprintf("SHOW COLUMNS FROM `%s`", p.TableName))
		if err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("query failed: %v", err))
		}
		defer rows.Close()

		type columnInfo struct {
			Field   string `json:"field"`
			Type    string `json:"type"`
			Null    string `json:"null"`
			Key     string `json:"key"`
			Default any    `json:"default"`
			Extra   string `json:"extra"`
		}

		var cols []columnInfo
		for rows.Next() {
			var c columnInfo
			if err := rows.Scan(&c.Field, &c.Type, &c.Null, &c.Key, &c.Default, &c.Extra); err != nil {
				return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("scan failed: %v", err))
			}
			cols = append(cols, c)
		}

		return rpcResult(req.ID, map[string]any{
			"table":   p.TableName,
			"columns": cols,
		})
	})
}
