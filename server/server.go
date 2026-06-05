package main

import (
	"flag"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync/atomic"
	"syscall"
	"time"

	"server/auth"
	"server/db"
	"server/ecs"
	"server/game"
	"server/logger"
	"server/mcp"
	"server/models"
	"server/network"
	"server/peakgo/config"
	"server/peakgo/perf"
	"server/protocol"
	"server/systems"
	"server/transport"
	"server/world"
)

func main() {
	devMode := flag.Bool("dev", false, "Chạy server ở chế độ Development (không cần Database)")
	cpuprofile := flag.String("cpuprofile", "", "Write cpu profile to file")
	memprofile := flag.String("memprofile", "", "Write memory profile to this file")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			logger.Error("Could not create CPU profile: %v", err)
			return
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// Initialize hot-reload config before any other subsystem.
	config.InitConfig("data/config.json")

	logger.Init() // Must be early: starts async log worker (reads config via peakgo/config)

	game.InitializeItemRegistry()
	game.InitializeLootTables()
	world.InitializeCollisionMaps()
	world.InitNavMesh() // Build global NavMesh from collision data for AI pathfinding
	models.InitializeSkillRegistry()

	// Sync item registry to MCP for inventory display.
	for id, t := range game.ItemRegistry {
		mcp.ItemRegistryGlobal[id] = struct {
			ID        uint64
			Name      string
			HealValue int
			SlotType  string
			BonusDam  int
			BonusHP   int
		}{
			ID: t.ID, Name: t.Name,
			HealValue: t.HealValue,
			SlotType:  t.SlotType,
			BonusDam:  t.BonusDam,
			BonusHP:   t.BonusHP,
		}
	}

	if *devMode {
		logger.Info("[BOOT] Khởi động bằng cờ -dev: Bỏ qua kết nối MySQL.")
		models.InitializeDatabase("")
	} else {
		models.InitializeDatabase("root:root@tcp(127.0.0.1:3306)/?parseTime=true")
	}

	db.StartSaveWorkerEngine()

	// Initialize the ECS Entity ID counter to the maximum character ID in the DB to avoid session ID collisions.
	// Skip if DB is not available (dev_mode).
	if models.DBEngine != nil {
		var maxID uint64
		if err := models.DBEngine.QueryRow("SELECT COALESCE(MAX(id), 0) FROM characters").Scan(&maxID); err == nil {
			ecs.DefaultRegistry.SetNextID(maxID)
		} else {
			logger.Error("[BOOT] Failed to scan max character ID: %v", err)
		}
	} else {
		logger.Info("[BOOT] No DB available — ECS ID counter starts from default (dev_mode).")
	}

	// ─── Crash Recovery: Load World Snapshot ─────────────────────────────────
	// Attempt to restore the world state from the most recent snapshot.
	// If a snapshot exists, entities are restored to the ECS registry.
	// This provides fast recovery (within seconds) instead of respawning all
	// monsters from scratch. Fresh monster templates + saved player state
	// coexist harmoniously — monster template IDs are negative/partitioned
	// while player entity IDs are positive and come from the DB.
	snapshot := world.LoadLatestSnapshot()
	if snapshot != nil {
		restored := world.RestoreWorldFromSnapshot(snapshot)
		logger.Info("[BOOT] Crash recovery: restored %d entities from world snapshot.", restored)
	} else {
		logger.Info("[BOOT] No crash recovery data — spawning fresh monsters.")
	}

	templates, err := models.LoadMonster("data/monster_templates.json")
	if err != nil {
		logger.Error("CRITICAL SERVER BOOT ERROR: %v", err)
		return
	}
	logger.Info("[BOOT] Loaded %d monster templates.", len(templates))

	spawned := 0
	for _, t := range templates {
		id, err := models.SpawnFromDefaultPosition(t.ID)
		if err != nil {
			logger.Warn("[BOOT] Failed to spawn template %d (%s): %v", t.ID, t.Name, err)
			continue
		}
		if pos, ok := ecs.DefaultRegistry.GetPosition(id); ok {
			world.GlobalSpatialGrid.UpdateEntityPosition(id, pos)
		}
		spawned++
	}
	logger.Info("[BOOT] Spawned %d live monster instances into ECS.", spawned)

	lis, err := net.Listen("tcp", ":1503")
	if err != nil {
		logger.Error("[BOOT] Failed to bind port: %v", err)
		return
	}
	defer lis.Close()
	logger.Info("[BOOT] Server listening on %s", lis.Addr())

	world.InitAOIManager()

	systems.StartGameLoop()
	auth.StartLoginWorkerPool(100) // Start 100 connection login workers for mass testing

	// Start the MCP JSON-RPC HTTP server for AI agent inspection.
	// Uses port 8080 by default; configure via MCP_PORT env var or data/config.json.
	mcp.Start(mcp.Config{Port: 8080})
	logger.Info("[BOOT] MCP admin interface available on http://localhost:8080/mcp")

	// Start the Admin Dashboard HTTP server on port 9090 (isolated admin port).
	// Serves a real-time HTML/JS metrics dashboard at / with JSON endpoints at
	// /debug/state, /debug/perf, and /debug/entities.
	adminServer := network.NewAdminServer(":9090")
	adminServer.Start()
	logger.Info("[BOOT] Admin dashboard available on http://localhost:9090/")

	// Start the periodic background alert monitor and metrics logger goroutine.
	// Samples memory, logs performance metrics, and checks thresholds every 5 seconds.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		var lastReport perf.Report
		for range ticker.C {
			// Trigger a forced memory sample for accurate reporting
			snap := perf.GlobalMemMonitor.Sample()

			report := perf.Collect(perf.GlobalTickMonitor, perf.GlobalPacketMonitor, perf.GlobalMemMonitor)

			aoiSec := (report.AoiQueries - lastReport.AoiQueries) / 5
			bcastSec := (report.Broadcasts - lastReport.Broadcasts) / 5

			// Print human-readable metrics report
			logger.Info("[METRICS] TickAvg: %v | HeapAlloc: %d MB | GC: %d | Goroutines: %d | AOI/s: %d | Broadcast/s: %d",
				report.TickAvg, report.Alloc/1024/1024, report.NumGC, report.Goroutines, aoiSec, bcastSec)

			lastReport = report

			// Alert on threshold breaches
			if snap != nil {
				perf.GlobalAlertMonitor.CheckHeapSize(snap.Alloc)
			}

			// Check save queue depth
			queueLen := len(db.SaveQueue)
			perf.GlobalAlertMonitor.CheckSaveQueue(queueLen)
		}
	}()
	logger.Info("[BOOT] Performance alert monitor started (5s interval).")

	// Start the WebSocket listener for WebGL clients on port 8081.
	// Runs in its own goroutine — does not block the TCP accept loop.
	go transport.StartWebSocketListener(":8081")
	logger.Info("[BOOT] WebSocket transport listening on ws://localhost:8081/ws")

	// Start the periodic world snapshot goroutine.
	// Saves all active entities every 5 minutes (configurable via SnapshotInterval).
	world.StartPeriodicSnapshot()
	logger.Info("[BOOT] Periodic world snapshot started (interval: %v).", world.SnapshotInterval)

	// Set up OS signal catching for graceful shutdown.
	// On SIGINT (Ctrl+C) or SIGTERM, we flush the save queue and exit cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var shuttingDown atomic.Bool

	go func() {
		sig := <-sigCh
		shuttingDown.Store(true)
		logger.Info("[SHUTDOWN] Received signal %v — starting graceful shutdown...", sig)

		// Step 1: Close the TCP listener to stop accepting new connections.
		lis.Close()
		logger.Info("[SHUTDOWN] TCP listener closed.")

		// Step 2: Take a shutdown snapshot for crash recovery.
		world.TakeShutdownSnapshot()
		logger.Info("[SHUTDOWN] Shutdown snapshot taken.")

		// Step 3: Flush the save queue — drain all pending snapshots to DB.
		db.FlushSaveQueue()

		// Step 4: Shutdown all MapWorkers gracefully.
		if world.GlobalWorld != nil {
			world.GlobalWorld.ShutdownAll()
		}
		logger.Info("[SHUTDOWN] All MapWorkers shut down.")

		if *memprofile != "" {
			f, err := os.Create(*memprofile)
			if err != nil {
				logger.Error("Could not create memory profile: %v", err)
			} else {
				runtime.GC() // get up-to-date statistics
				if err := pprof.WriteHeapProfile(f); err != nil {
					logger.Error("Could not write memory profile: %v", err)
				}
				f.Close()
			}
		}

		// Step 5: Log shutdown complete and flush all logs.
		logger.Info("[SHUTDOWN] Server shut down gracefully.")
		logger.Flush()
		os.Exit(0)
	}()

	for {
		conn, err := lis.Accept()
		if err != nil {
			if shuttingDown.Load() {
				// Block and let the shutdown handler exit cleanly via os.Exit(0)
				select {}
			}
			logger.Error("[ACCEPT] Error: %v", err)
			return
		}

		select {
		case auth.LoginQueue <- conn:
		default:
			// Queue full! Tell the client and drop connection cleanly.
			logger.Warn("[ACCEPT] Login queue full — dropping connection from %s", conn.RemoteAddr())
			protocol.SendErrorPacket(conn, protocol.ErrCodeServerFull, "Server login queue is full. Please try again later.")
			conn.Close()
		}
	}
}
