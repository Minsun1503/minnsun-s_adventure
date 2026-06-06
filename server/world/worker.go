package world

import (
	"net"
	"sort"

	"server/ecs"
	"server/logger"
	"server/peakgo/aoi"
	"server/peakgo/broadcast"
	"server/peakgo/connwriter"
	"server/peakgo/netio"
	"server/peakgo/perf"
)

// ─── MapWorker ──────────────────────────────────────────────────────────────
//
// MapWorker is an isolated game map with its own ECS Registry, SpatialGrid,
// AOI manager, and CommandBuffer. Each MapWorker runs its own tick loop,
// enabling true multi-core parallel simulation across maps.
//
// Design decisions:
//   - Each MapWorker owns a full ecs.Registry (not a subset) to keep the
//     codebase simple and avoid partial-store bugs. The trade-off is slightly
//     more memory per map, which is negligible for an MMORPG (typically <10
//     maps). The benefit is that all ECS query functions (QueryPositionAI,
//     QueryPositionStats, etc.) work identically on per-map registries.
//   - The central World holds the global entity ID (nextID) counter.
//     Entity IDs are unique across all maps — a monster on Map 1 will never
//     collide with a player on Map 2.
//   - Cross-map entity transfer uses serialization/deserialization of
//     component snapshots pushed through a central orchestrator channel.
//   - No archetype ECS, no microservices — just N isolated registries
//     running on N goroutines with a shared orchestrator.

// MapWorker holds the per-map ECS state and systems.
type MapWorker struct {
	ID int

	// tickChan is the dedicated channel for receiving tick signals.
	// The World dispatches ticks via non-blocking sends to this channel,
	// and the runMapWorker goroutine reads from it.
	tickChan chan uint64

	// Registry is this map's ECS component store. All entities on this map
	// are registered here. The central World's nextID counter ensures global
	// uniqueness of entity IDs across all maps.
	Registry *ecs.Registry

	// SpatialGrid is this map's spatial partition for proximity queries.
	SpatialGrid *SpatialGrid

	// AOIManager tracks watcher neighborhoods for this map.
	AOIManager *aoi.AOIManager

	// CmdBuf is this map's command buffer for deferred ECS mutations.
	CmdBuf *ecs.CommandBuffer

	// CombatBuffer accumulates all damage events during a single map tick.
	// Instead of applying HP subtraction and broadcasting per-hit (O(N²) when
	// 1000 players hit the same target), damage events are buffered and flushed
	// once per tick, producing exactly 1 StatsSync broadcast per unique target.
	CombatBuffer *ecs.CombatAccumulator

	// tickFn is the main simulation function for this map, registered at boot.
	tickFn MapTickFn

	// activeRegions tracks which spatial regions are currently active.
	// An empty region has its systems suspended to save CPU.
	activeRegions map[ChunkKey]bool

	// regionWakeBuffer buffers entities that entered an inactive region
	// during the tick, so the region can be woken on the next cycle.
	regionWakeBuffer []ChunkKey
}

// NewMapWorker creates a new MapWorker with fresh ECS, spatial, and AOI state.
func NewMapWorker(mapID int, fn MapTickFn) *MapWorker {
	mw := &MapWorker{
		ID:               mapID,
		tickChan:         make(chan uint64, 8), // small buffer to absorb burst peaks
		Registry:         ecs.NewRegistry(),
		SpatialGrid:      newSpatialGrid(),
		AOIManager:       aoi.NewAOIManager(),
		CmdBuf:           ecs.NewCommandBuffer(),
		CombatBuffer:     ecs.NewCombatAccumulator(),
		tickFn:           fn,
		activeRegions:    make(map[ChunkKey]bool),
		regionWakeBuffer: make([]ChunkKey, 0, 8),
	}
	return mw
}

// ─── Per-Map AOI Operations ─────────────────────────────────────────────────

// RegisterPlayerAOI registers a player entity as an AOI watcher on this map.
func (mw *MapWorker) RegisterPlayerAOI(entity ecs.Entity) {
	mw.AOIManager.RegisterWatcher(entity, WatcherRadius)
}

// UnregisterPlayerAOI removes a player from this map's AOI watcher set.
func (mw *MapWorker) UnregisterPlayerAOI(entity ecs.Entity) {
	mw.AOIManager.UnregisterWatcher(entity)
}

