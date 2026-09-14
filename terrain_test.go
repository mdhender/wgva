// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"reflect"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// The vocabulary
// ---------------------------------------------------------------------------

// TestTerrainVocabulary covers what stands in for exhaustive matching on the
// declared side: every terrain names itself, reports itself valid, and no two
// share a name or a number. The other half — that every terrain except the two
// inland-water values is actually reachable — is TestTerrainDistribution.
func TestTerrainVocabulary(t *testing.T) {
	seenName := map[string]bool{}
	seenValue := map[Terrain]bool{}
	for _, tr := range Terrains() {
		if !tr.Valid() {
			t.Errorf("%d is in Terrains and reports itself invalid", uint8(tr))
		}
		name := tr.String()
		if name == "" || name == "unknown" {
			t.Errorf("%d has no name", uint8(tr))
		}
		if seenName[name] {
			t.Errorf("%q names two terrains", name)
		}
		seenName[name] = true
		if seenValue[tr] {
			t.Errorf("%d is declared twice", uint8(tr))
		}
		seenValue[tr] = true
	}

	if got := len(Terrains()); got != terrainCount {
		t.Errorf("Terrains returns %d values, want terrainCount = %d", got, terrainCount)
	}

	// Terrain.Valid is a range check, which is sound only while the declared
	// values are contiguous from zero. This is what keeps that true: a terrain
	// added at the end without TerrainCoast moving would be a value that exists
	// and is not valid.
	for i, tr := range Terrains() {
		if uint8(tr) != uint8(i) {
			t.Fatalf("%v is %d and is the %dth declared terrain; Terrain.Valid is a range check and needs the values contiguous from zero",
				tr, uint8(tr), i)
		}
	}

	// The values are persisted, so the numbers themselves are the contract and
	// are asserted rather than assumed from declaration order.
	for value, want := range map[Terrain]string{
		0: "deep-ocean", 3: "coastal-water", 4: "inland-sea", 5: "lake",
		6: "glacial-ice", 14: "plains", 24: "volcano", 26: "coast",
	} {
		if got := value.String(); got != want {
			t.Errorf("terrain %d is %q, want %q; these values are persisted", uint8(value), got, want)
		}
	}

	if undeclared := Terrain(200); undeclared.Valid() || undeclared.String() != "unknown" {
		t.Error("an undeclared terrain reports itself valid or names itself")
	}
}

// TestTerrainWaterIsTheSixWetVariants asserts IsWater answers about the value
// rather than about what this version emits: the two inland-water terrains are
// water even though DESIGN.md 17.1 produces neither.
func TestTerrainWaterIsTheSixWetVariants(t *testing.T) {
	want := map[Terrain]bool{
		TerrainDeepOcean: true, TerrainOcean: true, TerrainShallowSea: true,
		TerrainCoastalWater: true, TerrainInlandSea: true, TerrainLake: true,
	}
	for _, tr := range Terrains() {
		if got := tr.IsWater(); got != want[tr] {
			t.Errorf("%v.IsWater() is %v, want %v", tr, got, want[tr])
		}
	}
}

// TestCoverTableIsTotal asserts the climate cover table has an entry for every
// declared pair of bands and that every entry is a declared terrain.
//
// It is what stands in for the exhaustiveness Go's type system cannot give over
// a table: the table is indexed by band value, so a band renumbered in either
// enumeration would silently transpose the map rather than fail to compile.
func TestCoverTableIsTotal(t *testing.T) {
	if len(coverTable) != len(Heats()) {
		t.Fatalf("the cover table has %d rows for %d heat bands", len(coverTable), len(Heats()))
	}
	if len(coverTable[0]) != len(Moistures()) {
		t.Fatalf("the cover table has %d columns for %d moisture bands", len(coverTable[0]), len(Moistures()))
	}

	for i, h := range Heats() {
		if int(h) != i {
			t.Fatalf("%v is %d and is the %dth heat band; the cover table is indexed by value", h, uint8(h), i)
		}
		for j, m := range Moistures() {
			if int(m) != j {
				t.Fatalf("%v is %d and is the %dth moisture band; the cover table is indexed by value", m, uint8(m), j)
			}
			got := coverFor(h, m)
			if !got.Valid() {
				t.Errorf("%v/%v covers to %d, which is not a declared terrain", h, m, uint8(got))
			}
			if got.IsWater() {
				t.Errorf("%v/%v covers to %v, which is water; the cover table classifies land", h, m, got)
			}
		}
	}

	// The documented default. An undeclared band is a programming error
	// upstream and the classifier's job is still to return a terrain.
	if got := coverFor(HeatBand(200), MoistureBand(200)); got != TerrainPlains {
		t.Errorf("an undeclared pair covers to %v, want the documented default %v", got, TerrainPlains)
	}
}

