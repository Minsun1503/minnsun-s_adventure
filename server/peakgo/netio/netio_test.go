package netio_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"server/peakgo/netio"
	"server/peakgo/pool"
	"testing"
)

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
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	header := []byte{0x00, 0x09} // Length mặc định bằng 9
	done := make(chan struct{})

	b.ReportAllocs()
	b.ResetTimer()

	// Đã sửa: Sử dụng channel 'done' kiểm soát vòng đời Goroutine ghi dữ liệu, chống rò rỉ nền
	go func() {
		defer close(done)
		for i := 0; i < b.N; i++ {
			_, _ = client.Write(header)
		}
	}()

	for i := 0; i < b.N; i++ {
		_, err := netio.ReadHeader(server)
		// Đã sửa: Dừng tiến trình và báo lỗi ngay lập tức nếu Benchmark dính lỗi nghẽn mạch mạng
		if err != nil {
			b.Fatalf("Benchmark ReadHeader failed at loop %d: %v", i, err)
		}
	}

	<-done // Đợi Goroutine ghi kết thúc sạch sẽ trước khi chuyển bài test
}

func BenchmarkReadPayload(b *testing.B) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	payload := make([]byte, 9)
	p := pool.NewBytesPool(1024)
	done := make(chan struct{})

	b.ReportAllocs()
	b.ResetTimer()

	go func() {
		defer close(done)
		for i := 0; i < b.N; i++ {
			_, _ = client.Write(payload)
		}
	}()

	for i := 0; i < b.N; i++ {
		pBuf, err := netio.ReadPayload(server, p, 9)
		if err != nil {
			b.Fatalf("Benchmark ReadPayload failed at loop %d: %v", i, err)
		}
		p.Put(pBuf)
	}

	<-done
}

// BenchmarkWritePacket bổ sung bài đo kiểm tải hiệu năng ghi gói tin TCP của hệ thống.
func BenchmarkWritePacket(b *testing.B) {
	client, server := pipeConn()
	defer client.Close()
	defer server.Close()

	packetData := make([]byte, 64)
	sinkBuf := make([]byte, 64)
	done := make(chan struct{})

	b.ReportAllocs()
	b.ResetTimer()

	// Kích hoạt Goroutine ngầm liên tục dọn sạch hàng đợi socket (Flush) để giải phóng hàng ghi
	go func() {
		defer close(done)
		for i := 0; i < b.N; i++ {
			_, err := io.ReadFull(server, sinkBuf)
			if err != nil {
				return
			}
		}
	}()

	for i := 0; i < b.N; i++ {
		err := netio.WritePacket(client, packetData)
		if err != nil {
			b.Fatalf("Benchmark WritePacket failed at loop %d: %v", i, err)
		}
	}

	<-done
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
