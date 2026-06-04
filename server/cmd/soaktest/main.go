// Soak Test — 72-hour stress test tool for Minnsun's Adventure Game Server.
//
// This tool spawns hundreds of synthetic WebSocket and TCP clients that
// connect to the game server, log in, send random packets (MOVE, HEARTBEAT,
// ATTACK), and disconnect. It runs for a configurable duration (default 72h)
// and collects live statistics:
//   - Active connections
//   - Successful logins
//   - Total packets sent/received
//   - Errors and disconnects
//   - Memory and goroutine snapshots via runtime.ReadMemStats
//
// Decision: Built as independent executable in cmd/soaktest/ per the roadmap.
// Uses raw TCP sockets — NOT the WebSocket path — because login handling
// on the server is gated through the TCP `auth.LoginQueue` channel.
// The WebSocket listener is a separate path for WebGL clients; soak testing
// the Main TCP game server uses direct TCP connections (port 1503).
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Command-line flags ───────────────────────────────────────────────────────

var (
	flagServer   = flag.String("server", "127.0.0.1:1503", "Game server TCP address")
	flagClients  = flag.Int("clients", 100, "Number of concurrent synthetic clients")
	flagDuration = flag.Duration("duration", 72*time.Hour, "Test duration (e.g. 24h, 48h, 72h)")
	flagInterval = flag.Duration("interval", 5*time.Second, "Stats reporting interval")
)

// ─── Statistics ───────────────────────────────────────────────────────────────

type Stats struct {
	mu sync.Mutex

	Connects     atomic.Int64
	Disconnects  atomic.Int64
	LoginsOK     atomic.Int64
	LoginsFail   atomic.Int64
	PacketsSent  atomic.Int64
	PacketsRecv  atomic.Int64
	Errors       atomic.Int64
	WriteErrors  atomic.Int64
	ReadErrors   atomic.Int64
	AuthTimeouts atomic.Int64

	startTime   time.Time
	peakClients atomic.Int64
}

func (s *Stats) Snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	elapsed := time.Since(s.startTime)
	active := s.Connects.Load() - s.Disconnects.Load()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return fmt.Sprintf(`
═══════════════════════════════════════════════════
SOAK TEST REPORT  after %s
───────────────────────────────────────────────────
  Active clients      : %d (peak: %d)
  Successful logins   : %d
  Failed logins       : %d
  Packets sent        : %d
  Packets received    : %d
  Write errors        : %d
  Read errors         : %d
  Auth timeouts       : %d
  ────────────────────────────────────
  Go routines         : %d
  HeapAlloc           : %.1f MB
  HeapInuse           : %.1f MB
  TotalAlloc          : %.1f MB
  Mallocs             : %d
  Frees               : %d
  NumGC               : %d
  PauseTotalNS        : %.1f ms
═══════════════════════════════════════════════════
`,
		elapsed.Round(time.Second),
		active, s.peakClients.Load(),
		s.LoginsOK.Load(), s.LoginsFail.Load(),
		s.PacketsSent.Load(), s.PacketsRecv.Load(),
		s.WriteErrors.Load(), s.ReadErrors.Load(),
		s.AuthTimeouts.Load(),
		runtime.NumGoroutine(),
		float64(m.HeapAlloc)/1024/1024,
		float64(m.HeapInuse)/1024/1024,
		float64(m.TotalAlloc)/1024/1024,
		m.Mallocs, m.Frees, m.NumGC,
		float64(m.PauseTotalNs)/1e6,
	)
}

// ─── Client Simulator ─────────────────────────────────────────────────────────

// ClientState tracks each synthetic client's lifecycle.
type ClientState struct {
	id            int
	username      string
	password      string
	conn          net.Conn
	entityID      uint64
	authenticated bool
	stats         *Stats
	logger        *Logger

	quit chan struct{}
	wg   sync.WaitGroup
}

func newClientState(id int, username, password string, stats *Stats, logger *Logger) *ClientState {
	return &ClientState{
		id:       id,
		username: username,
		password: password,
		stats:    stats,
		logger:   logger,
		quit:     make(chan struct{}),
	}
}

