// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"math/rand/v2"
	"testing"
)

// The rim suite of DESIGN.md 30.14. The rim is cheap to test and easy to get
// subtly wrong, and every assertion the section lists is here: that the distance
// agrees with an independent computation on every side of the map, that the
// falloff is monotonic and joins without a step, that a forced tile is forced
// and nothing else is, that the climate inside the band is still the generated
// climate, and that a zero rim reproduces the unrimmed world bit for bit.

// rimGenerator returns a generator whose rim is the given one, over the
// defaults. It is the one thing every test here needs and the reason the rim's
// two widths are configuration rather than constants.
func rimGenerator(t *testing.T, rc RimConfig) *Generator {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Rim = rc
	g, err := New(probeSeed, cfg)
	if err != nil {
		t.Fatalf("New with rim %+v: %v", rc, err)
	}
	return g
}

// rimRingCoords returns coordinates at hex distance radius from the origin,
// every step hexes along all six edges and including all six corners.
//
// It is a second ring walk beside field_test.go's, and the difference is the
// point twice over. Each edge here is the integer lerp between two cube corners,
// so the points come from the geometry of the hexagon rather than from the
// direction table — a rim test that walked the ring with Neighbor would be
// testing RimDistance against the same table RimDistance's own derivation rests
// on. And it strides, because the outermost ring of the shipped world is 196,602
// tiles and these tests classify terrain, which is seven evaluations apiece.
func rimRingCoords(radius, step int64) []Coord {
	corners := [6][3]int64{
		{radius, -radius, 0},
		{radius, 0, -radius},
		{0, radius, -radius},
		{-radius, radius, 0},
		{-radius, 0, radius},
		{0, -radius, radius},
	}
	var out []Coord
	for i := range corners {
		a, b := corners[i], corners[(i+1)%6]
		for t := int64(0); t < radius; t += step {
			q := a[0] + (b[0]-a[0])*t/radius
			r := a[1] + (b[1]-a[1])*t/radius
			out = append(out, NewCoord(q, r))
		}
	}
	return out
}

// TestRimDistanceIsTheBandMembership pins the three comparisons of
// DESIGN.md 15.1 against the hexagon's own geometry: a coordinate the
// construction put on the ring of radius N-d is d hexes from the rim, and the
// closed band is exactly the rings below ClosedHexes.
//
// The interesting rows are the last two pairs. Whether the band is `d < closed`
// or `d <= closed` is one character, both look right, and the difference is one
// ring of the map either forced or not.
func TestRimDistanceIsTheBandMembership(t *testing.T) {
	const closed = 24
	g := rimGenerator(t, RimConfig{ClosedHexes: closed, FalloffHexes: 96, FloorElevation: -1})

	for _, d := range []int64{0, 1, closed - 2, closed - 1, closed, closed + 1, 96, 120, 121, WorldRadius} {
		for _, c := range rimRingCoords(WorldRadius-d, 4093) {
			if got := c.RimDistance(); got != d {
				t.Fatalf("RimDistance at (%d, %d) = %d, want %d", c.Q(), c.R(), got, d)
			}
			if got, want := g.IsRim(c), d < closed; got != want {
				t.Fatalf("IsRim at (%d, %d), %d hexes from the rim, = %v, want %v",
					c.Q(), c.R(), d, got, want)
			}
		}
	}
}

