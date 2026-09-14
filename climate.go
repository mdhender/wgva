// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"github.com/mdhender/wgva/internal/mathx"
)

// ---------------------------------------------------------------------------
// The classification
// ---------------------------------------------------------------------------

// HeatBand is the game-facing temperature band a tile falls in.
//
// It is one of two independent axes. A single type mixing cold, arid, and humid
// would state that those properties exclude one another, and they do not: a
// polar desert and a polar rainforest are both ordinary places. See
// DESIGN.md 16.1.
type HeatBand uint8

// The heat bands, cold to hot.
//
// These values are persisted and appear in cached tiles, so each is written out
// explicitly rather than taken from iota: inserting a band into the middle of an
// iota block would silently renumber everything after it and change the meaning
// of stored data, with nothing in the diff that looks like a data change.
const (
	HeatPolar     HeatBand = 0
	HeatCold      HeatBand = 1
	HeatTemperate HeatBand = 2
	HeatWarm      HeatBand = 3
	HeatHot       HeatBand = 4
)

// Heats returns the declared heat bands, cold to hot. The order is fixed: it is
// what a histogram is laid out in and what a test iterates.
func Heats() []HeatBand {
	return []HeatBand{HeatPolar, HeatCold, HeatTemperate, HeatWarm, HeatHot}
}