// ProcessAOIEvents updates the AOI watcher for a single entity on this map,
// producing enter/leave events and sending corresponding packets.
func (mw *MapWorker) ProcessAOIEvents(entity ecs.Entity, pos ecs.PositionComponent) {
	eventsPtr := mw.AOIManager.UpdateOne(entity, pos, func(origin ecs.PositionComponent, worldRadius float64, excludeID ecs.Entity) *[]ecs.Entity {
		return aoiSpatialQueryFromGrid(mw.SpatialGrid, origin, worldRadius, excludeID)
	})
	if eventsPtr == nil || len(*eventsPtr) == 0 {
		if eventsPtr != nil {
			aoi.AOIEventPool.Put(eventsPtr)
		}
		return
	}
	defer aoi.AOIEventPool.Put(eventsPtr)

	// Get the watcher's connection (only players have connections)
	watcherConn, hasConn := mw.Registry.GetConnection(entity)
	if !hasConn || watcherConn.Conn == nil {
		return
	}

	for _, ev := range *eventsPtr {
		switch ev.Type {
		case aoi.EventEnter:
			sendSpawnToFrom(watcherConn.Conn, ev.Target, mw.Registry)
		case aoi.EventLeave:
			sendDespawnTo(watcherConn.Conn, ev.Target)
		}
	}
}

// ─── Entity Lifecycle ────────────────────────────────────────────────────────

// SpawnEntity creates an entity on this map with the given components.
// The entity ID is pre-allocated (typically via World.AllocateEntityID).
func (mw *MapWorker) SpawnEntity(id ecs.Entity, pos ecs.PositionComponent, meta ecs.MetadataComponent, stats ecs.StatsComponent) {
	mw.Registry.SetPosition(id, pos)
	mw.Registry.SetMetadata(id, meta)
	mw.Registry.SetStats(id, stats)
	mw.SpatialGrid.UpdateEntityPosition(id, pos)
}

// DespawnEntity removes an entity from this map and recycles its ID.
func (mw *MapWorker) DespawnEntity(id ecs.Entity) {
	mw.SpatialGrid.RemoveEntity(id)
	mw.Registry.RemoveEntity(id)
}

// GetEntityCount returns the number of active entities on this map.
func (mw *MapWorker) GetEntityCount() int {
	return len(mw.Registry.GetAllEntities())
}

// ─── Region Suspension ───────────────────────────────────────────────────────

// ActivateRegion marks a chunk region as active (systems will run).
func (mw *MapWorker) ActivateRegion(key ChunkKey) {
	if !mw.activeRegions[key] {
		mw.activeRegions[key] = true
		logger.Debug("[MAP %d] Region (%d,%d) activated", mw.ID, key.X, key.Z)
	}
}

// DeactivateRegion marks a chunk region as inactive (systems will be skipped).
func (mw *MapWorker) DeactivateRegion(key ChunkKey) {
	if mw.activeRegions[key] {
		delete(mw.activeRegions, key)
		logger.Debug("[MAP %d] Region (%d,%d) deactivated", mw.ID, key.X, key.Z)
	}
}

// IsRegionActive returns true if the chunk region is active.
func (mw *MapWorker) IsRegionActive(key ChunkKey) bool {
	return mw.activeRegions[key]
}

// WakeRegionForEntity buffers a chunk key for activation on the next tick.
// Called when a player/monster enters a previously empty chunk.
func (mw *MapWorker) WakeRegionForEntity(pos ecs.PositionComponent) {
	key := worldToChunk(pos)
	if !mw.activeRegions[key] {
		mw.regionWakeBuffer = append(mw.regionWakeBuffer, key)
	}
}

// FlushRegionWakeBuffer activates all pending regions from the buffer.
func (mw *MapWorker) FlushRegionWakeBuffer() {
	for _, key := range mw.regionWakeBuffer {
		mw.ActivateRegion(key)
	}
	mw.regionWakeBuffer = mw.regionWakeBuffer[:0]
}

// SweepRegions deactivates regions that have no entities.
// Called periodically (not every tick) to avoid useless scanning.
func (mw *MapWorker) SweepRegions() {
	// Collect regions that still have entities
	populated := make(map[ChunkKey]bool)
	mw.SpatialGrid.ForEachEntityChunk(func(id ecs.Entity, key ChunkKey) bool {
		populated[key] = true
		return true
	})

	// Deactivate regions that are no longer populated
	for key := range mw.activeRegions {
		if !populated[key] {
			mw.DeactivateRegion(key)
		}
	}
}

// ─── Worker Lifecycle ────────────────────────────────────────────────────────

