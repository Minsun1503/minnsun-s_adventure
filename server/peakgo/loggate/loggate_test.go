package loggate_test

import (
	"server/peakgo/loggate"
	"testing"
)

// BenchmarkDebugfProductionMode verifies that Debugf is a no-op (0 allocs)
// when debug mode is off (the production default).
//
// NOTE: logger.Init() is not called in this test, so debugMode defaults to
// false (the zero value of atomic.Bool). This correctly simulates production.
func BenchmarkDebugfProductionMode(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This must produce 0 allocs/op — if it doesn't, the guard is broken.
		loggate.Debugf("entity %d moved to (%d, %d)", 42, 10, 20)
	}
}

func BenchmarkInfof(b *testing.B) {
	// Infof always calls through to logger.Info. It will allocate due to fmt.Sprintf
	// inside logger.push — that is expected and acceptable for non-debug paths.
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		loggate.Infof("server tick %d", i)
	}
}

// TestDebugfDoesNotPanicWithoutInit ensures the function is safe to call
// before logger.Init() (e.g. in tests or early boot failures).
func TestDebugfDoesNotPanicWithoutInit(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Debugf panicked: %v", r)
		}
	}()
	loggate.Debugf("this should not panic: %d", 42)
}
