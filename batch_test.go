// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math/rand/v2"
	"testing"
)

// TestBatchMatchesTile is the assertion the batch API exists under: a batch
// result is bit-identical to the same tile asked for on its own. Anything less
// would make this a second generator.
func TestBatchMatchesTile(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)

	var coords []Coord
	for q := int64(-40); q <= 40; q += 7 {
		for r := int64(-40); r <= 40; r += 3 {
			coords = append(coords, NewCoord(q, r))
		}
	}
	// A run long enough to be split across workers, and one that crosses the
	// wrap seam, so the parallel path and the normalizer are both under test.
	for i := range 500 {
		coords = append(coords, NewCoord(WorldRadius-int64(i), -WorldRadius+int64(i)/2))
	}

	batch := g.Tiles(coords)
	if len(batch) != len(coords) {
		t.Fatalf("Tiles returned %d tiles for %d coordinates", len(batch), len(coords))
	}
	for i, c := range coords {
		if got, want := batch[i], g.Tile(c); got != want {
			t.Fatalf("Tiles()[%d] at %v = %+v, want %+v", i, c, got, want)
		}
	}
}

// TestTilesIntoIsIndependentOfTheSplit is the determinism rule of DESIGN.md 20
// read as a test. The per-slot rule is what makes it true, so the test that
// would actually fail is one run against a batch fill that accumulated
// something: it would pass at one GOMAXPROCS and fail at another.
func TestTilesIntoIsIndependentOfTheSplit(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)

	coords := CoordsWithin(NewCoord(1234, -567), 12)
	want := make([]Tile, len(coords))
	for i, c := range coords {
		want[i] = g.Tile(c)
	}

	got := make([]Tile, len(coords))
	for range 8 {
		for i := range got {
			got[i] = Tile{}
		}
		g.TilesInto(coords, got)
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("TilesInto filled slot %d with %+v, want %+v", i, got[i], want[i])
			}
		}
	}
}

// TestTilesIntoIsIndependentOfTheOrder is the other half: a batch is a set of
// independent slots, so shuffling the coordinates moves the answers and does not
// change any of them.
func TestTilesIntoIsIndependentOfTheOrder(t *testing.T) {
	g := NewDefault(7)

	coords := CoordsWithin(Origin, 9)
	want := map[Coord]Tile{}
	for i, tile := range g.Tiles(coords) {
		want[coords[i]] = tile
	}

	// math/rand/v2 is permitted for test data and never in the generation path.
	// The seed is written down so a failure is reproducible.
	shuffled := append([]Coord(nil), coords...)
	rng := rand.New(rand.NewPCG(1, 2))
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	for i, tile := range g.Tiles(shuffled) {
		if tile != want[shuffled[i]] {
			t.Fatalf("at %v the shuffled batch produced %+v, want %+v", shuffled[i], tile, want[shuffled[i]])
		}
	}
}

func TestTilesIntoPanicsOnMismatchedLengths(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TilesInto accepted mismatched lengths")
		}
	}()
	NewDefault(1).TilesInto(make([]Coord, 3), make([]Tile, 4))
}

// TestCoordsWithinCounts pins the hex region's size and its order. The count is
// derived rather than recorded: a hex of radius r holds 3r² + 3r + 1 tiles.
func TestCoordsWithinCounts(t *testing.T) {
	for _, radius := range []uint32{0, 1, 2, 5, 33} {
		r := int64(radius)
		want := 3*r*r + 3*r + 1
		got := CoordsWithin(NewCoord(-9, 4), radius)
		if int64(len(got)) != want {
			t.Errorf("CoordsWithin(radius %d) returned %d coordinates, want %d", radius, len(got), want)
		}
		for _, c := range got {
			if d := hexDistance(NewCoord(-9, 4), c); d > r {
				t.Errorf("CoordsWithin(radius %d) returned %v, %d hexes away", radius, c, d)
			}
		}
	}
}

// TestCoordsWithinIsFixedOrder states what "a fixed order" means: the same
// center and radius produce the same slice, every time and on every machine.
func TestCoordsWithinIsFixedOrder(t *testing.T) {
	first := CoordsWithin(NewCoord(100, -200), 7)
	for range 4 {
		again := CoordsWithin(NewCoord(100, -200), 7)
		for i := range first {
			if first[i] != again[i] {
				t.Fatalf("CoordsWithin is not a fixed order: slot %d was %v and is %v", i, first[i], again[i])
			}
		}
	}
}

func TestCoordsWithinPanicsBeyondTheWorld(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("CoordsWithin accepted a radius larger than the world")
		}
	}()
	CoordsWithin(Origin, uint32(WorldRadius)+1)
}

// hexDistance is the unwrapped hex distance, for checking the region walk. The
// package has no such function and does not need one; this is test arithmetic.
func hexDistance(a, b Coord) int64 {
	dq := int64(a.Q()) - int64(b.Q())
	dr := int64(a.R()) - int64(b.R())
	ds := -dq - dr
	return max(abs64(dq), abs64(dr), abs64(ds))
}
