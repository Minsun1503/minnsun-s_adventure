package world

import (
	"net"
	"sort"

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

// trimToMaxAOIWatchers sorts candidates by distance and keeps only the closest
// MaxAOIWatchers entries. This prevents CPU spikes when 500+ players stack on
// the same tile — each player only sees the 50 closest entities.
func trimToMaxAOIWatchers(candidates *[]ChunkEntry, origin ecs.PositionComponent) {
	if len(*candidates) <= aoi.MaxAOIWatchers {
		return
	}
	sort.Slice(*candidates, func(i, j int) bool {
		dxI := (*candidates)[i].Pos.X - origin.X
		dzI := (*candidates)[i].Pos.Z - origin.Z
		dxJ := (*candidates)[j].Pos.X - origin.X
		dzJ := (*candidates)[j].Pos.Z - origin.Z
		distSqI := dxI*dxI + dzI*dzI
		distSqJ := dxJ*dxJ + dzJ*dzJ
		return distSqI < distSqJ
	})
	*candidates = (*candidates)[:aoi.MaxAOIWatchers]
}

// aoiSpatialQuery adapts the global spatial grid QueryRadius to the aoi.SpatialQueryFunc signature.
// It extracts entity IDs from the ChunkEntry results, applying MaxAOIWatchers culling
// to prevent CPU spikes from 500+ stacked entities.
func aoiSpatialQuery(origin ecs.PositionComponent, worldRadius float64, excludeID ecs.Entity) *[]ecs.Entity {
	candidates := GlobalSpatialGrid.QueryRadius(origin, worldRadius, excludeID)
	if candidates == nil || len(*candidates) == 0 {
		FreeQueryCandidates(candidates)
		return nil
	}

	// Worst-case AOI culling: if 500 players stack on the same tile, keep only
	// the MaxAOIWatchers closest entities. Sorting by squared distance is fast
	// (avoids sqrt) and happens only when the threshold is exceeded.
	trimToMaxAOIWatchers(candidates, origin)

	// Use pooled slice instead of allocating a fresh slice
	ids := aoi.EntityListPool.Get()
	for _, entry := range *candidates {
		*ids = append(*ids, entry.ID)
	}
	FreeQueryCandidates(candidates)
	return ids
}

// aoiSpatialQueryFromGrid adapts any *SpatialGrid QueryRadius to the aoi.SpatialQueryFunc signature.
// It extracts entity IDs from the ChunkEntry results, using the specified grid,
// applying MaxAOIWatchers culling to prevent CPU spikes from 500+ stacked entities.
func aoiSpatialQueryFromGrid(grid *SpatialGrid, origin ecs.PositionComponent, worldRadius float64, excludeID ecs.Entity) *[]ecs.Entity {
	candidates := grid.QueryRadius(origin, worldRadius, excludeID)
	if candidates == nil || len(*candidates) == 0 {
		FreeQueryCandidates(candidates)
		return nil
	}

	// Worst-case AOI culling: keep only the closest MaxAOIWatchers entities.
	trimToMaxAOIWatchers(candidates, origin)

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
	eventsPtr := GlobalAOIManager.UpdateOne(entity, pos, aoiSpatialQuery)
	if eventsPtr == nil || len(*eventsPtr) == 0 {
		if eventsPtr != nil {
			aoi.AOIEventPool.Put(eventsPtr)
		}
		return
	}
	defer aoi.AOIEventPool.Put(eventsPtr)

	// Get the watcher's connection (only players have connections)
	watcherConn, hasConn := ecs.DefaultRegistry.GetConnection(entity)
	if !hasConn || watcherConn.Conn == nil {
		return
	}

	for _, ev := range *eventsPtr {
		switch ev.Type {
		case aoi.EventEnter:
			sendSpawnTo(watcherConn.Conn, ev.Target)
		case aoi.EventLeave:
			sendDespawnTo(watcherConn.Conn, ev.Target)
		}
	}
}

// sendSpawnToFrom builds a SpawnEntity frame for the target entity using a specific registry
// (per-map or global) and writes it to conn.
func sendSpawnToFrom(conn net.Conn, target ecs.Entity, reg *ecs.Registry) {
	meta, ok := reg.GetMetadata(target)
	if !ok {
		return
	}
	pos, ok2 := reg.GetPosition(target)
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

// sendSpawnTo builds a SpawnEntity frame for the target entity and writes it to conn.
func sendSpawnTo(conn net.Conn, target ecs.Entity) {
	meta, ok := ecs.DefaultRegistry.GetMetadata(target)
	if !ok {
		return
	}
	pos, ok2 := ecs.DefaultRegistry.GetPosition(target)
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
		connComp, hasConn := ecs.DefaultRegistry.GetConnection(entry.ID)
		if !hasConn || connComp.Conn == nil {
			continue
		}
		if err := netio.WritePacket(connComp.Conn, data); err != nil {
			connComp.Conn.Close()
		}
	}
}
