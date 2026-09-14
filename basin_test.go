// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"testing"

	"github.com/mdhender/wgva/internal/mathx"
)

// ---------------------------------------------------------------------------
// The composite
// ---------------------------------------------------------------------------

// TestBasinIsInRange asserts the composite stays inside [-1, +1].
//
// It is a weighted average of four terms that are each in that range, so it
// cannot leave it — which is exactly why it is worth asserting: the property is
// a consequence of dividing by the total of the weights, and a normalization
// that was changed to divide by something else would still look plausible.
func TestBasinIsInRange(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(4000, 0x0ba5) {
		if b := g.BasinAt(c); !(b >= -1 && b <= 1) {
			t.Fatalf("basin at (%d, %d) is %v, which is outside [-1, +1]", c.Q(), c.R(), b)
		}
	}
}

// TestVolcanicIsInRange asserts the same of the volcanic tendency, for the same
// reason.
func TestVolcanicIsInRange(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(4000, 0x0f1a) {
		if v := g.VolcanicAt(c); !(v >= -1 && v <= 1) {
			t.Fatalf("volcanic at (%d, %d) is %v, which is outside [-1, +1]", c.Q(), c.R(), v)
		}
	}
}

// TestBasinMatchesAnIndependentComputation rebuilds the composite from the
// derived fields and the blended region parameters and asserts the result is
// bit-identical.
//
// The expected value is derived rather than recorded: the weighted average is
// written out here from DESIGN.md 17 and compared against what the generator
// produced, so a reordered accumulation or a dropped mathx.Mul shows up as a
// difference in the last place.
func TestBasinMatchesAnIndependentComputation(t *testing.T) {
	g := NewDefault(probeSeed)
	bc := g.Config().Basin
	norm := bc.BroadWeight + bc.RegionalWeight + bc.LocalWeight + bc.BiasWeight

	for _, c := range probeCoords(500, 0x0ba51) {
		p := AxialToWorld(c)
		region := g.RegionInfluence(c)

		broad, regional, local := g.BasinBroadField(), g.BasinRegionalField(), g.BasinLocalField()
		want := (mathx.Mul(bc.BroadWeight, broad.Sample(p.X, p.Y)) +
			mathx.Mul(bc.RegionalWeight, regional.Sample(p.X, p.Y)) +
			mathx.Mul(bc.LocalWeight, local.Sample(p.X, p.Y)) +
			mathx.Mul(bc.BiasWeight, region.BasinBias)) / norm

		if got := g.BasinAt(c); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("basin at (%d, %d) = %#016x (%v), want %#016x (%v)",
				c.Q(), c.R(), math.Float64bits(got), got, math.Float64bits(want), want)
		}
	}
}

// TestVolcanicMatchesAnIndependentComputation does the same for the volcanic
// tendency. See TestBasinMatchesAnIndependentComputation.
func TestVolcanicMatchesAnIndependentComputation(t *testing.T) {
	g := NewDefault(probeSeed)
	tc := g.Config().Terrain
	norm := tc.VolcanicFieldWeight + tc.VolcanicBiasWeight

	for _, c := range probeCoords(500, 0x0f1ac) {
		p := AxialToWorld(c)
		volcanic := g.VolcanicField()
		want := (mathx.Mul(tc.VolcanicFieldWeight, volcanic.Sample(p.X, p.Y)) +
			mathx.Mul(tc.VolcanicBiasWeight, g.RegionInfluence(c).Volcanic)) / norm

		if got := g.VolcanicAt(c); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("volcanic at (%d, %d) = %#016x (%v), want %#016x (%v)",
				c.Q(), c.R(), math.Float64bits(got), got, math.Float64bits(want), want)
		}
	}
}

// ---------------------------------------------------------------------------
// The product
// ---------------------------------------------------------------------------

