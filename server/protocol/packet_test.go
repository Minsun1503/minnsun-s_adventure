package protocol

import (
	"encoding/binary"
	"io"
	"sync"
	"testing"
)

type staticReader struct {
	data []byte
	off  int
}

func (r *staticReader) Read(p []byte) (int, error) {
	n := copy(p, r.data[r.off:])
	r.off += n
	if r.off >= len(r.data) {
		r.off = 0
	}
	return n, nil
}

func BenchmarkReadLengthBaseline(b *testing.B) {
	var length uint16 = 512
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, length)
	r := &staticReader{data: buf}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var l uint16
		_ = binary.Read(r, binary.BigEndian, &l)
	}
}

func BenchmarkReadLengthOptimized(b *testing.B) {
	var length uint16 = 512
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, length)
	r := &staticReader{data: buf}

	b.ResetTimer()
	var lenBuf [2]byte
	for i := 0; i < b.N; i++ {
		_, _ = io.ReadFull(r, lenBuf[:])
		_ = binary.BigEndian.Uint16(lenBuf[:])
	}
}

func BenchmarkPayloadReadBaseline(b *testing.B) {
	length := 512
	payload := make([]byte, length)
	r := &staticReader{data: payload}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packetBytes := make([]byte, length)
		_, _ = io.ReadFull(r, packetBytes)
	}
}

var packetBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 1024)
		return &b
	},
}

func BenchmarkPayloadReadOptimized(b *testing.B) {
	length := 512
	payload := make([]byte, length)
	r := &staticReader{data: payload}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pBuf := packetBufferPool.Get().(*[]byte)
		buf := *pBuf
		if len(buf) < length {
			buf = make([]byte, length)
		} else {
			buf = buf[:length]
		}
		_, _ = io.ReadFull(r, buf)
		*pBuf = buf
		packetBufferPool.Put(pBuf)
	}
}
