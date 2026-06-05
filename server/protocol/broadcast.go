package protocol

import (
	"net"
	"server/ecs"
	"server/peakgo/netio"
	"server/peakgo/perf"
	"server/world"
)

// broadcastAOIRadius defines the area-of-interest radius (world units)
// for neighbor-based broadcasts (position sync, spawn/despawn).
const broadcastAOIRadius = 60.0

// BroadcastToMap sends text data to ALL players on targetMapID.
// Still O(N) across total connections but scoped to one map.
// Pre-allocates once via string → []byte conversion before looping.
func BroadcastToMap(targetMapID int, data string) {
	b := []byte(data)
	ecs.DefaultRegistry.RangeConnections(func(playerID ecs.Entity, netComp ecs.ConnectionComponent) bool {
		if netComp.Conn == nil {
			return true
		}
		playerPos, posExists := ecs.DefaultRegistry.GetPosition(playerID)
		if posExists && playerPos.MapID == targetMapID {
			writeConn(netComp.Conn, b)
		}
		return true
	})
}

// BroadcastToNeighbors sends data only to players within AOI radius of origin,
// using SpatialGrid.QueryRadius for O(1) chunk lookup (vs scanning all connections).
// This is the movement/combat sync hot-path — zero-allocation when data is pooled.
func BroadcastToNeighbors(origin ecs.PositionComponent, data []byte, excludeID ecs.Entity) {
	candidates := world.GlobalSpatialGrid.QueryRadius(origin, broadcastAOIRadius, excludeID)
	for _, entry := range *candidates {
		connComp, hasConn := ecs.DefaultRegistry.GetConnection(entry.ID)
		if !hasConn || connComp.Conn == nil {
			continue
		}
		// Monster/ground-item entities without net connections are skipped automatically.
		writeConn(connComp.Conn, data)
		perf.GlobalPacketMonitor.RecordBroadcast()
	}
	world.FreeQueryCandidates(candidates)
}

// BroadcastToNeighborsMap is a convenience: builds a PositionComponent from mapID/x/z
// and broadcasts to neighbors on that map within AOI.
func BroadcastToNeighborsMap(mapID int, x, z int, data []byte, excludeID ecs.Entity) {
	origin := ecs.PositionComponent{MapID: mapID, X: x, Z: z}
	BroadcastToNeighbors(origin, data, excludeID)
}

// BroadcastToChunk sends data only to players inside the exact spatial chunk containing pos.
// Uses SpatialGrid.QueryChunk for O(1) chunk lookup — no map-wide scan.
// Use this for events that only matter to entities in the same tile/cell (e.g. ground items).
func BroadcastToChunk(pos ecs.PositionComponent, data []byte, excludeID ecs.Entity) {
	candidates := world.GlobalSpatialGrid.QueryChunk(pos, excludeID)
	for _, entry := range candidates {
		connComp, hasConn := ecs.DefaultRegistry.GetConnection(entry.ID)
		if !hasConn || connComp.Conn == nil {
			continue
		}
		writeConn(connComp.Conn, data)
		perf.GlobalPacketMonitor.RecordBroadcast()
	}
}

// BroadcastToChunkWithRadius sends data to players in all chunks within
// AOI radius. Uses SpatialGrid.QueryRadius for O(chunk) lookup - lighter
// than BroadcastToMap's O(N) scan. Maintains exact distance filtering.
func BroadcastToChunkWithRadius(pos ecs.PositionComponent, data []byte, excludeID ecs.Entity) {
	candidates := world.GlobalSpatialGrid.QueryRadius(pos, broadcastAOIRadius, excludeID)
	for _, entry := range *candidates {
		connComp, hasConn := ecs.DefaultRegistry.GetConnection(entry.ID)
		if !hasConn || connComp.Conn == nil {
			continue
		}
		writeConn(connComp.Conn, data)
		perf.GlobalPacketMonitor.RecordBroadcast()
	}
	world.FreeQueryCandidates(candidates)
}

// writeConn is the single write point for all outbound TCP data.
func writeConn(c net.Conn, data []byte) {
	if c == nil {
		return
	}
	if err := netio.WritePacket(c, data); err != nil {
		c.Close()
	}
}