// TestWetnessIsAProductAndNotASum is the whole of DESIGN.md 17.1's geography,
// asserted rather than described: a basin makes a wet climate wetter and a dry
// one drier, and a rise sheds water either way.
//
// A sum would fail every dry row here, and failing them is what would put a
// marsh in the middle of a desert. Dry endorheic regions such as the Great
// Basin are the case the product exists to produce.
func TestWetnessIsAProductAndNotASum(t *testing.T) {
	cfg := DefaultConfig()

	for _, tc := range []struct {
		name     string
		moisture float64
		basin    float64
		want     string
	}{
		{"a basin in a wet climate", +0.5, +0.6, "wetter"},
		{"a basin in a dry climate", -0.5, +0.6, "drier"},
		{"a rise in a wet climate", +0.5, -0.6, "drier"},
		{"a rise in a dry climate", -0.5, -0.6, "wetter"},
		{"neutral ground", +0.5, 0, "unchanged"},
		{"a basin at the middle of the moisture scale", 0, +0.6, "unchanged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cfg.wetness(tc.moisture, tc.basin)
			// "Wetter" and "drier" are away from and toward the middle of the
			// scale, not up and down it: the scalar is signed and the product
			// moves a value away from zero in whichever direction it already
			// lay.
			var moved string
			switch {
			case math.Abs(got) > math.Abs(tc.moisture):
				moved = "wetter"
			case math.Abs(got) < math.Abs(tc.moisture):
				moved = "drier"
			default:
				moved = "unchanged"
			}
			// A dry climate made drier runs down the scale, so name the two
			// directions the way the terrain rules read them.
			if tc.moisture < 0 {
				switch moved {
				case "wetter":
					moved = "drier"
				case "drier":
					moved = "wetter"
				}
			}
			if moved != tc.want {
				t.Errorf("moisture %v in basin %v became %v, which is %s, want %s",
					tc.moisture, tc.basin, got, moved, tc.want)
			}
		})
	}
}

// TestWetnessIsClamped asserts the product cannot leave the range the moisture
// band ladder cuts.
//
// A saturated tile in a deep basin is the case: the product exceeds one, and a
// ladder whose top threshold is below one would classify it correctly anyway,
// which is what makes an unclamped value a defect nothing downstream would
// report.
func TestWetnessIsClamped(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Basin.MoistureWeight = 1

	for _, moisture := range []float64{-1, -0.99, 0, 0.99, 1} {
		for _, basin := range []float64{-1, -0.5, 0, 0.5, 1} {
			w := cfg.wetness(moisture, basin)
			if !(w >= -1 && w <= 1) {
				t.Errorf("wetness(%v, %v) = %v, which is outside [-1, +1]", moisture, basin, w)
			}
		}
	}
}

