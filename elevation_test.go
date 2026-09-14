// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"testing"
)

// probeSeed is the seed the measurements in this file are taken under. It is
// the one the terrain tuning tool opens on, so a number quoted here is a number
// somebody can go and look at.
const probeSeed Seed = 0x0123456789abcdef

// probeCoords returns n coordinates spread over the whole map, from a generator
// seeded independently of the world.
//
// math/rand/v2 is test data rather than generation, which is the only thing it
// is permitted for. The coordinates are what the sample is over; nothing about
// them reaches a generated value.
func probeCoords(n int, seed uint64) []Coord {
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	out := make([]Coord, n)
	for i := range out {
		out[i] = NewCoord(
			rng.Int64N(2*WorldRadius+1)-WorldRadius,
			rng.Int64N(2*WorldRadius+1)-WorldRadius,
		)
	}
	return out
}

// ---------------------------------------------------------------------------
// The enumeration
// ---------------------------------------------------------------------------

// TestElevationBandNames covers what stands in for exhaustive matching on the
// declared side: every band names itself, reports itself valid, and no two share
// a name or a number. The other half — that every band is actually reachable —
// is TestElevationDistribution.
func TestElevationBandNames(t *testing.T) {
	seenName := map[string]bool{}
	seenValue := map[Elevation]bool{}
	for _, e := range Elevations() {
		if !e.Valid() {
			t.Errorf("%d is in Elevations and reports itself invalid", uint8(e))
		}
		name := e.String()
		if name == "" || name == "unknown" {
			t.Errorf("%d has no name", uint8(e))
		}
		if seenName[name] {
			t.Errorf("%q names two bands", name)
		}
		seenName[name] = true
		if seenValue[e] {
			t.Errorf("%d is declared twice", uint8(e))
		}
		seenValue[e] = true
	}

	if got := len(Elevations()); got != 6 {
		t.Errorf("Elevations returns %d bands, want the 6 of DESIGN.md 14.1", got)
	}

	// The values are persisted, so the numbers themselves are the contract and
	// are asserted rather than assumed from declaration order.
	for value, want := range map[Elevation]string{
		0: "deep-water", 1: "shallow-water", 2: "lowland",
		3: "upland", 4: "highland", 5: "mountain",
	} {
		if got := value.String(); got != want {
			t.Errorf("elevation %d is %q, want %q; these values are persisted", uint8(value), got, want)
		}
	}

	if undeclared := Elevation(200); undeclared.Valid() || undeclared.String() != "unknown" {
		t.Error("an undeclared band reports itself valid or names itself")
	}
}

// TestElevationWaterIsTheTwoBandsBelowSeaLevel pins the land/water rule of
// DESIGN.md 15 on the classification side.
func TestElevationWaterIsTheTwoBandsBelowSeaLevel(t *testing.T) {
	for _, e := range Elevations() {
		want := e == ElevationDeepWater || e == ElevationShallowWater
		if e.IsWater() != want {
			t.Errorf("%v.IsWater() = %v, want %v", e, e.IsWater(), want)
		}
	}
}

