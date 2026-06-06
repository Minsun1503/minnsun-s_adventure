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

// Writer manages a non-blocking outbound queue for a single TCP connection.
// The game tick calls Send() which is a channel send — O(1), no syscall.
// A dedicated goroutine drains the channel and calls netio.WritePacket().
type Writer struct {
	conn   net.Conn
	queue  chan []byte
	closed atomic.Bool
	wg     sync.WaitGroup
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
func (w *Writer) drain() {
	for frame := range w.queue {
		// nil sentinel signals clean shutdown
		if frame == nil {
			break
		}
		if err := netio.WritePacket(w.conn, frame); err != nil {
			w.conn.Close()
			break
		}
	}
	// After nil sentinel (or write error), drain remaining frames non-blockingly.
	// We cannot use for range here because w.queue is NOT closed — it is reused
	// by the pool. Instead we consume until the channel is empty.
	for {
		select {
		case <-w.queue:
			// consume remaining channel values silently
		default:
			// queue is empty
			return
		}
	}
}

// Send enqueues a frame for outbound delivery. Non-blocking.
// Returns true if the frame was queued successfully, false if the queue is full
// (client is too slow or dead) or the writer is closed.
//
// The caller must NOT retain or reuse the frame slice after calling Send() —
// the frame is passed directly to the channel and will be read by the drain goroutine.
func (w *Writer) Send(frame []byte) bool {
	if w.closed.Load() {
		return false
	}
	select {
	case w.queue <- frame:
		return true
	default:
		// Queue full — drop frame silently (slow client protection)
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
