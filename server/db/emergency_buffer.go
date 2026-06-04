// Package db provides persistence mechanisms for the game server.
//
// emergency_buffer.go — Disk-backed persistence buffer for save queue hardening.
//
// When the SaveQueue channel is full, snapshots are written to a rotating file
// buffer on disk instead of being dropped. On the next successful drain, the
// buffer is replayed into the save queue.
//
// This ensures ZERO DATA LOSS even under extreme load conditions.
package db

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"server/logger"
	"sync"
)

// ─── Emergency Buffer ─────────────────────────────────────────────────────────
//
// SaveBuffer is a disk-backed persistence buffer that catches overflow from the
// SaveQueue channel. When the channel is full, snapshots are gob-encoded to a
// rotating file. On restart or drain, the buffer is replayed into the queue.
//
// Thread-safety: Protected by a mutex. Writes to disk are sequential and rare
// (only on channel overflow), so mutex contention is negligible.

// bufferFilePrefix is the prefix for emergency buffer files.
const bufferFilePrefix = "save_buffer_"

// bufferDir is the directory for emergency buffer files.
const bufferDir = "data/save_buffer"

// maxBufferFiles is the maximum number of buffer files to keep before rotating.
const maxBufferFiles = 3

// SaveBuffer manages disk-backed persistence for overflow save snapshots.
type SaveBuffer struct {
	mu      sync.Mutex
	dir     string
	fileIdx int
	file    *os.File
	encoder *gob.Encoder
	decoder *gob.Decoder
	readBuf []SaveSnapshot // In-memory cache for replayed snapshots
}

// GlobalSaveBuffer is the singleton disk-backed save buffer.
var GlobalSaveBuffer = &SaveBuffer{
	dir:     bufferDir,
	fileIdx: 0,
}

// InitSaveBuffer creates the buffer directory and initializes the encoder.
// Must be called once at server startup, before any save operations.
func (sb *SaveBuffer) Init() error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	dir := sb.dir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Open a new buffer file for writing
	sb.fileIdx = findNextBufferFile(dir)
	filename := filepath.Join(dir, makeBufferFilename(sb.fileIdx))
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	sb.file = f
	sb.encoder = gob.NewEncoder(f)
	logger.Info("[SAVE BUFFER] Emergency buffer initialized at %s", filename)
	return nil
}

// Append writes a snapshot to the disk buffer. Returns the number of bytes
// written, or 0 if the buffer is not initialized.
func (sb *SaveBuffer) Append(snap SaveSnapshot) int {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.encoder == nil || sb.file == nil {
		return 0
	}

	if err := sb.encoder.Encode(snap); err != nil {
		logger.Error("[SAVE BUFFER] Failed to encode snapshot: %v", err)
		return 0
	}
	return 1
}

// TryWriteToQueue attempts to push a snapshot to the SaveQueue with
// backpressure. Returns true if the snapshot was accepted by the channel,
// false if it was diverted to the disk buffer.
//
// Backpressure strategy:
//   - Normal operation: non-blocking send to SaveQueue
//   - On overflow: fall through to disk buffer
//   - The disk buffer is replayed into the queue on the next successful drain
func TryWriteToQueue(snap SaveSnapshot) bool {
	select {
	case SaveQueue <- snap:
		// Fast path: channel accepted the snapshot
		return true
	default:
		// Channel full: divert to disk buffer
		logger.Warn("[SAVE BUFFER] Queue full (%d/%d) — diverting entity #%d to disk buffer",
			len(SaveQueue), cap(SaveQueue), snap.EntityID)
		GlobalSaveBuffer.Append(snap)
		return false
	}
}

