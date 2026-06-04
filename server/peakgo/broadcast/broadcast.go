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
	OpcodeCombatHit     byte = 0x14
	OpcodeChat          byte = 0x15
	OpcodeNotice        byte = 0x16
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

// encodeSuccessWithEntityIDPayload encodes a success payload that also carries
// the local player's EntityID: [EntityID uint64 BE][MessageLen uint16 BE][Message UTF-8]
func encodeSuccessWithEntityIDPayload(dst []byte, entityID uint64, message string) {
	codec.WriteUint64(dst[0:8], entityID)
	codec.WriteUint16(dst[8:10], uint16(len(message)))
	copy(dst[10:], message)
}

func encodeDespawnEntityPayload(dst []byte, p DespawnPayload) {
	codec.WriteUint64(dst[0:8], p.EntityID)
}

func encodePositionSyncPayload(dst []byte, p PositionSyncPayload) {
	codec.WriteUint64(dst[0:8], p.EntityID)
	codec.WriteInt32(dst[8:12], p.X)
	codec.WriteInt32(dst[12:16], p.Z)
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
// Format: [Length 2B][Opcode 0x01][MessageLen 2B][Message UTF-8]
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

// BuildSuccessWithEntityID constructs an S2C success packet (opcode 0x01) that
// also carries the local player's EntityID so the client can set LocalPlayerID
// from a trusted server source instead of guessing from StatsSync order.
// Format: [Length 2B][Opcode 0x01][EntityID 8B][MessageLen 2B][Message UTF-8]
func BuildSuccessWithEntityID(entityID uint64, message string) []byte {
	payloadLen := 8 + 2 + len(message) // entityID + messageLen + message
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeSuccess
	encodeSuccessWithEntityIDPayload(pkt[3:], entityID, message)
	return pkt
}

// WriteSuccessWithEntityID writes an S2C success packet with EntityID into dst (0 allocs).
func WriteSuccessWithEntityID(dst []byte, entityID uint64, message string) []byte {
	payloadLen := 8 + 2 + len(message)
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeSuccess
	encodeSuccessWithEntityIDPayload(dst[3:], entityID, message)
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
	l := uint16(1 + payloadLen)
	pkt[0] = byte(l >> 8)
	pkt[1] = byte(l)
	pkt[2] = OpcodeSpawnEntity
	v := p.EntityID
	pkt[3] = byte(v >> 56)
	pkt[4] = byte(v >> 48)
	pkt[5] = byte(v >> 40)
	pkt[6] = byte(v >> 32)
	pkt[7] = byte(v >> 24)
	pkt[8] = byte(v >> 16)
	pkt[9] = byte(v >> 8)
	pkt[10] = byte(v)
	pkt[11] = p.Type
	v2 := uint32(p.MapID)
	pkt[12] = byte(v2 >> 24)
	pkt[13] = byte(v2 >> 16)
	pkt[14] = byte(v2 >> 8)
	pkt[15] = byte(v2)
	v2 = uint32(p.X)
	pkt[16] = byte(v2 >> 24)
	pkt[17] = byte(v2 >> 16)
	pkt[18] = byte(v2 >> 8)
	pkt[19] = byte(v2)
	v2 = uint32(p.Z)
	pkt[20] = byte(v2 >> 24)
	pkt[21] = byte(v2 >> 16)
	pkt[22] = byte(v2 >> 8)
	pkt[23] = byte(v2)
	pkt[24] = uint8(len(p.Name))
	copy(pkt[25:], p.Name)
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
	l := uint16(1 + payloadLen)
	dst[0] = byte(l >> 8)
	dst[1] = byte(l)
	dst[2] = OpcodeSpawnEntity
	v := p.EntityID
	dst[3] = byte(v >> 56)
	dst[4] = byte(v >> 48)
	dst[5] = byte(v >> 40)
	dst[6] = byte(v >> 32)
	dst[7] = byte(v >> 24)
	dst[8] = byte(v >> 16)
	dst[9] = byte(v >> 8)
	dst[10] = byte(v)
	dst[11] = p.Type
	v2 := uint32(p.MapID)
	dst[12] = byte(v2 >> 24)
	dst[13] = byte(v2 >> 16)
	dst[14] = byte(v2 >> 8)
	dst[15] = byte(v2)
	v2 = uint32(p.X)
	dst[16] = byte(v2 >> 24)
	dst[17] = byte(v2 >> 16)
	dst[18] = byte(v2 >> 8)
	dst[19] = byte(v2)
	v2 = uint32(p.Z)
	dst[20] = byte(v2 >> 24)
	dst[21] = byte(v2 >> 16)
	dst[22] = byte(v2 >> 8)
	dst[23] = byte(v2)
	dst[24] = uint8(len(p.Name))
	copy(dst[25:], p.Name)
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
	pkt := make([]byte, 35)
	binary.BigEndian.PutUint16(pkt[0:2], 33)
	pkt[2] = OpcodeStatsSync
	binary.BigEndian.PutUint64(pkt[3:11], p.EntityID)
	binary.BigEndian.PutUint64(pkt[11:19], uint64(uint32(p.HP))<<32|uint64(uint32(p.MaxHP)))
	binary.BigEndian.PutUint64(pkt[19:27], uint64(uint32(p.MP))<<32|uint64(uint32(p.MaxMP)))
	binary.BigEndian.PutUint64(pkt[27:35], uint64(uint32(p.Dam))<<32|uint64(uint32(p.Level)))
	return pkt
}

// WriteStatsSync writes an S2C StatsSync packet into dst (0 allocs).
func WriteStatsSync(dst []byte, p StatsSyncPayload) []byte {
	const needed = 35
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], 33)
	dst[2] = OpcodeStatsSync
	binary.BigEndian.PutUint64(dst[3:11], p.EntityID)
	binary.BigEndian.PutUint64(dst[11:19], uint64(uint32(p.HP))<<32|uint64(uint32(p.MaxHP)))
	binary.BigEndian.PutUint64(dst[19:27], uint64(uint32(p.MP))<<32|uint64(uint32(p.MaxMP)))
	binary.BigEndian.PutUint64(dst[27:35], uint64(uint32(p.Dam))<<32|uint64(uint32(p.Level)))
	return dst
}