// TestRimProfileIsMonotonicAndJoinsWithoutAStep is the shape of the falloff:
// zero across the closed band, rising without a jump, and exactly one from the
// inner edge inward.
//
// "Exactly one" is the load-bearing word. A weight that reached 0.9999999 would
// leave every tile in the world multiplied by not-quite-one, and the world
// outside the band has to be the world.
func TestRimProfileIsMonotonicAndJoinsWithoutAStep(t *testing.T) {
	for _, rc := range []RimConfig{
		{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: -1},
		{ClosedHexes: 0, FalloffHexes: 96, FloorElevation: -1},
		{ClosedHexes: 24, FalloffHexes: 0, FloorElevation: -1},
		{ClosedHexes: 0, FalloffHexes: 0, FloorElevation: -1},
		{ClosedHexes: 1, FalloffHexes: 1, FloorElevation: -1},
	} {
		closed := int64(rc.ClosedHexes)
		inner := closed + int64(rc.FalloffHexes)

		// The largest step a smoothstep can take over one ring is 1.5/width,
		// which is what "no step" means for a profile that has to climb from
		// zero to one in a finite number of hexes.
		maxStep := 1.0
		if rc.FalloffHexes > 0 {
			maxStep = 2 / float64(rc.FalloffHexes)
		}

		previous := 0.0
		for d := int64(0); d <= inner+4; d++ {
			w := rc.profile(d)
			switch {
			case w < 0 || w > 1:
				t.Fatalf("%+v: the profile at %d hexes is %v, outside [0, 1]", rc, d, w)
			case d < closed && w != 0:
				t.Fatalf("%+v: the profile at %d hexes is %v, want 0 across the closed band", rc, d, w)
			case d >= inner && w != 1:
				t.Fatalf("%+v: the profile at %d hexes is %v, want exactly 1 inside the falloff", rc, d, w)
			case w < previous:
				t.Fatalf("%+v: the profile fell from %v to %v at %d hexes", rc, previous, w, d)
			case w-previous > maxStep:
				t.Fatalf("%+v: the profile stepped from %v to %v at %d hexes, more than %v",
					rc, previous, w, d, maxStep)
			}
			previous = w
		}

		// The two joins, stated as the values rather than as differences. At the
		// outer join the profile is zero, which is what the closed band holds; at
		// the inner one it is one, which is what the world holds.
		if rc.FalloffHexes > 0 {
			if w := rc.profile(closed); w != 0 {
				t.Errorf("%+v: the profile at the outer join is %v, want 0", rc, w)
			}
			if w := rc.profile(inner - 1); w >= 1 {
				t.Errorf("%+v: the profile one ring outside the inner join is %v, want less than 1", rc, w)
			}
		}
	}
}

// TestRimForcesTheClosedBandAndNothingElse is the third of DESIGN.md 30.14's
// assertions: every tile with Rim true has the configured terrain and the
// configured elevation floor, and the flag is set by the distance rule and by
// nothing else.
//
// The second half is why the probe sample is here beside the ring walk. A flag
// that was set correctly at the rim and also set somewhere in the middle of the
// map — by a threshold on the elevation, say — would pass a test that only ever
// looked at the edge.
func TestRimForcesTheClosedBandAndNothingElse(t *testing.T) {
	for _, rc := range []RimConfig{
		{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: -1, Kind: RimDeepOcean},
		{ClosedHexes: 40, FalloffHexes: 200, FloorElevation: 0.6, Kind: RimPolarIce},
	} {
		g := rimGenerator(t, rc)
		want := TerrainDeepOcean
		if rc.Kind == RimPolarIce {
			want = TerrainGlacialIce
		}

		// Around the outermost ring and around the last closed ring, which is
		// where being off by one is invisible.
		for _, d := range []int64{0, int64(rc.ClosedHexes) - 1} {
			for _, c := range rimRingCoords(WorldRadius-d, 8191) {
				tile := g.Tile(c)
				if !tile.Rim {
					t.Fatalf("%+v: (%d, %d) is %d hexes from the rim and is not marked", rc, c.Q(), c.R(), d)
				}
				if tile.ElevationValue != rc.FloorElevation {
					t.Fatalf("%+v: the forced tile at (%d, %d) is at elevation %v, want the floor %v",
						rc, c.Q(), c.R(), tile.ElevationValue, rc.FloorElevation)
				}
				if tile.Terrain != want {
					t.Fatalf("%+v: the forced tile at (%d, %d) is %v, want %v",
						rc, c.Q(), c.R(), tile.Terrain, want)
				}
			}
		}

		// And nowhere else. Tile.Rim, IsRim, and the distance rule are three
		// statements of the same thing, and a coordinate where they disagree is
		// a coordinate where one of them has grown a second input.
		marked := 0
		for _, c := range probeCoords(3000, 0x7a11) {
			d := c.RimDistance()
			inBand := d < int64(rc.ClosedHexes)
			if got := g.Tile(c).Rim; got != inBand {
				t.Fatalf("%+v: Tile.Rim at (%d, %d), %d hexes from the rim, = %v, want %v",
					rc, c.Q(), c.R(), d, got, inBand)
			}
			if got := g.IsRim(c); got != inBand {
				t.Fatalf("%+v: IsRim at (%d, %d) disagrees with Tile.Rim", rc, c.Q(), c.R())
			}
			if inBand {
				marked++
			}
		}
		// The probe sample is uniform over the map and the band is a fraction of
		// a percent of it, so this is a statement that the sample did not happen
		// to land in the band rather than a bound worth tuning.
		if marked > 30 {
			t.Errorf("%+v: %d of 3000 uniform coordinates are in the closed band, which is too many to be the band",
				rc, marked)
		}
	}
}

