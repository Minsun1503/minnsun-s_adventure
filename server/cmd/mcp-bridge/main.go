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
	"regexp"
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
var workspaceDir string

func resolvePath(inputPath string) string {
	if filepath.IsAbs(inputPath) {
		return filepath.Clean(inputPath)
	}

	cleanPath := filepath.ToSlash(inputPath)
	cleanPath = strings.TrimPrefix(cleanPath, "server/")
	cleanPath = filepath.FromSlash(cleanPath)

	// Try resolving original inputPath against workspaceDir first
	pathInWorkspace := filepath.Join(workspaceDir, inputPath)
	if _, err := os.Stat(pathInWorkspace); err == nil {
		return pathInWorkspace
	}

	// Try resolving cleanPath against workspaceDir
	pathCleanInWorkspace := filepath.Join(workspaceDir, cleanPath)
	if _, err := os.Stat(pathCleanInWorkspace); err == nil {
		return pathCleanInWorkspace
	}

	// Try resolving cleanPath against serverDir
	pathInServer := filepath.Join(serverDir, cleanPath)
	if _, err := os.Stat(pathInServer); err == nil {
		return pathInServer
	}

	return pathCleanInWorkspace
}

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
	workspaceDir = filepath.Dir(serverDir)

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
							"name":        "code_extract",
							"description": "Trích xuất thông tin cấu trúc code Go (struct, interface, function, method, variable). Hỗ trợ liệt kê class, liệt kê member, tìm class chứa member, hoặc trích xuất nội dung chi tiết.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path":        map[string]any{"type": "string", "description": "Đường dẫn tới file hoặc thư mục Go"},
									"action":      map[string]any{"type": "string", "description": "Hành động: list_classes, list_class_members, find_member_class, extract_member"},
									"class_name":  map[string]any{"type": "string", "description": "Tên class/struct/interface (bắt buộc cho list_class_members, tùy chọn cho extract_member)"},
									"member_name": map[string]any{"type": "string", "description": "Tên function/method/field/variable cần tìm/trích xuất"},
								},
								"required": []string{"path", "action"},
							},
						},
						{
							"name":        "replace",
							"description": "Thay thế chuỗi văn bản trong file hoặc các file trong thư mục (tương tự Ctrl+H trên VS Code).",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path":         map[string]any{"type": "string", "description": "Đường dẫn tới file hoặc thư mục chứa các file cần thay thế"},
									"search_text":  map[string]any{"type": "string", "description": "Chuỗi văn bản cần tìm kiếm"},
									"replace_text": map[string]any{"type": "string", "description": "Chuỗi văn bản dùng để thay thế"},
									"glob":         map[string]any{"type": "string", "description": "Glob pattern để lọc file nếu path là thư mục (mặc định: *.go)"},
									"use_regex":    map[string]any{"type": "boolean", "description": "Sử dụng Regular Expression để tìm kiếm và thay thế (mặc định: false)"},
									"start_line":   map[string]any{"type": "number", "description": "Dòng bắt đầu giới hạn thay thế (tùy chọn, 1-based)"},
									"end_line":     map[string]any{"type": "number", "description": "Dòng kết thúc giới hạn thay thế (tùy chọn, inclusive)"},
								},
								"required": []string{"path", "search_text", "replace_text"},
							},
						},
						{
							"name":        "list_files",
							"description": "Liệt kê danh sách các tệp và thư mục con trong một thư mục cụ thể.",
							"inputSchema": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"path":      map[string]any{"type": "string", "description": "Đường dẫn tới thư mục cần liệt kê"},
									"recursive": map[string]any{"type": "boolean", "description": "Liệt kê đệ quy toàn bộ thư mục con (mặc định: false)"},
									"glob":      map[string]any{"type": "string", "description": "Glob pattern để lọc tên file (tùy chọn, ví dụ: *.go)"},
								},
								"required": []string{"path"},
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
				sendError(req.ID, -32602, fmt.Sprintf("Invalid tools/call params: %v (raw: %s)", err, string(req.Params)))
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

			case "code_extract":
				result, err := handleCodeExtract(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("extract error: %v", err))
					continue
				}
				sendMCPTextResult(req.ID, result)
				handled = true

			case "replace":
				result, err := handleReplace(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("replace error: %v", err))
					continue
				}
				sendMCPTextResult(req.ID, result)
				handled = true

			case "list_files":
				result, err := handleListFiles(callParams.Arguments)
				if err != nil {
					sendError(req.ID, -32603, fmt.Sprintf("list_files error: %v", err))
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
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "[MCP-BRIDGE] Stdin scanner error: %v\n", err)
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

	fullPath := resolvePath(p.FilePath)

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
// CodeExtractParams holds input arguments for code_extract tool.
type CodeExtractParams struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	ClassName  string `json:"class_name"`
	MemberName string `json:"member_name"`
}

