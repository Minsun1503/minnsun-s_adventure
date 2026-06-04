package transport

import (
	"net/http"

	"github.com/gorilla/websocket"

	"server/auth"
	"server/logger"
)

// ─── WebSocket Upgrader ───────────────────────────────────────────────────────

// upgrader is the gorilla/websocket upgrader used to upgrade incoming HTTP
// connections to WebSocket.  Read/Write buffer sizes are kept small to avoid
// wasting memory on idle WebGL clients.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,

	// Allow all origins during development.
	// PRODUCTION: Replace the wildcard check with a domain whitelist, e.g.:
	//   CheckOrigin: func(r *http.Request) bool {
	//       return r.Header.Get("Origin") == "https://yourgame.com"
	//   },
	CheckOrigin: func(r *http.Request) bool { return true },
}

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
	case auth.LoginQueue <- wsConn:
	default:
		logger.Warn("[WS] Login queue full — dropping WebSocket connection from %s", conn.RemoteAddr())
		// Send error frame and close.
		conn.Close()
	}
}

// ─── StartWebSocketListener ───────────────────────────────────────────────────

// StartWebSocketListener starts an HTTP server on addr that upgrades /ws
// connections to WebSocket and pushes them into the shared auth.LoginQueue.
//
// This function blocks — call it in a goroutine from main().
func StartWebSocketListener(addr string) {
	http.HandleFunc("/ws", handleWebSocket)

	logger.Info("[BOOT] WebSocket listener starting on %s/ws", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logger.Error("[BOOT] WebSocket listener error: %v", err)
	}
}
