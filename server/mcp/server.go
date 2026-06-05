// Package mcp provides a JSON-RPC 2.0 MCP (Model Context Protocol) server
// for Cline/AI agents to inspect and control the Minnsun's Adventure game server.
//
// The server runs on a dedicated HTTP port (default :8080) separate from the
// game TCP port (:1503). All endpoints use JSON-RPC 2.0 request/response format.
//
// # Security
//
// Access is controlled by a pre-shared API key passed via the X-API-Key header.
// In production, the key should be set via environment variable MCP_API_KEY.
// If no key is configured, requests without a key are allowed (dev mode).
//
// # Performance
//
// MCP handlers are non-blocking and safe for concurrent access with the game loop.
// Read-only queries acquire RLock; mutation commands use the standard ECS CoW pattern.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"server/ecs"
	"server/logger"
	"sync"
	"syscall"
	"time"
)

// ─── JSON-RPC Types ───────────────────────────────────────────────────────────

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string    `json:"jsonrpc"`
	Result  any       `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
	ID      any       `json:"id"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidReq     = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// ─── Error Responses ─────────────────────────────────────────────────────────

func rpcError(id any, code int, msg string) Response {
	return Response{
		JSONRPC: "2.0",
		Error:   &RPCError{Code: code, Message: msg},
		ID:      id,
	}
}

func rpcResult(id any, result any) Response {
	return Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      id,
	}
}

// ─── Handler Type ────────────────────────────────────────────────────────────

// HandlerFunc processes a validated JSON-RPC request and returns a response.
type HandlerFunc func(req Request) Response

// ─── Method Registry ─────────────────────────────────────────────────────────

var (
	mu      sync.RWMutex
	methods = make(map[string]HandlerFunc)
)

// Register registers a handler for the given JSON-RPC method name.
// Must be called before Start (typically in init() or at boot).
func Register(method string, handler HandlerFunc) {
	mu.Lock()
	methods[method] = handler
	mu.Unlock()
}

// ─── HTTP MCP Server ─────────────────────────────────────────────────────────

// Config holds the MCP server configuration.
type Config struct {
	Port   int    // HTTP listen port (default 8080)
	APIKey string // Pre-shared API key (empty = dev mode, no auth)
}

var activeConfig Config

// Start begins the MCP HTTP server in a background goroutine.
// Blocks until the server is ready or fails to bind.
func Start(cfg Config) {
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("MCP_API_KEY")
	}
	activeConfig = cfg

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", withAuth(jsonRPCHandler))
	mux.HandleFunc("/health", healthHandler)

	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown on interrupt.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	go func() {
		logger.Info("[MCP] JSON-RPC server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("[MCP] HTTP server error: %v", err)
		}
		logger.Info("[MCP] HTTP server shut down.")
	}()
}

// healthHandler is a simple health-check endpoint.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"mcp":    true,
	})
}

// withAuth wraps a handler with API key authentication.
func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// CORS headers for local development.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Auth check.
		if activeConfig.APIKey != "" {
			key := r.Header.Get("X-API-Key")
			if key != activeConfig.APIKey {
				writeJSONError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}
		}

		next(w, r)
	}
}

// jsonRPCHandler is the single HTTP endpoint for all JSON-RPC calls.
func jsonRPCHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "only POST method is accepted")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONRPCResponse(w, rpcError(nil, ErrCodeParse, "failed to read request body"))
		return
	}

	// Support both single request and batch arrays.
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeJSONRPCResponse(w, rpcError(nil, ErrCodeParse, "invalid JSON"))
		return
	}

	switch raw.(type) {
	case []any:
		// Batch request: process each request sequentially.
		var batch []Request
		if err := json.Unmarshal(body, &batch); err != nil {
			writeJSONRPCResponse(w, rpcError(nil, ErrCodeParse, "invalid batch JSON"))
			return
		}
		responses := make([]Response, 0, len(batch))
		for _, req := range batch {
			resp := dispatch(req)
			if resp.ID != nil {
				responses = append(responses, resp)
			}
		}
		if responses == nil {
			responses = []Response{} // always return an array for batch
		}
		writeJSON(w, http.StatusOK, responses)

	case map[string]any:
		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONRPCResponse(w, rpcError(nil, ErrCodeParse, "invalid request JSON"))
			return
		}
		resp := dispatch(req)
		writeJSONRPCResponse(w, resp)

	default:
		writeJSONRPCResponse(w, rpcError(nil, ErrCodeInvalidReq, "request must be an object or array"))
	}
}