// ClassInfo represents information about an extracted struct or interface.
type ClassInfo struct {
	Name string `json:"name"`
	Type string `json:"type"` // "struct" or "interface"
	File string `json:"file"`
}

// MemberInfo represents information about a struct field/method or interface method.
type MemberInfo struct {
	Name string `json:"name"`
	Type string `json:"type"` // "field", "method", "interface_method", or "embedded_interface"
	Sig  string `json:"signature,omitempty"`
}

// FindResult represents search results tracing back from a member name to its class.
type FindResult struct {
	ClassName  string `json:"class_name"`
	Type       string `json:"type"`        // "struct" or "interface"
	MemberType string `json:"member_type"` // "field", "method", or "interface_method"
	File       string `json:"file"`
}

func handleCodeExtract(args json.RawMessage) (string, error) {
	var p CodeExtractParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid json arguments: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.Action == "" {
		return "", fmt.Errorf("action is required")
	}

	fullPath := resolvePath(p.Path)

	// Determine if path is file or directory
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("stat path error: %w", err)
	}

	var files []string
	if info.IsDir() {
		err = filepath.WalkDir(fullPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && strings.HasSuffix(path, ".go") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk dir error: %w", err)
		}
	} else {
		files = append(files, fullPath)
	}

	switch p.Action {
	case "list_classes":
		return actionListClasses(files)
	case "list_class_members":
		if p.ClassName == "" {
			return "", fmt.Errorf("class_name is required for action list_class_members")
		}
		return actionListClassMembers(files, p.ClassName)
	case "find_member_class":
		if p.MemberName == "" {
			return "", fmt.Errorf("member_name is required for action find_member_class")
		}
		return actionFindMemberClass(files, p.MemberName)
	case "extract_member":
		if p.MemberName == "" {
			return "", fmt.Errorf("member_name is required for action extract_member")
		}
		return actionExtractMember(files, p.ClassName, p.MemberName)
	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

func actionListClasses(files []string) (string, error) {
	var classes []ClassInfo
	fset := token.NewFileSet()
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue // skip files with syntax errors
		}
		relFile, _ := filepath.Rel(serverDir, file)
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				classType := "other"
				switch ts.Type.(type) {
				case *ast.StructType:
					classType = "struct"
				case *ast.InterfaceType:
					classType = "interface"
				}
				if classType == "struct" || classType == "interface" {
					classes = append(classes, ClassInfo{
						Name: ts.Name.Name,
						Type: classType,
						File: filepath.ToSlash(relFile),
					})
				}
			}
		}
	}
	out, err := json.MarshalIndent(classes, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func actionListClassMembers(files []string, className string) (string, error) {
	var members []MemberInfo
	fset := token.NewFileSet()
	foundClass := false

	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue
		}

		// Check if the class is defined in this file
		var structType *ast.StructType
		var interfaceType *ast.InterfaceType
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name.Name != className {
					continue
				}
				foundClass = true
				if st, ok := ts.Type.(*ast.StructType); ok {
					structType = st
				} else if it, ok := ts.Type.(*ast.InterfaceType); ok {
					interfaceType = it
				}
			}
		}

		if structType != nil {
			if structType.Fields != nil {
				for _, field := range structType.Fields.List {
					typeStr := typeExprStr(field.Type)
					if len(field.Names) == 0 {
						members = append(members, MemberInfo{
							Name: typeStr,
							Type: "field",
							Sig:  typeStr,
						})
					} else {
						for _, name := range field.Names {
							members = append(members, MemberInfo{
								Name: name.Name,
								Type: "field",
								Sig:  typeStr,
							})
						}
					}
				}
			}
		}

		if interfaceType != nil {
			if interfaceType.Methods != nil {
				for _, field := range interfaceType.Methods.List {
					typeStr := typeExprStr(field.Type)
					if len(field.Names) == 0 {
						members = append(members, MemberInfo{
							Name: typeStr,
							Type: "embedded_interface",
							Sig:  typeStr,
						})
					} else {
						for _, name := range field.Names {
							members = append(members, MemberInfo{
								Name: name.Name,
								Type: "interface_method",
								Sig:  typeStr,
							})
						}
					}
				}
			}
		}
	}

	if !foundClass {
		return "", fmt.Errorf("class %s not found in the specified path", className)
	}

	// Scan all files to find methods belonging to className
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			recvType := typeExprStr(fd.Recv.List[0].Type)
			recvType = strings.TrimPrefix(recvType, "*")
			if recvType == className {
				params := formatFieldList(fd.Type.Params)
				results := formatFieldList(fd.Type.Results)
				if results != "" {
					results = " " + results
				}
				members = append(members, MemberInfo{
					Name: fd.Name.Name,
					Type: "method",
					Sig:  fmt.Sprintf("func (%s) (%s)%s", typeExprStr(fd.Recv.List[0].Type), params, results),
				})
			}
		}
	}

	out, err := json.MarshalIndent(members, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func actionFindMemberClass(files []string, memberName string) (string, error) {
	var results []FindResult
	fset := token.NewFileSet()

	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		relFile, _ := filepath.Rel(serverDir, file)

		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				className := ts.Name.Name
				if st, ok := ts.Type.(*ast.StructType); ok {
					if st.Fields != nil {
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								if name.Name == memberName {
									results = append(results, FindResult{
										ClassName:  className,
										Type:       "struct",
										MemberType: "field",
										File:       filepath.ToSlash(relFile),
									})
								}
							}
						}
					}
				} else if it, ok := ts.Type.(*ast.InterfaceType); ok {
					if it.Methods != nil {
						for _, field := range it.Methods.List {
							for _, name := range field.Names {
								if name.Name == memberName {
									results = append(results, FindResult{
										ClassName:  className,
										Type:       "interface",
										MemberType: "interface_method",
										File:       filepath.ToSlash(relFile),
									})
								}
							}
						}
					}
				}
			}
		}

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 || fd.Name.Name != memberName {
				continue
			}
			recvType := typeExprStr(fd.Recv.List[0].Type)
			recvType = strings.TrimPrefix(recvType, "*")
			results = append(results, FindResult{
				ClassName:  recvType,
				Type:       "struct",
				MemberType: "method",
				File:       filepath.ToSlash(relFile),
			})
		}
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func actionExtractMember(files []string, className, memberName string) (string, error) {
	fset := token.NewFileSet()

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		f, err := parser.ParseFile(fset, file, src, parser.ParseComments)
		if err != nil {
			continue
		}

		relFile, _ := filepath.Rel(serverDir, file)

		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != memberName {
				continue
			}

			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				recvType := typeExprStr(fd.Recv.List[0].Type)
				recvType = strings.TrimPrefix(recvType, "*")
				if className != "" && recvType != className {
					continue
				}
			} else {
				if className != "" {
					continue
				}
			}

			start := fset.Position(fd.Pos()).Offset
			end := fset.Position(fd.End()).Offset
			return fmt.Sprintf("Source file: %s\n\n%s", filepath.ToSlash(relFile), string(src[start:end])), nil
		}

		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}

			if gd.Tok == token.TYPE {
				for _, spec := range gd.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if className != "" && ts.Name.Name != className {
						continue
					}

					if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								if name.Name == memberName {
									start := fset.Position(field.Pos()).Offset
									end := fset.Position(field.End()).Offset
									return fmt.Sprintf("Source file: %s (Struct: %s)\nLine content: %s", filepath.ToSlash(relFile), ts.Name.Name, strings.TrimSpace(string(src[start:end]))), nil
								}
							}
						}
					} else if it, ok := ts.Type.(*ast.InterfaceType); ok && it.Methods != nil {
						for _, field := range it.Methods.List {
							for _, name := range field.Names {
								if name.Name == memberName {
									start := fset.Position(field.Pos()).Offset
									end := fset.Position(field.End()).Offset
									return fmt.Sprintf("Source file: %s (Interface: %s)\nLine content: %s", filepath.ToSlash(relFile), ts.Name.Name, strings.TrimSpace(string(src[start:end]))), nil
								}
							}
						}
					}
				}
			}

			if className == "" && (gd.Tok == token.VAR || gd.Tok == token.CONST) {
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						if name.Name == memberName {
							start := fset.Position(spec.Pos()).Offset
							end := fset.Position(spec.End()).Offset
							varType := "VAR"
							if gd.Tok == token.CONST {
								varType = "CONST"
							}
							return fmt.Sprintf("Source file: %s (%s)\nLine content: %s", filepath.ToSlash(relFile), varType, strings.TrimSpace(string(src[start:end]))), nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("member %s (class: %s) not found", memberName, className)
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
		root = workspaceDir
	} else {
		root = resolvePath(root)
	}

	var results []GrepResult
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}
		ext := strings.ToLower(filepath.Ext(path))
		if d.IsDir() || (ext != ".go" && ext != ".cs" && ext != ".json" && ext != ".sql" && ext != ".s") {
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
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "[MCP-BRIDGE] Grep scanner error on %s: %v\n", path, err)
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

