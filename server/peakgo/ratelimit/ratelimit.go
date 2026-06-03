package ratelimit

import (
	"net"
	"server/peakgo/config"
	"sync/atomic"
)

// TokenBucket implements a per-connection token bucket rate limiter.
// Uses the game tick for refill to avoid time.Now() calls.
// All fields are aligned for atomic access on 64-bit systems.
type TokenBucket struct {
	tokens     int32    // current token count (atomic)
	maxTokens  int32    // burst limit
	refillTick uint64   // last tick when refill happened (atomic)
	_          [44]byte // padding to prevent false sharing
}

// RateLimiter manages per-connection token buckets.
// Must be created with NewRateLimiter().
type RateLimiter struct {
	buckets map[net.Conn]*TokenBucket
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[net.Conn]*TokenBucket),
	}
}

// RegisterConnection adds a new connection with a token bucket.
func (rl *RateLimiter) RegisterConnection(conn net.Conn) {
	cfg := config.C()
	rl.buckets[conn] = &TokenBucket{
		tokens:    cfg.RateLimitMaxTokens,
		maxTokens: cfg.RateLimitMaxTokens,
	}
}

// UnregisterConnection removes a connection's bucket.
func (rl *RateLimiter) UnregisterConnection(conn net.Conn) {
	delete(rl.buckets, conn)
}

// Allow checks if a packet is allowed for the given connection.
// If false, the packet should be dropped (DoS prevention).
func (rl *RateLimiter) Allow(conn net.Conn, currentTick uint64) bool {
	tb, ok := rl.buckets[conn]
	if !ok {
		return true // not registered, allow (shouldn't happen)
	}

	cfg := config.C()
	// Refill based on elapsed ticks
	lastTick := atomic.LoadUint64(&tb.refillTick)
	if currentTick > lastTick {
		elapsed := currentTick - lastTick
		if elapsed > 0 {
			refill := int32(elapsed) * cfg.RateLimitRefillPerTick
			// atomic add with cap
			newTokens := atomic.LoadInt32(&tb.tokens) + refill
			if newTokens > tb.maxTokens {
				newTokens = tb.maxTokens
			}
			atomic.StoreInt32(&tb.tokens, newTokens)
			atomic.StoreUint64(&tb.refillTick, currentTick)
		}
	}

	// Try to consume one token (atomic decrement if > 0)
	for {
		current := atomic.LoadInt32(&tb.tokens)
		if current <= 0 {
			return false
		}
		if atomic.CompareAndSwapInt32(&tb.tokens, current, current-1) {
			return true
		}
	}
}

// Reset resets a connection's bucket to full tokens.
func (rl *RateLimiter) Reset(conn net.Conn) {
	cfg := config.C()
	if tb, ok := rl.buckets[conn]; ok {
		atomic.StoreInt32(&tb.tokens, cfg.RateLimitMaxTokens)
		atomic.StoreUint64(&tb.refillTick, 0)
	}
}

// ConnCount returns the number of registered connections.
func (rl *RateLimiter) ConnCount() int {
	return len(rl.buckets)
}
