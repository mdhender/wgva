// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"

	"github.com/mdhender/wgva/internal/mathx"
)

// Coord is a canonical axial coordinate. Its fields are unexported and there is
// no constructor that skips normalization, so a non-canonical Coord cannot be
// built outside this package. That makes ==, use as a map key, and slices.Sort
// over a []Coord automatically correct for tile identity, persistence keys,
// region lookup, and set membership.
//
// The zero value is the origin, which is canonical. There is no invalid zero
// value to defend against and no IsValid to forget to call.
//
// See DESIGN.md 4.
type Coord struct {
	q Component
	r Component
}

// Origin is the canonical origin, equal to the zero value of Coord.
var Origin Coord

// NewCoord normalizes any axial pair into the canonical wrapped map. It is the
// entry point for unwrapped or intermediate coordinates and the only way to
// obtain a Coord other than the zero value and the coordinate-producing methods,
// which normalize before returning.
//
// Every int64 pair is accepted, including values near the ends of the type.
// See DESIGN.md 7.1 for the three stages that get there.
func NewCoord(q, r int64) Coord {
	nq, nr := normalize(q, r)
	return newCanonical(nq, nr)
}

// Q returns the q component.
func (c Coord) Q() Component { return c.q }

// R returns the r component.
func (c Coord) R() Component { return c.r }

// S returns the derived cube component, -q - r. It is always in Component range
// for a canonical coordinate.
func (c Coord) S() Component { return componentOf(c.s()) }

// s returns the derived cube component as an int64. Every intermediate in this
// package is int64; see DESIGN.md 4.
func (c Coord) s() int64 { return -int64(c.q) - int64(c.r) }

// newCanonical builds a Coord from components already known to be canonical.
// The range checks are assertions of DESIGN.md 4.1 rather than formalities:
// they are the only thing keeping a Coord inside the symmetric domain, and the
// domain is what makes negation, absolute value, and the int64 round trip
// total. Component is wider than the domain, so it will not catch a mistake
// here — it will only make one visible.
func newCanonical(q, r int64) Coord {
	return Coord{q: componentOf(q), r: componentOf(r)}
}

// componentOf narrows an int64 to a Component, checking against ±WorldRadius
// rather than against the Component type's own range, which is wider on both
// sides and deliberately so. See DESIGN.md 4.1 and 4.2.
func componentOf(v int64) Component {
	if v < -WorldRadius || v > WorldRadius {
		panic("wgva: coordinate component outside the canonical domain")
	}
	return Component(v)
}

// ---------------------------------------------------------------------------
// World space
// ---------------------------------------------------------------------------

// Vec2 is a position in canonical world space, in miles.
type Vec2 struct {
	X float64
	Y float64
}

// The hex metric of DESIGN.md 7: a 3-mile apothem, so neighboring centers are
// 6 miles apart and one step in r advances 3*sqrt(3) miles.
const (
	hexApothemMiles        = 3.0
	hexCenterDistanceMiles = 6.0
)

// worldYPerR is 3*sqrt(3), computed once. math.Sqrt is one of the operations
// DESIGN.md 25.2 permits: it is correctly rounded by IEEE-754 and therefore
// bit-identical on every target.
var worldYPerR = mathx.Mul(hexApothemMiles, math.Sqrt(3))

// AxialToWorld converts a coordinate to its position in canonical world space,
// in miles. Every continuous field samples from this one coordinate system, so
// the embedding is pinned by AlgorithmVersion.
//
//	x = 6*q + 3*r
//	y = 3*sqrt(3)*r
//
// The world position is float64 at every scale and must never be computed in
// float32: at the map's far corner float32 spacing is about 165 feet, which is
// small enough to pass a test at the origin and fail one at the rim. See
// DESIGN.md 7.
func AxialToWorld(c Coord) Vec2 {
	q, r := float64(c.q), float64(c.r)
	return Vec2{
		X: mathx.Mul(hexCenterDistanceMiles, q) + mathx.Mul(hexApothemMiles, r),
		Y: mathx.Mul(worldYPerR, r),
	}
}

