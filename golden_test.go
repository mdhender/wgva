// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"testing"
)

// The golden suite of DESIGN.md 30.9.
//
// It is a table of representative coordinates and the exact float64 bit
// patterns of every continuous scale at each of them, under one seed and this
// binary's default configuration. Bit patterns rather than printed values,
// because a difference in the last place is exactly the difference this suite
// exists to catch and %v does not show it.
//
// **Run it on GOARCH=amd64 and GOARCH=arm64.** That is the only thing that
// actually proves DESIGN.md 25 is being honored: a missing mathx.Mul fuses into
// an FMA on one of the two and not on the other, and the same tile then differs
// between a laptop and a server with nothing in the source to see. A golden
// suite that has only ever run on one architecture has not been run.
//
//	GOARCH=arm64 go test -run TestGolden ./...
//	GOARCH=amd64 go test -run TestGolden ./...
//
// Updating this table requires an explicit compatibility decision recorded in
// the commit message, together with the AlgorithmVersion bump that goes with it.
// It is recorded output by construction — there is nothing to derive it from
// independently — which is why the decision is the control rather than the
// review.
//
// Recorded under AlgorithmVersion 2 at world radius 32767.
//
// This is deliberately not the seed the terrain tuning tool opens on. Two
// reasons, and the second is the one that matters. A golden table's whole signal
// is that a recorded value moved, so re-recording it to tidy up a constant
// spends that signal on nothing — every row would move at once with nothing else
// in the diff. And a seed nobody looks at every day is better coverage than one
// that is exercised by hand a hundred times an afternoon.
const goldenSeed Seed = 0x5747564100000001

// goldenCoords are the coordinates the table covers. The selection is
// deliberate: the origin and its immediate neighborhood, because DESIGN.md 9.4
// is about exactly that place; negative and mixed-sign components, because Go's
// division truncates; a coordinate on each axis far from the origin; the rim's
// six corners and a point on an edge; and one coordinate given outside the
// canonical domain, so the table also pins that normalization happens before
// anything is sampled.
var goldenCoords = [][2]int64{
	{0, 0},
	{1, 0},
	{0, 1},
	{-1, 0},
	{0, -1},
	{1, -1},
	{-1, 1},
	{7, 11},
	{-7, 11},
	{7, -11},
	{-7, -11},
	{1000, 0},
	{0, 1000},
	{-1000, -1000},
	{12345, -6789},
	{32767, 0},
	{0, 32767},
	{-32767, 0},
	{0, -32767},
	{32767, -32767},
	{-32767, 32767},
	{16384, -32767},
	// Outside the canonical domain: the first mirror center, which normalizes
	// to the origin, and one step past the rim on the q axis.
	{65535, -32767},
	{32768, 0},
}

// goldenRow is one coordinate and the float64 bits of every continuous scale at
// it, in the order Scales returns them.
type goldenRow struct {
	q, r   int64
	scales [scaleCount]uint64
}

