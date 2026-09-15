// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"fmt"
	"runtime"
	"sync"
)

// TilesInto fills out with one tile per coordinate. It panics if the lengths
// differ.
//
// Batch results are bit-identical to individual [Generator.Tile] calls, which a
// test asserts. Batch generation is an optimization and nothing else; if it ever
// produced a different number it would be a second generator, and the invariant
// this package exists for would have a second implementation to keep honest.
//
// Goroutines parallelize the fill. That is safe *and* deterministic here for a
// structural reason rather than by luck: every tile is a pure function of its
// own coordinate and is written to its own slot, so no scheduling split can
// reach any value. The slice is split into contiguous ranges, one per worker,
// and each worker writes only its own range.
//
// **Nothing here may accumulate across tiles** — no running sum, no min or max,
// no histogram. Floating-point addition is not associative, so an accumulation
// would depend on the split and a determinism test would pass only by luck. An
// integer count is a different matter and is not this method's business in any
// case; render.Measure is where a window is counted. See DESIGN.md 20.
func (g *Generator) TilesInto(coords []Coord, out []Tile) {
	if len(coords) != len(out) {
		panic(fmt.Sprintf("wgva: TilesInto: %d coordinates into %d tiles", len(coords), len(out)))
	}
	if len(coords) == 0 {
		return
	}

	// Below the threshold the goroutines cost more than the work. The result is
	// identical either way — that is the whole point of the per-slot rule — so
	// this is a scheduling choice and not a numerical one.
	const parallelFrom = 64
	workers := min(runtime.GOMAXPROCS(0), len(coords))
	if len(coords) < parallelFrom || workers < 2 {
		for i, c := range coords {
			out[i] = g.Tile(c)
		}
		return
	}

	// Contiguous ranges rather than a stride, so each worker walks a run of
	// adjacent slots and the write pattern stays cache-friendly.
	var wg sync.WaitGroup
	span := (len(coords) + workers - 1) / workers
	for lo := 0; lo < len(coords); lo += span {
		hi := min(lo+span, len(coords))
		wg.Go(func() {
			for i := lo; i < hi; i++ {
				out[i] = g.Tile(coords[i])
			}
		})
	}
	wg.Wait()
}

// Tiles returns one tile per coordinate.
//
// Prefer [Generator.TilesInto] in a hot path: [Tile] is a pointer-free value, so
// a caller can reuse one buffer across frames with no allocation and no work for
// the garbage collector.
func (g *Generator) Tiles(coords []Coord) []Tile {
	out := make([]Tile, len(coords))
	g.TilesInto(coords, out)
	return out
}

// TilesWithin returns every tile within radius hexes of center, in the fixed
// order [CoordsWithin] produces.
//
// It is named for what it returns rather than for the shape, because
// [Generator.Region] already means the region cell a coordinate falls in.
// DESIGN.md 20 calls this one Region; that name was taken in phase 3 by the
// addressing method, and two methods on one type cannot share it. See
// DESIGN.md appendix D.18.
func (g *Generator) TilesWithin(center Coord, radius uint32) []Tile {
	return g.Tiles(CoordsWithin(center, radius))
}

// CoordsWithin returns every coordinate within radius hexes of center, in a
// fixed order: by the offset's q and then by its r, which is the order the cube
// range walk produces and is stable for any center.
//
// The coordinates are normalized, so a region that runs off the edge of the map
// wraps — which is what the world's topology says it should do. At a radius
// approaching the world's own, the same canonical coordinate can therefore
// appear more than once, and that is the wrap rather than a defect. Nothing in
// this package deduplicates it, because a batch fill writes each slot from its
// own coordinate and a caller asking for a region larger than the world has
// asked for the world several times over.
//
// It panics on a radius above [WorldRadius]. The count is 3r² + 3r + 1, which
// passes the world's tile count a little above the radius and overflows an int
// well before a uint32 runs out; a panic naming the number is a better answer
// than an allocation that takes the machine down.
func CoordsWithin(center Coord, radius uint32) []Coord {
	if int64(radius) > WorldRadius {
		panic(fmt.Sprintf("wgva: CoordsWithin: radius %d is larger than the world radius %d", radius, WorldRadius))
	}

	r := int64(radius)
	out := make([]Coord, 0, 3*r*r+3*r+1)
	cq, cr := int64(center.Q()), int64(center.R())
	for dq := -r; dq <= r; dq++ {
		for dr := max(-r, -dq-r); dr <= min(r, -dq+r); dr++ {
			out = append(out, NewCoord(cq+dq, cr+dr))
		}
	}
	return out
}