// ---------------------------------------------------------------------------
// Directions and rotation
// ---------------------------------------------------------------------------

// directions is the six canonical direction vectors in axial form, numbered in
// the order Red Blob Games gives them. Increasing the index steps to the next
// neighbor in index order; decreasing it steps the other way.
//
// The package says neither "clockwise" nor "counter-clockwise": nothing here has
// a north, and a rotation sense is a statement about a picture this package
// cannot see. See DESIGN.md appendix A.
//
// Changing the table, or renumbering it, changes every world.
var directions = [6][2]int64{
	{+1, 0},
	{+1, -1},
	{0, -1},
	{-1, 0},
	{-1, +1},
	{0, +1},
}

// normalizeDirection reduces any integer direction or rotation step into 0..5.
// Go's % returns a remainder with the sign of the dividend, so -7 % 6 is -1
// rather than 5 and the adjustment branch is required. This is the only place
// in the module that does this.
func normalizeDirection(dir int) int {
	dir %= 6
	if dir < 0 {
		dir += 6
	}
	return dir
}

// Neighbor returns the adjacent coordinate in the given direction, normalized.
// Any integer direction is accepted; values differing by a multiple of six
// identify the same direction.
func (c Coord) Neighbor(direction int) Coord {
	d := directions[normalizeDirection(direction)]
	return NewCoord(int64(c.q)+d[0], int64(c.r)+d[1])
}

// rotateOnce advances a cube coordinate by one step in index order. It is an
// exact permutation with sign changes, so it is available everywhere in the
// generation path without troubling DESIGN.md 25.2: there is no matrix and no
// angle. Six applications are the identity, and the inverse step is
// (x, y, z) -> (-y, -z, -x).
func rotateOnce(x, y, z int64) (int64, int64, int64) {
	return -z, -x, -y
}

// Rotate turns by steps sixths of a turn, in index order, for any signed steps.
//
// The canonical domain is six-fold symmetric about the origin, so the rotation
// maps it onto itself and commutes with normalization. It also commutes with
// RimDistance, since max(|q|,|r|,|s|) is invariant under a permutation with sign
// changes.
func (c Coord) Rotate(steps int) Coord {
	q, r, s := int64(c.q), int64(c.r), c.s()
	for range normalizeDirection(steps) {
		q, r, s = rotateOnce(q, r, s)
	}
	return newCanonical(q, r)
}

// ---------------------------------------------------------------------------
// Cells
// ---------------------------------------------------------------------------

// Cell divides the coordinate into cells of sizeHexes on each axial axis, with
// mathematical floor division so the addressing is continuous across the origin.
// One method serves chunk, region, and macro-region addressing. See DESIGN.md
// 23.
//
// sizeHexes must not be zero. It is unsigned so that the divisor cannot be
// negative, which is where floor and Euclidean division part company, and the
// zero check completes the guarantee.
func (c Coord) Cell(sizeHexes uint32) (int64, int64) {
	size := cellSize(sizeHexes)
	return mathx.FloorDiv(int64(c.q), size), mathx.FloorDiv(int64(c.r), size)
}

// CellOffset returns the position of the coordinate inside its cell, in
// [0, sizeHexes) on each axis. It pairs with Cell:
// Cell*size + CellOffset == the coordinate.
func (c Coord) CellOffset(sizeHexes uint32) (int64, int64) {
	size := cellSize(sizeHexes)
	return mathx.FloorMod(int64(c.q), size), mathx.FloorMod(int64(c.r), size)
}

func cellSize(sizeHexes uint32) int64 {
	if sizeHexes == 0 {
		panic("wgva: cell size must be positive")
	}
	return int64(sizeHexes)
}