// run connects, authenticates, and then periodically sends random packets.
func (c *ClientState) run() {
	c.wg.Add(2) // reader + writer goroutines

	// 1. Connect to server.
	conn, err := net.DialTimeout("tcp", *flagServer, 10*time.Second)
	if err != nil {
		c.logger.Errorf("[Client %d] Connection failed: %v", c.id, err)
		c.stats.Errors.Add(1)
		return
	}
	c.conn = conn
	c.stats.Connects.Add(1)
	c.logger.Debugf("[Client %d] Connected to %s", c.id, *flagServer)

	// 2. Authenticate (LOGIN packet).
	if !c.authenticate() {
		c.stats.LoginsFail.Add(1)
		_ = conn.Close()
		c.stats.Disconnects.Add(1)
		return
	}
	c.authenticated = true
	c.stats.LoginsOK.Add(1)

	// 3. Spawn reader and writer goroutines.
	go c.reader()
	go c.writer()

	c.wg.Wait()
}

// authenticate sends a LOGIN packet (opcode 10) and waits for the S2C Success
// response containing the entityID.
//
// Wire format: [Length uint16 BE][Opcode 1][EntityID uint64 BE][MsgLen uint16 BE][Msg UTF-8]
func (c *ClientState) authenticate() bool {
	// Build LOGIN packet: [Length][Opcode=10][UsernameLen][Username][PasswordLen][Password]
	payload := make([]byte, 1+1+len(c.username)+1+len(c.password))
	payload[0] = byte(len(c.username))
	copy(payload[1:], c.username)
	payload[1+len(c.username)] = byte(len(c.password))
	copy(payload[2+len(c.username):], c.password)

	totalLen := 1 + len(payload) // opcode + payload
	frame := make([]byte, 2+totalLen)
	binary.BigEndian.PutUint16(frame[0:2], uint16(totalLen))
	frame[2] = 10 // OpcodeC2SLogin
	copy(frame[3:], payload)

	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.conn.Write(frame); err != nil {
		c.logger.Errorf("[Client %d] Login write error: %v", c.id, err)
		c.stats.WriteErrors.Add(1)
		return false
	}
	c.stats.PacketsSent.Add(1)

	// Read response: must be length prefix (2B) + opcode (1B) + payload.
	// Expected: opcode 0x01 (Success) with EntityID payload.
	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	var hdr [2]byte
	if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
		c.logger.Errorf("[Client %d] Login response header read error: %v", c.id, err)
		c.stats.ReadErrors.Add(1)
		return false
	}
	respLen := binary.BigEndian.Uint16(hdr[:])
	if respLen == 0 || respLen > 1024 {
		c.logger.Warnf("[Client %d] Login response length invalid: %d", c.id, respLen)
		c.stats.AuthTimeouts.Add(1)
		return false
	}

	respPayload := make([]byte, respLen)
	if _, err := io.ReadFull(c.conn, respPayload); err != nil {
		c.logger.Errorf("[Client %d] Login response body read error: %v", c.id, err)
		c.stats.ReadErrors.Add(1)
		return false
	}
	c.stats.PacketsRecv.Add(1)

	opcode := respPayload[0]
	if opcode == 0xFF {
		// Error packet: [Opcode=0xFF][ErrorCode uint16 BE][MsgLen uint16 BE][Msg UTF-8]
		errCode := binary.BigEndian.Uint16(respPayload[1:3])
		msgLen := binary.BigEndian.Uint16(respPayload[3:5])
		msg := ""
		if msgLen > 0 && 5+msgLen <= uint16(len(respPayload)) {
			msg = string(respPayload[5 : 5+msgLen])
		}
		c.logger.Warnf("[Client %d] Login rejected (err=%d): %s", c.id, errCode, msg)
		return false
	}

	if opcode != 0x01 {
		c.logger.Warnf("[Client %d] Unexpected login opcode: 0x%02X", c.id, opcode)
		return false
	}

	// Success with EntityID: [Opcode][EntityID uint64 BE][MsgLen uint16 BE][Msg UTF-8]
	if len(respPayload) < 11 {
		c.logger.Warnf("[Client %d] Login success payload too short: %d bytes", c.id, len(respPayload))
		return false
	}
	c.entityID = binary.BigEndian.Uint64(respPayload[1:9])
	c.logger.Debugf("[Client %d] Authenticated as entity %d", c.id, c.entityID)
	return true
}

