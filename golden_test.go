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

// ---------------------------------------------------------------------------
// The region influence
// ---------------------------------------------------------------------------

// The second golden table: the blended region influence of DESIGN.md 11.2 at
// the same coordinates, under the same seed and the same defaults.
//
// It is a separate table rather than more columns on the first because the two
// answer different questions and a golden table's whole signal is that a value
// moved. Re-recording the scale rows to widen them would spend that signal on
// nothing — every row would change at once with nothing else in the diff to
// explain it.
//
// It earns its keep twice over. The blend is ten multiply-adds a tile and every
// one of them is a place DESIGN.md 25.1 can be forgotten; a missing mathx.Mul
// fuses on arm64 and not on amd64, and nothing else in the module would notice.
// And the half-angle recovery of an orientation is arithmetic whose only check
// is that it still produces the number it produced yesterday.
//
// Recorded under AlgorithmVersion 2 at world radius 32767.

// regionScalarCount is how many normalized biases RegionParams carries, and
// TestRegionParamsShape is what keeps it true.
const regionScalarCount = 7

// goldenRegionRow is one coordinate, the float64 bits of every blended bias in
// declaration order, and the bits of the blended ridge orientation.
type goldenRegionRow struct {
	q, r    int64
	scalars [regionScalarCount]uint64
	ridge   [2]uint64
}

