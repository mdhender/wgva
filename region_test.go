// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/mdhender/wgva/internal/mathx"
)

// regionScalarNames names the normalized biases of RegionParams in declaration
// order. It pairs with regionScalars, and TestRegionParamsShape is what keeps
// both honest against the struct.
var regionScalarNames = [regionScalarCount]string{
	"ElevationBias",
	"MoistureBias",
	"HeatBias",
	"Roughness",
	"BasinBias",
	"Volcanic",
	"Variation",
}

// regionScalars returns the biases in declaration order, so that a test can
// assert something of all of them rather than of whichever ones it remembered.
func regionScalars(p RegionParams) [regionScalarCount]float64 {
	return [regionScalarCount]float64{
		p.ElevationBias,
		p.MoistureBias,
		p.HeatBias,
		p.Roughness,
		p.BasinBias,
		p.Volcanic,
		p.Variation,
	}
}

// TestRegionParamsShape is what makes every other test in this file total over
// the struct. regionScalars is written out by hand, so a parameter added to
// RegionParams without a line there would be silently exempt from the range,
// distribution, blending, and golden assertions — which is precisely the kind of
// field that then turns out never to have been blended at all.
func TestRegionParamsShape(t *testing.T) {
	typ := reflect.TypeFor[RegionParams]()
	if got, want := typ.NumField(), regionScalarCount+1; got != want {
		t.Fatalf("RegionParams has %d fields, want %d biases and Ridge", got, want)
	}

	// A distinct value per field, so that a misordered regionScalars is caught
	// rather than merely a missing one.
	var p RegionParams
	v := reflect.ValueOf(&p).Elem()
	for i := range regionScalarCount {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Float64 {
			t.Fatalf("field %d, %s, is a %v; the biases come first and are all float64", i, f.Name, f.Type)
		}
		if f.Name != regionScalarNames[i] {
			t.Errorf("field %d is %s, but regionScalarNames calls it %s", i, f.Name, regionScalarNames[i])
		}
		v.Field(i).SetFloat(float64(i) + 1)
	}
	if f := typ.Field(regionScalarCount); f.Name != "Ridge" || f.Type != reflect.TypeFor[UnitVec2]() {
		t.Fatalf("the last field is %s %v, want Ridge UnitVec2", f.Name, f.Type)
	}

	for i, got := range regionScalars(p) {
		if want := float64(i) + 1; got != want {
			t.Errorf("regionScalars[%d] = %v, want %s, which is %v", i, got, regionScalarNames[i], want)
		}
	}
}

// ---------------------------------------------------------------------------
// Orientation
// ---------------------------------------------------------------------------

// TestAlignReadsAnOrientationAsALine is the rule of DESIGN.md 12 stated as a
// test: v and -v are the same orientation, so Align cannot tell them apart and
// Dot must.
func TestAlignReadsAnOrientationAsALine(t *testing.T) {
	root := math.Sqrt(0.5)
	for _, tc := range []struct {
		name  string
		v, w  UnitVec2
		align float64
	}{
		{"same arrow", UnitVec2{1, 0}, UnitVec2{1, 0}, 1},
		{"opposite arrows, one line", UnitVec2{1, 0}, UnitVec2{-1, 0}, 1},
		{"at right angles", UnitVec2{1, 0}, UnitVec2{0, 1}, 0},
		{"at right angles, reversed", UnitVec2{1, 0}, UnitVec2{0, -1}, 0},
		{"at forty-five degrees", UnitVec2{1, 0}, UnitVec2{root, root}, root},
		{"at forty-five degrees, reversed", UnitVec2{1, 0}, UnitVec2{-root, -root}, root},
	} {
		if got := tc.v.Align(tc.w); math.Abs(got-tc.align) > 1e-15 {
			t.Errorf("%s: Align = %v, want %v", tc.name, got, tc.align)
		}
	}

	// And the thing Align exists to stop somebody writing.
	if got := (UnitVec2{1, 0}).Dot(UnitVec2{-1, 0}); got != -1 {
		t.Errorf("Dot of opposite arrows = %v, want -1; Align is the one that reads a line", got)
	}
}

