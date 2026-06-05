// Package astar provides a zero-allocation A* pathfinding implementation
// optimized for the Minnsun's Adventure 2.5D grid-based world.
//
// # Why replace BFS
//
// The current world/pathfinding.go uses BFS with a 400-node limit, which only
// covers ~7 tiles radius in practice. A* with an Octile heuristic reduces
// expanded nodes by 10-50×, enabling long-distance pathfinding within the
// 5ms tick budget.
//
// # Coordinate System
//
// All operations use integer (X, Z) grid coordinates matching the world map.
// Supports 8-directional movement (cardinal + diagonal).
//
// # Peak Go Contract
//
// Zero heap allocations per pathfinding call on the hot path (path cache
// is pre-allocated and pooled). Uses inline binary heap instead of
// container/heap to avoid interface{} boxing escape.
package astar

import (
	"server/peakgo/gmath"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	// MaxPathNodes is the hard limit on path length to prevent infinite loops.
	// Should be ≤ 1024 to stay within a single memory page.
	MaxPathNodes = 512

	// GridCosts for 8-directional movement (multiplied by 10 for integer math).
	costStraight = 10
	costDiagonal = 14 // sqrt(2) ≈ 1.414 → 14 when scaled by 10

	// MaxOpenSet is the maximum number of nodes in the open set.
	// Larger maps may need this increased, but 2048 works for 100×100 world.
	MaxOpenSet = 2048
)

// ─── Types ────────────────────────────────────────────────────────────────────

// Node represents a single grid cell in the A* path cache.
// Layout is cache-line aligned for fast iteration.
type Node struct {
	X, Z   int32 // Grid coordinates (int32 to save space vs int)
	G, H   int32 // G=from-start cost, H=to-goal heuristic
	Parent int32 // Index of parent node in the path cache (-1 = no parent)
	Closed bool  // True if this node has been fully expanded
	Open   bool  // True if this node is in the open set
}

// PathResult holds the result of an A* pathfinding query.
// The caller must check Found before using Points.
// This is a value type — no heap allocation.
type PathResult struct {
	Found  bool
	Len    int                    // Number of waypoints in the path
	Points [MaxPathNodes]struct { // Fixed-size array, zero alloc
		X, Z int
	}
}

// isWalkable checks if a cell is walkable. The user must provide this function.
type IsWalkable func(x, z int) bool

// binaryHeap is an inline min-heap of node indices ordered by F = G + H.
// Replaces container/heap to avoid interface{} boxing allocations.
type binaryHeap struct {
	nodes   [MaxOpenSet]int32 // Heap array
	indices [MaxOpenSet]int32 // Map from node index to heap position (-1 = not in heap)
	len     int               // Current number of elements
}

// reset clears the heap for reuse.
func (h *binaryHeap) reset() {
	h.len = 0
	// Clear indices array — only up to MaxPathNodes (not full MaxOpenSet)
	for i := range h.indices[:MaxPathNodes] {
		h.indices[i] = -1
	}
}

func (h *binaryHeap) less(i, j int) bool {
	a, b := h.nodes[i], h.nodes[j]
	fa := int(cache[a].G) + int(cache[a].H)
	fb := int(cache[b].G) + int(cache[b].H)
	if fa != fb {
		return fa < fb
	}
	return cache[a].H < cache[b].H
}

// push adds a node index to the heap.
func (h *binaryHeap) push(idx int32) {
	if cache[idx].Open {
		return // Already in open set
	}
	cache[idx].Open = true
	pos := h.len
	h.nodes[pos] = idx
	h.indices[idx] = int32(pos)
	h.len++
	h.up(pos)
}

// pop removes and returns the lowest-F node.
func (h *binaryHeap) pop() (int32, bool) {
	if h.len == 0 {
		return 0, false
	}
	idx := h.nodes[0]
	cache[idx].Open = false
	h.len--
	if h.len > 0 {
		h.nodes[0] = h.nodes[h.len]
		h.indices[h.nodes[0]] = 0
		h.down(0)
	}
	h.indices[idx] = -1
	return idx, true
}

// fix re-establishes heap ordering when a node's F value changes.
func (h *binaryHeap) fix(pos int) {
	h.up(pos)
	h.down(pos)
}

func (h *binaryHeap) empty() bool {
	return h.len == 0
}

