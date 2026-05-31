package systems

import (
	"server/ecs"
	"strconv"
	"strings"
)

// HandlePlayerMovementSystem parses a raw "M X Z" string packet and
// delegates execution to MovementSystem.
//
// This function owns only: string parsing + pre-validation error messages.
// MovementSystem owns: boundary enforcement, ECS write, broadcast.
//
// Returns:
//   - (errorMsg, false) if parsing fails before reaching MovementSystem.
//   - ("", true)        if MovementSystem was invoked (it handles its own notices).
func HandlePlayerMovementSystem(playerID ecs.Entity, rawMessage string) (string, bool) {
	parts := strings.Fields(rawMessage)
	if len(parts) != 3 {
		return "Syntax error. Use: M <X> <Z>\r\n", false
	}

	targetX, errX := strconv.Atoi(parts[1])
	targetZ, errZ := strconv.Atoi(parts[2])
	if errX != nil || errZ != nil {
		return "Error: Coordinates must be valid integers.\r\n", false
	}

	MovementSystem(playerID, targetX, targetZ)
	return "", true
}
