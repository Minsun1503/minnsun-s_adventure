package transport

import (
	"net"
	"net/http"

	"github.com/gorilla/websocket"

	"server/logger"
)

// ─── WebSocket Upgrader ───────────────────────────────────────────────────────

// upgrader is the gorilla/websocket upgrader used to upgrade incoming HTTP
// connections to WebSocket.  Read/Write buffer sizes are kept small to avoid
// wasting memory on idle WebGL clients.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,

	// Allow all origins during development.  In production, restrict this to
	// your actual game domain.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ─── External Login Queue ─────────────────────────────────────────────────────

// LoginQueue is the shared channel that accepts both TCP and WebSocket connections.
// It must be set before calling StartWebSocketListener — the existing LoginQueue
// from server.go is used directly.
//
//	server.LoginQueue ← TCP connections from :1503
//	                    ← WebSocket connections from :8080/ws
//
// Both transport types push net.Conn values into the same queue, so the existing
// worker pool (processLogin → handleClient → handleBinaryPacket) handles both
// transparently.
var LoginQueue chan net.Conn

// ─── WebSocket Handler ────────────────────────────────────────────────────────

// handleWebSocket upgrades an HTTP request to a WebSocket connection, wraps it
// in a WSConn, and pushes it into the shared LoginQueue.
//
// The first message from the client MUST be a LOGIN or REGISTER binary packet,
// just like TCP — processLogin enforces this.
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("[WS] Upgrade error from %s: %v", r.RemoteAddr, err)
		return
	}

	wsConn := NewWSConn(conn)
	logger.Info("[WS] Connection from %s → wrapping as net.Conn", conn.RemoteAddr())

	// Push into the shared login queue — the same worker pool handles it.
	select {
	case LoginQueue <- wsConn:
	default:
		logger.Warn("[WS] Login queue full — dropping WebSocket connection from %s", conn.RemoteAddr())
		// Send error frame and close.
		conn.Close()
	}
}

// ─── StartWebSocketListener ───────────────────────────────────────────────────

// StartWebSocketListener starts an HTTP server on addr that upgrades /ws
// connections to WebSocket and pushes them into the shared LoginQueue.
//
// Parameters:
//   - addr:      Bind address, e.g. ":8080"
//   - loginChan: Shared LoginQueue from server.go
//
// This function blocks — call it in a goroutine from main().
func StartWebSocketListener(addr string, loginChan chan net.Conn) {
	LoginQueue = loginChan

	http.HandleFunc("/ws", handleWebSocket)

	logger.Info("[BOOT] WebSocket listener starting on %s/ws", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Error("[BOOT] WebSocket listener error: %v", err)
	}
}
