// Package ecs provides ECS component storage and the CombatAccumulator for
// coalescing damage events during a single map tick.
package ecs

// ─── DamageBatch ───────────────────────────────────────────────────────────────

// DamageBatch accumulates all damage dealt to a single entity during one map tick.
type DamageBatch struct {
	TotalDamage int
	TotalThreat int64
}

// ─── CombatAccumulator ─────────────────────────────────────────────────────────

// CombatAccumulator buffers damage events during a map tick and flushes them in
// batch at the end, ensuring O(T) HP writes and O(T) broadcast packets per tick
// where T = number of unique targets (not attackers × targets).
//
// Memory: The batches map is cleared after every Flush call. The backing
// DamageBatch values are reused from a pooled slice to minimise GC pressure.
type CombatAccumulator struct {
	batches map[Entity]*DamageBatch
	pool    []DamageBatch
}

// NewCombatAccumulator creates an empty CombatAccumulator.
func NewCombatAccumulator() *CombatAccumulator {
	return &CombatAccumulator{
		batches: make(map[Entity]*DamageBatch, 64),
		pool:    make([]DamageBatch, 0, 64),
	}
}

// AddDamage buffers a damage + threat event for the given target.
// The damage and threat are NOT applied to the ECS registry until Flush is called.
//
// This is safe to call multiple times for the same target within a single tick —
// damage and threat are summed into the same batch.
func (ca *CombatAccumulator) AddDamage(target, source Entity, amount int, threatVal float64) {
	batch, ok := ca.batches[target]
	if !ok {
		// Reuse from pool if available, otherwise allocate.
		if len(ca.pool) > 0 {
			last := len(ca.pool) - 1
			batch = &ca.pool[last]
			ca.pool = ca.pool[:last]
			batch.TotalDamage = 0
			batch.TotalThreat = 0
		} else {
			batch = &DamageBatch{}
		}
		ca.batches[target] = batch
	}
	batch.TotalDamage += amount
	batch.TotalThreat += int64(threatVal)
}

// Flush applies all accumulated damage to the ECS registry.
// This method is called by MapWorker at the end of each tick.
// The caller provides a flush callback that handles the actual HP subtraction,
// broadcast, and death processing for each accumulated target.
// CurrentCombatBuffer is a global pointer to the map's active CombatAccumulator.
// The MapWorker sets this at the start of each tick, allowing DamageSystem
// and other game systems to route damage events into the buffer without
// needing a direct reference to the MapWorker.
//
// This is safe because each map runs on its own goroutine, so there is no
// concurrent read/write on this pointer during a single tick.
var CurrentCombatBuffer *CombatAccumulator

func (ca *CombatAccumulator) Flush(fn func(target Entity, batch *DamageBatch)) {
	if len(ca.batches) == 0 {
		return
	}

	for target, batch := range ca.batches {
		fn(target, batch)
		ca.recycleBatch(batch)
	}

	// Clear the map for the next tick
	for k := range ca.batches {
		delete(ca.batches, k)
	}
}

// recycleBatch clears a batch and returns it to the pool for reuse.
func (ca *CombatAccumulator) recycleBatch(batch *DamageBatch) {
	batch.TotalDamage = 0
	batch.TotalThreat = 0
	ca.pool = append(ca.pool, *batch)
}

// Len returns the number of unique targets currently buffered.
func (ca *CombatAccumulator) Len() int {
	return len(ca.batches)
}

// Free releases pooled resources held by the accumulator.
func (ca *CombatAccumulator) Free() {
	ca.batches = nil
	ca.pool = nil
}