// TestRimFalloffShelvesAndTheIcefieldRises is the falloff doing what
// DESIGN.md 15.1 says a traveller sees: ground that runs out rather than a wall,
// in whichever direction the floor lies.
//
// It compares each tile against *itself* under a zeroed rim rather than against
// its neighbor, which is the only comparison that means anything: the composite
// varies from tile to tile for reasons that have nothing to do with the rim, so
// a monotone walk is not a property of the world and a bound on the depression
// is.
func TestRimFalloffShelvesAndTheIcefieldRises(t *testing.T) {
	for _, rc := range []RimConfig{
		{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: -1, Kind: RimDeepOcean},
		{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: 0.6, Kind: RimPolarIce},
	} {
		g := rimGenerator(t, rc)
		plain := rimGenerator(t, RimConfig{FloorElevation: rc.FloorElevation, Kind: rc.Kind})
		closed, inner := int64(rc.ClosedHexes), int64(rc.ClosedHexes)+int64(rc.FalloffHexes)

		moved := 0
		for d := closed; d <= inner+1; d++ {
			for _, c := range rimRingCoords(WorldRadius-d, 16381) {
				got, was := g.ElevationAt(c), plain.ElevationAt(c)

				// Toward the floor and never past it. A blend that overshot
				// would be a rim that raised the ground on its way to lowering
				// it, which is the failure a weight outside [0, 1] produces.
				if min(was, rc.FloorElevation) > got || got > max(was, rc.FloorElevation) {
					t.Fatalf("%+v: at (%d, %d), %d hexes from the rim, %v is not between %v and the floor %v",
						rc, c.Q(), c.R(), d, got, was, rc.FloorElevation)
				}
				if got != was {
					moved++
				}

				switch d {
				case closed:
					// The outer join. The first depressed ring is the floor
					// exactly, which is what the ring outside it is forced to,
					// so the two bands meet with no step at all.
					if got != rc.FloorElevation {
						t.Fatalf("%+v: at (%d, %d), the outer join is %v, want the floor %v",
							rc, c.Q(), c.R(), got, rc.FloorElevation)
					}
				case inner - 1:
					// The inner join. The last depressed ring is within a
					// thousandth of the world it joins, which is what makes the
					// other end of the band a shelf rather than a step.
					if diff := math.Abs(got - was); diff > 0.001 {
						t.Fatalf("%+v: at (%d, %d), the inner join moved the elevation by %v",
							rc, c.Q(), c.R(), diff)
					}
				case inner, inner + 1:
					// Inside the band the world is the world, bit for bit.
					if math.Float64bits(got) != math.Float64bits(was) {
						t.Fatalf("%+v: at (%d, %d), %d hexes from the rim, the rim moved the elevation from %#016x to %#016x",
							rc, c.Q(), c.R(), d, math.Float64bits(was), math.Float64bits(got))
					}
				}
			}
		}
		if moved == 0 {
			t.Fatalf("%+v: the falloff moved no tile, so everything above is vacuous", rc)
		}
	}
}