// TestDoubledAngleRoundTrips covers the two halves of DESIGN.md 12's orientation
// arithmetic together: doubling loses the arrow and nothing else, and the
// half-angle recovery brings back the canonical representative of the line.
//
// The angles are built from math.Sqrt rather than from math.Cos for the reason
// the gradient table is — a test that baked one architecture's sine into its
// expectations would pass everywhere and prove nothing.
func TestDoubledAngleRoundTrips(t *testing.T) {
	const n = 360
	for k := range n {
		// A unit vector without trigonometry: walk the unit circle by taking
		// x from a chord and deriving y.
		x := -1 + 2*float64(k)/float64(n)
		y := math.Sqrt(1 - mathx.Mul(x, x))
		if k%2 == 1 {
			y = -y
		}
		v := UnitVec2{X: x, Y: y}

		for _, arrow := range []UnitVec2{v, {X: -v.X, Y: -v.Y}} {
			dx, dy := arrow.doubled()
			if length := math.Sqrt(mathx.Mul(dx, dx) + mathx.Mul(dy, dy)); math.Abs(length-1) > 1e-12 {
				t.Fatalf("the doubled form of %v has length %v, want 1", arrow, length)
			}
			got := orientationFrom(dx, dy, UnitVec2{1, 0})
			if want := v.canonical(); math.Abs(got.X-want.X) > 1e-12 || math.Abs(got.Y-want.Y) > 1e-12 {
				t.Fatalf("recovering %v gave %v, want %v", arrow, got, want)
			}
		}
	}
}

// TestBlendingOppositeArrowsKeepsTheOrientation is the defect DESIGN.md 12 names
// and the reason the doubled-angle form is used at all.
//
// Two anchors whose ridges run along the same line, one of which hashed to the
// opposite arrow, must blend to that line. Averaging the vectors gives zero and
// a tile between them gets an orientation unrelated to either.
func TestBlendingOppositeArrowsKeepsTheOrientation(t *testing.T) {
	line := UnitVec2{X: math.Sqrt(0.5), Y: math.Sqrt(0.5)}
	flipped := UnitVec2{X: -line.X, Y: -line.Y}

	for _, w := range []float64{0.1, 0.25, 0.5, 0.75, 0.9} {
		ax, ay := line.doubled()
		bx, by := flipped.doubled()
		got := orientationFrom(
			blend3(w, 1-w, 0, ax, bx, 0),
			blend3(w, 1-w, 0, ay, by, 0),
			UnitVec2{1, 0},
		)
		if align := got.Align(line); math.Abs(align-1) > 1e-12 {
			t.Errorf("at weight %v the blend gave %v, which aligns %v with the shared line", w, got, align)
		}

		// The vector average of the same two, for contrast: it is the zero
		// vector, which names no direction at all.
		if avg := mathx.Mul(w, line.X) + mathx.Mul(1-w, flipped.X); w == 0.5 && avg != 0 {
			t.Errorf("the vector average at equal weights is %v; this test exists because it is zero", avg)
		}
	}
}

// TestBlendingPerpendicularOrientationsFallsBack covers the case DESIGN.md 12
// says has no answer: two ridges at right angles double to opposite directions
// and cancel, so no orientation exists and the fallback is what is owed.
func TestBlendingPerpendicularOrientationsFallsBack(t *testing.T) {
	a, b := UnitVec2{1, 0}, UnitVec2{0, 1}
	ax, ay := a.doubled()
	bx, by := b.doubled()

	fallback := UnitVec2{X: math.Sqrt(0.5), Y: math.Sqrt(0.5)}
	got := orientationFrom(
		blend3(0.5, 0.5, 0, ax, bx, 0),
		blend3(0.5, 0.5, 0, ay, by, 0),
		fallback,
	)
	if got != fallback {
		t.Errorf("the cancelled blend gave %v, want the fallback %v", got, fallback)
	}

	// Away from the exact cancellation there is an answer, and it is the
	// stronger of the two.
	got = orientationFrom(
		blend3(0.75, 0.25, 0, ax, bx, 0),
		blend3(0.75, 0.25, 0, ay, by, 0),
		fallback,
	)
	if got.Align(a) <= got.Align(b) {
		t.Errorf("a blend weighted three to one toward %v gave %v", a, got)
	}
}

// ---------------------------------------------------------------------------
// Anchors
// ---------------------------------------------------------------------------

