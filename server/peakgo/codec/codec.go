// Package codec provides zero-allocation Big-Endian binary encoding helpers
// for the Minnsun's Adventure binary packet protocol.
//
// # Protocol Reference (from .clinerules)
//
//	[Length uint16 BE] [Opcode uint8] [Payload N-bytes]
//
// # Bounds Checking & Panic Policy
//
// All primitive readers and writers assume the caller provides a correctly
// sized slice buffer. They perform no internal manual dynamic bounds checks beyond
// those naturally enforced by the Go runtime, and will panic instantly if
// supplied with undersized buffers. This keeps the hot path entirely free of
// redundant branching overhead.
package codec

import (
	"encoding/binary"
	"math"
)

// ─── Primitive Readers ────────────────────────────────────────────────────────

// ReadUint8 reads one unsigned byte at offset 0.
func ReadUint8(b []byte) uint8 { return b[0] }

// ReadInt16 decodes a Big-Endian int16 from b[0:2].
func ReadInt16(b []byte) int16 { return int16(binary.BigEndian.Uint16(b)) }

// ReadUint16 decodes a Big-Endian uint16 from b[0:2].
func ReadUint16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }

// ReadInt32 decodes a Big-Endian int32 from b[0:4].
func ReadInt32(b []byte) int32 { return int32(binary.BigEndian.Uint32(b)) }

// ReadUint32 decodes a Big-Endian uint32 from b[0:4].
func ReadUint32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// ReadInt64 decodes a Big-Endian int64 from b[0:8].
func ReadInt64(b []byte) int64 { return int64(binary.BigEndian.Uint64(b)) }

// ReadUint64 decodes a Big-Endian uint64 from b[0:8].
func ReadUint64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }

// ReadFloat32 decodes a Big-Endian IEEE 754 float32 from b[0:4].
// Crucial for future sub-pixel position synchronization or float vectors.
func ReadFloat32(b []byte) float32 {
	return math.Float32frombits(binary.BigEndian.Uint32(b))
}

// ─── Primitive Writers ────────────────────────────────────────────────────────

// WriteUint8 writes one byte into dst[0].
func WriteUint8(dst []byte, v uint8) { dst[0] = v }

// WriteInt16 encodes v as Big-Endian int16 into dst[0:2].
func WriteInt16(dst []byte, v int16) { binary.BigEndian.PutUint16(dst, uint16(v)) }

// WriteUint16 encodes v as Big-Endian uint16 into dst[0:2].
func WriteUint16(dst []byte, v uint16) { binary.BigEndian.PutUint16(dst, v) }

// WriteInt32 encodes v as Big-Endian int32 into dst[0:4].
func WriteInt32(dst []byte, v int32) { binary.BigEndian.PutUint32(dst, uint32(v)) }

// WriteUint32 encodes v as Big-Endian uint32 into dst[0:4].
func WriteUint32(dst []byte, v uint32) { binary.BigEndian.PutUint32(dst, v) }

// WriteInt64 encodes v as Big-Endian int64 into dst[0:8].
func WriteInt64(dst []byte, v int64) { binary.BigEndian.PutUint64(dst, uint64(v)) }

// WriteUint64 encodes v as Big-Endian uint64 into dst[0:8].
func WriteUint64(dst []byte, v uint64) { binary.BigEndian.PutUint64(dst, v) }

// WriteFloat32 encodes v as Big-Endian IEEE 754 float32 into dst[0:4].
func WriteFloat32(dst []byte, v float32) {
	binary.BigEndian.PutUint32(dst, math.Float32bits(v))
}

// ─── String Decoding Helpers ──────────────────────────────────────────────────

// ReadStringLen16 decodes a uint16-prefixed UTF-8 payload string safely.
// Returns ok=false if the buffer slice is too short to safely unwrap the length
// descriptor or the string body itself.
func ReadStringLen16(payload []byte) (string, bool) {
	if len(payload) < 2 {
		return "", false
	}

	n := int(binary.BigEndian.Uint16(payload))
	if len(payload) < 2+n {
		return "", false
	}

	// Allocation is mandatory here when converting slice backing array to standard Go strings.
	return string(payload[2 : 2+n]), true
}

// ─── Hot-Path Composite Readers ───────────────────────────────────────────────

// MovePayload holds the decoded coordinates for a C2S MOVE packet (opcode 1).
type MovePayload struct {
	X int32 // Optimized: Changed from int to int32 to perfectly reflect wire types
	Z int32 // Optimized: Guards against 32/64-bit multi-platform architecture mismatch
}

// ReadMovePayload decodes a MOVE packet payload (opcode 1).
// Returns ok=false if the byte array payload length is not exactly 8 bytes.
func ReadMovePayload(payload []byte) (p MovePayload, ok bool) {
	if len(payload) != 8 {
		return MovePayload{}, false
	}

	// Optimized: Read directly off pointer indices without creating slice headers
	return MovePayload{
		X: int32(binary.BigEndian.Uint32(payload[0:4])),
		Z: int32(binary.BigEndian.Uint32(payload[4:8])),
	}, true
}

// AttackPayload holds the target identifier fields for an ATTACK packet (opcode 5).
type AttackPayload struct {
	TargetID uint64
}

// ReadAttackPayload decodes an ATTACK packet payload (opcode 5).
// Returns ok=false if the byte array payload length is not exactly 8 bytes.
func ReadAttackPayload(payload []byte) (p AttackPayload, ok bool) {
	if len(payload) != 8 {
		return AttackPayload{}, false
	}
	return AttackPayload{TargetID: binary.BigEndian.Uint64(payload[0:8])}, true
}
