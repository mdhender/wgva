// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"

	"github.com/mdhender/wgva/internal/mathx"
)

// The noise is implemented here rather than taken from a dependency, and that
// is a requirement rather than a preference. See DESIGN.md 9.2.
//
//   - A dependency's minor release may change its output. Every world generated
//     under the old output would become unreproducible with no AlgorithmVersion
//     bump on our side to record it.
//   - Anything that dispatches on runtime CPU features returns different values
//     on different machines, which breaks the central invariant outright.
//
// Two lattice noises are provided. Both take a position already divided by a
// wavelength, so one lattice cell is one wavelength on a side, and both return a
// value in [-1, +1].
//
//	simplexNoise  gradient noise on the skewed triangular lattice. Ken Perlin,
//	              "Noise hardware" (SIGGRAPH 2001 course notes) and Stefan
//	              Gustavson, "Simplex noise demystified" (2005). The public
//	              domain reference implementations were read for the skew
//	              constants and the corner walk; the gradient table, the hashing,
//	              and the multiply-add discipline are ours.
//	valueNoise    value noise with quintic interpolation. Standard construction;
//	              no reference implementation was copied.
//
// Everything below uses only the operations DESIGN.md 25.2 permits, and every
// multiply-add goes through mathx.Mul. A bare a*b + c here would be fused on
// arm64 and not on amd64, which is a world that differs between a laptop and a
// server with nothing in the source to see. That includes the subtractive forms:
// arm64 has FMSUB and FNMSUB too, so c - a*b is just as fusible as a*b + c.

// gradientCount is the number of gradient directions. Twelve directions at
// thirty-degree spacing is what the table below is, and thirty degrees divides
// the hex lattice's sixty, so no gradient direction is privileged relative to
// the world's own symmetry.
const gradientCount = 12

// gradients are twelve unit vectors at thirty-degree spacing.
//
// They are computed from math.Sqrt rather than from math.Cos, and that is not a
// stylistic choice: DESIGN.md 25.2 excludes the transcendental functions from
// the generation path because Go implements some of them in portable Go and
// others in architecture-specific assembly, and that split has moved between
// releases. A table built at init from math.Cos would bake one architecture's
// answer into every world generated on it. math.Sqrt is correctly rounded by
// IEEE-754 and is therefore bit-identical everywhere.
//
// Every entry is a unit vector, so no direction carries more amplitude than
// another. A table of mixed lengths — the (±1, ±1) form some reference
// implementations use — makes the diagonal directions sqrt(2) times stronger,
// which is visible as a diagonal grain once fbm stacks several octaves.
var gradients = func() [gradientCount][2]float64 {
	const half = 0.5
	root3Over2 := math.Sqrt(3) / 2
	return [gradientCount][2]float64{
		{+1, 0},
		{+root3Over2, +half},
		{+half, +root3Over2},
		{0, +1},
		{-half, +root3Over2},
		{-root3Over2, +half},
		{-1, 0},
		{-root3Over2, -half},
		{-half, -root3Over2},
		{0, -1},
		{+half, -root3Over2},
		{+root3Over2, -half},
	}
}()

// gradientAt returns the gradient vector for a lattice point.
func gradientAt(seed Seed, dom uint64, i, j int64) (float64, float64) {
	g := gradients[Hash2(uint64(seed), dom, i, j)%gradientCount]
	return g[0], g[1]
}

// The skew constants of the simplex lattice. F2 maps the square lattice onto
// the triangular one and G2 maps it back:
//
//	F2 = (sqrt(3) - 1) / 2
//	G2 = (3 - sqrt(3)) / 6
//
// Both are written as expressions over math.Sqrt for the reason the gradient
// table is.
var (
	simplexF2 = (math.Sqrt(3) - 1) / 2
	simplexG2 = (3 - math.Sqrt(3)) / 6

	// The last corner's offset is two unskew steps from the cell origin. It is
	// precomputed rather than written as 2*simplexG2 at the use site, because
	// that expression sits inside a subtraction and would be a multiply-add the
	// compiler is free to fuse. See the note on the skew below.
	simplexG2Twice = mathx.Mul(2, simplexG2)
)

// simplexScale converts the raw corner sum to [-1, +1].
//
// The raw sum's extreme is a property of the corner falloff and the gradient
// table, both fixed above, so this is a derived constant rather than a tuning
// knob. A search over two hundred seeds, refined locally, puts the extreme of
// the unscaled sum at 0.00983713, which would make the exact scale 101.6557.
// Ninety-nine is that rounded down, leaving about two percent of headroom for a
// sample the search did not visit; the clamp in simplexNoise is what covers the
// remainder, and it is a guarantee rather than an expectation.
//
// TestSimplexRange asserts both halves: nothing leaves [-1, +1], and the
// observed extreme still comes close to filling it. The second half is what
// catches a scale made conservative in a hurry, which costs the field contrast
// silently — every later threshold would then be tuned around the loss.
const simplexScale = 99.0