// reader consumes incoming packets from the server (mostly position syncs,
// combat hits, notices). Heartbeat responses (opcode 0x17) are expected.
func (c *ClientState) reader() {
	defer c.wg.Done()
	defer c.cleanup()

	for {
		select {
		case <-c.quit:
			return
		default:
		}

		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		var hdr [2]byte
		if _, err := io.ReadFull(c.conn, hdr[:]); err != nil {
			if isTimeoutOrClosed(err) {
				return
			}
			c.logger.Debugf("[Client %d] Reader header error: %v", c.id, err)
			c.stats.ReadErrors.Add(1)
			return
		}

		respLen := binary.BigEndian.Uint16(hdr[:])
		if respLen == 0 || respLen > 4096 {
			c.logger.Debugf("[Client %d] Reader invalid length: %d", c.id, respLen)
			c.stats.ReadErrors.Add(1)
			return
		}

		respPayload := make([]byte, respLen)
		if _, err := io.ReadFull(c.conn, respPayload); err != nil {
			if isTimeoutOrClosed(err) {
				return
			}
			c.logger.Debugf("[Client %d] Reader body error: %v", c.id, err)
			c.stats.ReadErrors.Add(1)
			return
		}
		c.stats.PacketsRecv.Add(1)

		// Opcode-based filtering — we process only what's useful to track.
		// Most packets are silently consumed to simulate realistic client load.
		// Heartbeat responses reset the read deadline on the server side,
		// but we don't need to respond to S2C packets from synthetic clients.
		switch respPayload[0] {
		case 0x12: // PositionSync — server confirms position change
		case 0x14: // CombatHit — server confirms attack
		case 0x16: // Notice — system message
		case 0x17: // Heartbeat response — expected pong
		default:
			// Unknown opcodes are consumed silently (like a real client would).
		}
	}
}

// writer periodically sends random packets: HEARTBEAT, MOVE, ATTACK.
func (c *ClientState) writer() {
	defer c.wg.Done()

	ticker := time.NewTicker(250 * time.Millisecond) // 4 packets/sec to match game tick rate
	defer ticker.Stop()

	moveCount := 0
	attackCooldown := 0

	for {
		select {
		case <-c.quit:
			return
		case <-ticker.C:
			// Choose a random action each tick.
			action := rand.IntN(10)
			switch {
			case action < 4:
				// Send MOVE packet (opcode 1): [X int32 BE][Z int32 BE]
				c.sendMove()
				moveCount++
			case action < 7 && attackCooldown <= 0:
				// Send ATTACK packet (opcode 5) at a random monster target.
				// Random target ID = 100+rand (monster entities start above 100).
				c.sendAttack(100 + uint64(rand.IntN(500)))
				attackCooldown = 8 // don't spam attack every tick
			case action < 8:
				// Send HEARTBEAT packet (opcode 21): empty payload
				c.sendPacket(21, nil)
			default:
				// Idle — do nothing this tick (simulate afk behavior).
			}
			if attackCooldown > 0 {
				attackCooldown--
			}
		}
	}
}

func (c *ClientState) sendMove() {
	payload := make([]byte, 8)
	// Random walk: stay within a ~200-unit area.
	binary.BigEndian.PutUint32(payload[0:4], uint32(250+rand.IntN(20)-10))
	binary.BigEndian.PutUint32(payload[4:8], uint32(250+rand.IntN(20)-10))
	c.sendPacket(1, payload) // OpcodeC2SMove
}

func (c *ClientState) sendAttack(targetID uint64) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, targetID)
	c.sendPacket(5, payload) // OpcodeC2SAttack
}

// sendPacket builds and sends a framed packet to the server.
// Format: [Length uint16 BE][Opcode uint8][Payload...]
func (c *ClientState) sendPacket(opcode byte, payload []byte) {
	totalLen := 1 + len(payload)
	frame := make([]byte, 2+totalLen)
	binary.BigEndian.PutUint16(frame[0:2], uint16(totalLen))
	frame[2] = opcode
	if len(payload) > 0 {
		copy(frame[3:], payload)
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write(frame); err != nil {
		c.stats.WriteErrors.Add(1)
		return
	}
	c.stats.PacketsSent.Add(1)
}

// cleanup closes the connection and marks the client as disconnected.
func (c *ClientState) cleanup() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.stats.Disconnects.Add(1)
}

// ─── Helper Functions ─────────────────────────────────────────────────────────

func isTimeoutOrClosed(err error) bool {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return true
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return false
}

// ─── Simple Logger ────────────────────────────────────────────────────────────

type Logger struct {
	mu sync.Mutex
}

func NewLogger() *Logger { return &Logger{} }