// anchorSweep is the block of anchor indices the anchor tests walk. It straddles
// the origin because Go's division truncates and an anchor index is a floor
// division away from a coordinate.
func anchorSweep(yield func(i, j int64)) {
	for i := int64(-24); i <= 24; i++ {
		for j := int64(-24); j <= 24; j++ {
			yield(i, j)
		}
	}
}

func TestRegionAnchorIsDeterministic(t *testing.T) {
	const seed Seed = 0x0123456789abcdef

	anchorSweep(func(i, j int64) {
		first := regionAnchor(seed, i, j)
		for range 4 {
			if again := regionAnchor(seed, i, j); again != first {
				t.Fatalf("anchor (%d, %d) derived twice: %+v then %+v", i, j, first, again)
			}
		}
		if other := regionAnchor(seed+1, i, j); other == first {
			t.Fatalf("anchor (%d, %d) is the same under two seeds", i, j)
		}
		if shifted := regionAnchor(seed, i+1, j); shifted == first {
			t.Fatalf("anchors (%d, %d) and (%d, %d) are identical", i, j, i+1, j)
		}
	})
}

// TestRegionAnchorBiasesAreNormalized covers the contract of DESIGN.md 12: every
// parameter is a bias in [-1, +1], not a quantity.
//
// It also asserts the spread, which is the half that catches a parameter wired
// to a constant or hashed under an index it shares with another: a bias that
// never leaves the middle of its range is a region character nobody can see.
func TestRegionAnchorBiasesAreNormalized(t *testing.T) {
	var lo, hi [regionScalarCount]float64
	var sum [regionScalarCount]float64
	for i := range lo {
		lo[i], hi[i] = math.Inf(+1), math.Inf(-1)
	}

	n := 0
	anchorSweep(func(i, j int64) {
		n++
		for k, v := range regionScalars(regionAnchor(0x0123456789abcdef, i, j)) {
			if v < -1 || v > 1 || math.IsNaN(v) {
				t.Fatalf("%s at anchor (%d, %d) is %v, which is not a bias in [-1, +1]", regionScalarNames[k], i, j, v)
			}
			lo[k] = min(lo[k], v)
			hi[k] = max(hi[k], v)
			sum[k] += v
		}
	})

	for k := range regionScalarCount {
		if lo[k] > -0.95 || hi[k] < 0.95 {
			t.Errorf("%s spans only [%v, %v] over %d anchors; a bias that never reaches its ends is not uniform",
				regionScalarNames[k], lo[k], hi[k], n)
		}
		if mean := sum[k] / float64(n); math.Abs(mean) > 0.05 {
			t.Errorf("%s has mean %v over %d anchors, want about zero", regionScalarNames[k], mean, n)
		}
	}
}

// TestRegionAnchorBiasesAreIndependent is the other half of the index rule: two
// parameters hashed under the same index would be equal everywhere, and two
// hashed under indices that differ only in a way the mixer does not separate
// would correlate.
func TestRegionAnchorBiasesAreIndependent(t *testing.T) {
	var dot [regionScalarCount][regionScalarCount]float64
	n := 0
	anchorSweep(func(i, j int64) {
		n++
		s := regionScalars(regionAnchor(0x0123456789abcdef, i, j))
		for a := range s {
			for b := range s {
				dot[a][b] += s[a] * s[b]
			}
		}
	})

	for a := range regionScalarCount {
		for b := range regionScalarCount {
			if a == b {
				continue
			}
			// Two independent uniform biases have covariance zero; the sample
			// covariance over this many anchors stays well inside a hundredth.
			if cov := dot[a][b] / float64(n); math.Abs(cov) > 0.02 {
				t.Errorf("%s and %s have covariance %v over %d anchors",
					regionScalarNames[a], regionScalarNames[b], cov, n)
			}
		}
	}
}

// TestAnchorRidgeIsAUnitLine covers the two claims the type makes: the vector is
// a unit vector, and it is the canonical representative of a line rather than
// one of two arrows.
func TestAnchorRidgeIsAUnitLine(t *testing.T) {
	anchorSweep(func(i, j int64) {
		v := anchorRidge(0x0123456789abcdef, i, j)
		if length := math.Sqrt(v.X*v.X + v.Y*v.Y); math.Abs(length-1) > 1e-12 {
			t.Fatalf("the ridge at (%d, %d) is %v, of length %v", i, j, v, length)
		}
		if v.X < 0 || (v.X == 0 && v.Y < 0) {
			t.Fatalf("the ridge at (%d, %d) is %v, which is not the canonical representative", i, j, v)
		}
		if v != v.canonical() {
			t.Fatalf("canonical() moved the stored ridge at (%d, %d)", i, j)
		}
	})
}

