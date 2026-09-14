// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"math/rand/v2"
	"testing"
)

// TestGradientsAreUnit pins the property the gradient table exists for. A table
// of mixed lengths — the (±1, ±1) form some reference implementations use —
// makes the diagonal directions sqrt(2) times stronger than the axial ones,
// which fbm turns into a visible diagonal grain.
func TestGradientsAreUnit(t *testing.T) {
	for i, g := range gradients {
		length := math.Sqrt(g[0]*g[0] + g[1]*g[1])
		if math.Abs(length-1) > 1e-15 {
			t.Errorf("gradient %d has length %v, want 1", i, length)
		}
	}
}

// TestGradientsAreEvenlySpaced asserts the twelve directions are distinct and
// thirty degrees apart, without naming an angle: consecutive unit vectors thirty
// degrees apart have a dot product of cos(30) = sqrt(3)/2.
func TestGradientsAreEvenlySpaced(t *testing.T) {
	want := math.Sqrt(3) / 2
	for i := range gradients {
		a, b := gradients[i], gradients[(i+1)%gradientCount]
		dot := a[0]*b[0] + a[1]*b[1]
		if math.Abs(dot-want) > 1e-15 {
			t.Errorf("gradients %d and %d have dot product %v, want %v", i, (i+1)%gradientCount, dot, want)
		}
	}
}

// TestSimplexRange asserts both halves of what simplexScale claims: nothing
// leaves [-1, +1], and the observed extreme still comes close to filling it.
//
// The second half is the one worth having. A scale made conservative in a hurry
// costs the field contrast silently, and every threshold tuned afterwards is
// tuned around the loss.
func TestSimplexRange(t *testing.T) {
	rng := rand.New(rand.NewPCG(20260914, 2))
	var extreme float64
	for s := range 40 {
		for range 50_000 {
			x := rng.Float64()*2000 - 1000
			y := rng.Float64()*2000 - 1000
			v := simplexNoise(Seed(s), DomRelief, x, y)
			if v < -1 || v > 1 {
				t.Fatalf("simplexNoise(%d, %v, %v) = %v, outside [-1, +1]", s, x, y, v)
			}
			extreme = max(extreme, math.Abs(v))
		}
	}
	if extreme < 0.9 {
		t.Errorf("the extreme observed over two million samples is %v; the scale is leaving contrast on the table", extreme)
	}
}

// TestValueNoiseRange asserts the range that holds by construction. Value noise
// interpolates between lattice values in [-1, +1), so nothing can leave the
// interval and no scale constant is involved.
func TestValueNoiseRange(t *testing.T) {
	rng := rand.New(rand.NewPCG(20260914, 3))
	var extreme float64
	for range 500_000 {
		x := rng.Float64()*2000 - 1000
		y := rng.Float64()*2000 - 1000
		v := valueNoise(7, DomMoisture, x, y)
		if v < -1 || v > 1 {
			t.Fatalf("valueNoise(%v, %v) = %v, outside [-1, +1]", x, y, v)
		}
		extreme = max(extreme, math.Abs(v))
	}
	if extreme < 0.9 {
		t.Errorf("the extreme observed is %v, which is too small for a noise whose lattice values fill [-1, +1)", extreme)
	}
}

// TestNoiseIsDeterministic is the invariant everything else rests on.
func TestNoiseIsDeterministic(t *testing.T) {
	for _, p := range [][2]float64{{0, 0}, {1.5, -2.25}, {-1e5, 3e4}, {12345.678, -98765.4321}} {
		a := simplexNoise(5, DomRelief, p[0], p[1])
		for range 10 {
			if b := simplexNoise(5, DomRelief, p[0], p[1]); b != a {
				t.Fatalf("simplexNoise at %v returned %v then %v", p, a, b)
			}
		}
		c := valueNoise(5, DomRelief, p[0], p[1])
		for range 10 {
			if d := valueNoise(5, DomRelief, p[0], p[1]); d != c {
				t.Fatalf("valueNoise at %v returned %v then %v", p, c, d)
			}
		}
	}
}

// TestNoiseSeparatesSeedsAndDomains is what the domain constants of DESIGN.md
// 8.1 are for. Two domains sampled at the same position must not agree, or a
// change to one field would perturb another.
func TestNoiseSeparatesSeedsAndDomains(t *testing.T) {
	const x, y = 3.25, -7.5
	base := simplexNoise(1, DomRelief, x, y)
	if other := simplexNoise(1, DomMoisture, x, y); other == base {
		t.Error("two domains returned the same value at the same position")
	}
	if other := simplexNoise(2, DomRelief, x, y); other == base {
		t.Error("two seeds returned the same value at the same position")
	}
}

