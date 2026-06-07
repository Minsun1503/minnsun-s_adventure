// Package transport provides WebSocket transport adapter for the game server.
//
// WSConn wraps a gorilla/websocket.Conn to satisfy the net.Conn interface,
// allowing the existing TCP-based game logic (processLogin, handleClient,
// handleBinaryPacket) to accept WebSocket connections transparently.
//
// Wire Model
//
//	WebSocket Binary Message → [Length uint16 BE][Opcode uint8][Payload N-bytes]
//
// Each binary message carries exactly one framed game packet.  WSConn.Read()
// transparently fetches the next message from the WebSocket stream when the
// current message's bytes are exhausted.
//
// # Text Message Interception
//
// Text messages with the prefix "[SNAPSHOT]" are intercepted by Read() and
// forwarded to logger.PushTraceLog() as client-side trace entries.  They are
// NOT passed to the binary packet pipeline.  All other text messages are
// silently discarded.
package transport

import (
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"server/logger"
)

// ─── Compile-time interface check ─────────────────────────────────────────────
var _ net.Conn = (*WSConn)(nil)

// ─── WSConn ───────────────────────────────────────────────────────────────────

// WSConn adapts a gorilla/websocket.Conn into a standard net.Conn for use with
// the game server's existing TCP-based packet I/O pipeline.
//
// Read behaviour:
//  1. Each WebSocket binary message is treated as a single game packet.
//  2. When the current reader is exhausted, ReadMessage is called to fetch the
//     next binary message — so the protocol framing [length][opcode][payload]
//     is split across multiple Read() calls as it would be over TCP.
//  3. Text messages with the prefix "[SNAPSHOT]" are intercepted and logged
//     as client-side trace entries via logger.PushTraceLog().  They are not
//     forwarded to the binary packet pipeline.  Other text messages are
//     silently discarded.
//
// Write behaviour:
//   - Write() sends a single binary WebSocket message.
//   - Multiple Write() calls on the same frame are NOT supported — the caller
//     must assemble the complete frame before writing.
//
// Deadline support:
//   - SetReadDeadline / SetWriteDeadline are propagated to the underlying
//     TCP connection when it is a *net.TCPConn, enabling login timeout and
//     idle-disconnect in processLogin / handleClient with zero code changes.
//   - If the underlying connection is not TCP (e.g. TLS), deadlines are
//     silently ignored.
type WSConn struct {
	conn   *websocket.Conn
	reader io.Reader // current binary message reader
	mu     sync.Mutex
}

// NewWSConn wraps an upgraded WebSocket connection into a net.Conn.
func NewWSConn(conn *websocket.Conn) *WSConn {
	return &WSConn{conn: conn}
}

// ─── Read ─────────────────────────────────────────────────────────────────────

// Read implements net.Conn.Read.
//
// If the current message reader is exhausted, it fetches the next binary
// message from the WebSocket stream.  Text messages with the "[SNAPSHOT]"
// prefix are intercepted and logged as client-side trace entries; other
// text messages are silently discarded.
func (w *WSConn) Read(b []byte) (int, error) {
	for {
		if w.reader != nil {
			n, err := w.reader.Read(b)
			if err == io.EOF {
				w.reader = nil // exhausted — fetch next message on next call
				if n > 0 {
					return n, nil
				}
				continue
			}
			return n, err
		}

		// Fetch next WebSocket message.
		messageType, msgReader, err := w.conn.NextReader()
		if err != nil {
			return 0, err
		}

		// ── Text message interception ──────────────────────────────
		// Snapshot frames from ClientSnapshotDumper are sent as text
		// (not binary) so they bypass the game's binary packet pipeline.
		if messageType == websocket.TextMessage {
			handleTextMessage(msgReader)
			// Loop to fetch next message (should be binary).
			continue
		}

		// Binary message: assign to reader for the caller to consume.
		w.reader = msgReader
	}
}

// handleTextMessage reads a complete WebSocket text message and, if it is a
// client snapshot (prefixed with "[SNAPSHOT]"), pushes it to the JSONL trace
// log.  Non-snapshot text messages are silently discarded.
func handleTextMessage(msgReader io.Reader) {
	// Read the full text payload.  Snapshot frames are small (~300 bytes),
	// so one allocation per snapshot is acceptable on this non-critical path.
	raw, err := io.ReadAll(msgReader)
	if err != nil {
		return
	}

	text := string(raw)

	// Only handle [SNAPSHOT]-prefixed messages.
	if !strings.HasPrefix(text, "[SNAPSHOT] ") {
		return
	}

	// Strip the prefix to get the raw JSON.
	jsonStr := text[len("[SNAPSHOT] "):]

	// Push into the JSONL trace log with "source": "client".
	logger.PushTraceLog(logger.TraceLog{
		Time:   time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		Msg:    "client_snapshot",
		Fields: map[string]any{"source": "client", "raw": jsonStr},
	})
}

// ─── Write ────────────────────────────────────────────────────────────────────

// Write implements net.Conn.Write.
//
// Sends the data as a single binary WebSocket message.  Multiple writes for
// the same logical frame are not expected — the caller (netio.WritePacket /
// protocol.Send*) always assembles the complete frame before calling Write.
func (w *WSConn) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// ─── Close ────────────────────────────────────────────────────────────────────

// Close implements net.Conn.Close.
//
// Sends a WebSocket close frame with a normal closure code before closing
// the underlying connection.
func (w *WSConn) Close() error {
	return w.conn.Close()
}

// ─── LocalAddr / RemoteAddr ───────────────────────────────────────────────────

// LocalAddr implements net.Conn.LocalAddr.
func (w *WSConn) LocalAddr() net.Addr {
	return w.conn.LocalAddr()
}

// RemoteAddr implements net.Conn.RemoteAddr.
func (w *WSConn) RemoteAddr() net.Addr {
	return w.conn.RemoteAddr()
}

// ─── Deadline helpers ─────────────────────────────────────────────────────────

// deadlineConn attempts to extract the *net.TCPConn underlying the WebSocket
// so we can propagate read/write deadlines.
func (w *WSConn) deadlineConn() net.Conn {
	raw := w.conn.UnderlyingConn()
	// If it's a plain TCP connection (the common case for WebGL clients), use it.
	if tcp, ok := raw.(*net.TCPConn); ok {
		return tcp
	}
	// Fallback: return the raw net.Conn (may not support deadlines).
	return raw
}

// SetDeadline implements net.Conn.SetDeadline.
func (w *WSConn) SetDeadline(t time.Time) error {
	return w.deadlineConn().SetDeadline(t)
}

// SetReadDeadline implements net.Conn.SetReadDeadline.
func (w *WSConn) SetReadDeadline(t time.Time) error {
	return w.deadlineConn().SetReadDeadline(t)
}

// SetWriteDeadline implements net.Conn.SetWriteDeadline.
func (w *WSConn) SetWriteDeadline(t time.Time) error {
	return w.deadlineConn().SetWriteDeadline(t)
}
