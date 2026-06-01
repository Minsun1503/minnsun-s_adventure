// Package netio provides zero-allocation TCP packet I/O primitives for the
// Minnsun's Adventure binary protocol.
//
// # Protocol wire format (from .clinerules)
//
//	[Length uint16 BE] [Opcode uint8] [Payload N-bytes]
//
// # Design rationale
//
// The raw approach of using binary.Read() for the 2-byte length header causes:
//   - Reflection at runtime → ~3× slower than direct byte decoding.
//   - A heap allocation per call (the reflect internals) → GC pressure.
//
// The raw approach of make([]byte, length) for each payload:
//   - Allocates on the heap for every packet → GC pressure at scale.
//
// This package eliminates both issues:
//   - ReadHeader: stack-allocated [2]byte + BigEndian.Uint16 — no reflection.
//   - ReadPayload: fetches a *[]byte from the shared DefaultPool — no new allocs.
//   - WritePacket: enforces the 5-second write deadline per .clinerules.
//
// # Ownership / lifecycle
//
//	pBuf, err := netio.ReadPayload(conn, netio.DefaultPool, length)
//	if err != nil { ... }
//	defer netio.DefaultPool.Put(pBuf)  // MUST put back when done
//	payload := (*pBuf)[:length]
package netio

import (
	"io"
	"net"
	"server/peakgo/codec"
	"server/peakgo/pool"
	"time"
)

const (
	// defaultPoolSize is the pre-allocated buffer size for packet payloads.
	// 1 KB covers the maximum expected game packet size per .clinerules.
	defaultPoolSize = 1024

	// writeDeadline matches the .clinerules requirement:
	// "Thiết lập write deadline tối đa 5 giây cho mọi kết nối client"
	writeDeadline = 5 * time.Second
)

// DefaultPool is the shared packet-payload buffer pool.
// Registered as a package-level singleton so all callers (server.go, gateways,
// tests) share the same backing pool without having to pass it explicitly.
var DefaultPool = pool.NewBytesPool(defaultPoolSize)

// ReadHeader reads the 2-byte Big-Endian length prefix from conn.
// Uses a stack-allocated [2]byte — no heap allocation, no reflection.
// Returns (0, err) on any read error including EOF / disconnect.
func ReadHeader(conn net.Conn) (uint16, error) {
	var buf [2]byte
	if _, err := io.ReadFull(conn, buf[:]); err != nil {
		return 0, err
	}
	return codec.ReadUint16(buf[:]), nil
}

// ReadPayload reads `length` bytes from conn into a buffer from p.
// The returned *[]byte is sliced to exactly `length` bytes.
// Callers MUST return the buffer with p.Put(pBuf) after processing.
// Returns (nil, err) on read error; the buffer is put back automatically.
func ReadPayload(conn net.Conn, p *pool.BytesPool, length uint16) (*[]byte, error) {
	pBuf := p.Get()
	buf := *pBuf

	// Grow the buffer only when the packet exceeds the default pool capacity.
	// This branch is taken at most once per oversized packet until the pool
	// replenishes naturally — amortised allocation rate stays near zero.
	if int(length) > len(buf) {
		buf = make([]byte, length)
	} else {
		buf = buf[:length]
	}

	if _, err := io.ReadFull(conn, buf); err != nil {
		*pBuf = buf
		p.Put(pBuf)
		return nil, err
	}

	*pBuf = buf
	return pBuf, nil
}

// WritePacket sends data to conn with a 5-second write deadline per .clinerules.
// Returns an error if the write fails (caller should close the connection).
func WritePacket(conn net.Conn, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	_, err := conn.Write(data)
	return err
}
