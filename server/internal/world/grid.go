// AOI grid (brief §3: uniform grid, ~30 m cells, broadcast only to nearby
// players). A cell is one square; each entity registers in every cell it
// overlaps. Nearby(cell) returns the 3×3 cell neighborhood.
package world

import "math"

// CellSize is the side length of one AOI cell in meters.
const CellSize = 30.0

// cellKey identifies one grid cell by its XZ indices.
type cellKey struct{ cx, cz int }

// Grid is a uniform spatial hash for AOI culling.
type Grid struct {
	cells map[cellKey]map[uint64]*Entity
}

// NewGrid returns an empty grid.
func NewGrid() *Grid {
	return &Grid{cells: make(map[cellKey]map[uint64]*Entity)}
}

// cellFor maps a position to its cell key.
func cellFor(p Vec3) cellKey {
	return cellKey{cx: int(math.Floor(p.X / CellSize)), cz: int(math.Floor(p.Z / CellSize))}
}

// Remove detaches the entity from whatever cell it was in (best effort; the
// entity keeps its own record of its cell for efficient re-insertion).
func (g *Grid) Remove(e *Entity) {
	if e.cell == nil {
		return
	}
	if set := g.cells[*e.cell]; set != nil {
		delete(set, e.ID)
		if len(set) == 0 {
			delete(g.cells, *e.cell)
		}
	}
	e.cell = nil
}

// Insert places the entity into the cell containing its position.
func (g *Grid) Insert(e *Entity) {
	c := cellFor(e.Pos)
	g.Remove(e)
	set := g.cells[c]
	if set == nil {
		set = make(map[uint64]*Entity)
		g.cells[c] = set
	}
	set[e.ID] = e
	e.cell = &c
}

// Refresh moves the entity between cells if it crossed a boundary.
func (g *Grid) Refresh(e *Entity) {
	if e.cell == nil {
		g.Insert(e)
		return
	}
	c := cellFor(e.Pos)
	if *e.cell == c {
		return
	}
	g.Remove(e)
	g.Insert(e)
}

// Nearby returns the entities in the 3×3 neighborhood of a position (the
// AOI that gets broadcast to one viewer), excluding the viewer itself.
// It is the bounding box a client can see: 3 cells × 30 m = 90 m.
func (g *Grid) Nearby(p Vec3, exclude uint64) []*Entity {
	center := cellFor(p)
	seen := make(map[uint64]*Entity)
	for dz := -1; dz <= 1; dz++ {
		for dx := -1; dx <= 1; dx++ {
			set := g.cells[cellKey{cx: center.cx + dx, cz: center.cz + dz}]
			for id, e := range set {
				if id == exclude {
					continue
				}
				seen[id] = e
			}
		}
	}
	out := make([]*Entity, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	return out
}
