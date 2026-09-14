// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"

	"github.com/mdhender/wgva/internal/mathx"
)

// ---------------------------------------------------------------------------
// The classification
// ---------------------------------------------------------------------------

// Elevation is the game-facing elevation band a tile falls in.
//
// The bands are cut from the elevation scalar by the thresholds in
// ElevationBands, and the two water bands are the two below sea level: the
// scalar puts sea level at exactly zero, so "elevation <= 0 is water" is a fact
// about the scale rather than a threshold somebody can move. See DESIGN.md 14.1
// and 15.
type Elevation uint8

// The elevation bands, low to high.
//
// These values are persisted and appear in cached tiles, so each is written out
// explicitly rather than taken from iota: inserting a band into the middle of an
// iota block would silently renumber everything after it and change the meaning
// of stored data, with nothing in the diff that looks like a data change.
const (
	ElevationDeepWater    Elevation = 0
	ElevationShallowWater Elevation = 1
	ElevationLowland      Elevation = 2
	ElevationUpland       Elevation = 3
	ElevationHighland     Elevation = 4
	ElevationMountain     Elevation = 5
)

// Elevations returns the declared bands, low to high. The order is fixed: it is
// what a histogram is laid out in and what a test iterates.
func Elevations() []Elevation {
	return []Elevation{
		ElevationDeepWater,
		ElevationShallowWater,
		ElevationLowland,
		ElevationUpland,
		ElevationHighland,
		ElevationMountain,
	}
}

