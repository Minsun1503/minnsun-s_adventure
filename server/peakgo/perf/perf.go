// Package perf provides a zero-allocation performance monitoring toolkit
// for the Minnsun's Adventure game server.
//
// # Why this package exists
//
// Game servers need real-time visibility into tick durations, memory usage,
// and packet throughput to diagnose performance bottlenecks without
// affecting hot-path performance.
//
// # Peak Go Contract
//
// Zero heap allocations on the recording hot-path. Uses pre-allocated
// ring buffers and atomic counters. Sampling and reporting are done
// on a separate goroutine.
package perf

import (
	"runtime"
	"sync/atomic"
	"time"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	// RingBufferSize is the number of tick duration samples stored.
	RingBufferSize = 1024

	// NanosecondBuckets is the number of histogram buckets for tick durations.
	NanosecondBuckets = 16
)

// ─── Global Monitor Instances ─────────────────────────────────────────────────
//
// Shared singleton monitors wired into the game loop, network layer, and
// admin panel. Declared here for zero-import coupling — any package can
// reference perf.GlobalTickMonitor, perf.GlobalPacketMonitor, etc.

// GlobalTickMonitor records game loop tick durations.
var GlobalTickMonitor = &TickMonitor{}

// GlobalPacketMonitor tracks network packet throughput.
var GlobalPacketMonitor = &PacketMonitor{}

// GlobalMemMonitor samples memory usage every 10 calls (sample rate = 10).
var GlobalMemMonitor = NewMemMonitor(10)

// GlobalAlertMonitor watches thresholds and fires log warnings.
var GlobalAlertMonitor = NewAlertMonitor()

// ─── Tick Monitor ─────────────────────────────────────────────────────────────

// TickMonitor tracks game loop tick durations using a lock-free ring buffer.
// Embed this in the game loop for zero-alloc performance tracking.
type TickMonitor struct {
	buffer   [RingBufferSize]int64 // Nanosecond durations, indexed by position
	position uint64                // Atomic write index
	overflow uint64                // Total ticks beyond buffer capacity
	minDur   int64                 // Minimum observed tick duration
	maxDur   int64                 // Maximum observed tick duration
	totalDur int64                 // Sum of all recorded durations (for avg)
}

// RecordTick records a tick duration. Zero alloc, atomic write.
func (tm *TickMonitor) RecordTick(duration time.Duration) {
	ns := int64(duration)
	pos := atomic.AddUint64(&tm.position, 1) - 1
	tm.buffer[pos%RingBufferSize] = ns

	// Update min/max (not atomic, eventual consistency is fine for monitoring)
	if ns < tm.minDur || tm.minDur == 0 {
		tm.minDur = ns
	}
	if ns > tm.maxDur {
		tm.maxDur = ns
	}
	atomic.AddInt64(&tm.totalDur, ns)

	if pos >= RingBufferSize {
		atomic.AddUint64(&tm.overflow, 1)
	}
}

// Min returns the minimum recorded tick duration.
func (tm *TickMonitor) Min() time.Duration { return time.Duration(tm.minDur) }

// Max returns the maximum recorded tick duration.
func (tm *TickMonitor) Max() time.Duration { return time.Duration(tm.maxDur) }

// Avg returns the average tick duration.
func (tm *TickMonitor) Avg() time.Duration {
	pos := atomic.LoadUint64(&tm.position)
	total := atomic.LoadInt64(&tm.totalDur)
	if pos == 0 {
		return 0
	}
	return time.Duration(total / int64(pos))
}

// Count returns the total number of ticks recorded.
func (tm *TickMonitor) Count() uint64 { return atomic.LoadUint64(&tm.position) }

// Overflow returns the number of ticks beyond the ring buffer capacity.
func (tm *TickMonitor) Overflow() uint64 { return atomic.LoadUint64(&tm.overflow) }

// Reset clears all recorded data.
func (tm *TickMonitor) Reset() {
	tm.minDur = 0
	tm.maxDur = 0
	tm.totalDur = 0
	atomic.StoreUint64(&tm.position, 0)
	atomic.StoreUint64(&tm.overflow, 0)
}

// ─── Histogram ────────────────────────────────────────────────────────────────

// Histogram provides nanosecond-bucketed tick duration distribution.
type Histogram struct {
	buckets [NanosecondBuckets]int64 // Count per bucket
	limits  [NanosecondBuckets]int64 // Upper limit for each bucket (ns)
}

// NewHistogram creates a histogram with exponential bucket limits.
func NewHistogram() *Histogram {
	h := &Histogram{}
	// Exponential bucket limits: 10μs, 50μs, 100μs, 500μs, 1ms, 5ms, 10ms, 50ms, ...
	limits := []int64{
		10000, 50000, 100000, 500000, // 10μs, 50μs, 100μs, 500μs
		1000000, 5000000, 10000000, 50000000, // 1ms, 5ms, 10ms, 50ms
		100000000, 500000000, 1000000000, // 100ms, 500ms, 1s
		5000000000, 10000000000, 50000000000, // 5s, 10s, 50s
	}
	copy(h.limits[:], limits)
	return h
}

// Record records a duration into the histogram.
// Zero alloc, atomic increment.
func (h *Histogram) Record(duration time.Duration) {
	ns := int64(duration)
	for i := range h.buckets {
		if ns <= h.limits[i] {
			atomic.AddInt64(&h.buckets[i], 1)
			return
		}
	}
	// Above all buckets (last bucket is overflow)
	atomic.AddInt64(&h.buckets[NanosecondBuckets-1], 1)
}

