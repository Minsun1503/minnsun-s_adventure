// Package logger provides a centralized, asynchronous, level-aware logging engine
// for the Minnsun's Adventure game server.
//
// Architecture:
//   - All callers push log entries into a non-blocking buffered channel (4096 slots).
//   - A single background worker goroutine drains the channel and writes to:
//     1. Console (stdout) with ANSI color codes.
//     2. A rotating log file (daily + size-based rotation at log_max_mb).
//   - When the channel is full, DEBUG entries are silently dropped to avoid blocking
//     the game loop. WARN and ERROR entries block until space is available.
//
// Usage:
//
//	logger.Init()           // Call once at server startup (reads data/config.json)
//	logger.Info("msg %v", x)
//	logger.Warn("msg %v", x)
//	logger.Error("msg %v", x)
//	logger.Debug("msg %v", x)  // No-op when debug=false in config
//	logger.Flush()             // Call on shutdown to drain all pending entries
package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Log Levels ──────────────────────────────────────────────────────────────

type level int

const (
	levelDebug level = iota
	levelInfo
	levelWarn
	levelError
)

var levelLabels = [...]string{"DEBUG", "INFO ", "WARN ", "ERROR"}

// ANSI color codes for console output.
var levelColors = [...]string{
	"\033[36m", // DEBUG  → Cyan
	"\033[32m", // INFO   → Green
	"\033[33m", // WARN   → Yellow
	"\033[31m", // ERROR  → Red
}

const ansiReset = "\033[0m"

// ─── Config ──────────────────────────────────────────────────────────────────

type serverConfig struct {
	Debug    bool   `json:"debug"`
	LogDir   string `json:"log_dir"`
	LogMaxMB int    `json:"log_max_mb"`
}

// ─── Log Entry ───────────────────────────────────────────────────────────────

type logEntry struct {
	lv     level
	ts     time.Time
	format string
	args   []any
}

// ─── Logger State ────────────────────────────────────────────────────────────

var (
	debugMode    atomic.Bool   // true = print DEBUG entries
	logChannel   chan logEntry // async buffer
	shutdownOnce sync.Once
	done         = make(chan struct{}) // signals worker has flushed and exited

	// file rotation state (guarded by fileMu)
	fileMu      sync.Mutex
	currentFile *os.File
	currentDay  string // "2006-01-02"
	currentSize int64  // bytes written to currentFile
	logDir      string
	logMaxBytes int64
)

const channelCapacity = 4096

// ─── Public API ──────────────────────────────────────────────────────────────

// Init reads data/config.json, creates the log directory, and starts the
// background writer goroutine. Must be called once at server startup.
func Init() {
	cfg := loadConfig("data/config.json")

	debugMode.Store(cfg.Debug)
	logDir = cfg.LogDir
	logMaxBytes = int64(cfg.LogMaxMB) * 1024 * 1024
	if logMaxBytes <= 0 {
		logMaxBytes = 10 * 1024 * 1024 // default 10 MB
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[LOGGER] Cannot create log dir %q: %v\n", logDir, err)
	}

	logChannel = make(chan logEntry, channelCapacity)
	go runWorker()

	modeStr := "INFO+"
	if cfg.Debug {
		modeStr = "DEBUG+"
	}
	Info("[LOGGER] Initialized. Mode: %s | LogDir: %s | MaxFileMB: %d",
		modeStr, logDir, cfg.LogMaxMB)
}

// Debug logs a message at DEBUG level. No-op when debug mode is disabled.
func Debug(format string, args ...any) {
	if !debugMode.Load() {
		return
	}
	push(levelDebug, format, args...)
}

// Info logs a message at INFO level.
func Info(format string, args ...any) {
	push(levelInfo, format, args...)
}

// Warn logs a message at WARN level.
func Warn(format string, args ...any) {
	push(levelWarn, format, args...)
}

// Error logs a message at ERROR level.
func Error(format string, args ...any) {
	push(levelError, format, args...)
}

