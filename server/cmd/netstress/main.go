package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"server/peakgo/gmath"
	"server/peakgo/rng"
)

// ─── Protocol Opcode Constants (mirrors server/protocol/opcodes.go) ────────────

const (
	opcodeC2SMove   byte = 1
	opcodeC2SAttack byte = 5
	opcodeC2SLogin  byte = 10

	opcodeS2CSuccess      byte = 0x01
	opcodeS2CSpawnEntity  byte = 0x10
	opcodeS2CPositionSync byte = 0x12
	opcodeS2CStatsSync    byte = 0x13
	opcodeS2CCombatHit    byte = 0x14
	opcodeS2CNotice       byte = 0x16
	opcodeS2CError        byte = 0xFF
)

// ─── Global Configuration (set by flags) ──────────────────────────────────────

var (
	flagBots     = flag.Int("bots", 100, "Number of bot TCP connections")
	flagServer   = flag.String("server", "localhost:1503", "Server TCP address")
	flagClump    = flag.Bool("clump", false, "Clump all bots near (50,50) on map 1")
	flagMove     = flag.Bool("move", true, "Bots send movement packets")
	flagAttack   = flag.Bool("attack", true, "Bots send attack packets")
	flagDuration = flag.Duration("duration", 0, "Test duration (0 = Ctrl+C)")
	flagTick     = flag.Duration("tick", 50*time.Millisecond, "Bot action interval")
	flagReport   = flag.Duration("report", 5*time.Second, "Metrics report interval")
	flagSpread   = flag.Bool("spread", false, "Spread bots across maps 1-5")
	flagPrefix   = flag.String("prefix", "bot", "Bot username prefix")
	flagMapCount = flag.Int("maps", 5, "Number of maps to spread across")

	// New AOI test flags
	flagTeleport = flag.Bool("teleport", false, "Bots teleport randomly every tick (Phase 6)")
	flagBorder   = flag.Bool("border", false, "Bots cross cell borders repeatedly (Phase 5)")
	flagBoss     = flag.Bool("boss", false, "Bots converge on single point + spam attack (Phase 8)")

	// Border test state
	borderStep atomic.Int32
)

// ─── Bot ──────────────────────────────────────────────────────────────────────

// Bot represents a single simulated player TCP connection.
type Bot struct {
	id       int
	username string
	password string
	conn     net.Conn

	// State parsed from server S2C packets
	entityID   uint64
	entityIDMu sync.RWMutex

	// The entity ID we will attack (first monster seen, or fallback dummy)
	targetID   uint64
	targetIDMu sync.RWMutex

	// Current position (last sent to server)
	x, z int32

	// Map assignment (for spread mode)
	mapID int

	// Per-bot statistics
	moveSent   atomic.Int64
	attackSent atomic.Int64
	recvCount  atomic.Int64
	bytesSent  atomic.Int64
	bytesRecv  atomic.Int64

	// Lifecycle
	connected  atomic.Bool
	readerDone chan struct{}
	stopCh     chan struct{}
}

// ─── Global Metrics (atomic counters) ─────────────────────────────────────────

var (
	totalBotsConnected    atomic.Int64
	totalBotsDisconnected atomic.Int64
	totalBytesWritten     atomic.Int64
	totalBytesRead        atomic.Int64
	totalPacketsRead      atomic.Int64
	totalMoveWritten      atomic.Int64
	totalAttackWritten    atomic.Int64
	currentBots           atomic.Int64
	peakBots              atomic.Int64

	mu   sync.Mutex
	bots []*Bot

	startTime time.Time
)

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	flag.Parse()
	startTime = time.Now()

	printBanner()

	// Sanity: spread and clump are mutually exclusive
	if *flagClump {
		*flagSpread = false
	}

	// Phase 1: Connect and login all bots
	connectStart := time.Now()
	connectAllBots()
	connectElapsed := time.Since(connectStart)
	fmt.Printf("\n  >> Connected %d bots in %v (%.0f bots/sec)\n",
		*flagBots, connectElapsed, float64(*flagBots)/connectElapsed.Seconds())

	// Phase 2: Start behavior loops
	mu.Lock()
	for _, b := range bots {
		go b.behaviorLoop()
	}
	mu.Unlock()

	// Phase 3: Start metrics reporter
	go reportLoop()

	// Phase 4: Wait for duration or interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	if *flagDuration > 0 {
		select {
		case <-time.After(*flagDuration):
			fmt.Println("\n  >> Duration elapsed. Shutting down...")
		case <-sigCh:
			fmt.Println("\n  >> Interrupt received. Shutting down...")
		}
	} else {
		<-sigCh
		fmt.Println("\n  >> Interrupt received. Shutting down...")
	}

	// Phase 5: Clean shutdown
	shutdownAll()
	printFinalReport()
}