// goldenRegionValues is one row per coordinate in goldenCoords, in that order.
var goldenRegionValues = []goldenRegionRow{
	{0, 0, [regionScalarCount]uint64{0xbfddd33abe9beeb0, 0xbfeefeda87bbd340, 0x3fe6a12f1769ecc2, 0xbfdd759da5b30948, 0x3fe7b516f9e2729a, 0x3fc253a8960d4608, 0xbfea66dfcb7dd3de}, [2]uint64{0x3fead2a60a4f6c24, 0xbfe1737017e72b3f}},
	{1, 0, [regionScalarCount]uint64{0xbfddd2756f6412ce, 0xbfeefeacb7116122, 0x3fe6a0a9a5b5e3e9, 0xbfdd74aed7f889f9, 0x3fe7b4bb9c679950, 0x3fc25520d0c5c9ed, 0xbfea662e4ab21bf1}, [2]uint64{0x3fead289f844e032, 0xbfe1739b3cde3797}},
	{0, 1, [regionScalarCount]uint64{0xbfddd3b7fc0ddb01, 0xbfeefe9e9ccecb3c, 0x3fe6a0e01e0b4ce5, 0xbfdd748159e80101, 0x3fe7b43da4ca8d22, 0x3fc254e7932a0baf, 0xbfea661bd86a7ce1}, [2]uint64{0x3fead2b01cbc3ddd, 0xbfe173609cb646ce}},
	{-1, 0, [regionScalarCount]uint64{0xbfddd23525c9f234, 0xbfeefe5b82af5e7f, 0x3fe6a0dd09111bbf, 0xbfdd74af01b63a29, 0x3fe7b52b41ef5631, 0x3fc2554f0ae9c5c1, 0xbfea668ea2f6a230}, [2]uint64{0x3fead28425ba1eb4, 0xbfe173a42fc2cd16}},
	{0, -1, [regionScalarCount]uint64{0xbfddd2c1176e3f5a, 0xbfeefe7c33e4c1c3, 0x3fe6a0e9659d9623, 0xbfdd74aec9edae84, 0x3fe7b46a964afdb6, 0x3fc2515f4959b528, 0xbfea66c9b1e054b1}, [2]uint64{0x3fead2b7d13becc0, 0xbfe17354c4d0593a}},
	{1, -1, [regionScalarCount]uint64{0xbfddd29ed2fb5977, 0xbfeefdeb61045dea, 0x3fe6a0f6c0057df8, 0xbfdd751d20ea7a8e, 0x3fe7b466a3a41cf3, 0x3fc25502d786e249, 0xbfea666144794fd0}, [2]uint64{0x3fead2a03a950f3e, 0xbfe173790697fab1}},
	{-1, 1, [regionScalarCount]uint64{0xbfddd32e41620d65, 0xbfeefde4b326d388, 0x3fe6a14595193423, 0xbfdd747d8af4cdc6, 0x3fe7b4c074088932, 0x3fc253bebed4aec4, 0xbfea668a0aedf31c}, [2]uint64{0x3fead2830e5c0f7b, 0xbfe173a5dd21f6fe}},
	{7, 11, [regionScalarCount]uint64{0xbfddef666b14a51e, 0xbfeece188a5fb55a, 0x3fe64e894711ba1d, 0xbfdc88e18a787e6d, 0x3fe7170faf1571b9, 0x3fc3788757d89c4a, 0xbfe9c07536987b41}, [2]uint64{0x3fead1d6c2ca6afd, 0xbfe174aea0b3fc9d}},
	{-7, 11, [regionScalarCount]uint64{0xbfddd99b0faca9b5, 0xbfeec347a7e1d5cf, 0x3fe6a071a89c8b13, 0xbfdd201b45012102, 0x3fe791bcf04e13a7, 0x3fc270037795d1cc, 0xbfea454345880a56}, [2]uint64{0x3feacb810d2e2ecb, 0xbfe17e666c8dba7f}},
	{7, -11, [regionScalarCount]uint64{0xbfdda7501f172b0a, 0xbfeec241ce6af936, 0x3fe68f6db3880565, 0xbfdd47426a4a65ad, 0x3fe780df1db47a0d, 0x3fc2767b85e4d36b, 0xbfea48da3eb20b63}, [2]uint64{0x3fead2a5a89ee28e, 0xbfe17370ae0e60a5}},
	{-7, -11, [regionScalarCount]uint64{0xbfdd45d14157599c, 0xbfeea4485a77e596, 0x3fe6613b7de480c0, 0xbfdca52795238584, 0x3fe74f144e81911e, 0x3fc15248400dc580, 0xbfea44b8a535ca73}, [2]uint64{0x3fead52d1acf6a5b, 0xbfe16f8ce929c92e}},
	{1000, 0, [regionScalarCount]uint64{0xbfcdae537e9c40e9, 0xbfe86c81ee580b33, 0xbfbb01c2e97cf7a9, 0x3fe53e8a93023e67, 0xbfa16e4fe2cf7d4e, 0x3fe0d9f0deb17d3f, 0xbfd53afdf52dcd91}, [2]uint64{0x3fefdcb944b42a2d, 0x3fb7bb611c06738c}},
	{0, 1000, [regionScalarCount]uint64{0x3fd6292ff578e3a0, 0x3fd17cc226233d79, 0xbfec71d9bf4b8ca8, 0x3f34eccad2722100, 0x3fe84c8d52e45339, 0x3fd0b094212162cc, 0xbfec4385dc53e61d}, [2]uint64{0x3fc7de52d6c108a8, 0x3fef70506be90d16}},
	{-1000, -1000, [regionScalarCount]uint64{0x3fd72415d1476401, 0xbfd919ba7e853bb5, 0x3fe8030d37517fd3, 0xbfe5e5f827ceab80, 0xbfb9eb6edd848f8f, 0xbfd5299e5ccdcff2, 0x3fea623ff84e8e68}, [2]uint64{0x3fe23ccb5ea3d68c, 0x3fea4b5c7358ef2c}},
	{12345, -6789, [regionScalarCount]uint64{0xbfe49234679cdbfe, 0xbfd470ebaec24214, 0x3fe03ad9aa4e47cf, 0xbfa2a2f0b3feb829, 0xbfe29789e1170152, 0xbfd95dcb81f3016c, 0xbfc825beab5b82af}, [2]uint64{0x3fe684d812b978d3, 0xbfe6bc42cc07cc0f}},
	{32767, 0, [regionScalarCount]uint64{0xbfe50e3bd748637d, 0x3fe230a91e2295a1, 0x3fe70e32ecdafeaf, 0xbfdd0622e7313f08, 0x3fdaf6e5be729be2, 0x3fe08159ee996d8b, 0xbfe142f68eab449e}, [2]uint64{0x3fe66fe3994ffd52, 0xbfe6d0f12187cbcd}},
	{0, 32767, [regionScalarCount]uint64{0xbfebb3739a306602, 0xbfecce2c3a4b4d24, 0x3fe776ca4b32c95b, 0xbfd7083d8543c06f, 0xbfc1ccd049649876, 0x3fe5718300d868a1, 0xbfc0dcce5a0c9bc4}, [2]uint64{0x3fe8bf9781600eef, 0xbfe4492e9e10e11a}},
	{-32767, 0, [regionScalarCount]uint64{0x3fa3ec3b391b6a96, 0x3fd44ffd1cbc7cc9, 0x3fe13423d6696d5f, 0x3fe1af94c09f5756, 0xbfe72ac30209b741, 0xbfe9555a53db1436, 0x3fd5d270dcbbc6e5}, [2]uint64{0x3fc33fd99a81330b, 0x3fefa2d5d2e0850c}},
	{0, -32767, [regionScalarCount]uint64{0xbfd063a8aaed4717, 0xbfd11ccbcfac026f, 0xbfbf1db3a6b2fa1b, 0xbfe0e9b2e7727c2a, 0x3fe1ce85a9aa1191, 0x3fcaad5bf03aa0ee, 0xbfe9f2259546499a}, [2]uint64{0x3fefee574182f6cf, 0x3fb0ccd0a3fb253c}},
	{32767, -32767, [regionScalarCount]uint64{0x3fe9e3290a621ceb, 0x3fecbbd47c75cd41, 0xbfe368f4dfeee04c, 0xbfe4c4d3986d31e6, 0x3fdb8efc0e40f7c7, 0x3fe625015419c14f, 0x3fea825d00742ca3}, [2]uint64{0x3fe2f98ce708755b, 0x3fe9c4718f79a05b}},
	{-32767, 32767, [regionScalarCount]uint64{0x3fc5d919e505f9d8, 0x3f8dc9fb9bf4f01c, 0xbfe5454479ecaf89, 0xbfee2508afa2322a, 0x3fd91132462562bf, 0xbfe3365e5d4bb50a, 0x3fd9a282f1586f59}, [2]uint64{0x3fe484144abd6c40, 0xbfe88eda815e63a2}},
	{16384, -32767, [regionScalarCount]uint64{0x3fc13b0590131702, 0xbfe68d98ca7973d2, 0xbfd3c16242e9632f, 0x3fe5a61c874d4049, 0x3fdc5dced85e68e2, 0x3fe2aa759a1ddb17, 0xbfd92495699b2510}, [2]uint64{0x3fe023d12b8e2f5e, 0xbfeba1ae2225fa87}},
	{65535, -32767, [regionScalarCount]uint64{0xbfddd33abe9beeb0, 0xbfeefeda87bbd340, 0x3fe6a12f1769ecc2, 0xbfdd759da5b30948, 0x3fe7b516f9e2729a, 0x3fc253a8960d4608, 0xbfea66dfcb7dd3de}, [2]uint64{0x3fead2a60a4f6c24, 0xbfe1737017e72b3f}},
	{32768, 0, [regionScalarCount]uint64{0x3fc5d919e505f9d8, 0x3f8dc9fb9bf4f01c, 0xbfe5454479ecaf89, 0xbfee2508afa2322a, 0x3fd91132462562bf, 0xbfe3365e5d4bb50a, 0x3fd9a282f1586f59}, [2]uint64{0x3fe484144abd6c40, 0xbfe88eda815e63a2}},
}

