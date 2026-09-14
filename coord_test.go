// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"math/big"
	"math/rand/v2"
	"reflect"
	"testing"
)

func TestOriginIsTheZeroValue(t *testing.T) {
	var zero Coord
	if Origin != zero {
		t.Fatalf("Origin = %v, want the zero value", Origin)
	}
	if Origin.Q() != 0 || Origin.R() != 0 || Origin.S() != 0 {
		t.Fatalf("Origin components = (%d, %d, %d), want (0, 0, 0)", Origin.Q(), Origin.R(), Origin.S())
	}
	if got := NewCoord(0, 0); got != Origin {
		t.Fatalf("NewCoord(0, 0) = %v, want Origin", got)
	}
}

func TestCoordComponentsSumToZero(t *testing.T) {
	for _, c := range sampleCoords(t, 4096) {
		if got := int64(c.Q()) + int64(c.R()) + int64(c.S()); got != 0 {
			t.Fatalf("%v: q+r+s = %d, want 0", c, got)
		}
	}
}

// TestCanonicalDomain is DESIGN.md 30.4's symmetric-domain sweep: over a large
// set of inputs, everything NewCoord produces is inside ±WorldRadius on all
// three axes, negation of any result stays in the domain, and RimDistance is
// never negative.
//
// The bound is the only thing enforcing this. Component is wider than the
// domain (DESIGN.md 4.2), so an escaped coordinate would be visible here rather
// than truncated into a plausible one — which is what this sweep is looking
// for.
func TestCanonicalDomain(t *testing.T) {
	for _, c := range sampleCoords(t, 200_000) {
		for _, v := range [3]Component{c.Q(), c.R(), c.S()} {
			if int64(v) < -WorldRadius || int64(v) > WorldRadius {
				t.Fatalf("%v: component %d outside ±%d", c, v, WorldRadius)
			}
			if int64(-v) != -int64(v) {
				t.Fatalf("%v: component %d does not negate cleanly", c, v)
			}
		}
		if d := c.RimDistance(); d < 0 {
			t.Fatalf("%v: RimDistance = %d, want non-negative", c, d)
		}
	}
}

func TestCornersAreCanonicalAndDistinct(t *testing.T) {
	corner := NewCoord(-WorldRadius, 0)
	seen := make(map[Coord]int, 6)
	for steps := range 6 {
		c := corner.Rotate(steps)
		if d := c.RimDistance(); d != 0 {
			t.Fatalf("rotation %d of the corner has RimDistance %d, want 0", steps, d)
		}
		if prev, dup := seen[c]; dup {
			t.Fatalf("rotation %d equals rotation %d at %v", steps, prev, c)
		}
		seen[c] = steps

		// Negating a canonical coordinate componentwise stays in range, which is
		// what the symmetric domain buys.
		neg := NewCoord(-int64(c.Q()), -int64(c.R()))
		if neg.Q() != -c.Q() || neg.R() != -c.R() {
			t.Fatalf("negation of %v renormalized to %v", c, neg)
		}
	}
}

// ---------------------------------------------------------------------------
// Directions, neighbors, rotation
// ---------------------------------------------------------------------------

func TestDirectionTable(t *testing.T) {
	// Transcribed from DESIGN.md appendix A rather than read from the code.
	want := [6][3]int64{
		{+1, 0, -1},
		{+1, -1, 0},
		{0, -1, +1},
		{-1, 0, +1},
		{-1, +1, 0},
		{0, +1, -1},
	}
	for d, w := range want {
		q, r := directions[d][0], directions[d][1]
		if q != w[0] || r != w[1] {
			t.Fatalf("direction %d = (%d, %d), want (%d, %d)", d, q, r, w[0], w[1])
		}
		if s := -q - r; s != w[2] {
			t.Fatalf("direction %d has s = %d, want %d", d, s, w[2])
		}
	}
}

func TestNormalizeDirection(t *testing.T) {
	// The table in DESIGN.md appendix A.
	for in, want := range map[int]int{7: 1, 6: 0, -1: 5, -2: 4, -6: 0, -7: 5, 0: 0, 5: 5} {
		if got := normalizeDirection(in); got != want {
			t.Fatalf("normalizeDirection(%d) = %d, want %d", in, got, want)
		}
	}
	for d := -600; d <= 600; d++ {
		n := normalizeDirection(d)
		if n < 0 || n > 5 {
			t.Fatalf("normalizeDirection(%d) = %d, outside 0..5", d, n)
		}
		if (d-n)%6 != 0 {
			t.Fatalf("normalizeDirection(%d) = %d, which does not differ by a multiple of six", d, n)
		}
	}
}