// dispatch routes a single JSON-RPC request to the registered handler.
func dispatch(req Request) Response {
	if req.JSONRPC != "2.0" {
		return rpcError(req.ID, ErrCodeInvalidReq, "jsonrpc field must be '2.0'")
	}
	if req.Method == "" {
		return rpcError(req.ID, ErrCodeInvalidReq, "method is required")
	}

	mu.RLock()
	handler, ok := methods[req.Method]
	mu.RUnlock()

	if !ok {
		return rpcError(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("method '%s' not found", req.Method))
	}

	return handler(req)
}

// ─── Response Writers ─────────────────────────────────────────────────────────

func writeJSONRPCResponse(w http.ResponseWriter, resp Response) {
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, statusCode int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ─── Standard Param Parsers ─────────────────────────────────────

// parseEntityParam extracts a single "id" uint64 parameter.
func parseEntityParam(params json.RawMessage) (ecs.Entity, error) {
	var p struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return 0, fmt.Errorf("missing or invalid 'id' parameter: %w", err)
	}
	if p.ID == 0 {
		return 0, fmt.Errorf("'id' must be a positive integer")
	}
	return ecs.Entity(p.ID), nil
}

// parseOptionalEntityParam extracts an optional "id" parameter (0 if not provided).
func parseOptionalEntityParam(params json.RawMessage) (ecs.Entity, error) {
	var p struct {
		ID uint64 `json:"id"`
	}
	if len(params) == 0 {
		return 0, nil // no params provided — caller handles default
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return 0, fmt.Errorf("invalid 'id' parameter: %w", err)
	}
	return ecs.Entity(p.ID), nil
}

// EntityInfo is a serializable summary of an entity's components.
type EntityInfo struct {
	ID      uint64 `json:"id"`
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	MapID   int    `json:"map_id,omitempty"`
	X       int    `json:"x,omitempty"`
	Z       int    `json:"z,omitempty"`
	HP      int    `json:"hp,omitempty"`
	MaxHP   int    `json:"max_hp,omitempty"`
	MP      int    `json:"mp,omitempty"`
	MaxMP   int    `json:"max_mp,omitempty"`
	Damage  int    `json:"damage,omitempty"`
	Level   int    `json:"level,omitempty"`
	XP      uint64 `json:"xp,omitempty"`
	Weapon  uint64 `json:"weapon_id,omitempty"`
	Armor   uint64 `json:"armor_id,omitempty"`
	AIState string `json:"ai_state,omitempty"`
}

// buildEntityInfo builds an EntityInfo from the entity's components.
func buildEntityInfo(id ecs.Entity) EntityInfo {
	info := EntityInfo{ID: uint64(id)}

	if meta, ok := ecs.DefaultRegistry.GetMetadata(id); ok {
		info.Name = meta.Name
		info.Type = meta.Type.String()
	}
	if pos, ok := ecs.DefaultRegistry.GetPosition(id); ok {
		info.MapID = pos.MapID
		info.X = pos.X
		info.Z = pos.Z
	}
	if stats, ok := ecs.DefaultRegistry.GetStats(id); ok {
		info.HP = stats.HP
		info.MaxHP = stats.MaxHP
		info.MP = stats.MP
		info.MaxMP = stats.MaxMP
		info.Damage = stats.Dam
		info.Level = stats.Level
		info.XP = stats.XP
	}
	if eq, ok := ecs.DefaultRegistry.GetEquipment(id); ok {
		info.Weapon = eq.WeaponID
		info.Armor = eq.ArmorID
	}
	if ai, ok := ecs.DefaultRegistry.GetAI(id); ok {
		info.AIState = ai.State.String()
	}

	return info
}
