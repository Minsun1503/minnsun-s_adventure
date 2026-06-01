package netio_test

import (
	"io"
	"net"
	"server/peakgo/netio"
	"server/peakgo/pool"
	"testing"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// pipeConn returns a synchronous in-process connection pair (no OS round-trip).
func pipeConn() (client, server net.Conn) {
	client, server = net.Pipe()
	return
}

// ─── Correctness ─────────────────────────────────────────────────────────────

func TestReadHeaderCorrectness(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	go func() {
		// Write big-endian 0x04D2 (1234) as the length header.
		_, _ = client.Write([]byte{0x04, 0xD2})
	}()

	length, err := netio.ReadHeader(server)
	if err != nil {
		t.Fatalf("ReadHeader error: %v", err)
	}
	if length != 1234 {
		t.Fatalf("ReadHeader: got %d want 1234", length)
	}
}

func TestReadPayloadCorrectness(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	want := []byte{0x01, 0xAB, 0xCD, 0xEF}
	go func() {
		_, _ = client.Write(want)
	}()

	p := pool.NewBytesPool(1024)
	pBuf, err := netio.ReadPayload(server, p, 4)
	if err != nil {
		t.Fatalf("ReadPayload error: %v", err)
	}
	defer p.Put(pBuf)

	got := (*pBuf)[:4]
	for i, b := range want {
		if got[i] != b {
			t.Fatalf("ReadPayload byte %d: got %X want %X", i, got[i], b)
		}
	}
}

func TestReadPayloadEOFReturnsError(t *testing.T) {
	client, server := pipeConn()
	defer server.Close()

	// Close client immediately to simulate disconnect mid-read.
	client.Close()

	p := pool.NewBytesPool(1024)
	_, err := netio.ReadPayload(server, p, 64)
	if err == nil {
		t.Fatal("expected error reading from closed connection, got nil")
	}
}

func TestWritePacket(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	data := []byte("hello, world")
	go func() {
		if err := netio.WritePacket(client, data); err != nil {
			t.Errorf("WritePacket error: %v", err)
		}
	}()

	buf := make([]byte, len(data))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatalf("reading written data: %v", err)
	}
	if string(buf) != string(data) {
		t.Fatalf("WritePacket: got %q want %q", buf, data)
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func BenchmarkReadHeader(b *testing.B) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	header := []byte{0x00, 0x09} // length = 9

	b.ReportAllocs()
	b.ResetTimer()

	go func() {
		for i := 0; i < b.N; i++ {
			_, _ = client.Write(header)
		}
	}()
	for i := 0; i < b.N; i++ {
		_, _ = netio.ReadHeader(server)
	}
}

func BenchmarkReadPayload(b *testing.B) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	payload := make([]byte, 9)
	p := pool.NewBytesPool(1024)

	b.ReportAllocs()
	b.ResetTimer()

	go func() {
		for i := 0; i < b.N; i++ {
			_, _ = client.Write(payload)
		}
	}()
	for i := 0; i < b.N; i++ {
		pBuf, err := netio.ReadPayload(server, p, 9)
		if err != nil {
			break
		}
		p.Put(pBuf)
	}
}