func TestRotateOnceAdvancesTheDirectionTable(t *testing.T) {
	for d := range 6 {
		q, r := directions[d][0], directions[d][1]
		x, y, z := rotateOnce(q, r, -q-r)
		next := directions[(d+1)%6]
		if x != next[0] || y != next[1] {
			t.Fatalf("rotateOnce(direction %d) = (%d, %d), want (%d, %d)", d, x, y, next[0], next[1])
		}
		if x+y+z != 0 {
			t.Fatalf("rotateOnce(direction %d) does not sum to zero", d)
		}
	}
}

func TestRotateIsPeriodicAndInvertible(t *testing.T) {
	for _, c := range sampleCoords(t, 2048) {
		if got := c.Rotate(6); got != c {
			t.Fatalf("%v.Rotate(6) = %v, want the original", c, got)
		}
		if got := c.Rotate(0); got != c {
			t.Fatalf("%v.Rotate(0) = %v, want the original", c, got)
		}
		for steps := -13; steps <= 13; steps++ {
			if got, want := c.Rotate(steps), c.Rotate(normalizeDirection(steps)); got != want {
				t.Fatalf("%v.Rotate(%d) = %v, want %v", c, steps, got, want)
			}
			// Rotation commutes with RimDistance, because max(|q|,|r|,|s|) is
			// invariant under a permutation with sign changes. That is why a
			// rotated grid view shows the rim in the same place.
			if got, want := c.Rotate(steps).RimDistance(), c.RimDistance(); got != want {
				t.Fatalf("%v.Rotate(%d).RimDistance() = %d, want %d", c, steps, got, want)
			}
		}
	}
}

func TestNeighborsAreAdjacentAndDistinct(t *testing.T) {
	for _, c := range sampleCoords(t, 4096) {
		seen := make(map[Coord]bool, 6)
		for d := range 6 {
			n := c.Neighbor(d)
			if n == c {
				t.Fatalf("%v: neighbor %d is the coordinate itself", c, d)
			}
			if seen[n] {
				t.Fatalf("%v: neighbor %d repeats an earlier neighbor", c, d)
			}
			seen[n] = true

			// Stepping back the other way returns to the start. Across the seam
			// this is a statement about wrapping, not about arithmetic.
			if back := n.Neighbor(d + 3); back != c {
				t.Fatalf("%v: neighbor %d then %d = %v, want the original", c, d, d+3, back)
			}
		}
	}
}

