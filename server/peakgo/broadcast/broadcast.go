// Package broadcast provides zero-allocation TCP packet framing helpers and
// typed S2C (Server-to-Client) packet builders for the Minnsun's Adventure
// binary protocol.
//
// # Wire format
//
//	[Length uint16 BE] [Opcode uint8] [Payload N-bytes]
//
// Length is the byte count of everything after the 2-byte length prefix
// (i.e. opcode + payload), NOT the total packet size.
//
// # Why this package exists
//
// Packet construction is currently scattered across server.go and
// protocol/error_packet.go with three different inline patterns:
//
//  1. Raw conn.Write([]byte("text message"))  — no framing at all
//  2. SendErrorPacket — manual binary.BigEndian arithmetic
//  3. SendSuccessPacket — same manual arithmetic, different opcode
//
// When S2C structured opcodes are added (SpawnEntity, PositionSync,
// StatsSync, InventorySync, ChatMessage) every new packet type would
// repeat the same boilerplate. This package provides:
//
//   - Frame(opcode, payload): the generic framing primitive
//   - FrameText(opcode, text): convenience wrapper for UTF-8 text payloads
//   - Typed builders for current and future hot-path S2C opcodes
//
// # Pool policy
//
// Frame() and FrameText() allocate a new []byte each call because packets
// are immediately handed to conn.Write and then discarded — pooling the
// output buffer would require callers to return it, adding lifecycle
// complexity for no measurable gain (the allocation is one-per-packet,
// not per-field).
//
// Builders that fill a caller-supplied dst []byte are provided for
// hot-path cases where the caller can manage the buffer lifetime.
//
// # Peak Go contract
//
//	Frame(opcode, payload) → 1 alloc/op (the output packet slice)
//	FrameText(opcode, text) → 1 alloc/op
//	FrameInto(dst, opcode, payload) → 0 allocs/op (caller owns buffer)
package broadcast

import (
	"encoding/binary"
	"server/peakgo/codec"
)

// ─── Generic framing ─────────────────────────────────────────────────────────

// Frame constructs a framed binary packet from an opcode and raw payload bytes.
//
// Output layout: [Length uint16 BE][Opcode uint8][Payload...]
// Length field = 1 (opcode) + len(payload).
//
// Allocates and returns a new []byte owned by the caller.
func Frame(opcode byte, payload []byte) []byte {
	payloadLen := len(payload)
	totalLen := 2 + 1 + payloadLen // length-prefix + opcode + payload
	pkt := make([]byte, totalLen)

	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen)) // length field
	pkt[2] = opcode
	if payloadLen > 0 {
		copy(pkt[3:], payload)
	}
	return pkt
}

// FrameText constructs a framed packet whose payload is a raw UTF-8 string.
// Equivalent to Frame(opcode, []byte(text)) but avoids a separate conversion.
func FrameText(opcode byte, text string) []byte {
	return Frame(opcode, []byte(text))
}

// FrameInto writes a framed packet into dst, growing it if necessary.
// Returns the slice resliced to exactly the packet length.
// 0 allocs when len(dst) >= 3+len(payload).
//
// Use for hot-path broadcast where the caller manages a pooled buffer:
//
//	buf := make([]byte, 0, 1024)
//	buf = broadcast.FrameInto(buf, opcode, payload)
//	conn.Write(buf)
func FrameInto(dst []byte, opcode byte, payload []byte) []byte {
	needed := 2 + 1 + len(payload)
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+len(payload)))
	dst[2] = opcode
	if len(payload) > 0 {
		copy(dst[3:], payload)
	}
	return dst
}

// ─── S2C typed packet builders ────────────────────────────────────────────────
//
// Convention: BuildXxx returns a ready-to-send []byte.
//             WriteXxx fills a caller-supplied []byte (0 allocs).

// BuildError constructs an S2C error packet (opcode 0xFF).
//
// Payload layout: [ErrorCode uint16 BE][MessageLen uint16 BE][Message UTF-8]
//
// This centralises the pattern from protocol/error_packet.go so all error
// packets are built identically regardless of call site.
func BuildError(errorCode uint16, message string) []byte {
	msgBytes := []byte(message)
	// payload = errorCode(2) + msgLen(2) + msg
	payload := make([]byte, 2+2+len(msgBytes))
	codec.WriteUint16(payload[0:2], errorCode)
	codec.WriteUint16(payload[2:4], uint16(len(msgBytes)))
	copy(payload[4:], msgBytes)
	return Frame(0xFF, payload)
}

