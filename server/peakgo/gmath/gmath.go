// Package gmath provides zero-allocation, ultra-fast spatial math primitives
// optimized for the Minnsun's Adventure 2.5D grid-based world coordinate system.
//
// # Coordinate System
//
// The world leverages a flat 2D grid utilizing integer coordinates (X, Z).
// The Y-axis is purely cosmetic on the client side; all critical server-side
// calculations (combat ranges, movement validation, pathfinding) rely exclusively
// on X and Z. Map bounds are currently set to the closed interval [0, 100] on both axes.
//
// # Peak Go Contract
//
// Every single primitive function in this package is strictly designed to be inlineable
// by the Go compiler (short execution paths, no complex branching, zero interface boxing).
// Generates exactly 0 heap allocations/op on the hot-path.
package gmath

// ─── Basic Math Utilities ────────────────────────────────────────────────────

// Abs returns the absolute value of an integer.
// Highly useful across AI systems, pathfinding heuristics, and combat logic.
func Abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ─── Distance Calculations ───────────────────────────────────────────────────

// DistanceSq returns the squared Euclidean distance between two 2D integer points
// (ax, az) and (bx, bz) using pure integer arithmetic.
//
// Optimized: Replaced float64 returns with int to completely eliminate floating-point
// overhead and unnecessary type casting on the hot path.
func DistanceSq(ax, az, bx, bz int) int {
	dx := ax - bx
	dz := az - bz
	return dx*dx + dz*dz
}

// ManhattanDistance calculates the taxicab/Manhattan distance between two 2D integer points.
// Essential for grid-based navigation, chunk indexing, and A* pathfinding heuristics.
func ManhattanDistance(ax, az, bx, bz int) int {
	return Abs(ax-bx) + Abs(az-bz)
}

// InRange reports whether the distance between (ax, az) and (bx, bz) is within
// the specified floating-point `radius` world units (inclusive).
func InRange(ax, az, bx, bz int, radius float64) bool {
	return float64(DistanceSq(ax, az, bx, bz)) <= radius*radius
}

// InRangeInt checks proximity using pure integer bounds, preventing float conversions entirely.
// Perfect for fixed hot-path range checks such as melee range or aggro radius.
//
// Typical usage:
//
//	if gmath.InRangeInt(player.X, player.Z, monster.X, monster.Z, 5) {
//	    // Execution path for melee attack range
//	}
func InRangeInt(ax, az, bx, bz, radius int) bool {
	return DistanceSq(ax, az, bx, bz) <= radius*radius
}

// ─── Map Bounds Validation ───────────────────────────────────────────────────

// InBounds reports whether both x and z coordinates fall within the closed interval [lo, hi].
// For standard zero-trust validation: gmath.InBounds(x, z, 0, 100).
func InBounds(x, z, lo, hi int) bool {
	return x >= lo && x <= hi && z >= lo && z <= hi
}

// ─── Clamping Helpers ────────────────────────────────────────────────────────

// Clamp limits an integer v to the closed interval [lo, hi].
// Extensively utilized to bind loot scattering and movement limits smoothly.
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClampPos clamps both x and z coordinates to the interval [lo, hi] simultaneously.
// Returns the safely constrained (x, z) coordinate tuple.
func ClampPos(x, z, lo, hi int) (int, int) {
	return Clamp(x, lo, hi), Clamp(z, lo, hi)
}