// ---------------------------------------------------------------------------
// The ordered rules
// ---------------------------------------------------------------------------

// TestTerrainRulesAreOrdered is DESIGN.md 17's ordering asserted one
// displacement at a time: each case is a tile that satisfies two rules, and the
// expected answer is the one that comes first.
//
// The order is the algorithm, and every one of these would classify plausibly
// under the wrong rule — which is what makes an ordering defect something a
// picture does not show. A marsh misread as a coast is still a green tile at
// the water's edge.
func TestTerrainRulesAreOrdered(t *testing.T) {
	cfg := DefaultConfig()

	// An ordinary temperate lowland, which every case below modifies.
	base := terrainInputs{
		elevation:   0.05,
		band:        ElevationLowland,
		relief:      0.1,
		heat:        0,
		heatBand:    HeatTemperate,
		wetness:     0,
		wetnessBand: MoistureModerate,
		volcanic:    0,
	}
	with := func(f func(*terrainInputs)) terrainInputs {
		in := base
		f(&in)
		return in
	}

	for _, tc := range []struct {
		name string
		in   terrainInputs
		want Terrain
		over Terrain
	}{
		{
			name: "the rim outranks the ground it stands on",
			in:   with(func(in *terrainInputs) { in.rim = true; in.rimKind = RimDeepOcean }),
			want: TerrainDeepOcean, over: TerrainGrassland,
		},
		{
			name: "an ice rim is ice whatever the climate",
			in: with(func(in *terrainInputs) {
				in.rim, in.rimKind = true, RimPolarIce
				in.heat, in.heatBand = 0.9, HeatHot
			}),
			want: TerrainGlacialIce, over: TerrainSavanna,
		},
		{
			name: "water beside land is coastal water, whatever its depth",
			in: with(func(in *terrainInputs) {
				in.elevation, in.band = -0.9, ElevationDeepWater
				in.landNeighbor = true
			}),
			want: TerrainCoastalWater, over: TerrainDeepOcean,
		},
		{
			name: "ice outranks the elevated family",
			in: with(func(in *terrainInputs) {
				in.elevation, in.band = 0.9, ElevationMountain
				in.heat = cfg.Terrain.IceHeat
			}),
			want: TerrainGlacialIce, over: TerrainAlpine,
		},
		{
			name: "a volcano outranks the mountain it stands on",
			in: with(func(in *terrainInputs) {
				in.elevation, in.band = 0.9, ElevationMountain
				in.volcanic, in.relief = 0.9, 0.9
			}),
			want: TerrainVolcano, over: TerrainMountain,
		},
		{
			name: "a volcanic province without a cone is volcanic highland",
			in: with(func(in *terrainInputs) {
				in.elevation, in.band = 0.6, ElevationHighland
				in.volcanic, in.relief = cfg.Terrain.VolcanicHighlandThreshold, 0
			}),
			want: TerrainVolcanicHighland, over: TerrainHills,
		},
		{
			name: "a cold mountain is alpine rather than bare rock",
			in: with(func(in *terrainInputs) {
				in.elevation, in.band = 0.9, ElevationMountain
				in.heat = cfg.Terrain.AlpineHeat
			}),
			want: TerrainAlpine, over: TerrainMountain,
		},
		{
			name: "steep ground is hills before it is a climate cover",
			in:   with(func(in *terrainInputs) { in.relief = cfg.Terrain.HillsRelief }),
			want: TerrainHills, over: TerrainGrassland,
		},
		{
			name: "a saturated shoreline is a marsh rather than a coast",
			in: with(func(in *terrainInputs) {
				in.wetness, in.wetnessBand = cfg.Terrain.WetlandWetness, MoistureSaturated
				in.waterNeighbor = true
			}),
			want: TerrainMarsh, over: TerrainCoast,
		},
		{
			name: "a cold wetland is a bog",
			in: with(func(in *terrainInputs) {
				in.wetness, in.wetnessBand = cfg.Terrain.WetlandWetness, MoistureSaturated
				in.heat, in.heatBand = cfg.Terrain.BogHeat, HeatCold
			}),
			want: TerrainBog, over: TerrainMarsh,
		},
		{
			name: "a warm wetland is a swamp",
			in: with(func(in *terrainInputs) {
				in.wetness, in.wetnessBand = cfg.Terrain.WetlandWetness, MoistureSaturated
				in.heat, in.heatBand = cfg.Terrain.SwampHeat, HeatWarm
			}),
			want: TerrainSwamp, over: TerrainRainforest,
		},
		{
			name: "a dry shoreline is a coast rather than a grassland",
			in:   with(func(in *terrainInputs) { in.waterNeighbor = true }),
			want: TerrainCoast, over: TerrainGrassland,
		},
		{
			name: "a steep desert is badlands",
			in: with(func(in *terrainInputs) {
				in.heatBand, in.wetnessBand = HeatHot, MoistureArid
				in.relief = cfg.Terrain.BadlandsRelief
			}),
			want: TerrainBadlands, over: TerrainDesert,
		},
		{
			name: "ordinary ground is whatever the climate covers it with",
			in:   base,
			want: TerrainGrassland, over: TerrainCoast,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfg.classifyTerrain(tc.in); got != tc.want {
				t.Errorf("classified as %v, want %v over %v", got, tc.want, tc.over)
			}
		})
	}
}

