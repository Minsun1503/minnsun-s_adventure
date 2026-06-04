package netio_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"server/peakgo/netio"
	"server/peakgo/pool"
	"sync/atomic"
	"testing"
	"time"
)

// ─── FAST MOCK CONN (Zero-Allocation, Lock-Free) ───────────────────────────
//
// fastMockBenchConn implements a minimal net.Conn that provides pre-loaded data
// for Read operations and discards Write data. Unlike net.Pipe(), this avoids
// all standard library timer/channel allocations, providing a clean micro-benchmark
// surface to measure true application logic.
//
// For reads: it wraps around a fixed buffer, returning data starting from index 0
// on each call (suitable for reading the same packet repeatedly in benchmarks).
// For writes: data is discarded into a black hole.

type fastMockBenchConn struct {
	readBuf []byte
	closed  int32 // atomic
}

func newFastMockBenchConn(readBuf []byte) *fastMockBenchConn {
	return &fastMockBenchConn{
		readBuf: readBuf,
	}
}

func (c *fastMockBenchConn) Read(b []byte) (int, error) {
	if atomic.LoadInt32(&c.closed) != 0 {
		return 0, io.EOF
	}
	n := copy(b, c.readBuf)
	return n, nil
}

func (c *fastMockBenchConn) Write(b []byte) (int, error) {
	if atomic.LoadInt32(&c.closed) != 0 {
		return 0, net.ErrClosed
	}
	// Discard all data (black hole)
	return len(b), nil
}

func (c *fastMockBenchConn) Close() error {
	atomic.StoreInt32(&c.closed, 1)
	return nil
}

func (c *fastMockBenchConn) LocalAddr() net.Addr                { return nil }
func (c *fastMockBenchConn) RemoteAddr() net.Addr               { return nil }
func (c *fastMockBenchConn) SetDeadline(t time.Time) error      { return nil }
func (c *fastMockBenchConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *fastMockBenchConn) SetWriteDeadline(t time.Time) error { return nil }

// ─── UTILITY HELPERS ─────────────────────────────────────────────────────────

// pipeConn thiết lập một cặp kết nối socket đồng bộ chạy trong bộ nhớ RAM (In-process).
// Giải pháp này triệt tiêu hoàn toàn chi phí network stack của hệ điều hành (Kernel),
// đảm bảo Unit Test và Benchmark chỉ đo đạc chính xác hiệu năng của mã nguồn Go.
func pipeConn() (client, server net.Conn) {
	client, server = net.Pipe()
	return
}

// ─── FUNCTIONAL CORRECTNESS TESTS ───────────────────────────────────────────

// TestReadHeaderCorrectness kiểm tra khả năng đọc trường độ dài 2-byte BE từ luồng mạng.
func TestReadHeaderCorrectness(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	go func() {
		// Ghi mã Big-Endian của số 1234 (0x04D2) xuống cổng mạng
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

// TestReadPayloadCorrectness xác thực tính toàn vẹn của dữ liệu payload nhận được.
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

	// Đã sửa: Sử dụng giải pháp bytes.Equal chuẩn mực của Go thay vì lặp thủ công
	got := (*pBuf)[:4]
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadPayload data corruption: got %X, want %X", got, want)
	}
}

// TestReadPayloadEOFReturnsError giả lập tình huống client ngắt kết nối đột ngột trước khi đọc.
func TestReadPayloadEOFReturnsError(t *testing.T) {
	client, server := pipeConn()
	defer server.Close()

	// Đóng socket client ngay lập tức để kích hoạt tín hiệu EOF ngắt luồng
	client.Close()

	p := pool.NewBytesPool(1024)
	_, err := netio.ReadPayload(server, p, 64)
	if err == nil {
		t.Fatal("expected error reading from closed connection, got nil")
	}
}

// TestReadPayloadShortRead bổ sung bài test lỗi phân mảnh mạng theo yêu cầu của kỹ sư.
// Xác thực rằng hệ thống mạng phải báo lỗi nếu client truyền thiếu dữ liệu so với Header đã khai báo.
func TestReadPayloadShortRead(t *testing.T) {
	client, server := pipeConn()
	defer server.Close()

	go func() {
		// Khai báo packet dài 10 bytes nhưng client chỉ ghi 3 bytes rồi đóng kết nối
		_, _ = client.Write([]byte{1, 2, 3})
		client.Close()
	}()

	p := pool.NewBytesPool(1024)
	_, err := netio.ReadPayload(server, p, 10)
	if err == nil {
		t.Fatal("expected short-read error due to truncated TCP stream data buffer")
	}
}