func TestGoldenRegion(t *testing.T) {
	if len(goldenRegionValues) != len(goldenCoords) {
		t.Fatalf("the region table has %d rows for %d coordinates", len(goldenRegionValues), len(goldenCoords))
	}

	g := NewDefault(goldenSeed)
	for i, row := range goldenRegionValues {
		if [2]int64{row.q, row.r} != goldenCoords[i] {
			t.Fatalf("row %d is for (%d, %d), want (%d, %d)", i, row.q, row.r, goldenCoords[i][0], goldenCoords[i][1])
		}
		c := NewCoord(row.q, row.r)
		p := g.RegionInfluence(c)

		for j, got := range regionScalars(p) {
			if want := row.scalars[j]; math.Float64bits(got) != want {
				t.Errorf("%s at (%d, %d) = %#016x (%v), want %#016x (%v)",
					regionScalarNames[j], row.q, row.r, math.Float64bits(got), got,
					want, math.Float64frombits(want))
			}
		}
		if got := math.Float64bits(p.Ridge.X); got != row.ridge[0] {
			t.Errorf("Ridge.X at (%d, %d) = %#016x, want %#016x", row.q, row.r, got, row.ridge[0])
		}
		if got := math.Float64bits(p.Ridge.Y); got != row.ridge[1] {
			t.Errorf("Ridge.Y at (%d, %d) = %#016x, want %#016x", row.q, row.r, got, row.ridge[1])
		}
	}
}