// BuildSuccess constructs an S2C success packet (opcode 0x01).
//
// Payload layout: [MessageLen uint16 BE][Message UTF-8]
func BuildSuccess(message string) []byte {
	msgBytes := []byte(message)
	payload := make([]byte, 2+len(msgBytes))
	codec.WriteUint16(payload[0:2], uint16(len(msgBytes)))
	copy(payload[2:], msgBytes)
	return Frame(0x01, payload)
}

// ─── Future S2C hot-path builders (stub layout, ready to fill) ───────────────

// SpawnPayload holds the fields for an S2C SpawnEntity packet (opcode 0x10).
// Sent when a new entity enters a player's view radius.
type SpawnPayload struct {
	EntityID uint64
	Type     uint8 // 0=player, 1=monster, 2=ground_item
	MapID    int32
	X        int32
	Z        int32
	NameLen  uint8
	Name     string
}

// BuildSpawnEntity constructs an S2C SpawnEntity packet (opcode 0x10).
//
// Payload layout:
//
//	[EntityID uint64 BE][Type uint8][MapID int32 BE][X int32 BE][Z int32 BE]
//	[NameLen uint8][Name UTF-8]
func BuildSpawnEntity(p SpawnPayload) []byte {
	nameBytes := []byte(p.Name)
	// 8 + 1 + 4 + 4 + 4 + 1 + len(name)
	payload := make([]byte, 22+len(nameBytes))
	codec.WriteUint64(payload[0:8], p.EntityID)
	codec.WriteUint8(payload[8:9], p.Type)
	codec.WriteInt32(payload[9:13], p.MapID)
	codec.WriteInt32(payload[13:17], p.X)
	codec.WriteInt32(payload[17:21], p.Z)
	codec.WriteUint8(payload[21:22], uint8(len(nameBytes)))
	copy(payload[22:], nameBytes)
	return Frame(0x10, payload)
}

// DespawnPayload holds the fields for an S2C DespawnEntity packet (opcode 0x11).
type DespawnPayload struct {
	EntityID uint64
}

// BuildDespawnEntity constructs an S2C DespawnEntity packet (opcode 0x11).
//
// Payload layout: [EntityID uint64 BE]
func BuildDespawnEntity(p DespawnPayload) []byte {
	payload := make([]byte, 8)
	codec.WriteUint64(payload[0:8], p.EntityID)
	return Frame(0x11, payload)
}

// PositionSyncPayload holds the fields for an S2C PositionSync packet (opcode 0x12).
type PositionSyncPayload struct {
	EntityID uint64
	X        int32
	Z        int32
}

// BuildPositionSync constructs an S2C PositionSync packet (opcode 0x12).
//
// Payload layout: [EntityID uint64 BE][X int32 BE][Z int32 BE]
func BuildPositionSync(p PositionSyncPayload) []byte {
	payload := make([]byte, 16)
	codec.WriteUint64(payload[0:8], p.EntityID)
	codec.WriteInt32(payload[8:12], p.X)
	codec.WriteInt32(payload[12:16], p.Z)
	return Frame(0x12, payload)
}

// StatsSyncPayload holds the fields for an S2C StatsSync packet (opcode 0x13).
type StatsSyncPayload struct {
	EntityID uint64
	HP       int32
	MaxHP    int32
	MP       int32
	MaxMP    int32
	Dam      int32
	Level    int32
}

// BuildStatsSync constructs an S2C StatsSync packet (opcode 0x13).
//
// Payload layout:
//
//	[EntityID uint64 BE][HP int32 BE][MaxHP int32 BE]
//	[MP int32 BE][MaxMP int32 BE][Dam int32 BE][Level int32 BE]
func BuildStatsSync(p StatsSyncPayload) []byte {
	payload := make([]byte, 32) // 8 + 6*4
	codec.WriteUint64(payload[0:8], p.EntityID)
	codec.WriteInt32(payload[8:12], p.HP)
	codec.WriteInt32(payload[12:16], p.MaxHP)
	codec.WriteInt32(payload[16:20], p.MP)
	codec.WriteInt32(payload[20:24], p.MaxMP)
	codec.WriteInt32(payload[24:28], p.Dam)
	codec.WriteInt32(payload[28:32], p.Level)
	return Frame(0x13, payload)
}

// ChatPayload holds the fields for an S2C ChatMessage packet (opcode 0x15).
type ChatPayload struct {
	Channel    uint8 // 0=map, 1=party, 2=global
	SenderName string
	Message    string
}

