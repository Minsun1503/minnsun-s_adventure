// Package loggate provides production-safe logging wrappers for the
// Minnsun's Adventure game server core.
//
// # The Allocation Problem
//
// Calling loggate.Debugf("entity %d moved", entity.ID) still evaluates the
// variadic parameters and allocates an argument slice (`[]any`) at the call site
// before entering the function, even when debug mode is turned OFF.
//
// To achieve a true, absolute zero-cost footprint on hyper-critical hot paths
// (such as spatial grids, entity motion ticks, or network frame processing),
// you must either explicitly guard the call with DebugEnabled() or utilize
// the deferred block helper DebugLazy().
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
// Performance Warning: This method prevents expensive string formatting and logger
// dispatching when disabled. However, variadic arguments are still evaluated and
// wrapped into an allocation slice by the caller before invocation.
func Debugf(format string, args ...any) {
	if !logger.IsDebug() {
		return
	}
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

// Infof logs at INFO level. Matches logger.Info signature exactly.
func Infof(format string, args ...any) {
	logger.Info(format, args...)
}

// Warnf logs at WARN level. Matches logger.Warn signature exactly.
func Warnf(format string, args ...any) {
	logger.Warn(format, args...)
}

// Errorf logs at ERROR level. Matches logger.Error signature exactly.
func Errorf(format string, args ...any) {
	logger.Error(format, args...)
}
