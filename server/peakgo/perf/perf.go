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
	"sync"
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

// P99 returns the 99th percentile tick duration by sorting the ring buffer.
// This is NOT a hot-path function — call from monitoring goroutine only.
// Allocates a slice for sorting.
func (tm *TickMonitor) P99() time.Duration {
	return tm.percentile(99)
}

// P999 returns the 99.9th percentile tick duration by sorting the ring buffer.
// This is NOT a hot-path function — call from monitoring goroutine only.
// Allocates a slice for sorting.
func (tm *TickMonitor) P999() time.Duration {
	return tm.percentile(999)
}

var sortBufPool = sync.Pool{
	New: func() any {
		s := make([]int64, 0, RingBufferSize)
		return &s
	},
}

// percentile returns the p-th percentile tick duration from the ring buffer.
// p=99 → P99, p=999 → P999.
func (tm *TickMonitor) percentile(p int) time.Duration {
	pos := atomic.LoadUint64(&tm.position)
	n := pos
	if n > RingBufferSize {
		n = RingBufferSize
	}
	if n == 0 {
		return 0
	}
	start := uint64(0)
	if pos > RingBufferSize {
		start = pos % RingBufferSize
	}
	
	pBuf := sortBufPool.Get().(*[]int64)
	vals := (*pBuf)[:0]
	
	for i := uint64(0); i < n; i++ {
		if v := tm.buffer[(start+i)%RingBufferSize]; v > 0 {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		sortBufPool.Put(pBuf)
		return 0
	}
	for i := 1; i < len(vals); i++ {
		for j := i; j > 0 && vals[j] < vals[j-1]; j-- {
			vals[j], vals[j-1] = vals[j-1], vals[j]
		}
	}
	idx := len(vals) * p / 1000
	if idx >= len(vals) {
		idx = len(vals) - 1
	}
	res := time.Duration(vals[idx])
	sortBufPool.Put(pBuf)
	return res
}

// MaxInRing scans the ring buffer for the maximum value within the current
// window. Unlike Max() which records the all-time maximum, MaxInRing() returns
// the worst tick in approximately the last 51.2 seconds (1024 ticks at 50ms
// per tick). This is NOT a hot-path function — call from monitoring goroutine
// only. Allocates a slice for iteration safety.
func (tm *TickMonitor) MaxInRing() time.Duration {
	pos := atomic.LoadUint64(&tm.position)
	n := pos
	if n > RingBufferSize {
		n = RingBufferSize
	}
	if n == 0 {
		return 0
	}
	start := uint64(0)
	if pos > RingBufferSize {
		start = pos % RingBufferSize
	}
	var maxVal int64
	for i := uint64(0); i < n; i++ {
		if v := tm.buffer[(start+i)%RingBufferSize]; v > maxVal {
			maxVal = v
		}
	}
	return time.Duration(maxVal)
}

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
	HeapSys     uint64        // Memory obtained from OS for heap
	StackSys    uint64        // Memory obtained from OS for stack (goroutine stacks)
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
		HeapSys:     m.HeapSys,
		StackSys:    m.StackSys,
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
	aoiQueries uint64 // Total AOI queries executed
	broadcasts uint64 // Total Broadcast packets sent

	// AOI metrics for benchmarking
	aoiEnters          uint64 // Total AOI Enter events
	aoiLeaves          uint64 // Total AOI Leave events
	visibleEntitiesSum uint64 // Sum of visible entity counts (for avg)
	visibleEntitiesCnt uint64 // Count of visible entity samples
	visibleEntitiesMax uint64 // Max visible entities in one viewport
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

// RecordAoiQuery records a single AOI spatial query execution.
func (pm *PacketMonitor) RecordAoiQuery() {
	atomic.AddUint64(&pm.aoiQueries, 1)
}

// RecordBroadcast records a broadcast packet sent.
func (pm *PacketMonitor) RecordBroadcast() {
	atomic.AddUint64(&pm.broadcasts, 1)
}

// RecordAoiEnter records an AOI Enter event.
func (pm *PacketMonitor) RecordAoiEnter() {
	atomic.AddUint64(&pm.aoiEnters, 1)
}

// RecordAoiLeave records an AOI Leave event.
func (pm *PacketMonitor) RecordAoiLeave() {
	atomic.AddUint64(&pm.aoiLeaves, 1)
}

// RecordAoiVisible records the number of entities visible in a viewport.
func (pm *PacketMonitor) RecordAoiVisible(count int) {
	atomic.AddUint64(&pm.visibleEntitiesSum, uint64(count))
	atomic.AddUint64(&pm.visibleEntitiesCnt, 1)
	// Update max using CAS loop for atomic max
	for {
		cur := atomic.LoadUint64(&pm.visibleEntitiesMax)
		if uint64(count) <= cur {
			break
		}
		if atomic.CompareAndSwapUint64(&pm.visibleEntitiesMax, cur, uint64(count)) {
			break
		}
	}
}

// AoiEnters returns the total AOI Enter events.
func (pm *PacketMonitor) AoiEnters() uint64 { return atomic.LoadUint64(&pm.aoiEnters) }

// AoiLeaves returns the total AOI Leave events.
func (pm *PacketMonitor) AoiLeaves() uint64 { return atomic.LoadUint64(&pm.aoiLeaves) }

// VisibleEntitiesAvg returns the average visible entities per viewport.
func (pm *PacketMonitor) VisibleEntitiesAvg() float64 {
	cnt := atomic.LoadUint64(&pm.visibleEntitiesCnt)
	if cnt == 0 {
		return 0
	}
	sum := atomic.LoadUint64(&pm.visibleEntitiesSum)
	return float64(sum) / float64(cnt)
}

// VisibleEntitiesMax returns the maximum visible entities in one viewport.
func (pm *PacketMonitor) VisibleEntitiesMax() uint64 {
	return atomic.LoadUint64(&pm.visibleEntitiesMax)
}

// Snapshot returns the current packet statistics.
func (pm *PacketMonitor) Snapshot() (packetsIn, packetsOut, bytesIn, bytesOut, aoi, bcast uint64) {
	return atomic.LoadUint64(&pm.packetsIn),
		atomic.LoadUint64(&pm.packetsOut),
		atomic.LoadUint64(&pm.bytesIn),
		atomic.LoadUint64(&pm.bytesOut),
		atomic.LoadUint64(&pm.aoiQueries),
		atomic.LoadUint64(&pm.broadcasts)
}

// SnapshotAOI returns AOI-specific metrics.
func (pm *PacketMonitor) SnapshotAOI() (enters, leaves, visibleSum, visibleCnt, visibleMax uint64) {
	return atomic.LoadUint64(&pm.aoiEnters),
		atomic.LoadUint64(&pm.aoiLeaves),
		atomic.LoadUint64(&pm.visibleEntitiesSum),
		atomic.LoadUint64(&pm.visibleEntitiesCnt),
		atomic.LoadUint64(&pm.visibleEntitiesMax)
}

// Reset resets all counters.
func (pm *PacketMonitor) Reset() {
	atomic.StoreUint64(&pm.packetsIn, 0)
	atomic.StoreUint64(&pm.packetsOut, 0)
	atomic.StoreUint64(&pm.bytesIn, 0)
	atomic.StoreUint64(&pm.bytesOut, 0)
	atomic.StoreUint64(&pm.peakIn, 0)
	atomic.StoreUint64(&pm.peakOut, 0)
	atomic.StoreUint64(&pm.aoiQueries, 0)
	atomic.StoreUint64(&pm.broadcasts, 0)
	atomic.StoreUint64(&pm.aoiEnters, 0)
	atomic.StoreUint64(&pm.aoiLeaves, 0)
	atomic.StoreUint64(&pm.visibleEntitiesSum, 0)
	atomic.StoreUint64(&pm.visibleEntitiesCnt, 0)
	atomic.StoreUint64(&pm.visibleEntitiesMax, 0)
}

// ─── Report ───────────────────────────────────────────────────────────────────

// Report aggregates all performance data into a human-readable format.
type Report struct {
	TickMin     time.Duration
	TickMax     time.Duration
	TickAvg     time.Duration
	TickP99     time.Duration
	TickP999    time.Duration
	TickWorst1m time.Duration
	TickCount   uint64
	PacketsIn   uint64
	PacketsOut  uint64
	BytesIn     uint64
	BytesOut    uint64
	AoiQueries  uint64
	Broadcasts  uint64
	Alloc       uint64
	HeapObjects uint64
	Sys         uint64
	HeapSys     uint64
	StackSys    uint64
	Goroutines  int
	NumGC       uint32
}

// Collect gathers all performance data into a single report.
// This function allocates (called from monitoring goroutine, not hot-path).
func Collect(tm *TickMonitor, pm *PacketMonitor, mm *MemMonitor) Report {
	report := Report{
		TickMin:     tm.Min(),
		TickMax:     tm.Max(),
		TickAvg:     tm.Avg(),
		TickP99:     tm.P99(),
		TickP999:    tm.P999(),
		TickWorst1m: tm.MaxInRing(),
		TickCount:   tm.Count(),
		Goroutines:  runtime.NumGoroutine(),
	}

	report.PacketsIn, report.PacketsOut, report.BytesIn, report.BytesOut, report.AoiQueries, report.Broadcasts = pm.Snapshot()

	if snap := mm.Last(); snap.Alloc > 0 {
		report.Alloc = snap.Alloc
		report.HeapObjects = snap.HeapObjects
		report.NumGC = snap.NumGC
		report.Sys = snap.Sys
		report.HeapSys = snap.HeapSys
		report.StackSys = snap.StackSys
	}

	return report
}