// TestReadPayloadBoundaryGuards kiểm thử hệ thống rào chắn phòng vệ của Netio
// trước các lỗi kích thước gói tin trống hoặc đòn tấn công cố tình spam packet khổng lồ (DoS).
func TestReadPayloadBoundaryGuards(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	p := pool.NewBytesPool(1024)

	// Case 1: Chặn đứng gói tin trống (Length = 0)
	_, errEmpty := netio.ReadPayload(server, p, 0)
	if !errors.Is(errEmpty, netio.ErrEmptyPacket) {
		t.Fatalf("expected ErrEmptyPacket, got %v", errEmpty)
	}

	// Case 2: Chặn đứng gói tin vượt ngưỡng an toàn (Length > MaxPacketSize)
	oversizedLen := uint16(netio.MaxPacketSize + 1)
	_, errOversized := netio.ReadPayload(server, p, oversizedLen)
	if !errors.Is(errOversized, netio.ErrPacketTooLarge) {
		t.Fatalf("expected ErrPacketTooLarge, got %v", errOversized)
	}
}

// TestWritePacketCorrectness xác thực luồng ghi gói tin và cơ chế an toàn dòng chảy dữ liệu.
func TestWritePacketCorrectness(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	data := []byte("hello, world, packet framing message data context payload")
	go func() {
		if err := netio.WritePacket(client, data); err != nil {
			t.Errorf("WritePacket error: %v", err)
		}
	}()

	buf := make([]byte, len(data))
	if _, err := io.ReadFull(server, buf); err != nil {
		t.Fatalf("reading written data: %v", err)
	}
	if !bytes.Equal(buf, data) {
		t.Fatalf("WritePacket failure: got %q want %q", buf, data)
	}
}

// ─── INTEGRITY TEST ─────────────────────────────────────────────────────────
//
// TestNetioReadWritePipeline xác thực toàn bộ pipeline đọc header + payload
// không bị deadlock khi dùng net.Pipe() (synchronous, unbuffered).
// net.Pipe() yêu cầu Write và Read phải chạy đồng thời trên 2 goroutine.

func TestNetioReadWritePipeline(t *testing.T) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	p := pool.NewBytesPool(1024)
	header := []byte{0x00, 0x04}
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	const N = 100

	// Writer goroutine
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for i := 0; i < N; i++ {
			_, _ = client.Write(header)
			_, _ = client.Write(payload)
		}
	}()

	// Reader goroutine (main)
	for i := 0; i < N; i++ {
		lenH, errH := netio.ReadHeader(server)
		if errH != nil {
			t.Fatalf("ReadHeader failed at iteration %d: %v", i, errH)
		}
		if lenH != 4 {
			t.Fatalf("ReadHeader: got %d want 4", lenH)
		}
		pB, errP := netio.ReadPayload(server, p, lenH)
		if errP != nil {
			t.Fatalf("ReadPayload failed at iteration %d: %v", i, errP)
		}
		got := (*pB)[:4]
		if !bytes.Equal(got, payload) {
			t.Fatalf("ReadPayload data corruption at iteration %d: got %X want %X", i, got, payload)
		}
		p.Put(pB)
	}
	<-writerDone
}

// ─── HIGH-FREQUENCY NETWORK MICRO-BENCHMARKS ─────────────────────────────────

func BenchmarkReadHeader(b *testing.B) {
	// Use fastMockBenchConn to eliminate net.Pipe allocations
	header := []byte{0x00, 0x09}
	mock := newFastMockBenchConn(header)
	defer mock.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := netio.ReadHeader(mock)
		if err != nil {
			b.Fatalf("Benchmark ReadHeader failed at loop %d: %v", i, err)
		}
	}
}

func BenchmarkReadPayload(b *testing.B) {
	// Use fastMockBenchConn to eliminate net.Pipe allocations
	payload := make([]byte, 9)
	mock := newFastMockBenchConn(payload)
	defer mock.Close()

	p := pool.NewBytesPool(1024)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pBuf, err := netio.ReadPayload(mock, p, 9)
		if err != nil {
			b.Fatalf("Benchmark ReadPayload failed at loop %d: %v", i, err)
		}
		p.Put(pBuf)
	}
}

// BenchmarkWritePacket bổ sung bài đo kiểm tải hiệu năng ghi gói tin TCP của hệ thống.
func BenchmarkWritePacket(b *testing.B) {
	// Use fastMockBenchConn to eliminate net.Pipe allocations
	var packetData [64]byte
	mock := newFastMockBenchConn(nil)
	defer mock.Close()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := netio.WritePacket(mock, packetData[:])
		if err != nil {
			b.Fatalf("Benchmark WritePacket failed at loop %d: %v", i, err)
		}
	}
}

// BenchmarkRingBufferPeakGo measures local buffer read/write operations simulating
// RingBuffer hot-path without actual network I/O.
func BenchmarkRingBufferPeakGo(b *testing.B) {
	buf := make([]byte, 2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.BigEndian.PutUint16(buf, 0xBEEF)
		_ = binary.BigEndian.Uint16(buf)
	}
}
