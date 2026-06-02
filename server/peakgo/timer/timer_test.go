package timer_test

import (
	"server/peakgo/timer"
	"testing"
	"time"
)

// ─── TICKTIMER CORRECTNESS TESTS ─────────────────────────────────────────────

// TestTickTimerNotReadyBeforeCooldown đảm bảo bộ đếm tick không báo Ready sớm
// khi chưa tích lũy đủ số lượng tick mục tiêu.
func TestTickTimerNotReadyBeforeCooldown(t *testing.T) {
	tt := timer.NewTickTimer(4)
	for i := 0; i < 3; i++ {
		tt.Advance()
		if tt.Ready() {
			t.Fatalf("expected not ready after %d ticks (cooldown=4)", i+1)
		}
	}
}

// TestTickTimerReadyAtCooldown xác thực bộ đếm báo Ready chính xác khi đạt mốc cooldown.
func TestTickTimerReadyAtCooldown(t *testing.T) {
	tt := timer.NewTickTimer(4)
	for i := 0; i < 4; i++ {
		tt.Advance()
	}
	if !tt.Ready() {
		t.Fatal("expected ready after 4 ticks (cooldown=4)")
	}
}

// TestTickTimerResetRestartsCooldown kiểm tra tính năng khôi phục bộ đếm về 0.
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

// TestTickTimerElapsed bổ sung kiểm thử cho hàm truy vấn số tick đã trôi qua.
func TestTickTimerElapsed(t *testing.T) {
	tt := timer.NewTickTimer(10)
	tt.Advance()
	tt.Advance()
	tt.Advance()

	if got := tt.Elapsed(); got != 3 {
		t.Fatalf("expected elapsed=3, got %d", got)
	}
}

// TestTickTimerRemaining xác thực số tick còn lại cho tới khi Ready.
func TestTickTimerRemaining(t *testing.T) {
	tt := timer.NewTickTimer(5)
	tt.Advance()
	tt.Advance()
	if tt.Remaining() != 3 {
		t.Fatalf("expected remaining=3, got %d", tt.Remaining())
	}
}

// TestTickTimerRemainingZeroWhenReady bảo vệ điều kiện biên, Remaining luôn bằng 0
// kể cả khi hệ thống bị over-tick (vượt quá mốc cooldown).
func TestTickTimerRemainingZeroWhenReady(t *testing.T) {
	tt := timer.NewTickTimer(2)
	tt.Advance()
	tt.Advance()
	tt.Advance() // over-tick
	if tt.Remaining() != 0 {
		t.Fatalf("expected remaining=0 when overdue, got %d", tt.Remaining())
	}
}

// TestTickTimerAdvanceAndCheck kiểm tra mô hình kết hợp tăng-và-kiểm-tra nhanh.
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

// TestTickTimerTickAPI xác thực hàm ngữ nghĩa cao mới bổ sung.
// Hàm này bắt buộc phải tự động Reset bộ đếm về 0 ngay khi đủ điều kiện Ready.
func TestTickTimerTickAPI(t *testing.T) {
	tt := timer.NewTickTimer(2)

	if tt.Tick() {
		t.Fatal("should not fire on the first tick")
	}
	// Tick thứ 2 đạt mốc cooldown -> Phải trả về true và tự động reset bộ đếm về 0
	if !tt.Tick() {
		t.Fatal("should fire on the second tick")
	}
	if tt.Elapsed() != 0 {
		t.Fatalf("expected auto-reset to 0 after firing, got %d", tt.Elapsed())
	}
}

// TestTickTimerSetCooldown kiểm tra tính năng thay đổi tốc độ cooldown động (khi mang item/buff).
func TestTickTimerSetCooldown(t *testing.T) {
	tt := timer.NewTickTimer(5)
	tt.Advance()
	tt.Advance() // Đang tích được 2 ticks

	tt.SetCooldown(2) // Giảm mốc xuống còn 2 -> Phải Ready ngay lập tức
	if !tt.Ready() {
		t.Fatal("expected timer to become ready after lowering cooldown dynamic bounds")
	}
}

// TestTickTimerProgress xác thực tỷ lệ phần trăm hoàn thành vòng cooldown (0.0 -> 1.0)
func TestTickTimerProgress(t *testing.T) {
	tt := timer.NewTickTimer(4)
	if tt.Progress() != 0.0 {
		t.Fatalf("expected progress 0.0, got %f", tt.Progress())
	}
	tt.Advance()
	if tt.Progress() != 0.25 {
		t.Fatalf("expected progress 0.25, got %f", tt.Progress())
	}
}

// TestTickTimerGuardCaps kiểm tra cơ chế phòng vệ nghiêm ngặt đã vá ở bước trước.
// Mọi giá trị cooldown rác (<= 0) nhập vào bắt buộc phải bị cưỡng ép đưa về bằng 1.
func TestTickTimerGuardCaps(t *testing.T) {
	// Case cooldown = 0
	ttZero := timer.NewTickTimer(0)
	if ttZero.Cooldown() != 1 {
		t.Fatalf("expected zero cooldown to cap at 1, got %d", ttZero.Cooldown())
	}

	// Case cooldown âm
	ttNeg := timer.NewTickTimer(-100)
	if ttNeg.Cooldown() != 1 {
		t.Fatalf("expected negative cooldown to cap at 1, got %d", ttNeg.Cooldown())
	}

	// Case SetCooldown rác
	ttZero.SetCooldown(-5)
	if ttZero.Cooldown() != 1 {
		t.Fatalf("expected SetCooldown dynamic rác to cap at 1")
	}
}

