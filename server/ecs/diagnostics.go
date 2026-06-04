// Package ecs provides the Entity Component System for the game server.
//
// diagnostics.go — ECS Diagnostics, Component Leak Detector, and health metrics.
//
// This file provides runtime introspection tools for the ECS layer:
//
//  1. ECSDiagnostics: Tracks entity count, component count, query count, and
//     query duration for each registered query. Exposed via MCP/admin API.
//
//  2. ComponentLeakDetector: Scans for orphan entities (missing required
//     components), forgotten components (component without parent entity),
//     and invalid references (entity IDs that reference non-existent entities).
//
// Both systems are zero-impact on hot-path — they run on-demand (via admin
// command or periodic background scan) and never during game ticks.
package ecs

import (
	"server/peakgo/perf"
	"sync"
	"sync/atomic"
	"time"
)

// ─── ECS Diagnostics ─────────────────────────────────────────────────────────
//
// ECSDiagnostics tracks structural metrics about the ECS registry.
// It is populated on demand (not continuously — zero hot-path impact).

// QueryCount tracks how many times a specific query was executed.
type QueryCount struct {
	Name      string
	Count     uint64
	TotalTime time.Duration
	LastTime  time.Duration
	MinTime   time.Duration
	MaxTime   time.Duration
}

// ECSDiagnostics holds runtime diagnostic data for the ECS layer.
type ECSDiagnostics struct {
	mu sync.Mutex

	// EntityCount is the total number of active entities.
	EntityCount int `json:"entity_count"`

	// ComponentCounts maps component type name to count.
	ComponentCounts map[string]int `json:"component_counts"`

	// QueryCounts tracks execution counts for each registered query.
	QueryCounts map[string]*QueryCount `json:"query_counts"`

	// LastScanTime is when the diagnostic data was last refreshed.
	LastScanTime string `json:"last_scan_time"`

	// LeakReport holds the most recent leak detector results.
	LeakReport *LeakReport `json:"leak_report,omitempty"`
}

// GlobalECSStats is the singleton ECS diagnostics instance.
var GlobalECSStats = &ECSDiagnostics{
	ComponentCounts: make(map[string]int),
	QueryCounts:     make(map[string]*QueryCount),
}

// RefreshDiagnostics collects current ECS state metrics.
// Call this on-demand (admin command, MCP handler, periodic scan).
// NOT to be called on the hot-path game loop.
func (d *ECSDiagnostics) RefreshDiagnostics(reg *Registry) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Entity count
	allEntities := reg.GetAllEntities()
	d.EntityCount = len(allEntities)

	// Component counts by type
	d.ComponentCounts = map[string]int{
		"position":      len(reg.positions.dense),
		"connection":    len(reg.conns.dense),
		"metadata":      len(reg.metadata.dense),
		"stats":         len(reg.stats.dense),
		"ai":            len(reg.ai.dense),
		"inventory":     len(reg.inventories.dense),
		"lifetime":      len(reg.lifetimes.dense),
		"item_template": len(reg.itemTemplates.dense),
		"equipment":     len(reg.equipment.dense),
		"party":         len(reg.parties.dense),
		"party_member":  len(reg.partyMembers.dense),
		"effects":       len(reg.effects.dense),
	}

	d.LastScanTime = time.Now().Format("2006-01-02 15:04:05")
}

// RecordQuery records a query execution for diagnostics.
// This is called by the Query layer — not hot-path, but called frequently
// enough that the implementation must be as cheap as possible.
func (d *ECSDiagnostics) RecordQuery(name string, duration time.Duration) {
	// Fast path: load existing entry
	d.mu.Lock()

	qc, ok := d.QueryCounts[name]
	if !ok {
		qc = &QueryCount{
			Name:    name,
			MinTime: duration,
			MaxTime: duration,
		}
		d.QueryCounts[name] = qc
	}

	atomic.AddUint64(&qc.Count, 1)
	qc.LastTime = duration
	qc.TotalTime += duration
	if duration < qc.MinTime {
		qc.MinTime = duration
	}
	if duration > qc.MaxTime {
		qc.MaxTime = duration
	}

	d.mu.Unlock()
}

