// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"testing"
)

// TestDomainFunction checks the FNV-1a implementation against values computed by
// hand from the algorithm, so the constants below are pinned to something other
// than the same code that produced them.
func TestDomainFunction(t *testing.T) {
	// Through variables: constant expressions do not wrap, and FNV-1a is
	// defined by the wrapping multiply.
	offset, prime := uint64(0xcbf29ce484222325), uint64(0x00000100000001b3)

	if got := domain(""); got != offset {
		t.Fatalf(`domain("") = %#016x, want the FNV-1a offset basis %#016x`, got, offset)
	}
	wantA := (offset ^ uint64('a')) * prime
	if got := domain("a"); got != wantA {
		t.Fatalf(`domain("a") = %#016x, want %#016x`, got, wantA)
	}
	wantAB := (wantA ^ uint64('b')) * prime
	if got := domain("ab"); got != wantAB {
		t.Fatalf(`domain("ab") = %#016x, want %#016x`, got, wantAB)
	}
}

// TestDomainConstants is the four-line test DESIGN.md 8.1 asks for: every
// identifier is FNV-1a over its name, so a renamed domain shows up here rather
// than silently changing every world.
func TestDomainConstants(t *testing.T) {
	// Written out rather than derived, so the name and the constant both appear.
	constants := map[string]uint64{
		"continentalness":    DomContinentalness,
		"regional-elevation": DomRegionalElevation,
		"relief":             DomRelief,
		"terrain-detail":     DomTerrainDetail,
		"temperature":        DomTemperature,
		"moisture":           DomMoisture,
		"moisture-variation": DomMoistureVariation,
		"region-style":       DomRegionStyle,
		"ridge-orientation":  DomRidgeOrientation,
		"ridge-structure":    DomRidgeStructure,
		"basin":              DomBasin,
		"basin-regional":     DomBasinRegional,
		"basin-local":        DomBasinLocal,
		"volcanic":           DomVolcanic,
		"warp-x":             DomWarpX,
		"warp-y":             DomWarpY,
		"detail-warp-x":      DomDetailWarpX,
		"detail-warp-y":      DomDetailWarpY,
		"field-offset":       DomFieldOffset,
	}
	for name, got := range constants {
		if want := domain(name); got != want {
			t.Errorf("domain(%q) = %#016x, constant is %#016x", name, want, got)
		}
	}

	// One field node, one domain: no two identifiers may collide, or two nodes
	// would share a gradient table and a sampling offset.
	seen := make(map[uint64]string, len(constants))
	for name, v := range constants {
		if other, dup := seen[v]; dup {
			t.Errorf("domains %q and %q share the identifier %#016x", name, other, v)
		}
		seen[v] = name
	}
}

func TestHashIsDeterministic(t *testing.T) {
	for range 1000 {
		if Hash2(7, DomRelief, 3, -4) != Hash2(7, DomRelief, 3, -4) {
			t.Fatal("Hash2 is not a function of its arguments")
		}
		if Hash3(7, DomRelief, 3, -4, 5) != Hash3(7, DomRelief, 3, -4, 5) {
			t.Fatal("Hash3 is not a function of its arguments")
		}
	}
}

// TestHashSeparatesItsInputs checks that every argument position actually
// reaches the result, including the sign of a coordinate and the difference
// between the two arities.
func TestHashSeparatesItsInputs(t *testing.T) {
	base := Hash2(1, DomRelief, 2, 3)
	cases := map[string]uint64{
		"seed":       Hash2(2, DomRelief, 2, 3),
		"domain":     Hash2(1, DomMoisture, 2, 3),
		"a":          Hash2(1, DomRelief, 3, 3),
		"b":          Hash2(1, DomRelief, 2, 4),
		"a negative": Hash2(1, DomRelief, -2, 3),
		"b negative": Hash2(1, DomRelief, 2, -3),
		"swapped":    Hash2(1, DomRelief, 3, 2),
	}
	for what, got := range cases {
		if got == base {
			t.Errorf("changing the %s did not change the hash", what)
		}
	}
	if Hash3(1, DomRelief, 2, 3, 0) == base {
		t.Error("Hash3 with a zero third coordinate collides with Hash2")
	}

	// Neighbouring coordinates must not produce neighbouring hashes.
	for a := int64(-4); a <= 4; a++ {
		for b := int64(-4); b <= 4; b++ {
			h := Hash2(0, DomTerrainDetail, a, b)
			for da := int64(-1); da <= 1; da++ {
				for db := int64(-1); db <= 1; db++ {
					if da == 0 && db == 0 {
						continue
					}
					if other := Hash2(0, DomTerrainDetail, a+da, b+db); other == h {
						t.Fatalf("(%d, %d) and (%d, %d) collide", a, b, a+da, b+db)
					}
				}
			}
		}
	}
}

// TestHashAvalanche is a coarse quality check: flipping one bit of any input
// should change about half the output bits. A mixer that failed this would show
// up as visible structure in a field long before anything else noticed.
func TestHashAvalanche(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 31))
	var flipped, total int
	for range 20_000 {
		seed := rng.Uint64()
		a, b := int64(rng.Uint64()), int64(rng.Uint64())
		h := Hash2(seed, DomContinentalness, a, b)
		for bit := range 64 {
			other := Hash2(seed, DomContinentalness, a^(1<<bit), b)
			flipped += bits.OnesCount64(h ^ other)
			total += 64
		}
	}
	ratio := float64(flipped) / float64(total)
	if ratio < 0.49 || ratio > 0.51 {
		t.Fatalf("one input bit flips %.4f of the output bits, want about 0.5", ratio)
	}
}

func TestToUnitRangeAndUniformity(t *testing.T) {
	if got := toUnit(0); got != 0 {
		t.Fatalf("toUnit(0) = %v, want 0", got)
	}
	// The largest hash is one ulp of the 53-bit grid below one, never one.
	top := toUnit(math.MaxUint64)
	if top >= 1 {
		t.Fatalf("toUnit(MaxUint64) = %v, want less than 1", top)
	}
	if want := float64((uint64(1)<<53)-1) / float64(uint64(1)<<53); top != want {
		t.Fatalf("toUnit(MaxUint64) = %v, want %v", top, want)
	}

	// Every one of the low 11 bits is discarded and no others, so shifting them
	// cannot move the result.
	for bit := range 11 {
		if toUnit(1<<bit) != 0 {
			t.Fatalf("toUnit lost more or fewer than the low 11 bits at bit %d", bit)
		}
	}
	if toUnit(1<<11) == 0 {
		t.Fatal("toUnit discards bit 11, which a float64 mantissa can hold")
	}

	// Coarse uniformity over the unit interval.
	const buckets = 16
	const draws = 200_000
	var counts [buckets]int
	for i := range draws {
		u := toUnit(Hash2(0, DomMoisture, int64(i), 0))
		if u < 0 || u >= 1 {
			t.Fatalf("toUnit produced %v, outside [0, 1)", u)
		}
		counts[int(u*buckets)]++
	}
	want := draws / buckets
	for i, n := range counts {
		if math.Abs(float64(n-want)) > 0.1*float64(want) {
			t.Fatalf("bucket %d holds %d of %d draws, want about %d", i, n, draws, want)
		}
	}
}