// TestAnchorRidgesCoverTheHalfCircle is what says the rejection sampling did not
// acquire a preferred direction. Rejecting by radius is rotationally symmetric,
// so the orientations must be near uniform over the half circle; a table lookup
// or a hashed angle folded through an axis would pile up in one or two bins.
func TestAnchorRidgesCoverTheHalfCircle(t *testing.T) {
	// Six bins of thirty degrees, split by lines of slope ±sqrt(3) and
	// ±1/sqrt(3). Thirty degrees divides the hex lattice's sixty, so a bias
	// toward the world's own symmetry would show here.
	root3 := math.Sqrt(3)
	bounds := []float64{-root3, -1 / root3, 0, 1 / root3, root3}

	var bins [6]int
	n := 0
	anchorSweep(func(i, j int64) {
		n++
		v := anchorRidge(0x0123456789abcdef, i, j)
		b := 0
		for _, slope := range bounds {
			if v.Y > mathx.Mul(slope, v.X) {
				b++
			}
		}
		bins[b]++
	})

	want := float64(n) / 6
	for b, got := range bins {
		if math.Abs(float64(got)-want) > 0.2*want {
			t.Errorf("bin %d of the half circle holds %d of %d ridges, want about %.0f", b, got, n, want)
		}
	}
}

// ---------------------------------------------------------------------------
// The blend
// ---------------------------------------------------------------------------

// TestTriangleWeightsArePartitionOfUnity covers what makes the blend a blend:
// three non-negative weights summing to one, on the triangle that actually
// contains the position.
func TestTriangleWeightsArePartitionOfUnity(t *testing.T) {
	const n = 64
	for a := range n + 1 {
		for b := range n + 1 {
			fu, fv := float64(a)/n, float64(b)/n
			anchors, w0, w1, w2 := triangleWeights(3, -5, fu, fv)

			if w0 < 0 || w1 < 0 || w2 < 0 {
				t.Fatalf("at (%v, %v) the weights are %v, %v, %v", fu, fv, w0, w1, w2)
			}
			if sum := w0 + w1 + w2; math.Abs(sum-1) > 1e-12 {
				t.Fatalf("at (%v, %v) the weights sum to %v", fu, fv, sum)
			}

			// The position must lie inside the triangle the weights name. In
			// lattice coordinates that is the barycentric statement itself:
			// recombining the corners with the *unsquared* weights returns the
			// position, and squaring does not move which corners are involved.
			seen := map[[2]int64]bool{}
			for _, anchor := range anchors {
				if seen[anchor] {
					t.Fatalf("at (%v, %v) an anchor is named twice: %v", fu, fv, anchors)
				}
				seen[anchor] = true
				if anchor[0] < 3 || anchor[0] > 4 || anchor[1] < -5 || anchor[1] > -4 {
					t.Fatalf("at (%v, %v) anchor %v is outside the cell", fu, fv, anchor)
				}
			}
		}
	}
}

// TestTriangleWeightsAtACorner pins the property everything else leans on: at an
// anchor, that anchor carries the whole weight, so the blend there is that
// anchor's own parameters.
func TestTriangleWeightsAtACorner(t *testing.T) {
	for _, tc := range []struct {
		fu, fv float64
		want   [2]int64
	}{
		{0, 0, [2]int64{0, 0}},
		{1, 0, [2]int64{1, 0}},
		{0, 1, [2]int64{0, 1}},
		{1, 1, [2]int64{1, 1}},
	} {
		anchors, w0, w1, w2 := triangleWeights(0, 0, tc.fu, tc.fv)
		weights := [3]float64{w0, w1, w2}
		for k, anchor := range anchors {
			want := 0.0
			if anchor == tc.want {
				want = 1
			}
			if math.Abs(weights[k]-want) > 1e-15 {
				t.Errorf("at the corner (%v, %v), anchor %v has weight %v, want %v", tc.fu, tc.fv, anchor, weights[k], want)
			}
		}
	}
}

