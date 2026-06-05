// Package mcp provides the MCP JSON-RPC server for AI agent inspection.
//
// handlers_diagnostics.go — MCP handlers for ECS diagnostics and leak detection.
package mcp

import (
	"encoding/json"
	"server/ecs"
)

func init() {
	// ─── ECS Diagnostics Tools ───────────────────────────────────────────────

	Register("ecs_diagnostics", func(req Request) Response {
		// Refresh diagnostics data
		ecs.GlobalECSStats.RefreshDiagnostics(ecs.DefaultRegistry)

		diag := ecs.GlobalECSStats.GetDiagnostics()

		return rpcResult(req.ID, map[string]any{
			"entity_count":     diag.EntityCount,
			"component_counts": diag.ComponentCounts,
			"query_counts":     buildQueryCountsMap(diag.QueryCounts),
			"last_scan_time":   diag.LastScanTime,
		})
	})

	Register("ecs_diagnostics_snapshot", func(req Request) Response {
		// Lightweight snapshot (no full refresh)
		ecs.GlobalECSStats.RefreshDiagnostics(ecs.DefaultRegistry)
		snap := ecs.GlobalECSStats.Snapshot()

		return rpcResult(req.ID, map[string]any{
			"entity_count": snap.EntityCount,
			"components":   snap.Components,
			"queries":      snap.Queries,
		})
	})

	// ─── Leak Detector Tools ────────────────────────────────────────────────

	Register("ecs_leak_scan", func(req Request) Response {
		report := ecs.GlobalLeakDetector.Scan(ecs.DefaultRegistry)

		return rpcResult(req.ID, map[string]any{
			"scan_time":          report.ScanTime,
			"total_scanned":      report.TotalScanned,
			"orphan_count":       report.OrphanCount,
			"forgotten_count":    report.ForgottenCount,
			"invalid_ref_count":  report.InvalidRefCount,
			"orphans":            buildLeakEntries(report.Orphans),
			"forgotten":          buildLeakEntries(report.Forgotten),
			"invalid_references": buildLeakEntries(report.InvalidReferences),
			"warnings":           report.Warnings,
			"healthy":            report.OrphanCount == 0 && report.ForgottenCount == 0 && report.InvalidRefCount == 0,
		})
	})

	// ─── World Snapshot Tools ───────────────────────────────────────────────

	Register("ecs_snapshot_metrics", func(req Request) Response {
		// This will be populated when the world snapshot system is integrated
		return rpcResult(req.ID, map[string]any{
			"message": "World snapshot metrics available via admin dashboard",
		})
	})
}

// buildQueryCountsMap converts the query counts map into a serializable format.
func buildQueryCountsMap(qc map[string]*ecs.QueryCount) map[string]map[string]any {
	result := make(map[string]map[string]any, len(qc))
	for name, q := range qc {
		result[name] = map[string]any{
			"count":      q.Count,
			"total_time": q.TotalTime.String(),
			"last_time":  q.LastTime.String(),
			"min_time":   q.MinTime.String(),
			"max_time":   q.MaxTime.String(),
		}
	}
	return result
}

// buildLeakEntries converts slice of LeakEntry to serializable format.
func buildLeakEntries(entries []ecs.LeakEntry) []map[string]any {
	if len(entries) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, len(entries))
	for i, e := range entries {
		result[i] = map[string]any{
			"entity_id":   uint64(e.EntityID),
			"description": e.Description,
			"detail":      e.Detail,
		}
	}
	return result
}

// Ensure json is used in this file
var _ = json.Marshal