func printBanner() {
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║      Minnsun's Adventure — Network & AOI Stress Test        ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Printf("  Server:     %s\n", *flagServer)
	fmt.Printf("  Bots:       %d\n", *flagBots)
	fmt.Printf("  Clump:      %v\n", *flagClump)
	fmt.Printf("  Spread:     %v\n", *flagSpread)
	fmt.Printf("  Border:     %v\n", *flagBorder)
	fmt.Printf("  Teleport:   %v\n", *flagTeleport)
	fmt.Printf("  Boss:       %v\n", *flagBoss)
	fmt.Printf("  Move:       %v\n", *flagMove)
	fmt.Printf("  Attack:     %v\n", *flagAttack)
	fmt.Printf("  Tick:       %v\n", *flagTick)
	fmt.Printf("  Report:     %v\n", *flagReport)
	if *flagDuration > 0 {
		fmt.Printf("  Duration:   %v\n", *flagDuration)
	} else {
		fmt.Println("  Duration:   Run until Ctrl+C")
	}
	fmt.Println()
}

// ─── Connection & Login ──────────────────────────────────────────────────────

func connectAllBots() {
	// Concurrency limit to avoid thundering herd on server
	sem := make(chan struct{}, 100)
	var wg sync.WaitGroup
	botCh := make(chan *Bot, *flagBots)

	for i := 0; i < *flagBots; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			username := fmt.Sprintf("%s%04d", *flagPrefix, id)
			password := "stress123"

			// Map assignment
			mapID := 1
			if *flagSpread {
				mapID = 1 + (id % *flagMapCount)
			}

			bot := &Bot{
				id:         id,
				username:   username,
				password:   password,
				mapID:      mapID,
				x:          0,
				z:          0,
				stopCh:     make(chan struct{}),
				readerDone: make(chan struct{}),
			}

			if err := bot.connectAndLogin(); err != nil {
				fmt.Printf("  [FAIL] Bot %d (%s): %v\n", id, username, err)
				return
			}

			mu.Lock()
			bots = append(bots, bot)
			mu.Unlock()

			updatePeak()
			botCh <- bot
		}(i)
	}

	wg.Wait()
	close(botCh)

	// Give bots a moment to receive initial state from server
	time.Sleep(1 * time.Second)
}

func updatePeak() {
	for {
		cur := currentBots.Load()
		peak := peakBots.Load()
		if cur <= peak {
			break
		}
		if peakBots.CompareAndSwap(peak, cur) {
			break
		}
	}
}