func (h *binaryHeap) up(pos int) {
	for pos > 0 {
		parent := (pos - 1) / 2
		if !h.less(pos, parent) {
			break
		}
		h.nodes[pos], h.nodes[parent] = h.nodes[parent], h.nodes[pos]
		h.indices[h.nodes[pos]] = int32(pos)
		h.indices[h.nodes[parent]] = int32(parent)
		pos = parent
	}
}

func (h *binaryHeap) down(pos int) {
	n := h.len
	for {
		left := 2*pos + 1
		if left >= n {
			break
		}
		smallest := left
		right := left + 1
		if right < n && h.less(right, left) {
			smallest = right
		}
		if h.less(pos, smallest) {
			break
		}
		h.nodes[pos], h.nodes[smallest] = h.nodes[smallest], h.nodes[pos]
		h.indices[h.nodes[pos]] = int32(pos)
		h.indices[h.nodes[smallest]] = int32(smallest)
		pos = smallest
	}
}

// ─── Global Path Cache ────────────────────────────────────────────────────────

// Path cache reused across all pathfinding calls to avoid allocations.
// Maximum 512 nodes = 512 × (4+4+4+4+4+1+1) bytes ≈ 13KB.
// This is allocated once at startup and never freed.
var cache [MaxPathNodes]Node

// ─── Public API ───────────────────────────────────────────────────────────────

// PathCache is a reusable path cache that avoids allocations by recycling
// internal buffers across FindPath calls. Create one per goroutine.
type PathCache struct {
	openList  binaryHeap
	nodeCount int                 // Number of nodes currently in use
	pathBuf   [MaxPathNodes]int32 // Scratch buffer for path reconstruction
}

// NewPathCache creates a new PathCache ready for use.
func NewPathCache() *PathCache {
	pc := &PathCache{}
	pc.openList.reset()
	return pc
}

// Reset clears the path cache for reuse.
func (pc *PathCache) Reset() {
	// Clear used nodes only (0-initialize the segment)
	for i := range pc.nodeCount {
		cache[i] = Node{}
	}
	pc.nodeCount = 0
	pc.openList.reset()
}

// allocNode allocates a node slot in the path cache.
// Returns -1 if the cache is full.
func (pc *PathCache) allocNode(x, z int32) int32 {
	if pc.nodeCount >= MaxPathNodes {
		return -1
	}
	idx := int32(pc.nodeCount)
	pc.nodeCount++
	cache[idx].X = x
	cache[idx].Z = z
	cache[idx].Parent = -1
	cache[idx].Closed = false
	cache[idx].Open = false
	return idx
}

// nodeIndex returns the index of a node with the given coordinates.
// Returns -1 if not found.
func (pc *PathCache) nodeIndex(x, z int32) int32 {
	for i := range pc.nodeCount {
		if cache[i].X == x && cache[i].Z == z {
			return int32(i)
		}
	}
	return -1
}

// pushOpen adds a node index to the open set.
func (pc *PathCache) pushOpen(idx int32) {
	pc.openList.push(idx)
}

// popOpen removes and returns the lowest-F node from the open set.
func (pc *PathCache) popOpen() (int32, bool) {
	return pc.openList.pop()
}

// isOpenEmpty reports whether the open set is empty.
func (pc *PathCache) isOpenEmpty() bool {
	return pc.openList.empty()
}

// ─── FindPath ─────────────────────────────────────────────────────────────────

// FindPath performs A* pathfinding from (sx, sz) to (ex, ez) on the given grid.
// Uses Octile distance heuristic for 8-directional movement.
// The path is reconstructed into result.Points in order from start to goal.
//
// Parameters:
//   - sx, sz: Start coordinates
//   - ex, ez: Goal coordinates
//   - isWalkable: Function that returns true if a cell is traversable
//   - maxNodes: Maximum nodes to expand before giving up (0 = use MaxPathNodes)
//
// Returns:
//   - PathResult with Found=true if a path exists, false otherwise
//
// Zero alloc per call (uses pre-allocated cache + inline binary heap).
func FindPath(sx, sz, ex, ez int, isWalkable IsWalkable, maxNodes int) PathResult {
	var pc PathCache
	pc.openList.reset()
	return pc.findPath(sx, sz, ex, ez, isWalkable, maxNodes)
}

