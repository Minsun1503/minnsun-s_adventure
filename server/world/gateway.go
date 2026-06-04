package world

import (
	"fmt"
	"net"
	"server/ecs"
	"server/peakgo/codec"
	"server/peakgo/netio"
)

// writeConn is the single write point for all outbound TCP data.
func writeConn(c net.Conn, data []byte) {
	if c == nil {
		return
	}
	if err := netio.WritePacket(c, data); err != nil {
		c.Close()
	}
}

// broadcastToMap sends data to all players on targetMapID.
// Uses GlobalRegistry for backward compatibility during migration.
func broadcastToMap(targetMapID int, data []byte) {
	ecs.GlobalRegistry.RangeConnections(func(playerID ecs.Entity, netComp ecs.ConnectionComponent) bool {
		if netComp.Conn == nil {
			return true
		}
		playerPos, posExists := ecs.GlobalRegistry.GetPosition(playerID)
		if posExists && playerPos.MapID == targetMapID {
			writeConn(netComp.Conn, data)
		}
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

// ExecuteMapTransfer handles moving a player entity row safely from one zone to another.
// It orchestrates clear notifications to both the old and new spatial maps.
//
// This version uses the World transfer orchestrator when available, falling back
// to the legacy direct-transfer path for backward compatibility.
func ExecuteMapTransfer(playerID ecs.Entity, targetMapID int, targetX int, targetZ int) (string, bool) {
	// 1. Authoritative Map Zone Validation
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
	oldMapID := oldPos.MapID

	// Phase 1: THE MAP EXIT
	// Alert all witnesses on the OLD map that this entity has vanished
	exitNotice := []byte(fmt.Sprintf("[PORTAL]: Player %s vanished into a warping rift!\r\n", meta.Name))
	broadcastToMap(oldMapID, exitNotice)

	// Phase 2: THE COORDINATE MIGRATION
	// Use World transfer orchestrator when running in multi-map mode.
	// This properly serializes/deserializes the entity between map workers.
	if GlobalWorld != nil {
		// Update position in GlobalRegistry for backward compat
		oldPos.MapID = targetMapID
		oldPos.X = targetX
		oldPos.Z = targetZ
		ecs.GlobalRegistry.SetPosition(playerID, oldPos)
		GlobalSpatialGrid.UpdateEntityPosition(playerID, oldPos)

		// Enqueue cross-map transfer via orchestrator
		GlobalWorld.TransferEntity(playerID, oldMapID, targetMapID)
	} else {
		// Legacy path: direct coordinate migration
		oldPos.MapID = targetMapID
		oldPos.X = targetX
		oldPos.Z = targetZ
		ecs.GlobalRegistry.SetPosition(playerID, oldPos)
		GlobalSpatialGrid.UpdateEntityPosition(playerID, oldPos)
	}

	// Phase 3: THE MAP ENTRANCE
	// Alert all witnesses on the NEW map that this entity has materialized
	entranceNotice := []byte(fmt.Sprintf("[PORTAL]: Player %s materialized out of a warping rift at X:%d, Z:%d!\r\n",
		meta.Name, targetX, targetZ))
	broadcastToMap(targetMapID, entranceNotice)

	successMsg := fmt.Sprintf("[WARP]: Successfully zoned from Map #%d to Map #%d! Position: (%d, %d)\r\n",
		oldMapID, targetMapID, targetX, targetZ)

	return successMsg, true
}
