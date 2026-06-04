# Implementation Roadmap v3 - Progress Checklist

## Phase 3: Data Safety

### 7. Save Queue Hardening (★★★★★)
- [x] Implement SaveBuffer with persistent disk-backed fallback
- [x] Add backpressure mechanism to QueuePlayerSave (TryWriteToQueue)
- [x] Add emergency flush that drains buffer to disk on shutdown

### 8. World Snapshot System (★★★★☆)
- [x] Enhanced periodic snapshot to serialize full world state (entities, inventory, party, AI, effects)
- [x] Add snapshot metadata (timestamp, entity count, version, snapshot type)
- [x] Add snapshot to disk serialization (gob format with marker file)

### 9. Crash Recovery (★★★★☆)
- [x] Load latest snapshot on startup (LoadLatestSnapshot with fallback)
- [x] Replay pending saves from disk buffer on drain
- [x] Restore entities, components, inventory, party, effects from snapshot
- [x] Shutdown snapshot for clean recovery

## Phase 4: ECS Maturity

### 10. Query Cache (★★★☆☆)
- [x] Add query planning cache (GlobalQueryCache with atomic pointer swap)
- [x] Cache smallest store selection to avoid repeated planning
- [x] Lock-free read path for hot-path queries

### 11. ECS Diagnostics (★★★☆☆)
- [x] Add entity/component/query count tracking (ECSDiagnostics)
- [x] Add query duration tracking per system (RecordQuery)
- [x] Expose diagnostics via MCP handlers (ecs_diagnostics, ecs_diagnostics_snapshot)

### 12. Component Leak Detector (★★★☆☆)
- [x] Detect orphan entities (missing required components like Position/Stats)
- [x] Detect forgotten components (component without parent Metadata)
- [x] Detect invalid references (party/AI target IDs pointing to non-existent entities)
- [x] Expose via MCP handler (ecs_leak_scan)