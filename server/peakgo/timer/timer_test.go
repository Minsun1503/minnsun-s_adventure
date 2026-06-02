package timer_test

import (
	"server/peakgo/timer"
	"testing"
	"time"
)

// ─── TickTimer correctness ────────────────────────────────────────────────────

func TestTickTimerNotReadyBeforeCooldown(t *testing.T) {
	tt := timer.NewTickTimer(4)
	for i := 0; i < 3; i++ {
		tt.Advance()
		if tt.Ready() {
			t.Fatalf("expected not ready after %d ticks (cooldown=4)", i+1)
		}
	}
}

func TestTickTimerReadyAtCooldown(t *testing.T) {
	tt := timer.NewTickTimer(4)
	for i := 0; i < 4; i++ {
		tt.Advance()
	}
	if !tt.Ready() {
		t.Fatal("expected ready after 4 ticks (cooldown=4)")
	}
}

func TestTickTimerResetRestartsCooldown(t *testing.T) {
	tt := timer.NewTickTimer(2)
	tt.Advance()
	tt.Advance()
	if !tt.Ready() {
		t.Fatal("expected ready")
	}
	tt.Reset()
	if tt.Ready() {
		t.Fatal("expected not ready after reset")
	}
	if tt.Elapsed() != 0 {
		t.Fatalf("expected elapsed=0 after reset, got %d", tt.Elapsed())
	}
}

func TestTickTimerRemaining(t *testing.T) {
	tt := timer.NewTickTimer(5)
	tt.Advance()
	tt.Advance()
	if tt.Remaining() != 3 {
		t.Fatalf("expected remaining=3, got %d", tt.Remaining())
	}
}

func TestTickTimerRemainingZeroWhenReady(t *testing.T) {
	tt := timer.NewTickTimer(2)
	tt.Advance()
	tt.Advance()
	tt.Advance() // over-tick
	if tt.Remaining() != 0 {
		t.Fatalf("expected remaining=0 when overdue, got %d", tt.Remaining())
	}
}

func TestTickTimerAdvanceAndCheck(t *testing.T) {
	tt := timer.NewTickTimer(3)
	if tt.AdvanceAndCheck() {
		t.Fatal("should not be ready after 1 tick")
	}
	if tt.AdvanceAndCheck() {
		t.Fatal("should not be ready after 2 ticks")
	}
	if !tt.AdvanceAndCheck() {
		t.Fatal("should be ready after 3 ticks")
	}
}

// ─── WallTimer correctness ────────────────────────────────────────────────────

func TestWallTimerNotDoneImmediately(t *testing.T) {
	wt := timer.NewWallTimer(1 * time.Hour)
	if wt.Done() {
		t.Fatal("timer with 1h duration should not be done immediately")
	}
}

func TestWallTimerDoneAfterExpiry(t *testing.T) {
	wt := timer.NewWallTimer(-1 * time.Millisecond) // already expired
	if !wt.Done() {
		t.Fatal("timer with negative duration should be done immediately")
	}
}

func TestWallTimerDoneAt(t *testing.T) {
	wt := timer.NewWallTimer(100 * time.Millisecond)
	now := time.Now()
	if wt.DoneAt(now) {
		t.Fatal("should not be done at current time")
	}
	future := now.Add(200 * time.Millisecond)
	if !wt.DoneAt(future) {
		t.Fatal("should be done at future time past expiry")
	}
}

func TestWallTimerRemaining(t *testing.T) {
	wt := timer.NewWallTimer(500 * time.Millisecond)
	r := wt.Remaining()
	if r <= 0 || r > 500*time.Millisecond {
		t.Fatalf("unexpected remaining: %v", r)
	}
}

func TestWallTimerRemainingZeroWhenExpired(t *testing.T) {
	wt := timer.NewWallTimer(-1 * time.Second)
	if wt.Remaining() != 0 {
		t.Fatalf("expected 0 remaining for expired timer, got %v", wt.Remaining())
	}
}

func TestWallTimerExtend(t *testing.T) {
	wt := timer.NewWallTimer(-1 * time.Millisecond) // already expired
	wt.Extend(1 * time.Hour)
	if wt.Done() {
		t.Fatal("after Extend, timer should not be done")
	}
}

func TestWallTimerExtendFrom(t *testing.T) {
	wt := timer.NewWallTimer(10 * time.Second)
	before := wt.ExpiresAt()
	wt.ExtendFrom(5 * time.Second)
	after := wt.ExpiresAt()
	if !after.After(before) {
		t.Fatal("ExtendFrom should push deadline forward")
	}
	diff := after.Sub(before)
	if diff < 4*time.Second || diff > 6*time.Second {
		t.Fatalf("expected ~5s extension, got %v", diff)
	}
}

func TestWallTimerNewWallTimerAt(t *testing.T) {
	past := time.Now().Add(-1 * time.Second)
	wt := timer.NewWallTimerAt(past)
	if !wt.Done() {
		t.Fatal("timer pointing to past should be done")
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkTickTimerAdvanceAndCheck(b *testing.B) {
	tt := timer.NewTickTimer(4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if tt.AdvanceAndCheck() {
			tt.Reset()
		}
	}
}

func BenchmarkWallTimerDone(b *testing.B) {
	wt := timer.NewWallTimer(1 * time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wt.Done()
	}
}

func BenchmarkWallTimerDoneAt(b *testing.B) {
	wt := timer.NewWallTimer(1 * time.Hour)
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wt.DoneAt(now)
	}
}

// BenchmarkTickTimerVsManual shows that TickTimer adds zero overhead
// compared to the raw ai.AttackTick++ pattern.
func BenchmarkTickTimerVsManual(b *testing.B) {
	b.Run("TickTimer", func(b *testing.B) {
		tt := timer.NewTickTimer(4)
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if tt.AdvanceAndCheck() {
				tt.Reset()
			}
		}
	})
	b.Run("ManualPattern", func(b *testing.B) {
		cur, cooldown := 0, 4
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cur++
			if cur >= cooldown {
				cur = 0
			}
		}
	})
}
