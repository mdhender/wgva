// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// TestFieldKindsAreExhaustive keeps String and Valid in step with the constants.
// Adding a kind without teaching either is how an undeclared node comes to
// evaluate as "unknown" and sample as a panic a long way from the mistake.
func TestFieldKindsAreExhaustive(t *testing.T) {
	declared := []FieldKind{FieldValue, FieldSimplex, FieldFBM, FieldWarp, FieldOffset, FieldSum}
	seen := map[string]bool{}
	for _, k := range declared {
		if !k.Valid() {
			t.Errorf("%d is declared but Valid says otherwise", uint8(k))
		}
		name := k.String()
		if name == "unknown" {
			t.Errorf("%d has no name", uint8(k))
		}
		if seen[name] {
			t.Errorf("%q names two kinds", name)
		}
		seen[name] = true
	}
	for _, k := range []FieldKind{0, 7, 255} {
		if k.Valid() {
			t.Errorf("%d is not declared but Valid accepts it", uint8(k))
		}
		if k.String() != "unknown" {
			t.Errorf("%d names itself %q", uint8(k), k.String())
		}
	}
}

// TestScalesAreExhaustive is the same check for the continuous scales, plus the
// one property scaleCount depends on: the declared values are 1..scaleCount with
// no gap, because they index into Generator.scales.
func TestScalesAreExhaustive(t *testing.T) {
	got := Scales()
	if len(got) != scaleCount {
		t.Fatalf("Scales() returned %d values, want scaleCount = %d", len(got), scaleCount)
	}
	for i, s := range got {
		if uint8(s) != uint8(i)+1 {
			t.Fatalf("Scales()[%d] is %d, want %d; the values index Generator.scales", i, uint8(s), i+1)
		}
		if !s.Valid() || s.String() == "unknown" {
			t.Errorf("%d is returned by Scales but is not a declared scale", uint8(s))
		}
	}
	for _, s := range []Scale{0, scaleCount + 1, 255} {
		if s.Valid() {
			t.Errorf("%d is not declared but Valid accepts it", uint8(s))
		}
	}
}

// leafField is the smallest valid tree, used as the starting point for the
// malformed ones below.
func leafField() Field {
	return Field{Kind: FieldSimplex, Seed: 1, Domain: DomRelief, WavelengthMiles: 600}
}

