// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package mathx

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

// TestFloorDivModAroundZero is exhaustive over the range where Go's truncating
// division and the mathematical floor disagree, which is every negative dividend
// with an inexact quotient — the case chunk and region addressing hits on every
// tile left of or above the origin.
func TestFloorDivModAroundZero(t *testing.T) {
	for b := int64(1); b <= 64; b++ {
		for a := int64(-4096); a <= 4096; a++ {
			// Derived independently rather than from the implementation.
			want := int64(math.Floor(float64(a) / float64(b)))
			if got := FloorDiv(a, b); got != want {
				t.Fatalf("FloorDiv(%d, %d) = %d, want %d", a, b, got, want)
			}
			m := FloorMod(a, b)
			if m < 0 || m >= b {
				t.Fatalf("FloorMod(%d, %d) = %d, outside [0, %d)", a, b, m, b)
			}
			if got := want*b + m; got != a {
				t.Fatalf("FloorDiv(%d, %d)*%d + FloorMod = %d, want %d", a, b, b, got, a)
			}
		}
	}
}

// TestFloorDivDiffersFromGo pins the reason these helpers exist. If this ever
// stops failing for Go's operators, the helpers are no longer needed.
func TestFloorDivDiffersFromGo(t *testing.T) {
	const a, b = -33, 32
	if a/b == FloorDiv(a, b) {
		t.Fatal("Go's / already floors; the helper's reason for existing has changed")
	}
	if a%b == FloorMod(a, b) {
		t.Fatal("Go's % already takes the sign of the divisor")
	}
	if got, want := FloorDiv(a, b), int64(-2); got != want {
		t.Fatalf("FloorDiv(%d, %d) = %d, want %d", a, b, got, want)
	}
	if got, want := FloorMod(a, b), int64(31); got != want {
		t.Fatalf("FloorMod(%d, %d) = %d, want %d", a, b, got, want)
	}
}

func TestFloorDivModRejectNonPositiveDivisor(t *testing.T) {
	for _, b := range []int64{0, -1, math.MinInt64} {
		t.Run("div", func(t *testing.T) {
			defer mustPanic(t, "FloorDiv")
			FloorDiv(7, b)
		})
		t.Run("mod", func(t *testing.T) {
			defer mustPanic(t, "FloorMod")
			FloorMod(7, b)
		})
	}
}

func mustPanic(t *testing.T, what string) {
	t.Helper()
	if recover() == nil {
		t.Fatalf("%s did not panic", what)
	}
}

func TestMulRoundsToFloat64(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 1))
	for range 100_000 {
		a := rng.NormFloat64()
		b := rng.NormFloat64()
		if got, want := Mul(a, b), float64(a*b); got != want {
			t.Fatalf("Mul(%v, %v) = %v, want %v", a, b, got, want)
		}
	}
}

// TestMulDefeatsFMA demonstrates that the hazard is real: there exist operands
// for which one rounding and two roundings disagree, so a bare a*b + c is not
// interchangeable with Mul(a, b) + c on a target that fuses.
func TestMulDefeatsFMA(t *testing.T) {
	rng := rand.New(rand.NewPCG(0x5747_5641, 2))
	differed := 0
	for range 100_000 {
		a, b, c := rng.NormFloat64(), rng.NormFloat64(), rng.NormFloat64()
		twice := Mul(a, b) + c
		once := math.FMA(a, b, c)
		if twice != once {
			differed++
		}
	}
	if differed == 0 {
		t.Fatal("no operand triple distinguished one rounding from two; the test is not measuring anything")
	}
	t.Logf("%d of 100000 triples differed between FMA and Mul", differed)
}

// ---------------------------------------------------------------------------
// Int128
// ---------------------------------------------------------------------------

// big returns x as an exact arbitrary-precision integer, computed as
// hi*2^64 + lo so that the reference is arithmetic rather than a second copy of
// the two's complement reasoning under test.
func (x Int128) big() *big.Int {
	b := big.NewInt(x.hi)
	b.Lsh(b, 64)
	return b.Add(b, new(big.Int).SetUint64(x.lo))
}

// interesting is the set of int64 values the lattice solve's inputs are chosen
// from. Nothing the program produces reaches the ends of the type, which is
// exactly why they are tested here directly.
var interesting = []int64{
	0, 1, -1, 2, -2, 6, -6, 32767, -32767, 65535, -65535,
	1 << 31, -(1 << 31), 1 << 62, -(1 << 62),
	math.MaxInt64, math.MaxInt64 - 1, math.MinInt64, math.MinInt64 + 1,
}

