// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"reflect"
	"testing"
)

// TestComponentHoldsTheDomain pins the one relationship that must hold between
// the storage type and the world: every value in the canonical domain survives
// the round trip through Component.
//
// It does not assert the converse. Component is deliberately wider than the
// domain (DESIGN.md 4.2), so "the type is no wider than the radius needs" is
// not an invariant here — it is the thing that was given up on purpose, and the
// 18-bit contingency is a change to WorldRadius alone because of it.
func TestComponentHoldsTheDomain(t *testing.T) {
	for _, v := range []int64{0, 1, -1, WorldRadius, -WorldRadius, WorldRadius - 1, 1 - WorldRadius} {
		if int64(Component(v)) != v {
			t.Fatalf("Component(%d) does not round-trip; the type is too narrow for the domain", v)
		}
	}
}

// TestDomainIsSymmetricAndTotal is DESIGN.md 4.1 stated as the property it
// actually is, rather than as a fact about a particular width: negation and
// absolute value are total on the canonical domain.
//
// The previous formulation was "the extreme negative value of Component is not
// a coordinate", which was true when the domain sat flush against the type. It
// no longer does, so the hazard is unreachable rather than excluded — but the
// symmetry is still what makes the six-fold rotation map the domain onto
// itself, so it is still worth asserting directly.
func TestDomainIsSymmetricAndTotal(t *testing.T) {
	for _, v := range []int64{0, 1, -1, WorldRadius, -WorldRadius} {
		c := Component(v)
		if int64(-c) != -v {
			t.Fatalf("negating Component(%d) gave %d, want %d", v, -c, -v)
		}
		if got := abs64(int64(c)); got < 0 {
			t.Fatalf("abs64 of Component(%d) is negative", v)
		}
	}

	// The domain is strictly inside the type on both sides, which is what makes
	// the two assertions above unconditional rather than lucky.
	lo, hi := int64(math.MinInt32), int64(math.MaxInt32)
	if -WorldRadius <= lo || WorldRadius >= hi {
		t.Fatal("the canonical domain reaches the ends of Component; DESIGN.md 4.1's hazard is live again")
	}
}

// TestOutOfDomainValuesAreRefused checks the other half: the bound is enforced
// by componentOf and by nothing else, so it has to be enforced correctly.
func TestOutOfDomainValuesAreRefused(t *testing.T) {
	// Values a narrower storage type would have silently truncated into
	// plausible coordinates, and which are now visibly out of range.
	for _, v := range []int64{
		WorldRadius + 1, -WorldRadius - 1,
		math.MinInt16, 98301, -98301,
		math.MaxInt32, math.MinInt32,
		math.MaxInt64, math.MinInt64,
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("componentOf(%d) was accepted; it is outside ±%d", v, WorldRadius)
				}
			}()
			componentOf(v)
		}()
	}
}

// TestTileCount is the arithmetic statement that the symmetric domain costs no
// tiles: the domain is the hexagon of radius N, whose size is 1 + 3N(N+1), and
// that is also the index of the wraparound lattice.
func TestTileCount(t *testing.T) {
	// uint64 throughout. The count fits an int64 comfortably at this radius, at
	// 3.2e9; uint64 costs nothing and does not have to be revisited if
	// DESIGN.md 4.2's contingency is ever exercised.
	n := uint64(WorldRadius)
	tiles := 1 + 3*n*(n+1)

	// Counted independently, ring by ring: ring 0 is the origin and ring k has
	// 6k tiles.
	var counted uint64 = 1
	for k := uint64(1); k <= n; k++ {
		counted += 6 * k
	}
	if counted != tiles {
		t.Fatalf("ring count %d, closed form %d", counted, tiles)
	}

	// The lattice determinant is the same number, which is what makes the
	// hexagon an exact fundamental domain with no tie to break.
	det := 3*n*n + 3*n + 1
	if det != tiles {
		t.Fatalf("lattice index %d, tile count %d", det, tiles)
	}
}

// TestCoordCostsNothingToWiden records the measurement behind DESIGN.md 4.2's
// claim that the wider storage type is free. A Coord doubles; anything that
// pairs it with a float64 does not, because the narrower Coord's saving was
// alignment padding.
func TestCoordCostsNothingToWiden(t *testing.T) {
	type narrow struct{ q, r int16 }
	type withNarrow struct {
		c                narrow
		a, b, c2, d      float64
		e, f, g, rimFlag uint8
	}
	type withWide struct {
		c                Coord
		a, b, c2, d      float64
		e, f, g, rimFlag uint8
	}
	if got, want := reflect.TypeFor[withWide]().Size(), reflect.TypeFor[withNarrow]().Size(); got != want {
		t.Fatalf("a tile-shaped struct is %d bytes with the current Component and %d with an int16 one", got, want)
	}
}