// TestFieldValidateRejectsWrongShape is the check DESIGN.md 9.1 asks for. The
// tagged struct's cost is that every field is addressable on every node, so a
// stray value reads as configured and evaluates as if it were not.
func TestFieldValidateRejectsWrongShape(t *testing.T) {
	leaf := leafField()

	cases := []struct {
		name  string
		field string
		want  error
		build func() Field
	}{
		{"undeclared kind", "field.Kind", ErrOutOfRange,
			func() Field { return Field{Kind: 9} }},
		{"leaf with octaves", "field.Octaves", ErrFieldShape,
			func() Field { f := leafField(); f.Octaves = 3; return f }},
		{"leaf with a source", "field.Source", ErrFieldShape,
			func() Field { f := leafField(); f.Source = &leaf; return f }},
		{"fbm with a wavelength", "field.WavelengthMiles", ErrFieldShape,
			func() Field {
				return Field{Kind: FieldFBM, WavelengthMiles: 10, Octaves: 2, Lacunarity: 2, Gain: 0.5, Source: &leaf}
			}},
		{"fbm with no source", "field.Source", ErrFieldShape,
			func() Field { return Field{Kind: FieldFBM, Octaves: 2, Lacunarity: 2, Gain: 0.5} }},
		{"warp with no warp fields", "field.WarpX", ErrFieldShape,
			func() Field { return Field{Kind: FieldWarp, StrengthMiles: 5, Source: &leaf} }},
		{"offset with a strength", "field.StrengthMiles", ErrFieldShape,
			func() Field {
				return Field{Kind: FieldOffset, DXMiles: 1, DYMiles: 2, StrengthMiles: 3, Source: &leaf}
			}},
		{"sum with a source", "field.Source", ErrFieldShape,
			func() Field {
				return Field{Kind: FieldSum, Terms: []WeightedField{{Weight: 1, Field: leaf}}, Source: &leaf}
			}},
		{"fbm with no octaves", "field.Octaves", ErrOctaveCount,
			func() Field { return Field{Kind: FieldFBM, Lacunarity: 2, Gain: 0.5, Source: &leaf} }},
		{"fbm below Nyquist", "field.Octaves", ErrBelowNyquist,
			func() Field {
				short := Field{Kind: FieldSimplex, Seed: 1, Domain: DomRelief, WavelengthMiles: 24}
				return Field{Kind: FieldFBM, Octaves: 4, Lacunarity: 2, Gain: 0.5, Source: &short}
			}},
		{"leaf below Nyquist", "field.WavelengthMiles", ErrBelowNyquist,
			func() Field { f := leafField(); f.WavelengthMiles = 1; return f }},
		{"warp with a negative strength", "field.StrengthMiles", ErrNotPositive,
			func() Field {
				x, y := leafField(), leafField()
				return Field{Kind: FieldWarp, StrengthMiles: -1, WarpX: &x, WarpY: &y, Source: &leaf}
			}},
		{"offset with a NaN", "field.DXMiles", ErrNotFinite,
			func() Field { return Field{Kind: FieldOffset, DXMiles: math.NaN(), Source: &leaf} }},
		{"sum term with a NaN weight", "field.Terms[0].Weight", ErrNotFinite,
			func() Field {
				return Field{Kind: FieldSum, Terms: []WeightedField{{Weight: math.NaN(), Field: leaf}}}
			}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.build()
			err := f.Validate()
			if err == nil {
				t.Fatal("Validate accepted the node")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate returned %v, want an error wrapping %v", err, tc.want)
			}
			ce, ok := errors.AsType[*ConfigError](err)
			if !ok {
				t.Fatalf("Validate returned %T, want a *ConfigError", err)
			}
			if ce.Field != tc.field {
				t.Fatalf("error names %q, want %q", ce.Field, tc.field)
			}
		})
	}
}

// TestDerivedFieldsValidate asserts what New relies on: a valid configuration
// cannot derive an invalid tree.
func TestDerivedFieldsValidate(t *testing.T) {
	cfg := DefaultConfig()
	for _, s := range Scales() {
		f := DeriveFields(42, cfg, s)
		if err := f.Validate(); err != nil {
			t.Errorf("the derived %v tree is invalid: %v", s, err)
		}
	}
}

// TestDerivedTreeShape pins the composition DESIGN.md 9.4 requires: the
// sampling offset sits above the fbm, not inside the leaf, and the warp sits
// above the offset so the warp fields see the unwarped position.
func TestDerivedTreeShape(t *testing.T) {
	f := DeriveFields(42, DefaultConfig(), ScaleContinental)

	if f.Kind != FieldWarp {
		t.Fatalf("the root is %v, want warp", f.Kind)
	}
	if f.Source.Kind != FieldOffset {
		t.Fatalf("the warp's source is %v, want offset", f.Source.Kind)
	}
	if f.Source.Source.Kind != FieldFBM {
		t.Fatalf("the offset's source is %v, want fbm; an offset below the fbm is the same defect with a smaller coefficient", f.Source.Source.Kind)
	}
	if f.Source.Source.Source.Kind != FieldSimplex {
		t.Fatalf("the fbm's source is %v, want a noise leaf", f.Source.Source.Source.Kind)
	}
	if f.Source.Source.Source.WavelengthMiles != DefaultConfig().Continental.WavelengthMiles {
		t.Error("the leaf does not carry the configured wavelength")
	}
	// Each warp field carries its own offset, for the same reason the source
	// does: without one they are zero at the origin too, and a warp that is zero
	// where the field it warps is also zero does not move the anomaly.
	for name, w := range map[string]*Field{"warp-x": f.WarpX, "warp-y": f.WarpY} {
		if w.Kind != FieldOffset {
			t.Errorf("%s is %v, want offset", name, w.Kind)
		}
	}
	if f.WarpX.DXMiles == f.WarpY.DXMiles {
		t.Error("the two warp fields share a sampling offset, so they are the same field")
	}
}