// Snapshot returns a copy of the current histogram state.
func (h *Histogram) Snapshot() [NanosecondBuckets]int64 {
	var snap [NanosecondBuckets]int64
	for i := range h.buckets {
		snap[i] = atomic.LoadInt64(&h.buckets[i])
	}
	return snap
}

// Reset clears all histogram buckets.
func (h *Histogram) Reset() {
	for i := range h.buckets {
		atomic.StoreInt64(&h.buckets[i], 0)
	}
}

// ─── Memory Monitor ───────────────────────────────────────────────────────────

// MemSnapshot holds a filtered memory statistics snapshot.
// Use MemMonitor to sample this periodically.
type MemSnapshot struct {
	Alloc       uint64        // Current heap allocation
	TotalAlloc  uint64        // Cumulative heap allocation
	Sys         uint64        // Total memory obtained from OS
	NumGC       uint32        // Number of completed GC cycles
	PauseTotal  time.Duration // Total GC pause time
	LastPause   time.Duration // Most recent GC pause
	HeapObjects uint64        // Number of objects on heap
	Goroutines  int           // Number of goroutines
}

// MemMonitor provides periodic memory usage sampling.
// Uses runtime.ReadMemStats but filters to only relevant fields.
type MemMonitor struct {
	last       MemSnapshot
	sampleRate int // Sample every N calls
	counter    int
}

// NewMemMonitor creates a memory monitor with the given sample rate.
// sampleRate: sample every N calls to Sample(). 1 = every call, 10 = every 10th.
func NewMemMonitor(sampleRate int) *MemMonitor {
	if sampleRate < 1 {
		sampleRate = 1
	}
	return &MemMonitor{sampleRate: sampleRate}
}

// Sample takes a memory snapshot (if due based on sample rate).
// Returns nil if not due for sampling this call.
func (mm *MemMonitor) Sample() *MemSnapshot {
	mm.counter++
	if mm.counter%mm.sampleRate != 0 {
		return nil
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	mm.last = MemSnapshot{
		Alloc:       m.Alloc,
		TotalAlloc:  m.TotalAlloc,
		Sys:         m.Sys,
		NumGC:       m.NumGC,
		PauseTotal:  time.Duration(m.PauseTotalNs),
		LastPause:   time.Duration(m.PauseNs[(m.NumGC+255)%256]),
		HeapObjects: m.HeapObjects,
		Goroutines:  runtime.NumGoroutine(),
	}
	return &mm.last
}

// Last returns the most recent snapshot without triggering a new sample.
func (mm *MemMonitor) Last() MemSnapshot {
	return mm.last
}

// ─── Packet Monitor ───────────────────────────────────────────────────────────

// PacketMonitor tracks network packet throughput.
// Uses atomic counters for zero-alloc tracking.
type PacketMonitor struct {
	packetsIn  uint64 // Total packets received
	packetsOut uint64 // Total packets sent
	bytesIn    uint64 // Total bytes received
	bytesOut   uint64 // Total bytes sent
	peakIn     uint64 // Peak inbound packets per second
	peakOut    uint64 // Peak outbound packets per second
}

// RecordIn records an inbound packet.
func (pm *PacketMonitor) RecordIn(bytes int) {
	atomic.AddUint64(&pm.packetsIn, 1)
	atomic.AddUint64(&pm.bytesIn, uint64(bytes))
}

// RecordOut records an outbound packet.
func (pm *PacketMonitor) RecordOut(bytes int) {
	atomic.AddUint64(&pm.packetsOut, 1)
	atomic.AddUint64(&pm.bytesOut, uint64(bytes))
}

// Snapshot returns the current packet statistics.
func (pm *PacketMonitor) Snapshot() (packetsIn, packetsOut, bytesIn, bytesOut uint64) {
	return atomic.LoadUint64(&pm.packetsIn),
		atomic.LoadUint64(&pm.packetsOut),
		atomic.LoadUint64(&pm.bytesIn),
		atomic.LoadUint64(&pm.bytesOut)
}

// Reset resets all counters.
func (pm *PacketMonitor) Reset() {
	atomic.StoreUint64(&pm.packetsIn, 0)
	atomic.StoreUint64(&pm.packetsOut, 0)
	atomic.StoreUint64(&pm.bytesIn, 0)
	atomic.StoreUint64(&pm.bytesOut, 0)
	atomic.StoreUint64(&pm.peakIn, 0)
	atomic.StoreUint64(&pm.peakOut, 0)
}

// ─── Report ───────────────────────────────────────────────────────────────────

// Report aggregates all performance data into a human-readable format.
type Report struct {
	TickMin     time.Duration
	TickMax     time.Duration
	TickAvg     time.Duration
	TickCount   uint64
	PacketsIn   uint64
	PacketsOut  uint64
	BytesIn     uint64
	BytesOut    uint64
	Alloc       uint64
	HeapObjects uint64
	Goroutines  int
	NumGC       uint32
}

// Collect gathers all performance data into a single report.
// This function allocates (called from monitoring goroutine, not hot-path).
func Collect(tm *TickMonitor, pm *PacketMonitor, mm *MemMonitor) Report {
	report := Report{
		TickMin:    tm.Min(),
		TickMax:    tm.Max(),
		TickAvg:    tm.Avg(),
		TickCount:  tm.Count(),
		Goroutines: runtime.NumGoroutine(),
	}

	report.PacketsIn, report.PacketsOut, report.BytesIn, report.BytesOut = pm.Snapshot()

	if snap := mm.Last(); snap.Alloc > 0 {
		report.Alloc = snap.Alloc
		report.HeapObjects = snap.HeapObjects
		report.NumGC = snap.NumGC
	}

	return report
}
