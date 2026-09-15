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

	// The three fields the climate composite of DESIGN.md 16 reads: the broad
	// heat zones, the broad moisture field, and the local moisture variation.
	// Each has its own hashing domain, so no two of them share a lattice.
	heat              Field
	moisture          Field
	moistureVariation Field

	// The three scales of basin influence of DESIGN.md 17, coarse to fine, and
	// the volcanic tendency. Each has its own hashing domain, for the reason
	// the climate fields do.
	//
	// Nothing above elevation or climate reads any of them. They are here
	// because terrain reads all four, and terrain is the one classification
	// that reads every field in the module at once.
	basinBroad    Field
	basinRegional Field
	basinLocal    Field
	volcanic      Field
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
	g.heat = DeriveHeatField(seed, cfg)
	g.moisture = DeriveMoistureField(seed, cfg)
	g.moistureVariation = DeriveMoistureVariationField(seed, cfg)
	g.basinBroad = DeriveBasinBroadField(seed, cfg)
	g.basinRegional = DeriveBasinRegionalField(seed, cfg)
	g.basinLocal = DeriveBasinLocalField(seed, cfg)
	g.volcanic = DeriveVolcanicField(seed, cfg)
	for _, f := range []*Field{
		&g.ridge, &g.heat, &g.moisture, &g.moistureVariation,
		&g.basinBroad, &g.basinRegional, &g.basinLocal, &g.volcanic,
	} {
		if err := f.Validate(); err != nil {
			return nil, err
		}
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

	// Heat and Moisture are the two climate scalars of DESIGN.md 16: -1 polar
	// and arid, 0 the middle of the temperate and moderate bands, +1 hot and
	// saturated. Each is bit-identical to HeatAt and MoistureAt, which a test
	// asserts.
	//
	// They are two values rather than one because the axes are independent, and
	// the reason they are computed together is that both read the elevation
	// composite above them. See DESIGN.md 16.1.
	Heat     float64
	Moisture float64

	// Climate is the two-axis classification of those scalars.
	Climate Climate

	// BasinInfluence is the blended basin composite of DESIGN.md 17: -1 a rise
	// that sheds water, 0 neutral ground, +1 a closed hollow.
	//
	// It is reported here and it is not in a Tile, which is the shape rather
	// than an omission. A tile carries what a game reads, and what a game reads
	// of a basin is the terrain the basin produced; this is the quantity behind
	// it, for the layer that draws basin geography and for the coordinate
	// somebody is inspecting because that terrain looked wrong.
	BasinInfluence float64

	// Volcanic is the volcanic tendency, in [-1, +1]. Like BasinInfluence it is
	// a field terrain reads rather than a value a tile carries.
	Volcanic float64

	// RimDistance is hexes from the outer edge of the map: 0 on the outermost
	// ring, positive inside. See DESIGN.md 15.1.
	RimDistance int64

	// Rim reports that the coordinate is inside the closed band, and RimProfile
	// is what the generated world is worth here: 0 inside that band, rising
	// smoothly across the falloff, and exactly 1 everywhere inside both.
	//
	// The profile is reported beside the elevation deliberately. It is the one
	// term of the composite that is a function of the coordinate rather than of
	// a field, so a tile whose elevation looks wrong near the edge of the map
	// has this to be read against rather than inferred from a picture.
	Rim        bool
	RimProfile float64
}

// Sample returns the diagnostic decomposition of one coordinate.
//
// It is bit-identical to calling ScaleAt for each scale, which a test asserts:
// a convenience that computed something slightly different from the thing it is
// a convenience for would be worse than not having it.
func (g *Generator) Sample(c Coord) Sample {
	// One evaluation of each composite at this coordinate, taken apart. A
	// Sample does not carry a Tile and does not call Relief, and that is the
	// shape rather than an omission: everything here is a function of this
	// coordinate alone, while relief reads the six neighbors and costs seven.
	// That is the distinction render.Layer.Cost is built on. DESIGN.md 4.3.
	parts := g.elevationAt(c)
	climate := g.climateAt(parts.pos, parts.region, parts.elevation)
	basin := g.basinAt(parts.pos, parts.region)

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
		Heat:            climate.heat,
		Moisture:        climate.moisture,
		Climate:         g.cfg.Climate.classify(climate),
		BasinInfluence:  basin.basin,
		Volcanic:        g.volcanicAt(parts.pos, parts.region),
		RimDistance:     parts.rimDistance,
		Rim:             parts.rim,
		RimProfile:      g.cfg.Rim.profile(parts.rimDistance),
	}
}