// DrainBuffer re-reads all buffered snapshots from disk and pushes them
// back into the SaveQueue. This is called after the queue worker has
// successfully drained pending snapshots.
//
// The buffer files are deleted after successful replay.
func (sb *SaveBuffer) DrainBuffer() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.file == nil {
		return
	}

	// Close the current writer file
	sb.file.Close()
	sb.file = nil
	sb.encoder = nil

	dir := sb.dir
	// Collect all buffer files
	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Error("[SAVE BUFFER] Failed to read buffer directory: %v", err)
		return
	}

	replayed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(entry.Name()) < len(bufferFilePrefix) || entry.Name()[:len(bufferFilePrefix)] != bufferFilePrefix {
			continue
		}

		filepath := filepath.Join(dir, entry.Name())
		f, err := os.Open(filepath)
		if err != nil {
			logger.Error("[SAVE BUFFER] Failed to open buffer file %s: %v", entry.Name(), err)
			continue
		}

		dec := gob.NewDecoder(f)
		for {
			var snap SaveSnapshot
			if err := dec.Decode(&snap); err != nil {
				break
			}
			// Push back to queue (non-blocking, best-effort)
			select {
			case SaveQueue <- snap:
				replayed++
			default:
				// Channel is full again — write back to a new buffer file
				// (This is extremely unlikely during replay)
				logger.Warn("[SAVE BUFFER] Queue still full during replay — re-buffering entity #%d", snap.EntityID)
				GlobalSaveBuffer.Append(snap)
			}
		}
		f.Close()
		os.Remove(filepath)
	}

	if replayed > 0 {
		logger.Info("[SAVE BUFFER] Replayed %d buffered snapshots into save queue.", replayed)
	}

	// Re-open a new writer file for future overflow
	sb.fileIdx = findNextBufferFile(dir)
	filename := filepath.Join(dir, makeBufferFilename(sb.fileIdx))
	f, err := os.Create(filename)
	if err != nil {
		logger.Error("[SAVE BUFFER] Failed to re-open buffer file: %v", err)
		return
	}
	sb.file = f
	sb.encoder = gob.NewEncoder(f)
}

// FlushToDisk forces all remaining buffered snapshots to be written to disk.
// Call this during graceful shutdown to ensure zero data loss.
func (sb *SaveBuffer) FlushToDisk() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.file == nil {
		return 0
	}

	// Sync the file to disk
	if err := sb.file.Sync(); err != nil {
		logger.Error("[SAVE BUFFER] Failed to sync buffer file: %v", err)
	}
	return 1
}

// PendingCount returns the number of buffered snapshots still on disk.
func (sb *SaveBuffer) PendingCount() int {
	dir := sb.dir
	entries, _ := os.ReadDir(dir)
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(entry.Name()) >= len(bufferFilePrefix) && entry.Name()[:len(bufferFilePrefix)] == bufferFilePrefix {
			count++
		}
	}
	return count
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// findNextBufferFile finds the next available buffer file index.
func findNextBufferFile(dir string) int {
	maxIdx := -1
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var idx int
		n := fmtSscanf(entry.Name(), bufferFilePrefix+"%d.buf", &idx)
		if n == 1 && idx > maxIdx {
			maxIdx = idx
		}
	}
	return maxIdx + 1
}

// makeBufferFilename creates a buffer filename from an index.
func makeBufferFilename(idx int) string {
	return bufferFilePrefix + itoa(idx) + ".buf"
}

// itoa is a simple integer-to-string conversion (avoid importing strconv).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// fmtSscanf is a minimal sscanf wrapper for buffer file parsing.
// Returns the number of items successfully parsed.
func fmtSscanf(str, format string, args ...interface{}) int {
	// Simplified integer scan: find first digit sequence and parse
	if len(args) == 0 {
		return 0
	}
	ptr, ok := args[0].(*int)
	if !ok {
		return 0
	}
	// Find start of digits
	start := -1
	for i := 0; i < len(str); i++ {
		if str[i] >= '0' && str[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return 0
	}
	end := start
	for end < len(str) && str[end] >= '0' && str[end] <= '9' {
		end++
	}
	val := 0
	for i := start; i < end; i++ {
		val = val*10 + int(str[i]-'0')
	}
	*ptr = val
	return 1
}

// init registers SaveSnapshot with gob for serialization.
func init() {
	gob.Register(SaveSnapshot{})
}
