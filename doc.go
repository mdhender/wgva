// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package wgva generates a deterministic, effectively unbounded procedural hex
// world.
//
// The whole design turns on one invariant:
//
//	Tile = F(seed, q, r, algorithmVersion, componentWidth, configuration)
//
// There is no dependency on generation order, on previously generated tiles, on
// what has been explored, or on which machine is running. Everything in this
// package exists either to make that hold or to keep it honest.
//
// The package depends on the standard library and nothing else. Persistence,
// rendering, configuration files, and the window grammar all import this
// package, so Go's import cycle rule makes "persistence and rendering are not in
// the core" a fact of the build rather than a review convention.
//
// # Coordinates
//
// [Coord] has unexported fields and only a normalizing constructor, so a
// non-canonical coordinate cannot be built outside the package and ==, map keys,
// and sorting are automatically correct for tile identity. The zero value is the
// origin, which is canonical.
//
// The world is a wrapped hexagon of radius [WorldRadius], paired with
// [Component]. The canonical domain excludes the extreme negative value of the
// component type, which is what makes negation and absolute value total.
//
// # Floating point
//
// Read this before writing any floating-point code in the generation path.
//
// The Go specification permits an implementation to fuse x*y + z into a single
// fused multiply-add with only one rounding. Noise code is almost entirely
// multiply-adds. On arm64 the compiler fuses; on amd64 targets without FMA it
// does not — so the same tile differs in its low bits between a developer laptop
// and a server, with nothing in the source to see.
//
// The specification also gives the escape: an explicit floating-point type
// conversion rounds to the precision of the target type, and the compiler must
// honor it.
//
//	// Fused on some targets. Never write this in the generation path.
//	v := a*b + c
//
//	// Rounded twice on every target. This is the rule.
//	v := mathx.Mul(a, b) + c
//
// Missing one site is a silent cross-platform world divergence, so the rule is
// carried by a named helper, internal/mathx.Mul, which makes it greppable and
// makes its absence visible in review. math.FMA is the explicit opt-in to
// fusion; do not call it anywhere in the generation path.
//
// Two further restrictions follow from the same concern. The generation path
// uses only +, -, *, /, math.Sqrt, math.Floor, math.Abs, min, max, and
// comparisons on float64 — no transcendental functions, because Go's
// implementations of those are portable Go for some and architecture-specific
// assembly for others, and that split has moved between releases. And
// accumulation order is fixed: floating-point addition is not associative, so
// directions are iterated 0..6 and octaves coarse-to-fine, always, and nothing
// ranges over a map anywhere the order can be observed.
//
// The golden tests run on GOARCH=amd64 and GOARCH=arm64. That is the only thing
// that actually proves any of this is being honored.
//
// See DESIGN.md sections 25.1 through 25.7.
package wgva