// ---------------------------------------------------------------------------
// The elevation composite
// ---------------------------------------------------------------------------

// The third golden table: the elevation composite of DESIGN.md 10 at the same
// coordinates, under the same seed and the same defaults.
//
// It is a separate table for the reason the region table is: each answers a
// different question, and a golden table's whole signal is that a value moved.
// Re-recording one to widen another would spend that signal on nothing.
//
// It earns its keep three times over. The composite is a dozen multiply-adds a
// tile and every one of them is a place DESIGN.md 25.1 can be forgotten. The
// ridge term folds three evaluations of its own field in a fixed order, and an
// order that changed would be a different number on every tile with nothing
// else to see. And relief is a seven-evaluation first difference, which is the
// one value in the module whose correctness depends on neighbors being
// enumerated the same way every time.
//
// The band is recorded beside the scalar deliberately: a threshold moved by a
// hair moves no float in this table and moves a band, and the two failures
// point at different commits.
//
// Recorded under AlgorithmVersion 3 at world radius 32767.

// goldenElevationRow is one coordinate, the float64 bits of the four values the
// composite is read through, and the band the scalar classifies to.
type goldenElevationRow struct {
	q, r      int64
	raw       uint64
	ridge     uint64
	elevation uint64
	relief    uint64
	band      Elevation
}