// ─── WALLTIMER CORRECTNESS TESTS ─────────────────────────────────────────────

func TestWallTimerNotDoneImmediately(t *testing.T) {
	wt := timer.NewWallTimer(1 * time.Hour)
	if wt.Done() {
		t.Fatal("timer with 1h duration should not be done immediately")
	}
}

func TestWallTimerDoneAfterExpiry(t *testing.T) {
	wt := timer.NewWallTimer(-1 * time.Millisecond) // Đã hết hạn từ quá khứ
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

// TestWallTimerRemaining Đã sửa lỗi Flaky theo đúng yêu cầu của kỹ sư.
// Kiểm tra biên an toàn bằng chặn khoảng trên, loại bỏ hoàn toàn sự phụ thuộc vào độ trễ Scheduler.
func TestWallTimerRemaining(t *testing.T) {
	wt := timer.NewWallTimer(500 * time.Millisecond)
	r := wt.Remaining()

	if r <= 0 {
		t.Fatalf("expected positive remaining duration, got %v", r)
	}
	if r > 500*time.Millisecond {
		t.Fatalf("remaining duration exceeds original bounds: %v", r)
	}
}

func TestWallTimerRemainingZeroWhenExpired(t *testing.T) {
	wt := timer.NewWallTimer(-1 * time.Second)
	if wt.Remaining() != 0 {
		t.Fatalf("expected 0 remaining for expired timer, got %v", wt.Remaining())
	}
}

func TestWallTimerExpiredDuration(t *testing.T) {
	past := time.Now().Add(-250 * time.Millisecond)
	wt := timer.NewWallTimerAt(past)

	if wt.ExpiredDuration() <= 0 {
		t.Fatal("expected expired duration to be positive for past deadlines")
	}
}

// TestWallTimerRefresh API xác thực tính năng làm mới mốc thời gian (Now + d).
func TestWallTimerRefresh(t *testing.T) {
	wt := timer.NewWallTimer(-1 * time.Millisecond) // Đã hết hạn
	wt.Refresh(1 * time.Hour)                       // Làm mới thêm 1 tiếng tính từ bây giờ
	if wt.Done() {
		t.Fatal("after Refresh, timer should not be done")
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

func TestWallTimerNewWallTimerAtAndIsZero(t *testing.T) {
	// Kiểm tra tính năng IsZero cho thực thể chưa khởi tạo mốc thời gian
	var wtZero timer.WallTimer
	if !wtZero.IsZero() {
		t.Fatal("expected uninitialized timer to report IsZero=true")
	}

	past := time.Now().Add(-1 * time.Second)
	wt := timer.NewWallTimerAt(past)
	if !wt.Done() {
		t.Fatal("timer pointing to past should be done")
	}
	if wt.IsZero() {
		t.Fatal("initialized timer should not report IsZero=true")
	}
}

// ─── STRICT ZERO-ALLOCATION CONTRACTS (AllocsPerRun) ───────────────────────

// TestTimerPackageZeroAllocations khóa chết giao kèo tối ưu RAM của framework,
// đảm bảo tuyệt đối không có hành vi rò rỉ hay cấp phát heap ngầm khi tính toán thời gian.
func TestTimerPackageZeroAllocations(t *testing.T) {
	tt := timer.NewTickTimer(5)
	allocs := testing.AllocsPerRun(1000, func() {
		tt.Advance()
		_ = tt.Ready()
		_ = tt.Tick()
	})
	if allocs > 0 {
		t.Fatalf("TickTimer operations leaked %f allocations to the heap", allocs)
	}

	wt := timer.NewWallTimer(time.Hour)
	now := time.Now()
	allocs = testing.AllocsPerRun(1000, func() {
		_ = wt.DoneAt(now)
		_ = wt.Remaining()
	})
	if allocs > 0 {
		t.Fatalf("WallTimer operations leaked %f allocations to the heap", allocs)
	}
}

// ─── BENCHMARKS ───────────────────────────────────────────────────────────────

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

// BenchmarkWallTimerDoneAt measures pure comparison cost without calling time.Now()
// dynamically inside the hot loop.
func BenchmarkWallTimerDoneAt(b *testing.B) {
	wt := timer.NewWallTimer(1 * time.Hour)
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = wt.DoneAt(now)
	}
}

// BenchmarkTickTimerVsManual chứng minh cấu trúc đóng gói nâng cao của TickTimer
// có hiệu năng tương đương 100% (Zero-Overhead) so với việc cộng biến int thô thủ công.
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

// BenchmarkIsReadyPeakGo measures TickTimer.Tick() hot-path with auto-reset.
func BenchmarkIsReadyPeakGo(b *testing.B) {
	tt := timer.NewTickTimer(4)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tt.Tick()
	}
}