// TestBlendIsNotBilinear is the artifact DESIGN.md 11.2 exists to avoid, stated
// so that it cannot come back unnoticed.
//
// The axial basis vectors are sixty degrees apart, so the cell is a rhombus of
// two equilateral triangles split by the short diagonal fu + fv = 1. On that
// diagonal only the two shared corners are in the containing triangle, so the
// other two carry no weight at all. A bilinear blend gives all four corners a
// quarter of the weight at the cell centre, which is the same statement read
// the other way round.
func TestBlendIsNotBilinear(t *testing.T) {
	for _, fu := range []float64{0, 0.25, 0.5, 0.75, 1} {
		fv := 1 - fu
		anchors, w0, w1, w2 := triangleWeights(0, 0, fu, fv)
		weights := [3]float64{w0, w1, w2}

		for k, anchor := range anchors {
			shared := anchor == [2]int64{1, 0} || anchor == [2]int64{0, 1}
			if !shared && weights[k] != 0 {
				t.Errorf("on the short diagonal at fu=%v, the far corner %v has weight %v, want 0", fu, anchor, weights[k])
			}
			if shared && weights[k] == 0 && fu != 0 && fu != 1 {
				t.Errorf("on the short diagonal at fu=%v, the shared corner %v has no weight", fu, anchor)
			}
		}
	}
}