// ChatPayload holds the fields for an S2C ChatMessage packet (opcode 0x15).
type ChatPayload struct {
	Channel    uint8
	SenderName string
	Message    string
}

// BuildChatMessage constructs an S2C ChatMessage packet (opcode 0x15).
// ─── CombatHit Payload ─────────────────────────────────────────────────

type CombatHitPayload struct {
	AttackerID uint64
	TargetID   uint64
	Damage     int32
	TargetHP   int32
	Killed     uint8
}

func BuildCombatHit(p CombatHitPayload) []byte {
	payloadLen := 25
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeCombatHit
	binary.BigEndian.PutUint64(pkt[3:11], p.AttackerID)
	binary.BigEndian.PutUint64(pkt[11:19], p.TargetID)
	binary.BigEndian.PutUint32(pkt[19:23], uint32(p.Damage))
	binary.BigEndian.PutUint32(pkt[23:27], uint32(p.TargetHP))
	pkt[27] = p.Killed
	return pkt
}

func WriteCombatHit(dst []byte, p CombatHitPayload) []byte {
	payloadLen := 25
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeCombatHit
	binary.BigEndian.PutUint64(dst[3:11], p.AttackerID)
	binary.BigEndian.PutUint64(dst[11:19], p.TargetID)
	binary.BigEndian.PutUint32(dst[19:23], uint32(p.Damage))
	binary.BigEndian.PutUint32(dst[23:27], uint32(p.TargetHP))
	dst[27] = p.Killed
	return dst
}

// ─── Notice Payload ────────────────────────────────────────────────────

type NoticePayload struct {
	Message string
}

func BuildNotice(p NoticePayload) []byte {
	payloadLen := len(p.Message)
	pkt := make([]byte, 3+payloadLen)
	binary.BigEndian.PutUint16(pkt[0:2], uint16(1+payloadLen))
	pkt[2] = OpcodeNotice
	copy(pkt[3:], p.Message)
	return pkt
}

func WriteNotice(dst []byte, p NoticePayload) []byte {
	payloadLen := len(p.Message)
	needed := 3 + payloadLen
	if cap(dst) >= needed {
		dst = dst[:needed]
	} else {
		dst = make([]byte, needed)
	}
	binary.BigEndian.PutUint16(dst[0:2], uint16(1+payloadLen))
	dst[2] = OpcodeNotice
	copy(dst[3:], p.Message)
	return dst
}

func BuildChatMessage(p ChatPayload) []byte {
	payloadLen := 4 + len(p.SenderName) + len(p.Message)
	pkt := make([]byte, 3+payloadLen)
	l := uint16(1 + payloadLen)
	pkt[0] = byte(l >> 8)
	pkt[1] = byte(l)
	pkt[2] = OpcodeChat
	hw := uint16(p.Channel)<<8 | uint16(uint8(len(p.SenderName)))
	pkt[3] = byte(hw >> 8)
	pkt[4] = byte(hw)
	_ = pkt[5+len(p.SenderName)+1] // bounds check hoist
	copy(pkt[5:], p.SenderName)
	offset := 5 + len(p.SenderName)
	ml := uint16(len(p.Message))
	pkt[offset] = byte(ml >> 8)
	pkt[offset+1] = byte(ml)
	copy(pkt[offset+2:], p.Message)
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
	l := uint16(1 + payloadLen)
	dst[0] = byte(l >> 8)
	dst[1] = byte(l)
	dst[2] = OpcodeChat
	hw := uint16(p.Channel)<<8 | uint16(uint8(len(p.SenderName)))
	dst[3] = byte(hw >> 8)
	dst[4] = byte(hw)
	_ = dst[5+len(p.SenderName)+1] // bounds check hoist
	copy(dst[5:], p.SenderName)
	offset := 5 + len(p.SenderName)
	ml := uint16(len(p.Message))
	dst[offset] = byte(ml >> 8)
	dst[offset+1] = byte(ml)
	copy(dst[offset+2:], p.Message)
	return dst
}