// ---------------------------------------------------------------------------
// The rim
// ---------------------------------------------------------------------------

// RimDistance reports hexes from the outer edge of the canonical map: 0 on the
// outermost ring, positive inside, and never negative for a canonical
// coordinate.
//
// Hex distance from the origin is exactly max(|q|,|r|,|s|) for cube coordinates
// summing to zero, so this is O(1) and purely local — it needs nothing outside
// the tile itself. DESIGN.md 4.1 is what makes the absolute values total.
func (c Coord) RimDistance() int64 {
	q, r, s := int64(c.q), int64(c.r), c.s()
	return WorldRadius - max(abs64(q), abs64(r), abs64(s))
}

// abs64 is total for every value this package produces, because the canonical
// domain is symmetric about the origin and bounded far inside int64. See
// DESIGN.md 4.1.
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// hexNorm is the hex distance from the origin of an axial pair: max of the three
// cube magnitudes. The caller is responsible for keeping q and r inside
// greedySafeBound so that q+r cannot overflow.
func hexNorm(q, r int64) int64 {
	return max(abs64(q), abs64(r), abs64(q+r))
}

// ---------------------------------------------------------------------------
// Normalization
// ---------------------------------------------------------------------------

// mirrorCenters holds the six rotations of (2N+1, -N, -N-1) in cube form,
// generated by rotateOnce rather than transcribed. Two of the six have
// components at ±(2N+1), outside the canonical domain, which is what the int64
// intermediate rule of DESIGN.md 4 protects.
//
// Go has no const array and no const function call, so this is a var built in
// init. It is immutable by convention and by a test that recomputes it.
var mirrorCenters [6][3]int64

func init() {
	x, y, z := 2*WorldRadius+1, -WorldRadius, -WorldRadius-1
	for i := range mirrorCenters {
		mirrorCenters[i] = [3]int64{x, y, z}
		x, y, z = rotateOnce(x, y, z)
	}
}

const (
	// greedyStepLimit bounds stage 2. Every coordinate the rest of the program
	// actually produces — a neighbor, a scroll step, a region anchor, a
	// rotation — lands inside the canonical hexagon within a handful of steps,
	// so reaching the limit means the input is wild and stage 3 is owed.
	greedyStepLimit = 32

	// greedySafeBound is the largest magnitude stage 2 may see on either axis.
	// Stage 2 computes q+r, so an input near the ends of int64 would overflow
	// before any mirror center was subtracted. Anything larger goes straight to
	// the exact stage-3 solve, which is where it belongs anyway.
	greedySafeBound int64 = 1 << 61
)

// normalize maps any axial pair onto its canonical representative, in the three
// stages of DESIGN.md 7.1. Staging the cheap cases first is what keeps the
// exotic arithmetic in a branch that ordinary gameplay never takes, so a bug in
// it cannot be reached by a neighbor step. Test stage 3 directly; see
// DESIGN.md 30.4.
func normalize(q, r int64) (int64, int64) {
	// Stage 1: already canonical. Three comparisons, and the overwhelmingly
	// common case.
	if isCanonical(q, r) {
		return q, r
	}

	// Stage 2: bounded greedy fix-up.
	if abs64(q) <= greedySafeBound && abs64(r) <= greedySafeBound {
		q, r = greedyReduce(q, r)
		if isCanonical(q, r) {
			return q, r
		}
	}

	// Stage 3: exact lattice solve, for wild input only. The residual is within
	// hex distance 2N+1 of the origin, which stage 2 then finishes.
	q, r = latticeSolve(q, r)
	q, r = greedyReduce(q, r)
	if !isCanonical(q, r) {
		panic("wgva: normalization failed to reach the canonical domain")
	}
	return q, r
}