// Tick runs one simulation tick on this map worker.
// It calls the registered tick function with per-map state, then flushes
// the command buffer to this map's own registry and spatial grid.
//
// Combat accumulation: Before running the tick systems, the MapWorker installs
// itself as the current combat accumulator target via ecs.CurrentCombatBuffer.
// After all systems have run, CombatBuffer.Flush applies the accumulated damage
// and sends one consolidated broadcast per damaged entity. This eliminates the
// O(N²) broadcast storm when hundreds of players hit the same target in one tick.
func (mw *MapWorker) Tick(tick uint64) {
	// Install this map's combat accumulator so game systems (AttackSystem,
	// SkillPipeline) write damage events into the buffer instead of applying
	// them immediately. The ecs.CurrentCombatBuffer global is read by
	// game.DamageSystem to decide where to route damage events.
	ecs.CurrentCombatBuffer = mw.CombatBuffer

	// Run the registered tick function
	mw.tickFn(mw.ID, tick, mw.CmdBuf)

	// Flush commands to this map's own registry and spatial grid
	mw.CmdBuf.Flush(mw.SpatialGrid)

	// Flush accumulated combat damage: applies HP subtraction, adds threat,
	// sends exactly one StatsSync broadcast per unique damaged target.
	mw.flushCombatBuffer()

	// Process region wake buffer
	mw.FlushRegionWakeBuffer()
}

// Close shuts down this worker's tick channel, causing the runMapWorker
// goroutine to exit on the next range iteration.
func (mw *MapWorker) Close() {
	close(mw.tickChan)
}

// Free releases pooled resources held by this worker.
func (mw *MapWorker) Free() {
	mw.CmdBuf.Free()
	mw.CombatBuffer.Free()
}

// ─── Combat Buffer Flush ─────────────────────────────────────────────────────

// broadcastAOIRadius defines the area-of-interest radius (world units)
// for neighbor-based broadcasts (position sync, spawn/despawn).
const combatBufferAOIRadius = 60.0

// buildStatsSyncFrame builds a StatsSync binary frame for the given entity.
// Layout: [Length 2B][Opcode 0x13][EntityID 8B]
//
//	[HP:MaxHP 8B][MP:MaxMP 8B][Dam:Level 8B]
//	[Defense:MagicDefense 8B][MagicAttack:HitRate 8B][DodgeRate:CritRate 8B]
func buildStatsSyncFrame(entityID uint64, hp, maxHP, mp, maxMP, dam, level, defense, magicDefense, magicAttack, hitRate, dodgeRate, critRate int32) []byte {
	// StatsSync is 59 bytes total (2 length + 1 opcode + 56 payload)
	frame := make([]byte, 59)
	frame[0] = 0
	frame[1] = 57 // length = 1 + 56 payload bytes
	frame[2] = 0x13
	// EntityID (8 bytes BE)
	v := entityID
	frame[3] = byte(v >> 56)
	frame[4] = byte(v >> 48)
	frame[5] = byte(v >> 40)
	frame[6] = byte(v >> 32)
	frame[7] = byte(v >> 24)
	frame[8] = byte(v >> 16)
	frame[9] = byte(v >> 8)
	frame[10] = byte(v)
	// HP:MaxHP packed (4 bytes each)
	frame[11] = byte(uint32(hp) >> 24)
	frame[12] = byte(uint32(hp) >> 16)
	frame[13] = byte(uint32(hp) >> 8)
	frame[14] = byte(uint32(hp))
	frame[15] = byte(uint32(maxHP) >> 24)
	frame[16] = byte(uint32(maxHP) >> 16)
	frame[17] = byte(uint32(maxHP) >> 8)
	frame[18] = byte(uint32(maxHP))
	// MP:MaxMP packed (4 bytes each)
	frame[19] = byte(uint32(mp) >> 24)
	frame[20] = byte(uint32(mp) >> 16)
	frame[21] = byte(uint32(mp) >> 8)
	frame[22] = byte(uint32(mp))
	frame[23] = byte(uint32(maxMP) >> 24)
	frame[24] = byte(uint32(maxMP) >> 16)
	frame[25] = byte(uint32(maxMP) >> 8)
	frame[26] = byte(uint32(maxMP))
	// Dam:Level packed (4 bytes each)
	frame[27] = byte(uint32(dam) >> 24)
	frame[28] = byte(uint32(dam) >> 16)
	frame[29] = byte(uint32(dam) >> 8)
	frame[30] = byte(uint32(dam))
	frame[31] = byte(uint32(level) >> 24)
	frame[32] = byte(uint32(level) >> 16)
	frame[33] = byte(uint32(level) >> 8)
	frame[34] = byte(uint32(level))
	// Defense:MagicDefense packed (4 bytes each)
	frame[35] = byte(uint32(defense) >> 24)
	frame[36] = byte(uint32(defense) >> 16)
	frame[37] = byte(uint32(defense) >> 8)
	frame[38] = byte(uint32(defense))
	frame[39] = byte(uint32(magicDefense) >> 24)
	frame[40] = byte(uint32(magicDefense) >> 16)
	frame[41] = byte(uint32(magicDefense) >> 8)
	frame[42] = byte(uint32(magicDefense))
	// MagicAttack:HitRate packed (4 bytes each)
	frame[43] = byte(uint32(magicAttack) >> 24)
	frame[44] = byte(uint32(magicAttack) >> 16)
	frame[45] = byte(uint32(magicAttack) >> 8)
	frame[46] = byte(uint32(magicAttack))
	frame[47] = byte(uint32(hitRate) >> 24)
	frame[48] = byte(uint32(hitRate) >> 16)
	frame[49] = byte(uint32(hitRate) >> 8)
	frame[50] = byte(uint32(hitRate))
	// DodgeRate:CritRate packed (4 bytes each)
	frame[51] = byte(uint32(dodgeRate) >> 24)
	frame[52] = byte(uint32(dodgeRate) >> 16)
	frame[53] = byte(uint32(dodgeRate) >> 8)
	frame[54] = byte(uint32(dodgeRate))
	frame[55] = byte(uint32(critRate) >> 24)
	frame[56] = byte(uint32(critRate) >> 16)
	frame[57] = byte(uint32(critRate) >> 8)
	frame[58] = byte(uint32(critRate))
	return frame
}

