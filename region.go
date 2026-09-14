// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"

	"github.com/mdhender/wgva/internal/mathx"
)

// ---------------------------------------------------------------------------
// Orientation
// ---------------------------------------------------------------------------

// UnitVec2 is a unit direction in canonical world space.
//
// Ridge orientation is stored and consumed as one of these rather than as an
// angle in radians, because DESIGN.md 25.2 excludes every trigonometric
// function from the generation path: an angle would have to be turned back into
// a direction by math.Cos, which Go implements in portable Go for some
// functions and architecture-specific assembly for others, and that split has
// moved between releases. A stored vector needs none of them.
//
// The field on RegionParams is named Ridge and typed UnitVec2 for the same
// reason. A field called RidgeAngle holding a vector invites somebody to put an
// angle in it, and a float64 there accepts one without complaint.
type UnitVec2 struct {
	X float64
	Y float64
}

// Dot returns the dot product of two unit vectors, in [-1, +1].
func (v UnitVec2) Dot(w UnitVec2) float64 {
	return mathx.Mul(v.X, w.X) + mathx.Mul(v.Y, w.Y)
}

// Align reports how closely two orientations agree, in [0, 1]: one when they
// run the same way and zero when they are at right angles.
//
// It is the absolute value of the dot product, and that is the whole reason it
// exists. A ridge orientation is a line, not an arrow — v and -v name the same
// orientation — so a consumer that took the signed dot product would read two
// identical ridges as opposites whenever the hashes happened to pick opposite
// arrows. Naming the operation is what keeps the rule from having to be
// remembered at each call site. See DESIGN.md 12.
func (v UnitVec2) Align(w UnitVec2) float64 { return math.Abs(v.Dot(w)) }

// canonical returns the representative of the orientation in the X >= 0 half
// plane, with the tie at X == 0 going to Y >= 0.
//
// An orientation is a line, so v and -v are the same value and one of the two
// has to be chosen consistently or nothing about a ridge is comparable. The
// half-angle recovery in orientationFrom produces the same representative by
// construction, which is what lets the blend at an anchor reproduce that
// anchor's ridge rather than its negation.
func (v UnitVec2) canonical() UnitVec2 {
	if v.X < 0 || (v.X == 0 && v.Y < 0) {
		return UnitVec2{X: -v.X, Y: -v.Y}
	}
	return v
}

// doubled returns the doubled-angle form (x^2 - y^2, 2xy).
//
// For a unit vector at angle t this is the unit vector at angle 2t, and it is
// identical for v and -v — which is exactly what makes it the form orientations
// are averaged in. Blending the vectors themselves would let two anchors whose
// ridges run the same way but hashed to opposite arrows cancel to nothing, and
// a tile between them would then get an orientation unrelated to either.
// Orientations cannot be averaged as vectors; see DESIGN.md 12.
func (v UnitVec2) doubled() (x, y float64) {
	return mathx.Mul(v.X, v.X) - mathx.Mul(v.Y, v.Y), mathx.Mul(2, mathx.Mul(v.X, v.Y))
}

// ridgeCancelLength2 is the shortest squared length an orientation is recovered
// from. Below it the blended doubled-angle vector has cancelled — two anchor
// ridges at right angles double to opposite directions — and the ratio dx/|d|
// is rounding noise rather than an angle. No orientation exists there, so the
// caller supplies a deterministic fallback. See DESIGN.md 12.
const ridgeCancelLength2 = 1e-12

// orientationFrom recovers a unit orientation from a blended doubled-angle
// vector, by the half-angle identities
//
//	cos t = sqrt((1 + cos 2t) / 2)
//	sin t = sqrt((1 - cos 2t) / 2)
//
// with the sign of sin t taken from sin 2t. Every step is multiply, add,
// divide, and math.Sqrt, which is what DESIGN.md 25.2 permits.
//
// cos t is non-negative by construction, so the result is already the canonical
// representative of the line.
func orientationFrom(dx, dy float64, fallback UnitVec2) UnitVec2 {
	length2 := mathx.Mul(dx, dx) + mathx.Mul(dy, dy)
	if length2 < ridgeCancelLength2 {
		return fallback
	}
	// The blended vector is a convex combination of unit vectors, so its length
	// is at most one; the clamp covers the rounding at the ends, where sqrt of a
	// negative would otherwise be a NaN that reached a tile.
	cos2 := min(max(dx/math.Sqrt(length2), -1), 1)
	cos := math.Sqrt((1 + cos2) / 2)
	sin := math.Sqrt((1 - cos2) / 2)
	if dy < 0 {
		sin = -sin
	}
	return UnitVec2{X: cos, Y: sin}
}

