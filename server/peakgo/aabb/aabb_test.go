package aabb

import (
	"testing"
)

// ─── Box Tests ────────────────────────────────────────────────────────────────

func TestNewBoxNormalizes(t *testing.T) {
	b := NewBox(10, 5, -5, 20)
	if b.MinX != -5 || b.MaxX != 10 {
		t.Fatalf("expected MinX=-5, MaxX=10, got MinX=%d, MaxX=%d", b.MinX, b.MaxX)
	}
	if b.MinZ != 5 || b.MaxZ != 20 {
		t.Fatalf("expected MinZ=5, MaxZ=20, got MinZ=%d, MaxZ=%d", b.MinZ, b.MaxZ)
	}
}

func TestBoxContains(t *testing.T) {
	b := NewBox(0, 0, 10, 10)

	tests := []struct {
		x, z int
		want bool
	}{
		{5, 5, true},   // center
		{0, 0, true},   // min corner
		{10, 10, true}, // max corner
		{-1, 5, false}, // outside X min
		{11, 5, false}, // outside X max
		{5, -1, false}, // outside Z min
		{5, 11, false}, // outside Z max
	}

	for _, tt := range tests {
		got := b.Contains(tt.x, tt.z)
		if got != tt.want {
			t.Errorf("Contains(%d,%d) = %v, want %v", tt.x, tt.z, got, tt.want)
		}
	}
}