// TestClassifyCutsAtTheThresholds covers the comparisons themselves, including
// the two that decide where water stops.
func TestClassifyCutsAtTheThresholds(t *testing.T) {
	b := DefaultConfig().Elevation.Bands

	cases := []struct {
		name string
		e    float64
		want Elevation
	}{
		{"the floor", -1, ElevationDeepWater},
		{"at the deep-water threshold", b.DeepWater, ElevationDeepWater},
		{"just above it", math.Nextafter(b.DeepWater, 1), ElevationShallowWater},
		// DESIGN.md 15: elevation <= 0 is water. Sea level itself is water, and
		// the very next float above it is land.
		{"at sea level", 0, ElevationShallowWater},
		{"negative zero", math.Copysign(0, -1), ElevationShallowWater},
		{"just above sea level", math.SmallestNonzeroFloat64, ElevationLowland},
		{"just below the upland threshold", math.Nextafter(b.Upland, -1), ElevationLowland},
		{"at the upland threshold", b.Upland, ElevationUpland},
		{"at the highland threshold", b.Highland, ElevationHighland},
		{"at the mountain threshold", b.Mountain, ElevationMountain},
		{"the ceiling", 1, ElevationMountain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.Classify(tc.e); got != tc.want {
				t.Errorf("Classify(%v) = %v, want %v", tc.e, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The shaping functions, derived independently
// ---------------------------------------------------------------------------

// TestSeaLevelRescale asserts the three properties the map has to have, against
// values worked out from the definition rather than recorded from the code.
func TestSeaLevelRescale(t *testing.T) {
	for _, sea := range []float64{0.25, 0.5, 0.62, 0.9} {
		cfg := DefaultConfig()
		cfg.SeaLevel = sea

		// The composite value that means sea level maps to exactly zero, and it
		// is exactly zero rather than nearly: the land/water rule compares
		// against it.
		if got := cfg.seaLevelRescale(2*sea - 1); got != 0 {
			t.Errorf("sea level %v: the composite at sea level rescaled to %v, want 0", sea, got)
		}
		// The two ends are fixed, which is what keeps DESIGN.md 14's three
		// anchor values meaning what they say.
		if got := cfg.seaLevelRescale(-1); got != -1 {
			t.Errorf("sea level %v: the floor rescaled to %v, want -1", sea, got)
		}
		if got := cfg.seaLevelRescale(+1); got != +1 {
			t.Errorf("sea level %v: the ceiling rescaled to %v, want +1", sea, got)
		}

		// Monotonic, so a higher composite is never lower ground.
		prev := math.Inf(-1)
		for i := range 2001 {
			v := float64(i)/1000 - 1
			e := cfg.seaLevelRescale(v)
			if e < prev {
				t.Fatalf("sea level %v: rescale fell from %v to %v at %v", sea, prev, e, v)
			}
			if e < -1 || e > 1 {
				t.Fatalf("sea level %v: rescale(%v) = %v, outside [-1, +1]", sea, v, e)
			}
			prev = e
		}
	}
}

// TestSeaLevelDecidesTheLandFraction is the other half of the rescale: which
// side of zero a composite lands on is decided by SeaLevel and nothing else, so
// raising it can only turn land into water.
func TestSeaLevelDecidesTheLandFraction(t *testing.T) {
	coords := probeCoords(3000, 11)

	var previous int
	for i, sea := range []float64{0.4, 0.5, 0.6, 0.7} {
		cfg := DefaultConfig()
		cfg.SeaLevel = sea
		g, err := New(probeSeed, cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		land := 0
		for _, c := range coords {
			if g.IsLand(c) {
				land++
			}
		}
		if i > 0 && land >= previous {
			t.Errorf("raising sea level to %v did not reduce the land count: %d then %d", sea, previous, land)
		}
		previous = land
	}
}

// TestContrastFixesTheEndsAndTheMiddle asserts what a contrast pass is for,
// against the definition rather than against recorded output.
func TestContrastFixesTheEndsAndTheMiddle(t *testing.T) {
	for passes := range MaxContrastPasses + 1 {
		for _, fixed := range []float64{-1, 0, +1} {
			if got := contrast(fixed, passes); got != fixed {
				t.Errorf("%d passes moved the fixed point %v to %v", passes, fixed, got)
			}
		}

		prev := math.Inf(-1)
		for i := range 2001 {
			v := float64(i)/1000 - 1
			w := contrast(v, passes)
			// The tolerance is rounding, not slack. Near the ends the cubic's
			// two terms are within an ulp of cancelling, so a later input can
			// land a bit or two below an earlier output; what would be a defect
			// is a reversal large enough to invert an ordering that matters,
			// and a few ulps is not one.
			if w < prev-1e-12 {
				t.Fatalf("%d passes: contrast fell from %v to %v at %v", passes, prev, w, v)
			}
			prev = max(prev, w)
			if w < -1 || w > 1 {
				t.Fatalf("%d passes: contrast(%v) = %v, outside [-1, +1]", passes, v, w)
			}
			// The whole point: a pass moves a value away from the middle.
			if passes > 0 {
				if v > 0 && w < v-1e-12 {
					t.Fatalf("%d passes: contrast(%v) = %v, which is toward the middle", passes, v, w)
				}
				if v < 0 && w > v+1e-12 {
					t.Fatalf("%d passes: contrast(%v) = %v, which is toward the middle", passes, v, w)
				}
			}
		}
	}

	if got := contrast(0.3, 0); got != 0.3 {
		t.Errorf("zero passes changed %v to %v; it must be the identity", 0.3, got)
	}
}

// TestSmoothstepUnitClampsAndJoins covers the ramp the ridge land mask is built
// from. The clamp is what makes it total; without it a value well outside the
// ramp runs away cubically.
func TestSmoothstepUnitClampsAndJoins(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{-100, 0}, {-1, 0}, {0, 0}, {0.5, 0.5}, {1, 1}, {2, 1}, {100, 1},
	} {
		if got := smoothstepUnit(tc.in); got != tc.want {
			t.Errorf("smoothstepUnit(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	prev := math.Inf(-1)
	for i := range 1001 {
		v := smoothstepUnit(float64(i) / 1000)
		if v < prev {
			t.Fatalf("smoothstepUnit fell from %v to %v", prev, v)
		}
		prev = v
	}
}

// ---------------------------------------------------------------------------
// The composite
// ---------------------------------------------------------------------------

// TestElevationIsInRange covers the one guarantee every consumer depends on.
func TestElevationIsInRange(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(20000, 3) {
		e := g.ElevationAt(c)
		if math.IsNaN(e) || e < -1 || e > 1 {
			t.Fatalf("elevation at (%d, %d) is %v, outside [-1, +1]", c.Q(), c.R(), e)
		}
		if r := g.Relief(c); math.IsNaN(r) || r < 0 || r > 1 {
			t.Fatalf("relief at (%d, %d) is %v, outside [0, 1]", c.Q(), c.R(), r)
		}
	}
}

// TestLandWaterAgreesWithTheBands pins the two statements of the same rule to
// each other: IsLand compares the scalar against zero and the classification
// cuts water at the same place, so the two can never disagree about a tile.
func TestLandWaterAgreesWithTheBands(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(20000, 5) {
		if g.IsLand(c) == g.ElevationBandAt(c).IsWater() {
			t.Fatalf("(%d, %d): IsLand and the band disagree at elevation %v",
				c.Q(), c.R(), g.ElevationAt(c))
		}
	}
}

// TestSampleIsTheSameEvaluation asserts that the diagnostic decomposition is
// bit-identical to the values it decomposes. A convenience that computed
// something slightly different from the thing it is a convenience for would be
// worse than not having it, and the difference would be invisible.
func TestSampleIsTheSameEvaluation(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(2000, 7) {
		s := g.Sample(c)
		if got, want := math.Float64bits(s.Elevation), math.Float64bits(g.ElevationAt(c)); got != want {
			t.Fatalf("(%d, %d): Sample.Elevation is %#016x and ElevationAt is %#016x", c.Q(), c.R(), got, want)
		}
		if s.Band != g.ElevationBandAt(c) {
			t.Fatalf("(%d, %d): Sample.Band is %v and ElevationBandAt is %v", c.Q(), c.R(), s.Band, g.ElevationBandAt(c))
		}
		for j, scale := range Scales() {
			got := math.Float64bits([]float64{s.Continentalness, s.Regional, s.Local, s.Detail}[j])
			if want := math.Float64bits(g.ScaleAt(scale, c)); got != want {
				t.Fatalf("(%d, %d): Sample.%v is %#016x and ScaleAt is %#016x", c.Q(), c.R(), scale, got, want)
			}
		}
		if s.Region != g.RegionInfluence(c) {
			t.Fatalf("(%d, %d): Sample.Region is not the blended region influence", c.Q(), c.R())
		}
	}
}

// TestReliefMatchesAnIndependentComputation derives the expected value from the
// definition — the mean of the six absolute differences — rather than recording
// what the code produces.
//
// It also proves the thing DESIGN.md 18 warns about: Relief reads the six
// neighboring elevations and returns, so neither it nor the scalar it calls can
// be reaching back into the other.
func TestReliefMatchesAnIndependentComputation(t *testing.T) {
	g := NewDefault(probeSeed)
	scale := g.Config().Elevation.ReliefScale

	for _, c := range probeCoords(500, 13) {
		here := g.ElevationAt(c)
		var total float64
		for d := range 6 {
			total += math.Abs(g.ElevationAt(c.Neighbor(d)) - here)
		}
		want := min(scale*(total/6), 1)
		if got := g.Relief(c); got != want {
			t.Fatalf("relief at (%d, %d) = %v, want %v", c.Q(), c.R(), got, want)
		}
	}
}

// TestReliefFollowsTheGroundItMeasures covers the claim relief makes: it is a
// statement about how fast elevation changes between neighbors, so ground whose
// continuous scales cannot change between neighbors must read as flat.
//
// It is a comparison rather than an absolute bound because the region influence
// still varies across the flattened world — an anchor blend changes over a
// region, not over a tile, but it does change — and relief that is honest about
// that is relief that is working. What would be a defect is relief that did not
// notice the difference.
//
// DESIGN.md 18's other case, relief inside the rim, is the same statement about
// the same arithmetic and arrives with the rim in phase 7.
func TestReliefFollowsTheGroundItMeasures(t *testing.T) {
	coords := probeCoords(400, 17)

	median := func(g *Generator) float64 {
		var rs []float64
		for _, c := range coords {
			rs = append(rs, g.Relief(c))
		}
		sort.Float64s(rs)
		return rs[len(rs)/2]
	}

	cfg := DefaultConfig()
	// One octave at an enormous wavelength is as close to constant as a
	// configuration can get: over six miles the composite's own scales cannot
	// move enough to register.
	for _, l := range []*LadderConfig{&cfg.Continental, &cfg.Regional, &cfg.Local, &cfg.Detail, &cfg.Elevation.Ridge} {
		l.WavelengthMiles, l.Octaves = 4e6, 1
	}
	cfg.WarpStrengthMiles = 0
	cfg.Elevation.RidgeStrideMiles = 0

	flat, err := New(probeSeed, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rough := median(NewDefault(probeSeed))
	smooth := median(flat)
	if smooth*10 > rough {
		t.Fatalf("flattening every continuous scale moved the median relief from %v only to %v", rough, smooth)
	}
}

// TestElevationIsDeterministic covers DESIGN.md 30.1 and 30.2 for the composite:
// the same coordinate gives the same bits however many times and in whatever
// order it is asked for.
func TestElevationIsDeterministic(t *testing.T) {
	g := NewDefault(probeSeed)
	coords := probeCoords(500, 19)

	want := make([]uint64, len(coords))
	for i, c := range coords {
		want[i] = math.Float64bits(g.ElevationAt(c))
	}

	// Backwards, which is the cheapest order that is not the one above.
	for i := len(coords) - 1; i >= 0; i-- {
		if got := math.Float64bits(g.ElevationAt(coords[i])); got != want[i] {
			t.Fatalf("(%d, %d) gave %#016x and then %#016x", coords[i].Q(), coords[i].R(), want[i], got)
		}
	}

	// A second generator on the same seed, because nothing may be carried in
	// the one that has been used.
	h := NewDefault(probeSeed)
	for i, c := range coords {
		if got := math.Float64bits(h.ElevationAt(c)); got != want[i] {
			t.Fatalf("a fresh generator gave %#016x at (%d, %d), want %#016x", got, c.Q(), c.R(), want[i])
		}
	}
}

// TestElevationIsConcurrentlyDeterministic covers DESIGN.md 30.3. Run it under
// -race: the point is as much that nothing is written as that nothing moves.
func TestElevationIsConcurrentlyDeterministic(t *testing.T) {
	g := NewDefault(probeSeed)
	coords := probeCoords(400, 23)

	want := make([]uint64, len(coords))
	for i, c := range coords {
		want[i] = math.Float64bits(g.ElevationAt(c))
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, c := range coords {
				if got := math.Float64bits(g.ElevationAt(c)); got != want[i] {
					t.Errorf("concurrent read of (%d, %d) gave %#016x, want %#016x", c.Q(), c.R(), got, want[i])
					return
				}
				g.Relief(c)
			}
		}()
	}
	wg.Wait()
}

// TestElevationRespectsTheWrap asserts that a coordinate outside the canonical
// domain is the tile it normalizes to, all the way through the composite. The
// region blend and the ridge stencil both read positions rather than
// coordinates, so this is where a normalization that happened too late would
// show.
func TestElevationRespectsTheWrap(t *testing.T) {
	g := NewDefault(probeSeed)
	const n = WorldRadius

	for _, pair := range [][2]int64{
		{2*n + 1, -n}, // the first mirror center, which is the origin
		{n + 1, 0},    // one step past the rim on the q axis
		{-n - 1, 0},
		{3 * n, -2 * n},
	} {
		c := NewCoord(pair[0], pair[1])
		if got, want := math.Float64bits(g.ElevationAt(c)), math.Float64bits(g.ElevationAt(NewCoord(int64(c.Q()), int64(c.R())))); got != want {
			t.Errorf("(%d, %d) normalized to (%d, %d) and gave %#016x, want %#016x",
				pair[0], pair[1], c.Q(), c.R(), got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Distribution and coherence
// ---------------------------------------------------------------------------

// TestElevationDistribution is DESIGN.md 30.8 for elevation, and it is the test
// that stands in for the exhaustiveness Go's type system cannot give: every
// declared band must be produced somewhere in the sample, and none may swallow
// the world. A band that is unreachable because of a threshold typo is
// otherwise invisible.
//
// The bounds are deliberately wide. This is not a test of the numbers somebody
// tuned to; it is a test that a threshold is not sitting on a knife edge, and a
// bound tight enough to pin the current defaults would fail every time one
// moved.
func TestElevationDistribution(t *testing.T) {
	for _, seed := range []Seed{probeSeed, 1, 7, 99, goldenSeed} {
		g := NewDefault(seed)

		const n = 20000
		counts := map[Elevation]int{}
		land := 0
		for _, c := range probeCoords(n, uint64(seed)) {
			e := g.ElevationAt(c)
			counts[g.cfg.Elevation.Bands.Classify(e)]++
			if e > 0 {
				land++
			}
		}

		// A world that is nearly all ocean or nearly all continent is a
		// configuration somebody would notice; the point of the bound is that
		// the seed does not decide it.
		if fraction := float64(land) / n; fraction < 0.1 || fraction > 0.6 {
			t.Errorf("seed %#x: land fraction is %.3f, outside [0.1, 0.6]", uint64(seed), fraction)
		}

		for _, e := range Elevations() {
			fraction := float64(counts[e]) / n
			if counts[e] == 0 {
				t.Errorf("seed %#x: %v is produced nowhere in %d coordinates; a band nothing reaches is a threshold typo", uint64(seed), e, n)
			}
			if fraction > 0.8 {
				t.Errorf("seed %#x: %v is %.3f of the world", uint64(seed), e, fraction)
			}
		}
	}
}

// TestNeighborCoherence is DESIGN.md 30.7. It asserts a bound on the
// distribution of neighbor deltas rather than on any individual pair, because
// an individual pair is allowed to be a cliff and the thing that would be wrong
// is every pair being one.
func TestNeighborCoherence(t *testing.T) {
	g := NewDefault(probeSeed)

	var deltas []float64
	for _, c := range probeCoords(3000, 29) {
		here := g.ElevationAt(c)
		for d := range 6 {
			deltas = append(deltas, math.Abs(g.ElevationAt(c.Neighbor(d))-here))
		}
	}
	sort.Float64s(deltas)

	median := deltas[len(deltas)/2]
	p99 := deltas[len(deltas)*99/100]

	// Adjacent tiles are six miles apart and the finest scale in the composite
	// has a wavelength of tens of miles, so a typical step is a small fraction
	// of the range. A median anywhere near the range would mean the ladder had
	// descended into the grid, which is what DESIGN.md 9.3 exists to prevent.
	if median > 0.05 {
		t.Errorf("the median neighbor step is %v; adjacent tiles are not related", median)
	}
	if p99 > 0.5 {
		t.Errorf("the 99th percentile neighbor step is %v; the field is a cliff nearly everywhere", p99)
	}
	// And it must not be flat either: a world with no step between neighbors is
	// a world with no terrain.
	if median <= 0 {
		t.Error("the median neighbor step is zero; the composite does not vary between adjacent tiles")
	}
}

// TestRegionLatticeIsNotVisibleInElevation is DESIGN.md 11.2 measured rather
// than looked at. The elevation composite reads the blended region influence,
// and the ridge stencil reads the blended orientation as a displacement, which
// is the one place a blend's derivative discontinuity can be amplified into a
// visible line. See Generator.ridgeStructure.
//
// The measurement walks a straight line across many region boundaries and
// compares the step across a boundary with the steps either side of it. A
// lattice that is visible in a picture is a step that is reliably larger at the
// boundary than next to it; that it is not is what this asserts.
func TestRegionLatticeIsNotVisibleInElevation(t *testing.T) {
	g := NewDefault(probeSeed)
	size := int64(g.Config().RegionSizeHexes)

	step := func(q, r int64) float64 {
		return math.Abs(g.ElevationAt(NewCoord(q+1, r)) - g.ElevationAt(NewCoord(q, r)))
	}

	var onBoundary, offBoundary float64
	var onCount, offCount int
	for i := range int64(60) {
		// A row far from the origin, crossing sixty region boundaries.
		r := 3001 + 97*i
		for j := range int64(40) {
			q := j*size - 20000
			onBoundary += step(q-1, r)
			onCount++
			for _, d := range []int64{size / 4, size / 2, 3 * size / 4} {
				offBoundary += step(q+d, r)
				offCount++
			}
		}
	}

	on := onBoundary / float64(onCount)
	off := offBoundary / float64(offCount)
	if on > 2*off {
		t.Errorf("the mean step across a region boundary is %v against %v away from one; the anchor lattice is in the output", on, off)
	}
}
