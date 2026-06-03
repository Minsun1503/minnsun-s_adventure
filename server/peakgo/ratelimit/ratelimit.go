package ratelimit

import (
	"net"
	"server/peakgo/config"
	"sync"
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
// Thread-safe: Allow uses RLock, Register/Unregister uses Lock.
type RateLimiter struct {
	mu      sync.RWMutex
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
	rl.mu.Lock()
	rl.buckets[conn] = &TokenBucket{
		tokens:    cfg.RateLimitMaxTokens,
		maxTokens: cfg.RateLimitMaxTokens,
	}
	rl.mu.Unlock()
}

// UnregisterConnection removes a connection's bucket.
func (rl *RateLimiter) UnregisterConnection(conn net.Conn) {
	rl.mu.Lock()
	delete(rl.buckets, conn)
	rl.mu.Unlock()
}

// Allow checks if a packet is allowed for the given connection.
// Thread-safe: atomic CAS for refillTick exclusivity + token consume.
func (rl *RateLimiter) Allow(conn net.Conn, currentTick uint64) bool {
	rl.mu.RLock()
	tb, ok := rl.buckets[conn]
	rl.mu.RUnlock()
	if !ok {
		return true // not registered, allow (shouldn't happen)
	}

	cfg := config.C()

	// Phase 1: Refill — only one goroutine wins the CAS on refillTick
	for {
		lastTick := atomic.LoadUint64(&tb.refillTick)
		if currentTick <= lastTick {
			break // no refill needed
		}
		// Try to claim refillTick advancement
		if !atomic.CompareAndSwapUint64(&tb.refillTick, lastTick, currentTick) {
			continue // another goroutine refilled, re-check
		}
		// We won — do the actual refill
		elapsed := currentTick - lastTick
		if elapsed > 0 {
			var refill int32
			// Overflow guard: cap refill at maxTokens
			if elapsed >= uint64(tb.maxTokens)/uint64(cfg.RateLimitRefillPerTick) {
				refill = tb.maxTokens
			} else {
				refill = int32(elapsed) * cfg.RateLimitRefillPerTick
			}

			// Add tokens atomically with cap
			for {
				current := atomic.LoadInt32(&tb.tokens)
				newTokens := current + refill
				if newTokens > tb.maxTokens {
					newTokens = tb.maxTokens
				}
				if atomic.CompareAndSwapInt32(&tb.tokens, current, newTokens) {
					break
				}
			}
		}
		break
	}

	// Phase 2: Consume one token (CAS loop)
	for {
		current := atomic.LoadInt32(&tb.tokens)
		if current < 1 {
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
	rl.mu.RLock()
	tb, ok := rl.buckets[conn]
	rl.mu.RUnlock()
	if ok {
		atomic.StoreInt32(&tb.tokens, cfg.RateLimitMaxTokens)
		atomic.StoreUint64(&tb.refillTick, 0)
	}
}

// ConnCount returns the number of registered connections.
func (rl *RateLimiter) ConnCount() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return len(rl.buckets)
}