func (b *Bot) connectAndLogin() error {
	conn, err := net.DialTimeout("tcp", *flagServer, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	b.conn = conn

	// Start reader goroutine first — it will process the login response
	go b.readLoop()

	// Send LOGIN packet
	if err := b.sendLogin(); err != nil {
		return fmt.Errorf("login send: %w", err)
	}

	// Wait for Success packet to arrive (entityID set by reader goroutine)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		b.entityIDMu.RLock()
		eid := b.entityID
		b.entityIDMu.RUnlock()
		if eid != 0 {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}

	return fmt.Errorf("login timeout (30s) — no Success packet received")
}

func (b *Bot) sendLogin() error {
	uname := []byte(b.username)
	pwd := []byte(b.password)

	// Payload: [UsernameLen 1B][Username...][PasswordLen 1B][Password...]
	payload := make([]byte, 1+len(uname)+1+len(pwd))
	payload[0] = byte(len(uname))
	copy(payload[1:], uname)
	payload[1+len(uname)] = byte(len(pwd))
	copy(payload[2+len(uname):], pwd)

	// Full wire packet: [Length 2B BE][Opcode 1B][Payload...]
	packet := make([]byte, 2+1+len(payload))
	binary.BigEndian.PutUint16(packet[0:2], uint16(1+len(payload)))
	packet[2] = opcodeC2SLogin
	copy(packet[3:], payload)

	n, err := b.conn.Write(packet)
	if err != nil {
		return err
	}
	totalBytesWritten.Add(int64(n))
	b.bytesSent.Add(int64(n))
	return nil
}

// ─── Packet Reader (per-bot goroutine) ────────────────────────────────────────

// readLoop drains all incoming server packets.
// It parses SpawnEntity to discover attack targets and Success to capture EntityID.
func (b *Bot) readLoop() {
	defer func() {
		b.connected.Store(false)
		currentBots.Add(-1)
		totalBotsDisconnected.Add(1)
		close(b.readerDone)
	}()

	b.connected.Store(true)
	currentBots.Add(1)
	totalBotsConnected.Add(1)

	// Pre-allocated buffer for the common case (most packets are < 256 bytes)
	var header [2]byte

	for {
		// ── Read 2-byte length header ──
		if _, err := io.ReadFull(b.conn, header[:]); err != nil {
			fmt.Printf("  [BOT %d] disconnect reason: %v\n", b.id, err)
			return
		}
		length := binary.BigEndian.Uint16(header[:])
		if length == 0 || length > 4096 {
			fmt.Printf("  [BOT %d] protocol violation: length=%d\n", b.id, length)
			return // protocol violation
		}

		// ── Read opcode + payload ──
		buf := make([]byte, length)
		if _, err := io.ReadFull(b.conn, buf); err != nil {
			fmt.Printf("  [BOT %d] payload read error: %v\n", b.id, err)
			return
		}

		// Metrics
		totalPacketsRead.Add(1)
		totalBytesRead.Add(int64(2 + length))
		b.recvCount.Add(1)
		b.bytesRecv.Add(int64(2 + length))

		opcode := buf[0]
		payload := buf[1:]

		switch opcode {
		case opcodeS2CSuccess:
			// Format: [EntityID 8B BE][MessageLen 2B BE][Message UTF-8]
			if len(payload) >= 10 {
				eid := binary.BigEndian.Uint64(payload[0:8])
				b.entityIDMu.Lock()
				b.entityID = eid
				b.entityIDMu.Unlock()
			}

		case opcodeS2CSpawnEntity:
			// Format: [EntityID 8B][Type 1B][MapID 4B][X 4B][Z 4B][NameLen 1B][Name]
			if len(payload) >= 22 {
				eid := binary.BigEndian.Uint64(payload[0:8])
				entityType := payload[8]
				// X at offset 13, Z at offset 17 (after: EntityID 8 + Type 1 + MapID 4)
				spawnX := int32(binary.BigEndian.Uint32(payload[13:17]))
				spawnZ := int32(binary.BigEndian.Uint32(payload[17:21]))

				// If this is our own entity, update our position from the server's trusted state.
				b.entityIDMu.RLock()
				myID := b.entityID
				b.entityIDMu.RUnlock()
				if eid == myID {
					b.x = spawnX
					b.z = spawnZ
				}

				if entityType == 1 { // Monster (0=player, 1=monster, 2=ground_item)
					b.targetIDMu.Lock()
					if b.targetID == 0 {
						b.targetID = eid
					}
					b.targetIDMu.Unlock()
				}
			}
		}
	}
}

// ─── Behavior Loop (per-bot goroutine) ────────────────────────────────────────

func (b *Bot) behaviorLoop() {
	ticker := time.NewTicker(*flagTick)
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-ticker.C:
			b.sendActions()
		}
	}
}