func TestNeighborAcceptsAnyDirection(t *testing.T) {
	c := NewCoord(17, -5)
	for d := -30; d <= 30; d++ {
		if got, want := c.Neighbor(d), c.Neighbor(normalizeDirection(d)); got != want {
			t.Fatalf("Neighbor(%d) = %v, want %v", d, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// World space
// ---------------------------------------------------------------------------

func TestAxialToWorldSpacing(t *testing.T) {
	if got := AxialToWorld(Origin); got != (Vec2{}) {
		t.Fatalf("AxialToWorld(Origin) = %v, want the zero vector", got)
	}
	// Adjacent hex centers are exactly 6 miles apart in every direction.
	for d := range 6 {
		a := AxialToWorld(Origin)
		b := AxialToWorld(Origin.Neighbor(d))
		dx, dy := b.X-a.X, b.Y-a.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if math.Abs(dist-hexCenterDistanceMiles) > 1e-9 {
			t.Fatalf("direction %d: center distance %v miles, want %v", d, dist, hexCenterDistanceMiles)
		}
	}
	// The embedding is the one in DESIGN.md 7, derived here rather than read
	// from the implementation.
	for _, c := range sampleCoords(t, 4096) {
		q, r := float64(c.Q()), float64(c.R())
		wantX := 6*q + 3*r
		wantY := 3 * math.Sqrt(3) * r
		got := AxialToWorld(c)
		if math.Abs(got.X-wantX) > 1e-6 || math.Abs(got.Y-wantY) > 1e-6 {
			t.Fatalf("%v: AxialToWorld = %v, want approximately (%v, %v)", c, got, wantX, wantY)
		}
	}
}

// ---------------------------------------------------------------------------
// Cells
// ---------------------------------------------------------------------------

func TestCellMatchesTheWorkedExample(t *testing.T) {
	// The table in DESIGN.md 23, at chunk size 32.
	for q, want := range map[int64]int64{0: 0, 31: 0, 32: 1, -1: -1, -32: -1, -33: -2} {
		c := NewCoord(q, 0)
		got, _ := c.Cell(32)
		if got != want {
			t.Fatalf("Cell(32) for q = %d gave %d, want %d", q, got, want)
		}
	}
}

func TestCellAndOffsetReconstructTheCoordinate(t *testing.T) {
	for _, size := range []uint32{1, 2, 32, 128, 512, 1000} {
		for _, c := range sampleCoords(t, 2048) {
			cq, cr := c.Cell(size)
			oq, or := c.CellOffset(size)
			if oq < 0 || oq >= int64(size) || or < 0 || or >= int64(size) {
				t.Fatalf("%v at size %d: offset (%d, %d) outside [0, %d)", c, size, oq, or, size)
			}
			if got := cq*int64(size) + oq; got != int64(c.Q()) {
				t.Fatalf("%v at size %d: q reconstructed as %d", c, size, got)
			}
			if got := cr*int64(size) + or; got != int64(c.R()) {
				t.Fatalf("%v at size %d: r reconstructed as %d", c, size, got)
			}
		}
	}
}

func TestCellRejectsZeroSize(t *testing.T) {
	t.Run("Cell", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("Cell(0) did not panic")
			}
		}()
		Origin.Cell(0)
	})
	t.Run("CellOffset", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("CellOffset(0) did not panic")
			}
		}()
		Origin.CellOffset(0)
	})
}

// ---------------------------------------------------------------------------
// The rim
// ---------------------------------------------------------------------------