// GetDiagnostics returns a copy of the current diagnostic data.
func (d *ECSDiagnostics) GetDiagnostics() ECSDiagnostics {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Deep copy the counts map
	compCounts := make(map[string]int, len(d.ComponentCounts))
	for k, v := range d.ComponentCounts {
		compCounts[k] = v
	}

	queryCounts := make(map[string]*QueryCount, len(d.QueryCounts))
	for k, v := range d.QueryCounts {
		qc := &QueryCount{
			Name:      v.Name,
			Count:     atomic.LoadUint64(&v.Count),
			TotalTime: v.TotalTime,
			LastTime:  v.LastTime,
			MinTime:   v.MinTime,
			MaxTime:   v.MaxTime,
		}
		queryCounts[k] = qc
	}

	var leakCopy *LeakReport
	if d.LeakReport != nil {
		lr := *d.LeakReport
		leakCopy = &lr
	}

	return ECSDiagnostics{
		EntityCount:     d.EntityCount,
		ComponentCounts: compCounts,
		QueryCounts:     queryCounts,
		LastScanTime:    d.LastScanTime,
		LeakReport:      leakCopy,
	}
}

// GetSnapshotMetrics returns ECS metrics in a simple format for the admin dashboard.
type ECSSnapshotMetrics struct {
	EntityCount int               `json:"entity_count"`
	Components  map[string]int    `json:"components"`
	Queries     map[string]uint64 `json:"queries"`
}

// Snapshot returns a lightweight snapshot of ECS metrics (allocation allowed — admin path).
func (d *ECSDiagnostics) Snapshot() ECSSnapshotMetrics {
	d.mu.Lock()
	defer d.mu.Unlock()

	compMap := make(map[string]int, len(d.ComponentCounts))
	for k, v := range d.ComponentCounts {
		compMap[k] = v
	}

	qMap := make(map[string]uint64, len(d.QueryCounts))
	for k, v := range d.QueryCounts {
		qMap[k] = atomic.LoadUint64(&v.Count)
	}

	return ECSSnapshotMetrics{
		EntityCount: d.EntityCount,
		Components:  compMap,
		Queries:     qMap,
	}
}

// ─── Component Leak Detector ─────────────────────────────────────────────────
//
// The ComponentLeakDetector scans the ECS registry for integrity issues:
//
//  1. Orphan Entities: Entities missing required components (e.g., a monster
//     without Position or Stats). These indicate a spawn bug.
//
//  2. Forgotten Components: Components stored without a parent entity
//     (no Metadata). These indicate a DeleteComponent call was missed.
//
//  3. Invalid References: Entity IDs that reference entities that no longer
//     exist (e.g., PartyComponent.LeaderID pointing to a removed entity).

// LeakReport summarizes the results of a leak detection scan.
type LeakReport struct {
	OrphanCount       int         `json:"orphan_count"`
	ForgottenCount    int         `json:"forgotten_count"`
	InvalidRefCount   int         `json:"invalid_ref_count"`
	TotalScanned      int         `json:"total_scanned"`
	ScanTime          string      `json:"scan_time"`
	Orphans           []LeakEntry `json:"orphans,omitempty"`
	Forgotten         []LeakEntry `json:"forgotten,omitempty"`
	InvalidReferences []LeakEntry `json:"invalid_references,omitempty"`
	Warnings          []string    `json:"warnings,omitempty"`
}

// LeakEntry describes a single detected leak.
type LeakEntry struct {
	EntityID    Entity `json:"entity_id"`
	Description string `json:"description"`
	Detail      string `json:"detail,omitempty"`
}

// GlobalLeakDetector is the singleton leak detector instance.
var GlobalLeakDetector = &ComponentLeakDetector{}

// ComponentLeakDetector scans the ECS registry for memory leaks and logic bugs.
type ComponentLeakDetector struct {
	mu sync.Mutex
}