// TestZeroMoistureWeightLeavesClimateAlone asserts the documented off position:
// at a weight of zero terrain reads the climate's moisture unchanged, bit for
// bit, and the basin fields are geography nothing consumes.
//
// It is what a bisecting tuner sets the dial to, and it has to be exact rather
// than close, because "the basin term is off" and "the basin term is small" are
// different answers to the question being asked.
func TestZeroMoistureWeightLeavesClimateAlone(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Basin.MoistureWeight = 0
	g, err := New(probeSeed, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, c := range probeCoords(2000, 0x0ff) {
		moisture := g.MoistureAt(c)
		if got := cfg.wetness(moisture, g.BasinAt(c)); math.Float64bits(got) != math.Float64bits(moisture) {
			t.Fatalf("at (%d, %d) a zero moisture weight moved moisture %v to %v", c.Q(), c.R(), moisture, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Where basin influence is not
// ---------------------------------------------------------------------------

// TestBasinDoesNotEnterElevationOrClimate is the rule DESIGN.md 17.1 is most
// worried about, asserted directly: moving every dial the basin composite has
// moves no elevation and no climate value anywhere.
//
// A basin term inside the elevation composite is the obvious way to make a
// depression *be* lower ground, and it would move every tile in every world.
// The golden tables catch it at twenty-four coordinates; this catches it at two
// thousand, and it names the cause rather than reporting that a recorded number
// moved.
func TestBasinDoesNotEnterElevationOrClimate(t *testing.T) {
	base := NewDefault(probeSeed)

	moved := DefaultConfig()
	moved.Basin.Broad.WavelengthMiles = 2500
	moved.Basin.Regional.Octaves = 2
	moved.Basin.Local.Gain = 0.25
	moved.Basin.BroadWeight = 0.4
	moved.Basin.RegionalWeight = 1
	moved.Basin.LocalWeight = 0.9
	moved.Basin.BiasWeight = 0.1
	moved.Basin.MoistureWeight = 1
	other, err := New(probeSeed, moved)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, c := range probeCoords(2000, 0x0e1e7) {
		for _, f := range []struct {
			name string
			got  float64
			want float64
		}{
			{"elevation", other.ElevationAt(c), base.ElevationAt(c)},
			{"relief", other.Relief(c), base.Relief(c)},
			{"heat", other.HeatAt(c), base.HeatAt(c)},
			{"moisture", other.MoistureAt(c), base.MoistureAt(c)},
		} {
			if math.Float64bits(f.got) != math.Float64bits(f.want) {
				t.Fatalf("moving the basin configuration moved %s at (%d, %d) from %v to %v",
					f.name, c.Q(), c.R(), f.want, f.got)
			}
		}
	}
}

// TestBasinReachesTerrain is the other half of the rule above. Basin influence
// must be absent from elevation and climate and present in terrain, and a
// composite that had been quietly disconnected would pass the first half
// perfectly.
func TestBasinReachesTerrain(t *testing.T) {
	base := NewDefault(probeSeed)

	moved := DefaultConfig()
	moved.Basin.MoistureWeight = 1
	other, err := New(probeSeed, moved)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	differences := 0
	coords := probeCoords(2000, 0x0e1e8)
	for _, c := range coords {
		if base.TerrainAt(c) != other.TerrainAt(c) {
			differences++
		}
	}
	if differences == 0 {
		t.Fatalf("the basin moisture weight moved no terrain in %d coordinates; the composite is not reaching the classifier", len(coords))
	}
}

// ---------------------------------------------------------------------------
// The fields underneath
// ---------------------------------------------------------------------------

// TestBasinFieldsHaveTheirOwnDomains asserts the three basin scales and the
// volcanic tendency are four lattices and not one.
//
// This is the rule of DESIGN.md 8.1 in the case it was written for. Two field
// nodes under one hashing domain share a gradient table *and* a seed-derived
// sampling offset, so at the world origin they land in the same lattice cell at
// the same fractional position and return the same value — and the basin
// composite would then be one field counted three times, which is a plausible
// looking map that is not the one the configuration describes.
func TestBasinFieldsHaveTheirOwnDomains(t *testing.T) {
	seen := map[uint64]string{}
	for _, f := range []struct {
		name string
		dom  uint64
	}{
		{"basin broad", DomBasin},
		{"basin regional", DomBasinRegional},
		{"basin local", DomBasinLocal},
		{"volcanic", DomVolcanic},
		{"heat", DomTemperature},
		{"moisture", DomMoisture},
		{"moisture variation", DomMoistureVariation},
		{"ridge", DomRidgeStructure},
	} {
		if other, ok := seen[f.dom]; ok {
			t.Errorf("%s and %s share the hashing domain %#016x", f.name, other, f.dom)
		}
		seen[f.dom] = f.name
	}

	// The same thing measured rather than declared: at the origin, which is
	// where a shared offset would put two fields in the same place, the four
	// values are distinct.
	g := NewDefault(probeSeed)
	p := AxialToWorld(NewCoord(0, 0))
	values := map[float64]string{}
	for _, f := range []struct {
		name  string
		field Field
	}{
		{"basin broad", g.BasinBroadField()},
		{"basin regional", g.BasinRegionalField()},
		{"basin local", g.BasinLocalField()},
		{"volcanic", g.VolcanicField()},
	} {
		field := f.field
		v := field.Sample(p.X, p.Y)
		if other, ok := values[v]; ok {
			t.Errorf("%s and %s both sample to %v at the origin", f.name, other, v)
		}
		values[v] = f.name
	}
}

// TestSampleCarriesTheBasinAndVolcanic asserts the diagnostic values are the
// same evaluation as the methods, bit for bit.
//
// A diagnostic that recomputed its own version of a number would eventually
// disagree with the number, and the disagreement would be invisible: a layer
// would draw one thing and the classifier would read another.
func TestSampleCarriesTheBasinAndVolcanic(t *testing.T) {
	g := NewDefault(probeSeed)
	for _, c := range probeCoords(500, 0x05a3) {
		s := g.Sample(c)
		if got, want := s.BasinInfluence, g.BasinAt(c); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("Sample.BasinInfluence at (%d, %d) is %v, want %v", c.Q(), c.R(), got, want)
		}
		if got, want := s.Volcanic, g.VolcanicAt(c); math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("Sample.Volcanic at (%d, %d) is %v, want %v", c.Q(), c.R(), got, want)
		}
	}
}

// TestBasinIsDeterministic asserts the composite is a pure function of the
// seed, the coordinate, and the configuration: two generators built the same
// way agree bit for bit, and the coordinates are visited in a different order
// the second time.
func TestBasinIsDeterministic(t *testing.T) {
	a := NewDefault(probeSeed)
	b := NewDefault(probeSeed)

	coords := probeCoords(1000, 0x0d37)
	first := make([]float64, len(coords))
	for i, c := range coords {
		first[i] = a.BasinAt(c)
	}
	for i := len(coords) - 1; i >= 0; i-- {
		if got := b.BasinAt(coords[i]); math.Float64bits(got) != math.Float64bits(first[i]) {
			t.Fatalf("basin at (%d, %d) is %v on the second pass and was %v on the first",
				coords[i].Q(), coords[i].R(), got, first[i])
		}
	}
}