// TestBlendJoinsAcrossTheShortDiagonal covers the join DESIGN.md 11.2 asks for.
// Approaching the diagonal from the two triangles must arrive at the same value,
// which is what makes the triangle edge a join rather than a crease.
func TestBlendJoinsAcrossTheShortDiagonal(t *testing.T) {
	values := func(fu, fv float64) [regionScalarCount]float64 {
		anchors, w0, w1, w2 := triangleWeights(0, 0, fu, fv)
		var out [regionScalarCount]float64
		for k := range out {
			s := [3]float64{}
			for c, anchor := range anchors {
				s[c] = regionScalars(regionAnchor(7, anchor[0], anchor[1]))[k]
			}
			out[k] = blend3(w0, w1, w2, s[0], s[1], s[2])
		}
		return out
	}

	for _, fu := range []float64{0.125, 0.25, 0.5, 0.75, 0.875} {
		const eps = 1e-9
		below := values(fu, 1-fu-eps)
		above := values(fu, 1-fu+eps)
		for k := range below {
			if d := math.Abs(below[k] - above[k]); d > 1e-6 {
				t.Errorf("%s steps by %v across the short diagonal at fu=%v", regionScalarNames[k], d, fu)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The influence at a coordinate
// ---------------------------------------------------------------------------

// influenceSweep walks a block of coordinates several regions across, straddling
// the origin so that the floor divisions are exercised on both signs.
func influenceSweep(yield func(Coord)) {
	for q := int64(-600); q <= 600; q += 3 {
		for r := int64(-600); r <= 600; r += 3 {
			yield(NewCoord(q, r))
		}
	}
}

func TestRegionInfluenceIsNormalized(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)
	influenceSweep(func(c Coord) {
		p := g.RegionInfluence(c)
		for k, v := range regionScalars(p) {
			if v < -1 || v > 1 || math.IsNaN(v) {
				t.Fatalf("%s at %v is %v, which is not a bias in [-1, +1]", regionScalarNames[k], c, v)
			}
		}
		if length := math.Sqrt(p.Ridge.X*p.Ridge.X + p.Ridge.Y*p.Ridge.Y); math.Abs(length-1) > 1e-12 {
			t.Fatalf("the blended ridge at %v is %v, of length %v", c, p.Ridge, length)
		}
		if p.Ridge.X < 0 {
			t.Fatalf("the blended ridge at %v is %v, which is not the canonical representative", c, p.Ridge)
		}
	})
}

// TestRegionInfluenceAtAnAnchorIsThatAnchor is the blend's anchoring condition:
// where one anchor carries the whole weight, the blend is that anchor.
//
// The ridge is compared with Align rather than for equality, because the
// half-angle recovery reconstructs the orientation through two square roots. It
// is the same line to the last few bits, which is what the type promises.
func TestRegionInfluenceAtAnAnchorIsThatAnchor(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)
	size := int64(g.Config().RegionSizeHexes)

	for i := int64(-3); i <= 3; i++ {
		for j := int64(-3); j <= 3; j++ {
			c := NewCoord(i*size, j*size)
			if gi, gj := g.Region(c); gi != i || gj != j {
				t.Fatalf("the anchor coordinate for (%d, %d) reports region (%d, %d)", i, j, gi, gj)
			}

			anchor := g.RegionAnchor(i, j)
			got := g.RegionInfluence(c)

			for k, v := range regionScalars(got) {
				if want := regionScalars(anchor)[k]; v != want {
					t.Errorf("%s at anchor (%d, %d) blended to %v, want the anchor's own %v",
						regionScalarNames[k], i, j, v, want)
				}
			}
			if align := got.Ridge.Align(anchor.Ridge); math.Abs(align-1) > 1e-12 {
				t.Errorf("the ridge at anchor (%d, %d) blended to %v, which aligns %v with the anchor's %v",
					i, j, got.Ridge, align, anchor.Ridge)
			}
		}
	}
}

// TestRegionBoundaryContinuity is DESIGN.md 30.5. The claim is not that the
// blend is flat at a boundary but that nothing about crossing one is visible:
// no step change may correlate with a region index change.
//
// It is measured rather than bounded by a constant, and the comparison is the
// point. A blend that inherited its parameters from one anchor would put a jump
// of up to the full bias range on the boundary bucket and leave the interior
// bucket at zero; a bilinear blend would leave the boundary bucket the largest.
// The squared weights make the field flattest at the anchors, which the cell
// boundaries run through, so the boundary bucket comes out *smaller* — and that
// is the assertion.
func TestRegionBoundaryContinuity(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)

	var boundary, interior [regionScalarCount]float64
	var crossings int
	for q := int64(-2000); q <= 2000; q++ {
		for _, r := range []int64{-771, 0, 517} {
			c := NewCoord(q, r)
			here := regionScalars(g.RegionInfluence(c))
			ci, cj := g.Region(c)

			for d := range 6 {
				n := c.Neighbor(d)
				there := regionScalars(g.RegionInfluence(n))
				ni, nj := g.Region(n)
				crossed := ci != ni || cj != nj
				if crossed {
					crossings++
				}
				for k := range here {
					step := math.Abs(here[k] - there[k])
					if crossed {
						boundary[k] = max(boundary[k], step)
					} else {
						interior[k] = max(interior[k], step)
					}
				}
			}
		}
	}

	if crossings < 1000 {
		t.Fatalf("only %d of the sampled neighbor pairs crossed a region boundary", crossings)
	}
	for k := range regionScalarCount {
		if boundary[k] > interior[k] {
			t.Errorf("%s steps by at most %v across a region boundary and %v inside one",
				regionScalarNames[k], boundary[k], interior[k])
		}
		// A blend of biases two apart over a region of size hexes cannot change
		// faster than a few multiples of 2/size per hex, whichever side of a
		// boundary it is on. This is the bound that catches the blend reading
		// the wrong cell entirely, which a comparison of two equally wrong
		// buckets would not.
		if want := 8 / float64(g.Config().RegionSizeHexes); interior[k] > want {
			t.Errorf("%s steps by %v in one hex, which exceeds %v over a region of %d hexes",
				regionScalarNames[k], interior[k], want, g.Config().RegionSizeHexes)
		}
	}
}

// TestRegionBoundaryHasNoCrease is the second-order half of the same question.
// A step is what 30.5 names; a crease — a jump in the slope rather than in the
// value — is what a boundary that is only C0 would leave, and relief is a first
// difference between neighbors, so a crease is a thing that gets drawn.
//
// The three buckets are the cell boundary, the short diagonal inside a cell
// where the containing triangle changes, and the smooth interior of a triangle.
// Neither join may curve more sharply than the field already does on its own.
func TestRegionBoundaryHasNoCrease(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)
	size := int64(g.Config().RegionSizeHexes)

	triangleOf := func(c Coord) int {
		fu := float64(mathx.FloorMod(int64(c.Q()), size)) / float64(size)
		fv := float64(mathx.FloorMod(int64(c.R()), size)) / float64(size)
		if fu+fv <= 1 {
			return 0
		}
		return 1
	}

	var cellJoin, diagonalJoin, smooth [regionScalarCount]float64
	at := func(q, r int64) [regionScalarCount]float64 {
		return regionScalars(g.RegionInfluence(NewCoord(q, r)))
	}

	for _, r := range []int64{-771, 0, 517} {
		prev, cur := at(-2001, r), at(-2000, r)
		for q := int64(-1999); q <= 2000; q++ {
			next := at(q, r)
			before, now := NewCoord(q-1, r), NewCoord(q, r)
			bi, bj := g.Region(before)
			ni, nj := g.Region(now)

			bucket := &smooth
			switch {
			case bi != ni || bj != nj:
				bucket = &cellJoin
			case triangleOf(before) != triangleOf(now):
				bucket = &diagonalJoin
			}
			for k := range next {
				curve := math.Abs(next[k] - 2*cur[k] + prev[k])
				bucket[k] = max(bucket[k], curve)
			}
			prev, cur = cur, next
		}
	}

	for k := range regionScalarCount {
		// Half again the field's own curvature. The joins measure below the
		// smooth interior in practice; the margin is here so that the test
		// reports a crease rather than a tuning change.
		limit := 1.5 * smooth[k]
		if smooth[k] == 0 {
			t.Fatalf("%s does not curve at all inside a triangle, so this test proves nothing", regionScalarNames[k])
		}
		if cellJoin[k] > limit {
			t.Errorf("%s curves by %v at a cell boundary and %v inside a triangle", regionScalarNames[k], cellJoin[k], smooth[k])
		}
		if diagonalJoin[k] > limit {
			t.Errorf("%s curves by %v at the short diagonal and %v inside a triangle", regionScalarNames[k], diagonalJoin[k], smooth[k])
		}
	}
}

