// Package perf provides a zero-allocation performance monitoring toolkit.
//
// monitor.go — Threshold alert hooks that trigger loggate.Warnf when
// performance thresholds are breached.
package perf

import (
	"server/peakgo/loggate"
	"sync/atomic"
)

// ─── Default Thresholds ──────────────────────────────────────────────────────

const (
	// DefaultTickWarnThreshold is the tick duration warning threshold (50ms).
	DefaultTickWarnThreshold = 50_000_000 // 50ms in nanoseconds

	// DefaultHeapWarnThreshold is the heap allocation warning threshold (2GB).
	DefaultHeapWarnThreshold = 2_000_000_000 // 2GB in bytes

	// DefaultSaveQueueWarnRatio is the save queue capacity warning threshold (80%).
	DefaultSaveQueueWarnRatio = 0.80
)

// ─── AlertMonitor ─────────────────────────────────────────────────────────────
//
// AlertMonitor watches performance metrics and fires loggate.Warnf when
// configured thresholds are breached. Uses atomic fields for lock-free reads
// on the alert goroutine — the hot-path recording methods are zero-alloc and
// only touch atomics.
//
// Design decision: Each threshold has a "breached" atomic flag to prevent
// log spam. Once breached, the monitor won't fire another alert until the
// value drops below the threshold again.
type AlertMonitor struct {
	// Tick threshold
	tickThreshold int64 // nanoseconds
	tickBreached  int32 // atomic bool: 1 = currently breached, 0 = ok

	// Heap threshold
	heapThreshold int64 // bytes
	heapBreached  int32 // atomic bool

	// Save queue capacity (checked externally, set via CheckSaveQueue)
	saveQueueCapacity int
	saveQueueBreached int32 // atomic bool
}

// NewAlertMonitor creates an AlertMonitor with default thresholds.
func NewAlertMonitor() *AlertMonitor {
	return &AlertMonitor{
		tickThreshold:     DefaultTickWarnThreshold,
		heapThreshold:     DefaultHeapWarnThreshold,
		saveQueueCapacity: 1000, // matches db.SaveQueue buffer size
	}
}

// SetTickThreshold overrides the default tick duration warning threshold.
func (am *AlertMonitor) SetTickThreshold(ns int64) {
	atomic.StoreInt64(&am.tickThreshold, ns)
	atomic.StoreInt32(&am.tickBreached, 0) // reset breach state
}

// SetHeapThreshold overrides the default heap warning threshold.
func (am *AlertMonitor) SetHeapThreshold(bytes int64) {
	atomic.StoreInt64(&am.heapThreshold, bytes)
	atomic.StoreInt32(&am.heapBreached, 0) // reset breach state
}

// SetSaveQueueCapacity sets the total save queue capacity for ratio checks.
func (am *AlertMonitor) SetSaveQueueCapacity(capacity int) {
	am.saveQueueCapacity = capacity
	atomic.StoreInt32(&am.saveQueueBreached, 0) // reset breach state
}

// CheckTickDuration checks a single tick duration against the threshold.
// Zero-alloc, uses atomic compare-and-swap to prevent log spam.
// Intended to be called from the game loop goroutine after each tick.
func (am *AlertMonitor) CheckTickDuration(ns int64) {
	threshold := atomic.LoadInt64(&am.tickThreshold)
	if threshold == 0 {
		return
	}

	if ns > threshold {
		if atomic.CompareAndSwapInt32(&am.tickBreached, 0, 1) {
			loggate.Warnf("[PERF ALERT] Tick duration %dms exceeds threshold %dms",
				ns/1_000_000, threshold/1_000_000)
		}
	} else {
		// Reset breach flag when back under threshold
		if atomic.LoadInt32(&am.tickBreached) == 1 {
			atomic.StoreInt32(&am.tickBreached, 0)
			loggate.Infof("[PERF ALERT] Tick duration recovered to %dms (below %dms threshold)",
				ns/1_000_000, threshold/1_000_000)
		}
	}
}

// CheckHeapSize checks heap allocation against the threshold.
// Intended to be called periodically from a monitoring goroutine.
func (am *AlertMonitor) CheckHeapSize(bytes uint64) {
	threshold := atomic.LoadInt64(&am.heapThreshold)
	if threshold == 0 {
		return
	}

	if bytes > uint64(threshold) {
		if atomic.CompareAndSwapInt32(&am.heapBreached, 0, 1) {
			loggate.Warnf("[PERF ALERT] Heap allocation %d MB exceeds threshold %d MB",
				bytes/1_000_000, threshold/1_000_000)
		}
	} else {
		if atomic.LoadInt32(&am.heapBreached) == 1 {
			atomic.StoreInt32(&am.heapBreached, 0)
			loggate.Infof("[PERF ALERT] Heap allocation recovered to %d MB (below %d MB threshold)",
				bytes/1_000_000, threshold/1_000_000)
		}
	}
}

// CheckSaveQueue checks the save queue fill ratio against the threshold.
// queueLen: current number of items in the save channel.
func (am *AlertMonitor) CheckSaveQueue(queueLen int) {
	capacity := am.saveQueueCapacity
	if capacity == 0 {
		return
	}

	ratio := float64(queueLen) / float64(capacity)
	if ratio >= DefaultSaveQueueWarnRatio {
		if atomic.CompareAndSwapInt32(&am.saveQueueBreached, 0, 1) {
			loggate.Warnf("[PERF ALERT] Save queue at %d%% capacity (%d/%d) — data loss risk!",
				int(ratio*100), queueLen, capacity)
		}
	} else {
		if atomic.LoadInt32(&am.saveQueueBreached) == 1 {
			atomic.StoreInt32(&am.saveQueueBreached, 0)
			loggate.Infof("[PERF ALERT] Save queue recovered to %d%% capacity (%d/%d)",
				int(ratio*100), queueLen, capacity)
		}
	}
}

// ResetBreachState forcibly clears all breach flags.
func (am *AlertMonitor) ResetBreachState() {
	atomic.StoreInt32(&am.tickBreached, 0)
	atomic.StoreInt32(&am.heapBreached, 0)
	atomic.StoreInt32(&am.saveQueueBreached, 0)
}
