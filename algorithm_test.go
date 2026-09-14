// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"reflect"
	"testing"
)

// TestWorldRadiusMatchesComponent pins the one decision the world's size is made
// of. It fails the instant one half of the pair moves without the other.
//
// Only the second assertion is width-specific: it says the Component type is no
// wider than WorldRadius needs, which is true because 16 bits is the settled
// width and 32767 is its maximum. DESIGN.md 4.2's 18-bit contingency would store
// a radius of 131071 in an int32 and that assertion would have to go, since the
// type would then be deliberately wider than the domain. Nothing else changes,
// because everything else range-checks against ±WorldRadius.
func TestWorldRadiusMatchesComponent(t *testing.T) {
	if int64(Component(WorldRadius)) != WorldRadius {
		t.Fatal("WorldRadius does not fit in a Component")
	}
	// Through a variable: Component(WorldRadius+1) as a constant expression does
	// not compile, which is a weaker form of the same check and not the one this
	// test is making.
	over := WorldRadius + 1
	if int64(Component(over)) == over {
		t.Fatal("WorldRadius is smaller than the Component can hold")
	}
}

// TestComponentWidthBitsMatchesComponent keeps the width recorded in world
// metadata and hashed into the fingerprint in step with the type. The reference
// counts bytes through reflect rather than repeating the derivation.
//
// This is width-specific for the same reason the pinning test above is.
// ComponentWidthBits derives the *domain* width from WorldRadius, which is what
// the fingerprint wants — it is what distinguishes one world topology from
// another. At the shipped width the domain and the storage type are the same
// size, so tying them together here is a free extra check. Under DESIGN.md
// 4.2's 18-bit contingency they part company: the function would report 18 and
// the type would be 32 bits wide, which is correct on both counts, and this
// assertion would be the one to drop.
func TestComponentWidthBitsMatchesComponent(t *testing.T) {
	bitsInType := 8 * uint32(reflect.TypeFor[Component]().Size())
	if got := ComponentWidthBits(); got != bitsInType {
		t.Fatalf("ComponentWidthBits() = %d, want %d", got, bitsInType)
	}
	// And the width and the radius agree: a two's complement type of w bits
	// holds a maximum of 2^(w-1) - 1.
	wantRadius := uint64(1)<<(bitsInType-1) - 1
	if uint64(WorldRadius) != wantRadius {
		t.Fatalf("WorldRadius = %d, want %d for a %d-bit component", WorldRadius, wantRadius, bitsInType)
	}
}

// TestExtremeNegativeIsNotACoordinate is DESIGN.md 4.1 in its own right: the
// canonical domain is symmetric, so the value a two's complement Component can
// hold but the domain excludes is one below -WorldRadius.
func TestExtremeNegativeIsNotACoordinate(t *testing.T) {
	if int64(math.MinInt16) >= -WorldRadius {
		t.Fatal("the Component type has no value below -WorldRadius; the symmetry claim is vacuous")
	}
	// Negation and absolute value are total on the canonical domain and are not
	// total on the type. Both halves matter.
	if -int64(math.MinInt16) <= 0 {
		t.Fatal("negating the excluded value is well behaved in int64; check the test")
	}
	if v := Component(math.MinInt16); -v != v {
		t.Fatal("negating the excluded value is well behaved in a Component; the exclusion is unnecessary")
	}
}

// TestTileCount is the arithmetic statement that excluding the extreme negative
// value costs no tiles: the domain is the hexagon of radius N, whose size is
// 1 + 3N(N+1), and that is also the index of the wraparound lattice.
func TestTileCount(t *testing.T) {
	// uint64 throughout. The count fits an int64 comfortably at this width, at
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
