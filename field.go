// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"fmt"
	"math"
	"strings"

	"github.com/mdhender/wgva/internal/mathx"
)

// FieldKind names one node shape in the field composition tree.
//
// The set is closed and comes from configuration, so this is a tagged struct
// with a switch on the kind rather than an interface with a type switch. The
// reason that matters is not the jump table: the tree has to be inspectable and
// printable — the terrain tuning tool draws it — and an interface with six
// implementing types needs custom marshalling to round-trip, which is a second
// place for the field graph to be described and therefore a second place for it
// to be wrong. See DESIGN.md 9.1.
//
// The derived tree is written into a world file as a diagnostic record, so these
// values are persisted. Each is written out explicitly rather than taken from
// iota: inserting a kind into the middle of an iota block would renumber
// everything after it with nothing in the diff that looks like a data change.
type FieldKind uint8

// The field kinds.
const (
	FieldValue   FieldKind = 1
	FieldSimplex FieldKind = 2
	FieldFBM     FieldKind = 3
	FieldWarp    FieldKind = 4
	FieldOffset  FieldKind = 5
	FieldSum     FieldKind = 6
)

// String returns the name of the field kind.
func (k FieldKind) String() string {
	switch k {
	case FieldValue:
		return "value"
	case FieldSimplex:
		return "simplex"
	case FieldFBM:
		return "fbm"
	case FieldWarp:
		return "warp"
	case FieldOffset:
		return "offset"
	case FieldSum:
		return "sum"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared field kind.
func (k FieldKind) Valid() bool {
	switch k {
	case FieldValue, FieldSimplex, FieldFBM, FieldWarp, FieldOffset, FieldSum:
		return true
	default:
		return false
	}
}

// MaxOctaves bounds an fbm ladder. It is a sanity bound rather than a tuning
// one — the Nyquist rule of DESIGN.md 9.3 is what actually stops a ladder, and
// it stops every useful one well before this.
const MaxOctaves uint8 = 16

// Field is one node of the composition tree. Its Kind decides which of the
// fields below are read; the rest must be zero, and Validate says so.
//
// Every kind returns a value in [-1, +1]. The tree is derived from the flat
// Config by DeriveFields and is never a second source of truth: a human edits
// wavelengths and weights, not a tree. See DESIGN.md 9.1.
type Field struct {
	Kind FieldKind

	// Value, Simplex: the lattice noise leaves. Seed and Domain separate this
	// leaf's lattice from every other; WavelengthMiles is how many miles of
	// world space one lattice cell spans.
	Seed            Seed
	Domain          uint64
	WavelengthMiles float64

	// FBM: a ladder of Source evaluations, coarse to fine. The position is
	// multiplied by the frequency before Source divides by its wavelength, so
	// octave k has an effective wavelength of WavelengthMiles/Lacunarity^k.
	Octaves    uint8
	Lacunarity float64
	Gain       float64

	// Warp: displace the sampling position by StrengthMiles times a pair of
	// fields. DESIGN.md 13.
	StrengthMiles float64
	WarpX, WarpY  *Field

	// Offset: translate the sampling position. This is the seed-derived
	// displacement of DESIGN.md 9.4, and it sits above the fbm rather than
	// inside the leaf.
	DXMiles, DYMiles float64

	// FBM, Warp, Offset
	Source *Field

	// Sum: a weighted average of the terms.
	Terms []WeightedField
}

// WeightedField is one term of a FieldSum.
type WeightedField struct {
	Weight float64
	Field  Field
}

// Sample returns the field's value at a position in canonical world space, in
// miles, in the range [-1, +1].
//
// It is a pure function of the tree and the position. Nothing here reads a
// coordinate, so nothing here can acquire a dependency on which tile is being
// generated or on what order the tiles are visited.
func (f *Field) Sample(x, y float64) float64 {
	switch f.Kind {
	case FieldValue:
		return valueNoise(f.Seed, f.Domain, x/f.WavelengthMiles, y/f.WavelengthMiles)

	case FieldSimplex:
		return simplexNoise(f.Seed, f.Domain, x/f.WavelengthMiles, y/f.WavelengthMiles)

	case FieldFBM:
		return f.sampleFBM(x, y)

	case FieldWarp:
		dx := mathx.Mul(f.WarpX.Sample(x, y), f.StrengthMiles)
		dy := mathx.Mul(f.WarpY.Sample(x, y), f.StrengthMiles)
		return f.Source.Sample(x+dx, y+dy)

	case FieldOffset:
		return f.Source.Sample(x+f.DXMiles, y+f.DYMiles)

	case FieldSum:
		return f.sampleSum(x, y)

	default:
		// Unreachable for a tree that passed Validate, which is every tree
		// DeriveFields produces and every tree New accepts. A zero here would
		// be a silently flat field, so it panics.
		panic(fmt.Sprintf("wgva: field kind %d is not a declared kind", uint8(f.Kind)))
	}
}

// sampleFBM accumulates the octave ladder coarse to fine, always in that order.
// Floating-point addition is not associative, so the order is part of the
// algorithm rather than an implementation detail; see DESIGN.md 25.3.
//
// The sum is divided by the sum of the amplitudes, which is what keeps the
// result inside [-1, +1] whatever the gain is.
func (f *Field) sampleFBM(x, y float64) float64 {
	var sum, norm float64
	frequency, amplitude := 1.0, 1.0
	for range f.Octaves {
		v := f.Source.Sample(mathx.Mul(x, frequency), mathx.Mul(y, frequency))
		sum += mathx.Mul(amplitude, v)
		norm += amplitude
		frequency = mathx.Mul(frequency, f.Lacunarity)
		amplitude = mathx.Mul(amplitude, f.Gain)
	}
	return sum / norm
}

// sampleSum returns the weighted average of the terms, in fixed slice order.
//
// It is an average rather than a plain sum so that the [-1, +1] range claim
// holds for every kind. A composition that wants an unnormalized total belongs
// to the phase that defines the composite, where the bound can be argued from
// the terms rather than assumed.
func (f *Field) sampleSum(x, y float64) float64 {
	var total, norm float64
	for i := range f.Terms {
		t := &f.Terms[i]
		total += mathx.Mul(t.Weight, t.Field.Sample(x, y))
		norm += math.Abs(t.Weight)
	}
	if norm == 0 {
		return 0
	}
	return total / norm
}

// Validate reports the first reason the tree cannot be evaluated, as a
// *ConfigError naming the path to the offending node.
//
// It rejects an undeclared kind, a missing child, an out-of-range octave count,
// a non-positive or sub-Nyquist wavelength, and — the rule DESIGN.md 9.1 asks
// for — a node carrying values that belong to some other kind.
func (f *Field) Validate() error { return f.validate("field") }

func (f *Field) validate(path string) error {
	if !f.Kind.Valid() {
		return &ConfigError{Field: path + ".Kind", Value: float64(f.Kind), Err: ErrOutOfRange}
	}
	if err := f.validateShape(path); err != nil {
		return err
	}

	switch f.Kind {
	case FieldValue, FieldSimplex:
		return checkWavelength(path+".WavelengthMiles", f.WavelengthMiles)

	case FieldFBM:
		if f.Octaves < 1 || f.Octaves > MaxOctaves {
			return &ConfigError{
				Field: path + ".Octaves", Value: float64(f.Octaves),
				Lo: 1, Hi: float64(MaxOctaves), Err: ErrOctaveCount,
			}
		}
		if err := checkLacunarity(path+".Lacunarity", f.Lacunarity); err != nil {
			return err
		}
		if err := checkGain(path+".Gain", f.Gain); err != nil {
			return err
		}
		if err := f.Source.validate(path + ".Source"); err != nil {
			return err
		}
		// The ladder's shortest octave is what the Nyquist rule bounds, and the
		// leaf below carries the wavelength it descends from.
		return checkLadder(path, f.Source.baseWavelengthMiles(), f.Octaves, f.Lacunarity)

	case FieldWarp:
		if !isFinite(f.StrengthMiles) {
			return &ConfigError{Field: path + ".StrengthMiles", Value: f.StrengthMiles, Err: ErrNotFinite}
		}
		if f.StrengthMiles < 0 {
			return &ConfigError{Field: path + ".StrengthMiles", Value: f.StrengthMiles, Err: ErrNotPositive}
		}
		if err := f.WarpX.validate(path + ".WarpX"); err != nil {
			return err
		}
		if err := f.WarpY.validate(path + ".WarpY"); err != nil {
			return err
		}
		return f.Source.validate(path + ".Source")

	case FieldOffset:
		if !isFinite(f.DXMiles) {
			return &ConfigError{Field: path + ".DXMiles", Value: f.DXMiles, Err: ErrNotFinite}
		}
		if !isFinite(f.DYMiles) {
			return &ConfigError{Field: path + ".DYMiles", Value: f.DYMiles, Err: ErrNotFinite}
		}
		return f.Source.validate(path + ".Source")

	case FieldSum:
		for i := range f.Terms {
			term := path + fmt.Sprintf(".Terms[%d]", i)
			if !isFinite(f.Terms[i].Weight) {
				return &ConfigError{Field: term + ".Weight", Value: f.Terms[i].Weight, Err: ErrNotFinite}
			}
			if err := f.Terms[i].Field.validate(term); err != nil {
				return err
			}
		}
		return nil

	default:
		panic("unreachable: kind was checked above")
	}
}

// validateShape rejects a node carrying values set for another kind.
//
// This is the check DESIGN.md 9.1 asks for, and it earns its keep: the tagged
// struct's cost is that every field is addressable on every node, so an fbm node
// with a stray WavelengthMiles reads as configured and evaluates as if it were
// not. Naming the offending field is the whole point — ErrFieldShape with no
// field name would send a reader to the wrong node.
func (f *Field) validateShape(path string) error {
	shape := func(name string, set bool) error {
		if set {
			return &ConfigError{Field: path + "." + name, Err: ErrFieldShape}
		}
		return nil
	}
	leaf := f.Kind == FieldValue || f.Kind == FieldSimplex

	checks := []struct {
		name string
		set  bool
	}{
		{"Seed", !leaf && f.Seed != 0},
		{"Domain", !leaf && f.Domain != 0},
		{"WavelengthMiles", !leaf && f.WavelengthMiles != 0},
		{"Octaves", f.Kind != FieldFBM && f.Octaves != 0},
		{"Lacunarity", f.Kind != FieldFBM && f.Lacunarity != 0},
		{"Gain", f.Kind != FieldFBM && f.Gain != 0},
		{"StrengthMiles", f.Kind != FieldWarp && f.StrengthMiles != 0},
		{"WarpX", f.Kind != FieldWarp && f.WarpX != nil},
		{"WarpY", f.Kind != FieldWarp && f.WarpY != nil},
		{"DXMiles", f.Kind != FieldOffset && f.DXMiles != 0},
		{"DYMiles", f.Kind != FieldOffset && f.DYMiles != 0},
		{"Terms", f.Kind != FieldSum && f.Terms != nil},
	}
	for _, c := range checks {
		if err := shape(c.name, c.set); err != nil {
			return err
		}
	}

	// The children a kind does need must be present. A nil child is a nil
	// dereference at the first sample rather than a flat field, so it is caught
	// here where it can name itself.
	needsSource := f.Kind == FieldFBM || f.Kind == FieldWarp || f.Kind == FieldOffset
	switch {
	case needsSource && f.Source == nil:
		return &ConfigError{Field: path + ".Source", Err: ErrFieldShape}
	case !needsSource && f.Source != nil:
		return &ConfigError{Field: path + ".Source", Err: ErrFieldShape}
	case f.Kind == FieldWarp && (f.WarpX == nil || f.WarpY == nil):
		return &ConfigError{Field: path + ".WarpX", Err: ErrFieldShape}
	}
	return nil
}

// baseWavelengthMiles is the wavelength of the leaf this node descends to, or
// zero if there is no single one. It exists so that an fbm node can state the
// ladder it is about to run without the caller having to walk the tree.
func (f *Field) baseWavelengthMiles() float64 {
	switch f.Kind {
	case FieldValue, FieldSimplex:
		return f.WavelengthMiles
	case FieldFBM, FieldWarp, FieldOffset:
		return f.Source.baseWavelengthMiles()
	default:
		return 0
	}
}

// Describe renders the tree as indented text, one node per line. The terrain
// tuning tool prints it beside a window so that what was evaluated can be read
// off the page rather than reconstructed from the configuration.
func (f *Field) Describe() string {
	var b strings.Builder
	f.describe(&b, 0, "")
	return b.String()
}

func (f *Field) describe(b *strings.Builder, depth int, label string) {
	b.WriteString(strings.Repeat("  ", depth))
	if label != "" {
		b.WriteString(label)
		b.WriteString(": ")
	}
	b.WriteString(f.Kind.String())

	switch f.Kind {
	case FieldValue, FieldSimplex:
		fmt.Fprintf(b, " wavelength=%g miles domain=%#016x", f.WavelengthMiles, f.Domain)
	case FieldFBM:
		fmt.Fprintf(b, " octaves=%d lacunarity=%g gain=%g", f.Octaves, f.Lacunarity, f.Gain)
	case FieldWarp:
		fmt.Fprintf(b, " strength=%g miles", f.StrengthMiles)
	case FieldOffset:
		fmt.Fprintf(b, " dx=%g dy=%g miles", f.DXMiles, f.DYMiles)
	case FieldSum:
		fmt.Fprintf(b, " terms=%d", len(f.Terms))
	}
	b.WriteByte('\n')

	switch f.Kind {
	case FieldWarp:
		f.WarpX.describe(b, depth+1, "warp-x")
		f.WarpY.describe(b, depth+1, "warp-y")
		f.Source.describe(b, depth+1, "source")
	case FieldFBM, FieldOffset:
		f.Source.describe(b, depth+1, "source")
	case FieldSum:
		for i := range f.Terms {
			f.Terms[i].Field.describe(b, depth+1, fmt.Sprintf("weight=%g", f.Terms[i].Weight))
		}
	}
}

// ---------------------------------------------------------------------------
// The seed-derived sampling offset
// ---------------------------------------------------------------------------

// The sampling offset is confined to this fraction of a cell, away from both
// corners. See samplingOffset.
const (
	samplingOffsetLo = 0.25
	samplingOffsetHi = 0.75
)

// samplingOffset returns the displacement of the sampling position for one
// field, in miles. See DESIGN.md 9.4.
//
// Without it the world origin is a lattice point of every scale at once, every
// field is exactly zero there, and — because gradient noise has its steepest
// slope at a lattice point — the region around the origin is measurably steeper
// than the rest of the world. That is a permanent anomaly at the one coordinate
// every worked example uses and every diagnostic render defaults to.
//
// Four properties are what make this a fix rather than a relocation:
//
//   - It is derived from the seed, so two worlds do not share the anomaly's new
//     location.
//   - It is derived per domain, so the scales do not all land on their own
//     lattice points at some other single coordinate.
//   - It is applied above the fbm. The fbm scales the position by the octave
//     frequency before the leaf divides by the wavelength, so an offset folded
//     into the leaf would be the same fraction of a cell at every octave: at the
//     origin every octave would sample the same cell with the same gradients and
//     their slopes would add. That is the same defect with a smaller
//     coefficient.
//   - It is scaled by the wavelength, which makes it an odd number of miles that
//     no other field's wavelength divides, and it is confined to the middle half
//     of a cell. A hash is free to come back near zero, and a fix that works for
//     most seeds is not a fix.
//
// Only the top octave is pinned by that confinement; octave k sees the offset
// multiplied by the frequency, and the fractional part of that is effectively
// free. That is the intent — the rule has to hold where the ladder starts,
// because that is where every octave coincides.
func samplingOffset(seed Seed, dom uint64, wavelengthMiles float64) (dx, dy float64) {
	const span = samplingOffsetHi - samplingOffsetLo
	fx := samplingOffsetLo + mathx.Mul(span, toUnit(Hash2(uint64(seed), DomFieldOffset, int64(dom), 0)))
	fy := samplingOffsetLo + mathx.Mul(span, toUnit(Hash2(uint64(seed), DomFieldOffset, int64(dom), 1)))
	return mathx.Mul(fx, wavelengthMiles), mathx.Mul(fy, wavelengthMiles)
}

// ---------------------------------------------------------------------------
// Deriving the tree from the configuration
// ---------------------------------------------------------------------------

// fbmField returns the standard leaf shape: an offset sampling position above an
// fbm ladder above a simplex leaf.
//
// Every continuous scale in the generator is built from this, so the rule of
// DESIGN.md 9.4 is honored in one place rather than at each call site.
func fbmField(seed Seed, dom uint64, l LadderConfig) Field {
	leaf := &Field{
		Kind:            FieldSimplex,
		Seed:            seed,
		Domain:          dom,
		WavelengthMiles: l.WavelengthMiles,
	}
	ladder := &Field{
		Kind:       FieldFBM,
		Octaves:    l.Octaves,
		Lacunarity: l.Lacunarity,
		Gain:       l.Gain,
		Source:     leaf,
	}
	dx, dy := samplingOffset(seed, dom, l.WavelengthMiles)
	return Field{Kind: FieldOffset, DXMiles: dx, DYMiles: dy, Source: ladder}
}

// warped wraps a field in the shared domain warp of DESIGN.md 13.
//
// The warp sits above the offset, so the two warp fields are evaluated at the
// unwarped position and each carries its own sampling offset underneath. A
// strength of zero makes the node an identity, which is why a disabled warp is a
// legitimate configuration rather than a missing one.
func warped(seed Seed, cfg Config, source Field) Field {
	warpX := fbmField(seed, DomWarpX, cfg.Warp)
	warpY := fbmField(seed, DomWarpY, cfg.Warp)
	return Field{
		Kind:          FieldWarp,
		StrengthMiles: cfg.WarpStrengthMiles,
		WarpX:         &warpX,
		WarpY:         &warpY,
		Source:        &source,
	}
}

// Scale names one of the continuous scales of DESIGN.md 10.
//
// The four are the raw noise that the elevation composite is built from, and
// they exist as a public enumeration because the diagnostic layers draw them
// one at a time: when a window looks wrong, that is what separates "the noise is
// wrong" from "the composition is wrong". Nothing in the generator reads a
// Scale to decide anything; it is an index into the derived trees.
//
// These values name a layer in a URL and are written explicitly rather than
// taken from iota, for the reason every other enumeration here is.
type Scale uint8

// The continuous scales, coarse to fine.
const (
	ScaleContinental Scale = 1
	ScaleRegional    Scale = 2
	ScaleLocal       Scale = 3
	ScaleDetail      Scale = 4
)

// scaleCount is how many scales there are. It is the highest declared value,
// and TestScalesAreContiguous is what keeps that true.
const scaleCount = 4

// String returns the name of the scale.
func (s Scale) String() string {
	switch s {
	case ScaleContinental:
		return "continentalness"
	case ScaleRegional:
		return "regional"
	case ScaleLocal:
		return "local"
	case ScaleDetail:
		return "detail"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared scale.
func (s Scale) Valid() bool {
	switch s {
	case ScaleContinental, ScaleRegional, ScaleLocal, ScaleDetail:
		return true
	default:
		return false
	}
}

// Scales returns the declared scales, coarse to fine. The order is fixed: it is
// what a diagnostic sheet is laid out in and what a test iterates.
func Scales() []Scale {
	return []Scale{ScaleContinental, ScaleRegional, ScaleLocal, ScaleDetail}
}

// DeriveFields returns the field tree for one scale, derived from the seed and
// the configuration by a pure function.
//
// The tree is derived and never stored as a second source of truth: the flat
// Config is what the fingerprint covers and what a person edits. See
// DESIGN.md 9.1.
func DeriveFields(seed Seed, cfg Config, s Scale) Field {
	ladder, dom := cfg.ladderFor(s)
	return warped(seed, cfg, fbmField(seed, dom, ladder))
}

// DeriveRidgeField returns the field tree the ridge structure term of
// DESIGN.md 10 is folded from, derived from the seed and the configuration by a
// pure function.
//
// It is the same shape as a continuous scale — the seed-derived offset above an
// fbm ladder above a simplex leaf, under the shared domain warp — because the
// thing that makes it a ridge is the fold and the directional blur above it,
// not the field underneath. Its own hashing domain is what keeps its lattice
// clear of the four scales'; see DESIGN.md 8.1.
func DeriveRidgeField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomRidgeStructure, cfg.Elevation.Ridge))
}