// Scan performs a comprehensive leak detection scan on the given registry.
// Returns a LeakReport with all detected issues.
func (ld *ComponentLeakDetector) Scan(reg *Registry) *LeakReport {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	start := time.Now()
	report := &LeakReport{
		ScanTime: start.Format("2006-01-02 15:04:05"),
	}

	// ── Phase 1: Check for orphan entities (metadata without position/stats) ──
	reg.metadata.Range(func(id Entity, meta MetadataComponent) bool {
		report.TotalScanned++

		_, hasPos := reg.positions.Get(id)
		_, hasStats := reg.stats.Get(id)

		if !hasPos && !hasStats {
			entry := LeakEntry{
				EntityID:    id,
				Description: "Entity has Metadata but NO Position or Stats",
				Detail:      "Type=" + meta.Type.String() + ", Name=" + meta.Name,
			}
			report.Orphans = append(report.Orphans, entry)
			report.OrphanCount++
		} else if !hasPos {
			entry := LeakEntry{
				EntityID:    id,
				Description: "Entity has Metadata but NO Position component",
				Detail:      "Type=" + meta.Type.String() + ", Name=" + meta.Name,
			}
			report.Orphans = append(report.Orphans, entry)
			report.OrphanCount++
		} else if !hasStats {
			entry := LeakEntry{
				EntityID:    id,
				Description: "Entity has Metadata but NO Stats component",
				Detail:      "Type=" + meta.Type.String() + ", Name=" + meta.Name,
			}
			report.Orphans = append(report.Orphans, entry)
			report.OrphanCount++
		}
		return true
	})

	// ── Phase 2: Check for forgotten components (component without Metadata) ──
	checkForgotten := func(storeName string, storeFunc func(func(Entity) bool)) {
		var forgotten []LeakEntry
		storeFunc(func(id Entity) bool {
			_, hasMeta := reg.metadata.Get(id)
			if !hasMeta {
				forgotten = append(forgotten, LeakEntry{
					EntityID:    id,
					Description: "Orphan " + storeName + " component (no parent Metadata)",
					Detail:      "Component exists but entity " + entityIDToString(id) + " has no Metadata",
				})
			}
			return true
		})
		if len(forgotten) > 0 {
			report.Forgotten = append(report.Forgotten, forgotten...)
			report.ForgottenCount += len(forgotten)
		}
	}

	// Check all component stores for orphan components
	checkForgotten("Position", func(f func(Entity) bool) {
		reg.positions.Range(func(id Entity, _ PositionComponent) bool { return f(id) })
	})
	checkForgotten("Stats", func(f func(Entity) bool) {
		reg.stats.Range(func(id Entity, _ StatsComponent) bool { return f(id) })
	})
	checkForgotten("AI", func(f func(Entity) bool) {
		reg.ai.Range(func(id Entity, _ AIComponent) bool { return f(id) })
	})
	checkForgotten("Inventory", func(f func(Entity) bool) {
		reg.inventories.Range(func(id Entity, _ InventoryComponent) bool { return f(id) })
	})
	checkForgotten("Equipment", func(f func(Entity) bool) {
		reg.equipment.Range(func(id Entity, _ EquipmentComponent) bool { return f(id) })
	})
	checkForgotten("Connection", func(f func(Entity) bool) {
		reg.conns.Range(func(id Entity, _ ConnectionComponent) bool { return f(id) })
	})
	checkForgotten("Effects", func(f func(Entity) bool) {
		reg.effects.Range(func(id Entity, _ EffectsComponent) bool { return f(id) })
	})
	checkForgotten("Party", func(f func(Entity) bool) {
		reg.parties.Range(func(id Entity, _ PartyComponent) bool { return f(id) })
	})
	checkForgotten("PartyMember", func(f func(Entity) bool) {
		reg.partyMembers.Range(func(id Entity, _ PartyMemberComponent) bool { return f(id) })
	})
	checkForgotten("Lifetime", func(f func(Entity) bool) {
		reg.lifetimes.Range(func(id Entity, _ LifetimeComponent) bool { return f(id) })
	})
	checkForgotten("ItemTemplate", func(f func(Entity) bool) {
		reg.itemTemplates.Range(func(id Entity, _ ItemTemplateComponent) bool { return f(id) })
	})

	// ── Phase 3: Check for invalid entity references ────────────────────────
	// Party leader references
	reg.parties.Range(func(id Entity, party PartyComponent) bool {
		if party.LeaderID != 0 && party.LeaderID != id {
			_, hasMeta := reg.metadata.Get(party.LeaderID)
			if !hasMeta {
				report.InvalidReferences = append(report.InvalidReferences, LeakEntry{
					EntityID:    id,
					Description: "Party leader reference to non-existent entity",
					Detail:      "Party #" + entityIDToString(id) + " leader=#" + entityIDToString(party.LeaderID) + " (missing)",
				})
				report.InvalidRefCount++
			}
		}
		// Check each member reference
		for _, memberID := range party.MemberIDs {
			_, hasMeta := reg.metadata.Get(memberID)
			if !hasMeta {
				report.InvalidReferences = append(report.InvalidReferences, LeakEntry{
					EntityID:    id,
					Description: "Party member reference to non-existent entity",
					Detail:      "Party #" + entityIDToString(id) + " member=#" + entityIDToString(memberID) + " (missing)",
				})
				report.InvalidRefCount++
			}
		}
		return true
	})

	// PartyMember references to parent party
	reg.partyMembers.Range(func(id Entity, pm PartyMemberComponent) bool {
		_, hasMeta := reg.metadata.Get(pm.PartyID)
		if !hasMeta {
			report.InvalidReferences = append(report.InvalidReferences, LeakEntry{
				EntityID:    id,
				Description: "PartyMember references non-existent party entity",
				Detail:      "Member #" + entityIDToString(id) + " party=#" + entityIDToString(pm.PartyID) + " (missing)",
			})
			report.InvalidRefCount++
		}
		return true
	})

	// AI Target references (monster chasing a non-existent entity)
	reg.ai.Range(func(id Entity, ai AIComponent) bool {
		if ai.TargetID != 0 {
			_, hasMeta := reg.metadata.Get(ai.TargetID)
			if !hasMeta {
				report.InvalidReferences = append(report.InvalidReferences, LeakEntry{
					EntityID:    id,
					Description: "AI TargetID references non-existent entity",
					Detail:      "Monster #" + entityIDToString(id) + " target=#" + entityIDToString(ai.TargetID) + " (missing)",
				})
				report.InvalidRefCount++
			}
		}
		return true
	})

	// Compile warnings if anything was found
	if report.OrphanCount > 0 {
		report.Warnings = append(report.Warnings,
			entityCountToString(report.OrphanCount)+" orphan entities found (missing Position/Stats)")
	}
	if report.ForgottenCount > 0 {
		report.Warnings = append(report.Warnings,
			entityCountToString(report.ForgottenCount)+" forgotten components found (no parent Metadata)")
	}
	if report.InvalidRefCount > 0 {
		report.Warnings = append(report.Warnings,
			entityCountToString(report.InvalidRefCount)+" invalid entity references found (dead pointers)")
	}

	// Store leak report in diagnostics
	GlobalECSStats.mu.Lock()
	GlobalECSStats.LeakReport = report
	GlobalECSStats.mu.Unlock()

	elapsed := time.Since(start)
	perf.GlobalTickMonitor.RecordTick(elapsed)

	return report
}

// entityIDToString converts an Entity to a string for diagnostics.
func entityIDToString(id Entity) string {
	if id == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	n := uint64(id)
	for n > 0 || pos == len(buf) {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

// entityCountToString formats an integer count as a string for diagnostic messages.
func entityCountToString(count int) string {
	if count == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	n := count
	for n > 0 || pos == len(buf) {
		pos--
		buf[pos] = byte('0' + byte(n%10))
		n /= 10
	}
	return string(buf[pos:])
}
