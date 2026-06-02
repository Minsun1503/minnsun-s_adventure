// Package netio provides zero-allocation, production-safe TCP packet I/O primitives
// for the Minnsun's Adventure binary network protocol.
//
// # Protocol Wire Format (from .clinerules)
//
//	[Length uint16 BE] [Opcode uint8] [Payload N-bytes]
//
// # Performance & Security Architecture
//
// This package eliminates the heavy overhead of standard binary.Read operations by
// replacing runtime reflection with direct byte decoding via stack-allocated buffers.
//
// To protect the server against Denial of Service (DoS) attacks and memory pollution,
// strict packet length validation is enforced at the boundary layer. Memory footprint
// is kept near zero using an amortized byte buffer pool.
package netio

import (
	"errors"
	"io"
	"net"
	"server/peakgo/codec"
	"server/peakgo/pool"
	"time"
)

// ─── Network Protocol Constraints ────────────────────────────────────────────

const (
	// defaultPoolSize quy định kích thước bộ đệm mặc định được cấp phát sẵn cho Pool.
	// 1 KB đáp ứng trọn vẹn hầu hết mọi gói tin vận hành thông thường của game.
	defaultPoolSize = 1024

	// MaxPacketSize là hàng rào phòng ngự tối cao chống DoS (4 KB).
	// Bất kỳ gói tin nào khai báo độ dài vượt quá mốc này sẽ bị ngắt kết nối ngay lập tức.
	MaxPacketSize = 4096

	// writeDeadline xác lập thời hạn tối đa 5 giây cho việc đẩy dữ liệu xuống mạng,
	// ngăn chặn tình trạng hụt tài nguyên thread do client bị treo socket half-open.
	writeDeadline = 5 * time.Second
)

// ─── Core Network Errors ──────────────────────────────────────────────────────

var (
	ErrEmptyPacket    = errors.New("netio: inbound packet length cannot be zero")
	ErrPacketTooLarge = errors.New("netio: inbound packet length exceeds maximum safety limit")
)

// DefaultPool serves as the global engine-wide singleton packet buffer pool.
var DefaultPool = pool.NewBytesPool(defaultPoolSize)

// ─── Packet Input Operations (Read Path) ──────────────────────────────────────

// ReadHeader reads the 2-byte Big-Endian length prefix from the connection.
// Utilizes a strict stack-allocated array to achieve 0 heap allocations and bypass reflection.
func ReadHeader(conn net.Conn) (uint16, error) {
	var buf [2]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return 0, err
	}
	return codec.ReadUint16(buf[:]), nil
}

// ReadPayload fetches a managed buffer from the pool and reads exactly `length` bytes into it.
//
// Callers MUST return the acquired pointer container back to the architecture pool
// using 'defer p.Put(pBuf)' immediately after the business packet has been processed.
func ReadPayload(conn net.Conn, p *pool.BytesPool, length uint16) (*[]byte, error) {
	// Hàng rào bảo vệ 1: Chặn gói tin rác có kích thước trống
	if length == 0 {
		return nil, ErrEmptyPacket
	}

	// Hàng rào bảo vệ 2: Chặn đòn tấn công cố tình spam gói tin khổng lồ gây cạn kiệt RAM
	if length > MaxPacketSize {
		return nil, ErrPacketTooLarge
	}

	pBuf := p.Get()
	buf := *pBuf

	if int(length) > len(buf) {
		// Gói tin lớn hơn dung lượng mặc định (1KB) nhưng nằm trong ngưỡng an toàn (<4KB).
		// Cấp phát một mảng thô độc lập trên Heap để xử lý riêng lẻ.
		// Lưu ý: Để tránh nhiễm bẩn Pool (Pool Pollution) bởi các mảng kích thước lớn,
		// cấu trúc BytesPool.Put nội bộ của hệ thống peakgo cần có cơ chế tự động từ chối
		// hoặc gọt slice về lại mức defaultPoolSize trước khi tái sử dụng.
		buf = make([]byte, length)
	} else {
		buf = buf[:length]
	}

	// Đọc bắt buộc trọn vẹn byte dữ liệu qua ReadFull, chống lỗi phân mảnh luồng TCP
	if _, err := io.ReadFull(conn, buf); err != nil {
		*pBuf = buf
		p.Put(pBuf) // Hoàn trả bộ đệm an toàn nếu xảy ra sự cố sập mạng giữa chừng
		return nil, err
	}

	*pBuf = buf
	return pBuf, nil
}

// ─── Packet Output Operations (Write Path) ────────────────────────────────────

// WritePacket transmits a structured binary frame with a strict 5-second write deadline.
//
// Đã sửa: Tích hợp vòng lặp kiểm soát Partial Writes để đảm bảo dữ liệu luôn được ghi
// đầy đủ xuống Network Card Buffer, và tự động clear deadline sau khi hoàn tất tác vụ.
func WritePacket(conn net.Conn, data []byte) error {
	// Thiết lập mốc giới hạn xả băng thông bảo vệ tài nguyên thread
	_ = conn.SetWriteDeadline(time.Now().Add(writeDeadline))

	// Giải phóng hoàn toàn Write Deadline sau khi thoát hàm để đưa kết nối về trạng thái tự do
	defer func() {
		_ = conn.SetWriteDeadline(time.Time{})
	}()

	// Vòng lặp bảo vệ: Tiếp tục đẩy phần dữ liệu còn sót lại cho tới khi xả sạch mảng byte
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err // Trả lỗi ngay lập tức để caller chủ động đóng connection bị gãy
		}
		data = data[n:] // Gọt bớt phần mảng dữ liệu đã gửi thành công
	}

	return nil
}
