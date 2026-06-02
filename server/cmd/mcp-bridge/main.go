package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	gameServerPort  = ":1503"
	mcpEndpoint     = "http://localhost:8080/mcp"
	healthEndpoint  = "http://localhost:8080/health"
	serverStartWait = 5 * time.Second
)

// Cấu trúc bắt gói tin từ Cline
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

// Params đặc thù của lệnh tools/call trong MCP
type MCPToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type GrepParams struct {
	Pattern string `json:"pattern"`
	Root    string `json:"root"`
}

type GrepResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

var serverDir string

func main() {
	// Compute serverDir once for reuse across bridge
	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}
	serverDir = filepath.Clean(filepath.Dir(exePath))
	if strings.HasSuffix(filepath.ToSlash(serverDir), "/cmd/mcp-bridge") {
		serverDir = filepath.Dir(filepath.Dir(serverDir))
	}

	// Phase 1: Vòng đời tự động (Giữ nguyên logic thông minh của bạn)
	if !isServerRunning() {
		fmt.Fprintf(os.Stderr, "[MCP-BRIDGE] Game server not detected. Starting...\n")
		if err := startGameServer(); err != nil {
			fmt.Fprintf(os.Stderr, "[MCP-BRIDGE] FATAL: %v\n", err)
			os.Exit(1)
		}
		if err := waitForHealth(); err != nil {
			fmt.Fprintf(os.Stderr, "[MCP-BRIDGE] FATAL: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "[MCP-BRIDGE] Connected to Go Backend. Protocol translating active...\n")

	// Phase 2: Đọc dữ liệu Stdio liên tục từ Cline
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		// Xử lý lệch pha giao thức ngay tại Bridge
		switch req.Method {
		case "initialize":
			// Tự bắt tay với Cline theo chuẩn MCP
			initResponse := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "minnsun-adventure-mcp", "version": "1.0.0"},
				},
			}
			out, _ := json.Marshal(initResponse)
			fmt.Println(string(out))

		case "notifications/initialized":
			// Chỉ là thông báo từ Cline, bỏ qua không cần trả lời

		case "tools/list":
			// Định nghĩa danh sách Tool chuẩn schema MCP để khai báo với Cline
			// Tôi làm mẫu 2 tool đại diện, bạn có thể copy-paste thêm các tool khác vào mảng này
			toolsResponse := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name":        "ecs_list_entities",
							"description": "Lấy danh sách tất cả thực thể trong bộ nhớ RAM (monster/player/npc)",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"type": map[string]any{"type": "string", "description": "Lọc theo: monster, player, hoặc npc"},
								},
							},
						},
						{
							"name":        "admin_teleport",
							"description": "Dịch chuyển tức thời một nhân vật đến tọa độ mới",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"player_id": map[string]any{"type": "number"},
									"x":         map[string]any{"type": "number"},
									"z":         map[string]any{"type": "number"},
								},
								"required": []string{"player_id", "x", "z"},
							},
						},
						{
							"name":        "server_grep",
							"description": "Recursively search .go files for a pattern under a given root path (pure Go, no shell)",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"pattern": map[string]any{"type": "string", "description": "Text pattern to search (case-sensitive)"},
									"root":    map[string]any{"type": "string", "description": "Root directory to search (default: server directory)"},
								},
								"required": []string{"pattern"},
							},
						},
					},
				},
			}
			out, _ := json.Marshal(toolsResponse)
			fmt.Println(string(out))

		case "tools/call":
			// DỊCH GIAO THỨC: Chuyển tools/call của MCP thành hàm gốc của Go Server
			var callParams MCPToolCallParams
			if err := json.Unmarshal(req.Params, &callParams); err != nil {
				sendError(req.ID, -32602, "Invalid tools/call params")
				continue
			}

			// Handle server_grep locally (no backend roundtrip needed)
			if callParams.Name == "server_grep" {
				results, err := handleGrep(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("grep error: %v", err))
					continue
				}
				mcpResult := map[string]any{
					"jsonrpc": "2.0",
					"id":      req.ID,
					"result": map[string]any{
						"content": []map[string]any{
							{
								"type": "text",
								"text": formatGrepResults(results),
							},
						},
					},
				}
				out, _ := json.Marshal(mcpResult)
				fmt.Println(string(out))
				continue
			}

			// Đóng gói lại thành struct JSON-RPC dạng phẳng gửi cho Go HTTP Server
			backendReq := map[string]any{
				"jsonrpc": "2.0",
				"method":  callParams.Name,
				"params":  callParams.Arguments,
				"id":      req.ID,
			}
			backendJSON, _ := json.Marshal(backendReq)

			respBody, err := proxyRequest(string(backendJSON))
			if err != nil {
				sendError(req.ID, -32603, fmt.Sprintf("Backend unreachable: %v", err))
				continue
			}

			// MCP yêu cầu kết quả trả về của tool phải bọc trong mảng "content" text
			var backendResp map[string]any
			_ = json.Unmarshal([]byte(respBody), &backendResp)

			mcpResult := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]any{
						{
							"type": "text",
							"text": string(respBody), // Trả toàn bộ cục JSON kết quả gốc của game về cho AI đọc
						},
					},
				},
			}
			out, _ := json.Marshal(mcpResult)
			fmt.Println(string(out))
		}
	}
}

func isServerRunning() bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1"+gameServerPort, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func startGameServer() error {
	serverExe := filepath.Join(serverDir, "server.exe")

	cmd := exec.Command(serverExe)
	cmd.Dir = serverDir
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func waitForHealth() error {
	deadline := time.Now().Add(serverStartWait)
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthEndpoint)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("server target timed out")
}

func proxyRequest(requestBody string) (string, error) {
	resp, err := http.Post(mcpEndpoint, "application/json", strings.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func handleGrep(args json.RawMessage) ([]GrepResult, error) {
	var params GrepParams
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if params.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	root := params.Root
	if root == "" {
		root = serverDir
	}

	var results []GrepResult
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.Contains(line, params.Pattern) {
				results = append(results, GrepResult{
					File:    path,
					Line:    lineNum,
					Content: strings.TrimSpace(line),
				})
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	if results == nil {
		results = []GrepResult{}
	}
	return results, nil
}

func formatGrepResults(results []GrepResult) string {
	if len(results) == 0 {
		return "No matches found."
	}
	var b strings.Builder
	for _, r := range results {
		b.WriteString(fmt.Sprintf("%s:%d: %s\n", r.File, r.Line, r.Content))
	}
	return b.String()
}

func sendError(id json.RawMessage, code int, msg string) {
	errResp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	}
	out, _ := json.Marshal(errResp)
	fmt.Println(string(out))
}