func (b *Bot) sendActions() {
	if !b.connected.Load() || b.conn == nil {
		return
	}

	// ── Compute next movement ──
	var targetX, targetZ int32

	// Priority: boss > teleport > border > clump > spread > default
	if *flagBoss {
		// Phase 8 — Boss Event: all bots converge on a single point (0,0)
		// and move randomly within a 2-unit radius. This simulates worst-case
		// stacking where all 1000+ players are visible to each other.
		targetX = int32(rng.Intn(3) - 1)
		targetZ = int32(rng.Intn(3) - 1)
	} else if *flagTeleport {
		// Phase 6 — Teleport: bots teleport randomly across the entire map
		// from (10,10) to (900,900) every tick. This stresses AOI enter/leave
		// as watchers rapidly appear/disappear from each other's viewport.
		targetX = int32(10 + rng.Intn(891))
		targetZ = int32(10 + rng.Intn(891))
	} else if *flagBorder {
		// Phase 5 — Cell Border Crossing: bots oscillate across cell borders.
		// The grid cell size is 32 units, so crossing from (31,31) to (32,31)
		// causes AOI recalculation. Each bot alternates its X coordinate between
		// 31 and 32 to trigger repeated enter/leave cycles every tick.
		step := borderStep.Add(1)
		if step%2 == 0 {
			targetX = 31
		} else {
			targetX = 32
		}
		targetZ = int32(b.id%62 + 1) // spread across Z to avoid all bots on same cell edge
	} else if *flagClump {
		// Phase 2 — Clump: all bots try to move near (0,0)
		// Clump near origin (0,0) — worst-case AOI density
		targetX = int32(rng.Intn(5) - 2)
		targetZ = int32(rng.Intn(5) - 2)
	} else if *flagSpread {
		// Spread mode: bots move randomly within [10, 990] to create natural
		// movement that crosses AOI cell borders (cell size = 32), generating
		// Enter/Leave chunk events as neighbors change.
		dx := int32(rng.Intn(3) - 1) // -1, 0, or 1
		dz := int32(rng.Intn(3) - 1)
		tx, tz := gmath.ClampPos(int(b.x+dx), int(b.z+dz), 10, 990)
		targetX = int32(tx)
		targetZ = int32(tz)
	} else {
		// Default: random walk within map bounds [1, 999]
		dx := int32(rng.Intn(3) - 1) // -1, 0, or 1
		dz := int32(rng.Intn(3) - 1)
		tx, tz := gmath.ClampPos(int(b.x+dx), int(b.z+dz), 1, 999)
		targetX = int32(tx)
		targetZ = int32(tz)
	}

	// For boss mode, also send a second attack packet to increase combat stress
	if *flagBoss && *flagAttack {
		// Send an extra attack packet to amplify combat stress on the boss target
		b.targetIDMu.RLock()
		target := b.targetID
		b.targetIDMu.RUnlock()
		if target == 0 {
			target = 99999
		}
		packet2 := make([]byte, 11)
		binary.BigEndian.PutUint16(packet2[0:2], 9)
		packet2[2] = opcodeC2SAttack
		binary.BigEndian.PutUint64(packet2[3:11], target)
		if n, err := b.conn.Write(packet2); err == nil {
			b.attackSent.Add(1)
			totalAttackWritten.Add(1)
			totalBytesWritten.Add(int64(n))
			b.bytesSent.Add(int64(n))
		}
	}

	// ── Send MOVE packet ──
	if *flagMove {
		// Move wire format: [Length 2B: 0x0009][Opcode 0x01][X int32 BE][Z int32 BE]
		packet := make([]byte, 11)
		binary.BigEndian.PutUint16(packet[0:2], 9) // 1 opcode + 8 payload
		packet[2] = opcodeC2SMove
		binary.BigEndian.PutUint32(packet[3:7], uint32(targetX))
		binary.BigEndian.PutUint32(packet[7:11], uint32(targetZ))

		if n, err := b.conn.Write(packet); err == nil {
			b.x = targetX
			b.z = targetZ
			b.moveSent.Add(1)
			totalMoveWritten.Add(1)
			totalBytesWritten.Add(int64(n))
			b.bytesSent.Add(int64(n))
		}
	}

	// ── Send ATTACK packet ──
	if *flagAttack {
		// Try to use a discovered monster, else use a dummy target
		b.targetIDMu.RLock()
		target := b.targetID
		b.targetIDMu.RUnlock()
		if target == 0 {
			target = 99999 // dummy target — still stresses server entity lookup
		}

		// Attack wire format: [Length 2B: 0x0009][Opcode 0x05][TargetID uint64 BE]
		packet := make([]byte, 11)
		binary.BigEndian.PutUint16(packet[0:2], 9)
		packet[2] = opcodeC2SAttack
		binary.BigEndian.PutUint64(packet[3:11], target)

		if n, err := b.conn.Write(packet); err == nil {
			b.attackSent.Add(1)
			totalAttackWritten.Add(1)
			totalBytesWritten.Add(int64(n))
			b.bytesSent.Add(int64(n))
		}
	}
}

// ─── Shutdown ─────────────────────────────────────────────────────────────────

func (b *Bot) disconnect() {
	close(b.stopCh)
	if b.conn != nil {
		_ = b.conn.Close()
	}
	<-b.readerDone
}

