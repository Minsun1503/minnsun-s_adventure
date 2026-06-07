// Package mcp — Blackbox Trace/Tail/Patch Tools
//
// handlers_blackbox.go provides MCP tools for reading and filtering the
// structured JSONL trace files produced by Phase 1+2, plus a controlled
// file patching tool with automatic build-verification and rollback.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"server/peakgo/config"
)

func init() {
	// ─── blackbox_list_snapshots ──────────────────────────────────────────────
	// Returns all *.jsonl files in the config-defined log directory.
	// Each entry: {"name": "...", "size": ..., "mtime": "..."}

	Register("blackbox_list_snapshots", func(req Request) Response {
		logDir := config.C().LogDir
		entries, err := os.ReadDir(logDir)
		if err != nil {
			// Directory may not exist yet — return empty.
			return rpcResult(req.ID, []any{})
		}

		type fileInfo struct {
			Name  string `json:"name"`
			Size  int64  `json:"size"`
			MTime string `json:"mtime"`
		}

		var files []fileInfo
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, fileInfo{
				Name:  e.Name(),
				Size:  info.Size(),
				MTime: info.ModTime().UTC().Format(time.RFC3339),
			})
		}

		// Sort by name descending (newest first by date prefix).
		sort.Slice(files, func(i, j int) bool {
			return files[i].Name > files[j].Name
		})

		if files == nil {
			files = []fileInfo{}
		}
		return rpcResult(req.ID, files)
	})

	// ─── blackbox_read_snapshot ───────────────────────────────────────────────
	// Reads the last N lines from a JSONL file via circular buffer.
	// Params: {"file": "trace-2026-06-07.jsonl", "tail": 50}

	Register("blackbox_read_snapshot", func(req Request) Response {
		var p struct {
			File string `json:"file"`
			Tail int    `json:"tail"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.File == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "'file' parameter is required")
		}
		if p.Tail <= 0 {
			p.Tail = 10
		}
		if p.Tail > 10000 {
			p.Tail = 10000 // safety cap
		}

		fullPath, err := safeTracePath(p.File)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}

		f, err := os.Open(fullPath)
		if err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("cannot open file: %v", err))
		}
		defer f.Close()

		// Circular buffer: ring[len] holds the last N lines.
		type lineEntry struct {
			LineNum int    `json:"line_num"`
			Content string `json:"content"`
		}
		ring := make([]lineEntry, p.Tail)
		idx := 0
		total := 0

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB max line length
		for scanner.Scan() {
			ring[idx] = lineEntry{LineNum: total + 1, Content: scanner.Text()}
			idx = (idx + 1) % p.Tail
			total++
		}

		if err := scanner.Err(); err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("read error: %v", err))
		}

		// Extract ring in order.
		var result []lineEntry
		if total < p.Tail {
			result = ring[:total]
		} else {
			result = make([]lineEntry, p.Tail)
			for i := 0; i < p.Tail; i++ {
				result[i] = ring[(idx+i)%p.Tail]
			}
		}

		if result == nil {
			result = []lineEntry{}
		}

		return rpcResult(req.ID, map[string]any{
			"file":     p.File,
			"total":    total,
			"returned": len(result),
			"lines":    result,
		})
	})

	// ─── blackbox_filter_trace ────────────────────────────────────────────────
	// Scans the newest *.jsonl file, filters lines by trace_id.
	// Params: {"trace_id": "abc123", "limit": 100}

	Register("blackbox_filter_trace", func(req Request) Response {
		var p struct {
			TraceID string `json:"trace_id"`
			Limit   int    `json:"limit"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.TraceID == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "'trace_id' parameter is required")
		}
		if p.Limit <= 0 {
			p.Limit = 100
		}
		if p.Limit > 10000 {
			p.Limit = 10000
		}

		// Find newest jsonl file.
		newestFile, err := findNewestTraceFile()
		if err != nil {
			return rpcResult(req.ID, map[string]any{
				"trace_id": p.TraceID,
				"file":     "",
				"matches":  []any{},
				"total":    0,
			})
		}

		f, err := os.Open(newestFile)
		if err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("cannot open %s: %v", newestFile, err))
		}
		defer f.Close()

		var matches []map[string]any
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			if len(matches) >= p.Limit {
				break
			}
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Quick check: skip lines that can't possibly match (optimization).
			// trace_id appears as "trace_id":"<value>" — check substring first.
			if !strings.Contains(string(line), `"trace_id":"`+p.TraceID+`"`) {
				continue
			}

			// Full unmarshal for accurate match.
			var obj map[string]any
			if err := json.Unmarshal(line, &obj); err != nil {
				continue
			}
			if tid, ok := obj["trace_id"].(string); ok && tid == p.TraceID {
				matches = append(matches, obj)
			}
		}

		if err := scanner.Err(); err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("scan error: %v", err))
		}

		if matches == nil {
			matches = []map[string]any{}
		}

		return rpcResult(req.ID, map[string]any{
			"trace_id": p.TraceID,
			"file":     filepath.Base(newestFile),
			"matches":  matches,
			"total":    len(matches),
		})
	})

	// ─── blackbox_apply_patch ─────────────────────────────────────────────────
	// Applies a string replacement to a file, runs go build, and auto-rollbacks
	// if the build fails.
	// Params: {"file": "server/game/combat.go", "old": "...", "new": "..."}

	Register("blackbox_apply_patch", func(req Request) Response {
		var p struct {
			File string `json:"file"`
			Old  string `json:"old"`
			New  string `json:"new"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.File == "" || p.Old == "" {
			return rpcError(req.ID, ErrCodeInvalidParams, "'file', 'old', and 'new' parameters are required")
		}

		// Resolve and validate the file path.
		resolvedPath, err := safePatchPath(p.File)
		if err != nil {
			return rpcError(req.ID, ErrCodeInvalidParams, err.Error())
		}

		// Read original content.
		original, err := os.ReadFile(resolvedPath)
		if err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("cannot read file: %v", err))
		}

		// Perform replacement.
		newContent := strings.ReplaceAll(string(original), p.Old, p.New)
		if newContent == string(original) {
			return rpcError(req.ID, ErrCodeInvalidParams, "'old' string not found in file")
		}

		// Write patched content.
		if err := os.WriteFile(resolvedPath, []byte(newContent), 0644); err != nil {
			return rpcError(req.ID, ErrCodeInternal, fmt.Sprintf("cannot write file: %v", err))
		}

		// Run go build to verify.
		serverDir := findServerDir()
		buildCmd := exec.Command("go", "build", "./...")
		buildCmd.Dir = serverDir
		buildOut, buildErr := buildCmd.CombinedOutput()

		if buildErr != nil {
			// Build failed — rollback to original.
			_ = os.WriteFile(resolvedPath, original, 0644)
			errResp := map[string]any{
				"status":  "build failed, rolled back",
				"output":  string(buildOut),
				"rollack": true,
			}
			errJSON, _ := json.Marshal(errResp)
			return rpcError(req.ID, ErrCodeInternal, string(errJSON))
		}

		return rpcResult(req.ID, map[string]any{
			"status":   "patch applied and verified",
			"file":     p.File,
			"output":   string(buildOut),
			"rollback": false,
		})
	})

	// ─── blackbox_trigger_build ───────────────────────────────────────────────
	// Triggers a Unity WebGL build via CLI. Reads Unity path and project path
	// from config.json or falls back to env vars UNITY_PATH / UNITY_PROJECT_PATH.
	// Unity logs go to a file (not stdout) to avoid capturing KB of text in memory.
	// On success returns minimal response; on failure reads last 50 log lines.

	Register("blackbox_trigger_build", func(req Request) Response {
		cfg := config.C()

		// Resolve Unity executable path.
		unityExe := cfg.UnityExe
		if unityExe == "" {
			unityExe = os.Getenv("UNITY_PATH")
		}
		if unityExe == "" {
			return rpcError(req.ID, ErrCodeInvalidParams,
				"unity_exe not set in config.json and UNITY_PATH env var not found")
		}

		// Validate executable exists.
		if _, err := os.Stat(unityExe); err != nil {
			return rpcError(req.ID, ErrCodeInternal,
				fmt.Sprintf("Unity executable not found at %s: %v", unityExe, err))
		}

		// Resolve Unity project path.
		projectPath := cfg.UnityProjectPath
		if projectPath == "" {
			projectPath = os.Getenv("UNITY_PROJECT_PATH")
		}
		if projectPath == "" {
			return rpcError(req.ID, ErrCodeInvalidParams,
				"unity_project_path not set in config.json and UNITY_PROJECT_PATH env var not found")
		}

		// Validate project path exists.
		if _, err := os.Stat(projectPath); err != nil {
			return rpcError(req.ID, ErrCodeInternal,
				fmt.Sprintf("Unity project path not found at %s: %v", projectPath, err))
		}

		// Resolve output path.
		outputPath := cfg.UnityBuildOutput
		if outputPath == "" {
			outputPath = filepath.Join(projectPath, "Builds", "WebGL")
		}

		// Ensure output directory exists.
		if err := os.MkdirAll(outputPath, 0755); err != nil {
			return rpcError(req.ID, ErrCodeInternal,
				fmt.Sprintf("failed to create output directory %s: %v", outputPath, err))
		}

		// Ensure log directory exists.
		logFilePath := `C:\Minnsun-s_Adventure\server\logs\unity_build.log`
		if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
			return rpcError(req.ID, ErrCodeInternal,
				fmt.Sprintf("failed to create log directory: %v", err))
		}

		// Build the command with 10-minute timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, unityExe,
			"-batchmode",
			"-quit",
			"-projectPath", projectPath,
			"-executeMethod", "BuildScript.Build",
			"-buildOutput", outputPath,
			"-logFile", logFilePath,
		)
		cmd.Dir = projectPath
		// Do NOT capture stdout/stderr — Unity writes directly to -logFile.
		cmd.Stdout = nil
		cmd.Stderr = nil

		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return rpcError(req.ID, ErrCodeInternal,
					"Unity build timed out after 10 minutes")
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		if exitCode == 0 {
			return rpcResult(req.ID, map[string]any{
				"status":      "success",
				"exit_code":   0,
				"output_path": outputPath,
			})
		}

		// Build failed — read last 50 lines via circular buffer (no memory spike).
		buildLog := readLastLines(logFilePath, 50)
		return rpcResult(req.ID, map[string]any{
			"status":      "failed",
			"exit_code":   exitCode,
			"output_path": outputPath,
			"build_log":   buildLog,
		})
	})
}

// readLastLines reads the last n lines from a file using a circular buffer,
// matching the pattern used in blackbox_read_snapshot. Returns an empty slice
// on any error (caller handles gracefully).
func readLastLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return []string{}
	}
	defer f.Close()

	ring := make([]string, n)
	idx := 0
	total := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		ring[idx] = scanner.Text()
		idx = (idx + 1) % n
		total++
	}

	if total < n {
		return ring[:total]
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = ring[(idx+i)%n]
	}
	return result
}

// ─── Internal Helpers ────────────────────────────────────────────────────────

// safeTracePath validates that the given filename resolves within the log
// directory and returns the full absolute path. Prevents directory traversal.
func safeTracePath(filename string) (string, error) {
	logDir := config.C().LogDir
	absLogDir, err := filepath.Abs(logDir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve log directory: %w", err)
	}

	clean := filepath.Clean(filename)
	if strings.Contains(clean, "..") || strings.Contains(clean, "/") || strings.Contains(clean, "\\") {
		return "", fmt.Errorf("invalid filename: path components not allowed")
	}

	fullPath := filepath.Join(absLogDir, clean)
	return fullPath, nil
}

// safePatchPath resolves and validates a relative file path under the server
// directory (project root + "server/"). Prevents directory traversal escape.
func safePatchPath(relativePath string) (string, error) {
	// For safety, only allow files under the server/ subtree.
	// The server is located at C:\Minnsun-s_Adventure\server.
	// We derive the absolute server root by walking up from the working dir.
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot get working directory: %w", err)
	}

	// Candidate: wd/server/relativePath (wd is project root).
	candidate := filepath.Join(wd, relativePath)
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path: %w", err)
	}

	serverRoot := filepath.Join(wd, "server")
	absServerRoot, err := filepath.Abs(serverRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve server root: %w", err)
	}

	// Ensure the resolved path is within the server/ directory.
	if !strings.HasPrefix(absCandidate, absServerRoot+string(filepath.Separator)) &&
		absCandidate != absServerRoot {
		return "", fmt.Errorf("path escapes server directory: %s", relativePath)
	}

	// Verify file exists.
	if _, err := os.Stat(absCandidate); err != nil {
		return "", fmt.Errorf("file not found: %s (%v)", relativePath, err)
	}

	return absCandidate, nil
}

// findNewestTraceFile finds the newest *.jsonl file in the log directory
// by modification time. Returns the full path.
func findNewestTraceFile() (string, error) {
	logDir := config.C().LogDir
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return "", err
	}

	var newest string
	var newestMod time.Time

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newest = filepath.Join(logDir, e.Name())
		}
	}

	if newest == "" {
		return "", fmt.Errorf("no jsonl files found in %s", logDir)
	}

	return newest, nil
}

// findServerDir returns the absolute path to the server/ directory
// (where go.mod lives).
func findServerDir() string {
	wd, _ := os.Getwd()
	serverDir := filepath.Join(wd, "server")
	// Verify go.mod exists.
	if _, err := os.Stat(filepath.Join(serverDir, "go.mod")); err == nil {
		return serverDir
	}
	// Fallback: maybe wd is already server/.
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
		return wd
	}
	return serverDir
}
