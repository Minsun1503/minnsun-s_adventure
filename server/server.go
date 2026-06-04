package main

import (
	"net"

	"server/auth"
	"server/db"
	"server/ecs"
	"server/game"
	"server/logger"
	"server/mcp"
	"server/models"
	"server/protocol"
	"server/systems"
	"server/transport"
	"server/world"
)

func main() {
	logger.Init() // Must be first: reads data/config.json, starts async log worker

	game.InitializeItemRegistry()
	game.InitializeLootTables()
	world.InitializeCollisionMaps()
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

	models.InitializeDatabase("root:root@tcp(127.0.0.1:3306)/?parseTime=true")
	db.StartSaveWorkerEngine()

	// Initialize the ECS Entity ID counter to the maximum character ID in the DB to avoid session ID collisions.
	// Skip if DB is not available (dev_mode).
	if models.DBEngine != nil {
		var maxID uint64
		if err := models.DBEngine.QueryRow("SELECT COALESCE(MAX(id), 0) FROM characters").Scan(&maxID); err == nil {
			ecs.GlobalRegistry.SetNextID(maxID)
		} else {
			logger.Error("[BOOT] Failed to scan max character ID: %v", err)
		}
	} else {
		logger.Info("[BOOT] No DB available — ECS ID counter starts from default (dev_mode).")
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
		if pos, ok := ecs.GlobalRegistry.GetPosition(id); ok {
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
	auth.StartLoginWorkerPool(4) // Start 4 connection login workers to process db queue

	// Start the MCP JSON-RPC HTTP server for AI agent inspection.
	// Uses port 8080 by default; configure via MCP_PORT env var or data/config.json.
	mcp.Start(mcp.Config{Port: 8080})
	logger.Info("[BOOT] MCP admin interface available on http://localhost:8080/mcp")

	// Start the WebSocket listener for WebGL clients on port 8081.
	// Runs in its own goroutine — does not block the TCP accept loop.
	go transport.StartWebSocketListener(":8081")
	logger.Info("[BOOT] WebSocket transport listening on ws://localhost:8081/ws")

	for {
		conn, err := lis.Accept()
		if err != nil {
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