// Flush waits for all buffered log entries to be written, then shuts down
// the background worker. Call this once during graceful server shutdown.
func Flush() {
	shutdownOnce.Do(func() {
		close(logChannel)
		<-done
	})
}

// SetDebugMode allows runtime toggling of debug mode (e.g. for admin commands).
func SetDebugMode(enabled bool) {
	debugMode.Store(enabled)
}

// IsDebug returns whether debug mode is currently active.
func IsDebug() bool {
	return debugMode.Load()
}

// ─── Internal ────────────────────────────────────────────────────────────────

func push(lv level, format string, args ...any) {
	if logChannel == nil {
		return
	}

	entry := logEntry{
		lv:     lv,
		ts:     time.Now(),
		format: format,
		args:   args,
	}

	// WARN and ERROR always make it through (block if needed).
	// DEBUG and INFO are dropped if the channel is full to protect the game loop.
	if lv >= levelWarn {
		logChannel <- entry
	} else {
		select {
		case logChannel <- entry:
		default:
			// Channel full: silently drop lower-priority entries.
		}
	}
}

// runWorker is the single goroutine that consumes log entries.
func runWorker() {
	defer close(done)
	for entry := range logChannel {
		writeEntry(entry)
	}
	// Drain is complete — close the current log file cleanly.
	fileMu.Lock()
	if currentFile != nil {
		currentFile.Close()
	}
	fileMu.Unlock()
}

// writeEntry formats and writes one log entry to console + file.
func writeEntry(e logEntry) {
	var message string
	if len(e.args) == 0 {
		message = e.format
	} else {
		message = fmt.Sprintf(e.format, e.args...)
	}

	ts := e.ts.Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, levelLabels[e.lv], message)
	colorLine := fmt.Sprintf("%s[%s] [%s]%s %s\n",
		levelColors[e.lv], ts, levelLabels[e.lv], ansiReset, message)

	// 1. Console (color)
	fmt.Fprint(os.Stdout, colorLine)

	// 2. File (plain text, rotated)
	fileMu.Lock()
	f := getOrOpenFile(e.ts)
	if f != nil {
		n, _ := fmt.Fprint(f, line)
		currentSize += int64(n)
	}
	fileMu.Unlock()
}

// getOrOpenFile returns the current log file, opening or rotating as needed.
// Must be called with fileMu held.
func getOrOpenFile(t time.Time) *os.File {
	day := t.Format("2006-01-02")

	// Rotate if day changed or file exceeds size limit.
	needRotate := currentFile == nil ||
		day != currentDay ||
		currentSize >= logMaxBytes

	if !needRotate {
		return currentFile
	}

	// Close the old file.
	if currentFile != nil {
		currentFile.Close()
		currentFile = nil
	}

	// Find a non-colliding file name.
	baseName := filepath.Join(logDir, fmt.Sprintf("server_%s.log", day))
	name := baseName
	for i := 1; ; i++ {
		if _, err := os.Stat(name); os.IsNotExist(err) {
			break
		}
		// File exists — check its size.
		info, err := os.Stat(name)
		if err == nil && info.Size() < logMaxBytes {
			break // Reuse this file (e.g. resumed same day, still small).
		}
		// File too large — try next suffix.
		name = filepath.Join(logDir,
			fmt.Sprintf("server_%s_%d.log", day, i))
	}

	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[LOGGER] Cannot open log file %q: %v\n", name, err)
		return nil
	}

	info, _ := f.Stat()
	if info != nil {
		currentSize = info.Size()
	} else {
		currentSize = 0
	}
	currentFile = f
	currentDay = day
	return f
}

// loadConfig reads and parses data/config.json. Falls back to safe defaults on error.
func loadConfig(path string) serverConfig {
	cfg := serverConfig{
		Debug:    false,
		LogDir:   "logs",
		LogMaxMB: 10,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[LOGGER] config.json not found at %q, using defaults.\n", path)
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[LOGGER] config.json parse error: %v, using defaults.\n", err)
	}
	return cfg
}
