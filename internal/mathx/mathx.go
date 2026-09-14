// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package mathx holds the integer and floating-point helpers the generator
// needs and the Go standard library does not supply.
//
// Three problems are solved here, and each one is a hazard described in
// DESIGN.md rather than a convenience:
//
//   - Go's / truncates toward zero and % takes the sign of the dividend, so
//     neither agrees with the mathematical floor across the origin. FloorDiv
//     and FloorMod do. Section 24.
//   - Go permits the compiler to contract a*b + c into a fused multiply-add,
//     and arm64 takes it. Mul forces the intermediate rounding that defeats it.
//     Section 25.1.
//   - Go wraps signed integer overflow silently, so the lattice solve of
//     section 7.1 written in int64 returns a plausible wrong answer rather than
//     a panic. Int128 carries that solve exactly.
package mathx

import "math/bits"

// FloorDiv returns the quotient of a and b rounded toward negative infinity.
//
// It differs from Go's / exactly when the signs of a and b differ and the
// division is inexact, which is the case chunk, region, and interpolation-cell
// addressing hit on every tile with a negative component.
//
// b must be positive. Every divisor in this design is a chunk size, a region
// size, or 6, and floor division parts company with Euclidean division for a
// negative divisor, so a non-positive b is a caller error rather than a
// question this function should answer. It panics.
func FloorDiv(a, b int64) int64 {
	if b <= 0 {
		panic("mathx: FloorDiv divisor must be positive")
	}
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

// FloorMod returns a modulo b with the sign of b, so the result is always in
// [0, b). It is the remainder that pairs with FloorDiv:
// FloorDiv(a, b)*b + FloorMod(a, b) == a.
//
// b must be positive; see FloorDiv. It panics otherwise.
func FloorMod(a, b int64) int64 {
	if b <= 0 {
		panic("mathx: FloorMod divisor must be positive")
	}
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// Mul returns a*b rounded to float64, defeating any fused multiply-add the
// compiler might otherwise form with a following addition. See DESIGN.md 25.1.
//
// The explicit conversion is what carries the guarantee: the Go specification
// requires an explicit floating-point conversion to round to the target type's
// precision, and that requirement survives inlining. Write
//
//	v := mathx.Mul(a, b) + c
//
// at every multiply-add in the generation path. A bare a*b + c is permitted to
// round once instead of twice, which makes the same tile differ between arm64
// and amd64 with nothing in the source to see.
//
// Do not replace this with math.FMA. That is the explicit opt-in to fusion and
// is the opposite of what this function is for.
func Mul(a, b float64) float64 {
	return float64(a * b)
}

// Int128 is an exact signed 128-bit integer, in two's complement with hi
// holding the high 64 bits.
//
// It exists for one caller: the stage-3 lattice solve in DESIGN.md 7.1, whose
// products reach 6e23 at the alpha component width and 4e28 at the shipping
// width, and whose basis determinant alone does not fit in an int64 at the
// shipping width. That branch is cold — no coordinate the program produces
// reaches it — so this is written for exactness and for being testable
// directly, not for speed.
type Int128 struct {
	hi int64
	lo uint64
}

// MulInt64 returns the exact 128-bit product of a and b.
func MulInt64(a, b int64) Int128 {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	// bits.Mul64 computes the unsigned product. Converting it to the signed
	// product is two corrections, each of which is the operand that was
	// implicitly added as 2^64 by the unsigned reading of a negative input.
	if a < 0 {
		hi -= uint64(b)
	}
	if b < 0 {
		hi -= uint64(a)
	}
	return Int128{hi: int64(hi), lo: lo}
}

// Int128Of returns v widened to 128 bits.
func Int128Of(v int64) Int128 {
	var hi int64
	if v < 0 {
		hi = -1
	}
	return Int128{hi: hi, lo: uint64(v)}
}

// Add returns x+y. It wraps at 128 bits, which no caller here reaches.
func (x Int128) Add(y Int128) Int128 {
	lo, carry := bits.Add64(x.lo, y.lo, 0)
	return Int128{hi: x.hi + y.hi + int64(carry), lo: lo}
}

// Sub returns x-y. It wraps at 128 bits, which no caller here reaches.
func (x Int128) Sub(y Int128) Int128 {
	lo, borrow := bits.Sub64(x.lo, y.lo, 0)
	return Int128{hi: x.hi - y.hi - int64(borrow), lo: lo}
}

// Neg returns -x.
func (x Int128) Neg() Int128 {
	return Int128{}.Sub(x)
}

// IsNegative reports whether x is less than zero.
func (x Int128) IsNegative() bool { return x.hi < 0 }

// IsZero reports whether x is zero.
func (x Int128) IsZero() bool { return x.hi == 0 && x.lo == 0 }

// Int64 returns x as an int64 and reports whether it fit.
func (x Int128) Int64() (int64, bool) {
	v := int64(x.lo)
	if Int128Of(v) != x {
		return 0, false
	}
	return v, true
}

// DivRoundPos returns x/d rounded to the nearest integer, with a tie rounded
// away from zero. d must be positive and the quotient must fit in an int64;
// both are invariants of the lattice solve rather than conditions a caller is
// expected to handle, so violating either panics.
func (x Int128) DivRoundPos(d uint64) int64 {
	if d == 0 {
		panic("mathx: Int128.DivRoundPos divisor must be positive")
	}
	neg := x.IsNegative()
	m := x
	if neg {
		m = m.Neg()
	}
	hi, lo := uint64(m.hi), m.lo
	if hi >= d {
		// bits.Div64 panics in this case, and it means the quotient needs more
		// than 64 bits, which no lattice solve input can produce.
		panic("mathx: Int128.DivRoundPos quotient overflows 64 bits")
	}
	q, rem := bits.Div64(hi, lo, d)
	// Round half away from zero. rem < d, so d-rem cannot underflow and the
	// comparison stands in for 2*rem >= d without the overflow that doubling
	// rem would risk when d is above 2^63.
	if rem >= d-rem {
		if q == ^uint64(0) {
			panic("mathx: Int128.DivRoundPos quotient overflows 64 bits")
		}
		q++
	}
	if neg {
		if q > 1<<63 {
			panic("mathx: Int128.DivRoundPos quotient overflows an int64")
		}
		return -int64(q)
	}
	if q > 1<<63-1 {
		panic("mathx: Int128.DivRoundPos quotient overflows an int64")
	}
	return int64(q)
}
