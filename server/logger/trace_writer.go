// Package logger — Structured JSON Trace Writer
//
// TraceWriter provides an asynchronous JSONL trace logging system that runs
// in parallel with the existing text-based logger. Each trace entry is written
// as a single JSON object per line to a daily-rotated file in the configured
// log directory.
//
// Trace files are named trace-YYYY-MM-DD.jsonl and are written to the same
// log directory configured for the main logger. This keeps all operational
// logs co-located for easy administration.
//
// Usage:
//
//	logger.InitTraceWriter()           // Call once after logger.Init()
//	logger.PushTraceLog(TraceLog{...}) // Non-blocking push
//	logger.FlushTraceWriter()          // Call on shutdown
package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"server/peakgo/config"
)

// ─── Trace Log Entry ──────────────────────────────────────────────────────────

// TraceLog represents a single structured trace event to be written as JSONL.
type TraceLog struct {
	Time     string         `json:"time"`
	TraceID  string         `json:"trace_id,omitempty"`
	Opcode   string         `json:"opcode,omitempty"`
	EntityID uint64         `json:"entity_id,omitempty"`
	Msg      string         `json:"msg"`
	Fields   map[string]any `json:"fields,omitempty"`
}

// ─── Trace Writer State ───────────────────────────────────────────────────────

var (
	traceChannel   chan TraceLog
	traceOnce      sync.Once
	traceCloseOnce sync.Once
	traceDone      = make(chan struct{})
	traceClosed    bool

	// trace file state (guarded by traceFileMu)
	traceFileMu  sync.Mutex
	traceFile    *os.File
	traceFileDay string // "2006-01-02"
	traceLogDir  string

	// delta file state (guarded by traceFileMu — same mutex)
	deltaFile    *os.File
	deltaFileDay string
)

const traceChannelCapacity = 4096

// ─── Public API ───────────────────────────────────────────────────────────────

// InitTraceWriter starts the background trace writer goroutine.
// Must be called after logger.Init() so that logDir is already configured.
func InitTraceWriter() {
	cfg := config.C()
	traceLogDir = cfg.LogDir

	// Ensure the log directory exists.
	if err := os.MkdirAll(traceLogDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "[TRACE] Cannot create log dir %q: %v\n", traceLogDir, err)
	}

	traceChannel = make(chan TraceLog, traceChannelCapacity)
	go runTraceWorker()
}

// PushTraceLog enqueues a trace log entry for async writing.
// This is non-blocking — if the channel is full, the entry is silently dropped
// to avoid impacting the game loop.
func PushTraceLog(entry TraceLog) {
	if traceChannel == nil || traceClosed {
		return
	}
	select {
	case traceChannel <- entry:
	default:
		// Channel full: silently drop.
	}
}

// FlushTraceWriter waits for all buffered trace entries to be written, then
// shuts down the background worker. Call this during graceful server shutdown.
func FlushTraceWriter() {
	traceCloseOnce.Do(func() {
		traceClosed = true
		// Send sentinel: empty TraceLog signals shutdown.
		if traceChannel != nil {
			traceChannel <- TraceLog{}
		}
		<-traceDone
	})
}

// ─── Internal ─────────────────────────────────────────────────────────────────

// runTraceWorker is the single goroutine that consumes trace log entries
// and writes them as JSONL to the daily-rotated file.
func runTraceWorker() {
	defer close(traceDone)
	for entry := range traceChannel {
		// Sentinel: empty entry signals shutdown.
		if entry.Time == "" && entry.Msg == "" && entry.TraceID == "" {
			break
		}
		writeTraceEntry(entry)
	}

	// Drain remaining entries so we don't block callers.
	for {
		select {
		case entry := <-traceChannel:
			if entry.Time != "" || entry.Msg != "" || entry.TraceID != "" {
				writeTraceEntry(entry)
			}
		default:
			traceFileMu.Lock()
			if traceFile != nil {
				traceFile.Close()
				traceFile = nil
			}
			if deltaFile != nil {
				deltaFile.Close()
				deltaFile = nil
			}
			traceFileMu.Unlock()
			return
		}
	}
}

// writeTraceEntry marshals one TraceLog to JSON and appends it to the
// daily-rotated trace file. Additionally, if the entry's source=="client",
// it computes a delta/anomaly line and appends it to a separate delta-*.txt file.
func writeTraceEntry(entry TraceLog) {
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}

	traceFileMu.Lock()
	f := getOrOpenTraceFile()
	if f != nil {
		_, _ = fmt.Fprintln(f, string(line))
	}

	// ── Delta / Anomaly for client entries ────────────────────────────────
	if source, ok := entry.Fields["source"].(string); ok && source == "client" {
		trigger, _ := entry.Fields["trigger"].(string)
		deltaLine := GetDeltaEncoder("client").Encode(entry.Fields, trigger)
		if deltaLine != "" {
			df := getOrOpenDeltaFile()
			if df != nil {
				_, _ = fmt.Fprintln(df, deltaLine)
			}
		}
	}

	traceFileMu.Unlock()
}

// getOrOpenTraceFile returns the current trace file, rotating by day as needed.
// Must be called with traceFileMu held.
func getOrOpenTraceFile() *os.File {
	now := time.Now()
	day := now.Format("2006-01-02")

	if traceFile != nil && day == traceFileDay {
		return traceFile
	}

	// Close the old file.
	if traceFile != nil {
		traceFile.Close()
		traceFile = nil
	}

	name := filepath.Join(traceLogDir, fmt.Sprintf("trace-%s.jsonl", day))

	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[TRACE] Cannot open trace file %q: %v\n", name, err)
		return nil
	}

	traceFile = f
	traceFileDay = day
	return f
}

// getOrOpenDeltaFile returns the current delta file, rotating by day as needed.
// Must be called with traceFileMu held.
func getOrOpenDeltaFile() *os.File {
	now := time.Now()
	day := now.Format("2006-01-02")

	if deltaFile != nil && day == deltaFileDay {
		return deltaFile
	}

	// Close the old file.
	if deltaFile != nil {
		deltaFile.Close()
		deltaFile = nil
	}

	name := filepath.Join(traceLogDir, fmt.Sprintf("delta-%s.txt", day))

	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[DELTA] Cannot open delta file %q: %v\n", name, err)
		return nil
	}

	deltaFile = f
	deltaFileDay = day
	return f
}
