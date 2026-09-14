// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"sort"
	"sync"
	"testing"

	"github.com/mdhender/wgva/internal/mathx"
)

// ---------------------------------------------------------------------------
// The enumerations
// ---------------------------------------------------------------------------

// TestHeatBandNames covers what stands in for exhaustive matching on the
// declared side. The other half — that every band is actually reachable — is
// TestClimateDistribution.
func TestHeatBandNames(t *testing.T) {
	seenName := map[string]bool{}
	seenValue := map[HeatBand]bool{}
	for _, h := range Heats() {
		if !h.Valid() {
			t.Errorf("%d is in Heats and reports itself invalid", uint8(h))
		}
		name := h.String()
		if name == "" || name == "unknown" {
			t.Errorf("%d has no name", uint8(h))
		}
		if seenName[name] {
			t.Errorf("%q names two bands", name)
		}
		seenName[name] = true
		if seenValue[h] {
			t.Errorf("%d is declared twice", uint8(h))
		}
		seenValue[h] = true
	}

	if got := len(Heats()); got != 5 {
		t.Errorf("Heats returns %d bands, want the 5 of DESIGN.md 16.1", got)
	}

	// The values are persisted, so the numbers themselves are the contract and
	// are asserted rather than assumed from declaration order.
	for value, want := range map[HeatBand]string{
		0: "polar", 1: "cold", 2: "temperate", 3: "warm", 4: "hot",
	} {
		if got := value.String(); got != want {
			t.Errorf("heat band %d is %q, want %q; these values are persisted", uint8(value), got, want)
		}
	}

	if undeclared := HeatBand(200); undeclared.Valid() || undeclared.String() != "unknown" {
		t.Error("an undeclared band reports itself valid or names itself")
	}
}

// TestMoistureBandNames is TestHeatBandNames for the other axis.
func TestMoistureBandNames(t *testing.T) {
	seenName := map[string]bool{}
	seenValue := map[MoistureBand]bool{}
	for _, m := range Moistures() {
		if !m.Valid() {
			t.Errorf("%d is in Moistures and reports itself invalid", uint8(m))
		}
		name := m.String()
		if name == "" || name == "unknown" {
			t.Errorf("%d has no name", uint8(m))
		}
		if seenName[name] {
			t.Errorf("%q names two bands", name)
		}
		seenName[name] = true
		if seenValue[m] {
			t.Errorf("%d is declared twice", uint8(m))
		}
		seenValue[m] = true
	}

	if got := len(Moistures()); got != 5 {
		t.Errorf("Moistures returns %d bands, want the 5 of DESIGN.md 16.1", got)
	}

	for value, want := range map[MoistureBand]string{
		0: "arid", 1: "dry", 2: "moderate", 3: "humid", 4: "saturated",
	} {
		if got := value.String(); got != want {
			t.Errorf("moisture band %d is %q, want %q; these values are persisted", uint8(value), got, want)
		}
	}

	if undeclared := MoistureBand(200); undeclared.Valid() || undeclared.String() != "unknown" {
		t.Error("an undeclared band reports itself valid or names itself")
	}
}

// TestClimateIsAPairOfBands asserts the shape DESIGN.md 16.1 asks for: two
// independent axes carried side by side, with no combined value anywhere. Every
// one of the twenty-five pairs is a climate, which is the statement that the
// properties do not exclude one another — a polar desert and a polar rainforest
// are both ordinary places.
func TestClimateIsAPairOfBands(t *testing.T) {
	seen := map[string]bool{}
	for _, h := range Heats() {
		for _, m := range Moistures() {
			c := Climate{Heat: h, Moisture: m}
			if !c.Valid() {
				t.Errorf("%v reports itself invalid", c)
			}
			if seen[c.String()] {
				t.Errorf("%q names two climates", c)
			}
			seen[c.String()] = true
		}
	}
	if got := len(seen); got != 25 {
		t.Errorf("the two axes make %d pairs, want 5 by 5", got)
	}

	if bad := (Climate{Heat: HeatBand(9), Moisture: MoistureModerate}); bad.Valid() {
		t.Error("a climate with an undeclared heat band reports itself valid")
	}
	if bad := (Climate{Heat: HeatTemperate, Moisture: MoistureBand(9)}); bad.Valid() {
		t.Error("a climate with an undeclared moisture band reports itself valid")
	}
}

