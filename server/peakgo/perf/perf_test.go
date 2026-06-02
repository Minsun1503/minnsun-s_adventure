package perf

import (
	"testing"
	"time"
)

func TestTickMonitorRecord(t *testing.T) {
	tm := &TickMonitor{}
	tm.RecordTick(100 * time.Microsecond)
	tm.RecordTick(200 * time.Microsecond)
	tm.RecordTick(300 * time.Microsecond)

	if tm.Count() != 3 {
		t.Fatalf("expected count 3, got %d", tm.Count())
	}
	if tm.Min() != 100*time.Microsecond {
		t.Fatalf("expected min 100µs, got %v", tm.Min())
	}
	if tm.Max() != 300*time.Microsecond {
		t.Fatalf("expected max 300µs, got %v", tm.Max())
	}
}

func TestTickMonitorAvg(t *testing.T) {
	tm := &TickMonitor{}
	tm.RecordTick(100 * time.Microsecond)
	tm.RecordTick(200 * time.Microsecond)

	if tm.Avg() != 150*time.Microsecond {
		t.Fatalf("expected avg 150µs, got %v", tm.Avg())
	}
}

func TestTickMonitorEmpty(t *testing.T) {
	tm := &TickMonitor{}
	if tm.Min() != 0 {
		t.Fatal("expected min 0 for empty monitor")
	}
	if tm.Avg() != 0 {
		t.Fatal("expected avg 0 for empty monitor")
	}
	if tm.Count() != 0 {
		t.Fatal("expected count 0 for empty monitor")
	}
}

func TestTickMonitorReset(t *testing.T) {
	tm := &TickMonitor{}
	tm.RecordTick(100 * time.Microsecond)
	tm.Reset()

	if tm.Count() != 0 {
		t.Fatal("expected count 0 after reset")
	}
}

func TestTickMonitorRingBuffer(t *testing.T) {
	tm := &TickMonitor{}
	// Write more than ring buffer capacity
	for range RingBufferSize + 10 {
		tm.RecordTick(100 * time.Microsecond)
	}

	if tm.Overflow() != 10 {
		t.Fatalf("expected overflow 10, got %d", tm.Overflow())
	}
}

func TestHistogramRecord(t *testing.T) {
	h := NewHistogram()
	h.Record(5 * time.Microsecond)  // Bucket 0: <= 10µs
	h.Record(20 * time.Microsecond) // Bucket 1: <= 50µs
	h.Record(200 * time.Microsecond)

	snap := h.Snapshot()
	if snap[0] != 1 {
		t.Fatalf("expected bucket 0 count 1, got %d", snap[0])
	}
	if snap[1] != 1 {
		t.Fatalf("expected bucket 1 count 1, got %d", snap[1])
	}
}

func TestHistogramReset(t *testing.T) {
	h := NewHistogram()
	h.Record(100 * time.Microsecond)
	h.Reset()

	snap := h.Snapshot()
	total := 0
	for _, v := range snap {
		total += int(v)
	}
	if total != 0 {
		t.Fatal("expected all buckets 0 after reset")
	}
}

func TestMemMonitorSample(t *testing.T) {
	mm := NewMemMonitor(1) // Sample every call
	snap := mm.Sample()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.Alloc == 0 && snap.HeapObjects == 0 {
		t.Log("memory stats may be zero in testing")
	}
}

func TestMemMonitorSampleRate(t *testing.T) {
	mm := NewMemMonitor(5) // Sample every 5th call

	// First 4 calls should return nil
	for range 4 {
		if mm.Sample() != nil {
			t.Fatal("expected nil for non-sample calls")
		}
	}

	// 5th call should return a snapshot
	snap := mm.Sample()
	if snap == nil {
		t.Fatal("expected snapshot on 5th call")
	}
}

func TestMemMonitorLast(t *testing.T) {
	mm := NewMemMonitor(1)
	mm.Sample()
	last := mm.Last()
	if last.Goroutines <= 0 {
		t.Fatal("expected goroutines > 0")
	}
}

func TestPacketMonitor(t *testing.T) {
	pm := &PacketMonitor{}
	pm.RecordIn(100)
	pm.RecordOut(200)

	in, out, bytesIn, bytesOut := pm.Snapshot()
	if in != 1 {
		t.Fatalf("expected 1 packet in, got %d", in)
	}
	if out != 1 {
		t.Fatalf("expected 1 packet out, got %d", out)
	}
	if bytesIn != 100 {
		t.Fatalf("expected 100 bytes in, got %d", bytesIn)
	}
	if bytesOut != 200 {
		t.Fatalf("expected 200 bytes out, got %d", bytesOut)
	}
}

func TestPacketMonitorReset(t *testing.T) {
	pm := &PacketMonitor{}
	pm.RecordIn(100)
	pm.Reset()

	in, _, _, _ := pm.Snapshot()
	if in != 0 {
		t.Fatal("expected 0 packets after reset")
	}
}

func TestCollect(t *testing.T) {
	tm := &TickMonitor{}
	pm := &PacketMonitor{}
	mm := NewMemMonitor(1)

	tm.RecordTick(100 * time.Microsecond)
	pm.RecordIn(50)
	mm.Sample()

	report := Collect(tm, pm, mm)
	if report.TickCount != 1 {
		t.Fatalf("expected 1 tick, got %d", report.TickCount)
	}
	if report.PacketsIn != 1 {
		t.Fatalf("expected 1 packet in, got %d", report.PacketsIn)
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkTickMonitorRecord(b *testing.B) {
	tm := &TickMonitor{}
	b.ResetTimer()
	for range b.N {
		tm.RecordTick(time.Microsecond)
	}
}

func BenchmarkHistogramRecord(b *testing.B) {
	h := NewHistogram()
	b.ResetTimer()
	for range b.N {
		h.Record(time.Microsecond)
	}
}

func BenchmarkPacketMonitorRecord(b *testing.B) {
	pm := &PacketMonitor{}
	b.ResetTimer()
	for range b.N {
		pm.RecordIn(100)
	}
}

func BenchmarkCollect(b *testing.B) {
	tm := &TickMonitor{}
	pm := &PacketMonitor{}
	mm := NewMemMonitor(1)

	for range 100 {
		tm.RecordTick(time.Microsecond)
		pm.RecordIn(50)
	}
	mm.Sample()

	b.ResetTimer()
	for range b.N {
		Collect(tm, pm, mm)
	}
}
