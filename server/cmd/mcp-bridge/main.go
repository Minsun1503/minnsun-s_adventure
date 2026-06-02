package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
							"description": "Dịch chuyển tức thời một nhân vật đến tọa độ mới (map_id mặc định = 1)",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"player_id": map[string]any{"type": "number", "description": "ID của người chơi"},
									"map_id":    map[string]any{"type": "number", "description": "ID map (mặc định = 1)"},
									"x":         map[string]any{"type": "number", "description": "Tọa độ X"},
									"z":         map[string]any{"type": "number", "description": "Tọa độ Z"},
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
						// ─── Token-Saver Tools ─────────────────────────────────────────
						{
							"name":        "code_get_outline",
							"description": "Trả về danh sách Struct, Fields, Function/Method Signatures (KHÔNG body) của file .go để xem nhanh cấu trúc mà không cần đọc toàn bộ file.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"file_path": map[string]any{"type": "string", "description": "Đường dẫn tới file .go (tương đối hoặc tuyệt đối)"},
								},
								"required": []string{"file_path"},
							},
						},
						{
							"name":        "code_read_lines",
							"description": "Đọc một khoảng dòng cụ thể từ file (start_line đến end_line), chỉ trả về đúng phân đoạn đó để tiết kiệm token.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"file_path":  map[string]any{"type": "string", "description": "Đường dẫn tới file cần đọc"},
									"start_line": map[string]any{"type": "number", "description": "Dòng bắt đầu (1-based)"},
									"end_line":   map[string]any{"type": "number", "description": "Dòng kết thúc (inclusive)"},
								},
								"required": []string{"file_path", "start_line", "end_line"},
							},
						},
						{
							"name":        "db_get_schema",
							"description": "Truy vấn MySQL để trả về cấu trúc bảng (columns, types, keys) mà không cần đọc file SQL.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"table_name": map[string]any{"type": "string", "description": "Tên bảng cần xem schema (ví dụ: characters, character_states, character_inventory)"},
								},
								"required": []string{"table_name"},
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

			// Handle local (bridge-side) tools first
			var handled bool

			switch callParams.Name {
			case "server_grep":
				results, err := handleGrep(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("grep error: %v", err))
					continue
				}
				sendMCPTextResult(req.ID, formatGrepResults(results))
				handled = true

			case "code_get_outline":
				result, err := handleCodeOutline(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("outline error: %v", err))
					continue
				}
				sendMCPTextResult(req.ID, result)
				handled = true

			case "code_read_lines":
				result, err := handleReadLines(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("read_lines error: %v", err))
					continue
				}
				sendMCPTextResult(req.ID, result)
				handled = true
			}

			if handled {
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

			// Forward the raw backend JSON response as-is
			sendMCPTextResult(req.ID, string(respBody))
		}
	}
}

// ─── Local Tool Handlers ──────────────────────────────────────────────────────

// handleCodeOutline parses a .go file and returns struct types + function signatures only.
func handleCodeOutline(args json.RawMessage) (string, error) {
	var p struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.FilePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	// Path Sanitizer: normalize slashes and strip duplicate "server/" prefix
	cleanPath := filepath.ToSlash(p.FilePath)
	cleanPath = strings.TrimPrefix(cleanPath, "server/")
	cleanPath = filepath.FromSlash(cleanPath)

	// Resolve relative paths against serverDir
	fullPath := cleanPath
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(serverDir, fullPath)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("package %s\n\n", f.Name.Name))

	// Collect types, functions, and methods
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						// Non-struct type alias (e.g. type Entity uint64)
						b.WriteString(fmt.Sprintf("type %s %s\n", ts.Name.Name, typeExprStr(ts.Type)))
						continue
					}
					b.WriteString(fmt.Sprintf("type %s struct {\n", ts.Name.Name))
					for _, field := range st.Fields.List {
						names := make([]string, len(field.Names))
						for i, n := range field.Names {
							names[i] = n.Name
						}
						b.WriteString(fmt.Sprintf("    %s %s\n",
							strings.Join(names, ", "),
							typeExprStr(field.Type)))
					}
					b.WriteString("}\n")
				}
			}
			if d.Tok == token.CONST {
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if vs.Type != nil {
							b.WriteString(fmt.Sprintf("const %s %s\n", name.Name, typeExprStr(vs.Type)))
						} else {
							b.WriteString(fmt.Sprintf("const %s\n", name.Name))
						}
					}
				}
			}
			if d.Tok == token.VAR {
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if vs.Type != nil {
							b.WriteString(fmt.Sprintf("var %s %s\n", name.Name, typeExprStr(vs.Type)))
						} else {
							b.WriteString(fmt.Sprintf("var %s\n", name.Name))
						}
					}
				}
			}

		case *ast.FuncDecl:
			recv := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recv = "(" + typeExprStr(d.Recv.List[0].Type) + ") "
			}
			params := formatFieldList(d.Type.Params)
			results := formatFieldList(d.Type.Results)
			if results != "" {
				results = " " + results
			}
			b.WriteString(fmt.Sprintf("func %s%s(%s)%s\n", recv, d.Name.Name, params, results))
		}
	}

	return b.String(), nil
}