// TestClimateClassifyCutsAtTheThresholds covers the comparisons themselves, at
// each threshold and on either side of it.
func TestClimateClassifyCutsAtTheThresholds(t *testing.T) {
	cc := DefaultConfig().Climate

	heat := []struct {
		name string
		v    float64
		want HeatBand
	}{
		{"the floor", -1, HeatPolar},
		{"at the polar threshold", cc.HeatBands.Polar, HeatPolar},
		{"just above it", math.Nextafter(cc.HeatBands.Polar, 1), HeatCold},
		{"at the cold threshold", cc.HeatBands.Cold, HeatCold},
		{"just above it", math.Nextafter(cc.HeatBands.Cold, 1), HeatTemperate},
		{"the middle of the scale", 0, HeatTemperate},
		{"negative zero", math.Copysign(0, -1), HeatTemperate},
		{"at the temperate threshold", cc.HeatBands.Temperate, HeatTemperate},
		{"just above it", math.Nextafter(cc.HeatBands.Temperate, 1), HeatWarm},
		{"at the warm threshold", cc.HeatBands.Warm, HeatWarm},
		{"just above it", math.Nextafter(cc.HeatBands.Warm, 1), HeatHot},
		{"the ceiling", 1, HeatHot},
	}
	for _, tc := range heat {
		t.Run("heat/"+tc.name, func(t *testing.T) {
			if got := cc.HeatBands.Classify(tc.v); got != tc.want {
				t.Errorf("Classify(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}

	moisture := []struct {
		name string
		v    float64
		want MoistureBand
	}{
		{"the floor", -1, MoistureArid},
		{"at the arid threshold", cc.MoistureBands.Arid, MoistureArid},
		{"just above it", math.Nextafter(cc.MoistureBands.Arid, 1), MoistureDry},
		{"at the dry threshold", cc.MoistureBands.Dry, MoistureDry},
		{"just above it", math.Nextafter(cc.MoistureBands.Dry, 1), MoistureModerate},
		{"the middle of the scale", 0, MoistureModerate},
		{"at the moderate threshold", cc.MoistureBands.Moderate, MoistureModerate},
		{"just above it", math.Nextafter(cc.MoistureBands.Moderate, 1), MoistureHumid},
		{"at the humid threshold", cc.MoistureBands.Humid, MoistureHumid},
		{"just above it", math.Nextafter(cc.MoistureBands.Humid, 1), MoistureSaturated},
		{"the ceiling", 1, MoistureSaturated},
	}
	for _, tc := range moisture {
		t.Run("moisture/"+tc.name, func(t *testing.T) {
			if got := cc.MoistureBands.Classify(tc.v); got != tc.want {
				t.Errorf("Classify(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The composite
// ---------------------------------------------------------------------------

// TestClimateIsInRange covers the one guarantee every consumer depends on.
func TestClimateIsInRange(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(20000, 31) {
		h, m := g.HeatAt(c), g.MoistureAt(c)
		if math.IsNaN(h) || h < -1 || h > 1 {
			t.Fatalf("heat at (%d, %d) is %v, outside [-1, +1]", c.Q(), c.R(), h)
		}
		if math.IsNaN(m) || m < -1 || m > 1 {
			t.Fatalf("moisture at (%d, %d) is %v, outside [-1, +1]", c.Q(), c.R(), m)
		}
		if cl := g.ClimateAt(c); !cl.Valid() {
			t.Fatalf("climate at (%d, %d) is %v, which is not a pair of declared bands", c.Q(), c.R(), cl)
		}
	}
}

// TestClimateMatchesAnIndependentComputation derives the expected values from
// the formula written down in DESIGN.md 16 and in Generator.climateAt, out of
// the public pieces, rather than recording what the code produces.
//
// It is what would catch a term dropped, a weight applied to the wrong field,
// or the cooling reading the elevation scalar instead of its positive part.
func TestClimateMatchesAnIndependentComputation(t *testing.T) {
	g := NewDefault(probeSeed)
	cc := g.Config().Climate
	heatField, moistureField, variationField := g.HeatField(), g.MoistureField(), g.MoistureVariationField()

	for _, c := range probeCoords(500, 37) {
		p := AxialToWorld(c)
		region := g.RegionInfluence(c)
		elevation := g.ElevationAt(c)

		heatBase := (mathx.Mul(cc.HeatFieldWeight, heatField.Sample(p.X, p.Y)) +
			mathx.Mul(cc.HeatBiasWeight, region.HeatBias)) /
			(cc.HeatFieldWeight + cc.HeatBiasWeight)
		cooling := mathx.Mul(cc.ElevationCooling, max(elevation, 0))
		wantHeat := clampUnitSigned(contrast(heatBase, cc.HeatContrastPasses) - cooling)

		moistureBase := (mathx.Mul(cc.MoistureFieldWeight, moistureField.Sample(p.X, p.Y)) +
			mathx.Mul(cc.MoistureBiasWeight, region.MoistureBias) +
			mathx.Mul(cc.MoistureVariationWeight, variationField.Sample(p.X, p.Y))) /
			(cc.MoistureFieldWeight + cc.MoistureBiasWeight + cc.MoistureVariationWeight)
		wantMoisture := clampUnitSigned(contrast(moistureBase, cc.MoistureContrastPasses))

		if got := g.HeatAt(c); got != wantHeat {
			t.Fatalf("heat at (%d, %d) = %v, want %v", c.Q(), c.R(), got, wantHeat)
		}
		if got := g.MoistureAt(c); got != wantMoisture {
			t.Fatalf("moisture at (%d, %d) = %v, want %v", c.Q(), c.R(), got, wantMoisture)
		}
	}
}

// TestSampleIsTheSameClimateEvaluation asserts that the diagnostic
// decomposition is bit-identical to the values it decomposes, for the reason
// TestSampleIsTheSameEvaluation does it for elevation.
func TestSampleIsTheSameClimateEvaluation(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(2000, 41) {
		s := g.Sample(c)
		if got, want := math.Float64bits(s.Heat), math.Float64bits(g.HeatAt(c)); got != want {
			t.Fatalf("(%d, %d): Sample.Heat is %#016x and HeatAt is %#016x", c.Q(), c.R(), got, want)
		}
		if got, want := math.Float64bits(s.Moisture), math.Float64bits(g.MoistureAt(c)); got != want {
			t.Fatalf("(%d, %d): Sample.Moisture is %#016x and MoistureAt is %#016x", c.Q(), c.R(), got, want)
		}
		if got, want := s.Climate, g.ClimateAt(c); got != want {
			t.Fatalf("(%d, %d): Sample.Climate is %v and ClimateAt is %v", c.Q(), c.R(), got, want)
		}
		if want := (Climate{
			Heat:     g.Config().Climate.HeatBands.Classify(s.Heat),
			Moisture: g.Config().Climate.MoistureBands.Classify(s.Moisture),
		}); s.Climate != want {
			t.Fatalf("(%d, %d): the classification does not match the scalars it was cut from", c.Q(), c.R())
		}
	}
}

// TestElevationCoolingCoolsLandAndLeavesTheSeaAlone pins the decision in
// Generator.climateAt: the cooling term reads max(elevation, 0), so below sea
// level the scalar is depth rather than altitude and has no lapse rate.
//
// A term that read the elevation scalar directly would warm the deep ocean in
// proportion to how deep it was, which is an ocean that is hottest where it is
// darkest. That defect is invisible in a heat map — the ocean is a broad smooth
// field either way — and obvious here.
func TestElevationCoolingCoolsLandAndLeavesTheSeaAlone(t *testing.T) {
	cold := DefaultConfig()
	cold.Climate.ElevationCooling = 0.5
	warm := DefaultConfig()
	warm.Climate.ElevationCooling = 0

	cooled, err := New(probeSeed, cold)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	uncooled, err := New(probeSeed, warm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var sea, land int
	for _, c := range probeCoords(4000, 43) {
		e := cooled.ElevationAt(c)
		with, without := cooled.HeatAt(c), uncooled.HeatAt(c)

		if e <= 0 {
			sea++
			// Water, where the two configurations must agree bit for bit.
			if math.Float64bits(with) != math.Float64bits(without) {
				t.Fatalf("at (%d, %d), elevation %v: cooling moved the heat of a water tile from %v to %v",
					c.Q(), c.R(), e, without, with)
			}
			continue
		}

		land++
		// Land, where the cooling is exactly the lapse rate times the
		// elevation unless the clamp has taken over at the floor.
		if with > without {
			t.Fatalf("at (%d, %d), elevation %v: cooling warmed the tile from %v to %v",
				c.Q(), c.R(), e, without, with)
		}
		want := clampUnitSigned(without - mathx.Mul(0.5, e))
		if with != want {
			t.Fatalf("at (%d, %d), elevation %v: heat is %v, want %v", c.Q(), c.R(), e, with, want)
		}
	}

	if sea == 0 || land == 0 {
		t.Fatalf("the sample covered %d water tiles and %d land tiles; it has to cover both", sea, land)
	}
}

// TestRegionalClimateBiasIsRead asserts that the two region biases reach the two
// axes, and that neither reaches the other one.
//
// It measures by moving the weight rather than by moving a bias, because a bias
// is derived from the seed and cannot be set. Raising the heat bias weight must
// move heat and leave moisture bit-identical, and the other way round; a
// composite that read the wrong bias, or the same bias twice, fails both ways.
func TestRegionalClimateBiasIsRead(t *testing.T) {
	base := NewDefault(probeSeed)

	heavierHeat := DefaultConfig()
	heavierHeat.Climate.HeatBiasWeight = 0.9
	heat, err := New(probeSeed, heavierHeat)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	heavierMoisture := DefaultConfig()
	heavierMoisture.Climate.MoistureBiasWeight = 0.9
	moisture, err := New(probeSeed, heavierMoisture)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var heatMoved, moistureMoved int
	for _, c := range probeCoords(2000, 47) {
		if heat.HeatAt(c) != base.HeatAt(c) {
			heatMoved++
		}
		if got, want := math.Float64bits(heat.MoistureAt(c)), math.Float64bits(base.MoistureAt(c)); got != want {
			t.Fatalf("(%d, %d): the heat bias weight moved moisture from %#016x to %#016x", c.Q(), c.R(), want, got)
		}
		if moisture.MoistureAt(c) != base.MoistureAt(c) {
			moistureMoved++
		}
		if got, want := math.Float64bits(moisture.HeatAt(c)), math.Float64bits(base.HeatAt(c)); got != want {
			t.Fatalf("(%d, %d): the moisture bias weight moved heat from %#016x to %#016x", c.Q(), c.R(), want, got)
		}
	}

	// Not every tile: the clamp saturates some and the bias is near zero at
	// some others. Most of them is the claim.
	if heatMoved < 1500 {
		t.Errorf("raising the heat bias weight moved heat at only %d of 2000 coordinates", heatMoved)
	}
	if moistureMoved < 1500 {
		t.Errorf("raising the moisture bias weight moved moisture at only %d of 2000 coordinates", moistureMoved)
	}
}

// TestClimateIsDeterministic covers DESIGN.md 30.1 and 30.2 for the two axes.
func TestClimateIsDeterministic(t *testing.T) {
	g := NewDefault(probeSeed)
	coords := probeCoords(500, 53)

	type pair struct{ heat, moisture uint64 }
	want := make([]pair, len(coords))
	for i, c := range coords {
		want[i] = pair{math.Float64bits(g.HeatAt(c)), math.Float64bits(g.MoistureAt(c))}
	}

	// Backwards, which is the cheapest order that is not the one above.
	for i := len(coords) - 1; i >= 0; i-- {
		got := pair{math.Float64bits(g.HeatAt(coords[i])), math.Float64bits(g.MoistureAt(coords[i]))}
		if got != want[i] {
			t.Fatalf("(%d, %d) gave %+v and then %+v", coords[i].Q(), coords[i].R(), want[i], got)
		}
	}

	// A second generator on the same seed, because nothing may be carried in
	// the one that has been used.
	h := NewDefault(probeSeed)
	for i, c := range coords {
		got := pair{math.Float64bits(h.HeatAt(c)), math.Float64bits(h.MoistureAt(c))}
		if got != want[i] {
			t.Fatalf("a fresh generator gave %+v at (%d, %d), want %+v", got, c.Q(), c.R(), want[i])
		}
	}
}

// TestClimateIsConcurrentlyDeterministic covers DESIGN.md 30.3. Run it under
// -race: the point is as much that nothing is written as that nothing moves.
func TestClimateIsConcurrentlyDeterministic(t *testing.T) {
	g := NewDefault(probeSeed)
	coords := probeCoords(400, 59)

	want := make([]uint64, len(coords))
	for i, c := range coords {
		want[i] = math.Float64bits(g.HeatAt(c)) ^ math.Float64bits(g.MoistureAt(c))
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i, c := range coords {
				got := math.Float64bits(g.HeatAt(c)) ^ math.Float64bits(g.MoistureAt(c))
				if got != want[i] {
					t.Errorf("concurrent read of (%d, %d) gave %#016x, want %#016x", c.Q(), c.R(), got, want[i])
					return
				}
				g.ClimateAt(c)
			}
		})
	}
	wg.Wait()
}

// TestClimateRespectsTheWrap asserts that a coordinate outside the canonical
// domain is the tile it normalizes to, all the way through both composites.
func TestClimateRespectsTheWrap(t *testing.T) {
	g := NewDefault(probeSeed)
	const n = WorldRadius

	for _, pair := range [][2]int64{
		{2*n + 1, -n}, // the first mirror center, which is the origin
		{n + 1, 0},    // one step past the rim on the q axis
		{-n - 1, 0},
		{3 * n, -2 * n},
	} {
		c := NewCoord(pair[0], pair[1])
		normalized := NewCoord(int64(c.Q()), int64(c.R()))
		if got, want := math.Float64bits(g.HeatAt(c)), math.Float64bits(g.HeatAt(normalized)); got != want {
			t.Errorf("heat at (%d, %d) normalized to (%d, %d) gave %#016x, want %#016x",
				pair[0], pair[1], c.Q(), c.R(), got, want)
		}
		if got, want := math.Float64bits(g.MoistureAt(c)), math.Float64bits(g.MoistureAt(normalized)); got != want {
			t.Errorf("moisture at (%d, %d) normalized to (%d, %d) gave %#016x, want %#016x",
				pair[0], pair[1], c.Q(), c.R(), got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Distribution and coherence
// ---------------------------------------------------------------------------

// TestClimateDistribution is DESIGN.md 30.8 for the two climate axes, and it is
// the test that stands in for the exhaustiveness Go's type system cannot give:
// every declared band on both axes must be produced somewhere in the sample,
// and none may swallow the world.
//
// The bounds are deliberately wide, for the reason TestElevationDistribution's
// are: this is a test that a threshold is not sitting on a knife edge, not a
// test of the numbers somebody tuned to.
func TestClimateDistribution(t *testing.T) {
	for _, seed := range []Seed{probeSeed, 1, 7, 99, goldenSeed} {
		g := NewDefault(seed)

		const n = 20000
		heat := map[HeatBand]int{}
		moisture := map[MoistureBand]int{}
		for _, c := range probeCoords(n, uint64(seed)) {
			cl := g.ClimateAt(c)
			heat[cl.Heat]++
			moisture[cl.Moisture]++
		}

		for _, h := range Heats() {
			if heat[h] == 0 {
				t.Errorf("seed %#x: %v is produced nowhere in %d coordinates; a band nothing reaches is a threshold typo", uint64(seed), h, n)
			}
			if fraction := float64(heat[h]) / n; fraction > 0.8 {
				t.Errorf("seed %#x: %v is %.3f of the world", uint64(seed), h, fraction)
			}
		}
		for _, m := range Moistures() {
			if moisture[m] == 0 {
				t.Errorf("seed %#x: %v is produced nowhere in %d coordinates; a band nothing reaches is a threshold typo", uint64(seed), m, n)
			}
			if fraction := float64(moisture[m]) / n; fraction > 0.8 {
				t.Errorf("seed %#x: %v is %.3f of the world", uint64(seed), m, fraction)
			}
		}
	}
}

// TestClimateFormsBroadZones is the exit condition of DESIGN.md 32's phase 5,
// measured rather than looked at: climate maps form coherent broad zones rather
// than tile-level speckle.
//
// Speckle is a statement about the step between neighbors, so that is what is
// measured. The bound is the same shape as TestNeighborCoherence's and tighter,
// because climate has no equivalent of the detail scale: the finest thing in
// either axis is the moisture variation ladder, whose shortest octave is tens of
// miles, and adjacent tiles are six miles apart. A median step anywhere near a
// band's width would mean a tile's band was decided by its own noise rather than
// by the zone it stands in, which is exactly the tile-level speckle the phase
// exists to avoid.
func TestClimateFormsBroadZones(t *testing.T) {
	g := NewDefault(probeSeed)

	// The narrowest band on either axis, which is what a step has to be small
	// against for a zone to be a zone.
	cc := g.Config().Climate
	narrowest := min(
		cc.HeatBands.Cold-cc.HeatBands.Polar,
		cc.HeatBands.Temperate-cc.HeatBands.Cold,
		cc.MoistureBands.Dry-cc.MoistureBands.Arid,
		cc.MoistureBands.Moderate-cc.MoistureBands.Dry,
	)

	for _, axis := range []struct {
		name   string
		sample func(Coord) float64
	}{
		{"heat", g.HeatAt},
		{"moisture", g.MoistureAt},
	} {
		t.Run(axis.name, func(t *testing.T) {
			var deltas []float64
			for _, c := range probeCoords(3000, 61) {
				here := axis.sample(c)
				for d := range 6 {
					deltas = append(deltas, math.Abs(axis.sample(c.Neighbor(d))-here))
				}
			}
			sort.Float64s(deltas)
			median := deltas[len(deltas)/2]
			p99 := deltas[len(deltas)*99/100]

			// A tenth of the narrowest band means crossing that band takes ten
			// tiles at the typical step, which is a zone rather than a speckle.
			if limit := narrowest / 10; median > limit {
				t.Errorf("the median neighbor step is %v against a narrowest band of %v; the map is speckle rather than zones", median, narrowest)
			}
			if p99 > narrowest {
				t.Errorf("the 99th percentile neighbor step is %v, which crosses a whole band between neighbors", p99)
			}
			// And it must vary: a climate with no step between neighbors is a
			// world of one color, which passes every bound above.
			if median <= 0 {
				t.Errorf("the median neighbor step is zero; %s does not vary between adjacent tiles", axis.name)
			}
		})
	}
}

// TestRegionLatticeIsNotVisibleInClimate is DESIGN.md 11.2 measured on the two
// axes rather than on elevation, and it matters more here than it does there.
//
// Elevation reads a region's bias as one term among five and scaled by a
// quarter; each climate axis reads one as a quarter of a weighted average of
// two or three, which is the most direct exposure any consumer has to the
// anchor blend. If the blend had a crease at a triangle edge, climate is where
// it would surface first, and a climate map is exactly the kind of picture in
// which a faint triangular mesh reads as geography rather than as a defect.
//
// The measurement is TestRegionLatticeIsNotVisibleInElevation's: walk a
// straight line across many region boundaries and compare the step across a
// boundary with the steps either side of it.
func TestRegionLatticeIsNotVisibleInClimate(t *testing.T) {
	g := NewDefault(probeSeed)
	size := int64(g.Config().RegionSizeHexes)

	for _, axis := range []struct {
		name   string
		sample func(Coord) float64
	}{
		{"heat", g.HeatAt},
		{"moisture", g.MoistureAt},
	} {
		t.Run(axis.name, func(t *testing.T) {
			step := func(q, r int64) float64 {
				return math.Abs(axis.sample(NewCoord(q+1, r)) - axis.sample(NewCoord(q, r)))
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
				t.Errorf("the mean %s step across a region boundary is %v against %v away from one; the anchor lattice is in the output",
					axis.name, on, off)
			}
		})
	}
}

// TestHeatAndMoistureAreIndependent is DESIGN.md 16.1 measured. The two axes
// come from separate fields under separate hashing domains and read separate
// region biases, so the correlation between them over the whole map is nothing.
//
// What it catches is one axis being built out of the other — a composite that
// sampled the heat field for moisture, or read one region bias twice. It is not
// what catches a shared hashing *domain*: two nodes under one domain share a
// gradient table and a sampling offset, but at different wavelengths they still
// produce unrelated values, so the correlation stays near zero and the defect
// goes by. TestDomainConstants pins that the identifiers differ and
// TestClimateFieldsHaveTheirOwnDomains pins that the three trees carry them.
func TestHeatAndMoistureAreIndependent(t *testing.T) {
	for _, seed := range []Seed{probeSeed, 1, 7, goldenSeed} {
		g := NewDefault(seed)

		var n, sh, sm, shh, smm, shm float64
		for _, c := range probeCoords(20000, uint64(seed)) {
			h, m := g.HeatAt(c), g.MoistureAt(c)
			n++
			sh, sm = sh+h, sm+m
			shh, smm = shh+h*h, smm+m*m
			shm += h * m
		}
		cov := shm/n - (sh/n)*(sm/n)
		corr := cov / math.Sqrt((shh/n-(sh/n)*(sh/n))*(smm/n-(sm/n)*(sm/n)))

		// Two broad fields over a finite world will not be exactly
		// uncorrelated, and the bound is loose because what it is looking for is
		// not a small number but a large one: two axes sharing a field
		// correlate near one.
		if math.Abs(corr) > 0.25 {
			t.Errorf("seed %#x: the correlation between heat and moisture is %+.4f", uint64(seed), corr)
		}
	}
}

// TestClimateFieldsHaveTheirOwnDomains is the rule of DESIGN.md 8.1 at the one
// place this phase could break it: three fields derived by three functions that
// differ in one identifier each.
//
// A copy-paste that gave the variation field the broad moisture field's domain
// would put both on one gradient table with one sampling offset, and the
// variation term would become a scaled copy of the field it exists to vary.
// Nothing measured elsewhere would see it — the two wavelengths differ by a
// factor of thirty, so the values still look unrelated — and the map would
// simply be a little less varied than the configuration says.
func TestClimateFieldsHaveTheirOwnDomains(t *testing.T) {
	cfg := DefaultConfig()

	// The leaf under the warp and the offset is what carries the domain.
	leafDomain := func(f Field) uint64 {
		n := &f
		for n.Source != nil {
			n = n.Source
		}
		return n.Domain
	}

	want := map[string]uint64{
		"heat":               DomTemperature,
		"moisture":           DomMoisture,
		"moisture-variation": DomMoistureVariation,
	}
	got := map[string]uint64{
		"heat":               leafDomain(DeriveHeatField(7, cfg)),
		"moisture":           leafDomain(DeriveMoistureField(7, cfg)),
		"moisture-variation": leafDomain(DeriveMoistureVariationField(7, cfg)),
	}

	seen := map[uint64]string{}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("the %s field samples domain %#016x, want %#016x", name, got[name], w)
		}
		if other, dup := seen[got[name]]; dup {
			t.Errorf("the %s and %s fields share domain %#016x", name, other, got[name])
		}
		seen[got[name]] = name
	}

	// And the three are the shape every other continuous field has: the shared
	// domain warp over the seed-derived offset over an fbm ladder over a leaf.
	for name, f := range map[string]Field{
		"heat":               DeriveHeatField(7, cfg),
		"moisture":           DeriveMoistureField(7, cfg),
		"moisture-variation": DeriveMoistureVariationField(7, cfg),
	} {
		if f.Kind != FieldWarp || f.Source.Kind != FieldOffset ||
			f.Source.Source.Kind != FieldFBM || f.Source.Source.Source.Kind != FieldSimplex {
			t.Errorf("the %s field is not warp/offset/fbm/leaf:\n%s", name, f.Describe())
		}
		if err := f.Validate(); err != nil {
			t.Errorf("the %s field does not validate: %v", name, err)
		}
	}
}

// TestTheWorldHasNoEquator is the rule this phase is most likely to break, and
// it is a rule about what the code must *not* do, so it is measured on the
// output: r == 0 is an addressing origin and not a latitude, and neither is the
// far edge of the map a pole. See DESIGN.md 16 and 30.13.
//
// A latitude model would put the row r == 0 near the hot end of the scale and
// the two extreme rows near the polar end, so the test compares the mean heat of
// those rows against the mean over the whole map. The bound is loose on purpose:
// a broad field will make some rows warmer than others at any seed, and what is
// being refused is a systematic offset of the size a real latitude term would
// produce, not the ordinary variation of a field.
//
// It is run at several seeds because a single seed's rows are one sample of a
// field, and the claim is about the model rather than about a world.
func TestTheWorldHasNoEquator(t *testing.T) {
	const step = 211

	rowMean := func(g *Generator, r int64) float64 {
		var total, n float64
		for q := -WorldRadius; q <= WorldRadius; q += step {
			total += g.HeatAt(NewCoord(q, r))
			n++
		}
		return total / n
	}

	for _, seed := range []Seed{probeSeed, 1, 7, goldenSeed} {
		g := NewDefault(seed)

		// The map's mean, over rows spread from edge to edge.
		var total, n float64
		for r := -WorldRadius; r <= WorldRadius; r += 997 {
			total += rowMean(g, r)
			n++
		}
		world := total / n

		for _, row := range []struct {
			name string
			r    int64
		}{
			{"the r == 0 row, which an equator model would make hottest", 0},
			{"the r == +WorldRadius row, which a pole model would make coldest", WorldRadius},
			{"the r == -WorldRadius row", -WorldRadius},
		} {
			if offset := rowMean(g, row.r) - world; math.Abs(offset) > 0.35 {
				t.Errorf("seed %#x: %s is %+.3f from the world mean of %+.3f",
					uint64(seed), row.name, offset, world)
			}
		}
	}
}
