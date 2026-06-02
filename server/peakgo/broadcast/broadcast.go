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
package broadcast

import (
	"encoding/binary"
	"server/peakgo/codec"
)

// ─── Opcode Constants ────────────────────────────────────────────────────────

const (
	OpcodeSuccess       byte = 0x01
	OpcodeSpawnEntity   byte = 0x10
	OpcodeDespawnEntity byte = 0x11
	OpcodePositionSync  byte = 0x12
	OpcodeStatsSync     byte = 0x13
	OpcodeChat          byte = 0x15
	OpcodeError         byte = 0xFF
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
// Convenience wrapper for API readability. Note that casting string to []byte
// still causes an allocation/copy in standard Go.
func FrameText(opcode byte, text string) []byte {
	return Frame(opcode, []byte(text))
}

// FrameInto writes a framed packet into dst, growing it if necessary.
// Returns the slice resliced to exactly the packet length.
// 0 allocs when len(dst) >= 3+len(payload).
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

// ─── S2C Internal Encoders (Single Source of Truth) ──────────────────────────
//
// Các hàm encodePayload này chỉ tập trung xử lý cấu trúc dữ liệu nhị phân của payload,
// hoàn toàn không quan tâm đến vùng đệm (buffer) hay header của packet.

func encodeErrorPayload(dst []byte, errorCode uint16, message string) {
	codec.WriteUint16(dst[0:2], errorCode)
	codec.WriteUint16(dst[2:4], uint16(len(message)))
	copy(dst[4:], message)
}

func encodeSuccessPayload(dst []byte, message string) {
	codec.WriteUint16(dst[0:2], uint16(len(message)))
	copy(dst[2:], message)
}

func encodeSpawnEntityPayload(dst []byte, p SpawnPayload) {
	codec.WriteUint64(dst[0:8], p.EntityID)
	codec.WriteUint8(dst[8:9], p.Type)
	codec.WriteInt32(dst[9:13], p.MapID)
	codec.WriteInt32(dst[13:17], p.X)
	codec.WriteInt32(dst[17:21], p.Z)
	codec.WriteUint8(dst[21:22], uint8(len(p.Name)))
	copy(dst[22:], p.Name)
}

func encodeDespawnEntityPayload(dst []byte, p DespawnPayload) {
	codec.WriteUint64(dst[0:8], p.EntityID)
}

func encodePositionSyncPayload(dst []byte, p PositionSyncPayload) {
	codec.WriteUint64(dst[0:8], p.EntityID)
	codec.WriteInt32(dst[8:12], p.X)
	codec.WriteInt32(dst[12:16], p.Z)
}

func encodeStatsSyncPayload(dst []byte, p StatsSyncPayload) {
	codec.WriteUint64(dst[0:8], p.EntityID)
	codec.WriteInt32(dst[8:12], p.HP)
	codec.WriteInt32(dst[12:16], p.MaxHP)
	codec.WriteInt32(dst[16:20], p.MP)
	codec.WriteInt32(dst[20:24], p.MaxMP)
	codec.WriteInt32(dst[24:28], p.Dam)
	codec.WriteInt32(dst[28:32], p.Level)
}

func encodeChatMessagePayload(dst []byte, p ChatPayload) {
	codec.WriteUint8(dst[0:1], p.Channel)
	codec.WriteUint8(dst[1:2], uint8(len(p.SenderName)))
	copy(dst[2:], p.SenderName)
	offset := 2 + len(p.SenderName)
	codec.WriteUint16(dst[offset:offset+2], uint16(len(p.Message)))
	offset += 2
	copy(dst[offset:], p.Message)
}

// ─── S2C Typed Packet Builders & Writers ─────────────────────────────────────

// BuildError constructs an S2C error packet (opcode 0xFF).
// Optimized to execute with exactly 1 allocation/op.
func BuildError(errorCode uint16, message string) []byte {
	payloadLen := 2 + 2 + len(message)
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeError
	encodeErrorPayload(pkt[3:], errorCode, message)
	return pkt
}

// WriteError writes an S2C error packet into a caller-supplied dst buffer (0 allocs).
func WriteError(dst []byte, errorCode uint16, message string) []byte {
	payloadLen := 2 + 2 + len(message)
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeError
	encodeErrorPayload(dst[3:], errorCode, message)
	return dst
}

// BuildSuccess constructs an S2C success packet (opcode 0x01).
func BuildSuccess(message string) []byte {
	payloadLen := 2 + len(message)
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeSuccess
	encodeSuccessPayload(pkt[3:], message)
	return pkt
}

// WriteSuccess writes an S2C success packet into dst (0 allocs).
func WriteSuccess(dst []byte, message string) []byte {
	payloadLen := 2 + len(message)
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeSuccess
	encodeSuccessPayload(dst[3:], message)
	return dst
}

// SpawnPayload holds the fields for an S2C SpawnEntity packet (opcode 0x10).
type SpawnPayload struct {
	EntityID uint64
	Type     uint8 // 0=player, 1=monster, 2=ground_item
	MapID    int32
	X        int32
	Z        int32
	Name     string // Sửa đổi: Xóa NameLen thừa để tránh mâu thuẫn dữ liệu
}

// BuildSpawnEntity constructs an S2C SpawnEntity packet (opcode 0x10).
func BuildSpawnEntity(p SpawnPayload) []byte {
	payloadLen := 22 + len(p.Name)
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeSpawnEntity
	encodeSpawnEntityPayload(pkt[3:], p)
	return pkt
}

// WriteSpawnEntity writes an S2C SpawnEntity packet into dst (0 allocs).
func WriteSpawnEntity(dst []byte, p SpawnPayload) []byte {
	payloadLen := 22 + len(p.Name)
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeSpawnEntity
	encodeSpawnEntityPayload(dst[3:], p)
	return dst
}

// DespawnPayload holds the fields for an S2C DespawnEntity packet (opcode 0x11).
type DespawnPayload struct {
	EntityID uint64
}

// BuildDespawnEntity constructs an S2C DespawnEntity packet (opcode 0x11).
func BuildDespawnEntity(p DespawnPayload) []byte {
	payloadLen := 8
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeDespawnEntity
	encodeDespawnEntityPayload(pkt[3:], p)
	return pkt
}

// WriteDespawnEntity writes an S2C DespawnEntity packet into dst (0 allocs).
func WriteDespawnEntity(dst []byte, p DespawnPayload) []byte {
	payloadLen := 8
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeDespawnEntity
	encodeDespawnEntityPayload(dst[3:], p)
	return dst
}

// PositionSyncPayload holds the fields for an S2C PositionSync packet (opcode 0x12).
type PositionSyncPayload struct {
	EntityID uint64
	X        int32
	Z        int32
}

// BuildPositionSync constructs an S2C PositionSync packet (opcode 0x12).
func BuildPositionSync(p PositionSyncPayload) []byte {
	payloadLen := 16
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodePositionSync
	encodePositionSyncPayload(pkt[3:], p)
	return pkt
}

// WritePositionSync writes an S2C PositionSync packet into dst (0 allocs).
func WritePositionSync(dst []byte, p PositionSyncPayload) []byte {
	payloadLen := 16
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodePositionSync
	encodePositionSyncPayload(dst[3:], p)
	return dst
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
func BuildStatsSync(p StatsSyncPayload) []byte {
	payloadLen := 32
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeStatsSync
	encodeStatsSyncPayload(pkt[3:], p)
	return pkt
}

// WriteStatsSync writes an S2C StatsSync packet into dst (0 allocs).
func WriteStatsSync(dst []byte, p StatsSyncPayload) []byte {
	payloadLen := 32
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeStatsSync
	encodeStatsSyncPayload(dst[3:], p)
	return dst
}

// ChatPayload holds the fields for an S2C ChatMessage packet (opcode 0x15).
type ChatPayload struct {
	Channel    uint8
	SenderName string
	Message    string
}

// BuildChatMessage constructs an S2C ChatMessage packet (opcode 0x15).
func BuildChatMessage(p ChatPayload) []byte {
	payloadLen := 4 + len(p.SenderName) + len(p.Message)
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeChat
	encodeChatMessagePayload(pkt[3:], p)
	return pkt
}

// WriteChatMessage writes an S2C ChatMessage packet into dst (0 allocs).
func WriteChatMessage(dst []byte, p ChatPayload) []byte {
	payloadLen := 4 + len(p.SenderName) + len(p.Message)
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeChat
	encodeChatMessagePayload(dst[3:], p)
	return dst
}
