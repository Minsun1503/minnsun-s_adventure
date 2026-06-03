package anticheat

import (
	"server/ecs"
	"server/peakgo/gmath"
	"server/peakgo/pool"
)

// ViolationType categorizes each anti-cheat hit.
type ViolationType uint8

const (
	ViolNone           ViolationType = 0
	ViolSpeedHack      ViolationType = 1
	ViolMapMismatch    ViolationType = 2
	ViolPacketSequence ViolationType = 3
	ViolActionRange    ViolationType = 4
)

// Violation records a single anti-cheat violation.
type Violation struct {
	Type  ViolationType
	Tick  uint64
	Value int32 // context-dependent
}

// violationPool caches slices for zero-alloc violation tracking.
var violationPool = pool.NewSlicePool[Violation](4)

const seqWindowSize = 32

// Validator performs per-connection anti-cheat validation.
// Designed for zero-alloc hot-path checking.
type Validator struct {
	lastMoveTick   uint64
	lastMoveMapID  int
	lastMoveX      int
	lastMoveZ      int
	seqWindow      [seqWindowSize]uint32 // sliding window of recent seq nums
	seqHead        int                   // current position in circular buffer
	violationCount int32
}

// ValidateMovement checks if a movement is valid (speed hack).
// Returns true if valid, false = cheating detected.
func (v *Validator) ValidateMovement(pos, newPos ecs.PositionComponent, maxDistance int, currentTick uint64) bool {
	// Basic bounds check
	if !gmath.InRangeInt(pos.X, pos.Z, newPos.X, newPos.Z, int(maxDistance)) {
		return false
	}
	v.lastMoveTick = currentTick
	v.lastMoveMapID = newPos.MapID
	v.lastMoveX = newPos.X
	v.lastMoveZ = newPos.Z
	return true
}

// ValidateSequence checks if a packet sequence number is valid (no replay/out-of-order).
// Uses a sliding window of recently seen seq numbers.
// currentSeq should be monotonically increasing per-connection.
func (v *Validator) ValidateSequence(seq uint32) bool {
	if seq == 0 {
		return true // seq 0 is always valid (initial packet)
	}
	// Check against window
	for i := 0; i < seqWindowSize; i++ {
		if v.seqWindow[i] == seq {
			return false // replay detected
		}
	}
	// Add to circular buffer
	v.seqWindow[v.seqHead] = seq
	v.seqHead = (v.seqHead + 1) % seqWindowSize
	return true
}

// ValidateAction checks if an action (attack, skill, etc.) is in range of a target.
func (v *Validator) ValidateAction(entityPos, targetPos ecs.PositionComponent, maxRange float64) bool {
	dx := float64(entityPos.X - targetPos.X)
	dz := float64(entityPos.Z - targetPos.Z)
	return dx*dx+dz*dz <= maxRange*maxRange
}

// ValidateMapAction checks if the entity is on the same map as the target.
func (v *Validator) ValidateMapAction(entityPos, targetPos ecs.PositionComponent) bool {
	return entityPos.MapID == targetPos.MapID
}

// RecordViolation increments the violation counter.
func (v *Validator) RecordViolation(typ ViolationType, tick uint64) {
	v.violationCount++
}

// ViolationCount returns the total number of violations recorded.
func (v *Validator) ViolationCount() int32 {
	return v.violationCount
}

// Reset clears the validator state.
func (v *Validator) Reset() {
	v.lastMoveTick = 0
	v.lastMoveMapID = 0
	v.lastMoveX = 0
	v.lastMoveZ = 0
	v.seqHead = 0
	v.violationCount = 0
	for i := range v.seqWindow {
		v.seqWindow[i] = 0
	}
}
