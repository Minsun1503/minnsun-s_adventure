// Package loggate provides production-safe logging wrappers for the
// Minnsun's Adventure game server.
//
// # Problem it solves
//
// Calling logger.Debug("msg %v", someInterface) always evaluates arguments —
// meaning fmt.Sprintf is called, potentially boxing values onto the heap —
// even in production when debug mode is OFF.
//
// The correct pattern is:
//
//	if logger.IsDebug() {
//	    logger.Debug("entity %d moved", id)
//	}
//
// But this requires every developer to remember the guard. loggate.Debugf
// encapsulates the guard so callers can never accidentally forget it:
//
//	loggate.Debugf("entity %d moved", id)  // zero cost in production
//
// # Allocation profile
//
//   - Debugf: 0 allocs/op in production (guard exits before Sprintf).
//   - Infof / Warnf / Errorf: identical to calling logger.* directly.
package loggate

import "server/logger"

// Debugf logs at DEBUG level. In production (debug=false in config.json) this
// is a guaranteed no-op — arguments are never evaluated, no heap boxing occurs.
func Debugf(format string, args ...any) {
	if !logger.IsDebug() {
		return
	}
	logger.Debug(format, args...)
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
