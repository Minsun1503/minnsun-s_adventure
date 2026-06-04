package world

import (
	"net"

	"server/ecs"
	"server/peakgo/aoi"
	"server/peakgo/broadcast"
	"server/peakgo/netio"
)

// GlobalAOIManager is the singleton AOI manager used to track watcher neighborhoods.
var GlobalAOIManager *aoi.AOIManager

// WatcherRadius is the visibility radius (game units) used when registering watchers.
const WatcherRadius = 60.0

// InitAOIManager creates the global AOI manager. Must be called once at server startup.
func InitAOIManager() {
	GlobalAOIManager = aoi.NewAOIManager()
}

// RegisterPlayerAOI registers a player entity as an AOI watcher.
// This should be called when a player connects or enters the world.
func RegisterPlayerAOI(entity ecs.Entity) {
	GlobalAOIManager.RegisterWatcher(entity, WatcherRadius)
}

// UnregisterPlayerAOI removes a player from the AOI watcher set.
// This should be called when a player disconnects or leaves the world.
func UnregisterPlayerAOI(entity ecs.Entity) {
	GlobalAOIManager.UnregisterWatcher(entity)
}

// aoiSpatialQuery adapts the spatial grid QueryRadius to the aoi.SpatialQueryFunc signature.
// aoiSpatialQuery adapts the spatial grid QueryRadius to the aoi.SpatialQueryFunc signature.
// It extracts entity IDs from the ChunkEntry results.
func aoiSpatialQuery(origin ecs.PositionComponent, worldRadius float64, excludeID ecs.Entity) *[]ecs.Entity {
	candidates := GlobalSpatialGrid.QueryRadius(origin, worldRadius, excludeID)
	if candidates == nil || len(*candidates) == 0 {
		FreeQueryCandidates(candidates)
		return nil
	}
	// Use pooled slice instead of allocating a fresh slice
	ids := aoi.EntityListPool.Get()
	for _, entry := range *candidates {
		*ids = append(*ids, entry.ID)
	}
	FreeQueryCandidates(candidates)
	return ids
}

// ProcessAOIEvents updates the AOI watcher for a single entity, producing enter/leave
// events and sending corresponding SpawnEntity/DespawnEntity packets to the affected watcher.
func ProcessAOIEvents(entity ecs.Entity, pos ecs.PositionComponent) {
	events := GlobalAOIManager.UpdateOne(entity, pos, aoiSpatialQuery)
	if len(events) == 0 {
		return
	}

	// Get the watcher's connection (only players have connections)
	watcherConn, hasConn := ecs.GlobalRegistry.GetConnection(entity)
	if !hasConn || watcherConn.Conn == nil {
		return
	}

	for _, ev := range events {
		switch ev.Type {
		case aoi.EventEnter:
			sendSpawnTo(watcherConn.Conn, ev.Target)
		case aoi.EventLeave:
			sendDespawnTo(watcherConn.Conn, ev.Target)
		}
	}
}

// sendSpawnTo builds a SpawnEntity frame for the target entity and writes it to conn.
func sendSpawnTo(conn net.Conn, target ecs.Entity) {
	meta, ok := ecs.GlobalRegistry.GetMetadata(target)
	if !ok {
		return
	}
	pos, ok2 := ecs.GlobalRegistry.GetPosition(target)
	if !ok2 {
		return
	}

	payload := broadcast.SpawnPayload{
		EntityID: uint64(target),
		Type:     uint8(meta.Type),
		MapID:    int32(pos.MapID),
		X:        int32(pos.X),
		Z:        int32(pos.Z),
		Name:     meta.Name,
	}
	frame := broadcast.BuildSpawnEntity(payload)
	if err := netio.WritePacket(conn, frame); err != nil {
		conn.Close()
	}
}

// sendDespawnTo builds a DespawnEntity frame for the target entity and writes it to conn.
func sendDespawnTo(conn net.Conn, target ecs.Entity) {
	payload := broadcast.DespawnPayload{
		EntityID: uint64(target),
	}
	frame := broadcast.BuildDespawnEntity(payload)
	if err := netio.WritePacket(conn, frame); err != nil {
		conn.Close()
	}
}

// BroadcastToNeighborsDelta is the replacement for BroadcastToMap.
// It sends binary data only to the AOI watchers that have the origin in their neighbor set.
// Unlike the old BroadcastToMap which scanned all connections, this uses the AOI
// watcher state to deliver delta (enter/leave) targeted packets.
func BroadcastToNeighborsDelta(origin ecs.PositionComponent, data []byte, excludeID ecs.Entity) {
	candidates := GlobalSpatialGrid.QueryRadius(origin, WatcherRadius, excludeID)
	if candidates == nil {
		return
	}
	defer FreeQueryCandidates(candidates)
	for _, entry := range *candidates {
		connComp, hasConn := ecs.GlobalRegistry.GetConnection(entry.ID)
		if !hasConn || connComp.Conn == nil {
			continue
		}
		if err := netio.WritePacket(connComp.Conn, data); err != nil {
			connComp.Conn.Close()
		}
	}
}