// goldenElevationValues is one row per coordinate in goldenCoords, in that
// order.
var goldenElevationValues = []goldenElevationRow{
	{0, 0, 0x3fc052cc757968ba, 0x3fd2ee9a8e1440c0, 0xbfc58e3b039ea18b, 0x3fe0b95e84b6136e, ElevationDeepWater},
	{1, 0, 0x3fb58eba65c87b8c, 0x3fd1449125b350df, 0xbfca43213d71969c, 0x3fdbdfd4e39553ff, ElevationDeepWater},
	{0, 1, 0x3fb12fe882f0306d, 0x3fd3b9d8649b2bd5, 0xbfcbf56148c2f806, 0x3fdd3f2f3e7f69d3, ElevationDeepWater},
	{-1, 0, 0x3fc4305e5fe531c8, 0x3fce0acb33154e75, 0xbfc1b4a6b8af75f6, 0x3fde3970c162308b, ElevationShallowWater},
	{0, -1, 0x3fc3482e89b0eceb, 0x3fcb47981f937e35, 0xbfc14b4b1b5fbd7d, 0x3fd86b9420742e0d, ElevationShallowWater},
	{1, -1, 0x3fbd31ae7129b36b, 0x3fce1ecea3b2cba9, 0xbfc5d0a2f6ca75fc, 0x3fd967abdfe3e4b8, ElevationDeepWater},
	{-1, 1, 0x3fb9a213947a052e, 0x3fd33f3ca03a48cc, 0xbfc85fc064958bd3, 0x3fe1cedb159dec1f, ElevationDeepWater},
	{7, 11, 0xbfbf9e61d009db82, 0x3fc3fefca55c21e3, 0xbfda7980839e55ef, 0x3fbcc50df39f2624, ElevationDeepWater},
	{-7, 11, 0xbfa673a37cfbac58, 0x3fd30894d30533da, 0xbfd4f548189a7e38, 0x3fd1d6e91251c47e, ElevationDeepWater},
	{7, -11, 0x3fca786f1b2676c4, 0x3fdc629e6608bfb4, 0xbf8e55c2be2c3cdf, 0x3fd8e8fb9f913a5b, ElevationShallowWater},
	{-7, -11, 0x3fd0525fcdcd76b0, 0x3fea4e1f3dd2aa46, 0x3fc50ecfb6ef711c, 0x3fe3c6f57c57fff2, ElevationLowland},
	{1000, 0, 0xbfd5cba98aa87957, 0x3fe2b361202f793d, 0xbfe45b7f8b9e820a, 0x3fcf5bb68ae2b2a8, ElevationDeepWater},
	{0, 1000, 0xbfd9bd08d1a31a6b, 0x3fc12b8140ceb9ec, 0xbfe2b7d6c3259d99, 0x3fc428dd2bcd3e24, ElevationDeepWater},
	{-1000, -1000, 0xbfd737c6aff61943, 0x3fe49bedbbf38da4, 0xbfe0bd7a1308d666, 0x3fc1a38ba7c41760, ElevationDeepWater},
	{12345, -6789, 0xbfdbdbbb847c538b, 0x3fdf2371f44eea7c, 0xbfe9e28978d234d9, 0x3fc9de18520af4f8, ElevationDeepWater},
	{32767, 0, 0x3fcee54d57bbe0e5, 0x3fea74561756cd7c, 0x3fb100e5aebf3e4a, 0x3ff0000000000000, ElevationLowland},
	{0, 32767, 0x3fd4138510f8d44b, 0x3fe58e2d222110b6, 0x3fbaf217d2f2967d, 0x3ff0000000000000, ElevationLowland},
	{-32767, 0, 0xbfbb6c4e8f06ef08, 0x3fe31f96f68562c3, 0xbfd44c2edee71d3b, 0x3ff0000000000000, ElevationDeepWater},
	{0, -32767, 0x3fc3d59bbd54e739, 0x3fccfc01e8d94e01, 0xbfa46a3db6ae2994, 0x3fecffe95291e933, ElevationShallowWater},
	{32767, -32767, 0x3fca7cdd62c1fb8d, 0x3fdb2536986de0e9, 0x3fdba56ce8ce9823, 0x3ff0000000000000, ElevationUpland},
	{-32767, 32767, 0x3fad3b96cb75f082, 0x3fe36c5291d83da0, 0xbfb55fc9e368a457, 0x3fe5bfa89dbe5473, ElevationShallowWater},
	{16384, -32767, 0x3fa4e3050c3a359e, 0x3fe8be41b1b44988, 0xbfb52a778728d721, 0x3ff0000000000000, ElevationShallowWater},
	{65535, -32767, 0x3fc052cc757968ba, 0x3fd2ee9a8e1440c0, 0xbfc58e3b039ea18b, 0x3fe0b95e84b6136e, ElevationDeepWater},
	{32768, 0, 0x3fad3b96cb75f082, 0x3fe36c5291d83da0, 0xbfb55fc9e368a457, 0x3fe5bfa89dbe5473, ElevationShallowWater},
}

