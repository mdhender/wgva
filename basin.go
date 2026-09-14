// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"github.com/mdhender/wgva/internal/mathx"
)

// basinParts is one coordinate's basin composite taken apart.
//
// Like elevationParts and climateParts it exists so that Sample can report what
// went into a value without evaluating the composite a second time. A diagnostic
// that recomputed its own version of the number would eventually disagree with
// the number, and the disagreement would be invisible.
type basinParts struct {
	// The three scales of enclosed low ground, each in [-1, +1] and before
	// anything is weighed against it. Positive is a hollow and negative is a
	// rise; nothing about the sign is arbitrary, because what reads this reads
	// it as a multiplier on moisture.
	broad    float64
	regional float64
	local    float64

	// basin is the blended influence: -1 a rise that sheds water, 0 ground that
	// neither gathers nor sheds, +1 a closed hollow.
	basin float64
}

// basinAt evaluates the basin composite of DESIGN.md 17 at one position.
//
// It takes the position and the region blend rather than a coordinate, because
// both have already been computed by the elevation composite. Written out:
//
//	basin = (wB*broad + wR*regional + wL*local + wBias*regionBasinBias)
//	        / (wB + wR + wL + wBias)
//
// One divisor over all four terms, so the result is a weighted average in
// [-1, +1] whatever the weights are and no term is normalized against itself.
// That is the shape both composites above it use, and it is what lets a weight
// be moved without the scalar's meaning moving with it.
//
// **This is not, and must never become, a term in the elevation composite.**
// Making a depression *be* lower ground is the obvious next thought and it
// would move every tile in every world; DESIGN.md 17.1 puts basin influence in
// a product with moisture inside terrain instead, and the golden tables are
// what enforce the placement. Adding basin influence adds columns and moves
// none of the ones already there.
//
// There is no contrast pass here, and its absence is a decision rather than an
// omission. The two composites above use one to spread a field that would
// otherwise sit in its middle band; this one is never cut into bands at all. It
// is a multiplier, and a multiplier that has been pushed toward its ends is a
// world where every tile is either a sump or a dome.
func (g *Generator) basinAt(p Vec2, region RegionParams) basinParts {
	bc := &g.cfg.Basin

	var parts basinParts
	parts.broad = g.basinBroad.Sample(p.X, p.Y)
	parts.regional = g.basinRegional.Sample(p.X, p.Y)
	parts.local = g.basinLocal.Sample(p.X, p.Y)

	norm := bc.BroadWeight + bc.RegionalWeight + bc.LocalWeight + bc.BiasWeight
	parts.basin = (mathx.Mul(bc.BroadWeight, parts.broad) +
		mathx.Mul(bc.RegionalWeight, parts.regional) +
		mathx.Mul(bc.LocalWeight, parts.local) +
		mathx.Mul(bc.BiasWeight, region.BasinBias)) / norm

	return parts
}

// wetness is what terrain reads where climate reads moisture.
//
//	wetness = clamp(moisture + basinMoistureWeight * basin * moisture)
//
// **It is a product and not a sum, and that is the whole of DESIGN.md 17.1's
// geography.** A sum would make every basin wet, which would put a marsh in the
// middle of a desert wherever the basin field happened to be high. The product
// scales the moisture that is already there: a basin makes a wet climate wetter
// and a dry one drier, and a rise sheds water either way. Dry endorheic regions
// such as the Great Basin then arrive as a consequence of the composition
// rather than as a special case.
//
// The scalar is signed, which is what makes that work. Moisture runs -1 arid to
// +1 saturated with zero the middle of the moderate band, so multiplying by
// something above one moves a value away from the middle in whichever direction
// it already lay.
//
// It is clamped because the product can leave [-1, +1] — a saturated tile in a
// deep basin is the case — and the band ladder it is handed to cuts a clamped
// scalar. See DESIGN.md 25.5.
func (c Config) wetness(moisture, basin float64) float64 {
	return clampUnitSigned(moisture + mathx.Mul(mathx.Mul(c.Basin.MoistureWeight, basin), moisture))
}

// BasinAt returns the basin influence at a coordinate: -1 a rise that sheds
// water, 0 neutral ground, +1 a closed hollow.
//
// It is a pure function of the seed, the coordinate, the algorithm version, the
// world radius, and the configuration. Nothing above it reads it except
// terrain, and terrain reads it as a multiplier on moisture.
func (g *Generator) BasinAt(c Coord) float64 {
	e := g.elevationAt(c)
	return g.basinAt(e.pos, e.region).basin
}

// DeriveBasinBroadField returns the broadest of the three basin fields, derived
// from the seed and the configuration by a pure function.
//
// It is the same shape as a continuous scale — the seed-derived offset above an
// fbm ladder above a simplex leaf, under the shared domain warp — because what
// makes it a basin field is its wavelength and what reads it. Its own hashing
// domain keeps its lattice clear of every other field's; see DESIGN.md 8.1.
func DeriveBasinBroadField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomBasin, cfg.Basin.Broad))
}

// DeriveBasinRegionalField returns the middle basin field. See
// DeriveBasinBroadField.
func DeriveBasinRegionalField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomBasinRegional, cfg.Basin.Regional))
}

// DeriveBasinLocalField returns the finest basin field. See
// DeriveBasinBroadField.
//
// The three have separate domains rather than one shared between them, which is
// the rule of DESIGN.md 8.1 in the case that rule was written for: three nodes
// under one domain would share a gradient table and a sampling offset, so at the
// world origin they would land in the same lattice cell at the same fractional
// position and the composite would be one field counted three times.
func DeriveBasinLocalField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomBasinLocal, cfg.Basin.Local))
}

// BasinBroadField, BasinRegionalField, and BasinLocalField return the derived
// field trees the basin composite reads, so the tuning tool can print what was
// evaluated beside the window it drew. Like ScaleField they are not a second
// source of truth.
func (g *Generator) BasinBroadField() Field { return g.basinBroad }

// BasinRegionalField returns the middle basin field. See BasinBroadField.
func (g *Generator) BasinRegionalField() Field { return g.basinRegional }

// BasinLocalField returns the finest basin field. See BasinBroadField.
func (g *Generator) BasinLocalField() Field { return g.basinLocal }
