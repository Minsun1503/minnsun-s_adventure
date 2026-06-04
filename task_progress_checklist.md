# Phase 5 & 6 Implementation Checklist

## Phase 5: Combat System
### 13. Combat Profiling (★★★★☆)
- [x] Add benchmarks for combat hot paths (AttackSystem, DamageSystem, DeathSystem)
- [x] Run benchmarks and identify hotspots
  - AttackSystem: ~233 ns/op, 32 B/op, 1 allocs/op
  - DamageSystem: ~82 ns/op, 0 B/op, 0 allocs/op
  - DeathSystem: ~1613 ns/op, 175 B/op, 4 allocs/op (expected: DB + broadcast)
  - StatsToCombatStats: ~3 ns/op, 0 B/op, 0 allocs/op (zero alloc)
- [x] Add zero-alloc profiling test for combat paths

### 14. Skill Execution Pipeline (★★★★☆)
- [x] Create SkillPipeline with stages: Target Selection → Damage Calc → Effect Application → Broadcast
- [x] Refactor AttackSystem to use pipeline stages
- [x] Refactor HandleSkillCastingSystem to use pipeline stages
- [x] Add pipeline tests (8 tests passing)

### 15. Threat Table Cleanup (★★★☆☆)
- [x] Fix memory leak: call Close() on threat tables when monsters die
- [x] Add threat table cleanup in DeathSystem
- [x] Verify memory allocation with benchmarks (threat: 0 B/op, 0 allocs/op)

## Phase 6: Operations
### 16. Live Admin Dashboard Enhancements (★★★★☆)
- [x] Add TPS display to dashboard (fixed 4 TPS display)
- [x] Add save queue size display (size/capacity + percentage gauge)
- [x] Add goroutine growth tracking (delta/rate with color coding)
- [x] Add GC metrics to JSON API and HTML dashboard (avg pause, GC count)
- [x] Add save queue depth gauge to HTML

### 17. Alert System Enhancements (★★★★☆)
- [x] Add goroutine growth anomaly detection via /debug/ops delta
- [x] Add abnormal goroutine growth alert (color-coded in HTML dashboard)
- [x] Add save queue alert enhancements (existing AlertMonitor + dashboard gauge)

### 18. Long-Run Stability Test (★★★★☆)
- [x] Create automated long-run test script (cmd/soaktest)
- [x] Build report generator for memory/GC/queue trends
- [x] Add soak test with combat interactions (HTTP-based metrics polling)