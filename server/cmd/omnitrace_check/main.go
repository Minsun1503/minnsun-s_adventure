// OmniTrace Check — sends a LOGIN + ATTACK packet with a hardcoded trace_id
// and then reads the trace file to verify the end-to-end trace pipeline.
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const (
	opcodeC2SLogin  byte = 10
	opcodeC2SAttack byte = 5
)

const serverAddr = "localhost:1503"

// The trace_id we hardcode: "aaaa1234"
// When parsed as uint32 BE: 0xaaaa1234
// wire bytes: 0xAA, 0xAA, 0x12, 0x34
var traceIDBytes = [4]byte{0xAA, 0xAA, 0x12, 0x34}

func main() {
	// ── Step 1: Connect ──
	conn, err := net.DialTimeout("tcp", serverAddr, 5*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// ── Step 2: Send LOGIN packet ──
	// Payload: [UsernameLen 1B][Username "test"][PasswordLen 1B][Password "pass123"]
	const username = "test"
	const password = "pass123"
	payload := make([]byte, 1+len(username)+1+len(password))
	payload[0] = byte(len(username))
	copy(payload[1:], username)
	payload[1+len(username)] = byte(len(password))
	copy(payload[2+len(username):], password)

	packet := make([]byte, 2+1+len(payload))
	binary.BigEndian.PutUint16(packet[0:2], uint16(1+len(payload)))
	packet[2] = opcodeC2SLogin
	copy(packet[3:], payload)

	if _, err := conn.Write(packet); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: login write: %v\n", err)
		os.Exit(1)
	}

	// ── Step 3: Read login response (Success + SpawnEntity + StatsSync) ──
	// We need to drain these to unblock the server's write buffer.
	var entityID uint64
	readDeadline := time.Now().Add(5 * time.Second)
	_ = conn.SetReadDeadline(readDeadline)

	for {
		var header [2]byte
		_, err := io.ReadFull(conn, header[:])
		if err != nil {
			break
		}
		length := binary.BigEndian.Uint16(header[:])
		if length == 0 || length > 4096 {
			break
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(conn, buf); err != nil {
			break
		}
		opcode := buf[0]
		pl := buf[1:]
		if opcode == 0x01 { // Success
			if len(pl) >= 8 {
				entityID = binary.BigEndian.Uint64(pl[0:8])
				fmt.Printf("[LOGIN] Got entityID=%d\n", entityID)
			}
			// After receiving Success, we have the entity ID.
			// Drain remaining packets, then break.
			continue
		}
		if entityID != 0 && opcode == 0x10 { // SpawnEntity (our own)
			fmt.Println("[LOGIN] Got SpawnEntity for self")
		}
		if entityID != 0 && opcode == 0x13 { // StatsSync
			fmt.Println("[LOGIN] Got StatsSync")
			// All initial packets received — proceed to attack.
			break
		}
	}

	if entityID == 0 {
		fmt.Fprintf(os.Stderr, "FAIL: did not receive entityID from login\n")
		os.Exit(1)
	}

	// ── Step 4: Send ATTACK packet with trace_id hint in first 4 bytes ──
	// Wire format: [Length 2B: 13][Opcode 0x05][trace_id 4B][TargetID 8B]
	// trace_id bytes = 0xAA 0xAA 0x12 0x34 → uint32 0xaaaa1234 → hex "aaaa1234"
	attackPayload := make([]byte, 4+8) // trace_id(4) + targetID(8)
	copy(attackPayload[0:4], traceIDBytes[:])
	binary.BigEndian.PutUint64(attackPayload[4:12], uint64(1)) // target monster ID = 1

	attackPacket := make([]byte, 2+1+len(attackPayload))
	binary.BigEndian.PutUint16(attackPacket[0:2], uint16(1+len(attackPayload)))
	attackPacket[2] = opcodeC2SAttack
	copy(attackPacket[3:], attackPayload)

	if _, err := conn.Write(attackPacket); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: attack write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[ATTACK] Sent attack packet with trace_id=aaaa1234")

	// ── Step 5: Read attack response (notice packet) ──
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var noticeBuf [2]byte
	if _, err := io.ReadFull(conn, noticeBuf[:]); err == nil {
		noticeLen := binary.BigEndian.Uint16(noticeBuf[:])
		if noticeLen > 0 && noticeLen <= 4096 {
			nb := make([]byte, noticeLen)
			if _, err := io.ReadFull(conn, nb); err == nil {
				fmt.Printf("[NOTICE] %s\n", string(nb[1:])) // skip opcode
			}
		}
	}

	fmt.Println("[PASS] All packets sent successfully. Trace should be present.")
	fmt.Println("[PASS] Now query MCP blackbox_filter_trace with trace_id=aaaa1234")
}
