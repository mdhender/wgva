// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

// Seed identifies a world. Everything generated is a function of it together
// with the algorithm version, the world radius, the configuration, and the
// coordinate.
type Seed uint64

// domain is FNV-1a over the domain name. Stable by construction: the algorithm
// is written here, so no dependency can change it.
//
// It is not used at generation time. The domain identifiers below are constants
// because Go cannot call a function in a constant expression, and this function
// exists so a test can check each one.
func domain(name string) uint64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(name); i++ {
		h ^= uint64(name[i])
		h *= 0x00000100000001b3
	}
	return h
}

// Domain identifiers separate the procedural systems, so a change to one field
// cannot perturb another. Each value is FNV-1a over the name in the comment
// beside it, and TestDomainConstants asserts it.
//
// Writing them out rather than computing them into package-level vars is
// deliberate: a var can be assigned, the values never appear in a diff, and a
// renamed domain would silently change every world with nothing in the commit
// to see.
//
// One field node, one domain. Where a family has several scales — the three
// basin fields, the broad moisture field and its variation term — each gets its
// own identifier rather than sharing one, because sharing would give two nodes
// the same gradient table and the same seed-derived sampling offset, so at the
// world origin they would land in the same lattice cell at the same fractional
// position and return the same value.
//
// Adding a domain never disturbs an existing one. Renaming one changes the world
// and is an algorithm version change. See DESIGN.md 8.1.
const (
	DomContinentalness   uint64 = 0xe6a3e327f10250f5 // domain("continentalness")
	DomRegionalElevation uint64 = 0xe3b0ad5b632039e6 // domain("regional-elevation")
	DomRelief            uint64 = 0xfa189c4daf367720 // domain("relief")
	DomTerrainDetail     uint64 = 0xa90f24aae7b9a828 // domain("terrain-detail")
	DomTemperature       uint64 = 0x556575c1ce107955 // domain("temperature")
	DomMoisture          uint64 = 0x4532a0329655240f // domain("moisture")
	DomMoistureVariation uint64 = 0x2427b6a41708b52f // domain("moisture-variation")
	DomRegionStyle       uint64 = 0x41156f2ba1d9a7d5 // domain("region-style")
	DomRidgeOrientation  uint64 = 0x0f5bac8866b1037b // domain("ridge-orientation")
	DomRidgeStructure    uint64 = 0xe29a7e331d9ba86c // domain("ridge-structure")
	DomBasin             uint64 = 0xd6e851826dfb0aa6 // domain("basin")
	DomBasinRegional     uint64 = 0x203b69310f71797c // domain("basin-regional")
	DomBasinLocal        uint64 = 0x2c97a5cf22fbc844 // domain("basin-local")
	DomVolcanic          uint64 = 0x32086c8b90c635b8 // domain("volcanic")
	DomWarpX             uint64 = 0xa1e186323c6662fa // domain("warp-x")
	DomWarpY             uint64 = 0xa1e187323c6664ad // domain("warp-y")
	DomDetailWarpX       uint64 = 0xcbea8698f30a6740 // domain("detail-warp-x")
	DomDetailWarpY       uint64 = 0xcbea8798f30a68f3 // domain("detail-warp-y")
	DomFieldOffset       uint64 = 0xb8f53a94de4a5069 // domain("field-offset")
)

// mixInit is the fractional part of the golden ratio, the usual SplitMix64
// increment. It is here so that a zero seed is not a fixed point of the mixer.
const mixInit uint64 = 0x9e3779b97f4a7c15

// mix64 is the SplitMix64 finalizer, written out here rather than pulled from a
// dependency for the same reason the noise is: nothing version-unstable may
// reach a persisted value.
//
// Go wraps integer arithmetic silently, and this is the one place in the module
// where that is exactly the behavior wanted. The same silence is why the lattice
// solve in coord.go must widen explicitly.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// Hash2 derives a deterministic 64-bit value from a seed, a domain, and two
// coordinates.
//
// The arity is fixed rather than variadic so that a call passing the wrong
// number of coordinates does not compile. See DESIGN.md 8.2.
func Hash2(seed, domain uint64, a, b int64) uint64 {
	h := mix64(seed + mixInit)
	h = mix64(h ^ domain)
	h = mix64(h ^ uint64(a))
	h = mix64(h ^ uint64(b))
	return h
}

// Hash3 derives a deterministic 64-bit value from a seed, a domain, and three
// coordinates. See Hash2.
func Hash3(seed, domain uint64, a, b, c int64) uint64 {
	h := mix64(seed + mixInit)
	h = mix64(h ^ domain)
	h = mix64(h ^ uint64(a))
	h = mix64(h ^ uint64(b))
	h = mix64(h ^ uint64(c))
	return h
}

// toUnit converts a hash to a float64 in [0, 1) by taking the top 53 bits, which
// is every bit a float64 mantissa can hold and no bit it cannot.
//
// Never float64(h) / float64(math.MaxUint64): that rounds the low bits away and
// is not uniform.
func toUnit(h uint64) float64 {
	return float64(h>>11) / float64(uint64(1)<<53)
}