func TestMulInt64Exact(t *testing.T) {
	for _, a := range interesting {
		for _, b := range interesting {
			want := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
			if got := MulInt64(a, b).big(); got.Cmp(want) != 0 {
				t.Fatalf("MulInt64(%d, %d) = %s, want %s", a, b, got, want)
			}
		}
	}
	rng := rand.New(rand.NewPCG(0x5747_5641, 3))
	for range 20_000 {
		a, b := int64(rng.Uint64()), int64(rng.Uint64())
		want := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
		if got := MulInt64(a, b).big(); got.Cmp(want) != 0 {
			t.Fatalf("MulInt64(%d, %d) = %s, want %s", a, b, got, want)
		}
	}
}

func TestInt128AddSubNeg(t *testing.T) {
	for _, a := range interesting {
		for _, b := range interesting {
			x, y := MulInt64(a, 3), MulInt64(b, 5)
			bx, by := x.big(), y.big()

			if got, want := x.Add(y).big(), new(big.Int).Add(bx, by); got.Cmp(want) != 0 {
				t.Fatalf("Add: got %s, want %s", got, want)
			}
			if got, want := x.Sub(y).big(), new(big.Int).Sub(bx, by); got.Cmp(want) != 0 {
				t.Fatalf("Sub: got %s, want %s", got, want)
			}
			if got, want := x.Neg().big(), new(big.Int).Neg(bx); got.Cmp(want) != 0 {
				t.Fatalf("Neg: got %s, want %s", got, want)
			}
			if got, want := x.IsNegative(), bx.Sign() < 0; got != want {
				t.Fatalf("IsNegative(%s) = %v", bx, got)
			}
			if got, want := x.IsZero(), bx.Sign() == 0; got != want {
				t.Fatalf("IsZero(%s) = %v", bx, got)
			}
		}
	}
}

func TestInt128Int64RoundTrip(t *testing.T) {
	for _, v := range interesting {
		got, ok := Int128Of(v).Int64()
		if !ok || got != v {
			t.Fatalf("Int128Of(%d).Int64() = %d, %v", v, got, ok)
		}
	}
	// One past each end of the type does not fit.
	tooBig := Int128Of(math.MaxInt64).Add(Int128Of(1))
	if _, ok := tooBig.Int64(); ok {
		t.Fatal("MaxInt64+1 reported as fitting in an int64")
	}
	tooSmall := Int128Of(math.MinInt64).Sub(Int128Of(1))
	if _, ok := tooSmall.Int64(); ok {
		t.Fatal("MinInt64-1 reported as fitting in an int64")
	}
}

// TestDivRoundPosExact checks the rounding against arbitrary precision for
// numerators that need more than 64 bits, which is the only reason this type
// exists.
func TestDivRoundPosExact(t *testing.T) {
	divisors := []uint64{1, 2, 3, 7, 32767, 3221127169, 1 << 62, math.MaxUint64 / 4}
	checked := 0
	for _, a := range interesting {
		for _, d := range divisors {
			// Keep the quotient inside an int64 by scaling the numerator by a
			// value no larger than the divisor.
			x := MulInt64(a, int64(min(d, 1<<31)))
			want := divRoundHalfAway(x.big(), d)
			if !want.IsInt64() {
				continue
			}
			if got := x.DivRoundPos(d); got != want.Int64() {
				t.Fatalf("(%s).DivRoundPos(%d) = %d, want %s", x.big(), d, got, want)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d cases were in range; the test is not measuring much", checked)
	}
	t.Logf("checked %d numerator/divisor pairs", checked)
}

func TestDivRoundPosTies(t *testing.T) {
	cases := []struct {
		num  int64
		d    uint64
		want int64
	}{
		{5, 2, 3},   // 2.5 rounds away from zero
		{-5, 2, -3}, // and so does -2.5
		{4, 2, 2},
		{3, 2, 2},
		{-3, 2, -2},
		{1, 3, 0},
		{-1, 3, 0},
		{2, 3, 1},
		{-2, 3, -1},
	}
	for _, c := range cases {
		if got := Int128Of(c.num).DivRoundPos(c.d); got != c.want {
			t.Fatalf("Int128Of(%d).DivRoundPos(%d) = %d, want %d", c.num, c.d, got, c.want)
		}
	}
}

func TestDivRoundPosRejectsZero(t *testing.T) {
	defer mustPanic(t, "DivRoundPos")
	Int128Of(1).DivRoundPos(0)
}

func TestDivRoundPosRejectsOverflowingQuotient(t *testing.T) {
	defer mustPanic(t, "DivRoundPos")
	// 2^126 / 1 needs far more than 64 bits.
	MulInt64(1<<63-1, 1<<63-1).DivRoundPos(1)
}

// divRoundHalfAway is the reference rounding: n/d to the nearest integer with a
// tie going away from zero.
func divRoundHalfAway(n *big.Int, d uint64) *big.Int {
	bd := new(big.Int).SetUint64(d)
	q, r := new(big.Int).QuoRem(n, bd, new(big.Int))
	r.Abs(r)
	r.Lsh(r, 1)
	if r.Cmp(bd) >= 0 {
		if n.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}
