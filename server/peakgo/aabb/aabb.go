// Package aabb provides zero-allocation Axis-Aligned Bounding Box primitives
// optimized for the Minnsun's Adventure 2.5D grid-based collision and spatial queries.
//
// # Coordinate System
//
// All AABB operations use integer (X, Z) coordinates matching the world grid.
// The Y-axis is cosmetic on the client side; critical server-side collision
// (combat ranges, skill AoE, movement validation, spawn zones) relies on X and Z.
//
// # Why this package exists
//
// Raw distance checks are insufficient for area-of-effect skills, rectangular
// spawn zones, line-of-sight corridors, or bounding-box overlap detection.
// This package provides typed AABB primitives with zero alloc hot-path queries.
//
// # Peak Go Contract
//
// Every primitive in this package is inlineable by the Go compiler.
// Generates exactly 0 heap allocations/op on the hot-path.
package aabb

// ─── Box ──────────────────────────────────────────────────────────────────────

// Box represents an axis-aligned bounding box on the 2D integer grid.
// Stored as min/max coordinates for fast overlap/rejection tests.
// Stored as an inline value — copy-modify-overwrite like all ECS components.
type Box struct {
	MinX, MinZ int
	MaxX, MaxZ int
}

// NewBox creates a Box from two corner points, normalizing to ensure
// Min ≤ Max on both axes regardless of input order.
func NewBox(x1, z1, x2, z2 int) Box {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if z1 > z2 {
		z1, z2 = z2, z1
	}
	return Box{
		MinX: x1, MinZ: z1,
		MaxX: x2, MaxZ: z2,
	}
}

// NewBoxFromCenter creates a Box centered at (cx, cz) with the given half-extents.
// Useful for AoE skills, splash damage, and spawn areas defined by radius.
func NewBoxFromCenter(cx, cz, halfX, halfZ int) Box {
	return Box{
		MinX: cx - halfX, MinZ: cz - halfZ,
		MaxX: cx + halfX, MaxZ: cz + halfZ,
	}
}

// Width returns the width of the box along the X axis.
func (b Box) Width() int { return b.MaxX - b.MinX }

// Depth returns the depth of the box along the Z axis.
func (b Box) Depth() int { return b.MaxZ - b.MinZ }

// Contains reports whether the point (x, z) lies inside (or on the edge of) the box.
// O(1), zero alloc.
func (b Box) Contains(x, z int) bool {
	return x >= b.MinX && x <= b.MaxX &&
		z >= b.MinZ && z <= b.MaxZ
}

// ContainsBox reports whether this box fully contains the other box.
// O(1), zero alloc.
func (b Box) ContainsBox(other Box) bool {
	return b.MinX <= other.MinX && b.MaxX >= other.MaxX &&
		b.MinZ <= other.MinZ && b.MaxZ >= other.MaxZ
}

// Overlaps reports whether this box overlaps with the other box (edge inclusive).
// O(1), zero alloc.
func (b Box) Overlaps(other Box) bool {
	return b.MinX <= other.MaxX && b.MaxX >= other.MinX &&
		b.MinZ <= other.MaxZ && b.MaxZ >= other.MinZ
}

// Intersect returns the overlapping region of two boxes.
// Returns a zero-value Box (MinX > MaxX) if they do not overlap.
// Check with Overlaps() first, or verify Width() > 0 && Depth() > 0.
// O(1), zero alloc.
func (b Box) Intersect(other Box) Box {
	return Box{
		MinX: max(b.MinX, other.MinX),
		MinZ: max(b.MinZ, other.MinZ),
		MaxX: min(b.MaxX, other.MaxX),
		MaxZ: min(b.MaxZ, other.MaxZ),
	}
}

// Union returns the smallest box that contains both this box and the other.
// O(1), zero alloc.
func (b Box) Union(other Box) Box {
	return Box{
		MinX: min(b.MinX, other.MinX),
		MinZ: min(b.MinZ, other.MinZ),
		MaxX: max(b.MaxX, other.MaxX),
		MaxZ: max(b.MaxZ, other.MaxZ),
	}
}