func TestGoldenElevation(t *testing.T) {
	if len(goldenElevationValues) != len(goldenCoords) {
		t.Fatalf("the elevation golden table has %d rows for %d coordinates", len(goldenElevationValues), len(goldenCoords))
	}

	g := NewDefault(goldenSeed)
	for i, row := range goldenElevationValues {
		if [2]int64{row.q, row.r} != goldenCoords[i] {
			t.Fatalf("row %d is for (%d, %d), want (%d, %d)", i, row.q, row.r, goldenCoords[i][0], goldenCoords[i][1])
		}
		c := NewCoord(row.q, row.r)
		s := g.Sample(c)

		for _, f := range []struct {
			name string
			got  uint64
			want uint64
		}{
			{"elevation-raw", math.Float64bits(s.ElevationRaw), row.raw},
			{"ridge", math.Float64bits(s.Ridge), row.ridge},
			{"elevation", math.Float64bits(s.Elevation), row.elevation},
			{"relief", math.Float64bits(g.Relief(c)), row.relief},
		} {
			if f.got != f.want {
				t.Errorf("%s at (%d, %d) = %#016x (%v), want %#016x (%v)",
					f.name, row.q, row.r, f.got, math.Float64frombits(f.got), f.want, math.Float64frombits(f.want))
			}
		}

		if s.Band != row.band {
			t.Errorf("band at (%d, %d) = %v, want %v", row.q, row.r, s.Band, row.band)
		}
	}
}

// ---------------------------------------------------------------------------
// The climate composite
// ---------------------------------------------------------------------------

// The fourth golden table: the two climate axes of DESIGN.md 16 at the same
// coordinates, under the same seed and the same defaults.
//
// A separate table again, for the reason the others are separate from each
// other. What it pins that nothing above it does: the heat composite reads the
// elevation composite's output, so this is the only table where a change to
// elevation shows up twice — once in its own table and once here — and the
// difference between "elevation moved" and "only climate moved" is the
// difference between two commits.
//
// The two bands are recorded beside the two scalars for the reason the
// elevation band is: a threshold moved by a hair moves no float in this table
// and moves a band.
//
// The coordinates that are far apart cover all five heat bands and all five
// moisture bands between them, which is worth knowing: the rows near the origin
// are all one climate, because the heat field's wavelength is twelve thousand
// miles and they are a few dozen miles apart. That is the model working, and a
// table where the neighbors of the origin differed in band would be a heat
// field that had descended into the tile grid.
//
// Recorded under AlgorithmVersion 4 at world radius 32767.

// goldenClimateRow is one coordinate, the float64 bits of the two climate
// scalars, and the two bands they classify to.
type goldenClimateRow struct {
	q, r         int64
	heat         uint64
	moisture     uint64
	heatBand     HeatBand
	moistureBand MoistureBand
}