func TestRimDistanceAgreesWithHexDistance(t *testing.T) {
	// The six corners of the canonical hexagon are on the outermost ring.
	corners := [6][2]int64{
		{WorldRadius, 0},
		{WorldRadius, -WorldRadius},
		{0, -WorldRadius},
		{-WorldRadius, 0},
		{-WorldRadius, WorldRadius},
		{0, WorldRadius},
	}
	for i, c := range corners {
		if got := NewCoord(c[0], c[1]).RimDistance(); got != 0 {
			t.Fatalf("corner %d: RimDistance = %d, want 0", i, got)
		}
	}

	// Walking each edge between adjacent corners stays on the outermost ring.
	for i := range corners {
		a, b := corners[i], corners[(i+1)%6]
		for step := int64(0); step <= 16; step++ {
			q := a[0] + (b[0]-a[0])*step/16
			r := a[1] + (b[1]-a[1])*step/16
			if got := NewCoord(q, r).RimDistance(); got != 0 {
				t.Fatalf("edge %d step %d at (%d, %d): RimDistance = %d, want 0", i, step, q, r, got)
			}
		}
	}

	// And a radial walk inward decreases the distance by exactly one per step.
	for d := range 6 {
		c := NewCoord(corners[d][0], corners[d][1])
		for step := int64(0); step < 64; step++ {
			if got := c.RimDistance(); got != step {
				t.Fatalf("direction %d step %d: RimDistance = %d, want %d", d, step, got, step)
			}
			c = c.Neighbor(d + 3)
		}
	}

	// Independently: hex distance from the origin is half the sum of the
	// magnitudes for cube coordinates summing to zero.
	for _, c := range sampleCoords(t, 20_000) {
		q, r, s := int64(c.Q()), int64(c.R()), int64(c.S())
		dist := (abs64(q) + abs64(r) + abs64(s)) / 2
		if got, want := c.RimDistance(), WorldRadius-dist; got != want {
			t.Fatalf("%v: RimDistance = %d, want %d", c, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Normalization
// ---------------------------------------------------------------------------

func TestMirrorCentersAreTheSixRotations(t *testing.T) {
	// Recomputed here from the definition rather than read from the package
	// table, which is the check that the init-time generation is what it claims.
	n := WorldRadius
	want := [6][3]int64{{2*n + 1, -n, -n - 1}}
	for i := 1; i < 6; i++ {
		x, y, z := want[i-1][0], want[i-1][1], want[i-1][2]
		want[i] = [3]int64{-z, -x, -y}
	}
	if mirrorCenters != want {
		t.Fatalf("mirrorCenters = %v, want %v", mirrorCenters, want)
	}
	for i, m := range mirrorCenters {
		if m[0]+m[1]+m[2] != 0 {
			t.Fatalf("mirror center %d does not sum to zero: %v", i, m)
		}
		// Opposite centers negate, which is the six-fold symmetry of the domain.
		o := mirrorCenters[(i+3)%6]
		if m[0] != -o[0] || m[1] != -o[1] || m[2] != -o[2] {
			t.Fatalf("mirror center %d and %d are not opposites: %v, %v", i, (i+3)%6, m, o)
		}
		// Two of the six have components outside Component range at either
		// width, which is what the int64 intermediate rule protects.
		if i == 0 || i == 3 {
			if abs64(m[0]) <= WorldRadius {
				t.Fatalf("mirror center %d was expected to exceed Component range: %v", i, m)
			}
		}
	}
}

// TestNormalizeStage1 exercises the already-canonical path directly.
func TestNormalizeStage1(t *testing.T) {
	inside := [][2]int64{
		{0, 0}, {1, 0}, {0, 1}, {-1, 1}, {WorldRadius, 0}, {0, -WorldRadius},
		{-WorldRadius, WorldRadius}, {WorldRadius / 2, -WorldRadius / 3},
	}
	for _, c := range inside {
		if !isCanonical(c[0], c[1]) {
			t.Fatalf("(%d, %d) should be canonical", c[0], c[1])
		}
		q, r := normalize(c[0], c[1])
		if q != c[0] || r != c[1] {
			t.Fatalf("normalize(%d, %d) = (%d, %d), want the input unchanged", c[0], c[1], q, r)
		}
	}
	// One step outside on each axis is not canonical, which is what makes the
	// three comparisons a real test rather than an always-true one.
	outside := [][2]int64{
		{WorldRadius + 1, 0}, {0, WorldRadius + 1}, {WorldRadius, WorldRadius},
		{-WorldRadius, -WorldRadius}, {-WorldRadius - 1, 0},
	}
	for _, c := range outside {
		if isCanonical(c[0], c[1]) {
			t.Fatalf("(%d, %d) should not be canonical", c[0], c[1])
		}
	}
}

// TestNormalizeStage2 exercises the greedy fix-up on inputs a few mirror centers
// out, which is where every coordinate the rest of the program produces lands.
func TestNormalizeStage2(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 11))
	for range 20_000 {
		base := randomCanonical(rng)
		// A small integer combination of the six centers, which stays well
		// inside the greedy-safe bound.
		q, r := int64(base.Q()), int64(base.R())
		for range 1 + rng.IntN(3) {
			m := mirrorCenters[rng.IntN(6)]
			q, r = q+m[0], r+m[1]
		}
		if isCanonical(q, r) {
			continue // the offset landed back inside; nothing to fix up
		}
		if abs64(q) > greedySafeBound || abs64(r) > greedySafeBound {
			t.Fatalf("test input (%d, %d) is outside the greedy-safe bound", q, r)
		}
		gq, gr := greedyReduce(q, r)
		if !isCanonical(gq, gr) {
			t.Fatalf("greedyReduce(%d, %d) = (%d, %d), not canonical", q, r, gq, gr)
		}
		if got := newCanonical(gq, gr); got != base {
			t.Fatalf("greedyReduce(%d, %d) = %v, want %v", q, r, got, base)
		}
	}
}

// TestNormalizeStage2FallsThroughToStage3 covers the seam between the stages:
// an input inside the greedy-safe bound, so stage 2 is entered, but far enough
// out that the step limit runs out before it is canonical. It is the only path
// where stage 2 runs and does not decide the answer. See DESIGN.md appendix D.2.
func TestNormalizeStage2FallsThroughToStage3(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 14))
	for range 200 {
		q := greedySafeBound - rng.Int64N(1<<20)
		r := -greedySafeBound + rng.Int64N(1<<20)

		if abs64(q) > greedySafeBound || abs64(r) > greedySafeBound {
			t.Fatalf("(%d, %d) is outside the greedy-safe bound; stage 2 would be skipped", q, r)
		}
		if gq, gr := greedyReduce(q, r); isCanonical(gq, gr) {
			t.Fatalf("greedyReduce finished from (%d, %d); the test is not covering the fall-through", q, r)
		}

		got := NewCoord(q, r)
		if !isCanonical(int64(got.Q()), int64(got.R())) {
			t.Fatalf("NewCoord(%d, %d) = %v, not canonical", q, r, got)
		}
		assertSameCoset(t, q, r, got)
	}
}

