// Package gmath provides zero-allocation spatial math primitives for the
// Minnsun's Adventure 2.5D world coordinate system.
//
// # Coordinate system
//
// The world uses a flat 2D grid (X, Z) with integer coordinates.
// Y-axis is purely cosmetic on the client — all server-side logic uses X and Z only.
// Map bounds are currently [0, 100] on both axes (see .clinerules zero-trust rules).
//
// # Why this package exists
//
// The same distance and bounds calculations appear in:
//   - game/ai_roaming.go  (leash radius, melee range, aggro, spawn radius)
//   - game/pickup.go      (pickup range 5.0 via world.IsInRange)
//   - world/proximity.go  (QueryRadius Euclidean filter)
//   - world/pathfinding.go (path cost heuristics)
//
// Without a shared primitive, each new system either copies the formula
// (risk of subtle divergence) or imports a package above its layer (import cycle).
// gmath sits at the lowest layer (no server/* imports) so anyone can import it.
//
// # Peak Go contract
//
// All functions are inlineable by the Go compiler (short, no branches on hot path).
// Zero heap allocations guaranteed.
package gmath

// ─── Distance ────────────────────────────────────────────────────────────────

// DistanceSq returns the squared Euclidean distance between two 2D integer
// points (ax, az) and (bx, bz).
//
// Use squared distance whenever you are comparing distances against a fixed
// radius — it avoids the expensive math.Sqrt call:
//
//	gmath.DistanceSq(ax, az, bx, bz) <= radius*radius  // instead of math.Sqrt(...) <= radius
func DistanceSq(ax, az, bx, bz int) float64 {
	dx := float64(ax - bx)
	dz := float64(az - bz)
	return dx*dx + dz*dz
}

// InRange reports whether the distance between (ax, az) and (bx, bz) is
// within `radius` world units (inclusive).
//
// Equivalent to: math.Sqrt(DistanceSq(ax,az,bx,bz)) <= radius
// but without the sqrt — pure multiplication.
func InRange(ax, az, bx, bz int, radius float64) bool {
	return DistanceSq(ax, az, bx, bz) <= radius*radius
}

// ─── Bounds ───────────────────────────────────────────────────────────────────

// InBounds reports whether both x and z fall within the closed interval [lo, hi].
// For the current map size: InBounds(x, z, 0, 100).
func InBounds(x, z, lo, hi int) bool {
	return x >= lo && x <= hi && z >= lo && z <= hi
}

// ─── Clamping ─────────────────────────────────────────────────────────────────

// Clamp constrains v to the closed interval [lo, hi].
//
// Used for loot scatter (clamp drop position to map edge) and movement
// validation (prevent out-of-bounds teleports).
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ClampPos constrains both x and z to [lo, hi] in a single call.
// Returns the clamped (x, z) pair.
func ClampPos(x, z, lo, hi int) (int, int) {
	return Clamp(x, lo, hi), Clamp(z, lo, hi)
}
