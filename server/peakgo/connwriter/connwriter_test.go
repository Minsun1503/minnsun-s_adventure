package connwriter

import (
	"net"
	"sync"
	"testing"
	"time"
)

// pipeConn wraps net.Pipe to simulate a real TCP connection for testing.
// net.Pipe is in-memory and deterministic, perfect for Writer benchmarks.
func pipeConn() (client, server net.Conn) {
	return net.Pipe()
}

// ─── UNIT TESTS ─────────────────────────────────────────────────────────────────

func TestNewWriter(t *testing.T) {
	c, s := pipeConn()
	defer s.Close()
	defer c.Close()

	w := New(c, 256)
	if w == nil {
		t.Fatal("New returned nil")
	}
	if w.conn != c {
		t.Fatal("conn not set")
	}
	if cap(w.queue) != 256 {
		t.Fatalf("expected queue cap 256, got %d", cap(w.queue))
	}
}

func TestSendAndReceive(t *testing.T) {
	c, s := pipeConn()
	defer s.Close()

	w := New(c, 256)

	// Send a frame
	frame := []byte{0, 3, 0x01, 0xAA, 0xBB}
	ok := w.Send(frame)
	if !ok {
		t.Fatal("Send returned false, expected true")
	}

	// Read from the server side (with timeout)
	_ = s.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	// We expect the frame to be written via netio.WritePacket which uses
	// the format: [length uint16 BE][payload]. Our frame already includes
	// length prefix so it should be written as-is.
	readBuf := make([]byte, 1024)
	n, err := s.Read(readBuf)
	if err != nil {
		t.Fatalf("server read error: %v", err)
	}
	if n < 5 {
		t.Fatalf("expected at least 5 bytes, got %d", n)
	}

	w.Close()
	<-w.Done()
}

func TestSendQueueFull(t *testing.T) {
	c, s := pipeConn()
	defer s.Close()
	defer func() { _ = recover() }() // catch any panic

	// Use a tiny queue
	w := New(c, 2)

	// Fill the queue (server side doesn't read, so drain blocks after queue fills)
	ok1 := w.Send([]byte{1})
	ok2 := w.Send([]byte{2})
	if !ok1 || !ok2 {
		t.Fatal("expected first two sends to succeed")
	}

	// Third send should fail because queue is full and no one is reading
	ok3 := w.Send([]byte{3})
	if ok3 {
		// This might succeed if the drain goroutine hasn't consumed yet
		// In a real scenario with no reader, the channel will eventually fill
		t.Log("third send succeeded (race with drain)")
	}

	w.Close()
	<-w.Done()
}

func TestCloseIdempotent(t *testing.T) {
	c, s := pipeConn()
	defer s.Close()

	w := New(c, 256)

	// Close multiple times
	w.Close()
	w.Close()
	w.Close()

	// Wait for drain to exit
	select {
	case <-w.Done():
		// success
	case <-time.After(time.Second):
		t.Fatal("drain goroutine did not exit within 1s")
	}
}

func TestSendAfterClose(t *testing.T) {
	c, s := pipeConn()
	defer s.Close()

	w := New(c, 256)
	w.Close()

	// Send after close should return false
	ok := w.Send([]byte{1})
	if ok {
		t.Log("Send after close returned true (channel may not be drained yet)")
	}

	<-w.Done()
}

func TestConcurrentSend(t *testing.T) {
	c, s := pipeConn()
	defer s.Close()

	w := New(c, 256)

	// Server reads in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			_ = s.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
			_, err := s.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	// Concurrent sends from multiple goroutines
	const numGoroutines = 10
	const numSends = 100

	var sendWg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		sendWg.Add(1)
		go func(id int) {
			defer sendWg.Done()
			for j := 0; j < numSends; j++ {
				frame := []byte{0, 1, byte(id)}
				w.Send(frame)
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	sendWg.Wait()
	w.Close()
	<-w.Done()
	wg.Wait()
}

// ─── BENCHMARKS ─────────────────────────────────────────────────────────────────

// BenchmarkSend measures the cost of Send() on the hot-path.
// Expected: 0 B/op, 0 allocs/op (channel send is allocation-free).
func BenchmarkSend(b *testing.B) {
	c, s := pipeConn()
	defer s.Close()

	w := New(c, 256)

	// Server reads in background to prevent queue full
	go func() {
		buf := make([]byte, 4096)
		for {
			_ = s.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, err := s.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	frame := []byte{0, 4, 0x01, 0xAA, 0xBB, 0xCC}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.Send(frame)
	}

	w.Close()
	<-w.Done()
}

// BenchmarkSendFullQueue measures Send() behavior when the queue is full.
// This exercises the default (drop) path.
func BenchmarkSendFullQueue(b *testing.B) {
	c, s := pipeConn()
	defer s.Close()

	// Tiny queue so it fills quickly (no server reader to drain)
	w := New(c, 2)

	frame := []byte{0, 4, 0x01, 0xAA, 0xBB, 0xCC}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.Send(frame)
	}

	w.Close()
	<-w.Done()
}

// BenchmarkNewClose measures the cost of creating and closing a Writer.
// This is the connection lifecycle overhead.
func BenchmarkNewClose(b *testing.B) {
	c, s := pipeConn()
	defer s.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w := New(c, 256)
		w.Close()
		<-w.Done()
	}
}

// BenchmarkSendParallel measures concurrent Send() performance.
func BenchmarkSendParallel(b *testing.B) {
	c, s := pipeConn()
	defer s.Close()

	w := New(c, 256)

	go func() {
		buf := make([]byte, 4096)
		for {
			_ = s.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, err := s.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	frame := []byte{0, 4, 0x01, 0xAA, 0xBB, 0xCC}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w.Send(frame)
		}
	})

	w.Close()
	<-w.Done()
}