// TestUnknownRimKindIsStillAClosedRim asserts the documented default of the one
// switch in the classifier that has a value from outside the tile.
//
// Validation refuses an undeclared rim kind, so this is unreachable from a
// generator — and the answer is deep ocean rather than a panic because an
// unknown rim is still a closed rim, and deep ocean is what the default one is
// made of.
func TestUnknownRimKindIsStillAClosedRim(t *testing.T) {
	cfg := DefaultConfig()
	got := cfg.classifyTerrain(terrainInputs{rim: true, rimKind: RimKind(200)})
	if got != TerrainDeepOcean {
		t.Errorf("an undeclared rim kind classified as %v, want %v", got, TerrainDeepOcean)
	}
}

// ---------------------------------------------------------------------------
// The tile
// ---------------------------------------------------------------------------

// TestTileShape is the shape assertion of DESIGN.md 30.12.
//
// A Tile is a plain value with no pointers so that a batch API can fill a slice
// of them with no allocation and no indirection, and so that the garbage
// collector has nothing to scan. A pointer or a slice added here would be
// invisible in review and would show up as an allocation per tile in a render.
func TestTileShape(t *testing.T) {
	typ := reflect.TypeOf(Tile{})
	for i := range typ.NumField() {
		f := typ.Field(i)
		switch f.Type.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func,
			reflect.Interface, reflect.String, reflect.UnsafePointer:
			t.Errorf("Tile.%s is a %v; a Tile must be a pointer-free value", f.Name, f.Type.Kind())
		}
	}

	// Four float64s, the coordinate, three single-byte classifications and a
	// bool. The number is asserted rather than computed so that a field added
	// without thought fails here.
	if got, want := typ.Size(), reflect.TypeOf(Coord{}).Size()+4*8+8; got != want {
		t.Errorf("a Tile is %d bytes, want %d; DESIGN.md 4.3 asks for a small pointer-free value", got, want)
	}
}