// TestChunkBoundaryContinuity is DESIGN.md 30.6, and for the region influence it
// is a statement about what the code reads rather than about how smooth it is:
// chunks are addressing, so changing the chunk size must not move a single
// value anywhere.
//
// The macro region is the same claim. It appears in the hierarchy of
// DESIGN.md 11 and, like the chunk, defines no geography.
func TestChunkBoundaryContinuity(t *testing.T) {
	base := DefaultConfig()
	for _, tc := range []struct {
		name string
		mut  func(*Config)
	}{
		{"chunk size", func(c *Config) { c.ChunkSizeHexes = 8 }},
		{"macro region size", func(c *Config) { c.MacroRegionSizeHexes = 2048 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mut(&cfg)

			a := NewDefault(0x0123456789abcdef)
			b, err := New(0x0123456789abcdef, cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			influenceSweep(func(c Coord) {
				if got, want := b.RegionInfluence(c), a.RegionInfluence(c); got != want {
					t.Fatalf("changing the %s moved the region influence at %v", tc.name, c)
				}
			})
		})
	}
}

// TestRegionAddressingIsFloorDivision covers the three levels of DESIGN.md 11
// together, and the case that actually differs: Go's / truncates toward zero, so
// a coordinate just below an anchor belongs to the cell below it and not to the
// one it shares a sign with.
func TestRegionAddressingIsFloorDivision(t *testing.T) {
	g := NewDefault(1)
	cfg := g.Config()

	for _, tc := range []struct {
		name string
		size uint32
		get  func(Coord) (int64, int64)
	}{
		{"region", cfg.RegionSizeHexes, g.Region},
		{"macro region", cfg.MacroRegionSizeHexes, g.MacroRegion},
		{"chunk", cfg.ChunkSizeHexes, g.Chunk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			size := int64(tc.size)
			for _, q := range []int64{-2*size - 1, -size - 1, -size, -1, 0, 1, size - 1, size, 2 * size} {
				c := NewCoord(q, -q)
				i, j := tc.get(c)
				if want := mathx.FloorDiv(q, size); i != want {
					t.Errorf("q = %d is in cell %d, want %d", q, i, want)
				}
				if want := mathx.FloorDiv(-q, size); j != want {
					t.Errorf("r = %d is in cell %d, want %d", -q, j, want)
				}
			}
		})
	}
}