// TestDeriveFieldsIsPure asserts the tree is a function of the seed and the
// configuration and of nothing else. It is derived rather than stored precisely
// so that there is one description of the field graph; two calls that disagreed
// would make it two.
func TestDeriveFieldsIsPure(t *testing.T) {
	cfg := DefaultConfig()
	for _, s := range Scales() {
		a := DeriveFields(7, cfg, s)
		b := DeriveFields(7, cfg, s)
		for _, p := range [][2]float64{{0, 0}, {123.5, -456.25}, {-9e4, 7e4}} {
			if av, bv := a.Sample(p[0], p[1]), b.Sample(p[0], p[1]); av != bv {
				t.Fatalf("%v at %v: two derivations returned %v and %v", s, p, av, bv)
			}
		}
	}
}

// TestFieldSampleRange asserts the range every kind documents. It is what the
// diagnostic ramp and every later threshold are written against.
func TestFieldSampleRange(t *testing.T) {
	g := NewDefault(31337)
	for _, s := range Scales() {
		var extreme float64
		for q := int64(-400); q <= 400; q += 7 {
			for r := int64(-400); r <= 400; r += 7 {
				v := g.ScaleAt(s, NewCoord(q, r))
				if v < -1 || v > 1 {
					t.Fatalf("%v at (%d, %d) = %v, outside [-1, +1]", s, q, r, v)
				}
				extreme = max(extreme, math.Abs(v))
			}
		}
		if extreme < 0.2 {
			t.Errorf("%v never exceeded %v over the sampled window, which is too flat to tune against", s, extreme)
		}
	}
}

// TestSumIsAWeightedAverage pins the choice sampleSum makes. A plain sum would
// leave the range claim to the caller, and every consumer would then carry its
// own normalization.
func TestSumIsAWeightedAverage(t *testing.T) {
	a := leafField()
	b := leafField()
	b.Domain = DomMoisture

	sum := Field{Kind: FieldSum, Terms: []WeightedField{{Weight: 3, Field: a}, {Weight: 1, Field: b}}}
	if err := sum.Validate(); err != nil {
		t.Fatalf("the sum is invalid: %v", err)
	}

	const x, y = 91.5, -37.25
	want := (3*a.Sample(x, y) + b.Sample(x, y)) / 4
	if got := sum.Sample(x, y); math.Abs(got-want) > 1e-12 {
		t.Errorf("sum sampled %v, want the weighted average %v", got, want)
	}

	empty := Field{Kind: FieldSum}
	if got := empty.Sample(x, y); got != 0 {
		t.Errorf("an empty sum returned %v, want 0", got)
	}
}

// TestWarpStrengthZeroIsIdentity is the property that makes a disabled warp a
// legitimate setting rather than a missing one.
func TestWarpStrengthZeroIsIdentity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarpStrengthMiles = 0
	g, err := New(5, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	inner := fbmField(5, DomContinentalness, cfg.Continental)
	for _, p := range [][2]float64{{0, 0}, {1000, -2000}, {-55.5, 12.25}} {
		c := NewCoord(int64(p[0]), int64(p[1]))
		w := AxialToWorld(c)
		if got, want := g.ScaleAt(ScaleContinental, c), inner.Sample(w.X, w.Y); got != want {
			t.Fatalf("at %v a zero-strength warp returned %v, want the unwarped %v", c, got, want)
		}
	}
}