// ---------------------------------------------------------------------------
// Region parameters
// ---------------------------------------------------------------------------

// RegionParams is one region anchor's deterministic parameters: the persistent
// geographic character of a large area, derived from the seed and the anchor's
// lattice position and stored nowhere. See DESIGN.md 11.1 and 12.
//
// Every scalar is a normalized bias in [-1, +1], not a quantity. How much
// uplift an elevation bias is worth, or how many degrees a heat bias moves a
// tile, belongs to the phase that consumes it — which is what lets elevation,
// climate, and terrain be tuned independently without redefining what a region
// is.
//
// The same type carries a blend of several anchors, because a blend of biases
// is a bias.
type RegionParams struct {
	// ElevationBias is whether the region stands high or low. Regional uplift
	// is what elevation makes of it.
	ElevationBias float64

	// MoistureBias and HeatBias are the region's wet/dry and warm/cool
	// tendencies, which climate reads as an offset to its own fields.
	MoistureBias float64
	HeatBias     float64

	// Roughness is whether the region's relief is exaggerated or subdued.
	Roughness float64

	// BasinBias is the region's tendency toward enclosed low ground. It enters
	// terrain as a product with moisture and never enters elevation; see
	// DESIGN.md 17.1.
	BasinBias float64

	// Volcanic is the region's volcanic tendency.
	Volcanic float64

	// Variation is how much local terrain within the region departs from what
	// the other biases would predict.
	Variation float64

	// Ridge is the orientation of the region's ridge structure, as a line
	// rather than an arrow: consumers take Align, never Dot.
	Ridge UnitVec2
}

// The parameter index each scalar is hashed under.
//
// It is the third coordinate of the anchor hash rather than a hashing domain of
// its own, which is the shape the seed-derived sampling offset already uses:
// one domain, with an index that separates the values taken under it. The
// per-node domain rule of DESIGN.md 8.1 is about noise lattices — two field
// nodes sharing a domain would share a gradient table and a sampling offset —
// and an anchor parameter is neither.
//
// These values are pinned by every world they generate, so each is written out
// explicitly rather than taken from iota. Renumbering one swaps two parameters
// across the whole world, with nothing in the diff that looks like a data
// change.
const (
	paramElevationBias int64 = 0
	paramMoistureBias  int64 = 1
	paramHeatBias      int64 = 2
	paramRoughness     int64 = 3
	paramBasinBias     int64 = 4
	paramVolcanic      int64 = 5
	paramVariation     int64 = 6
)

// anchorBias is one normalized bias for one anchor, uniform in [-1, +1).
func anchorBias(seed Seed, param, i, j int64) float64 {
	return mathx.Mul(2, toUnit(Hash3(uint64(seed), DomRegionStyle, param, i, j))) - 1
}

// ridgeAttempts bounds the rejection sampling in anchorRidge. The unit disc
// covers pi/4 of the square, so sixteen consecutive rejections have a
// probability of about 3e-11 per anchor; the bound is there to make the
// function total rather than because it is expected to be reached.
const ridgeAttempts = 16

// ridgeSampleMin2 is the shortest squared length the rejection accepts. A
// sample much shorter than this is almost all rounding by the time it has been
// divided by its own length.
const ridgeSampleMin2 = 1e-4

// anchorRidge returns the anchor's ridge orientation, uniform over the half
// circle.
//
// The direction comes from rejection sampling in the unit disc: hash a point in
// the square, keep it if it lands inside the disc and is not so short that
// normalizing it would divide out its own information, and normalize with
// math.Sqrt. Rejecting by radius is rotationally symmetric, so it does not bias
// the direction — which a hashed angle through math.Cos would not be allowed to
// do at all. See DESIGN.md 12 and 25.2.
func anchorRidge(seed Seed, i, j int64) UnitVec2 {
	for attempt := range int64(ridgeAttempts) {
		x := mathx.Mul(2, toUnit(Hash3(uint64(seed), DomRidgeOrientation, 2*attempt, i, j))) - 1
		y := mathx.Mul(2, toUnit(Hash3(uint64(seed), DomRidgeOrientation, 2*attempt+1, i, j))) - 1
		length2 := mathx.Mul(x, x) + mathx.Mul(y, y)
		if length2 > 1 || length2 < ridgeSampleMin2 {
			continue
		}
		length := math.Sqrt(length2)
		return UnitVec2{X: x / length, Y: y / length}.canonical()
	}
	// Reached with probability about 3e-11 per anchor, which over a whole world
	// of anchors is still a coin nobody will see land. It returns a value rather
	// than panicking because an anchor with an arbitrary ridge is a region that
	// looks slightly less varied, and a panic here would be a world that cannot
	// be generated at all.
	return UnitVec2{X: 1, Y: 0}
}