// TestTileIsItsParts asserts every field of a Tile is bit-identical to the
// method that returns the same thing on its own.
//
// It is the same assertion TestSampleIsItsParts makes and it matters more here:
// Tile is the public entry point, so a Tile whose elevation disagreed with
// ElevationAt in the last place would be two answers to one question with
// nothing to say which was the world.
func TestTileIsItsParts(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(500, 0x71133) {
		tile := g.Tile(c)

		if tile.Coord != c {
			t.Fatalf("Tile((%d, %d)).Coord is (%d, %d)", c.Q(), c.R(), tile.Coord.Q(), tile.Coord.R())
		}
		for _, f := range []struct {
			name string
			got  float64
			want float64
		}{
			{"ElevationValue", tile.ElevationValue, g.ElevationAt(c)},
			{"HeatValue", tile.HeatValue, g.HeatAt(c)},
			{"MoistureValue", tile.MoistureValue, g.MoistureAt(c)},
			{"ReliefValue", tile.ReliefValue, g.Relief(c)},
		} {
			if math.Float64bits(f.got) != math.Float64bits(f.want) {
				t.Fatalf("Tile.%s at (%d, %d) = %#016x (%v), want %#016x (%v)",
					f.name, c.Q(), c.R(), math.Float64bits(f.got), f.got,
					math.Float64bits(f.want), f.want)
			}
		}
		if got, want := tile.Elevation, g.ElevationBandAt(c); got != want {
			t.Fatalf("Tile.Elevation at (%d, %d) is %v, want %v", c.Q(), c.R(), got, want)
		}
		if got, want := tile.Climate, g.ClimateAt(c); got != want {
			t.Fatalf("Tile.Climate at (%d, %d) is %v, want %v", c.Q(), c.R(), got, want)
		}
		if got, want := tile.Terrain, g.TerrainAt(c); got != want {
			t.Fatalf("Tile.Terrain at (%d, %d) is %v, want %v", c.Q(), c.R(), got, want)
		}
	}
}

// TestTileReportsTheClimatesMoistureNotTheWetness asserts the one place a Tile
// could plausibly report the wrong number.
//
// Terrain is classified from moisture after the basin product, and a Tile that
// reported that as its moisture would be reporting a value its climate model
// never produced — which would corrupt every distribution measurement that
// reads the field and would make the two-axis climate classification disagree
// with the scalar beside it.
func TestTileReportsTheClimatesMoistureNotTheWetness(t *testing.T) {
	g := NewDefault(probeSeed)
	cfg := g.Config()

	differed := 0
	coords := probeCoords(1000, 0x717e7)
	for _, c := range coords {
		tile := g.Tile(c)
		if got, want := tile.MoistureValue, g.MoistureAt(c); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("Tile.MoistureValue at (%d, %d) is %v, want the climate's %v", c.Q(), c.R(), got, want)
		}
		if cfg.wetness(tile.MoistureValue, g.BasinAt(c)) != tile.MoistureValue {
			differed++
		}
		if got, want := tile.Climate.Moisture, cfg.Climate.MoistureBands.Classify(tile.MoistureValue); got != want {
			t.Fatalf("Tile.Climate.Moisture at (%d, %d) is %v, want %v, which is what the reported scalar classifies to",
				c.Q(), c.R(), got, want)
		}
	}
	if differed == 0 {
		t.Fatalf("wetness equalled moisture at all %d coordinates; the assertion above is vacuous", len(coords))
	}
}

// TestRimFlagIsFalseUntilPhaseSeven pins the state this phase leaves the rim
// in, so that the phase that implements the profile has something to change.
//
// DESIGN.md 32's phase 7 is what sets the flag and forces the band. The
// classifier's rim rule is written and tested because the *order* is what this
// phase settles; nothing sets its input yet.
func TestRimFlagIsFalseUntilPhaseSeven(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range []Coord{
		NewCoord(0, 0),
		NewCoord(WorldRadius, 0),
		NewCoord(0, -WorldRadius),
		NewCoord(WorldRadius, -WorldRadius),
	} {
		if g.Tile(c).Rim {
			t.Errorf("Tile.Rim is set at (%d, %d), which is phase 7's to do", c.Q(), c.R())
		}
	}
}

// ---------------------------------------------------------------------------
// What the classification says about the world
// ---------------------------------------------------------------------------

// TestTerrainAgreesWithTheElevationBand asserts the water terrains and the
// water elevation bands describe the same tiles.
//
// Both come from the same rule — elevation at or below zero is water — through
// two different paths, so a disagreement means one of them has acquired a
// threshold of its own. Coastal water is the case worth having a test for: it
// is chosen by adjacency rather than by depth and could easily be assigned to a
// tile that is not wet at all.
func TestTerrainAgreesWithTheElevationBand(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(4000, 0x7e11) {
		tile := g.Tile(c)
		if tile.Terrain.IsWater() != tile.Elevation.IsWater() {
			t.Fatalf("at (%d, %d) terrain %v and elevation band %v disagree about water at elevation %v",
				c.Q(), c.R(), tile.Terrain, tile.Elevation, tile.ElevationValue)
		}
	}
}

