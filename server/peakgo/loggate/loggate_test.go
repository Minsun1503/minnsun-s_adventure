package loggate_test

import (
	"server/peakgo/loggate"
	"testing"
)

// ─── NO-PANIC LIFECYCLE TESTS ────────────────────────────────────────────────
//
// Đảm bảo toàn bộ hệ thống hàm bọc ghi log hoạt động an toàn tuyệt đối và không bao giờ
// gây sập server (panic) kể cả khi bị gọi vô tình trước khi logger chính được khởi tạo.

func TestDebugfDoesNotPanicWithoutInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Debugf panicked without init: %v", r)
		}
	}()
	loggate.Debugf("this should not panic: %d", 42)
}

func TestInfofDoesNotPanicWithoutInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Infof panicked without init: %v", r)
		}
	}()
	loggate.Infof("this should not panic: %s", "info_msg")
}

func TestWarnfDoesNotPanicWithoutInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Warnf panicked without init: %v", r)
		}
	}()
	loggate.Warnf("this should not panic: %s", "warn_msg")
}

func TestErrorfDoesNotPanicWithoutInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Errorf panicked without init: %v", r)
		}
	}()
	loggate.Errorf("this should not panic: %s", "error_msg")
}

// ─── BEHAVIORAL TESTS ────────────────────────────────────────────────────────

// TestDebugLazyDisabled xác thực cơ chế đóng băng thực thi closure của DebugLazy.
// Khi cờ hiệu Debug đang tắt (Production mode), nội dung hàm closure bắt buộc không được chạy.
func TestDebugLazyDisabled(t *testing.T) {
	called := false
	loggate.DebugLazy(func() {
		called = true
	})
	if called {
		t.Fatal("expected deferred logging closure to NOT be executed when debug mode is disabled")
	}
}

// ─── HIGH-FREQUENCY PERFORMANCE BENCHMARKS ───────────────────────────────────
//
// Các bài đo tải rạch ròi này phản ánh chân thực chi phí CPU/RAM của từng mô hình phòng vệ:
// 1. Debugf (Variadic): Thấy rõ lượng allocation sinh ra do Go Runtime bọc mảng []any ngầm.
// 2. DebugEnabled Guard: Đạt mốc 0 allocs/op hoàn hảo vì chặn đứng lát cắt ngay từ Call-site.
// 3. DebugLazy (Closure): Đạt mốc 0 allocs/op nhờ kỹ thuật trì hoãn đánh giá tham số.

// BenchmarkDebugfDisabled đo đạc chi phí khi gọi Debugf thông thường lúc log đã tắt.
func BenchmarkDebugfDisabled(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Chi phí bọc slice ngầm []any{i, 10, 20} vẫn xảy ra tại đây trước khi vào hàm!
		loggate.Debugf("entity %d moved to (%d, %d)", i, 10, 20)
	}
}

// BenchmarkDebugEnabledGuardDisabled chứng minh sức mạnh của việc tối ưu bằng cờ hiệu Guard.
func BenchmarkDebugEnabledGuardDisabled(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Chặn từ ngoài giúp triệt tiêu hoàn toàn việc khởi tạo slice variadic rác (0 allocs)
		if loggate.DebugEnabled() {
			loggate.Debugf("entity %d moved to (%d, %d)", i, 10, 20)
		}
	}
}

// BenchmarkDebugLazyDisabled xác thực giải pháp bọc closure an toàn cho các tác vụ nặng.
func BenchmarkDebugLazyDisabled(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Hàm ẩn danh không bị kích hoạt khi debug tắt, giải phóng hoàn toàn gánh nặng cho Heap
		loggate.DebugLazy(func() {
			loggate.Debugf("entity %d moved to (%d, %d)", i, 10, 20)
		})
	}
}

// BenchmarkInfof đo tải chi phí ghi log Info thông thường (luôn thực thi).
func BenchmarkInfof(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loggate.Infof("server heartbeat status checkpoint loop tick index: %d", i)
	}
}
