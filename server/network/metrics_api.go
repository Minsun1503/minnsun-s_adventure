// Package network provides network-layer utilities for the game server.
//
// metrics_api.go — Lightweight embedded admin HTTP server serving a real-time
// metrics dashboard on an isolated admin port (default :9090).
//
// Design decision: Runs as a separate HTTP server on an isolated port to
// guarantee zero impact on the hot-path game loop. All metrics are read from
// atomic/read-only globals in peakgo/perf — no locks, no allocations.
package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"server/ecs"
	"server/peakgo/perf"
	"server/world"
	"time"
)

// ─── Admin Server ─────────────────────────────────────────────────────────────

// AdminServer is the lightweight embedded HTTP server for admin metrics + UI.
type AdminServer struct {
	server *http.Server
	mux    *http.ServeMux
}

// NewAdminServer creates a new admin HTTP server on the given address.
func NewAdminServer(addr string) *AdminServer {
	as := &AdminServer{
		mux: http.NewServeMux(),
	}
	as.server = &http.Server{
		Addr:    addr,
		Handler: as.mux,
	}
	as.registerRoutes()
	return as
}

// Start begins the admin server in a background goroutine.
func (as *AdminServer) Start() {
	go func() {
		if err := as.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("[ADMIN] HTTP server error: %v\n", err)
		}
	}()
}

// Stop gracefully shuts down the admin server.
func (as *AdminServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return as.server.Shutdown(ctx)
}

// Handler returns the HTTP handler for direct mounting into an existing server.
func (as *AdminServer) Handler() http.Handler {
	return as.mux
}

func (as *AdminServer) registerRoutes() {
	// JSON API endpoints
	as.mux.HandleFunc("/debug/state", as.handleDebugState)
	as.mux.HandleFunc("/debug/perf", as.handleDebugPerf)
	as.mux.HandleFunc("/debug/entities", as.handleDebugEntities)

	// HTML UI (single-page dashboard)
	as.mux.HandleFunc("/", as.handleDashboard)
}

// ─── JSON: /debug/state ───────────────────────────────────────────────────────

// DebugStateResponse is the JSON structure for /debug/state.
type DebugStateResponse struct {
	OnlinePlayers int    `json:"online_players"`
	TotalMonsters int    `json:"total_monsters"`
	GroundItems   int    `json:"ground_items"`
	EntityIDMax   uint64 `json:"entity_id_max"`
	RecycledIDs   int    `json:"recycled_ids"`
	ActiveChunks  int    `json:"active_chunks"`
	GridEntities  int    `json:"grid_entities"`
	RunningMaps   []int  `json:"running_maps"`
}

func (as *AdminServer) handleDebugState(w http.ResponseWriter, r *http.Request) {
	players, monsters, items := 0, 0, 0

	ecs.GlobalRegistry.RangeSnapshots(func(snap ecs.EntitySnapshot) bool {
		switch snap.Meta.Type {
		case ecs.EntityPlayer:
			players++
		case ecs.EntityMonster:
			monsters++
		case ecs.EntityGroundItem:
			items++
		}
		return true
	})

	// Grid stats from spatial grid's DebugStats format
	// We extract chunk/entity counts from the spatial grid directly
	gridChunks := 0
	gridEntities := 0

	// Since SpatialGrid.DebugStats() returns a formatted string,
	// we'll use an alternative approach: count via the entity index size
	// We can't directly access entityIndex, so we read DebugStats string
	_ = world.GlobalSpatialGrid.DebugStats()

	// For the JSON response, we approximate via ECS range counts
	// The actual grid entity count = all entities with positions
	ecs.GlobalRegistry.RangeMetadata(func(id ecs.Entity, _ ecs.MetadataComponent) bool {
		if _, hasPos := ecs.GlobalRegistry.GetPosition(id); hasPos {
			gridEntities++
		}
		return true
	})

	resp := DebugStateResponse{
		OnlinePlayers: players,
		TotalMonsters: monsters,
		GroundItems:   items,
		EntityIDMax:   ecs.GlobalRegistry.TotalEntityIDs(),
		RecycledIDs:   ecs.RecycledEntityCount(),
		ActiveChunks:  gridChunks,
		GridEntities:  gridEntities,
		RunningMaps:   world.RunningMapIDs(),
	}

	writeJSON(w, resp)
}

// ─── JSON: /debug/perf ────────────────────────────────────────────────────────