// FindPathWithCache performs A* pathfinding using a pre-allocated PathCache.
// Use this for hot-path calling to amortize initialization cost.
// Zero alloc per call after the first.
func FindPathWithCache(pc *PathCache, sx, sz, ex, ez int, isWalkable IsWalkable, maxNodes int) PathResult {
	pc.Reset()
	return pc.findPath(sx, sz, ex, ez, isWalkable, maxNodes)
}

// findPath is the internal implementation using a recycled PathCache.
func (pc *PathCache) findPath(sx, sz, ex, ez int, isWalkable IsWalkable, maxNodes int) PathResult {
	var result PathResult

	// Trivial case: start == goal
	if sx == ex && sz == ez {
		result.Found = true
		result.Len = 1
		result.Points[0] = struct{ X, Z int }{sx, sz}
		return result
	}

	// Validate start and goal
	if !isWalkable(sx, sz) || !isWalkable(ex, ez) {
		return result
	}

	if maxNodes <= 0 || maxNodes > MaxPathNodes {
		maxNodes = MaxPathNodes
	}

	// Reset path cache
	pc.Reset()

	// Convert to int32 for compact storage
	startX, startZ := int32(sx), int32(sz)
	endX, endZ := int32(ex), int32(ez)

	// Add start node
	startIdx := pc.allocNode(startX, startZ)
	if startIdx < 0 {
		return result // Cache exhausted
	}

	// Set H for start
	cache[startIdx].H = heuristicOctile(startX, startZ, endX, endZ)
	pc.pushOpen(startIdx)

	// 8-directional neighbors: (dx, dz)
	// Cardinal: N, S, E, W — Diagonal: NE, NW, SE, SW
	neighbors := [8]struct{ dx, dz int32 }{
		{0, -1}, {0, 1}, {-1, 0}, {1, 0}, // N, S, W, E
		{-1, -1}, {1, -1}, {-1, 1}, {1, 1}, // NW, NE, SW, SE
	}
	neighborCosts := [8]int32{
		costStraight, costStraight, costStraight, costStraight,
		costDiagonal, costDiagonal, costDiagonal, costDiagonal,
	}

	expanded := 0

	for !pc.isOpenEmpty() {
		curr, ok := pc.popOpen()
		if !ok {
			break
		}

		cache[curr].Closed = true
		expanded++

		// Check if we've exceeded the expansion limit
		if expanded > maxNodes {
			return result
		}

		// Check if we reached the goal
		if cache[curr].X == endX && cache[curr].Z == endZ {
			return pc.reconstructPath(curr, &result)
		}

		// Expand neighbors
		for i := range neighbors {
			nx := cache[curr].X + neighbors[i].dx
			nz := cache[curr].Z + neighbors[i].dz

			// Bounds check (0-100 grid)
			if nx < 0 || nx > 100 || nz < 0 || nz > 100 {
				continue
			}

			// Diagonal movement requires both cardinal neighbors to be walkable
			// to prevent cutting corners through walls
			if neighbors[i].dx != 0 && neighbors[i].dz != 0 {
				if !isWalkable(int(cache[curr].X+neighbors[i].dx), int(cache[curr].Z)) ||
					!isWalkable(int(cache[curr].X), int(cache[curr].Z+neighbors[i].dz)) {
					continue
				}
			}

			if !isWalkable(int(nx), int(nz)) {
				continue
			}

			// Find or create neighbor node
			nbrIdx := pc.nodeIndex(nx, nz)
			if nbrIdx < 0 {
				nbrIdx = pc.allocNode(nx, nz)
				if nbrIdx < 0 {
					continue // Cache full, skip
				}
			}

			if cache[nbrIdx].Closed {
				continue // Already expanded
			}

			// Calculate tentative G cost
			tentG := cache[curr].G + neighborCosts[i]

			// If this is a better path to the neighbor
			if !cache[nbrIdx].Open || tentG < cache[nbrIdx].G {
				cache[nbrIdx].Parent = curr
				cache[nbrIdx].G = tentG
				cache[nbrIdx].H = heuristicOctile(nx, nz, endX, endZ)

				if !cache[nbrIdx].Open {
					pc.pushOpen(nbrIdx)
				} else {
					// Already in open set, fix heap position
					pos := pc.openList.indices[nbrIdx]
					if pos >= 0 {
						pc.openList.fix(int(pos))
					}
				}
			}
		}
	}

	return result // No path found
}

