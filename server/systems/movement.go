package systems

import (
	"encoding/binary"
	"server/ecs"
)

// HandlePlayerMovementSystem parses a binary payload containing target X and Z coordinates.
// Payload layout: [X (int32 - BE)] [Z (int32 - BE)] (total 8 bytes)
//
// Returns:
//   - (errorMsg, false) if parsing fails before reaching MovementSystem.
//   - ("", true)        if MovementSystem was invoked.
func HandlePlayerMovementSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	if len(payload) != 8 {
		return "Error: Invalid movement payload length. Expected 8 bytes.\r\n", false
	}

	targetX := int(int32(binary.BigEndian.Uint32(payload[0:4])))
	targetZ := int(int32(binary.BigEndian.Uint32(payload[4:8])))

	MovementSystem(playerID, targetX, targetZ)
	return "", true
}
