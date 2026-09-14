// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"github.com/mdhender/wgva/internal/mathx"
)

// volcanicAt evaluates the volcanic tendency at one position, in [-1, +1].
//
//	volcanic = (wF*volcanicField + wB*regionVolcanicBias) / (wF + wB)
//
// It takes the position and the region blend rather than a coordinate, for the
// reason the basin and climate composites do: both have already been computed
// by the elevation composite, and re-deriving them would make a tile cost two
// evaluations of everything underneath it.
//
// **A volcano is not a rare roll.** DESIGN.md 17 and 33.1 are explicit that
// terrain is never an independent per-tile random selection, and a volcanic
// tendency is what makes the difference: this is a continuous field blended
// with a regional bias, so volcanoes cluster into provinces the way they do on
// a real map, and the same tile is volcanic in every generation of the world.
// What makes them rare is the threshold terrain applies to this, together with
// the uplift and the concentrated relief it also demands — three conditions on
// three fields, not one draw against a probability.
//
// The field is weighed against the region's bias by the same weighted average
// the other composites use, so the result is in [-1, +1] whatever the weights
// are.
func (g *Generator) volcanicAt(p Vec2, region RegionParams) float64 {
	tc := &g.cfg.Terrain
	norm := tc.VolcanicFieldWeight + tc.VolcanicBiasWeight
	return (mathx.Mul(tc.VolcanicFieldWeight, g.volcanic.Sample(p.X, p.Y)) +
		mathx.Mul(tc.VolcanicBiasWeight, region.Volcanic)) / norm
}

// VolcanicAt returns the volcanic tendency at a coordinate, in [-1, +1].
//
// It is what a volcano and a volcanic highland are drawn from, and it is a
// tendency rather than a verdict: the thresholds that turn it into terrain live
// in TerrainConfig, and this value is defined everywhere including under the
// ocean.
func (g *Generator) VolcanicAt(c Coord) float64 {
	e := g.elevationAt(c)
	return g.volcanicAt(e.pos, e.region)
}

// DeriveVolcanicField returns the volcanic tendency field, derived from the
// seed and the configuration by a pure function.
//
// It is the same shape as a continuous scale, under its own hashing domain. Its
// wavelength is the span of a volcanic province rather than of a cone: what
// draws one cone out of a province is the uplift and the relief terrain also
// reads, and a field fine enough to place individual peaks would be the
// per-tile random selection DESIGN.md 33.1 forbids, arrived at by way of a
// wavelength.
func DeriveVolcanicField(seed Seed, cfg Config) Field {
	return warped(seed, cfg, fbmField(seed, DomVolcanic, cfg.Terrain.Volcanic))
}

// VolcanicField returns the derived field tree the volcanic composite reads, so
// the tuning tool can print what was evaluated beside the window it drew. Like
// ScaleField it is not a second source of truth.
func (g *Generator) VolcanicField() Field { return g.volcanic }