func (l *Logger) Debugf(format string, args ...interface{}) {
	if *flagClients <= 50 { // only log for small client counts to avoid spam
		l.mu.Lock()
		defer l.mu.Unlock()
		fmt.Printf("[%s] [DEBUG] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
	}
}

func (l *Logger) Warnf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Printf("[%s] [WARN] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
}

func (l *Logger) Errorf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(os.Stderr, "[%s] [ERROR] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
}

func (l *Logger) Infof(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Printf("[%s] [INFO] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	flag.Parse()

	stats := &Stats{startTime: time.Now()}
	logger := NewLogger()

	fmt.Printf(`
╔══════════════════════════════════════════════════════╗
║        MINNSUN ADVENTURE — SERVER SOAK TEST          ║
╠══════════════════════════════════════════════════════╣
║  Server      : %s
║  Clients     : %d
║  Duration    : %s
║  Report every: %s
╚══════════════════════════════════════════════════════╝
`,
		*flagServer, *flagClients, flagDuration.Round(time.Second), flagInterval.Round(time.Second))

	// Generate unique usernames for each synthetic client.
	// Format: soak_<id> — these are registered via the LOGIN path.
	// In dev_mode, the server auto-creates entities for any unknown username.

	// Channel to track completion of all clients.
	doneCh := make(chan struct{}, *flagClients)
	startTime := time.Now()

	// Launch synthetic clients.
	for i := 0; i < *flagClients; i++ {
		go func(id int) {
			username := fmt.Sprintf("soak_%d", id)
			password := "soaktest123"

			// Loop reconnect logic for the full test duration.
			for time.Since(startTime) < *flagDuration {
				client := newClientState(id, username, password, stats, logger)
				client.run()

				// Update peak concurrent clients.
				active := stats.Connects.Load() - stats.Disconnects.Load()
				if peak := stats.peakClients.Load(); active > peak {
					stats.peakClients.Store(active)
				}

				// If we still have time, reconnect after a random delay.
				if time.Since(startTime) >= *flagDuration {
					break
				}
				reconnectDelay := time.Duration(500+rand.IntN(1500)) * time.Millisecond
				time.Sleep(reconnectDelay)
			}
			doneCh <- struct{}{}
		}(i)
	}

	// Stats reporter goroutine.
	reportTicker := time.NewTicker(*flagInterval)
	defer reportTicker.Stop()

	// Signal handler for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	running := true
	for running {
		select {
		case <-reportTicker.C:
			fmt.Print(stats.Snapshot())
		case <-sigCh:
			fmt.Println("\n[SOAK] Interrupt received — shutting down...")
			running = false
		case <-time.After(*flagDuration + 10*time.Second):
			fmt.Println("\n[SOAK] Test duration reached — shutting down...")
			running = false
		}
	}

	// Wait for remaining clients to finish or timeout.
	remaining := *flagClients
	timeout := time.After(30 * time.Second)
	for remaining > 0 {
		select {
		case <-doneCh:
			remaining--
		case <-timeout:
			fmt.Printf("[SOAK] Timeout waiting for %d remaining clients\n", remaining)
			remaining = 0
		}
	}

	// Final report.
	fmt.Println("\n═══════════════════════════════════════════════════")
	fmt.Println("FINAL SOAK TEST REPORT")
	fmt.Println(stats.Snapshot())

	// Calculate and display pass/fail criteria.
	elapsed := time.Since(stats.startTime)
	totalLogins := stats.LoginsOK.Load() + stats.LoginsFail.Load()
	loginSuccessRate := 0.0
	if totalLogins > 0 {
		loginSuccessRate = float64(stats.LoginsOK.Load()) / float64(totalLogins) * 100
	}

	fmt.Printf(`
PASS/FAIL CRITERIA:
────────────────────
  Duration           : %s
  Login success rate : %.1f%% (target: >95%%)
  Write errors       : %d
  Read errors        : %d
  Total goroutines   : %d
  HeapInuse          : %.1f MB
`,
		elapsed.Round(time.Second),
		loginSuccessRate,
		stats.WriteErrors.Load(), stats.ReadErrors.Load(),
		runtime.NumGoroutine(),
		float64(func() uint64 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return m.HeapInuse
		}())/1024/1024,
	)

	if loginSuccessRate < 95.0 {
		fmt.Println("⚠ WARNING: Login success rate below 95% threshold")
	}
	if stats.WriteErrors.Load() > 100 || stats.ReadErrors.Load() > 100 {
		fmt.Println("⚠ WARNING: High error count detected — check server stability")
	}
}
