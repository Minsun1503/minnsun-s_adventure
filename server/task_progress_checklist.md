# Task Progress - Roadmap v4

## Phase 1 - Combat Accumulator (S-TIER)
- [x] Create server/game/combat_accumulator.go with CombatAccumulator struct
- [x] Modify server/world/worker.go - Add CombatBuffer to MapWorker, init & Flush in Tick
- [x] Modify server/game/combat.go - DamageSystem delegates to combat buffer
- [x] Modify server/game/skill_pipeline.go - stageEffectApplication delegates to combat buffer
- [x] Run go vet and verify compilation

## Phase 1 - Save Consistency (S-TIER)
- [ ] Modify `server/db/save_engine.go` to wrap Upsert Character and Inventory in a single `BeginTx` block
- [ ] Ensure any error in the SQL transaction triggers a strict `Rollback()`
- [ ] Add `snapshot_data JSON` backup field logic to the Upsert query

## Phase 1 - Cross-Map Migration (S-TIER)
- [ ] Create `server/ecs/transfer_component.go` for Two-Phase Commit locking
- [ ] Update `server/world/partition.go` to implement lock-copy-spawn-commit workflow for `processTransfer`
- [ ] Update `MovementSystem` and `CombatSystem` to ignore entities with `TransferComponent`

## Phase 2 - Memory & State Safety (A-TIER)
- [x] Fix Entity Lifecycle Leak (`ecs.go` RemoveEntity calls Close/Nil)
- [x] Fix AOI Worst Case (`MaxAOIWatchers = 50` culling)
- [x] Global State Cleanup (Replaced `GlobalRegistry` with `DefaultRegistry`)