// BuildChatMessage constructs an S2C ChatMessage packet (opcode 0x15).
//
// Payload layout:
//
//	[Channel uint8][SenderLen uint8][Sender UTF-8][MsgLen uint16 BE][Message UTF-8]
func BuildChatMessage(p ChatPayload) []byte {
	senderBytes := []byte(p.SenderName)
	msgBytes := []byte(p.Message)
	// 1 + 1 + len(sender) + 2 + len(msg)
	payload := make([]byte, 4+len(senderBytes)+len(msgBytes))
	offset := 0
	codec.WriteUint8(payload[offset:offset+1], p.Channel)
	offset++
	codec.WriteUint8(payload[offset:offset+1], uint8(len(senderBytes)))
	offset++
	copy(payload[offset:], senderBytes)
	offset += len(senderBytes)
	codec.WriteUint16(payload[offset:offset+2], uint16(len(msgBytes)))
	offset += 2
	copy(payload[offset:], msgBytes)
	return Frame(0x15, payload)
}

// WriteError ghi một S2C error packet vào dst (opcode 0xFF).
func WriteError(dst []byte, errorCode uint16, message string) []byte {
	msgLen := len(message)
	payloadLen := 2 + 2 + msgLen // errorCode(2) + msgLen(2) + msg
	needed := 2 + 1 + payloadLen // length(2) + opcode(1) + payload

	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = 0xFF
	codec.WriteUint16(dst[3:5], errorCode)
	codec.WriteUint16(dst[5:7], uint16(msgLen))
	copy(dst[7:], message) // 0 allocs khi copy trực tiếp từ string
	return dst
}

// WriteSuccess ghi một S2C success packet vào dst (opcode 0x01).
func WriteSuccess(dst []byte, message string) []byte {
	msgLen := len(message)
	payloadLen := 2 + msgLen // msgLen(2) + msg
	needed := 2 + 1 + payloadLen

	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = 0x01
	codec.WriteUint16(dst[3:5], uint16(msgLen))
	copy(dst[5:], message)
	return dst
}

// WriteSpawnEntity ghi một S2C SpawnEntity packet vào dst (opcode 0x10).
func WriteSpawnEntity(dst []byte, p SpawnPayload) []byte {
	nameLen := len(p.Name)
	payloadLen := 22 + nameLen // 8 + 1 + 4 + 4 + 4 + 1 + len
	needed := 2 + 1 + payloadLen

	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = 0x10
	codec.WriteUint64(dst[3:11], p.EntityID)
	codec.WriteUint8(dst[11:12], p.Type)
	codec.WriteInt32(dst[12:16], p.MapID)
	codec.WriteInt32(dst[16:20], p.X)
	codec.WriteInt32(dst[20:24], p.Z)
	codec.WriteUint8(dst[24:25], uint8(nameLen))
	copy(dst[25:], p.Name)
	return dst
}

// WriteDespawnEntity ghi một S2C DespawnEntity packet vào dst (opcode 0x11).
func WriteDespawnEntity(dst []byte, p DespawnPayload) []byte {
	needed := 2 + 1 + 8
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+8))
	dst[2] = 0x11
	codec.WriteUint64(dst[3:11], p.EntityID)
	return dst
}

// WritePositionSync ghi một S2C PositionSync packet vào dst (opcode 0x12).
func WritePositionSync(dst []byte, p PositionSyncPayload) []byte {
	needed := 2 + 1 + 16
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+16))
	dst[2] = 0x12
	codec.WriteUint64(dst[3:11], p.EntityID)
	codec.WriteInt32(dst[11:15], p.X)
	codec.WriteInt32(dst[15:19], p.Z)
	return dst
}

// WriteStatsSync ghi một S2C StatsSync packet vào dst (opcode 0x13).
func WriteStatsSync(dst []byte, p StatsSyncPayload) []byte {
	needed := 2 + 1 + 32
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+32))
	dst[2] = 0x13
	codec.WriteUint64(dst[3:11], p.EntityID)
	codec.WriteInt32(dst[11:15], p.HP)
	codec.WriteInt32(dst[15:19], p.MaxHP)
	codec.WriteInt32(dst[19:23], p.MP)
	codec.WriteInt32(dst[23:27], p.MaxMP)
	codec.WriteInt32(dst[27:31], p.Dam)
	codec.WriteInt32(dst[31:35], p.Level)
	return dst
}

// WriteChatMessage ghi một S2C ChatMessage packet vào dst (opcode 0x15).
func WriteChatMessage(dst []byte, p ChatPayload) []byte {
	senderLen := len(p.SenderName)
	msgLen := len(p.Message)
	payloadLen := 4 + senderLen + msgLen
	needed := 2 + 1 + payloadLen

	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}

	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = 0x15

	offset := 3
	codec.WriteUint8(dst[offset:offset+1], p.Channel)
	offset++
	codec.WriteUint8(dst[offset:offset+1], uint8(senderLen))
	offset++
	copy(dst[offset:], p.SenderName)
	offset += senderLen
	codec.WriteUint16(dst[offset:offset+2], uint16(msgLen))
	offset += 2
	copy(dst[offset:], p.Message)
	return dst
}
