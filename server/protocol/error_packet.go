package protocol

import (
	"encoding/binary"
	"net"
)

// Server-to-Client Error Opcode
// Packet format: [Length uint16 BE][Opcode 0xFF][ErrorCode uint16 BE][MessageLen uint16 BE][Message UTF-8]
//
// Server-to-Client Success Opcode
// Packet format: [Length uint16 BE][Opcode 0x01][MessageLen uint16 BE][Message UTF-8]
//
// S2C opcode constants are defined in opcodes.go.
// This file only contains the packet builder functions.

// SendErrorPacket constructs and transmits a binary error packet to the given TCP connection.
// It writes directly to net.Conn because at the point of error, the player entity
// may not yet exist in the ECS registry (e.g., login failure before CreatePlayerEntity).
//
// Parameters:
//   - conn:      The raw TCP socket to write to.
//   - errorCode: One of the predefined ErrCode* constants.
//   - message:   Human-readable error description for client UI display.
func SendErrorPacket(conn net.Conn, errorCode uint16, message string) {
	if conn == nil {
		return
	}

	msgBytes := []byte(message)

	// Payload = [Opcode 1B][ErrorCode 2B][MessageLen 2B][Message NB]
	payloadLen := 1 + 2 + 2 + len(msgBytes)

	// Full packet = [Length 2B][Payload]
	packet := make([]byte, 2+payloadLen)

	// Header: total payload length (excludes the 2-byte length prefix itself)
	binary.BigEndian.PutUint16(packet[0:2], uint16(payloadLen))

	// Opcode
	packet[2] = OpcodeS2CError

	// Error code
	binary.BigEndian.PutUint16(packet[3:5], errorCode)

	// Message length
	binary.BigEndian.PutUint16(packet[5:7], uint16(len(msgBytes)))

	// Message body
	copy(packet[7:], msgBytes)

	// Direct write — no deadline needed since we close immediately after
	conn.Write(packet)
}

// SendSuccessPacket constructs and transmits a binary success packet to the given TCP connection.
// Use this for one-shot positive acknowledgements (e.g., registration complete) where
// the server closes the connection immediately after sending.
//
// Packet format: [Length uint16 BE][Opcode 0x01][MessageLen uint16 BE][Message UTF-8]
//
// Parameters:
//   - conn:    The raw TCP socket to write to.
//   - message: Human-readable success message for client UI display.
func SendSuccessPacket(conn net.Conn, message string) {
	if conn == nil {
		return
	}

	msgBytes := []byte(message)

	// Payload = [Opcode 1B][MessageLen 2B][Message NB]
	payloadLen := 1 + 2 + len(msgBytes)

	// Full packet = [Length 2B][Payload]
	packet := make([]byte, 2+payloadLen)

	// Header: total payload length (excludes the 2-byte length prefix itself)
	binary.BigEndian.PutUint16(packet[0:2], uint16(payloadLen))

	// Opcode
	packet[2] = OpcodeS2CSuccess

	// Message length
	binary.BigEndian.PutUint16(packet[3:5], uint16(len(msgBytes)))

	// Message body
	copy(packet[5:], msgBytes)

	conn.Write(packet)
}