// TestTheWatersEdgeReadsItsNeighbors asserts the two terrains that are decided
// by adjacency are decided by adjacency.
//
// They are the only thing in the classifier that is not a function of the tile
// alone, and they are affordable only because relief has already paid for the
// same six evaluations. A coast with no water next to it would mean the
// neighbor walk and the classifier had come apart.
func TestTheWatersEdgeReadsItsNeighbors(t *testing.T) {
	g := NewDefault(probeSeed)

	coasts, coastalWaters := 0, 0
	for _, c := range probeCoords(4000, 0x7e12) {
		switch g.TerrainAt(c) {
		case TerrainCoast:
			coasts++
			if !hasNeighbor(g, c, func(e float64) bool { return e <= 0 }) {
				t.Fatalf("the coast at (%d, %d) has no water neighbor", c.Q(), c.R())
			}
		case TerrainCoastalWater:
			coastalWaters++
			if !hasNeighbor(g, c, func(e float64) bool { return e > 0 }) {
				t.Fatalf("the coastal water at (%d, %d) has no land neighbor", c.Q(), c.R())
			}
		}
	}
	if coasts == 0 || coastalWaters == 0 {
		t.Fatalf("found %d coasts and %d coastal waters; the assertions above are vacuous", coasts, coastalWaters)
	}
}

// hasNeighbor reports whether any of the six neighbors' elevation scalars
// satisfies the predicate. The walk is in fixed direction order, like every
// other neighbor walk in the module.
func hasNeighbor(g *Generator, c Coord, ok func(float64) bool) bool {
	for d := range 6 {
		if ok(g.ElevationAt(c.Neighbor(d))) {
			return true
		}
	}
	return false
}

// TestTerrainDistribution is the distribution test of DESIGN.md 30.8 for the
// terrain vocabulary, and it carries two assertions the others do not.
//
// **Every declared terrain must be produced somewhere, except the two
// inland-water values, which must be produced nowhere.** The first is what
// stands in for the exhaustiveness Go cannot give: a terrain unreachable
// because of a threshold typo is otherwise invisible, since nothing fails and
// the map still looks like a map. The second is the decision record of
// DESIGN.md 17.1 asserted rather than described — the day somebody implements
// inland water, this is the test that says so.
//
// The sample is four seeds because the rare terrains are rare on purpose. A
// volcano is about one tile in three thousand of land, which is what three
// conditions on three continuous fields comes to, and a single twenty-thousand
// coordinate sample would find a handful.
func TestTerrainDistribution(t *testing.T) {
	const n = 20000
	seeds := []Seed{probeSeed, 1, 7, goldenSeed}

	counts := map[Terrain]int{}
	total := 0
	for _, seed := range seeds {
		g := NewDefault(seed)
		for _, c := range probeCoords(n, uint64(seed)) {
			tr := g.TerrainAt(c)
			if !tr.Valid() {
				t.Fatalf("seed %#x: (%d, %d) classified as %d, which is not a declared terrain",
					uint64(seed), c.Q(), c.R(), uint8(tr))
			}
			counts[tr]++
			total++
		}
	}

	for _, tr := range Terrains() {
		switch tr {
		case TerrainInlandSea, TerrainLake:
			if counts[tr] != 0 {
				t.Errorf("%v was produced %d times; DESIGN.md 17.1 omits inland water, and emitting it is a decision that belongs in a commit message",
					tr, counts[tr])
			}
		default:
			if counts[tr] == 0 {
				t.Errorf("%v is produced nowhere in %d coordinates across %d seeds; a terrain nothing reaches is a threshold typo",
					tr, total, len(seeds))
			}
			if fraction := float64(counts[tr]) / float64(total); fraction > 0.5 {
				t.Errorf("%v is %.3f of the world; no single terrain should swallow it", tr, fraction)
			}
		}
	}

	// The land fraction, measured through terrain rather than through
	// elevation. It is the same rule read the other way round, and it is the
	// number DESIGN.md 4.2 reasons about when it settles the world radius.
	water := 0
	for tr, n := range counts {
		if tr.IsWater() {
			water += n
		}
	}
	if land := 1 - float64(water)/float64(total); land < 0.2 || land > 0.45 {
		t.Errorf("the land fraction is %.3f, which is a long way from the third DESIGN.md 4.2 reasons about", land)
	}
}