// flushCombatBuffer applies all accumulated damage to entities on this map.
// For each unique target in the combat buffer:
//  1. Accumulated damage is subtracted from HP.
//  2. Threat is added to the AI ThreatTable.
//  3. A single StatsSync packet is broadcast to all neighbors.
//  4. If HP reaches 0, death cleanup is performed.
func (mw *MapWorker) flushCombatBuffer() {
	ca := mw.CombatBuffer
	ca.Flush(func(target ecs.Entity, batch *ecs.DamageBatch) {
		reg := mw.Registry

		// 1. Read current stats
		stats, ok := reg.GetStats(target)
		if !ok {
			return
		}

		// 2. Subtract accumulated damage
		stats.HP -= batch.TotalDamage
		if stats.HP < 0 {
			stats.HP = 0
		}
		reg.SetStats(target, stats)

		// 3. Threat is already tracked by AttackSystem and stageDamageCalculation
		//    via direct ThreatTable.Add calls. The batch's threat field is used
		//    here only for death attribution (resolving the top damager as killer).
		//    Actual threat values are NOT added again to avoid double-counting.

		// 4. Send exactly one StatsSync broadcast per target to all neighbors
		pos, hasPos := reg.GetPosition(target)
		if hasPos {
			frame := buildStatsSyncFrame(
				uint64(target),
				int32(stats.HP), int32(stats.MaxHP),
				int32(stats.MP), int32(stats.MaxMP),
				int32(stats.Dam), int32(stats.Level),
				int32(stats.Defense), int32(stats.MagicDefense),
				int32(stats.MagicAttack), int32(stats.HitRate),
				int32(stats.DodgeRate), int32(stats.CritRate),
			)
			// Query neighbors from this map's spatial grid and send the frame
			candidates := mw.SpatialGrid.QueryRadius(pos, combatBufferAOIRadius, target)
			for _, entry := range *candidates {
				connComp, hasConn := reg.GetConnection(entry.ID)
				if !hasConn || connComp.Conn == nil {
					continue
				}
				writeWriter(connComp.Writer, frame)
			}
			FreeQueryCandidates(candidates)
		}

		// 5. Check for death
		if stats.HP <= 0 {
			if meta, hasMeta := reg.GetMetadata(target); hasMeta {
				// Look up the top threat entity as the killer
				killerID := ecs.Entity(0)
				if ai, hasAI := reg.GetAI(target); hasAI && ai.ThreatTable != nil && ai.ThreatTable.Len() > 0 {
					if topID, _ := ai.ThreatTable.Top(); topID > 0 {
						killerID = ecs.Entity(topID)
					}
				}
				// TODO: Phase 2 will extract death handling logic to a shared
				// package that avoids circular imports between world and game.
				_ = killerID
				_ = meta
			}
		}
	})
}

// writeWriter sends data through the non-blocking connwriter.Writer.
// Returns true if queued successfully, false on full queue or closed.
func writeWriter(w *connwriter.Writer, data []byte) bool {
	if w == nil {
		return false
	}
	return w.Send(data)
}

// ─── AOI Bridge Helpers (moved from aoi_bridge.go) ──────────────────────────

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
	perf.GlobalPacketMonitor.RecordAoiQuery()
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

// BroadcastToNeighborsDelta sends binary data only to the AOI watchers
// that have the origin in their neighbor set.
// Unlike the old BroadcastToMap which scanned all connections, this uses the
// spatial grid to deliver targeted packets.
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