// goldenClimateValues is one row per coordinate in goldenCoords, in that order.
var goldenClimateValues = []goldenClimateRow{
	{0, 0, 0x3fe8dd08acd60bc0, 0xbfe1a0aaf44c712e, HeatHot, MoistureDry},
	{1, 0, 0x3fe8e58b926accbe, 0xbfe203f576e89aa4, HeatHot, MoistureDry},
	{0, 1, 0x3fe8dfb3ea5d2ba0, 0xbfe17f2d115ac310, HeatHot, MoistureDry},
	{-1, 0, 0x3fe8d478cbf941aa, 0xbfe1c2d2564f0489, HeatHot, MoistureDry},
	{0, -1, 0x3fe8da3c1bc43212, 0xbfe1e359867e2ecc, HeatHot, MoistureDry},
	{1, -1, 0x3fe8e2a526f60b8c, 0xbfe2196ee0c0ee1c, HeatHot, MoistureDry},
	{-1, 1, 0x3fe8d710f35def8c, 0xbfe1c6339de06ce6, HeatHot, MoistureDry},
	{7, 11, 0x3fe92adb9237c9be, 0xbfe3ab7d63db1bd0, HeatHot, MoistureArid},
	{-7, 11, 0x3fe8ac935c2b9a4c, 0xbfe3f817e1becdb9, HeatHot, MoistureArid},
	{7, -11, 0x3fe8ebf2a2bfa7ca, 0xbfe0fe14c78a3702, HeatHot, MoistureDry},
	{-7, -11, 0x3fe5e752fd2931b8, 0xbfe143cef1591f72, HeatHot, MoistureDry},
	{1000, 0, 0x3fdbc8b0435b0e34, 0x3fcb764d1ff53c38, HeatWarm, MoistureHumid},
	{0, 1000, 0xbfec65aea1c9414f, 0xbf93346f8aee8220, HeatPolar, MoistureModerate},
	{-1000, -1000, 0x3fdb988c68cb5670, 0x3fc7632f8fcabbe0, HeatWarm, MoistureModerate},
	{12345, -6789, 0xbfe1deee52fde538, 0xbfaec96f8fe2f820, HeatCold, MoistureModerate},
	{32767, 0, 0x3fe758957113315f, 0x3f8dfd5bdbe8df00, HeatHot, MoistureModerate},
	{0, 32767, 0x3fe9035cb57bf1ba, 0xbfe197cb0cfadc2e, HeatHot, MoistureDry},
	{-32767, 0, 0x3fa0266f18b560e0, 0x3fe091c80c98fff6, HeatTemperate, MoistureHumid},
	{0, -32767, 0x3fb950ca3a90fbf0, 0x3fe05c75196ca31e, HeatTemperate, MoistureHumid},
	{32767, -32767, 0x3fba0b3eebd9451a, 0x3fe3710088d457f6, HeatTemperate, MoistureSaturated},
	{-32767, 32767, 0xbfe4204766a7ea99, 0xbfaba77b6a55ddd0, HeatPolar, MoistureModerate},
	{16384, -32767, 0x3fc3c9d67096b988, 0xbfcab849f3a3320c, HeatTemperate, MoistureDry},
	{65535, -32767, 0x3fe8dd08acd60bc0, 0xbfe1a0aaf44c712e, HeatHot, MoistureDry},
	{32768, 0, 0xbfe4204766a7ea99, 0xbfaba77b6a55ddd0, HeatPolar, MoistureModerate},
}

func TestGoldenClimate(t *testing.T) {
	if len(goldenClimateValues) != len(goldenCoords) {
		t.Fatalf("the climate golden table has %d rows for %d coordinates", len(goldenClimateValues), len(goldenCoords))
	}

	g := NewDefault(goldenSeed)
	for i, row := range goldenClimateValues {
		if [2]int64{row.q, row.r} != goldenCoords[i] {
			t.Fatalf("row %d is for (%d, %d), want (%d, %d)", i, row.q, row.r, goldenCoords[i][0], goldenCoords[i][1])
		}
		c := NewCoord(row.q, row.r)
		s := g.Sample(c)

		for _, f := range []struct {
			name string
			got  uint64
			want uint64
		}{
			{"heat", math.Float64bits(s.Heat), row.heat},
			{"moisture", math.Float64bits(s.Moisture), row.moisture},
		} {
			if f.got != f.want {
				t.Errorf("%s at (%d, %d) = %#016x (%v), want %#016x (%v)",
					f.name, row.q, row.r, f.got, math.Float64frombits(f.got), f.want, math.Float64frombits(f.want))
			}
		}

		if want := (Climate{Heat: row.heatBand, Moisture: row.moistureBand}); s.Climate != want {
			t.Errorf("climate at (%d, %d) = %v, want %v", row.q, row.r, s.Climate, want)
		}
	}
}

// TestGoldenClimateCoversEveryBand states what the table above is also good
// for: between them the rows produce all five heat bands and all five moisture
// bands, so the golden suite is a reachability check on both ladders as well as
// a bit-pattern check. See DESIGN.md 30.8.
func TestGoldenClimateCoversEveryBand(t *testing.T) {
	heat := map[HeatBand]bool{}
	moisture := map[MoistureBand]bool{}
	for _, row := range goldenClimateValues {
		heat[row.heatBand] = true
		moisture[row.moistureBand] = true
	}
	for _, h := range Heats() {
		if !heat[h] {
			t.Errorf("no golden coordinate is %v", h)
		}
	}
	for _, m := range Moistures() {
		if !moisture[m] {
			t.Errorf("no golden coordinate is %v", m)
		}
	}
}