// handleReadLines reads a specific range of lines from a file.
func handleReadLines(args json.RawMessage) (string, error) {
	var p struct {
		FilePath  string `json:"file_path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if p.FilePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if p.StartLine < 1 {
		return "", fmt.Errorf("start_line must be >= 1")
	}
	if p.EndLine < p.StartLine {
		return "", fmt.Errorf("end_line must be >= start_line")
	}

	// Path Sanitizer: normalize slashes and strip duplicate "server/" prefix
	cleanPath := filepath.ToSlash(p.FilePath)
	cleanPath = strings.TrimPrefix(cleanPath, "server/")
	cleanPath = filepath.FromSlash(cleanPath)

	fullPath := cleanPath
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join(serverDir, fullPath)
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return "", fmt.Errorf("open error: %w", err)
	}
	defer f.Close()

	var b strings.Builder
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum > p.EndLine {
			break
		}
		if lineNum >= p.StartLine {
			b.WriteString(scanner.Text())
			b.WriteString("\n")
		}
	}

	if lineNum < p.StartLine {
		return "", fmt.Errorf("file has only %d lines, requested start=%d", lineNum, p.StartLine)
	}

	return b.String(), scanner.Err()
}

// ─── AST Helpers ──────────────────────────────────────────────────────────────

func typeExprStr(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeExprStr(t.X)
	case *ast.SelectorExpr:
		return typeExprStr(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeExprStr(t.Elt)
		}
		return "[" + typeExprStr(t.Len) + "]" + typeExprStr(t.Elt)
	case *ast.MapType:
		return "map[" + typeExprStr(t.Key) + "]" + typeExprStr(t.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.FuncType:
		return "func(" + formatFieldList(t.Params) + ")"
	case *ast.Ellipsis:
		return "..." + typeExprStr(t.Elt)
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func formatFieldList(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	parts := make([]string, len(fl.List))
	for i, f := range fl.List {
		typeStr := typeExprStr(f.Type)
		if len(f.Names) == 0 {
			parts[i] = typeStr
		} else {
			names := make([]string, len(f.Names))
			for j, n := range f.Names {
				names[j] = n.Name
			}
			parts[i] = strings.Join(names, ", ") + " " + typeStr
		}
	}
	return strings.Join(parts, ", ")
}

// ─── Bridge Infrastructure ────────────────────────────────────────────────────

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
	} else {
		// Path Sanitizer: normalize slashes and strip duplicate "server/" prefix
		cleanRoot := filepath.ToSlash(root)
		cleanRoot = strings.TrimPrefix(cleanRoot, "server/")
		root = filepath.FromSlash(cleanRoot)
		if !filepath.IsAbs(root) {
			root = filepath.Join(serverDir, root)
		}
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

// sendMCPTextResult sends a tool result wrapped in MCP "content" array format.
func sendMCPTextResult(id json.RawMessage, text string) {
	mcpResult := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": text,
				},
			},
		},
	}
	out, _ := json.Marshal(mcpResult)
	fmt.Println(string(out))
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