// String returns the name of the band.
func (h HeatBand) String() string {
	switch h {
	case HeatPolar:
		return "polar"
	case HeatCold:
		return "cold"
	case HeatTemperate:
		return "temperate"
	case HeatWarm:
		return "warm"
	case HeatHot:
		return "hot"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared band.
func (h HeatBand) Valid() bool {
	switch h {
	case HeatPolar, HeatCold, HeatTemperate, HeatWarm, HeatHot:
		return true
	default:
		return false
	}
}

// MoistureBand is the game-facing moisture band a tile falls in. It is the
// other of the two independent axes; see HeatBand.
type MoistureBand uint8

// The moisture bands, dry to wet. Explicit values, for the reason HeatBand's
// are.
const (
	MoistureArid      MoistureBand = 0
	MoistureDry       MoistureBand = 1
	MoistureModerate  MoistureBand = 2
	MoistureHumid     MoistureBand = 3
	MoistureSaturated MoistureBand = 4
)

// Moistures returns the declared moisture bands, dry to wet. The order is
// fixed, for the reason Heats' is.
func Moistures() []MoistureBand {
	return []MoistureBand{MoistureArid, MoistureDry, MoistureModerate, MoistureHumid, MoistureSaturated}
}

// String returns the name of the band.
func (m MoistureBand) String() string {
	switch m {
	case MoistureArid:
		return "arid"
	case MoistureDry:
		return "dry"
	case MoistureModerate:
		return "moderate"
	case MoistureHumid:
		return "humid"
	case MoistureSaturated:
		return "saturated"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared band.
func (m MoistureBand) Valid() bool {
	switch m {
	case MoistureArid, MoistureDry, MoistureModerate, MoistureHumid, MoistureSaturated:
		return true
	default:
		return false
	}
}

// Climate is a tile's climate: two independent bands, never one combined value.
//
// The two-axis representation is part of the public model rather than an
// implementation choice, and the bands and their thresholds are what may be
// tuned under it. See DESIGN.md 16.1.
type Climate struct {
	Heat     HeatBand
	Moisture MoistureBand
}

// String renders the pair as "heat/moisture", which is how a diagnostic prints
// one and how a test names a cell of the two-axis table.
func (c Climate) String() string { return c.Heat.String() + "/" + c.Moisture.String() }

// Valid reports whether both axes are declared bands.
func (c Climate) Valid() bool { return c.Heat.Valid() && c.Moisture.Valid() }

// HeatBands are the thresholds that cut the heat scalar into bands.
//
// Each names the top of the band it closes, so the four of them cut the scalar's
// [-1, +1] into five. Unlike the elevation ladder none of these is fixed by
// construction: heat has no sea level, so the middle of the temperate band is
// wherever these say it is.
type HeatBands struct {
	Polar     float64
	Cold      float64
	Temperate float64
	Warm      float64
}

// Classify returns the band a heat scalar falls in.
//
// The comparisons run cold to hot and the default is the top band, which is the
// documented fallback DESIGN.md 14.1 asks for: a heat above every threshold is
// hot, and there is nothing else it could be. Go cannot make this total the way
// a sum type would, so the distribution test of DESIGN.md 30.8 is what asserts
// every band is actually reachable.
func (b HeatBands) Classify(h float64) HeatBand {
	switch {
	case h <= b.Polar:
		return HeatPolar
	case h <= b.Cold:
		return HeatCold
	case h <= b.Temperate:
		return HeatTemperate
	case h <= b.Warm:
		return HeatWarm
	default:
		return HeatHot
	}
}

// MoistureBands are the thresholds that cut the moisture scalar into bands.
// Each names the top of the band it closes; see HeatBands.
type MoistureBands struct {
	Arid     float64
	Dry      float64
	Moderate float64
	Humid    float64
}

// Classify returns the band a moisture scalar falls in. See HeatBands.Classify.
func (b MoistureBands) Classify(m float64) MoistureBand {
	switch {
	case m <= b.Arid:
		return MoistureArid
	case m <= b.Dry:
		return MoistureDry
	case m <= b.Moderate:
		return MoistureModerate
	case m <= b.Humid:
		return MoistureHumid
	default:
		return MoistureSaturated
	}
}

// ---------------------------------------------------------------------------
// The composite
// ---------------------------------------------------------------------------

// climateParts is one coordinate's climate composite taken apart.
//
// Like elevationParts it exists so that Sample can report what went into a
// value without evaluating the composite a second time. A diagnostic that
// recomputed its own version of the number would eventually disagree with the
// number, and the disagreement would be invisible.
type climateParts struct {
	// heatField and moistureField are the broad zone fields, each in [-1, +1]
	// and before anything is added to them.
	heatField     float64
	moistureField float64

	// variation is the local moisture variation term of DESIGN.md 16, in
	// [-1, +1] and before its weight.
	variation float64

	// cooling is what this tile's altitude took off its temperature. It is
	// non-negative: see climateAt.
	cooling float64

	// heat and moisture are the scalars: -1 polar and arid, 0 the middle of the
	// temperate and moderate bands, +1 hot and saturated.
	heat     float64
	moisture float64
}

// climateAt evaluates the climate composite of DESIGN.md 16 at one position.
//
// It takes the position, the region blend, and the elevation scalar rather than
// a coordinate, because all three have already been computed by the elevation
// composite and re-deriving them here would make a tile cost two evaluations of
// everything underneath it. Written out:
//
//	heatBase     = (wHF*heatField + wHB*regionHeatBias) / (wHF + wHB)
//	cooling      = elevationCooling * max(elevation, 0)
//	heat         = clamp(contrast(heatBase, heatPasses) - cooling)
//
//	moistureBase = (wMF*moistureField + wMB*regionMoistureBias + wMV*variation) / (wMF + wMB + wMV)
//	moisture     = clamp(contrast(moistureBase, moisturePasses))
//
// Four things about that are deliberate:
//
//   - **There is no latitude.** The wrapped world has no equator, and r == 0 is
//     an addressing origin rather than a place. The broad zones are a field of
//     their own instead, which is what DESIGN.md 16 asks the first version to
//     do. Nothing here reads a coordinate component, so nothing here can come to
//     believe one of them means north.
//   - Each base is a weighted average, so it lands in [-1, +1] whatever the
//     weights are, and the contrast pass is what decides how much of that range
//     is actually used. Without it a sum of fields that are each concentrated
//     about zero puts most of the world in the middle band, which is a climate
//     map of one color.
//   - **Cooling reads max(elevation, 0), not elevation.** Below sea level the
//     scalar is depth rather than altitude, and depth has no lapse rate: a
//     linear term would warm the deep ocean in proportion to how deep it is.
//     The ocean surface is at sea level however far the floor is beneath it.
//   - Cooling is subtracted after the contrast pass rather than before. The
//     contrast pass shapes the zones; the lapse rate is a physical offset on top
//     of whatever zone a tile is in, and running it back through the S-curve
//     would make a mountain's temperature depend on which zone it stood in
//     twice over.
//
// Basin influence is deliberately absent. DESIGN.md 17.1 puts it in a product
// with moisture inside *terrain*, which means it multiplies what this returns
// rather than joining it — and it never enters elevation at all.
func (g *Generator) climateAt(p Vec2, region RegionParams, elevation float64) climateParts {
	cc := &g.cfg.Climate

	var parts climateParts
	parts.heatField = g.heat.Sample(p.X, p.Y)
	parts.moistureField = g.moisture.Sample(p.X, p.Y)
	parts.variation = g.moistureVariation.Sample(p.X, p.Y)

	heatNorm := cc.HeatFieldWeight + cc.HeatBiasWeight
	heatBase := (mathx.Mul(cc.HeatFieldWeight, parts.heatField) +
		mathx.Mul(cc.HeatBiasWeight, region.HeatBias)) / heatNorm
	parts.cooling = mathx.Mul(cc.ElevationCooling, max(elevation, 0))
	parts.heat = clampUnitSigned(contrast(heatBase, cc.HeatContrastPasses) - parts.cooling)

	moistureNorm := cc.MoistureFieldWeight + cc.MoistureBiasWeight + cc.MoistureVariationWeight
	moistureBase := (mathx.Mul(cc.MoistureFieldWeight, parts.moistureField) +
		mathx.Mul(cc.MoistureBiasWeight, region.MoistureBias) +
		mathx.Mul(cc.MoistureVariationWeight, parts.variation)) / moistureNorm
	parts.moisture = clampUnitSigned(contrast(moistureBase, cc.MoistureContrastPasses))

	return parts
}

// climateScalars evaluates the climate composite at a coordinate, together with
// the elevation composite it reads.
//
// It is the one place that pairs the two, so a caller that wants either scalar
// pays for exactly one evaluation of everything underneath both. Like
// elevationScalar it is unexported and nothing above it calls anything above
// it; see DESIGN.md 18 on why that structure is load-bearing.
func (g *Generator) climateScalars(c Coord) climateParts {
	e := g.elevationAt(c)
	return g.climateAt(e.pos, e.region, e.elevation)
}

// HeatAt returns the heat scalar at a coordinate: -1 polar, 0 the middle of the
// temperate band, +1 hot.
//
// It is a pure function of the seed, the coordinate, the algorithm version, the
// world radius, and the configuration.
func (g *Generator) HeatAt(c Coord) float64 { return g.climateScalars(c).heat }

// MoistureAt returns the moisture scalar at a coordinate: -1 arid, 0 the middle
// of the moderate band, +1 saturated. See HeatAt.
func (g *Generator) MoistureAt(c Coord) float64 { return g.climateScalars(c).moisture }

// ClimateAt returns the two-axis climate classification at a coordinate.
//
// Both bands come from one evaluation, which is the other reason the two axes
// are classified together even though they are independent: they share the
// elevation composite underneath them.
func (g *Generator) ClimateAt(c Coord) Climate {
	parts := g.climateScalars(c)
	return g.cfg.Climate.classify(parts)
}

// classify cuts a climate composite into its two bands.
func (cc ClimateConfig) classify(parts climateParts) Climate {
	return Climate{
		Heat:     cc.HeatBands.Classify(parts.heat),
		Moisture: cc.MoistureBands.Classify(parts.moisture),
	}
}

// ---------------------------------------------------------------------------
// Deriving the fields
// ---------------------------------------------------------------------------

// DeriveHeatField returns the broad heat zone field, derived from the seed and
// the configuration by a pure function.
//
// It is the same shape as a continuous scale — the seed-derived offset above an
// fbm ladder above a simplex leaf, under the shared domain warp — because what
// makes it a climate field is its wavelength and what reads it, not anything
// about how it is built. Its own hashing domain keeps its lattice clear of
// every other field's; see DESIGN.md 8.1.
func DeriveHeatField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomTemperature, cfg.Climate.Heat))
}

// DeriveMoistureField returns the broad moisture field. See DeriveHeatField.
func DeriveMoistureField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomMoisture, cfg.Climate.Moisture))
}

// DeriveMoistureVariationField returns the local moisture variation field of
// DESIGN.md 16.
//
// It has its own domain rather than sharing the broad moisture field's, which is
// the rule of DESIGN.md 8.1 in the case that rule was written for: two nodes
// under one domain would share a gradient table and a sampling offset, so at the
// world origin they would land in the same lattice cell at the same fractional
// position and the variation term would be a scaled copy of the field it is
// supposed to vary.
func DeriveMoistureVariationField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomMoistureVariation, cfg.Climate.MoistureVariation))
}

// HeatField, MoistureField, and MoistureVariationField return the derived field
// trees the climate composite reads, so the tuning tool can print what was
// evaluated beside the window it drew. Like ScaleField they are not a second
// source of truth.
func (g *Generator) HeatField() Field { return g.heat }

// MoistureField returns the broad moisture field. See HeatField.
func (g *Generator) MoistureField() Field { return g.moisture }

// MoistureVariationField returns the local moisture variation field. See
// HeatField.
func (g *Generator) MoistureVariationField() Field { return g.moistureVariation }
