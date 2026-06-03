package ratelimit

import (
	"net"
	"server/peakgo/config"
	"testing"
	"time"
)

// testKey implements net.Conn as a comparable key for map usage.
type testKey int

func (k testKey) Network() string                    { return "test" }
func (k testKey) String() string                     { return "" }
func (k testKey) Read(b []byte) (n int, err error)   { return 0, nil }
func (k testKey) Write(b []byte) (n int, err error)  { return 0, nil }
func (k testKey) Close() error                       { return nil }
func (k testKey) LocalAddr() net.Addr                { return nil }
func (k testKey) RemoteAddr() net.Addr               { return nil }
func (k testKey) SetDeadline(t time.Time) error      { return nil }
func (k testKey) SetReadDeadline(t time.Time) error  { return nil }
func (k testKey) SetWriteDeadline(t time.Time) error { return nil }

func init() {
	config.InitConfig("")
}

func TestRegisterAndAllow(t *testing.T) {
	rl := NewRateLimiter()
	var conn testKey = 1

	rl.RegisterConnection(conn)
	if rl.ConnCount() != 1 {
		t.Fatalf("expected 1 connection, got %d", rl.ConnCount())
	}

	// First 60 tokens should be allowed (burst)
	for i := 0; i < 60; i++ {
		if !rl.Allow(conn, 0) {
			t.Fatalf("expected token %d to be allowed", i)
		}
	}

	// 61st should be denied
	if rl.Allow(conn, 0) {
		t.Fatal("expected 61st token to be denied")
	}
}

func TestRefillOverTime(t *testing.T) {
	rl := NewRateLimiter()
	var conn testKey = 1

	rl.RegisterConnection(conn)

	// Drain all tokens
	for i := 0; i < 60; i++ {
		rl.Allow(conn, 0)
	}

	// Should be dry
	if rl.Allow(conn, 0) {
		t.Fatal("expected denied after drain")
	}

	// Advance 10 ticks (refill 2/tick = 20 tokens)
	for i := 0; i < 20; i++ {
		if !rl.Allow(conn, uint64(10)) {
			t.Fatalf("expected token %d after refill to be allowed", i)
		}
	}

	// 21st should be denied (only 20 refilled)
	if rl.Allow(conn, uint64(10)) {
		t.Fatal("expected 21st token after refill to be denied")
	}
}

func TestReset(t *testing.T) {
	rl := NewRateLimiter()
	var conn testKey = 1
	rl.RegisterConnection(conn)

	// Drain
	for i := 0; i < 60; i++ {
		rl.Allow(conn, 0)
	}

	// Reset
	rl.Reset(conn)

	// Should be full again
	for i := 0; i < 60; i++ {
		if !rl.Allow(conn, 0) {
			t.Fatalf("expected token %d after reset to be allowed", i)
		}
	}
}

func TestUnregister(t *testing.T) {
	rl := NewRateLimiter()
	var conn testKey = 1
	rl.RegisterConnection(conn)
	if rl.ConnCount() != 1 {
		t.Fatal("expected 1 connection")
	}
	rl.UnregisterConnection(conn)
	if rl.ConnCount() != 0 {
		t.Fatal("expected 0 connections after unregister")
	}
}

func TestMultipleConnections(t *testing.T) {
	rl := NewRateLimiter()
	var conn1 testKey = 1
	var conn2 testKey = 2

	rl.RegisterConnection(conn1)
	rl.RegisterConnection(conn2)

	// Conn1 drains
	for i := 0; i < 60; i++ {
		rl.Allow(conn1, 0)
	}
	// Conn2 still has tokens
	if !rl.Allow(conn2, 0) {
		t.Fatal("expected conn2 to have independent tokens")
	}
	// Conn1 is dry
	if rl.Allow(conn1, 0) {
		t.Fatal("expected conn1 to be dry")
	}
}

// BenchmarkAllow measures hot-path per-packet performance.
func BenchmarkAllow(b *testing.B) {
	rl := NewRateLimiter()
	var conn testKey = 1
	rl.RegisterConnection(conn)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rl.Allow(conn, uint64(i))
	}
}
