// Package main — Scripted Bot Scenario Runner
//
// scenarios.go provides the Scenario interface with 4 implementations
// (move, attack, inventory, combat_loop) for blackbox testing via MCP.
//
// Each scenario runs a single bot through a scripted sequence, writes
// structured trace entries via logger.PushTraceLog with
// "source":"scenario_runner", and returns an error on failure.

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"server/logger"
)

// ─── MCP Endpoint ──────────────────────────────────────────────────────────────

const mcpEndpoint = "http://localhost:8080/mcp"

// callMCP sends a JSON-RPC 2.0 request to the MCP HTTP server and returns the
// result map. Returns an error if the HTTP call fails or the RPC response
// contains an error object.
func callMCP(method string, params map[string]any) (map[string]any, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp marshal %s: %w", method, err)
	}

	resp, err := http.Post(mcpEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp call %s: %w", method, err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("mcp decode %s: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp error %s: %s", method, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// ─── Scenario Packet ───────────────────────────────────────────────────────────

// ScenarioPacket is a single parsed packet forwarded from the bot's readLoop
// to the scenario goroutine.
type ScenarioPacket struct {
	Opcode  byte
	Payload []byte
}

// ─── Scenario Interface ────────────────────────────────────────────────────────

// Scenario defines a scripted bot behavior sequence.
// Run executes the scenario. On success it returns nil; on failure it returns
// a descriptive error. The bot is already connected and logged in when Run is
// called.
type Scenario interface {
	Name() string
	Run(bot *Bot, traceID string, duration time.Duration) error
}

// ─── Scenario Registry ─────────────────────────────────────────────────────────

var scenarioRegistry = map[string]Scenario{
	"move":        &ScenarioMove{},
	"attack":      &ScenarioAttack{},
	"inventory":   &ScenarioInventory{},
	"combat_loop": &ScenarioCombatLoop{},
}

// getScenario returns the scenario by name, or nil if not found.
func getScenario(name string) Scenario {
	s, ok := scenarioRegistry[name]
	if !ok {
		return nil
	}
	return s
}

// ─── ScenarioMove ──────────────────────────────────────────────────────────────

// ScenarioMove walks the bot in a 10×10 grid pattern and verifies that the
// server echoes back PositionSync packets for each step.
type ScenarioMove struct{}

func (s *ScenarioMove) Name() string { return "move" }

func (s *ScenarioMove) Run(bot *Bot, traceID string, duration time.Duration) error {
	bot.scenarioMsgCh = make(chan ScenarioPacket, 256)
	defer func() { bot.scenarioMsgCh = nil }()

	// Wait for entity ID from login.
	eid, err := waitForEntityID(bot, 10*time.Second)
	if err != nil {
		return err
	}

	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "scenario_move_start",
		Fields:   map[string]any{"grid": "10x10"},
	})

	// Walk a 10×10 grid (100 steps).
	for x := int32(0); x < 10; x++ {
		for z := int32(0); z < 10; z++ {
			if !bot.connected.Load() {
				return fmt.Errorf("bot disconnected at grid (%d,%d)", x, z)
			}

			// Send move packet.
			bot.sendMove(x, z)

			// Wait for PositionSync matching this entity.
			syncDeadline := time.Now().Add(2 * time.Second)
			received := false
			for time.Now().Before(syncDeadline) {
				select {
				case pkt := <-bot.scenarioMsgCh:
					if pkt.Opcode == opcodeS2CPositionSync && len(pkt.Payload) >= 16 {
						pktEid := binary.BigEndian.Uint64(pkt.Payload[0:8])
						if pktEid == eid {
							received = true
						}
					}
				default:
					time.Sleep(5 * time.Millisecond)
				}
				if received {
					break
				}
			}

			if !received {
				logger.PushTraceLog(logger.TraceLog{
					Time:     time.Now().UTC().Format(time.RFC3339Nano),
					TraceID:  traceID,
					EntityID: eid,
					Msg:      "move_step_timeout",
					Fields:   map[string]any{"x": x, "z": z},
				})
				return fmt.Errorf("position sync timeout at grid (%d,%d)", x, z)
			}
		}
	}

	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "scenario_move_pass",
		Fields:   map[string]any{"steps": 100},
	})
	return nil
}

// ─── ScenarioAttack ────────────────────────────────────────────────────────────

// ScenarioAttack locates the nearest monster via MCP (world_query_radius),
// records its HP, sends an attack, then verifies the monster's HP decreased.
type ScenarioAttack struct{}

func (s *ScenarioAttack) Name() string { return "attack" }

func (s *ScenarioAttack) Run(bot *Bot, traceID string, duration time.Duration) error {
	eid, err := waitForEntityID(bot, 10*time.Second)
	if err != nil {
		return err
	}

	// Find nearest monster via MCP world_query_radius.
	monsterID, err := findNearestMonster(eid, 10*time.Second)
	if err != nil {
		return fmt.Errorf("find monster: %w", err)
	}

	// Get monster HP before attack.
	beforeStats, err := callMCP("ecs_get_stats", map[string]any{
		"id": float64(monsterID),
	})
	if err != nil {
		return fmt.Errorf("get stats before attack: %w", err)
	}
	beforeHP, _ := beforeStats["hp"].(float64)

	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "attack_before",
		Fields: map[string]any{
			"monster_id": monsterID,
			"hp_before":  beforeHP,
		},
	})

	// Send attack.
	bot.sendAttack(monsterID)

	// Wait a brief moment for combat processing.
	time.Sleep(500 * time.Millisecond)

	// Get monster HP after attack.
	afterStats, err := callMCP("ecs_get_stats", map[string]any{
		"id": float64(monsterID),
	})
	if err != nil {
		return fmt.Errorf("get stats after attack: %w", err)
	}
	afterHP, _ := afterStats["hp"].(float64)

	damage := beforeHP - afterHP
	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "attack_after",
		Fields: map[string]any{
			"monster_id": monsterID,
			"hp_before":  beforeHP,
			"hp_after":   afterHP,
			"damage":     damage,
		},
	})

	if afterHP >= beforeHP {
		return fmt.Errorf("HP did not decrease: before=%.0f after=%.0f", beforeHP, afterHP)
	}
	return nil
}