// String returns the name of the band.
func (e Elevation) String() string {
	switch e {
	case ElevationDeepWater:
		return "deep-water"
	case ElevationShallowWater:
		return "shallow-water"
	case ElevationLowland:
		return "lowland"
	case ElevationUpland:
		return "upland"
	case ElevationHighland:
		return "highland"
	case ElevationMountain:
		return "mountain"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared band.
func (e Elevation) Valid() bool {
	switch e {
	case ElevationDeepWater, ElevationShallowWater, ElevationLowland,
		ElevationUpland, ElevationHighland, ElevationMountain:
		return true
	default:
		return false
	}
}

// IsWater reports whether the band is ocean water.
//
// It is a property of the band rather than a comparison against the scalar so
// that a caller holding a classified tile does not have to remember which two
// bands are wet. Inland water is not one of these: DESIGN.md 17.1 omits it, and
// a lake is a terrain rather than an elevation band.
func (e Elevation) IsWater() bool {
	return e == ElevationDeepWater || e == ElevationShallowWater
}

// ElevationBands are the thresholds that cut the elevation scalar into bands.
//
// Sea level is not among them. It is zero by construction — Config.SeaLevel
// decides where zero falls in the composite, not where the band boundary is —
// so the boundary between water and land cannot drift away from the definition
// in DESIGN.md 15 no matter how these are tuned.
type ElevationBands struct {
	// DeepWater is the elevation at or below which water is deep. It is
	// negative, because zero is sea level.
	DeepWater float64

	// Upland, Highland, and Mountain are where land rises through its bands.
	// Each is above sea level and above the one before it.
	Upland   float64
	Highland float64
	Mountain float64
}

// Classify returns the band an elevation scalar falls in.
//
// The comparisons run low to high and the default is the top band, which is the
// documented fallback DESIGN.md 14.1 asks for: an elevation above every
// threshold is a mountain, and there is nothing else it could be. Go cannot make
// this total the way a sum type would, so the distribution test of DESIGN.md
// 30.8 is what asserts every band is actually reachable.
func (b ElevationBands) Classify(e float64) Elevation {
	switch {
	case e <= b.DeepWater:
		return ElevationDeepWater
	case e <= 0:
		// DESIGN.md 15: elevation <= 0 is ocean water, > 0 is land. Sea level
		// itself is water.
		return ElevationShallowWater
	case e < b.Upland:
		return ElevationLowland
	case e < b.Highland:
		return ElevationUpland
	case e < b.Mountain:
		return ElevationHighland
	default:
		return ElevationMountain
	}
}

// ---------------------------------------------------------------------------
// The composite
// ---------------------------------------------------------------------------

// elevationParts is one coordinate's elevation composite taken apart.
//
// It exists so that Sample can report what went into a value without evaluating
// the composite a second time. A diagnostic that recomputed its own version of
// the number would eventually disagree with the number, and the disagreement
// would be invisible.
type elevationParts struct {
	// The four continuous scales at this coordinate, in the order Scales
	// returns them. They are carried out rather than re-sampled because Sample
	// reports them beside the composite, and DESIGN.md 4.3 says a Sample is one
	// elevation evaluation — which it is only if nothing above evaluates the
	// scales a second time to print them.
	scales [scaleCount]float64

	// region is the blended region influence, carried out for the same reason.
	region RegionParams

	// raw is the weighted sum of the four continuous scales alone, before the
	// contrast pass, the uplift, and the ridges. It is the `elevation-raw`
	// layer and nothing reads it.
	raw float64

	// uplift is what the region's elevation bias is worth here.
	uplift float64

	// ridge is the ridge structure term, before the region roughness scales it
	// and before the land mask confines it to land. DESIGN.md 4.3.
	ridge float64

	// elevation is the scalar: -1 deep ocean, 0 sea level, +1 extreme highland.
	elevation float64
}

// elevationAt evaluates the composite of DESIGN.md 10 at one coordinate.
//
// The order of the terms is the algorithm, not a detail. Written out:
//
//	coarse  = (wC*continentalness + wR*regional) / w
//	fine    = (wL*local + wD*detail) / w
//	shaped  = contrast(coarse)                      sharpens the coast
//	rough   = 1 + roughnessInfluence * regionRoughness
//	base    = shaped + rough*fine + upliftWeight*regionElevationBias
//	mask    = smoothstep(base / ridgeOnset)         zero at and below sea level
//	elev    = rescale(base + ridgeWeight*rough*ridge*mask)
//
// Four things about that are deliberate:
//
//   - The contrast pass is applied to the coarse half only. Its job is to pull
//     the continental scale away from sea level so a coastline is a line rather
//     than a wide band of near-zero noise; applied to the whole composite it
//     would flatten the hills as well, which is the opposite of what it is for.
//   - Region roughness scales the fine half and the ridges, and nothing else.
//     That is what makes it mean "relief here is exaggerated or subdued" rather
//     than "here is higher", which is what the elevation bias already means.
//   - The ridge term is multiplied by a land mask that is zero at and below sea
//     level, so mountain belts do not surface as islands in open ocean. The
//     mask rises over the first ridgeOnset of land, which is what keeps the
//     coast itself from being a wall.
//   - Nothing here is normalized against a window. Every threshold and every
//     weight is a constant of the world, so no value depends on what has been
//     looked at. DESIGN.md 14 and 33.5.
//
// Basin influence is deliberately absent: DESIGN.md 17.1 puts it in a product
// with moisture and keeps it out of elevation entirely.
func (g *Generator) elevationAt(c Coord) elevationParts {
	ec := &g.cfg.Elevation
	p := AxialToWorld(c)
	region := g.RegionInfluence(c)

	// The four continuous scales, in the fixed order Scales returns them.
	var parts elevationParts
	parts.region = region
	for i := range parts.scales {
		parts.scales[i] = g.scales[i].Sample(p.X, p.Y)
	}
	cont := parts.scales[ScaleContinental-1]
	reg := parts.scales[ScaleRegional-1]
	loc := parts.scales[ScaleLocal-1]
	det := parts.scales[ScaleDetail-1]

	// One divisor over all four weights, so the two halves sum to a weighted
	// average in [-1, +1] and neither half is normalized against itself.
	norm := ec.ContinentalWeight + ec.RegionalWeight + ec.LocalWeight + ec.DetailWeight
	coarse := (mathx.Mul(ec.ContinentalWeight, cont) + mathx.Mul(ec.RegionalWeight, reg)) / norm
	fine := (mathx.Mul(ec.LocalWeight, loc) + mathx.Mul(ec.DetailWeight, det)) / norm

	parts.raw = coarse + fine
	parts.uplift = mathx.Mul(ec.UpliftWeight, region.ElevationBias)
	parts.ridge = g.ridgeStructure(p, region.Ridge)

	rough := 1 + mathx.Mul(ec.RoughnessInfluence, region.Roughness)
	base := contrast(coarse, ec.ContrastPasses) + mathx.Mul(rough, fine) + parts.uplift

	mask := smoothstepUnit(base / ec.RidgeOnset)
	lift := mathx.Mul(mathx.Mul(ec.RidgeWeight, rough), mathx.Mul(parts.ridge, mask))

	parts.elevation = g.cfg.seaLevelRescale(clampUnitSigned(base + lift))
	return parts
}

// elevationScalar returns the elevation scalar at a coordinate.
//
// **Both Tile and Relief call this, and neither calls the other.** Relief reads
// the six neighboring elevations, so a Relief that called Tile — or a Tile that
// called Relief on a neighbor — would recurse until the stack was gone, and Go
// will not catch it. Structuring the internals around one unexported scalar is
// what makes the recursion unwritable. See DESIGN.md 18.
func (g *Generator) elevationScalar(c Coord) float64 { return g.elevationAt(c).elevation }

// ElevationAt returns the elevation scalar at a coordinate: -1 deep ocean, 0
// sea level, +1 extreme highland.
//
// It is a pure function of the seed, the coordinate, the algorithm version, the
// world radius, and the configuration. Nothing about which tiles have been
// generated, in what order, or on which machine can reach it.
func (g *Generator) ElevationAt(c Coord) float64 { return g.elevationScalar(c) }

// ElevationBandAt returns the elevation band at a coordinate.
func (g *Generator) ElevationBandAt(c Coord) Elevation {
	return g.cfg.Elevation.Bands.Classify(g.elevationScalar(c))
}

// IsLand reports whether a coordinate is above sea level.
//
// This is the whole of the land/water rule of DESIGN.md 15: elevation at or
// below zero is ocean water and above zero is land. There is no second
// threshold and no runtime statistic — a land fraction is tuned by moving
// SeaLevel and the continentalness distribution, never measured from a window.
func (g *Generator) IsLand(c Coord) bool { return g.elevationScalar(c) > 0 }

// Relief returns the local steepness at a coordinate, in [0, 1].
//
// It is the mean of the six absolute elevation differences to the neighbors,
// scaled and clamped. The mean rather than the maximum because terrain reads it
// as "is this flat, hilly, or steep", and one steep neighbor out of six is a
// scarp rather than a rough tile.
//
// The six differences are accumulated in fixed direction order 0..5. Floating
// point addition is not associative, so the order is part of the algorithm; see
// DESIGN.md 25.3. Sampling neighbors creates no dependency problem because
// every neighbor is a pure function of its own coordinate.
//
// Relief inside the rim will be the relief of the forced elevation, which is
// flat. That is correct and is not a special case: a forced constant has no
// slope.
func (g *Generator) Relief(c Coord) float64 {
	here := g.elevationScalar(c)

	var total float64
	for d := range 6 {
		total += math.Abs(g.elevationScalar(c.Neighbor(d)) - here)
	}

	// Clamped before it is returned and before anything converts it to a band
	// index. DESIGN.md 25.5.
	return min(mathx.Mul(g.cfg.Elevation.ReliefScale, total/6), 1)
}

// ---------------------------------------------------------------------------
// The ridge structure
// ---------------------------------------------------------------------------

// ridgeStructure returns the ridge structure term at a position, in [-1, +1],
// with ridges running along the region's blended ridge orientation.
//
// The fold is the usual one — 1 - 2*|noise| creases the field along its own zero
// set, which is a network of curves — and the orientation enters as a symmetric
// blur *along* it. Blurring along a direction removes variation in that
// direction, so what survives is structure that is constant along the
// orientation: belts running with the region's grain rather than an isotropic
// web.
//
// **The obvious implementation is wrong and is worth writing down.** Rotating
// or shearing the sampling frame by the blended orientation — sampling at
// (p·u, p·n) instead of p — looks equivalent and is not, because the position
// it produces is proportional to |p|. At the rim |p| is about 170,000 miles, so
// a thousandth of a radian of orientation change moves the sampled point
// further than the local wavelength, and the term decorrelates into noise
// everywhere except near the origin. A displacement of a fixed number of miles
// has no such term: its sensitivity to the orientation is bounded by
// RidgeStrideMiles wherever it is evaluated, which is what makes the result
// continuous across the whole map rather than only near the middle.
//
// The taps are symmetric in the stride, so the orientation's arrow does not
// change the value it produces — which matters because an orientation is a line
// and its arrow is whichever one the half-angle recovery happened to pick.
//
// This costs three evaluations of the ridge field, which is why the tuning
// tool's budget is counted in elevation evaluations rather than in noise
// samples: what a caller can count is tiles.
func (g *Generator) ridgeStructure(p Vec2, orientation UnitVec2) float64 {
	dx := mathx.Mul(orientation.X, g.cfg.Elevation.RidgeStrideMiles)
	dy := mathx.Mul(orientation.Y, g.cfg.Elevation.RidgeStrideMiles)

	// Fixed order: behind, here, ahead.
	behind := g.ridgedAt(p.X-dx, p.Y-dy)
	here := g.ridgedAt(p.X, p.Y)
	ahead := g.ridgedAt(p.X+dx, p.Y+dy)

	return mathx.Mul(0.25, behind) + mathx.Mul(0.5, here) + mathx.Mul(0.25, ahead)
}

// ridgedAt folds the ridge field at one position. The result is in [-1, +1] and
// is +1 exactly on the field's zero set, which is what draws a ridge line.
//
// The fold has a positive mean by construction, and that is intended rather than
// tolerated: a ridge term whose mean was zero would carve as much as it raised,
// and a mountain belt is ground that stands above what is around it. What the
// mean costs is that SeaLevel and the band thresholds are tuned against a
// composite that includes it, which is what the tuning tool is for.
func (g *Generator) ridgedAt(x, y float64) float64 {
	return 1 - mathx.Mul(2, math.Abs(g.ridge.Sample(x, y)))
}

// RidgeField returns the derived field tree the ridge structure term is folded
// from, so the tuning tool can print what was evaluated beside the window it
// drew. Like ScaleField it is not a second source of truth.
func (g *Generator) RidgeField() Field { return g.ridge }

// ---------------------------------------------------------------------------
// The shaping functions
// ---------------------------------------------------------------------------

// smoothstepUnit is 3t^2 - 2t^3 with the input clamped to [0, 1].
//
// It is written as a polynomial because DESIGN.md 25.2 permits multiply, add,
// and subtract and excludes every transcendental function. The clamp is what
// makes it total: the callers divide by a configured width and a value well
// outside the ramp must saturate rather than run away cubically.
func smoothstepUnit(t float64) float64 {
	t = min(max(t, 0), 1)
	return mathx.Mul(mathx.Mul(t, t), 3-mathx.Mul(2, t))
}

// contrast applies n smoothstep passes to a value in [-1, +1], pushing it away
// from the middle and toward the ends.
//
// A weighted sum of several noise fields is more concentrated about zero than
// any one of them, which draws a coastline as a wide band of near-sea-level
// ground rather than as a line. Each pass is the same S-curve, so the number of
// them is a single legible dial rather than an exponent somebody has to reason
// about, and it is bounded by MaxContrastPasses because past a few the field is
// a two-valued mask.
//
// Zero passes is the identity and is a legitimate setting.
func contrast(v float64, passes uint8) float64 {
	for range passes {
		v = mathx.Mul(2, smoothstepUnit((v+1)/2)) - 1
	}
	return v
}

// seaLevelRescale maps a composite in [-1, +1] to the elevation scalar, putting
// sea level at exactly zero.
//
// SeaLevel is the fraction of the composite's range that lies under water, so
// the two sides are rescaled by different factors: the water side stretches
// [-1, seaLevel] onto [-1, 0] and the land side stretches [seaLevel, +1] onto
// [0, +1]. That is what lets the land fraction be tuned without the meaning of
// the scalar moving — zero is sea level at every setting, and DESIGN.md 14's
// three anchor values keep their meanings.
//
// The map is continuous at the join, where both branches give zero, and
// monotonic everywhere. A SeaLevel of zero or one would make one branch a
// division by zero, which is why validation refuses both.
func (c Config) seaLevelRescale(v float64) float64 {
	// The composite on [0, 1], which is the scale SeaLevel is expressed in.
	u := (v + 1) / 2
	if u <= c.SeaLevel {
		return (u - c.SeaLevel) / c.SeaLevel
	}
	return (u - c.SeaLevel) / (1 - c.SeaLevel)
}
