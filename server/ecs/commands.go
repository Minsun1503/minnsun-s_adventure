package ecs

import "sync"

// ─── Command Types ───────────────────────────────────────────────────────────
//
// CommandBuffer records deferred ECS mutations (Move, Damage, Spawn, Destroy)
// during a game tick and applies them sequentially at the end of the tick.
// This prevents race logic and mutation bugs caused by systems reading stale
// state while other systems write mid-tick.
//
// Each MapInstance gets its own CommandBuffer. The buffer is local to the map
// goroutine — no cross-map locking needed.
//
// Zero-alloc design: slices are pre-allocated on first use and reset (not
// re-allocated) each flush via Truncate(0). The sync.Pool provides size-class
// isolation for burst scenarios.

// MoveCommand records a deferred position update for an entity.
type MoveCommand struct {
	EntityID Entity
	MapID    int
	X, Z     int
}

// DamageCommand records a deferred damage application from source to target.
type DamageCommand struct {
	Source Entity
	Target Entity
	Amount int
}

// SpawnCommand records a deferred entity spawn.
type SpawnCommand struct {
	EntityID Entity
	Pos      PositionComponent
	Meta     MetadataComponent
	Stats    StatsComponent
}

// DestroyCommand records a deferred entity destruction.
type DestroyCommand struct {
	EntityID Entity
}

// SpatialGrid is the interface for spatial operations needed by CommandBuffer.
// The world package's GlobalSpatialGrid satisfies this interface without
// creating a circular dependency (world imports ecs; ecs does not import world).
type SpatialGrid interface {
	UpdateEntityPosition(id Entity, pos PositionComponent)
	RemoveEntity(id Entity)
}

// ─── CommandBuffer ───────────────────────────────────────────────────────────

// moveSlicePool recycles move command slices to avoid allocation churn.
var moveSlicePool = sync.Pool{
	New: func() any {
		s := make([]MoveCommand, 0, 16)
		return &s
	},
}

// damageSlicePool recycles damage command slices.
var damageSlicePool = sync.Pool{
	New: func() any {
		s := make([]DamageCommand, 0, 16)
		return &s
	},
}

// spawnSlicePool recycles spawn command slices.
var spawnSlicePool = sync.Pool{
	New: func() any {
		s := make([]SpawnCommand, 0, 8)
		return &s
	},
}

// destroySlicePool recycles destroy command slices.
var destroySlicePool = sync.Pool{
	New: func() any {
		s := make([]DestroyCommand, 0, 8)
		return &s
	},
}

// CommandBuffer records deferred ECS mutations during a game tick.
// Not safe for concurrent access — each Map owns its own CommandBuffer.
type CommandBuffer struct {
	moves    []MoveCommand
	damages  []DamageCommand
	spawns   []SpawnCommand
	destroys []DestroyCommand
}

// NewCommandBuffer creates a CommandBuffer with pooled backing slices.
func NewCommandBuffer() *CommandBuffer {
	return &CommandBuffer{
		moves:    *moveSlicePool.Get().(*[]MoveCommand),
		damages:  *damageSlicePool.Get().(*[]DamageCommand),
		spawns:   *spawnSlicePool.Get().(*[]SpawnCommand),
		destroys: *destroySlicePool.Get().(*[]DestroyCommand),
	}
}

// AddMove queues a position update for deferred execution.
func (cb *CommandBuffer) AddMove(entityID Entity, mapID int, x, z int) {
	cb.moves = append(cb.moves, MoveCommand{
		EntityID: entityID,
		MapID:    mapID,
		X:        x,
		Z:        z,
	})
}

// AddDamage queues a damage application for deferred execution.
func (cb *CommandBuffer) AddDamage(source, target Entity, amount int) {
	cb.damages = append(cb.damages, DamageCommand{
		Source: source,
		Target: target,
		Amount: amount,
	})
}

// AddSpawn queues an entity spawn for deferred execution.
func (cb *CommandBuffer) AddSpawn(entityID Entity, pos PositionComponent, meta MetadataComponent, stats StatsComponent) {
	cb.spawns = append(cb.spawns, SpawnCommand{
		EntityID: entityID,
		Pos:      pos,
		Meta:     meta,
		Stats:    stats,
	})
}

// AddDestroy queues an entity destruction for deferred execution.
func (cb *CommandBuffer) AddDestroy(entityID Entity) {
	cb.destroys = append(cb.destroys, DestroyCommand{
		EntityID: entityID,
	})
}

// Len returns the total number of pending commands across all queues.
func (cb *CommandBuffer) Len() int {
	return len(cb.moves) + len(cb.damages) + len(cb.spawns) + len(cb.destroys)
}

// Flush applies all queued commands sequentially to DefaultRegistry and the
// spatial grid, then resets all buffers for reuse in the next tick.
//
// Execution order is deterministic: Moves → Damages → Spawns → Destroys.
// This guarantees that:
//   - Moves are applied before damage (correct combat position)
//   - Spawns happen before destroys (no spawn-then-immediately-destroy race)
//   - Single pass: each command is applied and the buffer is cleared atomically
//
// The grid parameter must be the world package's GlobalSpatialGrid or any
// implementation of the SpatialGrid interface. This avoids a circular import
// (world imports ecs; ecs cannot import world).
func (cb *CommandBuffer) Flush(grid SpatialGrid) {
	// Phase 1: Move commands — update positions and spatial grid
	r := DefaultRegistry
	for _, cmd := range cb.moves {
		pos := PositionComponent{
			MapID: cmd.MapID,
			X:     cmd.X,
			Z:     cmd.Z,
		}
		r.positions.Set(cmd.EntityID, pos)
		grid.UpdateEntityPosition(cmd.EntityID, pos)
	}

	// Phase 2: Damage commands — apply HP reduction
	for _, cmd := range cb.damages {
		if stats, ok := r.stats.Get(cmd.Target); ok {
			stats.HP -= cmd.Amount
			if stats.HP < 0 {
				stats.HP = 0
			}
			r.stats.Set(cmd.Target, stats)
		}
	}

	// Phase 3: Spawn commands — register entity with all components
	for _, cmd := range cb.spawns {
		r.positions.Set(cmd.EntityID, cmd.Pos)
		r.metadata.Set(cmd.EntityID, cmd.Meta)
		r.stats.Set(cmd.EntityID, cmd.Stats)
		grid.UpdateEntityPosition(cmd.EntityID, cmd.Pos)
	}

	// Phase 4: Destroy commands — remove entity from registry and spatial grid
	for _, cmd := range cb.destroys {
		r.RemoveEntity(cmd.EntityID)
		grid.RemoveEntity(cmd.EntityID)
	}

	// Reset all slices (keep backing arrays for reuse — zero alloc on next tick)
	cb.moves = cb.moves[:0]
	cb.damages = cb.damages[:0]
	cb.spawns = cb.spawns[:0]
	cb.destroys = cb.destroys[:0]
}

// Free returns pooled slices to their respective pools.
// Call this after Flush if the CommandBuffer will be long-lived and idle,
// to return memory to the pool. Not required between ticks — just reslice.
func (cb *CommandBuffer) Free() {
	moveSlicePool.Put(&cb.moves)
	damageSlicePool.Put(&cb.damages)
	spawnSlicePool.Put(&cb.spawns)
	destroySlicePool.Put(&cb.destroys)
	cb.moves = nil
	cb.damages = nil
	cb.spawns = nil
	cb.destroys = nil
}
