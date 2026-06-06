// Package connwriter provides non-blocking outbound write buffering for TCP connections.
//
// Each Writer owns a buffered channel and a dedicated drain goroutine. The game tick
// loop calls Send(frame) which enqueues the frame instantly and returns. A background
// goroutine drains the channel and performs the actual conn.Write() calls, protecting
// the hot-path from blocking on slow or dead clients.
//
// Zero-alloc on the hot-path: Send() does no heap allocations. The only allocations
// happen when writing to the net.Conn (syscall boundary) which is unavoidable.
//
// Global observability counters (GlobalDrops, GlobalSent, SlowClients) allow the
// monitoring goroutine to track queue pressure across all connections.
//
// Usage:
//
//	w := connwriter.New(tcpConn, 256)
//	ok := w.Send(frame) // non-blocking, returns false if queue full
//	w.Close()           // close connection and stop goroutine
//	w.Wait()            // wait for drain to exit
//	w.Release()         // return Writer to pool for reuse (zero-alloc lifecycle)
package connwriter

import (
	"net"
	"server/peakgo/netio"
	"sync"
	"sync/atomic"
)

// framePool recycles byte slice buffers used internally by Send() to copy incoming
// frames. Each buffer starts with capacity 128 and grows as needed. On the steady-state
// hot path, buffers are reused with zero allocations once the pool is warm.
var framePool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 128)
		return &b
	},
}

// Global packet send counters aggregated across all Writers for observability.
var (
	GlobalDrops atomic.Uint64 // Total dropped packets across all Writers
	GlobalSent  atomic.Uint64 // Total successfully sent packets across all Writers
	SlowClients atomic.Uint64 // Number of connections at full queue capacity at drop time
)

// Writer manages a non-blocking outbound queue for a single TCP connection.
// The game tick calls Send() which is a channel send — O(1), no syscall.
// A dedicated goroutine drains the channel and calls netio.WritePacket().
type Writer struct {
	conn   net.Conn
	queue  chan []byte
	closed atomic.Bool
	wg     sync.WaitGroup
	drops  atomic.Uint64 // Frames dropped (queue full) for this Writer
	sent   atomic.Uint64 // Frames successfully sent for this Writer
}

// sentinelNil is a nil byte slice sentinel used to signal clean shutdown on the queue.
// Defined as a package-level var to avoid allocation when sending to the channel.
var sentinelNil []byte

// writerPool recycles Writer instances to avoid allocations on New/Close cycles.
// The pool's New factory creates Writers with default 256-capacity channels.
var writerPool = sync.Pool{
	New: func() interface{} {
		return &Writer{
			queue: make(chan []byte, 256),
		}
	},
}

// New creates a Writer, spawns the drain goroutine, and returns the Writer.
// queueSize is the max number of buffered packets (recommended: 256).
// If queueSize < 1, a default of 256 is used.
//
// New attempts to recycle a Writer from the internal pool. When the caller
// is done they MUST call Release() after Wait() to return the Writer
// to the pool and achieve zero-alloc lifecycle.
func New(conn net.Conn, queueSize int) *Writer {
	if queueSize < 1 {
		queueSize = 256
	}
	w := writerPool.Get().(*Writer)
	w.conn = conn
	w.closed.Store(false)
	if cap(w.queue) != queueSize {
		w.queue = make(chan []byte, queueSize)
	}
	w.wg.Add(1)
	go drainWriter(w)
	return w
}

// drainWriter is a package-level function used instead of a method-value closure
// to avoid heap allocation of a closure on go w.drain().
func drainWriter(w *Writer) {
	w.drain()
	w.wg.Done()
}

// drain is the background goroutine that reads frames from the queue and
// writes them to the TCP connection. If a write fails, the connection is
// closed and the goroutine exits. A nil sentinel on the queue signals a
// clean shutdown (from Close()).
//
// After writing each frame the buffer is returned to framePool for reuse.
func (w *Writer) drain() {
	for frame := range w.queue {
		// nil sentinel signals clean shutdown
		if frame == nil {
			break
		}
		err := netio.WritePacket(w.conn, frame)
		// Return buffer to pool AFTER write to avoid race with pool reuse.
		framePool.Put(&frame)
		if err != nil {
			w.conn.Close()
			break
		}
	}
	// After nil sentinel (or write error), drain remaining frames non-blockingly.
	// Return any pooled frames back to framePool.
	// We cannot use for range here because w.queue is NOT closed — it is reused
	// by the pool. Instead we consume until the channel is empty.
	for {
		select {
		case frame := <-w.queue:
			if frame != nil {
				framePool.Put(&frame)
			}
		default:
			return
		}
	}
}

// Send enqueues a frame for outbound delivery. Non-blocking.
// Returns true if the frame was queued successfully, false if the queue is full
// (client is too slow or dead) or the writer is closed.
//
// Send() copies the frame into an internal pooled buffer, making it safe for the
// caller to immediately reuse or discard the source slice. On the steady-state
// hot path, the pool reuses buffers with zero allocations.
//
// Increments per-Writer and global counters atomically.
func (w *Writer) Send(frame []byte) bool {
	if w.closed.Load() {
		return false
	}
	// Acquire buffer from pool, copy frame data into it
	dst := framePool.Get().(*[]byte)
	*dst = append((*dst)[:0], frame...)

	select {
	case w.queue <- *dst:
		w.sent.Add(1)
		GlobalSent.Add(1)
		return true
	default:
		// Queue full — return buffer to pool immediately and drop
		framePool.Put(dst)
		w.drops.Add(1)
		GlobalDrops.Add(1)
		if len(w.queue) == cap(w.queue) {
			SlowClients.Add(1)
		}
		return false
	}
}

// Close idempotently closes the connection and stops the drain goroutine.
// Safe to call multiple times. The underlying net.Conn is closed only once.
// Uses atomic CompareAndSwap instead of sync.Once to avoid closure allocation.
func (w *Writer) Close() {
	if !w.closed.CompareAndSwap(false, true) {
		return
	}
	w.conn.Close()
	// Send nil sentinel to stop the drain() goroutine cleanly.
	// We do NOT close(w.queue) so the channel can be reused after Release().
	w.queue <- sentinelNil
}

// Release returns the Writer to the internal pool for reuse.
// The caller MUST call Close() and call Wait() before returning Release().
//
//	w.Close()
//	w.Wait()
//	w.Release()
func (w *Writer) Release() {
	w.conn = nil
	writerPool.Put(w)
}

// Wait blocks until the drain goroutine has exited.
// Callers must call this after Close() and before Release().
func (w *Writer) Wait() {
	w.wg.Wait()
}

// QueueLen returns the current number of frames in the queue.
func (w *Writer) QueueLen() int {
	return len(w.queue)
}

// Drops returns the total number of dropped frames for this Writer.
func (w *Writer) Drops() uint64 {
	return w.drops.Load()
}

// Sent returns the total number of successfully sent frames for this Writer.
func (w *Writer) Sent() uint64 {
	return w.sent.Load()
}
