// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import "fmt"

// Generator holds an immutable seed and configuration and is safe for
// concurrent read-only use.
//
// Go cannot check that at compile time, which is a real loss, so the shape is
// maintained by hand and asserted by a test: no mutex, no channel, no map, and
// no captured-state function value may appear in this struct, and any cache
// belongs outside it. See DESIGN.md 22 and 30.12.
//
// The generation methods land in the phases that define what they compute. What
// exists here is what every one of them will be a function of.
type Generator struct {
	seed Seed
	cfg  Config

	// scales holds the derived field tree for each Scale, indexed by Scale-1.
	// It is derived from seed and cfg by a pure function at construction and is
	// never written again, which is what keeps the struct safe to read from any
	// number of goroutines without a lock.
	scales [scaleCount]Field

	// ridge is the derived field the ridge structure term of DESIGN.md 10 is
	// folded from. It is separate from scales because it is not one of the four
	// continuous scales a diagnostic layer draws raw: what a window shows is
	// the fold, which is what elevation reads.
	ridge Field
}

// New returns a generator for the seed and configuration, or the first reason
// the configuration cannot be used.
func New(seed Seed, cfg Config) (*Generator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	g := &Generator{seed: seed, cfg: cfg}
	for _, s := range Scales() {
		g.scales[s-1] = DeriveFields(seed, cfg, s)
		// A validated Config cannot derive an invalid tree, so this is an
		// assertion about DeriveFields rather than about the caller's input. It
		// is checked anyway: the alternative to failing here is a panic deep in
		// Sample on some tile a long way from the mistake.
		if err := g.scales[s-1].Validate(); err != nil {
			return nil, err
		}
	}

	g.ridge = DeriveRidgeField(seed, cfg)
	if err := g.ridge.Validate(); err != nil {
		return nil, err
	}

	return g, nil
}

// NewDefault returns a generator for the seed and the built-in defaults. It
// cannot fail, and it is the ergonomic path for tests and the terrain tuning
// tool.
func NewDefault(seed Seed) *Generator {
	g, err := New(seed, DefaultConfig())
	if err != nil {
		// DefaultConfig is checked by TestDefaultConfigValidates, so reaching
		// this means the defaults and the validation have been changed apart.
		panic(fmt.Sprintf("wgva: default configuration is invalid: %v", err))
	}
	return g
}

// Seed returns the world seed.
func (g *Generator) Seed() Seed { return g.seed }

// Config returns the effective configuration.
//
// It returns a value rather than a pointer, and that states the intent: the
// configuration is immutable after construction. Config will contain a slice or
// two as the later phases land, so the copy is shallow and a caller could in
// principle reach into it; returning a pointer would state the opposite.
func (g *Generator) Config() Config { return g.cfg }

// ScaleAt returns one continuous scale's value at a coordinate, in [-1, +1].
//
// It panics on an undeclared Scale. That is a programming error rather than bad
// data — a Scale arrives from a constant or from a parsed layer name that was
// already checked — and returning zero would be a flat field nobody notices.
func (g *Generator) ScaleAt(s Scale, c Coord) float64 {
	if !s.Valid() {
		panic(fmt.Sprintf("wgva: %d is not a declared scale", uint8(s)))
	}
	p := AxialToWorld(c)
	return g.scales[s-1].Sample(p.X, p.Y)
}

// ScaleField returns the derived field tree for one scale.
//
// The tree is derived from the configuration and is not a second source of
// truth; this exists so that the terrain tuning tool can print what was actually
// evaluated beside the window it drew. Field.Describe renders it.
func (g *Generator) ScaleField(s Scale) Field {
	if !s.Valid() {
		panic(fmt.Sprintf("wgva: %d is not a declared scale", uint8(s)))
	}
	return g.scales[s-1]
}

// Sample is the diagnostic decomposition of one coordinate: every continuous
// scale that went into it, and where it sits relative to the rim.
//
// It gains fields as the later phases land. Nothing in the generator reads a
// Sample — it exists so that a coordinate can be inspected rather than inferred
// from a picture.
type Sample struct {
	Coord Coord

	// The four continuous scales of DESIGN.md 10, each in [-1, +1].
	Continentalness float64
	Regional        float64
	Local           float64
	Detail          float64

	// Region is the blended region influence at the coordinate: every anchor
	// parameter of DESIGN.md 12, carried so that a tuning layer can be drawn
	// and so that a coordinate's regional character can be read rather than
	// inferred from a picture.
	//
	// It is the whole blend rather than the two entries DESIGN.md 4.3 names.
	// Regional uplift and the roughness the ridge term is scaled by are what
	// elevation makes of two of these biases, and neither exists until it does.
	Region RegionParams

	// ElevationRaw is the weighted sum of the four continuous scales alone,
	// before the contrast pass, the regional uplift, and the ridges. It is what
	// separates "the composition is wrong" from "one of the later terms is".
	ElevationRaw float64

	// RegionalUplift is what the region's elevation bias is worth here, in
	// elevation. Region is the bias; this is the quantity.
	RegionalUplift float64

	// Ridge is the ridge structure term, before the region roughness scales it
	// and before the land mask confines it to land. DESIGN.md 4.3.
	Ridge float64

	// Elevation is the elevation scalar: -1 deep ocean, 0 sea level, +1 extreme
	// highland. It is bit-identical to ElevationAt, which a test asserts.
	Elevation float64

	// Band is the elevation classification of that scalar.
	Band Elevation

	// RimDistance is hexes from the outer edge of the map. See DESIGN.md 15.1.
	RimDistance int64
}

// Sample returns the diagnostic decomposition of one coordinate.
//
// It is bit-identical to calling ScaleAt for each scale, which a test asserts:
// a convenience that computed something slightly different from the thing it is
// a convenience for would be worse than not having it.
func (g *Generator) Sample(c Coord) Sample {
	// One evaluation of the composite, taken apart. A Sample does not carry a
	// Tile and does not call Relief: a sample is one elevation evaluation and
	// relief is seven, which is the distinction render.Layer.Cost is built on.
	// DESIGN.md 4.3.
	parts := g.elevationAt(c)

	return Sample{
		Coord:           c,
		Continentalness: parts.scales[ScaleContinental-1],
		Regional:        parts.scales[ScaleRegional-1],
		Local:           parts.scales[ScaleLocal-1],
		Detail:          parts.scales[ScaleDetail-1],
		Region:          parts.region,
		ElevationRaw:    parts.raw,
		RegionalUplift:  parts.uplift,
		Ridge:           parts.ridge,
		Elevation:       parts.elevation,
		Band:            g.cfg.Elevation.Bands.Classify(parts.elevation),
		RimDistance:     c.RimDistance(),
	}
}