type ReplaceParams struct {
	Path        string `json:"path"`
	SearchText  string `json:"search_text"`
	ReplaceText string `json:"replace_text"`
	Glob        string `json:"glob"`
	UseRegex    bool   `json:"use_regex"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}

func handleReplace(args json.RawMessage) (string, error) {
	var p ReplaceParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid json arguments: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.SearchText == "" {
		return "", fmt.Errorf("search_text is required")
	}

	fullPath := resolvePath(p.Path)

	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("stat path error: %w", err)
	}

	var files []string
	if info.IsDir() {
		pattern := p.Glob
		err = filepath.WalkDir(fullPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if pattern != "" {
				match, err := filepath.Match(pattern, d.Name())
				if err == nil && match {
					files = append(files, path)
				}
			} else {
				ext := strings.ToLower(filepath.Ext(d.Name()))
				if ext == ".go" || ext == ".cs" {
					files = append(files, path)
				}
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk dir error: %w", err)
		}
	} else {
		files = append(files, fullPath)
	}

	searchTextNorm := strings.ReplaceAll(p.SearchText, "\r\n", "\n")
	replaceTextNorm := strings.ReplaceAll(p.ReplaceText, "\r\n", "\n")

	var re *regexp.Regexp
	if p.UseRegex {
		var err error
		re, err = regexp.Compile(searchTextNorm)
		if err != nil {
			return "", fmt.Errorf("invalid regex pattern: %w", err)
		}
	}

	replacedCount := 0
	filesModified := 0

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		strContent := string(content)
		// Normalize CRLF to LF
		strContent = strings.ReplaceAll(strContent, "\r\n", "\n")
		var newContent string
		var totalReplaced int

		// Line constraints
		lines := strings.Split(strContent, "\n")
		numLines := len(lines)

		startIdx := p.StartLine - 1
		if startIdx < 0 {
			startIdx = 0
		}
		endIdx := p.EndLine - 1
		if p.EndLine <= 0 || endIdx >= numLines {
			endIdx = numLines - 1
		}

		if startIdx > endIdx || startIdx >= numLines {
			continue
		}

		// Perform replacement only on segment
		segment := strings.Join(lines[startIdx:endIdx+1], "\n")
		var newSegment string
		var count int

		if p.UseRegex {
			matches := re.FindAllStringIndex(segment, -1)
			if len(matches) > 0 {
				count = len(matches)
				newSegment = re.ReplaceAllString(segment, replaceTextNorm)
			}
		} else {
			if strings.Contains(segment, searchTextNorm) {
				count = strings.Count(segment, searchTextNorm)
				newSegment = strings.ReplaceAll(segment, searchTextNorm, replaceTextNorm)
			}
		}

		if count > 0 {
			newSegmentLines := strings.Split(newSegment, "\n")
			resultLines := append([]string{}, lines[:startIdx]...)
			resultLines = append(resultLines, newSegmentLines...)
			resultLines = append(resultLines, lines[endIdx+1:]...)
			newContent = strings.Join(resultLines, "\n")
			totalReplaced = count
		}

		if totalReplaced > 0 {
			err = os.WriteFile(file, []byte(newContent), 0644)
			if err != nil {
				return "", fmt.Errorf("failed to write to file %s: %w", file, err)
			}
			replacedCount += totalReplaced
			filesModified++
		}
	}

	return fmt.Sprintf("Successfully replaced %d occurrences across %d files.", replacedCount, filesModified), nil
}

type ListFilesParams struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Glob      string `json:"glob"`
}

type FileEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Path  string `json:"path"`
	Size  int64  `json:"size,omitempty"`
}

func handleListFiles(args json.RawMessage) (string, error) {
	var p ListFilesParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid json arguments: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	fullPath := resolvePath(p.Path)

	var results []FileEntry
	if p.Recursive {
		err := filepath.WalkDir(fullPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == fullPath {
				return nil
			}
			rel, err := filepath.Rel(workspaceDir, path)
			if err != nil {
				rel, _ = filepath.Rel(serverDir, path)
			}
			rel = filepath.ToSlash(rel)

			if p.Glob != "" {
				match, err := filepath.Match(p.Glob, d.Name())
				if err != nil || !match {
					if !d.IsDir() {
						return nil
					}
				}
			}

			info, _ := d.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}

			results = append(results, FileEntry{
				Name:  d.Name(),
				IsDir: d.IsDir(),
				Path:  rel,
				Size:  size,
			})
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("walk dir error: %w", err)
		}
	} else {
		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return "", fmt.Errorf("read dir error: %w", err)
		}
		for _, d := range entries {
			if p.Glob != "" {
				match, err := filepath.Match(p.Glob, d.Name())
				if err != nil || !match {
					continue
				}
			}
			path := filepath.Join(fullPath, d.Name())
			rel, err := filepath.Rel(workspaceDir, path)
			if err != nil {
				rel, _ = filepath.Rel(serverDir, path)
			}
			rel = filepath.ToSlash(rel)

			info, _ := d.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}

			results = append(results, FileEntry{
				Name:  d.Name(),
				IsDir: d.IsDir(),
				Path:  rel,
				Size:  size,
			})
		}
	}

	if results == nil {
		results = []FileEntry{}
	}

	out, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