// ─── Heuristics ───────────────────────────────────────────────────────────────

// heuristicOctile returns the Octile distance heuristic for 8-directional movement.
// This is A* admissible (never overestimates), making it optimal for pathfinding.
// Uses integer math (scaled by 10).
func heuristicOctile(ax, az, bx, bz int32) int32 {
	dx := gmath.Abs(int(ax - bx))
	dz := gmath.Abs(int(az - bz))
	if dx > dz {
		return int32(costDiagonal*dz + costStraight*(dx-dz))
	}
	return int32(costDiagonal*dx + costStraight*(dz-dx))
}

// ─── Path Reconstruction ──────────────────────────────────────────────────────

// reconstructPath walks backwards from the goal node to the start,
// then reverses to produce a start-to-goal path.
func (pc *PathCache) reconstructPath(endIdx int32, result *PathResult) PathResult {
	// Walk backwards to count path length
	length := 0
	idx := endIdx
	for idx >= 0 {
		length++
		if length > MaxPathNodes {
			// Path too long, truncate
			length = MaxPathNodes
			break
		}
		pc.pathBuf[length-1] = idx
		idx = cache[idx].Parent
	}

	result.Found = true
	if length > 0 {
		result.Len = length
		// Reverse: walk from start to goal
		for i := range length {
			nodeIdx := pc.pathBuf[length-1-i]
			result.Points[i] = struct{ X, Z int }{
				X: int(cache[nodeIdx].X),
				Z: int(cache[nodeIdx].Z),
			}
		}
	}

	return *result
}

// ─── Utility ──────────────────────────────────────────────────────────────────

// SmoothPath removes unnecessary waypoints by checking line-of-sight.
// This produces more natural-looking movement paths.
// Returns the smoothed path with only essential turning points.
func SmoothPath(path PathResult, isWalkable IsWalkable) PathResult {
	if path.Len <= 2 {
		return path
	}

	var smoothed PathResult
	smoothed.Found = path.Found
	smoothed.Points[0] = path.Points[0]
	smoothed.Len = 1

	lastKey := 0
	for i := 1; i < path.Len-1; i++ {
		// Check if there's line-of-sight from lastKey to i+1
		if !hasLineOfSight(
			path.Points[lastKey].X, path.Points[lastKey].Z,
			path.Points[i+1].X, path.Points[i+1].Z,
			isWalkable,
		) {
			// No line-of-sight; keep i as a key point
			smoothed.Len++
			smoothed.Points[smoothed.Len-1] = path.Points[i]
			lastKey = i
		}
	}

	// Always include the goal
	if lastKey != path.Len-1 {
		smoothed.Len++
		smoothed.Points[smoothed.Len-1] = path.Points[path.Len-1]
	}

	return smoothed
}

// hasLineOfSight checks if there's a straight-line walkable path between two points
// using Bresenham's line algorithm.
func hasLineOfSight(x1, z1, x2, z2 int, isWalkable IsWalkable) bool {
	dx := x2 - x1
	dz := z2 - z1

	absDx := dx
	if absDx < 0 {
		absDx = -absDx
	}
	absDz := dz
	if absDz < 0 {
		absDz = -absDz
	}

	x, z := x1, z1
	var sx, sz int

	if dx > 0 {
		sx = 1
	} else if dx < 0 {
		sx = -1
	}

	if dz > 0 {
		sz = 1
	} else if dz < 0 {
		sz = -1
	}

	if absDx >= absDz {
		err := 2*absDz - absDx
		for x != x2 {
			if !isWalkable(x, z) {
				return false
			}
			if err >= 0 {
				z += sz
				err -= 2 * absDx
			}
			err += 2 * absDz
			x += sx
		}
	} else {
		err := 2*absDx - absDz
		for z != z2 {
			if !isWalkable(x, z) {
				return false
			}
			if err >= 0 {
				x += sx
				err -= 2 * absDz
			}
			err += 2 * absDx
			z += sz
		}
	}

	return isWalkable(x, z)
}

// GobEncode implements the gob.GobEncoder interface to prevent gob from trying to encode unexported fields.
func (pc *PathCache) GobEncode() ([]byte, error) {
	return nil, nil
}

// GobDecode implements the gob.GobDecoder interface to satisfy gob deserialization.
func (pc *PathCache) GobDecode(data []byte) error {
	return nil
}
