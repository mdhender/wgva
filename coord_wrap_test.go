// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math/rand/v2"
	"testing"
)

// The wrap tests are about identity, not about smoothness. DESIGN.md 7.1 no
// longer owes periodic fields — the rim covers the seam instead — but a
// coordinate and its wrapped image must name the same tile unconditionally, and
// that is what every one of these asserts.

// TestWrapBySingleMirrorCenter is the six-edge case: translating any coordinate
// by any one mirror center names the same tile.
func TestWrapBySingleMirrorCenter(t *testing.T) {
	for _, c := range sampleCoords(t, 20_000) {
		for i, m := range mirrorCenters {
			got := NewCoord(int64(c.Q())+m[0], int64(c.R())+m[1])
			if got != c {
				t.Fatalf("%v translated by mirror center %d gave %v", c, i, got)
			}
		}
	}
}

// TestWrapByIntegerCombinations covers all integer combinations of the mirror
// centers within a small range, which is the statement that the whole lattice —
// not just its six generators — maps onto the same tile.
func TestWrapByIntegerCombinations(t *testing.T) {
	coords := sampleCoords(t, 256)
	for _, c := range coords {
		for i := -3; i <= 3; i++ {
			for j := -3; j <= 3; j++ {
				q := int64(c.Q()) + int64(i)*mirrorCenters[0][0] + int64(j)*mirrorCenters[1][0]
				r := int64(c.R()) + int64(i)*mirrorCenters[0][1] + int64(j)*mirrorCenters[1][1]
				if got := NewCoord(q, r); got != c {
					t.Fatalf("%v translated by %d*v0 + %d*v1 gave %v", c, i, j, got)
				}
			}
		}
	}
}

// TestWrapAcrossEachEdge walks off each of the six edges of the canonical
// hexagon and checks that the step lands on the tile the opposite mirror center
// names. This is the test that would notice a normalizer that agreed with itself
// but disagreed with the lattice.
func TestWrapAcrossEachEdge(t *testing.T) {
	corners := [6][2]int64{
		{WorldRadius, 0},
		{WorldRadius, -WorldRadius},
		{0, -WorldRadius},
		{-WorldRadius, 0},
		{-WorldRadius, WorldRadius},
		{0, WorldRadius},
	}
	for i := range corners {
		a, b := corners[i], corners[(i+1)%6]
		for step := int64(0); step <= 64; step++ {
			q := a[0] + (b[0]-a[0])*step/64
			r := a[1] + (b[1]-a[1])*step/64
			edge := NewCoord(q, r)
			if edge.RimDistance() != 0 {
				t.Fatalf("edge %d step %d is not on the outermost ring", i, step)
			}
			// Every direction out of an edge tile is still a canonical
			// coordinate on the far side of the map, and stepping back returns.
			for d := range 6 {
				n := edge.Neighbor(d)
				if n.RimDistance() < 0 {
					t.Fatalf("edge %d step %d neighbor %d left the canonical domain", i, step, d)
				}
				if back := n.Neighbor(d + 3); back != edge {
					t.Fatalf("edge %d step %d: neighbor %d then %d gave %v, want %v",
						i, step, d, d+3, back, edge)
				}
			}
		}
	}
}

// TestWrapIsBitExactUnderRotation combines the two symmetries: rotating a
// coordinate and rotating its wrapped image agree, because the rotation maps the
// canonical domain onto itself and the lattice onto itself.
func TestWrapIsBitExactUnderRotation(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 21))
	for range 20_000 {
		c := randomCanonical(rng)
		m := mirrorCenters[rng.IntN(6)]
		wrapped := NewCoord(int64(c.Q())+m[0], int64(c.R())+m[1])
		steps := rng.IntN(13) - 6
		if got, want := wrapped.Rotate(steps), c.Rotate(steps); got != want {
			t.Fatalf("%v: rotating the wrapped image by %d gave %v, want %v", c, steps, got, want)
		}
	}
}

// TestScrollWalkCrossesTheSeamAndRetraces is the shipped world radius earning
// its place: a walk from the origin to the rim and out the other side is a few
// thousand steps, so the wrap is covered by a test that finishes in
// milliseconds rather than by argument.
//
// The walk does not return to its start by going far enough — the lattice period
// along an axial direction is the whole tile count, 1 + 3N(N+1) — so it retraces
// instead, which is the property a scrolling viewport actually depends on.
func TestScrollWalkCrossesTheSeamAndRetraces(t *testing.T) {
	const steps = 3 * WorldRadius

	for d := range 6 {
		c := Origin
		path := make([]Coord, 0, steps)
		crossings := 0
		for range steps {
			q := int64(c.Q()) + directions[d][0]
			r := int64(c.R()) + directions[d][1]
			if !isCanonical(q, r) {
				crossings++
			}
			c = NewCoord(q, r)
			if c.RimDistance() < 0 {
				t.Fatalf("direction %d: walk left the canonical domain at %v", d, c)
			}
			path = append(path, c)
		}
		if crossings == 0 {
			t.Fatalf("direction %d: a walk of %d steps never crossed the seam", d, steps)
		}

		// Retrace. Every step back must revisit exactly the tile the outbound
		// walk stood on, seam crossings included.
		for i := len(path) - 1; i > 0; i-- {
			c = c.Neighbor(d + 3)
			if c != path[i-1] {
				t.Fatalf("direction %d: retracing step %d gave %v, want %v", d, i, c, path[i-1])
			}
		}
		if c = c.Neighbor(d + 3); c != Origin {
			t.Fatalf("direction %d: retracing ended at %v, want the origin", d, c)
		}
	}
}
