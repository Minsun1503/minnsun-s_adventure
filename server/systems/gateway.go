package systems

import (
	"encoding/binary"
	"fmt"
	"server/ecs"
)

// HandleWarpSystem parses a binary payload containing MapID, X, and Z coordinates,
// and delegates to ExecuteMapTransfer.
// Payload layout: [MapID (int32 - BE)] [X (int32 - BE)] [Z (int32 - BE)] (total 12 bytes)
func HandleWarpSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	if len(payload) != 12 {
		return "Error: Invalid warp payload length. Expected 12 bytes.\r\n", false
	}

	targetMapID := int(int32(binary.BigEndian.Uint32(payload[0:4])))
	targetX := int(int32(binary.BigEndian.Uint32(payload[4:8])))
	targetZ := int(int32(binary.BigEndian.Uint32(payload[8:12])))

	return ExecuteMapTransfer(playerID, targetMapID, targetX, targetZ)
}

// ExecuteMapTransfer handles moving a player entity row safely from one zone to another.
// It orchestrates clear notifications to both the old and new spatial maps.
func ExecuteMapTransfer(playerID ecs.Entity, targetMapID int, targetX int, targetZ int) (string, bool) {
	// 1. Authoritative Map Zone Validation
	// Let's assume our server currently supports 3 maps (1=Town, 2=Forest, 3=Dungeon)
	if targetMapID < 1 || targetMapID > 3 {
		return "Warp Error: Target map zone does not exist in server configuration!\r\n", false
	}

	// Boundary check on the landing coordinates
	if targetX < 0 || targetX > 100 || targetZ < 0 || targetZ > 100 {
		return "Warp Denied: Landing coordinates out of world boundaries (0-100)!\r\n", false
	}

	// 2. COPY: Fetch the player's current spatial position record
	oldPos, exists := ecs.GlobalRegistry.GetPosition(playerID)
	if !exists {
		return "Warp Error: Your spatial position record was not found.\r\n", false
	}

	meta, _ := ecs.GlobalRegistry.GetMetadata(playerID)

	// Phase 1: THE MAP EXIT
	// Alert all witnesses on the OLD map that this entity has vanished
	exitNotice := fmt.Sprintf("[PORTAL]: Player %s vanished into a warping rift!\r\n", meta.Name)
	BroadcastToMap(oldPos.MapID, exitNotice)

	// Phase 2: THE COORDINATE MIGRATION
	// Modify our local structure copy and overwrite the database column lock-free
	oldMapID := oldPos.MapID
	oldPos.MapID = targetMapID
	oldPos.X = targetX
	oldPos.Z = targetZ
	ecs.GlobalRegistry.SetPosition(playerID, oldPos)

	// CRITICAL SYNC: Update spatial grid to move player to the new map chunk!
	GlobalSpatialGrid.UpdateEntityPosition(playerID, oldPos)

	// Phase 3: THE MAP ENTRANCE
	// Alert all witnesses on the NEW map that this entity has materialized
	entranceNotice := fmt.Sprintf("[PORTAL]: Player %s materialized out of a warping rift at X:%d, Z:%d!\r\n",
		meta.Name, targetX, targetZ)
	BroadcastToMap(targetMapID, entranceNotice)

	successMsg := fmt.Sprintf("[WARP]: Successfully zoned from Map #%d to Map #%d! Position: (%d, %d)\r\n",
		oldMapID, targetMapID, targetX, targetZ)

	return successMsg, true
}
