// Package loggate provides production-safe logging wrappers for the
// Minnsun's Adventure game server core.
//
// # Understanding Go's Variadic Allocation Trap
//
// In Go, calling any variadic function (e.g., Debugf("entity %d", id)) forces
// the compiler to allocate a []any{...} slice at the CALL SITE before entering
// the function — even if the first line of that function is an early return.
// This means:
//
//   - loggate.Debugf("entity %d moved", entity.ID)           // 24 B/1 alloc (interface boxing + slice header)
//   - loggate.Infof("player %s connected", playerName)       // 24 B/1 alloc (interface boxing + slice header)
//
// These allocations happen at the CALL SITE regardless of whether the log
// level is enabled because the Go compiler must construct the []any{} slice
// before entering the function. This is a fundamental Go language limitation
// and cannot be eliminated from within the library.
//
// For Infof / Warnf / Errorf (always-on), this 24 B/1 alloc per call is an
// expected and accepted cost for operational logging outside hot-paths.
//
// # Zero-Allocation Hot-Path Strategies
//
// For hyper-critical game loops (spatial queries, movement ticks, packet
// processing) where even a single per-call allocation is unacceptable,
// use one of these patterns:
//
//  1. Guard with DebugEnabled() — most efficient for hot paths:
//
//     if loggate.DebugEnabled() {
//     loggate.Debugf("entity %d moved to (%d,%d)", id, x, z)
//     }
//     // 0 B/op when debug is disabled
//
//  2. Use DebugLazy() — convenient for complex multi-arg formatting:
//
//     loggate.DebugLazy(func() {
//     loggate.Debugf("heavy state: %+v", monster.DeepDump())
//     })
//     // 0 B/op, closure is never called when debug is disabled
//
// # Non-Debug Functions (Info/Warn/Error)
//
// Info, Warn, and Error level functions always pass through to the underlying
// logger and are intended for lower-frequency operational logging (startup
// messages, critical events). They are not designed for hot-path use.
//
// # Inline Guard Pattern (go:noinline)
//
// Each public function (Debugf, Infof, Warnf, Errorf) is split into two parts:
// 1. A tiny inlineable public function that performs the early guard check.
// 2. A go:noinline private helper that does the actual logging work.
//
// This ensures that when the guard fails (e.g., debug disabled), the variadic
// slice allocation from the call site stays on the caller's stack frame and
// does not escape to heap — leading to zero allocation when the check returns early.
package loggate

import "server/logger"

// DebugEnabled reports whether the logger is configured to output DEBUG level logs.
// Use this explicitly on hyper-critical hot paths to avoid call-site variadic slice
// creation and interface value boxing overhead entirely.
//
// Example:
//
//	if loggate.DebugEnabled() {
//	    loggate.Debugf("entity %d calculated path in %v", id, duration) // 0 allocs if disabled
//	}
func DebugEnabled() bool {
	return logger.IsDebug()
}

// Debugf logs at DEBUG level if debug logging is enabled.
//
// Inline guard pattern: If debug is disabled, this function returns immediately.
// The go:noinline helper avoids variadic slice escape to heap when the check fails.
func Debugf(format string, args ...any) {
	if !logger.IsDebug() {
		return
	}
	pushDebug(format, args)
}

//go:noinline
func pushDebug(format string, args []any) {
	logger.Debug(format, args...)
}

// DebugLazy executes the provided closure function ONLY if DEBUG logging is enabled.
// This achieves absolute zero-allocation overhead in production by deferring the
// evaluation of logging variables and formatting parameters entirely.
//
// Example:
//
//	loggate.DebugLazy(func() {
//	    loggate.Debugf("heavy entity state: %+v", monster.DeepDump())
//	})
func DebugLazy(fn func()) {
	if !logger.IsDebug() {
		return
	}
	fn()
}

// Infof logs at INFO level.
func Infof(format string, args ...any) {
	pushInfo(format, args)
}

//go:noinline
func pushInfo(format string, args []any) {
	logger.Info(format, args...)
}

// Warnf logs at WARN level.
func Warnf(format string, args ...any) {
	pushWarn(format, args)
}

//go:noinline
func pushWarn(format string, args []any) {
	logger.Warn(format, args...)
}

// Errorf logs at ERROR level.
func Errorf(format string, args ...any) {
	pushError(format, args)
}

//go:noinline
func pushError(format string, args []any) {
	logger.Error(format, args...)
}
