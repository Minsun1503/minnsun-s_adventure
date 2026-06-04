# Phase 4 - Observability Progress

## [10] Server Metrics Panel (★★★★☆)
- [x] Analyze existing architecture (perf monitors, MCP server, game loop, ECS registry)
- [x] Add global perf monitor instances to `server/peakgo/perf/perf.go`
- [x] Create `server/network/metrics_api.go` - admin HTTP server + HTML/JS UI
- [x] Wire tick timing into game loop
- [x] Wire packet monitoring into transport layer (globals ready)
- [x] Integrate admin server into `server.go`
- [x] Verify build compiles

## [11] ECS Inspector (★★★☆☆)
- [x] Create `server/network/inspector_api.go` - entity inspection endpoints
- [x] Add `/debug/entities` - list all entities with snapshots
- [x] Add `/debug/entity?id=X` - detailed single entity view
- [x] Extend admin server mux with inspector routes
- [x] Verify build compiles

## [12] Performance Alerts (★★★☆☆)
- [x] Create `server/peakgo/perf/monitor.go` - threshold alert hooks
- [x] Add tick duration > 50ms warning
- [x] Add heap > 2GB warning
- [x] Add save queue > 80% full warning
- [x] Wire alert monitor into `server.go`
- [x] Verify build compiles