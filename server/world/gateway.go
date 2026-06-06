package world

import (
	"fmt"
	"server/ecs"
	"server/peakgo/codec"
)

// broadcastToMap sends data to all players on targetMapID.
// Uses the map worker's registry for the target map.
func broadcastToMap(targetMapID int, data []byte) {
	mw := GlobalWorld.GetWorker(targetMapID)
	if mw == nil {
		return
	}
	mw.Registry.RangeConnections(func(playerID ecs.Entity, netComp ecs.ConnectionComponent) bool {
		if netComp.Writer == nil {
			return true
		}
		netComp.Writer.Send(data)
		return true
	})
}

// HandleWarpSystem parses a binary payload containing MapID, X, and Z coordinates,
// and delegates to ExecuteMapTransfer.
// Payload layout: [MapID (int32 - BE)] [X (int32 - BE)] [Z (int32 - BE)] (total 12 bytes)
func HandleWarpSystem(playerID ecs.Entity, payload []byte) (string, bool) {
	if len(payload) != 12 {
		return "Error: Invalid warp payload length. Expected 12 bytes.\r\n", false
	}

	targetMapID := int(codec.ReadInt32(payload[0:4]))
	targetX := int(codec.ReadInt32(payload[4:8]))
	targetZ := int(codec.ReadInt32(payload[8:12]))

	return ExecuteMapTransfer(playerID, targetMapID, targetX, targetZ)
}

// ExecuteMapTransfer handles moving a player entity from one map to another
// using the World transfer orchestrator (two-phase commit).
// This replaces the legacy direct-coordinate-migration path.
func ExecuteMapTransfer(playerID ecs.Entity, targetMapID int, targetX int, targetZ int) (string, bool) {
	// 1. Authoritative Map Zone Validation
	if targetMapID < 1 || targetMapID > 3 {
		return "Warp Error: Target map zone does not exist in server configuration!\r\n", false
	}

	// Boundary check on the landing coordinates
	if targetX < 0 || targetX > 100 || targetZ < 0 || targetZ > 100 {
		return "Warp Denied: Landing coordinates out of world boundaries (0-100)!\r\n", false
	}

	// 2. Fetch the player's current spatial position record
	oldPos, exists := ecs.DefaultRegistry.GetPosition(playerID)
	if !exists {
		return "Warp Error: Your spatial position record was not found.\r\n", false
	}

	meta, _ := ecs.DefaultRegistry.GetMetadata(playerID)
	oldMapID := oldPos.MapID

	// Phase 1: THE MAP EXIT
	// Alert all witnesses on the OLD map that this entity has vanished
	exitNotice := []byte(fmt.Sprintf("[PORTAL]: Player %s vanished into a warping rift!\r\n", meta.Name))
	broadcastToMap(oldMapID, exitNotice)

	// Phase 2: THE COORDINATE MIGRATION via World transfer orchestrator.
	// This properly serializes/deserializes the entity between map workers
	// using the two-phase commit protocol.
	oldPos.MapID = targetMapID
	oldPos.X = targetX
	oldPos.Z = targetZ
	ecs.DefaultRegistry.SetPosition(playerID, oldPos)
	GlobalSpatialGrid.UpdateEntityPosition(playerID, oldPos)

	// Enqueue cross-map transfer via orchestrator
	GlobalWorld.TransferEntity(playerID, oldMapID, targetMapID)

	// Phase 3: THE MAP ENTRANCE
	// Alert all witnesses on the NEW map that this entity has materialized
	entranceNotice := []byte(fmt.Sprintf("[PORTAL]: Player %s materialized out of a warping rift at X:%d, Z:%d!\r\n",
		meta.Name, targetX, targetZ))
	broadcastToMap(targetMapID, entranceNotice)

	successMsg := fmt.Sprintf("[WARP]: Successfully zoned from Map #%d to Map #%d! Position: (%d, %d)\r\n",
		oldMapID, targetMapID, targetX, targetZ)

	return successMsg, true
}