// regionAnchor derives one anchor's parameters. The anchor is named by its
// lattice indices, not by a coordinate: see Generator.Region.
func regionAnchor(seed Seed, i, j int64) RegionParams {
	return RegionParams{
		ElevationBias: anchorBias(seed, paramElevationBias, i, j),
		MoistureBias:  anchorBias(seed, paramMoistureBias, i, j),
		HeatBias:      anchorBias(seed, paramHeatBias, i, j),
		Roughness:     anchorBias(seed, paramRoughness, i, j),
		BasinBias:     anchorBias(seed, paramBasinBias, i, j),
		Volcanic:      anchorBias(seed, paramVolcanic, i, j),
		Variation:     anchorBias(seed, paramVariation, i, j),
		Ridge:         anchorRidge(seed, i, j),
	}
}

// ---------------------------------------------------------------------------
// Blending
// ---------------------------------------------------------------------------

// blend3 is a weighted sum of three values in fixed argument order.
//
// The order is part of the algorithm rather than an implementation detail:
// floating-point addition is not associative, so summing the same three terms
// in another order is another number. See DESIGN.md 25.3.
func blend3(w0, w1, w2, v0, v1, v2 float64) float64 {
	return mathx.Mul(w0, v0) + mathx.Mul(w1, v1) + mathx.Mul(w2, v2)
}

// triangleWeights returns the three anchors of the triangle containing a
// position inside a lattice cell, and the weight of each.
//
// The axial basis vectors are sixty degrees apart and equal in length, so
// anchors at (i*size, j*size) form a triangular lattice in world space and each
// cell is a rhombus of two equilateral triangles. Interpolating bilinearly
// across the cell privileges the cell's long diagonal, and a field built that
// way draws the lattice as rows of aligned lozenges; interpolating across the
// containing triangle has no preferred diagonal and carries the lattice's own
// six-fold symmetry. DESIGN.md 11.2.
//
// The short diagonal is the one from (1, 0) to (0, 1): in world space the cell's
// corners are (0,0), (6s,0), (3s,3s*sqrt(3)) and (9s,3s*sqrt(3)) miles, so that
// diagonal is 6s miles long and the other is 6s*sqrt(3). It splits the cell at
// fu + fv = 1.
//
// Barycentric coordinates on a triangle are already a partition of unity.
// Squaring each and renormalizing keeps the sum at one and makes an anchor's
// weight and its first derivative vanish as that anchor leaves the
// neighborhood, so a triangle edge — and a cell boundary, which is one — is a
// join rather than a crease. Cubing joins too and looks worse: each anchor
// acquires a plateau, and the plateaus meet along the hexagonal boundaries of
// the lattice's Voronoi cells, which is the lattice made visible by another
// route.
func triangleWeights(i, j int64, fu, fv float64) (anchors [3][2]int64, w0, w1, w2 float64) {
	if fu+fv <= 1 {
		anchors = [3][2]int64{{i, j}, {i + 1, j}, {i, j + 1}}
		w0, w1, w2 = 1-fu-fv, fu, fv
	} else {
		anchors = [3][2]int64{{i + 1, j + 1}, {i + 1, j}, {i, j + 1}}
		w0, w1, w2 = fu+fv-1, 1-fv, 1-fu
	}

	// Square and renormalize. Three non-negative weights summing to one have a
	// sum of squares of at least a third, so the divisor cannot be zero and no
	// guard is owed here.
	w0, w1, w2 = mathx.Mul(w0, w0), mathx.Mul(w1, w1), mathx.Mul(w2, w2)
	norm := w0 + w1 + w2
	return anchors, w0 / norm, w1 / norm, w2 / norm
}