// goldenValues is one row per coordinate in goldenCoords, in that order.
var goldenValues = []goldenRow{
	{0, 0, [scaleCount]uint64{0x3fc8671f5d4f7df6, 0xbfd41045a2710bcb, 0x3fe520aabf158bf0, 0x3fd094151954a18d}},
	{1, 0, [scaleCount]uint64{0x3fc81df9cf9a419d, 0xbfd84707d0838534, 0x3fe02c9a3a9f1248, 0xbfd3b7308622d375}},
	{0, 1, [scaleCount]uint64{0x3fc6be033fa4de78, 0xbfd76359b21f8e4a, 0x3fd5aa4a78514241, 0xbfd107bda1c43e99}},
	{-1, 0, [scaleCount]uint64{0x3fc8a96fbf1a8935, 0xbfcf8b7f83ba40d5, 0x3fe7f8aa9ae0467a, 0x3fe151f20701f2d5}},
	{0, -1, [scaleCount]uint64{0x3fca02abc8c96854, 0xbfcbe2e6c25e3f4e, 0x3fe3c730f50bf637, 0x3fc957533b41e77c}},
	{1, -1, [scaleCount]uint64{0x3fc9bb03dc8546f4, 0xbfd2b56815297007, 0x3fdcddc6818b5976, 0xbf9851f61d0b5e95}},
	{-1, 1, [scaleCount]uint64{0x3fc701333abe006d, 0xbfd3d6a3a653bea4, 0x3fd6f0b83c206d68, 0x3fda4f94f7a5377f}},
	{7, 11, [scaleCount]uint64{0x3fad9ca5387843ce, 0xbfe32a1afa2aa109, 0xbfd476d582f3a1de, 0xbfe01955d4bbb8e1}},
	{-7, 11, [scaleCount]uint64{0x3fb0e6963c6307e9, 0xbfd4c286b4255029, 0xbfd3c863bab489f3, 0x3fbbed624e390d2d}},
	{7, -11, [scaleCount]uint64{0x3fd178df7e9226c6, 0x3f997c9d13db3c37, 0x3fd37715e0e33709, 0xbfd80f92f0c03b2b}},
	{-7, -11, [scaleCount]uint64{0x3fd0b3d444379cb7, 0x3fdc7ca935622524, 0x3fad299f93786d9b, 0xbfe733d15ca810c3}},
	{1000, 0, [scaleCount]uint64{0xbfd9bc46d7a26d3b, 0xbfc7acbdb6e877a1, 0xbfe1f0865bd9f190, 0x3fe622162f6d63f0}},
	{0, 1000, [scaleCount]uint64{0xbfe7c25490b1aecd, 0x3fdfb351be4739a2, 0x3f7b964564246c6e, 0x3fc2e9a41655f4f7}},
	{-1000, -1000, [scaleCount]uint64{0xbfd857d3a549fe46, 0xbfd5dc5a8361e4e0, 0xbfdcbea58759fa35, 0x3fc6a0dcfa1ebc04}},
	{12345, -6789, [scaleCount]uint64{0xbfdbdd4e762437c9, 0xbfdf5a38811f064d, 0xbfe1b0eabaa758a9, 0x3fd512954e244c77}},
	{32767, 0, [scaleCount]uint64{0x3fd3a1f61784ddb3, 0x3fd00fb9ae20ba98, 0xbfb325318c73d810, 0xbfe089281db9b9cd}},
	{0, 32767, [scaleCount]uint64{0x3fde3a81ddbe14d2, 0xbfad0203ae19cb65, 0x3fbe010e74dfaeb7, 0xbfd23d1d2085583c}},
	{-32767, 0, [scaleCount]uint64{0xbfd5cb9e8537f588, 0x3fe168e81c8dd94f, 0x3fd0e8da3461990a, 0xbfd139fa201cd199}},
	{0, -32767, [scaleCount]uint64{0x3fc6c045217fdd96, 0x3fc8dd31d950e709, 0x3f8efe7ff23d208e, 0xbfd28e5cd115f368}},
	{32767, -32767, [scaleCount]uint64{0x3fdd05f03413955b, 0xbfd93700b8b43bfb, 0xbfd301f83020c91b, 0x3fafa60970a5b039}},
	{-32767, 32767, [scaleCount]uint64{0x3f9b99ab6ce5a738, 0x3fbf5f6d5ac96d4b, 0x3fc9de6582603642, 0xbfbd83049e9ae918}},
	{16384, -32767, [scaleCount]uint64{0x3fc3b67b4eb4858f, 0xbfdbce21b897234b, 0x3fcd4e842d55f3fa, 0x3fcaedf0a8cb53b4}},
	{65535, -32767, [scaleCount]uint64{0x3fc8671f5d4f7df6, 0xbfd41045a2710bcb, 0x3fe520aabf158bf0, 0x3fd094151954a18d}},
	{32768, 0, [scaleCount]uint64{0x3f9b99ab6ce5a738, 0x3fbf5f6d5ac96d4b, 0x3fc9de6582603642, 0xbfbd83049e9ae918}},
}

func TestGolden(t *testing.T) {
	if len(goldenValues) != len(goldenCoords) {
		t.Fatalf("the golden table has %d rows for %d coordinates", len(goldenValues), len(goldenCoords))
	}

	g := NewDefault(goldenSeed)
	for i, row := range goldenValues {
		if [2]int64{row.q, row.r} != goldenCoords[i] {
			t.Fatalf("row %d is for (%d, %d), want (%d, %d)", i, row.q, row.r, goldenCoords[i][0], goldenCoords[i][1])
		}
		c := NewCoord(row.q, row.r)

		for j, s := range Scales() {
			got := math.Float64bits(g.ScaleAt(s, c))
			if want := row.scales[j]; got != want {
				t.Errorf("%v at (%d, %d) = %#016x (%v), want %#016x (%v)",
					s, row.q, row.r, got, math.Float64frombits(got), want, math.Float64frombits(want))
			}
		}
	}
}

// TestGoldenCoordinatesAreCanonical pins the second thing the table asserts:
// every row's coordinate normalizes to what the generator was handed, so a
// change to the normalizer moves these values and is caught here rather than
// only in the coordinate tests.
func TestGoldenCoordinatesAreCanonical(t *testing.T) {
	for _, gc := range goldenCoords {
		c := NewCoord(gc[0], gc[1])
		if !isCanonical(int64(c.Q()), int64(c.R())) {
			t.Errorf("(%d, %d) normalized to (%d, %d), which is not canonical", gc[0], gc[1], c.Q(), c.R())
		}
	}
}