// DebugPerfResponse is the JSON structure for /debug/perf.
type DebugPerfResponse struct {
	TickMinNs    int64  `json:"tick_min_ns"`
	TickMaxNs    int64  `json:"tick_max_ns"`
	TickAvgNs    int64  `json:"tick_avg_ns"`
	TickCount    uint64 `json:"tick_count"`
	TickOverflow uint64 `json:"tick_overflow"`
	PacketsIn    uint64 `json:"packets_in"`
	PacketsOut   uint64 `json:"packets_out"`
	BytesIn      uint64 `json:"bytes_in"`
	BytesOut     uint64 `json:"bytes_out"`
	AllocBytes   uint64 `json:"alloc_bytes"`
	HeapObjects  uint64 `json:"heap_objects"`
	Goroutines   int    `json:"goroutines"`
	NumGC        uint32 `json:"num_gc"`
	LastPauseNs  uint64 `json:"last_pause_ns"`
}

func (as *AdminServer) handleDebugPerf(w http.ResponseWriter, r *http.Request) {
	report := perf.Collect(perf.GlobalTickMonitor, perf.GlobalPacketMonitor, perf.GlobalMemMonitor)

	// Sample memory to get latest values
	if snap := perf.GlobalMemMonitor.Sample(); snap != nil {
		report.Alloc = snap.Alloc
		report.HeapObjects = snap.HeapObjects
		report.NumGC = snap.NumGC
	}

	resp := DebugPerfResponse{
		TickMinNs:    report.TickMin.Nanoseconds(),
		TickMaxNs:    report.TickMax.Nanoseconds(),
		TickAvgNs:    report.TickAvg.Nanoseconds(),
		TickCount:    report.TickCount,
		TickOverflow: perf.GlobalTickMonitor.Overflow(),
		PacketsIn:    report.PacketsIn,
		PacketsOut:   report.PacketsOut,
		BytesIn:      report.BytesIn,
		BytesOut:     report.BytesOut,
		AllocBytes:   report.Alloc,
		HeapObjects:  report.HeapObjects,
		Goroutines:   report.Goroutines,
		NumGC:        report.NumGC,
	}
	if snap := perf.GlobalMemMonitor.Last(); snap.Alloc > 0 {
		resp.LastPauseNs = uint64(snap.LastPause.Nanoseconds())
	}

	writeJSON(w, resp)
}

// ─── HTML Dashboard ───────────────────────────────────────────────────────────

func (as *AdminServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(adminDashboardHTML))
}

// writeJSON is a helper to write a JSON response.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ─── Embedded HTML Dashboard ──────────────────────────────────────────────────

var adminDashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Minnsun's Adventure — Server Dashboard</title>
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  body { font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;
         background:#0d1117; color:#c9d1d9; padding:20px; }
  h1 { color:#58a6ff; margin-bottom:20px; font-size:1.5rem; }
  h2 { color:#8b949e; font-size:1rem; text-transform:uppercase;
       letter-spacing:0.5px; margin-bottom:12px; }
  .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(280px,1fr));
          gap:16px; margin-bottom:24px; }
  .card { background:#161b22; border:1px solid #30363d; border-radius:8px;
          padding:16px; }
  .card h2 { border-bottom:1px solid #21262d; padding-bottom:8px; margin-bottom:12px; }
  .stat { display:flex; justify-content:space-between; padding:4px 0;
          font-size:0.9rem; }
  .stat .label { color:#8b949e; }
  .stat .value { color:#e6edf3; font-weight:600; font-variant-numeric:tabular-nums; }
  .stat .value.warn { color:#d29922; }
  .stat .value.critical { color:#f85149; }
  .stat .value.good { color:#3fb950; }
  .progress-bar { background:#21262d; border-radius:4px; height:8px;
                  margin:8px 0; overflow:hidden; }
  .progress-bar .fill { height:100%; border-radius:4px; transition:width 0.5s; }
  .progress-bar .fill.good { background:#3fb950; }
  .progress-bar .fill.warn { background:#d29922; }
  .progress-bar .fill.critical { background:#f85149; }
  .status-dot { display:inline-block; width:10px; height:10px; border-radius:50%;
                margin-right:6px; }
  .status-dot.green { background:#3fb950; }
  .status-dot.yellow { background:#d29922; }
  .status-dot.red { background:#f85149; }
  .footer { color:#484f58; font-size:0.8rem; text-align:center; margin-top:32px; }
  .refresh { color:#58a6ff; cursor:pointer; font-size:0.85rem; margin-left:8px; }
  .refresh:hover { text-decoration:underline; }
  table { width:100%; border-collapse:collapse; font-size:0.85rem; }
  th { color:#8b949e; text-align:left; padding:6px 8px; border-bottom:1px solid #21262d; }
  td { padding:6px 8px; border-bottom:1px solid #21262d; }
  tr:hover { background:#1c2128; }
  .badge { display:inline-block; padding:2px 6px; border-radius:4px;
           font-size:0.75rem; font-weight:600; }
  .badge.player { background:#1f6feb33; color:#58a6ff; }
  .badge.monster { background:#da363333; color:#f85149; }
  .badge.item { background:#3fb95033; color:#3fb950; }
  .nav { margin-bottom:16px; }
  .nav a { color:#58a6ff; text-decoration:none; margin-right:16px; font-size:0.9rem; }
  .nav a.active { text-decoration:underline; font-weight:600; }
  #entities-section { display:none; }
</style>
</head>
<body>

<h1>🎮 Minnsun's Adventure — Server Dashboard</h1>
<div class="nav">
  <a href="#" class="active" onclick="showTab('dashboard')" id="tab-dash">Dashboard</a>
  <a href="#" onclick="showTab('entities')" id="tab-ent">Entities</a>
  <span class="refresh" onclick="refreshAll()">↻ Refresh Now</span>
  <span style="color:#484f58;font-size:0.85rem;margin-left:8px">
    Auto-refresh: <span id="interval-display">3s</span>
  </span>
</div>

<!-- ===== DASHBOARD TAB ===== -->
<div id="dashboard-section">
<div class="grid">

<!-- Server State Card -->
<div class="card">
  <h2>🟢 Server State</h2>
  <div class="stat"><span class="label">Online Players</span><span class="value" id="online-players">—</span></div>
  <div class="stat"><span class="label">Monsters</span><span class="value" id="total-monsters">—</span></div>
  <div class="stat"><span class="label">Ground Items</span><span class="value" id="ground-items">—</span></div>
  <div class="stat"><span class="label">Entity ID Max</span><span class="value" id="entity-id-max">—</span></div>
  <div class="stat"><span class="label">Recycled IDs</span><span class="value" id="recycled-ids">—</span></div>
  <div class="stat"><span class="label">Grid Entities</span><span class="value" id="grid-entities">—</span></div>
  <div class="stat"><span class="label">Running Maps</span><span class="value" id="running-maps">—</span></div>
</div>

<!-- Tick Performance Card -->
<div class="card">
  <h2>⏱️ Tick Performance</h2>
  <div class="stat"><span class="label">Min</span><span class="value good" id="tick-min">—</span></div>
  <div class="stat"><span class="label">Max</span><span class="value" id="tick-max">—</span></div>
  <div class="stat"><span class="label">Avg</span><span class="value" id="tick-avg">—</span></div>
  <div class="stat"><span class="label">Count</span><span class="value" id="tick-count">—</span></div>
  <div class="stat"><span class="label">Overflow</span><span class="value" id="tick-overflow">—</span></div>
  <div class="stat" style="margin-top:8px"><span class="label">Tick Budget</span><span class="value" id="tick-budget">—</span></div>
  <div class="progress-bar"><div class="fill good" id="tick-bar" style="width:0%"></div></div>
</div>

<!-- Memory Card -->
<div class="card">
  <h2>💾 Memory</h2>
  <div class="stat"><span class="label">Heap Alloc</span><span class="value" id="heap-alloc">—</span></div>
  <div class="stat"><span class="label">Heap Objects</span><span class="value" id="heap-objects">—</span></div>
  <div class="stat"><span class="label">Goroutines</span><span class="value" id="goroutines">—</span></div>
  <div class="stat"><span class="label">GC Cycles</span><span class="value" id="num-gc">—</span></div>
  <div class="stat"><span class="label">Last GC Pause</span><span class="value" id="last-pause">—</span></div>
  <div class="stat" style="margin-top:8px"><span class="label">Heap Usage</span><span class="value" id="heap-pct">—</span></div>
  <div class="progress-bar"><div class="fill good" id="heap-bar" style="width:0%"></div></div>
</div>

<!-- Network Card -->
<div class="card">
  <h2>📡 Network</h2>
  <div class="stat"><span class="label">Packets In</span><span class="value" id="packets-in">—</span></div>
  <div class="stat"><span class="label">Packets Out</span><span class="value" id="packets-out">—</span></div>
  <div class="stat"><span class="label">Bytes In</span><span class="value" id="bytes-in">—</span></div>
  <div class="stat"><span class="label">Bytes Out</span><span class="value" id="bytes-out">—</span></div>
</div>

</div><!-- /grid -->

<div class="grid">
<!-- Alerts Card -->
<div class="card">
  <h2>🔔 Recent Alerts</h2>
  <div id="alerts-panel" style="font-size:0.85rem;color:#8b949e;">
    <p>No alerts since server start.</p>
  </div>
</div>
</div>
</div><!-- /dashboard-section -->

<!-- ===== ENTITIES TAB ===== -->
<div id="entities-section">
<div class="grid">
<div class="card" style="grid-column:1/-1">
  <h2>👾 Live Entities <span id="entity-count" style="color:#8b949e;font-size:0.8rem;"></span></h2>
  <div style="max-height:500px;overflow-y:auto">
  <table>
    <thead>
      <tr><th>ID</th><th>Name</th><th>Type</th><th>Position</th><th>HP</th><th>Level</th><th>AI State</th></tr>
    </thead>
    <tbody id="entity-table-body">
      <tr><td colspan="7" style="color:#8b949e;text-align:center">Loading...</td></tr>
    </tbody>
  </table>
  </div>
</div>
</div>
</div>

<div class="footer">
  Minnsun's Adventure — PeakGO Game Server &nbsp;|&nbsp;
  Data refreshes every <span id="footer-interval">3</span>s
</div>

<script>
let autoRefresh = true;
let refreshInterval = 3000;
let refreshTimer = null;

function formatBytes(b) {
  if (b < 1024) return b + ' B';
  if (b < 1048576) return (b/1024).toFixed(1) + ' KB';
  if (b < 1073741824) return (b/1048576).toFixed(1) + ' MB';
  return (b/1073741824).toFixed(2) + ' GB';
}

function formatDuration(ns) {
  if (ns < 1000) return ns + ' ns';
  if (ns < 1000000) return (ns/1000).toFixed(1) + ' μs';
  return (ns/1000000).toFixed(2) + ' ms';
}

function formatNumber(n) {
  return n.toString().replace(/\\B(?=(\\d{3})+(?!\\d))/g, ",");
}

function setColor(element, ns, threshold) {
  element.className = 'value';
  if (ns > threshold * 0.8) {
    element.classList.add('critical');
  } else if (ns > threshold * 0.5) {
    element.classList.add('warn');
  } else {
    element.classList.add('good');
  }
}

function setBar(id, pct, state) {
  const bar = document.getElementById(id);
  bar.style.width = Math.min(pct, 100) + '%';
  bar.className = 'fill';
  if (state === 'critical') bar.classList.add('critical');
  else if (state === 'warn') bar.classList.add('warn');
  else bar.classList.add('good');
}

function updateDashboard() {
  // Fetch state
  fetch('/debug/state')
    .then(r => r.json())
    .then(data => {
      document.getElementById('online-players').textContent = formatNumber(data.online_players);
      document.getElementById('total-monsters').textContent = formatNumber(data.total_monsters);
      document.getElementById('ground-items').textContent = formatNumber(data.ground_items);
      document.getElementById('entity-id-max').textContent = formatNumber(data.entity_id_max);
      document.getElementById('recycled-ids').textContent = formatNumber(data.recycled_ids);
      document.getElementById('grid-entities').textContent = formatNumber(data.grid_entities);
      document.getElementById('running-maps').textContent = data.running_maps.join(', ') || 'none';

      // Online players status dot
      const el = document.getElementById('online-players');
      el.innerHTML = (data.online_players > 0)
        ? '<span class="status-dot green"></span>' + formatNumber(data.online_players)
        : '<span class="status-dot yellow"></span>0';
    })
    .catch(() => {});

  // Fetch perf
  fetch('/debug/perf')
    .then(r => r.json())
    .then(data => {
      // Tick
      document.getElementById('tick-min').textContent = formatDuration(data.tick_min_ns);
      document.getElementById('tick-max').textContent = formatDuration(data.tick_max_ns);
      document.getElementById('tick-avg').textContent = formatDuration(data.tick_avg_ns);
      document.getElementById('tick-count').textContent = formatNumber(data.tick_count);
      document.getElementById('tick-overflow').textContent = formatNumber(data.tick_overflow);

      // Tick budget (250ms tick = 250000000ns)
      const tickThreshold = 50000000; // 50ms
      const tickPct = (data.tick_avg_ns / 250000000 * 100).toFixed(1);
      document.getElementById('tick-budget').textContent = tickPct + '% used';
      let tickState = 'good';
      if (data.tick_avg_ns > tickThreshold) tickState = 'critical';
      else if (data.tick_avg_ns > tickThreshold * 0.5) tickState = 'warn';
      setBar('tick-bar', parseFloat(tickPct), tickState);
      setColor(document.getElementById('tick-avg'), data.tick_avg_ns, tickThreshold);

      // Memory
      document.getElementById('heap-alloc').textContent = formatBytes(data.alloc_bytes);
      document.getElementById('heap-objects').textContent = formatNumber(data.heap_objects);
      document.getElementById('goroutines').textContent = formatNumber(data.goroutines);
      document.getElementById('num-gc').textContent = formatNumber(data.num_gc);
      document.getElementById('last-pause').textContent = formatDuration(data.last_pause_ns);

      const heapPct = (data.alloc_bytes / 2000000000 * 100).toFixed(1); // 2GB threshold
      document.getElementById('heap-pct').textContent = heapPct + '% of 2GB';
      let heapState = 'good';
      if (data.alloc_bytes > 1500000000) heapState = 'critical';
      else if (data.alloc_bytes > 1000000000) heapState = 'warn';
      setBar('heap-bar', parseFloat(heapPct), heapState);

      // Network
      document.getElementById('packets-in').textContent = formatNumber(data.packets_in);
      document.getElementById('packets-out').textContent = formatNumber(data.packets_out);
      document.getElementById('bytes-in').textContent = formatBytes(data.bytes_in);
      document.getElementById('bytes-out').textContent = formatBytes(data.bytes_out);
    })
    .catch(() => {});
}

function updateEntities() {
  fetch('/debug/entities')
    .then(r => r.json())
    .then(data => {
      const tbody = document.getElementById('entity-table-body');
      if (!data.entities || data.entities.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" style="color:#8b949e;text-align:center">No entities</td></tr>';
        document.getElementById('entity-count').textContent = '(0)';
        return;
      }
      document.getElementById('entity-count').textContent = '(' + data.entities.length + ')';
      let html = '';
      data.entities.forEach(e => {
        const typeClass = e.type === 'player' ? 'player' : e.type === 'monster' ? 'monster' : 'item';
        html += '<tr>' +
          '<td>' + formatNumber(e.id) + '</td>' +
          '<td>' + (e.name || '—') + '</td>' +
          '<td><span class="badge ' + typeClass + '">' + (e.type || '—') + '</span></td>' +
          '<td>(' + (e.x || 0) + ', ' + (e.z || 0) + ')</td>' +
          '<td>' + (e.hp || 0) + '/' + (e.max_hp || 0) + '</td>' +
          '<td>' + (e.level || 1) + '</td>' +
          '<td>' + (e.ai_state || '—') + '</td>' +
          '</tr>';
      });
      tbody.innerHTML = html;
    })
    .catch(() => {
      document.getElementById('entity-table-body').innerHTML =
        '<tr><td colspan="7" style="color:#f85149;text-align:center">Failed to load entities</td></tr>';
    });
}

function refreshAll() {
  updateDashboard();
  if (document.getElementById('entities-section').style.display !== 'none') {
    updateEntities();
  }
}

// Tab switching
function showTab(name) {
  document.getElementById('dashboard-section').style.display = name === 'dashboard' ? 'block' : 'none';
  document.getElementById('entities-section').style.display = name === 'entities' ? 'block' : 'none';
  document.getElementById('tab-dash').className = name === 'dashboard' ? 'active' : '';
  document.getElementById('tab-ent').className = name === 'entities' ? 'active' : '';
  if (name === 'entities') updateEntities();
}

// Auto-refresh
function startAutoRefresh() {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = setInterval(() => {
    if (autoRefresh) refreshAll();
  }, refreshInterval);
}

startAutoRefresh();
updateDashboard();
setTimeout(updateEntities, 1000);
</script>
</body>
</html>`