// TestDescribeNamesTheTree is a light assertion on the printable form. The tool
// draws this beside a window so that what was evaluated can be read off the page
// rather than reconstructed from the configuration.
func TestDescribeNamesTheTree(t *testing.T) {
	tree := DeriveFields(1, DefaultConfig(), ScaleRegional)
	got := tree.Describe()
	for _, want := range []string{"warp", "warp-x", "warp-y", "offset", "fbm", "simplex", "octaves=", "wavelength="} {
		if !strings.Contains(got, want) {
			t.Errorf("Describe() does not mention %q:\n%s", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// The sampling offset — DESIGN.md 9.4 and 30.13
// ---------------------------------------------------------------------------

// TestSamplingOffsetIsClearOfTheCellCorners asserts the confinement. A hash is
// free to come back near zero, and a fix that works for most seeds is not a fix.
func TestSamplingOffsetIsClearOfTheCellCorners(t *testing.T) {
	const wavelength = 600.0
	for s := range Seed(500) {
		for _, dom := range []uint64{DomContinentalness, DomRegionalElevation, DomRelief, DomTerrainDetail} {
			dx, dy := samplingOffset(s, dom, wavelength)
			for _, d := range []float64{dx, dy} {
				f := d / wavelength
				if f < samplingOffsetLo || f > samplingOffsetHi {
					t.Fatalf("seed %d domain %#x: offset is %v of a cell, outside [%v, %v]",
						s, dom, f, samplingOffsetLo, samplingOffsetHi)
				}
			}
		}
	}
}

// TestSamplingOffsetSeparatesSeedsAndDomains asserts the two properties that
// make the offset a fix rather than a relocation: two worlds do not share the
// anomaly's new location, and the scales do not all land on their own lattice
// points at some other single coordinate.
func TestSamplingOffsetSeparatesSeedsAndDomains(t *testing.T) {
	const wavelength = 600.0

	seen := map[[2]float64]string{}
	for _, dom := range []uint64{DomContinentalness, DomRegionalElevation, DomRelief, DomTerrainDetail, DomWarpX, DomWarpY} {
		dx, dy := samplingOffset(4242, dom, wavelength)
		if prev, ok := seen[[2]float64{dx, dy}]; ok {
			t.Errorf("domains %#x and %s share an offset", dom, prev)
		}
		seen[[2]float64{dx, dy}] = "another"
	}

	seenSeeds := map[[2]float64]Seed{}
	for s := range Seed(200) {
		dx, dy := samplingOffset(s, DomContinentalness, wavelength)
		if prev, ok := seenSeeds[[2]float64{dx, dy}]; ok {
			t.Errorf("seeds %d and %d share an offset", s, prev)
		}
		seenSeeds[[2]float64{dx, dy}] = s
	}
}

// ringCoords returns the canonical coordinates at a hex distance from the
// origin, in a fixed order. Radius zero is the origin alone.
func ringCoords(radius int64) []Coord {
	if radius == 0 {
		return []Coord{Origin}
	}
	var out []Coord
	q, r := directions[4][0]*radius, directions[4][1]*radius
	for d := range 6 {
		for range radius {
			out = append(out, NewCoord(q, r))
			q += directions[d][0]
			r += directions[d][1]
		}
	}
	return out
}

// TestNoPlaceIsSpecial is the measurement DESIGN.md 30.13 asks for.
//
// It cannot be a single window. At one seed the origin is a bright dot among a
// handful of others, and a single-tile assertion would pass as soon as the
// defect's peak moved one hex. Pooling over many seeds is what separates the
// signal from terrain: ordinary terrain is uncorrelated between worlds and
// cancels, leaving whatever is a function of position relative to the centre.
//
// Two statistics are pooled per ring, and each catches a different defect.
//
//   - The mean absolute value catches a missing offset. Gradient noise is
//     exactly zero at a lattice point, so without the offset the origin's pooled
//     mean collapses to zero while every other ring sits near a third.
//   - The mean absolute first difference to the six neighbors catches an offset
//     folded into the leaf, which DESIGN.md 9.4 calls the same defect with a
//     smaller coefficient. The value there is not zero — every octave samples
//     the same cell at the same fractional position, so they agree rather than
//     vanish — but their slopes then add constructively, and the whole region
//     around the origin is measurably steeper.
//
// The reference ring is far enough out to lie beyond the coarsest wavelength.
// That is not a detail: the folded-offset artifact is the size of the field it
// belongs to, about a thousand hexes for the continental scale, so a reference
// ring a few dozen hexes from the origin sits inside the defect and measures it
// against itself. With a reference at 34 hexes the folded-offset tree passes
// this test; with the reference below, it fails on every scale.
//
// Both thresholds were set against the defects rather than guessed. Measured
// over these seeds, the correct tree's slope ratios run from 0.91 to 1.00 and
// its level ratios from 0.85 to 1.09; dropping the offset takes the origin's
// level ratio to 0.00, and folding it into the leaf takes slope ratios to
// between 1.13 and 2.32.
func TestNoPlaceIsSpecial(t *testing.T) {
	const (
		seeds = 96
		// referenceRing is beyond the coarsest wavelength, which is a thousand
		// hexes; ringSamples keeps a ring that large affordable.
		referenceRing = 4096
		ringSamples   = 60
		// A ring's pooled mean must stay inside these multiples of the
		// reference ring's.
		levelLo, levelHi = 0.5, 2.0
		slopeHi          = 1.10
	)
	rings := []int64{0, 1, 2, 3, 5, 8, 13, 21, 34, referenceRing}

	for _, s := range Scales() {
		t.Run(s.String(), func(t *testing.T) {
			t.Parallel()

			level := make([]float64, len(rings))
			slope := make([]float64, len(rings))

			for seed := range Seed(seeds) {
				g := NewDefault(seed)
				for i, radius := range rings {
					ring := sampledRing(radius, ringSamples)
					var sumLevel, sumSlope float64
					for _, c := range ring {
						v := g.ScaleAt(s, c)
						sumLevel += math.Abs(v)
						var d float64
						for dir := range 6 {
							d += math.Abs(g.ScaleAt(s, c.Neighbor(dir)) - v)
						}
						sumSlope += d / 6
					}
					level[i] += sumLevel / float64(len(ring))
					slope[i] += sumSlope / float64(len(ring))
				}
			}

			ref := len(rings) - 1
			for i, radius := range rings {
				l := level[i] / level[ref]
				if l < levelLo || l > levelHi {
					t.Errorf("ring %d has %.3f of the reference ring's pooled mean |value|: this place is special",
						radius, l)
				}
				if sl := slope[i] / slope[ref]; sl > slopeHi {
					t.Errorf("ring %d has %.3f of the reference ring's pooled mean slope: there is a halo here",
						radius, sl)
				}
			}
		})
	}
}

// sampledRing returns at most limit coordinates spread evenly around a ring. A
// ring four thousand hexes out has twenty-four thousand tiles and the pooled
// statistic does not need them all.
func sampledRing(radius int64, limit int) []Coord {
	all := ringCoords(radius)
	if len(all) <= limit {
		return all
	}
	out := make([]Coord, 0, limit)
	for i := range limit {
		out = append(out, all[i*len(all)/limit])
	}
	return out
}

// TestOriginIsNotZero is the blunt half of the same rule, and the one that fails
// first if the offset is ever dropped: without it every field is exactly zero at
// the world origin, for every seed.
func TestOriginIsNotZero(t *testing.T) {
	for seed := range Seed(64) {
		g := NewDefault(seed)
		for _, s := range Scales() {
			if v := g.ScaleAt(s, Origin); v == 0 {
				t.Errorf("seed %d: %v is exactly zero at the origin", seed, s)
			}
		}
	}
}

// TestOffsetDoesNotDisturbTileIdentity asserts the third thing DESIGN.md 30.13
// asks for. The offset moves where the lattice sits; it must not move what a
// coordinate means, so the origin and its six wrapped images still sample
// identically.
func TestOffsetDoesNotDisturbTileIdentity(t *testing.T) {
	g := NewDefault(2026)
	for _, m := range mirrorCenters {
		c := NewCoord(m[0], m[1])
		if c != Origin {
			t.Fatalf("a mirror center normalized to %v, want the origin", c)
		}
		for _, s := range Scales() {
			if got, want := g.ScaleAt(s, c), g.ScaleAt(s, Origin); got != want {
				t.Errorf("%v at a wrapped image of the origin is %v, want %v", s, got, want)
			}
		}
	}
}

func BenchmarkScaleAt(b *testing.B) {
	g := NewDefault(1)
	var sink float64
	q := int64(0)
	for b.Loop() {
		q++
		sink += g.ScaleAt(ScaleContinental, NewCoord(q%1000, 17))
	}
	_ = sink
}