// TestRimKeepsTheGeneratedClimate is DESIGN.md 15.1's rule about what a forced
// tile still reports: the heat and the moisture inside the band are the
// generated values, not zeros and not a constant.
//
// The reason is a measurement rather than a nicety. Every distribution readout
// that includes the band would be reporting a fabricated climate, and the
// fabrication would look like a cold dry ring around the world that the climate
// model never produced.
func TestRimKeepsTheGeneratedClimate(t *testing.T) {
	g := rimGenerator(t, RimConfig{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: -1})

	heats, moistures := map[float64]bool{}, map[float64]bool{}
	for _, c := range rimRingCoords(WorldRadius, 2039) {
		tile := g.Tile(c)
		if !tile.Rim {
			t.Fatalf("(%d, %d) is on the outermost ring and is not marked", c.Q(), c.R())
		}
		if tile.HeatValue == 0 || tile.MoistureValue == 0 {
			t.Fatalf("the forced tile at (%d, %d) reports heat %v and moisture %v; one of them is a fabricated zero",
				c.Q(), c.R(), tile.HeatValue, tile.MoistureValue)
		}
		if tile.HeatValue != g.HeatAt(c) || tile.MoistureValue != g.MoistureAt(c) {
			t.Fatalf("the forced tile at (%d, %d) reports a climate the composite does not", c.Q(), c.R())
		}
		heats[tile.HeatValue] = true
		moistures[tile.MoistureValue] = true
	}
	// A climate that was generated varies around the band; one that was
	// manufactured from the forced elevation alone would not.
	if len(heats) < 8 || len(moistures) < 8 {
		t.Errorf("the rim carries %d heats and %d moistures around the whole world, which is not a generated climate",
			len(heats), len(moistures))
	}
}

// TestRimAtZeroIsTheUnrimmedWorld is the assertion that lets the rim coexist
// with the wrap tests and with every golden value: ClosedHexes = 0 and
// FalloffHexes = 0 reproduce the unrimmed world **bit for bit**.
//
// It is bits rather than values because the failure it guards against is an
// arithmetically transparent rim — floor + 1*(e - floor), which is not e — and
// that moves a tile by an ulp. An ulp is invisible in a picture, fails no bound,
// and invalidates every recorded number in the module.
func TestRimAtZeroIsTheUnrimmedWorld(t *testing.T) {
	// The identity at the level of the profile, over values chosen to break a
	// lerp: the floor itself, the two ends of the scale, and a value whose
	// round trip through the blend would not land where it started.
	off := RimConfig{FloorElevation: -1}
	rng := rand.New(rand.NewPCG(0x7a1d, 0x9e3779b97f4a7c15))
	for _, d := range []int64{0, 1, 2, 96, 120, WorldRadius} {
		for _, e := range []float64{-1, -0.7, -0.1, 0, 0.1, 1.0 / 3, 0.7, 1, rng.Float64()} {
			got, rim := off.apply(d, e)
			if math.Float64bits(got) != math.Float64bits(e) || rim {
				t.Fatalf("a zero rim at %d hexes turned %#016x into %#016x (rim %v)",
					d, math.Float64bits(e), math.Float64bits(got), rim)
			}
		}
	}

	// And at the level of a tile, at the rim's own corners and edges, which is
	// where a rim that reached would reach. There is nothing to compare a zero
	// rim against except a rim that is not there at all, so the comparison is
	// the whole tile: every scalar bit for bit and every classification.
	zero := rimGenerator(t, RimConfig{FloorElevation: -1})
	wide := rimGenerator(t, RimConfig{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: -1})

	coords := rimRingCoords(WorldRadius, 4093)
	coords = append(coords, rimRingCoords(WorldRadius-13, 4093)...)
	coords = append(coords, probeCoords(500, 0x7a12)...)
	differed := 0
	for _, c := range coords {
		a, b := zero.Tile(c), wide.Tile(c)
		if a.Rim {
			t.Fatalf("a zero rim marked (%d, %d)", c.Q(), c.R())
		}
		for _, f := range []struct {
			name string
			got  float64
		}{
			{"elevation", a.ElevationValue},
			{"heat", a.HeatValue},
			{"moisture", a.MoistureValue},
			{"relief", a.ReliefValue},
		} {
			if math.IsNaN(f.got) {
				t.Fatalf("the unrimmed %s at (%d, %d) is NaN", f.name, c.Q(), c.R())
			}
		}
		if c.RimDistance() >= 120 {
			// Outside the wide rim's reach the two configurations are the same
			// world, and "the same" is bits.
			if math.Float64bits(a.ElevationValue) != math.Float64bits(b.ElevationValue) ||
				math.Float64bits(a.HeatValue) != math.Float64bits(b.HeatValue) ||
				math.Float64bits(a.MoistureValue) != math.Float64bits(b.MoistureValue) ||
				math.Float64bits(a.ReliefValue) != math.Float64bits(b.ReliefValue) ||
				a.Elevation != b.Elevation || a.Climate != b.Climate || a.Terrain != b.Terrain {
				t.Fatalf("at (%d, %d), %d hexes from the rim, a rim 120 hexes wide changed the tile",
					c.Q(), c.R(), c.RimDistance())
			}
		} else if a != b {
			differed++
		}
	}
	if differed == 0 {
		t.Fatal("the two configurations agreed everywhere, so the comparison above is vacuous")
	}
}

