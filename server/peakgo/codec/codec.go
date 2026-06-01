// Package codec provides zero-allocation Big-Endian binary encoding helpers
// for the Minnsun's Adventure binary packet protocol.
//
// # Protocol reference (from .clinerules)
//
//	[Length uint16 BE] [Opcode uint8] [Payload N-bytes]
//
// # Design rationale
//
// Inline use of encoding/binary.BigEndian is verbose, requires manual offset
// arithmetic, and is easy to get wrong (wrong offset, wrong size). This package
// centralises every read/write operation in named functions so callers only
// think in terms of "what field am I reading", not "which byte offset is this".
//
// # Hot-path composites
//
// ReadMovePayload and ReadAttackPayload are provided because MOVE (opcode 1) and
// ATTACK (opcode 5) are the two highest-frequency C2S packets. Pre-building
// typed readers for them avoids repeated slice indexing in handleBinaryPacket.
//
// All functions operate on []byte slices already held in caller memory:
// no allocations, no copies.
package codec

import "encoding/binary"

// ─── Primitive readers ────────────────────────────────────────────────────────

// ReadUint8 reads one unsigned byte at offset 0.
// Panics if b is empty (same contract as b[0]).
func ReadUint8(b []byte) uint8 { return b[0] }

// ReadUint16 decodes a Big-Endian uint16 from b[0:2].
// Panics if len(b) < 2.
func ReadUint16(b []byte) uint16 { return binary.BigEndian.Uint16(b) }

// ReadInt32 decodes a Big-Endian int32 from b[0:4].
// Panics if len(b) < 4.
func ReadInt32(b []byte) int32 { return int32(binary.BigEndian.Uint32(b)) }

// ReadUint32 decodes a Big-Endian uint32 from b[0:4].
// Panics if len(b) < 4.
func ReadUint32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// ReadUint64 decodes a Big-Endian uint64 from b[0:8].
// Panics if len(b) < 8.
func ReadUint64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }

// ReadInt64 decodes a Big-Endian int64 from b[0:8].
// Panics if len(b) < 8.
func ReadInt64(b []byte) int64 { return int64(binary.BigEndian.Uint64(b)) }

// ─── Primitive writers ────────────────────────────────────────────────────────

// WriteUint8 writes one byte into dst[0].
// Panics if dst is empty.
func WriteUint8(dst []byte, v uint8) { dst[0] = v }

// WriteUint16 encodes v as Big-Endian uint16 into dst[0:2].
// Panics if len(dst) < 2.
func WriteUint16(dst []byte, v uint16) { binary.BigEndian.PutUint16(dst, v) }

// WriteInt32 encodes v as Big-Endian int32 into dst[0:4].
// Panics if len(dst) < 4.
func WriteInt32(dst []byte, v int32) { binary.BigEndian.PutUint32(dst, uint32(v)) }

// WriteUint32 encodes v as Big-Endian uint32 into dst[0:4].
// Panics if len(dst) < 4.
func WriteUint32(dst []byte, v uint32) { binary.BigEndian.PutUint32(dst, v) }

// WriteUint64 encodes v as Big-Endian uint64 into dst[0:8].
// Panics if len(dst) < 8.
func WriteUint64(dst []byte, v uint64) { binary.BigEndian.PutUint64(dst, v) }

// WriteInt64 encodes v as Big-Endian int64 into dst[0:8].
// Panics if len(dst) < 8.
func WriteInt64(dst []byte, v int64) { binary.BigEndian.PutUint64(dst, uint64(v)) }

// ─── Hot-path composite readers ───────────────────────────────────────────────

// MovePayload holds the decoded fields for a MOVE packet (opcode 1).
//
// Wire layout: [X int32 BE][Z int32 BE]  (8 bytes total)
type MovePayload struct {
	X int
	Z int
}

// ReadMovePayload decodes a MOVE packet payload (opcode 1).
// Returns ok=false if payload is not exactly 8 bytes.
func ReadMovePayload(payload []byte) (p MovePayload, ok bool) {
	if len(payload) != 8 {
		return MovePayload{}, false
	}
	return MovePayload{
		X: int(ReadInt32(payload[0:4])),
		Z: int(ReadInt32(payload[4:8])),
	}, true
}

// AttackPayload holds the decoded fields for an ATTACK packet (opcode 5).
//
// Wire layout: [TargetID uint64 BE]  (8 bytes total)
type AttackPayload struct {
	TargetID uint64
}

// ReadAttackPayload decodes an ATTACK packet payload (opcode 5).
// Returns ok=false if payload is not exactly 8 bytes.
func ReadAttackPayload(payload []byte) (p AttackPayload, ok bool) {
	if len(payload) != 8 {
		return AttackPayload{}, false
	}
	return AttackPayload{TargetID: ReadUint64(payload[0:8])}, true
}