// TestNormalizeStage3 exercises the lattice solve directly with inputs chosen
// for it. No coordinate the program produces reaches this branch, which is
// exactly why it needs its own inputs and an independently computed answer.
func TestNormalizeStage3(t *testing.T) {
	wild := [][2]int64{
		{math.MaxInt64, 0},
		{0, math.MaxInt64},
		{math.MaxInt64, math.MaxInt64},
		{math.MinInt64, 0},
		{0, math.MinInt64},
		{math.MinInt64, math.MinInt64},
		{math.MaxInt64, math.MinInt64},
		{math.MinInt64, math.MaxInt64},
		{math.MaxInt64 - 1, math.MinInt64 + 1},
		{greedySafeBound + 1, -greedySafeBound - 1},
	}
	rng := rand.New(rand.NewPCG(0x5747_5641, 12))
	for range 5_000 {
		wild = append(wild, [2]int64{int64(rng.Uint64()), int64(rng.Uint64())})
	}

	reachedStage3 := 0
	for _, w := range wild {
		q, r := w[0], w[1]
		if abs64(q) > greedySafeBound || abs64(r) > greedySafeBound {
			reachedStage3++
		}
		got := NewCoord(q, r)
		if !isCanonical(int64(got.Q()), int64(got.R())) {
			t.Fatalf("NewCoord(%d, %d) = %v, not canonical", q, r, got)
		}
		assertSameCoset(t, q, r, got)
	}
	if reachedStage3 < len(wild)/2 {
		t.Fatalf("only %d of %d inputs reached the lattice solve", reachedStage3, len(wild))
	}
}

// assertSameCoset checks that the difference between the input and the canonical
// result is an exact integer combination of the two lattice basis vectors. The
// arithmetic is arbitrary precision so the reference shares nothing with the
// 128-bit solve under test.
func assertSameCoset(t *testing.T, q, r int64, got Coord) {
	t.Helper()

	n := big.NewInt(WorldRadius)
	m := new(big.Int).Add(new(big.Int).Lsh(n, 1), big.NewInt(1)) // 2N+1
	nPlus1 := new(big.Int).Add(n, big.NewInt(1))

	// dq = a*M + b*(N+1), dr = a*(-N) + b*(-M)
	dq := new(big.Int).Sub(big.NewInt(q), big.NewInt(int64(got.Q())))
	dr := new(big.Int).Sub(big.NewInt(r), big.NewInt(int64(got.R())))

	// det = -(3N^2 + 3N + 1)
	det := new(big.Int).Mul(n, n)
	det.Mul(det, big.NewInt(3))
	det.Add(det, new(big.Int).Mul(n, big.NewInt(3)))
	det.Add(det, big.NewInt(1))
	det.Neg(det)

	// a = (-M*dq - (N+1)*dr) / det, b = (N*dq + M*dr) / det
	aNum := new(big.Int).Neg(new(big.Int).Mul(m, dq))
	aNum.Sub(aNum, new(big.Int).Mul(nPlus1, dr))
	bNum := new(big.Int).Mul(n, dq)
	bNum.Add(bNum, new(big.Int).Mul(m, dr))

	a, aRem := new(big.Int).QuoRem(aNum, det, new(big.Int))
	b, bRem := new(big.Int).QuoRem(bNum, det, new(big.Int))
	if aRem.Sign() != 0 || bRem.Sign() != 0 {
		t.Fatalf("NewCoord(%d, %d) = %v: the difference is not on the wraparound lattice", q, r, got)
	}

	// Reconstruct, so a sign error in the inverse cannot pass.
	wantQ := new(big.Int).Add(new(big.Int).Mul(a, m), new(big.Int).Mul(b, nPlus1))
	wantR := new(big.Int).Sub(new(big.Int).Neg(new(big.Int).Mul(a, n)), new(big.Int).Mul(b, m))
	if wantQ.Cmp(dq) != 0 || wantR.Cmp(dr) != 0 {
		t.Fatalf("NewCoord(%d, %d) = %v: lattice reconstruction gave (%s, %s), want (%s, %s)",
			q, r, got, wantQ, wantR, dq, dr)
	}
}