// ─── ScenarioInventory ─────────────────────────────────────────────────────────

// ScenarioInventory sends an inventory request (C2SInv) and verifies the bot
// does not disconnect (crash test). Also queries inventory via MCP for logging.
type ScenarioInventory struct{}

func (s *ScenarioInventory) Name() string { return "inventory" }

func (s *ScenarioInventory) Run(bot *Bot, traceID string, duration time.Duration) error {
	eid, err := waitForEntityID(bot, 10*time.Second)
	if err != nil {
		return err
	}

	// Build and send C2SInv packet: [Length 2B][Opcode 1B]
	packet := make([]byte, 3)
	binary.BigEndian.PutUint16(packet[0:2], 1) // payload = 1 byte (opcode only)
	packet[2] = opcodeC2SInv
	if _, err := bot.conn.Write(packet); err != nil {
		return fmt.Errorf("send inventory request: %w", err)
	}

	// Wait 2 seconds — check bot stays connected.
	time.Sleep(2 * time.Second)

	if !bot.connected.Load() {
		return fmt.Errorf("bot disconnected after inventory request")
	}

	// Also check inventory via MCP for logging.
	invResult, err := callMCP("ecs_get_inventory", map[string]any{
		"id": float64(eid),
	})
	itemCount := 0
	if err == nil {
		if items, ok := invResult["items"].([]any); ok {
			itemCount = len(items)
		}
	}

	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "inventory_check",
		Fields:   map[string]any{"item_count": itemCount, "mcp_ok": err == nil},
	})

	return nil
}

// ─── ScenarioCombatLoop ────────────────────────────────────────────────────────

// ScenarioCombatLoop runs move + attack actions in a loop for the specified
// duration. Fails if the bot disconnects or the connection drops.
type ScenarioCombatLoop struct{}

func (s *ScenarioCombatLoop) Name() string { return "combat_loop" }

func (s *ScenarioCombatLoop) Run(bot *Bot, traceID string, duration time.Duration) error {
	eid, err := waitForEntityID(bot, 10*time.Second)
	if err != nil {
		return err
	}

	// Try to find a monster target.
	targetID, _ := findNearestMonster(eid, 5*time.Second)
	if targetID == 0 {
		// Fallback: use whatever target the bot discovered via SpawnEntity.
		bot.targetIDMu.RLock()
		targetID = uint64(bot.targetID)
		bot.targetIDMu.RUnlock()
	}

	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "combat_loop_start",
		Fields: map[string]any{
			"target_id": targetID,
			"duration":  duration.String(),
		},
	})

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	endTime := time.Now().Add(duration)
	step := int32(0)
	for time.Now().Before(endTime) {
		if !bot.connected.Load() {
			return fmt.Errorf("bot disconnected during combat loop")
		}

		select {
		case <-ticker.C:
			// Move in a small area.
			x := (step % 20)
			z := ((step / 20) % 20)
			bot.sendMove(x, z)

			// Attack if a target is known.
			if targetID != 0 {
				bot.sendAttack(targetID)
			}
			step++

		case <-bot.stopCh:
			return fmt.Errorf("bot stopped during combat loop")
		}
	}

	logger.PushTraceLog(logger.TraceLog{
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
		TraceID:  traceID,
		EntityID: eid,
		Msg:      "combat_loop_pass",
		Fields:   map[string]any{"steps": step, "duration": duration.String()},
	})
	return nil
}

// ─── Internal Helpers ──────────────────────────────────────────────────────────

// waitForEntityID polls the bot's entityID until it's non-zero or timeout.
func waitForEntityID(bot *Bot, timeout time.Duration) (uint64, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		bot.entityIDMu.RLock()
		eid := uint64(bot.entityID)
		bot.entityIDMu.RUnlock()
		if eid != 0 {
			return eid, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, fmt.Errorf("entity ID not received within %v", timeout)
}

// findNearestMonster calls world_query_radius MCP tool repeatedly until a
// monster entity is found or the timeout expires.
func findNearestMonster(entityID uint64, timeout time.Duration) (uint64, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		result, err := callMCP("world_query_radius", map[string]any{
			"entity_id": float64(entityID),
			"radius":    60.0,
		})
		if err == nil {
			raw, ok := result["entities"].([]any)
			if ok {
				for _, e := range raw {
					emap, ok := e.(map[string]any)
					if !ok {
						continue
					}
					etype, _ := emap["type"].(string)
					if etype == "monster" {
						mid, _ := emap["id"].(float64)
						if mid > 0 {
							return uint64(mid), nil
						}
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("no monster found within %v", timeout)
}