// simplexNoise returns gradient noise on the triangular lattice at (x, y),
// in [-1, +1]. One lattice cell is one unit on a side.
func simplexNoise(seed Seed, dom uint64, x, y float64) float64 {
	return clampUnitSigned(simplexScale * simplexRaw(seed, dom, x, y))
}

// simplexRaw is simplexNoise without the scale and the clamp. It is separate so
// that TestSimplexRange can search for the extreme of the corner sum itself,
// which is what simplexScale is derived from; scaling first would make the test
// measure its own constant.
func simplexRaw(seed Seed, dom uint64, x, y float64) float64 {
	// Skew the input onto the triangular lattice and find the cell.
	//
	// Both the skew and the unskew are multiply-adds and both go through
	// mathx.Mul, and the cost of forgetting is not theoretical: the first draft
	// of this function wrote them plainly and the golden suite of DESIGN.md 30.9
	// failed on amd64 while passing on arm64, with two of twenty-four rows
	// differing in the last two digits of the mantissa. Nothing else in the
	// module would have noticed.
	skew := mathx.Mul(x+y, simplexF2)
	ix := math.Floor(x + skew)
	iy := math.Floor(y + skew)
	i, j := int64(ix), int64(iy)

	// Unskew the cell origin back and take the position within the cell.
	unskew := mathx.Mul(ix+iy, simplexG2)
	x0 := x - (ix - unskew)
	y0 := y - (iy - unskew)

	// The cell is two triangles. Which one holds the sample decides the middle
	// corner; the first and last corners are the same either way.
	var i1, j1 int64
	if x0 > y0 {
		i1, j1 = 1, 0
	} else {
		i1, j1 = 0, 1
	}

	x1 := x0 - float64(i1) + simplexG2
	y1 := y0 - float64(j1) + simplexG2
	x2 := x0 - 1 + simplexG2Twice
	y2 := y0 - 1 + simplexG2Twice

	// Corners are summed in a fixed order, always. Floating-point addition is
	// not associative; see DESIGN.md 25.3.
	n := simplexCorner(seed, dom, i, j, x0, y0)
	n += simplexCorner(seed, dom, i+i1, j+j1, x1, y1)
	n += simplexCorner(seed, dom, i+1, j+1, x2, y2)

	return n
}

// simplexCorner returns one corner's contribution: a radial falloff times the
// dot product of that corner's gradient with the offset to the sample.
func simplexCorner(seed Seed, dom uint64, i, j int64, dx, dy float64) float64 {
	t := 0.5 - mathx.Mul(dx, dx) - mathx.Mul(dy, dy)
	if t <= 0 {
		return 0
	}
	gx, gy := gradientAt(seed, dom, i, j)
	dot := mathx.Mul(gx, dx) + mathx.Mul(gy, dy)
	t2 := mathx.Mul(t, t)
	return mathx.Mul(mathx.Mul(t2, t2), dot)
}

// valueNoise returns value noise with quintic interpolation at (x, y), in
// [-1, +1]. One lattice cell is one unit on a side.
//
// Its range is exact by construction rather than by a scale constant: the four
// lattice values lie in [-1, +1) and the interpolation is a convex combination
// of them, so nothing can leave the interval. That is the whole reason it is
// here beside the gradient noise — it is the field kind to reach for when a
// bound has to be provable rather than measured.
func valueNoise(seed Seed, dom uint64, x, y float64) float64 {
	fx := math.Floor(x)
	fy := math.Floor(y)
	i, j := int64(fx), int64(fy)

	u := quintic(x - fx)
	v := quintic(y - fy)

	v00 := latticeValue(seed, dom, i, j)
	v10 := latticeValue(seed, dom, i+1, j)
	v01 := latticeValue(seed, dom, i, j+1)
	v11 := latticeValue(seed, dom, i+1, j+1)

	a := lerp(v00, v10, u)
	b := lerp(v01, v11, u)
	return lerp(a, b, v)
}

// latticeValue is the hashed value at a lattice point, in [-1, +1).
func latticeValue(seed Seed, dom uint64, i, j int64) float64 {
	return mathx.Mul(2, toUnit(Hash2(uint64(seed), dom, i, j))) - 1
}

// quintic is Perlin's 6t^5 - 15t^4 + 10t^3, whose first and second derivatives
// both vanish at 0 and 1. The cubic smoothstep leaves a second-derivative
// discontinuity at every cell boundary, which relief — a first difference
// between neighbors — turns into a visible lattice.
//
// It is evaluated in Horner form so that every step is one multiply and one
// add, each written through mathx.Mul.
func quintic(t float64) float64 {
	p := mathx.Mul(6, t) - 15
	p = mathx.Mul(p, t) + 10
	p = mathx.Mul(p, t)
	p = mathx.Mul(p, t)
	return mathx.Mul(p, t)
}

// lerp returns a + (b-a)*t.
func lerp(a, b, t float64) float64 {
	return a + mathx.Mul(b-a, t)
}

// clampUnitSigned confines v to [-1, +1].
func clampUnitSigned(v float64) float64 {
	return min(max(v, -1), 1)
}