// IsCanonical reports whether an axial pair names a tile of this world: every
// one of q, r, and s within +/-WorldRadius.
//
// It is exported for the one caller that must ask the question without
// answering it. SQLite columns hold anything, so a stored (q, r) outside the
// domain is malformed data rather than a distant tile, and passing it through
// NewCoord would silently relocate a player's settlement to a real coordinate
// somewhere else. store asks this and refuses; nothing repairs. See DESIGN.md
// 27.6 and 4.1.
func IsCanonical(q, r int64) bool { return isCanonical(q, r) }

// isCanonical reports whether the axial pair lies in the canonical hexagon. It
// checks q and r before deriving s so that the derivation cannot overflow.
func isCanonical(q, r int64) bool {
	if q < -WorldRadius || q > WorldRadius || r < -WorldRadius || r > WorldRadius {
		return false
	}
	s := -q - r
	return s >= -WorldRadius && s <= WorldRadius
}

// greedyReduce subtracts whichever mirror center most reduces the hex norm, in
// fixed index order, until none does or the step limit is reached.
//
// The six centers are the relevant vectors of the wraparound lattice and the
// canonical hexagon is an exact fundamental domain of it — 1 + 3N(N+1) tiles for
// a lattice of the same index — so every coordinate has exactly one canonical
// representative, there is no tie to break, and a point no center improves is
// already canonical.
func greedyReduce(q, r int64) (int64, int64) {
	for range greedyStepLimit {
		best := hexNorm(q, r)
		bq, br := q, r
		for _, m := range mirrorCenters {
			nq, nr := q-m[0], r-m[1]
			if n := hexNorm(nq, nr); n < best {
				best, bq, br = n, nq, nr
			}
		}
		if bq == q && br == r {
			return q, r
		}
		q, r = bq, br
	}
	return q, r
}

// latticeSolve removes the bulk of a wild coordinate in one step by inverting
// the two-vector basis that generates the wraparound lattice.
//
// Mirror centers 0 and 1 are that basis: in axial form v0 = (2N+1, -N) and
// v1 = (N+1, -(2N+1)). Writing (q, r) = a*v0 + b*v1 and rounding a and b to the
// nearest integer leaves a residual near the origin. The inverse is
//
//	a =  (M*q + (N+1)*r) / D
//	b = -(N*q +     M*r) / D
//
// with M = 2N+1 and D = 3N^2 + 3N + 1, which is |det| and is also 1 + 3N(N+1),
// the tile count of the canonical hexagon.
//
// This is the one place in the module that widens past int64, and settling the
// world radius does not change that. NewCoord accepts any int64
// pair, so the products reach 6.0e23 — the bound comes from the input domain, not
// from the world size. D itself fits comfortably here, at 3.2e9; do not read that
// as licence to narrow the solve, because it is the numerator that overflows. Go
// wraps signed overflow silently, so an int64 solve returns a plausible wrong
// answer with nothing to report it. See DESIGN.md 7.1 and appendix D.1.
func latticeSolve(q, r int64) (int64, int64) {
	n := WorldRadius
	m := 2*n + 1

	un := uint64(n)
	d := 3*un*un + 3*un + 1

	a := mathx.MulInt64(m, q).Add(mathx.MulInt64(n+1, r)).DivRoundPos(d)
	b := mathx.MulInt64(n, q).Add(mathx.MulInt64(m, r)).Neg().DivRoundPos(d)

	// residual = (q, r) - a*v0 - b*v1, computed at 128 bits because a*M and
	// b*(N+1) are each about the size of q and very nearly cancel.
	resQ := mathx.Int128Of(q).Sub(mathx.MulInt64(a, m)).Sub(mathx.MulInt64(b, n+1))
	resR := mathx.Int128Of(r).Add(mathx.MulInt64(a, n)).Add(mathx.MulInt64(b, m))

	rq, okQ := resQ.Int64()
	rr, okR := resR.Int64()
	if !okQ || !okR {
		panic("wgva: lattice solve residual does not fit in an int64")
	}
	return rq, rr
}