// Region returns the region-lattice cell a coordinate lies in.
//
// The two values are lattice indices, not a coordinate. The anchor lattice does
// not tile the wrapped hexagon — the region size does not divide the world —
// so an anchor near the map's edge has no canonical coordinate and must never
// be turned into one. That the lattice does not close up at the seam is what
// the rim covers rather than smooths; DESIGN.md 15.1 is why field periodicity is
// not owed.
func (g *Generator) Region(c Coord) (int64, int64) { return c.Cell(g.cfg.RegionSizeHexes) }

// MacroRegion returns the macro-region-lattice cell a coordinate lies in. Like
// Chunk it is addressing only: nothing in the generator reads it.
func (g *Generator) MacroRegion(c Coord) (int64, int64) { return c.Cell(g.cfg.MacroRegionSizeHexes) }

// Chunk returns the chunk-lattice cell a coordinate lies in.
//
// Chunks are useful to callers and caches and do not define geography. Nothing
// in the generator reads one, and a chunk boundary visible in a render would be
// a bug rather than a seam to tune around. DESIGN.md 23.
func (g *Generator) Chunk(c Coord) (int64, int64) { return c.Cell(g.cfg.ChunkSizeHexes) }

// RegionAnchor returns the deterministic parameters of one region anchor, named
// by its lattice indices. It exists so that what was blended can be inspected
// beside the blend.
func (g *Generator) RegionAnchor(i, j int64) RegionParams { return regionAnchor(g.seed, i, j) }

// RegionInfluence returns the region parameters blended at a coordinate.
//
// A tile must not inherit its parameters from exactly one region: that is a
// visible seam wherever the addressing changes. The blend is barycentric across
// the three anchors of the containing triangle, with the weights squared and
// renormalized. See triangleWeights and DESIGN.md 11.2.
func (g *Generator) RegionInfluence(c Coord) RegionParams {
	size := int64(g.cfg.RegionSizeHexes)
	i := mathx.FloorDiv(int64(c.q), size)
	j := mathx.FloorDiv(int64(c.r), size)

	// The position inside the cell, in [0, 1). Both are ratios of integers, so
	// the cell's own arithmetic contributes no rounding of its own and the two
	// sides of a cell boundary agree exactly.
	fu := float64(mathx.FloorMod(int64(c.q), size)) / float64(size)
	fv := float64(mathx.FloorMod(int64(c.r), size)) / float64(size)

	anchors, w0, w1, w2 := triangleWeights(i, j, fu, fv)
	p0 := regionAnchor(g.seed, anchors[0][0], anchors[0][1])
	p1 := regionAnchor(g.seed, anchors[1][0], anchors[1][1])
	p2 := regionAnchor(g.seed, anchors[2][0], anchors[2][1])

	d0x, d0y := p0.Ridge.doubled()
	d1x, d1y := p1.Ridge.doubled()
	d2x, d2y := p2.Ridge.doubled()

	// The clamps are a guarantee rather than an expectation. Each blend is a
	// convex combination of values in [-1, +1] and cannot leave it in exact
	// arithmetic; the weights are normalized by a division, so their sum is one
	// only to within rounding, and a later classifier must not have to wonder.
	return RegionParams{
		ElevationBias: clampUnitSigned(blend3(w0, w1, w2, p0.ElevationBias, p1.ElevationBias, p2.ElevationBias)),
		MoistureBias:  clampUnitSigned(blend3(w0, w1, w2, p0.MoistureBias, p1.MoistureBias, p2.MoistureBias)),
		HeatBias:      clampUnitSigned(blend3(w0, w1, w2, p0.HeatBias, p1.HeatBias, p2.HeatBias)),
		Roughness:     clampUnitSigned(blend3(w0, w1, w2, p0.Roughness, p1.Roughness, p2.Roughness)),
		BasinBias:     clampUnitSigned(blend3(w0, w1, w2, p0.BasinBias, p1.BasinBias, p2.BasinBias)),
		Volcanic:      clampUnitSigned(blend3(w0, w1, w2, p0.Volcanic, p1.Volcanic, p2.Volcanic)),
		Variation:     clampUnitSigned(blend3(w0, w1, w2, p0.Variation, p1.Variation, p2.Variation)),

		// Where the doubled-angle blend cancels, no orientation exists. The
		// fallback is the containing triangle's apex anchor, which is
		// deterministic and local — the two properties the choice has to have,
		// since there is no right answer to fall back to.
		Ridge: orientationFrom(
			blend3(w0, w1, w2, d0x, d1x, d2x),
			blend3(w0, w1, w2, d0y, d1y, d2y),
			p0.Ridge,
		),
	}
}