// TestTerrainIsNotAnIndependentDraw is DESIGN.md 33.1 measured: terrain is a
// classification of continuous fields, so neighboring tiles usually agree.
//
// Independent per-tile selection over twenty-five reachable terrains would
// agree about four percent of the time. The bound here is loose on purpose —
// what it is testing is that terrain is downstream of geography at all, not how
// smooth the geography is.
func TestTerrainIsNotAnIndependentDraw(t *testing.T) {
	g := NewDefault(probeSeed)

	same, total := 0, 0
	for _, c := range probeCoords(2000, 0x7e13) {
		here := g.TerrainAt(c)
		for d := range 6 {
			if g.TerrainAt(c.Neighbor(d)) == here {
				same++
			}
			total++
		}
	}

	if agreement := float64(same) / float64(total); agreement < 0.5 {
		t.Errorf("neighboring tiles share a terrain %.3f of the time; an independent per-tile draw would manage about 0.04, and a classification of continuous fields should manage a great deal more",
			agreement)
	}
}

// ---------------------------------------------------------------------------
// Determinism
// ---------------------------------------------------------------------------

// TestTerrainIsDeterministic asserts a tile is a pure function of the seed, the
// coordinate, the algorithm version, the world radius, and the configuration:
// two generators built the same way agree, and the second visits the
// coordinates in the opposite order.
func TestTerrainIsDeterministic(t *testing.T) {
	a := NewDefault(probeSeed)
	b := NewDefault(probeSeed)

	coords := probeCoords(1000, 0x7e14)
	first := make([]Tile, len(coords))
	for i, c := range coords {
		first[i] = a.Tile(c)
	}
	for i := len(coords) - 1; i >= 0; i-- {
		if got := b.Tile(coords[i]); got != first[i] {
			t.Fatalf("the tile at (%d, %d) is %+v on the second pass and was %+v on the first",
				coords[i].Q(), coords[i].R(), got, first[i])
		}
	}
}

// TestTerrainIsConcurrentlyDeterministic is DESIGN.md 30.3 for the tile: the
// same tiles generated from several goroutines are the same tiles.
//
// Generator is immutable and every tile is a pure function of its own
// coordinate, so this is structurally true rather than true by luck — which is
// what makes it worth asserting, because the structure is maintained by hand
// and Go cannot check it.
func TestTerrainIsConcurrentlyDeterministic(t *testing.T) {
	g := NewDefault(probeSeed)
	coords := probeCoords(2000, 0x7e15)

	want := make([]Tile, len(coords))
	for i, c := range coords {
		want[i] = g.Tile(c)
	}

	const workers = 8
	got := make([]Tile, len(coords))
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := w; i < len(coords); i += workers {
				got[i] = g.Tile(coords[i])
			}
		})
	}
	wg.Wait()

	for i, c := range coords {
		if got[i] != want[i] {
			t.Fatalf("the tile at (%d, %d) differs between the sequential and concurrent passes", c.Q(), c.R())
		}
	}
}

// TestTerrainRespectsTheWrap asserts that a coordinate and its mirror images
// are one tile.
//
// Terrain reads six neighbors, so this is a stronger statement than the
// elevation and climate versions: it asserts that the neighborhood of a
// coordinate near the seam is the neighborhood of its image, which is the whole
// of what "values that normalize to the same coordinate identify the same tile"
// has to mean once a classification reads its neighbors.
func TestTerrainRespectsTheWrap(t *testing.T) {
	g := NewDefault(probeSeed)

	for _, c := range probeCoords(200, 0x7e16) {
		want := g.Tile(c)
		for _, m := range mirrorCenters {
			image := NewCoord(int64(c.Q())+m[0], int64(c.R())+m[1])
			if got := g.Tile(image); got != want {
				t.Fatalf("the tile at (%d, %d) differs from its image at (%d, %d)",
					c.Q(), c.R(), image.Q(), image.R())
			}
		}
	}
}