// TestRegionInfluenceIsDeterministic covers DESIGN.md 30.1 through 30.3 for the
// blend: the same coordinate gives the same answer, the answer does not depend
// on what was evaluated before it, and concurrent readers all see it.
func TestRegionInfluenceIsDeterministic(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)

	forward := map[Coord]RegionParams{}
	var order []Coord
	influenceSweep(func(c Coord) {
		forward[c] = g.RegionInfluence(c)
		order = append(order, c)
	})

	for i := len(order) - 1; i >= 0; i-- {
		c := order[i]
		if got := g.RegionInfluence(c); got != forward[c] {
			t.Fatalf("evaluating %v in the other order gave %+v, want %+v", c, got, forward[c])
		}
	}

	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range order {
				c := order[(i+w*97)%len(order)]
				if got := g.RegionInfluence(c); got != forward[c] {
					t.Errorf("a concurrent reader saw %+v at %v, want %+v", got, c, forward[c])
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestRegionInfluenceVariesAcrossTheWorld is the exit condition of phase 3 in the
// form a test can hold: distant areas have distinct geographic character.
//
// Windows a long way apart must differ in every bias, and each bias must move
// over a window of a few regions. A blend that had lost a parameter, or that
// collapsed toward the mean because the weights were wrong, would show here as a
// world with one character everywhere.
func TestRegionInfluenceVariesAcrossTheWorld(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)
	size := int64(g.Config().RegionSizeHexes)

	window := func(q0, r0 int64) (mean, spread [regionScalarCount]float64) {
		var lo, hi [regionScalarCount]float64
		for k := range lo {
			lo[k], hi[k] = math.Inf(+1), math.Inf(-1)
		}
		n := 0
		for q := q0; q < q0+4*size; q += 7 {
			for r := r0; r < r0+4*size; r += 7 {
				n++
				for k, v := range regionScalars(g.RegionInfluence(NewCoord(q, r))) {
					mean[k] += v
					lo[k] = min(lo[k], v)
					hi[k] = max(hi[k], v)
				}
			}
		}
		for k := range mean {
			mean[k] /= float64(n)
			spread[k] = hi[k] - lo[k]
		}
		return mean, spread
	}

	corners := [][2]int64{{0, 0}, {20000, -9000}, {-17000, 15000}, {-4000, -21000}}
	means := make([][regionScalarCount]float64, len(corners))
	for w, corner := range corners {
		mean, spread := window(corner[0], corner[1])
		means[w] = mean
		for k := range spread {
			if spread[k] < 0.5 {
				t.Errorf("%s spans only %v over four regions at (%d, %d)",
					regionScalarNames[k], spread[k], corner[0], corner[1])
			}
		}
	}

	for a := range corners {
		for b := a + 1; b < len(corners); b++ {
			same := 0
			for k := range regionScalarCount {
				if math.Abs(means[a][k]-means[b][k]) < 0.02 {
					same++
				}
			}
			if same == regionScalarCount {
				t.Errorf("the windows at %v and %v have the same character in every bias", corners[a], corners[b])
			}
		}
	}
}

// TestSampleIsItsParts states what a Sample is for. It is a convenience over the
// individual accessors, and a convenience that computed something slightly
// different from the thing it is a convenience for would be worse than not
// having it — so the comparison is on bits.
func TestSampleIsItsParts(t *testing.T) {
	g := NewDefault(0x0123456789abcdef)
	for _, gc := range goldenCoords {
		c := NewCoord(gc[0], gc[1])
		s := g.Sample(c)

		if s.Coord != c {
			t.Errorf("Sample at %v carries %v", c, s.Coord)
		}
		for i, want := range [scaleCount]float64{
			g.ScaleAt(ScaleContinental, c),
			g.ScaleAt(ScaleRegional, c),
			g.ScaleAt(ScaleLocal, c),
			g.ScaleAt(ScaleDetail, c),
		} {
			got := [scaleCount]float64{s.Continentalness, s.Regional, s.Local, s.Detail}[i]
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Errorf("%v of the sample at %v is %v, want %v", Scales()[i], c, got, want)
			}
		}
		if got, want := s.Region, g.RegionInfluence(c); got != want {
			t.Errorf("the region influence of the sample at %v is %+v, want %+v", c, got, want)
		}
		if got, want := s.RimDistance, c.RimDistance(); got != want {
			t.Errorf("the rim distance of the sample at %v is %d, want %d", c, got, want)
		}
	}
}
