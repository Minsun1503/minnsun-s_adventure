package nav

import (
	"math"
	"server/peakgo/astar"
	"server/peakgo/config"
	"server/peakgo/pool"
)

// pathResultPool is a package-level pool for MultiFloorPath results.
var pathResultPool = pool.NewSlicePool[astar.PathResult](2)

// ZoneType describes how a zone behaves.
type ZoneType uint8

const (
	ZoneNormal     ZoneType = iota // standard walkable area
	ZoneSafe                       // no combat allowed
	ZoneDungeon                    // instance-like, separate map
	ZoneRestricted                 // needs key/quest to enter
)

// Zone defines a rectangular region on a map.
type Zone struct {
	ID          int
	MapID       int
	MinX, MinZ  int32
	MaxX, MaxZ  int32
	Type        ZoneType
	LinkedZones []int // portal connections
}

// Portal defines a connection between two points (potentially across maps).
type Portal struct {
	ID       int
	SrcMapID int
	SrcX     int32
	SrcZ     int32
	DstMapID int
	DstX     int32
	DstZ     int32
	MinLevel int32 // minimum level to use this portal
}

// NavMesh wraps astar with zone/portal awareness and multi-floor support.
type NavMesh struct {
	zones   map[int]*Zone
	portals []Portal
	cache   *astar.PathCache
}

// NewNavMesh creates a new NavMesh instance.
func NewNavMesh() *NavMesh {
	return &NavMesh{
		zones:   make(map[int]*Zone),
		portals: make([]Portal, 0, 8),
		cache:   astar.NewPathCache(),
	}
}

// RegisterZone adds a zone to the nav mesh.
func (nm *NavMesh) RegisterZone(z Zone) {
	nm.zones[z.ID] = &Zone{
		ID:          z.ID,
		MapID:       z.MapID,
		MinX:        z.MinX,
		MinZ:        z.MinZ,
		MaxX:        z.MaxX,
		MaxZ:        z.MaxZ,
		Type:        z.Type,
		LinkedZones: append([]int{}, z.LinkedZones...),
	}
}

// RegisterPortal adds a portal connection.
func (nm *NavMesh) RegisterPortal(p Portal) {
	nm.portals = append(nm.portals, p)
}

// GetZoneAt returns the zone at the given map position, nil if none.
func (nm *NavMesh) GetZoneAt(mapID int, x, z int32) *Zone {
	for _, zone := range nm.zones {
		if zone.MapID == mapID && x >= zone.MinX && x <= zone.MaxX && z >= zone.MinZ && z <= zone.MaxZ {
			return zone
		}
	}
	return nil
}

// IsWalkable checks if a tile is walkable, consulting zones.
func (nm *NavMesh) IsWalkable(mapID int, x, z int32) bool {
	// Check if this position has a walkable zone
	for _, zone := range nm.zones {
		if zone.MapID == mapID && x >= zone.MinX && x <= zone.MaxX && z >= zone.MinZ && z <= zone.MaxZ {
			if zone.Type == ZoneSafe || zone.Type == ZoneNormal || zone.Type == ZoneDungeon {
				return true
			}
			return false
		}
	}
	// Default: assume all tiles within map bounds are walkable
	cfg := config.C()
	return x >= cfg.MapBoundsMinX && x <= cfg.MapBoundsMaxX &&
		z >= cfg.MapBoundsMinZ && z <= cfg.MapBoundsMaxZ
}

// FindPath finds a path within a single map using A* with zone awareness.
func (nm *NavMesh) FindPath(mapID int, sx, sz, ex, ez int32) astar.PathResult {
	cfg := config.C()
	return astar.FindPath(int(sx), int(sz), int(ex), int(ez),
		func(x, z int) bool {
			return nm.IsWalkable(mapID, int32(x), int32(z))
		},
		cfg.BFSMaxNodes,
	)
}

// FindPathWithCache finds a path using a pre-allocated PathCache (zero-alloc).
func (nm *NavMesh) FindPathWithCache(mapID int, sx, sz, ex, ez int32) astar.PathResult {
	cfg := config.C()
	return astar.FindPathWithCache(nm.cache, int(sx), int(sz), int(ex), int(ez),
		func(x, z int) bool {
			return nm.IsWalkable(mapID, int32(x), int32(z))
		},
		cfg.BFSMaxNodes,
	)
}

// MultiFloorPath finds a path potentially across multiple maps via portals.
// Returns the path segments: each segment is a path within one map ending at a portal.
func (nm *NavMesh) MultiFloorPath(sx, sz int32, srcMapID int, ex, ez int32, dstMapID int) []astar.PathResult {
	if srcMapID == dstMapID {
		// Single map case
		result := nm.FindPath(srcMapID, sx, sz, ex, ez)
		return []astar.PathResult{result}
	}

	// Multi-map: find closest portal chain
	// Build graph: origin → nearest portal on srcMap → ... → destination portal on dstMap → destination
	srcPortal := nm.findNearestPortal(srcMapID, sx, sz)
	if srcPortal == nil {
		return nil
	}

	dstPortal := nm.findNearestPortalTo(dstMapID, ex, ez)
	if dstPortal == nil {
		return nil
	}

	// Path 1: origin → srcPortal
	seg1 := nm.FindPath(srcMapID, sx, sz, srcPortal.SrcX, srcPortal.SrcZ)

	// Path 2: dstPortal → destination (on dst map)
	seg2 := nm.FindPath(dstMapID, dstPortal.DstX, dstPortal.DstZ, ex, ez)

	results := pathResultPool.Get()
	*results = (*results)[:0]
	*results = append(*results, seg1, seg2)
	return *results
}

// findNearestPortal finds the portal closest to a point on a given map.
func (nm *NavMesh) findNearestPortal(mapID int, x, z int32) *Portal {
	var best *Portal
	bestDist := math.MaxFloat64
	for i := range nm.portals {
		p := &nm.portals[i]
		if p.SrcMapID != mapID {
			continue
		}
		dx := float64(p.SrcX - x)
		dz := float64(p.SrcZ - z)
		dist := dx*dx + dz*dz
		if dist < bestDist {
			bestDist = dist
			best = p
		}
	}
	return best
}

// findNearestPortalTo finds the portal whose destination is closest to a point.
func (nm *NavMesh) findNearestPortalTo(mapID int, x, z int32) *Portal {
	var best *Portal
	bestDist := math.MaxFloat64
	for i := range nm.portals {
		p := &nm.portals[i]
		if p.DstMapID != mapID {
			continue
		}
		dx := float64(p.DstX - x)
		dz := float64(p.DstZ - z)
		dist := dx*dx + dz*dz
		if dist < bestDist {
			bestDist = dist
			best = p
		}
	}
	return best
}

// ResetCache resets the internal A* path cache.
func (nm *NavMesh) ResetCache() {
	nm.cache.Reset()
}

// ZoneCount returns the number of registered zones.
func (nm *NavMesh) ZoneCount() int {
	return len(nm.zones)
}

// PortalCount returns the number of registered portals.
func (nm *NavMesh) PortalCount() int {
	return len(nm.portals)
}