// TestRimIsUniformAllTheWayRound is the exit condition of DESIGN.md 32's phase 7
// that would otherwise be answered by looking at a whole-world image: the band is
// the same band on every side of the map.
//
// It is a statement about the terrain rather than about the distance, because the
// distance being uniform is arithmetic and the terrain being uniform is the
// thing somebody would notice. The profile weights are compared as bit patterns
// at one distance on all six sides, which is the other half: the same ring is
// the same depression whatever direction it is reached from.
func TestRimIsUniformAllTheWayRound(t *testing.T) {
	const closed, falloff = 24, 96
	g := rimGenerator(t, RimConfig{ClosedHexes: closed, FalloffHexes: falloff, FloorElevation: -1})

	for _, c := range rimRingCoords(WorldRadius, 3571) {
		if got := g.TerrainAt(c); got != TerrainDeepOcean {
			t.Fatalf("the rim at (%d, %d) is %v, want deep ocean all the way round", c.Q(), c.R(), got)
		}
	}

	for _, d := range []int64{closed, closed + 40, closed + falloff - 1} {
		want := math.Float64bits(g.RimProfileAt(NewCoord(WorldRadius-d, 0)))
		for _, c := range rimRingCoords(WorldRadius-d, 1021) {
			if got := math.Float64bits(g.RimProfileAt(c)); got != want {
				t.Fatalf("the profile at (%d, %d), %d hexes from the rim, is %#016x, want %#016x",
					c.Q(), c.R(), d, got, want)
			}
		}
	}
}

// TestRimReachesTheSample is the diagnostic half. A coordinate whose elevation
// looks wrong near the edge of the map has to be inspectable rather than
// inferred from a picture, which means the Sample carries the same three numbers
// the tile was made from.
func TestRimReachesTheSample(t *testing.T) {
	g := rimGenerator(t, RimConfig{ClosedHexes: 24, FalloffHexes: 96, FloorElevation: -1})
	for _, c := range []Coord{
		NewCoord(0, 0),
		NewCoord(WorldRadius, 0),
		NewCoord(WorldRadius-24, 0),
		NewCoord(WorldRadius-60, 0),
		NewCoord(WorldRadius-120, 0),
		NewCoord(0, -WorldRadius),
	} {
		s := g.Sample(c)
		if s.RimDistance != c.RimDistance() {
			t.Errorf("Sample.RimDistance at (%d, %d) = %d, want %d", c.Q(), c.R(), s.RimDistance, c.RimDistance())
		}
		if s.Rim != g.IsRim(c) {
			t.Errorf("Sample.Rim at (%d, %d) = %v, want %v", c.Q(), c.R(), s.Rim, g.IsRim(c))
		}
		if s.RimProfile != g.RimProfileAt(c) {
			t.Errorf("Sample.RimProfile at (%d, %d) = %v, want %v", c.Q(), c.R(), s.RimProfile, g.RimProfileAt(c))
		}
		if s.Elevation != g.ElevationAt(c) {
			t.Errorf("Sample.Elevation at (%d, %d) is not the elevation the rim left", c.Q(), c.R())
		}
	}
}
