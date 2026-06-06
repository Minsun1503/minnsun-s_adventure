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
	once   sync.Once
	done   chan struct{}
	closed atomic.Bool
}

// New creates a Writer, spawns the drain goroutine, and returns the Writer.
// queueSize is the max number of buffered packets (recommended: 256).
// If queueSize < 1, a default of 256 is used.
func New(conn net.Conn, queueSize int) *Writer {
	if queueSize < 1 {
		queueSize = 256
	}
	w := &Writer{
		conn:  conn,
		queue: make(chan []byte, queueSize),
		done:  make(chan struct{}),
	}
	go w.drain()
	return w
}

// drain is the background goroutine that reads frames from the queue and
// writes them to the TCP connection. If a write fails, the connection is
// closed and the goroutine exits.
func (w *Writer) drain() {
	for frame := range w.queue {
		if err := netio.WritePacket(w.conn, frame); err != nil {
			w.conn.Close()
			break
		}
	}
	// Drain remaining frames on close — skip them to avoid writing to a closed conn
	for range w.queue {
		// consume remaining channel values silently
	}
	close(w.done)
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
func (w *Writer) Close() {
	w.once.Do(func() {
		w.closed.Store(true)
		w.conn.Close()
		close(w.queue)
	})
}

// Done returns a channel that is closed when the drain goroutine has exited.
// Callers can use this to wait for clean shutdown:
//
//	w.Close()
//	<-w.Done()
func (w *Writer) Done() <-chan struct{} {
	return w.done
}