// Center returns the midpoint of the box (integer-rounded toward min).
func (b Box) Center() (int, int) {
	return (b.MinX + b.MaxX) / 2, (b.MinZ + b.MaxZ) / 2
}

// Area returns the area of the box (width × depth).
func (b Box) Area() int {
	return b.Width() * b.Depth()
}

// ─── Circle ────────────────────────────────────────────────────────────────────

// Circle represents a circular area on the 2D integer grid, defined by center and radius.
// Used for splash damage, aggro ranges, and proximity queries.
type Circle struct {
	X, Z   int
	Radius int
}

// NewCircle creates a Circle at (cx, cz) with the given integer radius.
func NewCircle(cx, cz, radius int) Circle {
	return Circle{X: cx, Z: cz, Radius: radius}
}

// Contains reports whether the point (x, z) lies inside (or on the edge of) the circle.
// Uses squared distance to avoid sqrt. O(1), zero alloc.
func (c Circle) Contains(x, z int) bool {
	dx := x - c.X
	dz := z - c.Z
	return dx*dx+dz*dz <= c.Radius*c.Radius
}

// OverlapsBox reports whether this circle overlaps with the given AABB.
// Uses the separating axis theorem for rectangle-circle intersection.
// O(1), zero alloc.
func (c Circle) OverlapsBox(b Box) bool {
	// Find the closest point on the box to the circle center
	nx := clamp(c.X, b.MinX, b.MaxX)
	nz := clamp(c.Z, b.MinZ, b.MaxZ)
	dx := c.X - nx
	dz := c.Z - nz
	return dx*dx+dz*dz <= c.Radius*c.Radius
}

// BoundingBox returns the smallest AABB that fully contains this circle.
// O(1), zero alloc.
func (c Circle) BoundingBox() Box {
	return Box{
		MinX: c.X - c.Radius, MinZ: c.Z - c.Radius,
		MaxX: c.X + c.Radius, MaxZ: c.Z + c.Radius,
	}
}

// ─── Ray ───────────────────────────────────────────────────────────────────────

// Ray represents a line segment from (X1, Z1) to (X2, Z2).
// Used for line-of-sight checks, skill projectiles, and charge movements.
type Ray struct {
	X1, Z1 int
	X2, Z2 int
}

// NewRay creates a Ray from two endpoint coordinates.
func NewRay(x1, z1, x2, z2 int) Ray {
	return Ray{X1: x1, Z1: z1, X2: x2, Z2: z2}
}

// IntersectsBox reports whether this ray segment intersects the given AABB.
// Uses the slab method for ray-AABB intersection.
// O(1), zero alloc.
func (r Ray) IntersectsBox(b Box) bool {
	dx := r.X2 - r.X1
	dz := r.Z2 - r.Z1

	// Compute intersection parameters for each slab
	tMin := float64(-1e9)
	tMax := float64(1e9)

	if dx != 0 {
		t1 := float64(b.MinX-r.X1) / float64(dx)
		t2 := float64(b.MaxX-r.X1) / float64(dx)
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tMin {
			tMin = t1
		}
		if t2 < tMax {
			tMax = t2
		}
	} else {
		// Ray is parallel to X axis — check if within slab
		if r.X1 < b.MinX || r.X1 > b.MaxX {
			return false
		}
	}

	if dz != 0 {
		t1 := float64(b.MinZ-r.Z1) / float64(dz)
		t2 := float64(b.MaxZ-r.Z1) / float64(dz)
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		if t1 > tMin {
			tMin = t1
		}
		if t2 < tMax {
			tMax = t2
		}
	} else {
		if r.Z1 < b.MinZ || r.Z1 > b.MaxZ {
			return false
		}
	}

	return tMin <= tMax && tMax >= 0 && tMin <= 1
}

// ─── Internal helpers ──────────────────────────────────────────────────────────

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// clamp restricts v to the closed interval [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Required for go 1.23 compatibility if max/min are not builtins.
// If go 1.26+ already has them, these will shadow harmlessly.
