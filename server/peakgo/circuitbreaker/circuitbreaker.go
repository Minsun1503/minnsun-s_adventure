package circuitbreaker

import (
	"fmt"
	"log"
	"math"
	"os"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State uint8

const (
	StateClosed   State = 0 // normal operation
	StateOpen     State = 1 // failing, fast-fail
	StateHalfOpen State = 2 // testing recovery
)

// Op is a generic database operation to execute.
type Op struct {
	Label string       // human-readable label for logging
	Run   func() error // the actual operation
}

// Config for the circuit breaker.
type Config struct {
	FailureThreshold int           // failures before opening circuit
	SuccessThreshold int           // successes in half-open before closing
	Timeout          time.Duration // time before moving from open to half-open
	MaxRetries       int           // max retries before circuit opens
	BaseBackoff      time.Duration // initial backoff duration
	MaxBackoff       time.Duration // maximum backoff duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxRetries:       3,
		BaseBackoff:      100 * time.Millisecond,
		MaxBackoff:       5 * time.Second,
	}
}

// WAL implements a simple write-ahead log for offline queuing.
type WAL struct {
	mu     sync.Mutex
	file   *os.File
	path   string
	ops    []Op
	bufLen int
}

// NewWAL creates a new write-ahead log at the given file path.
func NewWAL(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{
		file: f,
		path: path,
		ops:  make([]Op, 0, 8),
	}, nil
}

// Append adds an operation to the WAL (after logging its label).
func (w *WAL) Append(op Op) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ops = append(w.ops, op)
	w.bufLen++
	_, err := w.file.WriteString(fmt.Sprintf("%s\n", op.Label))
	return err
}

// Replay replays all buffered operations, calling fn for each.
// Clears the buffer after successful replay.
func (w *WAL) Replay(fn func(Op) error) error {
	w.mu.Lock()
	batch := make([]Op, len(w.ops))
	copy(batch, w.ops)
	w.mu.Unlock()

	for _, op := range batch {
		if err := fn(op); err != nil {
			return err
		}
	}

	// Clear successfully replayed ops
	w.mu.Lock()
	w.ops = w.ops[:0]
	w.bufLen = 0
	_ = w.file.Truncate(0)
	_, _ = w.file.Seek(0, 0)
	w.mu.Unlock()
	return nil
}

// Close closes the WAL file.
func (w *WAL) Close() error {
	return w.file.Close()
}

// CircuitBreaker provides retry with backoff, circuit breaking, and WAL fallback.
type CircuitBreaker struct {
	mu              sync.Mutex
	state           State
	failures        int
	successes       int
	lastFailureTime time.Time
	cfg             Config
	wal             *WAL
}

// NewCircuitBreaker creates a new circuit breaker.
// If walPath is non-empty, failed operations are recorded to a WAL.
func NewCircuitBreaker(cfg Config, walPath string) (*CircuitBreaker, error) {
	cb := &CircuitBreaker{
		state: StateClosed,
		cfg:   cfg,
	}
	if walPath != "" {
		wal, err := NewWAL(walPath)
		if err != nil {
			return nil, err
		}
		cb.wal = wal
	}
	return cb, nil
}

// Execute runs an operation with circuit breaker protection.
// It implements: retry with exponential backoff → circuit breaker → WAL fallback.
func (cb *CircuitBreaker) Execute(op Op) error {
	// Check circuit state
	if !cb.ready() {
		// Circuit is open: WAL fallback
		return cb.fallback(op)
	}

	// Attempt with retries
	var lastErr error
	for attempt := 1; attempt <= cb.cfg.MaxRetries; attempt++ {
		// Exponential backoff
		if attempt > 1 {
			backoff := cb.cfg.BaseBackoff * time.Duration(math.Pow(2, float64(attempt-2)))
			if backoff > cb.cfg.MaxBackoff {
				backoff = cb.cfg.MaxBackoff
			}
			time.Sleep(backoff)
		}

		if err := op.Run(); err != nil {
			lastErr = err
			log.Printf("[circuitbreaker] attempt %d/%d failed: %v", attempt, cb.cfg.MaxRetries, err)
			cb.recordFailure()
			continue
		}

		// Success
		cb.recordSuccess()
		return nil
	}

	// All retries failed: fallback to WAL
	log.Printf("[circuitbreaker] all %d retries exhausted, WAL fallback for %s", cb.cfg.MaxRetries, op.Label)
	if err := cb.fallback(op); err != nil {
		return fmt.Errorf("circuitbreaker: operation failed and WAL fallback also failed: %w", lastErr)
	}
	return lastErr
}

// ready returns true if the circuit breaker allows operation execution.
func (cb *CircuitBreaker) ready() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailureTime) > cb.cfg.Timeout {
			cb.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return true
}

// recordFailure records a failure and possibly opens the circuit.
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.cfg.FailureThreshold {
			cb.state = StateOpen
			log.Printf("[circuitbreaker] circuit OPEN after %d failures", cb.failures)
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.failures = 1
		log.Printf("[circuitbreaker] circuit OPEN after half-open failure")
	}
}

// recordSuccess records a success and possibly closes the circuit.
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successes++
	cb.failures = 0

	switch cb.state {
	case StateHalfOpen:
		if cb.successes >= cb.cfg.SuccessThreshold {
			cb.state = StateClosed
			cb.successes = 0
			log.Printf("[circuitbreaker] circuit CLOSED after %d successes", cb.successes)
		}
	}
}

// fallback writes the operation to the WAL for later replay.
func (cb *CircuitBreaker) fallback(op Op) error {
	if cb.wal == nil {
		return fmt.Errorf("circuitbreaker: no WAL configured, dropping operation: %s", op.Label)
	}
	return cb.wal.Append(op)
}

// ReplayWAL attempts to replay all WAL entries.
// Call this after reconnecting to the database.
func (cb *CircuitBreaker) ReplayWAL() error {
	if cb.wal == nil {
		return nil
	}
	log.Printf("[circuitbreaker] replaying WAL (%d ops)...", len(cb.wal.ops))
	return cb.wal.Replay(func(op Op) error {
		return op.Run()
	})
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Stats returns current circuit breaker statistics.
func (cb *CircuitBreaker) Stats() (State, int, int) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state, cb.failures, cb.successes
}

// Close cleans up resources.
func (cb *CircuitBreaker) Close() error {
	if cb.wal != nil {
		return cb.wal.Close()
	}
	return nil
}