// TestNormalizeIsIdempotent is the property every other coordinate operation
// leans on: renormalizing a canonical coordinate changes nothing.
func TestNormalizeIsIdempotent(t *testing.T) {
	for _, c := range sampleCoords(t, 20_000) {
		if got := NewCoord(int64(c.Q()), int64(c.R())); got != c {
			t.Fatalf("renormalizing %v gave %v", c, got)
		}
	}
}

// TestLatticeSolveResidualIsSmall states what stage 3 owes stage 2: a residual
// within hex distance 2N+1 of the origin.
func TestLatticeSolveResidualIsSmall(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 13))
	limit := 2*WorldRadius + 1
	for range 20_000 {
		q, r := int64(rng.Uint64()), int64(rng.Uint64())
		rq, rr := latticeSolve(q, r)
		if n := hexNorm(rq, rr); n > limit {
			t.Fatalf("latticeSolve(%d, %d) residual (%d, %d) has hex norm %d, want at most %d",
				q, r, rq, rr, n, limit)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// sampleCoords returns canonical coordinates spread over the whole map, always
// including the origin, the six corners, and a ring just inside the rim.
func sampleCoords(t *testing.T, n int) []Coord {
	t.Helper()
	out := make([]Coord, 0, n+16)
	out = append(out, Origin)
	corner := NewCoord(WorldRadius, 0)
	for steps := range 6 {
		out = append(out, corner.Rotate(steps))
		out = append(out, corner.Rotate(steps).Neighbor(steps+3))
	}
	rng := rand.New(rand.NewPCG(0x5747_5641, uint64(n)))
	for len(out) < cap(out) {
		out = append(out, randomCanonical(rng))
	}
	return out
}

// randomCanonical picks a uniformly random coordinate inside the canonical
// hexagon by rejection, so the sample is not biased toward the middle.
func randomCanonical(rng *rand.Rand) Coord {
	for {
		q := rng.Int64N(2*WorldRadius+1) - WorldRadius
		r := rng.Int64N(2*WorldRadius+1) - WorldRadius
		if isCanonical(q, r) {
			return newCanonical(q, r)
		}
	}
}

// TestCoordShape asserts the structural claim the rest of the package leans on:
// every field is unexported, so a non-canonical Coord cannot be constructed
// outside this package and ==, map keys, and sorting are correct for tile
// identity without anyone having to remember a convention.
//
// It also asserts that Coord holds no pointer, which is what lets a []Coord and
// the []Tile built from it be one allocation the garbage collector never scans.
func TestCoordShape(t *testing.T) {
	typ := reflect.TypeFor[Coord]()
	if typ.NumField() == 0 {
		t.Fatal("Coord has no fields; the test is not measuring anything")
	}
	for i := range typ.NumField() {
		f := typ.Field(i)
		if f.IsExported() {
			t.Errorf("Coord.%s is exported, so a non-canonical value can be built outside the package", f.Name)
		}
		switch f.Type.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.String,
			reflect.Chan, reflect.Func, reflect.Interface, reflect.UnsafePointer:
			t.Errorf("Coord.%s is %v, which is pointer-shaped", f.Name, f.Type.Kind())
		}
	}

	// Two components of the stored width and nothing else.
	if got, want := typ.Size(), 2*reflect.TypeFor[Component]().Size(); got != want {
		t.Errorf("Coord is %d bytes, want %d", got, want)
	}

	// Coord is comparable and usable as a map key, which is the whole return on
	// canonical-by-construction.
	if !typ.Comparable() {
		t.Error("Coord is not comparable")
	}
	seen := map[Coord]bool{}
	c := NewCoord(WorldRadius+1, 0)
	seen[c] = true
	if !seen[NewCoord(WorldRadius+1, 0)] {
		t.Error("two coordinates that normalize alike are not the same map key")
	}
}
