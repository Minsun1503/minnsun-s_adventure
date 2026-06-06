package network

import (
	"fmt"
	"net"
	"server/db"

	"server/ecs"
	"server/game"
	"server/logger"
	"server/models"
	"server/peakgo/codec"
	"server/peakgo/loggate"
	"server/peakgo/netio"
	"server/peakgo/ratelimit"
	"server/systems"
	"server/world"
	"time"
)

// packetPool is the shared payload buffer pool for all client connections.
// Exposed via netio.DefaultPool; defined here for documentation locality.
var packetPool = netio.DefaultPool

// Global rate limiter for all client connections.
// Uses peakgo/ratelimit.TokenBucket — tick-based refill, zero time.Now() calls.
var globalRateLimiter = ratelimit.NewRateLimiter()

var opcodeNames = map[byte]string{
	1: "MOVE", 2: "INV", 3: "USE", 4: "WARP", 5: "ATTACK",
	6: "INFO", 7: "QUIT", 8: "PICKUP", 9: "EQUIP",
	10: "LOGIN", 11: "REGISTER", 12: "PARTY_CREATE",
	13: "PARTY_INVITE", 14: "PARTY_JOIN",
	15: "TRADE_INIT", 16: "TRADE_OFFER", 17: "TRADE_CONFIRM", 18: "TRADE_CANCEL",
	19: "SKILL_CAST", 20: "CHAT", 21: "HEARTBEAT",
}

// opcodeNameOf returns a human-readable name for a binary opcode byte.
// Used by the network packet debug middleware in handleBinaryPacket.
func opcodeNameOf(op byte) string {
	if s, ok := opcodeNames[op]; ok {
		return s
	}
	return "UNKNOWN"
}

// HandleClient manages the lifecycle of a single connected player.
// snap is passed in from main so we avoid a redundant ECS lookup at goroutine start.
//
// Parameters:
//   - conn:         TCP socket for this player.
//   - playerEntity: ECS entity ID.
//   - snap:         Pre-fetched snapshot (name + spawn position already resolved).
func HandleClient(conn net.Conn, playerEntity ecs.Entity, snap ecs.EntitySnapshot) {
	isBot := len(snap.Meta.Name) >= 3 && snap.Meta.Name[:3] == "bot"

	// Register connection with rate limiter
	globalRateLimiter.RegisterConnection(conn)

	// Deferred cleanup: remove rate limiter, broadcast logout, remove from ECS, close socket.
	defer func() {
		globalRateLimiter.UnregisterConnection(conn)
		name := snap.Meta.Name
		if live, ok := ecs.DefaultRegistry.GetMetadata(playerEntity); ok {
			name = live.Name
		}
		game.GlobalTradeRegistry.CancelTradeSession(playerEntity)
		game.RemovePlayerFromParty(playerEntity)
		db.QueuePlayerSave(playerEntity)
		world.UnregisterPlayerAOI(playerEntity)
		world.GlobalSpatialGrid.RemoveEntity(playerEntity)

		// Close the Writer (which closes the underlying conn) before RemoveEntity
		// so pending outbound frames are drained before entity cleanup.
		if connComp, ok := ecs.DefaultRegistry.GetConnection(playerEntity); ok && connComp.Writer != nil {
			connComp.Writer.Close()
		}

		ecs.DefaultRegistry.RemoveEntity(playerEntity)
		models.ActivePlayers.Delete(conn.RemoteAddr().String())

		if !isBot {
			logger.Info("[DISCONNECT] %s (%s)", name, conn.RemoteAddr())
			systems.BroadcastSystem(
				[]byte(fmt.Sprintf("Player %s has logged out the game!\r\n", name)),
			)
		}
		conn.Close()
	}()

	// Greet the player with their name and spawn position — both already in snap.
	// Only for real players to save allocation and bandwidth.
	if !isBot {
		spawnMsg := fmt.Sprintf(
			"Welcome to the Realm, %s!\r\nYour Spawn Position is X: %d, Z: %d\r\n",
			snap.Meta.Name, snap.Pos.X, snap.Pos.Z,
		)
		systems.SendNoticeSystem(playerEntity, []byte(spawnMsg))
	}

	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))

	for {
		// Zero-alloc header read: stack [2]byte + BigEndian.Uint16, no reflection.
		// Rate limit check before reading any data.
		// peakgo/ratelimit uses tick-based refill — zero time.Now() calls.
		if !globalRateLimiter.Allow(conn, systems.CurrentTick()) {
			loggate.Debugf("[RATE LIMIT] %s exceeded packet budget, dropping", conn.RemoteAddr())
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			continue
		}

		length, err := netio.ReadHeader(conn)
		if err != nil {
			break // Disconnected or read error
		}
		if length == 0 {
			continue
		}

		// Pooled payload read: no heap allocation on steady-state path.
		pBuf, err := netio.ReadPayload(conn, packetPool, length)
		if err != nil {
			break // Disconnected or read error
		}

		buf := (*pBuf)[:length]
		opcode := codec.ReadUint8(buf)
		payload := buf[1:]

		HandleBinaryPacket(conn, playerEntity, opcode, payload)

		packetPool.Put(pBuf)
		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	}
}

// HandleBinaryPacket dispatches a single binary packet using the handler registry.
// Falls back to the default unknown-opcode path if no handler is registered.
func HandleBinaryPacket(conn net.Conn, playerEntity ecs.Entity, opcode byte, payload []byte) {
	// Network packet trace middleware: only active when debug=true in config.json.
	loggate.Debugf("[NET RX] Conn: %s | Opcode: %d (%s) | Payload: %d bytes | Hex: [% X]",
		conn.RemoteAddr(), opcode, opcodeNameOf(opcode), len(payload), payload)

	// O(1) map dispatch — replaces the giant switch block.
	if !DispatchPacket(conn, playerEntity, opcode, payload) {
		loggate.Warnf("[NET] Unknown opcode %d from %s", opcode, conn.RemoteAddr())
		systems.SendNoticeSystem(playerEntity, []byte(fmt.Sprintf("Error: Unknown opcode %d\r\n", opcode)))
	}
}