// TestNoiseIsContinuous is the property that separates coherent noise from
// per-sample randomness: a small step in position must make a small step in
// value. A hash evaluated per position would fail this on the first pair.
func TestNoiseIsContinuous(t *testing.T) {
	const step = 1.0 / 4096
	rng := rand.New(rand.NewPCG(20260914, 4))

	for _, f := range []struct {
		name  string
		noise func(Seed, uint64, float64, float64) float64
		bound float64
	}{
		// Gradient noise has a bounded slope; value noise's quintic has a
		// steeper maximum derivative, so the two carry different bounds. Both
		// are far below what an uncorrelated sample would produce.
		{"simplex", simplexNoise, 0.01},
		{"value", valueNoise, 0.01},
	} {
		t.Run(f.name, func(t *testing.T) {
			var worst float64
			for range 200_000 {
				x := rng.Float64()*200 - 100
				y := rng.Float64()*200 - 100
				a := f.noise(11, DomRelief, x, y)
				b := f.noise(11, DomRelief, x+step, y)
				c := f.noise(11, DomRelief, x, y+step)
				worst = max(worst, math.Abs(b-a), math.Abs(c-a))
			}
			if worst > f.bound {
				t.Errorf("a step of %v moved the value by %v, which is more than %v", step, worst, f.bound)
			}
		})
	}
}

// TestQuinticFade pins the interpolant. The cubic smoothstep leaves a
// second-derivative discontinuity at every cell boundary, which relief — a first
// difference between neighbors — turns into a visible lattice, so the endpoints
// and the vanishing first derivative are the properties that matter.
func TestQuinticFade(t *testing.T) {
	if got := quintic(0); got != 0 {
		t.Errorf("quintic(0) = %v, want 0", got)
	}
	if got := quintic(1); got != 1 {
		t.Errorf("quintic(1) = %v, want 1", got)
	}
	if got := quintic(0.5); math.Abs(got-0.5) > 1e-15 {
		t.Errorf("quintic(0.5) = %v, want 0.5", got)
	}

	const h = 1e-6
	for _, t0 := range []float64{0, 1} {
		slope := math.Abs(quintic(t0+h) - quintic(t0-h))
		if slope > 1e-10 {
			t.Errorf("the first derivative at %v is about %v, want zero", t0, slope/(2*h))
		}
	}

	prev := math.Inf(-1)
	for i := range 1001 {
		v := quintic(float64(i) / 1000)
		if v < prev {
			t.Fatalf("quintic is not monotonic: it fell at t = %v", float64(i)/1000)
		}
		prev = v
	}
}

// TestNoiseHasNoFusedMultiplyAdd is the cheapest available check on DESIGN.md
// 25.1: the value at a position must not depend on how the expression was
// arranged. It cannot see a missing mathx.Mul on its own — only the golden test
// on a second GOARCH can — but it does catch a rewrite that changed the
// arithmetic while claiming not to.
func TestNoiseHasNoFusedMultiplyAdd(t *testing.T) {
	// The corner contribution, recomputed here in the same order with the same
	// explicit roundings.
	corner := func(seed Seed, dom uint64, i, j int64, dx, dy float64) float64 {
		t1 := float64(dx * dx)
		t2 := float64(dy * dy)
		tt := 0.5 - t1 - t2
		if tt <= 0 {
			return 0
		}
		gx, gy := gradientAt(seed, dom, i, j)
		dot := float64(gx*dx) + float64(gy*dy)
		sq := float64(tt * tt)
		return float64(float64(sq*sq) * dot)
	}
	for i := range int64(7) {
		for j := range int64(7) {
			dx := float64(i)/11 - 0.3
			dy := float64(j)/13 - 0.3
			got := simplexCorner(3, DomRelief, i, j, dx, dy)
			want := corner(3, DomRelief, i, j, dx, dy)
			if got != want {
				t.Fatalf("simplexCorner(%d, %d, %v, %v) = %v, want %v", i, j, dx, dy, got, want)
			}
		}
	}
}

func BenchmarkSimplexNoise(b *testing.B) {
	var sink float64
	x := 0.0
	for b.Loop() {
		x += 0.125
		sink += simplexNoise(1, DomRelief, x, x*0.5)
	}
	_ = sink
}

func BenchmarkValueNoise(b *testing.B) {
	var sink float64
	x := 0.0
	for b.Loop() {
		x += 0.125
		sink += valueNoise(1, DomRelief, x, x*0.5)
	}
	_ = sink
}