func shutdownAll() {
	mu.Lock()
	defer mu.Unlock()

	fmt.Printf("  Shutting down %d bots...\n", len(bots))
	var wg sync.WaitGroup
	for _, b := range bots {
		wg.Add(1)
		go func(bot *Bot) {
			defer wg.Done()
			bot.disconnect()
		}(b)
	}
	wg.Wait()
	fmt.Println("  All bots disconnected.")
}

// ─── Metrics Reporter ─────────────────────────────────────────────────────────

func reportLoop() {
	ticker := time.NewTicker(*flagReport)
	defer ticker.Stop()

	var prevMove, prevAttack, prevRecv int64
	var prevBytesW, prevBytesR int64

	for range ticker.C {
		elapsed := time.Since(startTime)
		moves := totalMoveWritten.Load()
		attacks := totalAttackWritten.Load()
		recv := totalPacketsRead.Load()
		bytesW := totalBytesWritten.Load()
		bytesR := totalBytesRead.Load()
		connected := totalBotsConnected.Load()
		disconnected := totalBotsDisconnected.Load()
		cur := currentBots.Load()
		pk := peakBots.Load()

		dMove := moves - prevMove
		dAttack := attacks - prevAttack
		dRecv := recv - prevRecv
		dBytesW := bytesW - prevBytesW
		dBytesR := bytesR - prevBytesR

		interval := *flagReport
		secs := float64(interval) / float64(time.Second)

		fmt.Println()
		fmt.Println("──────────────────────────────────────────────────────────────")
		fmt.Printf("  ⏱  Elapsed: %v\n", elapsed.Round(time.Second))
		fmt.Printf("  👥  Bots: %d active (peak: %d) | %d total | %d disconnected\n",
			cur, pk, connected, disconnected)
		fmt.Printf("  📤  TX: %.0f msg/s (%d moves + %d attacks) | %.1f KB/s\n",
			float64(dMove+dAttack)/secs, dMove, dAttack,
			float64(dBytesW)/1024.0/secs)
		fmt.Printf("  📥  RX: %.0f pkt/s | %.1f KB/s\n",
			float64(dRecv)/secs, float64(dBytesR)/1024.0/secs)
		if dMove+dAttack > 0 {
			fmt.Printf("  📊  Avg pkt size TX: %.0f B | RX: %.0f B\n",
				float64(dBytesW)/float64(dMove+dAttack+1),
				float64(dBytesR)/float64(dRecv+1))
		}
		fmt.Println("──────────────────────────────────────────────────────────────")

		prevMove = moves
		prevAttack = attacks
		prevRecv = recv
		prevBytesW = bytesW
		prevBytesR = bytesR
	}
}

func printFinalReport() {
	elapsed := time.Since(startTime)
	moves := totalMoveWritten.Load()
	attacks := totalAttackWritten.Load()
	recv := totalPacketsRead.Load()
	bytesW := totalBytesWritten.Load()
	bytesR := totalBytesRead.Load()
	connected := totalBotsConnected.Load()
	disconnected := totalBotsDisconnected.Load()
	pk := peakBots.Load()

	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                       FINAL REPORT                          ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
	fmt.Printf("  Total test time:     %v\n", elapsed.Round(time.Second))
	fmt.Printf("  Peak concurrent:     %d bots\n", pk)
	fmt.Printf("  Total connected:     %d\n", connected)
	fmt.Printf("  Disconnected:        %d\n", disconnected)
	fmt.Printf("  Moves sent:          %d (%.0f/sec)\n", moves, float64(moves)/elapsed.Seconds())
	fmt.Printf("  Attacks sent:        %d (%.0f/sec)\n", attacks, float64(attacks)/elapsed.Seconds())
	fmt.Printf("  Total TX:            %d packets (%.1f MB)\n",
		moves+attacks, float64(bytesW)/1024.0/1024.0)
	fmt.Printf("  Total RX:            %d packets (%.1f MB)\n",
		recv, float64(bytesR)/1024.0/1024.0)
	fmt.Printf("  Avg TX rate:         %.1f KB/s\n", float64(bytesW)/1024.0/elapsed.Seconds())
	fmt.Printf("  Avg RX rate:         %.1f KB/s\n", float64(bytesR)/1024.0/elapsed.Seconds())
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Println()
}
