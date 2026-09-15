// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import "github.com/mdhender/wgva"

// Count is one row of a readout: what the row is called and how many tiles of
// the window fell in it.
type Count struct {
	Label string
	Tiles int
}

// Percent is the row's share of a window of n tiles, rounded down.
//
// It is integer arithmetic on integer counts, so two front ends printing the
// same window print the same numbers. A float here would be a rounding policy
// each front end got to have an opinion about.
func (c Count) Percent(n int) int {
	if n == 0 {
		return 0
	}
	return c.Tiles * 100 / n
}

// Distribution is what is in one window: the terrain mix, the elevation band
// histogram, and the two climate ladders.
//
// It lives in render because it is a measurement over a window and a window is
// this package's, so a front end and the CLI cannot disagree about what a window
// contains. The global statistical tests of DESIGN.md 30.8 measure the same
// quantities over the world — every terrain reachable, none dominating — and
// what they cannot say is whether the threshold somebody just moved did what
// they meant *here*. In WGVB, moving one dryness threshold by 0.1 took a coastal
// window from 6% dry to 62%, and from 70% plains to 33% plains and 38%
// grassland; the picture says something changed and the readout says what.
//
// Every declared value is listed, including the ones with no tiles, because a
// row reading zero is usually the row somebody is trying to move off zero — and
// the two inland-water terrains of DESIGN.md 17.1 are deliberately emitted
// nowhere, so their zeros are a statement rather than an omission.
type Distribution struct {
	// Tiles is the number of cells measured, which is the denominator of every
	// percentage. A strided window measures the cells it sampled and not the
	// hexes they stand for.
	Tiles int

	Terrain   []Count
	Elevation []Count
	Heat      []Count
	Moisture  []Count
}

// Cost is what measuring a window costs, in generator evaluations.
//
// It is a whole Tile per cell whatever layer is on screen, which is seven
// evaluations, because a Tile reads the six neighboring elevations. So a page
// that shows a readout beside an image goes through the evaluation budget twice
// over, and the grid tab shows no readout at all: a million tiles of readout is
// seven million evaluations for a second copy of work the image already did. See
// DESIGN.md 29.1.
func (v Viewport) Cost() int { return v.Cols * v.Rows * TileCost }

// TileCost is how many generator evaluations one whole Tile takes. It is the
// cost of the relief and terrain layers for the same reason: a Tile reads the
// six neighboring elevation scalars as well as its own.
const TileCost = 7

// Measure counts what is in a window.
//
// The tiles are generated as a batch, which parallelizes across goroutines
// because every tile is a pure function of its own coordinate written to its own
// slot. The counting is then done in one goroutine over the finished slice, in
// cell order.
//
// That ordering is deliberate and is not the accumulation DESIGN.md 20 forbids.
// The forbidden thing is a *batch fill* that accumulates, because a
// floating-point sum would then depend on how the work was split. These are
// integer counters, they are incremented in one goroutine in a fixed order, and
// a test asserts the result does not depend on the order the coordinates
// arrive in.
func Measure(g *wgva.Generator, v Viewport) Distribution {
	coords := make([]wgva.Coord, 0, v.Cols*v.Rows)
	for row := range v.Rows {
		for col := range v.Cols {
			coords = append(coords, v.CoordAt(col, row))
		}
	}

	tiles := make([]wgva.Tile, len(coords))
	g.TilesInto(coords, tiles)

	terrains := wgva.Terrains()
	elevations := wgva.Elevations()
	heats := wgva.Heats()
	moistures := wgva.Moistures()

	// The counters are indexed by the declared value rather than by a position
	// in a sorted list, so a value with no tiles keeps its row and the row order
	// is the enumeration's order. Nothing here ranges over a map.
	terrainCounts := make([]int, len(terrains))
	elevationCounts := make([]int, len(elevations))
	heatCounts := make([]int, len(heats))
	moistureCounts := make([]int, len(moistures))

	// A value's row is found through a lookup table built from the enumeration
	// rather than by treating the value as an index. The values are persisted
	// and are written out explicitly rather than with iota (DESIGN.md 19), so
	// they are under no obligation to stay contiguous, and a table that assumed
	// they were would silently mis-file a row the day one was retired.
	//
	// These maps are read and never ranged over, so nothing here is exposed to
	// Go's randomized map iteration.
	terrainRow := map[wgva.Terrain]int{}
	for i, t := range terrains {
		terrainRow[t] = i
	}
	elevationRow := map[wgva.Elevation]int{}
	for i, e := range elevations {
		elevationRow[e] = i
	}
	heatRow := map[wgva.HeatBand]int{}
	for i, h := range heats {
		heatRow[h] = i
	}
	moistureRow := map[wgva.MoistureBand]int{}
	for i, m := range moistures {
		moistureRow[m] = i
	}

	// A value with no row is dropped rather than counted somewhere plausible.
	// It cannot happen while the classifiers only return declared values, and if
	// it ever does, a total below Tiles is what says so.
	for _, tile := range tiles {
		if i, ok := terrainRow[tile.Terrain]; ok {
			terrainCounts[i]++
		}
		if i, ok := elevationRow[tile.Elevation]; ok {
			elevationCounts[i]++
		}
		if i, ok := heatRow[tile.Climate.Heat]; ok {
			heatCounts[i]++
		}
		if i, ok := moistureRow[tile.Climate.Moisture]; ok {
			moistureCounts[i]++
		}
	}

	d := Distribution{Tiles: len(tiles)}
	for i, t := range terrains {
		d.Terrain = append(d.Terrain, Count{Label: t.String(), Tiles: terrainCounts[i]})
	}
	for i, e := range elevations {
		d.Elevation = append(d.Elevation, Count{Label: e.String(), Tiles: elevationCounts[i]})
	}
	for i, h := range heats {
		d.Heat = append(d.Heat, Count{Label: h.String(), Tiles: heatCounts[i]})
	}
	for i, m := range moistures {
		d.Moisture = append(d.Moisture, Count{Label: m.String(), Tiles: moistureCounts[i]})
	}
	return d
}