func TestBoxOverlaps(t *testing.T) {
	a := NewBox(0, 0, 10, 10)

	tests := []struct {
		name string
		box  Box
		want bool
	}{
		{"identical", NewBox(0, 0, 10, 10), true},
		{"touching edge", NewBox(10, 0, 20, 10), true},
		{"contained", NewBox(2, 2, 8, 8), true},
		{"overlap corner", NewBox(8, 8, 15, 15), true},
		{"no overlap", NewBox(11, 0, 20, 10), false},
		{"far away", NewBox(100, 100, 200, 200), false},
	}

	for _, tt := range tests {
		got := a.Overlaps(tt.box)
		if got != tt.want {
			t.Errorf("Overlaps(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestBoxIntersect(t *testing.T) {
	a := NewBox(0, 0, 10, 10)
	b := NewBox(5, 5, 15, 15)

	overlap := a.Intersect(b)
	if overlap.MinX != 5 || overlap.MinZ != 5 || overlap.MaxX != 10 || overlap.MaxZ != 10 {
		t.Fatalf("expected (5,5)-(10,10), got (%d,%d)-(%d,%d)",
			overlap.MinX, overlap.MinZ, overlap.MaxX, overlap.MaxZ)
	}

	// Non-overlapping boxes
	c := NewBox(20, 20, 30, 30)
	empty := a.Intersect(c)
	if empty.Width() <= 0 || empty.Depth() <= 0 {
		// Expected: empty result, MinX > MaxX or zero area
	} else {
		t.Fatal("expected empty intersection for non-overlapping boxes")
	}
}

func TestBoxUnion(t *testing.T) {
	a := NewBox(0, 0, 5, 5)
	b := NewBox(10, 10, 15, 15)

	u := a.Union(b)
	if u.MinX != 0 || u.MinZ != 0 || u.MaxX != 15 || u.MaxZ != 15 {
		t.Fatalf("expected (0,0)-(15,15), got (%d,%d)-(%d,%d)",
			u.MinX, u.MinZ, u.MaxX, u.MaxZ)
	}
}

func TestBoxCenter(t *testing.T) {
	b := NewBox(0, 0, 10, 10)
	cx, cz := b.Center()
	if cx != 5 || cz != 5 {
		t.Fatalf("expected (5,5), got (%d,%d)", cx, cz)
	}
}

func TestBoxArea(t *testing.T) {
	b := NewBox(0, 0, 10, 10)
	if b.Area() != 100 {
		t.Fatalf("expected 100, got %d", b.Area())
	}
}

func TestBoxContainsBox(t *testing.T) {
	outer := NewBox(0, 0, 20, 20)
	inner := NewBox(5, 5, 15, 15)
	partial := NewBox(15, 15, 25, 25)

	if !outer.ContainsBox(inner) {
		t.Fatal("outer should contain inner")
	}
	if outer.ContainsBox(partial) {
		t.Fatal("outer should not contain partial")
	}
}

// ─── Circle Tests ─────────────────────────────────────────────────────────────

func TestCircleContains(t *testing.T) {
	c := NewCircle(10, 10, 5)

	tests := []struct {
		x, z int
		want bool
	}{
		{10, 10, true},  // center
		{14, 10, true},  // edge (distance 4 ≤ 5)
		{15, 10, true},  // edge (distance 5 ≤ 5)
		{16, 10, false}, // outside (distance 6 > 5)
		{13, 13, false}, // diagonal outside (distance^2=18 > 25? 18 ≤ 25 -> true)
		{17, 10, false}, // far outside
	}

	// Fix: (13,13): dx=3, dz=3, distSq=18 <= 25 = true
	// Let me adjust expectation
	tests[4].want = true

	for _, tt := range tests {
		got := c.Contains(tt.x, tt.z)
		if got != tt.want {
			t.Errorf("Circle.Contains(%d,%d) = %v, want %v (distSq=%d, rSq=%d)",
				tt.x, tt.z, got, tt.want,
				(tt.x-c.X)*(tt.x-c.X)+(tt.z-c.Z)*(tt.z-c.Z),
				c.Radius*c.Radius)
		}
	}
}

func TestCircleOverlapsBox(t *testing.T) {
	c := NewCircle(5, 5, 5)
	b := NewBox(0, 0, 10, 10)

	if !c.OverlapsBox(b) {
		t.Fatal("circle centered inside box should overlap")
	}

	// Circle far away
	c2 := NewCircle(100, 100, 5)
	if c2.OverlapsBox(b) {
		t.Fatal("circle far away should not overlap")
	}

	// Circle touching box edge
	c3 := NewCircle(15, 5, 5)
	if !c3.OverlapsBox(b) {
		t.Fatal("circle touching box edge should overlap")
	}
}

func TestCircleBoundingBox(t *testing.T) {
	c := NewCircle(10, 10, 5)
	b := c.BoundingBox()
	if b.MinX != 5 || b.MinZ != 5 || b.MaxX != 15 || b.MaxZ != 15 {
		t.Fatalf("expected (5,5)-(15,15), got (%d,%d)-(%d,%d)",
			b.MinX, b.MinZ, b.MaxX, b.MaxZ)
	}
}

// ─── Ray Tests ────────────────────────────────────────────────────────────────

func TestRayIntersectsBox(t *testing.T) {
	// Ray through center of box
	r := NewRay(-10, 5, 20, 5)
	b := NewBox(0, 0, 10, 10)
	if !r.IntersectsBox(b) {
		t.Fatal("ray through box center should intersect")
	}

	// Ray missing box
	r2 := NewRay(-10, -10, 20, -10)
	if r2.IntersectsBox(b) {
		t.Fatal("ray below box should not intersect")
	}

	// Ray starting inside box
	r3 := NewRay(5, 5, 20, 5)
	if !r3.IntersectsBox(b) {
		t.Fatal("ray starting inside box should intersect")
	}

	// Ray ending inside box
	r4 := NewRay(-5, 5, 5, 5)
	if !r4.IntersectsBox(b) {
		t.Fatal("ray ending inside box should intersect")
	}
}

// ─── Benchmark Tests ──────────────────────────────────────────────────────────

func BenchmarkBoxContains(b *testing.B) {
	box := NewBox(0, 0, 100, 100)
	b.ResetTimer()
	for i := range b.N {
		box.Contains(i%100, (i/2)%100)
	}
}

func BenchmarkBoxOverlaps(b *testing.B) {
	a := NewBox(0, 0, 50, 50)
	other := NewBox(25, 25, 75, 75)
	b.ResetTimer()
	for range b.N {
		a.Overlaps(other)
	}
}

func BenchmarkCircleContains(b *testing.B) {
	c := NewCircle(50, 50, 25)
	b.ResetTimer()
	for i := range b.N {
		c.Contains(i%100, (i/2)%100)
	}
}

func BenchmarkCircleOverlapsBox(b *testing.B) {
	c := NewCircle(50, 50, 25)
	box := NewBox(0, 0, 100, 100)
	b.ResetTimer()
	for range b.N {
		c.OverlapsBox(box)
	}
}

func BenchmarkRayIntersectsBox(b *testing.B) {
	r := NewRay(-50, 25, 150, 25)
	box := NewBox(0, 0, 100, 100)
	b.ResetTimer()
	for range b.N {
		r.IntersectsBox(box)
	}
}
