// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

// RimConfig describes the outermost band of the canonical hexagon, which is
// forced terrain and closed to play. The wrapped topology is unchanged; what the
// rim does is cover the seam rather than smooth it, which is why field
// periodicity is no longer owed. See DESIGN.md 15.1.
//
// ClosedHexes = 0 with FalloffHexes = 0 is a valid configuration and restores
// the raw wrapped world exactly. Keep it working: it is how the wrap tests see
// the seam they assert on.
type RimConfig struct {
	// ClosedHexes is the outermost band: terrain forced, passage refused.
	ClosedHexes uint32

	// FalloffHexes is the band inside it, over which elevation is driven
	// smoothly down to the forced value. Terrain there is ordinary terrain,
	// classified from the depressed elevation.
	FalloffHexes uint32

	// Kind is what the closed band is made of.
	Kind RimKind
}

// RimKind is what the closed band is made of.
//
// Deep ocean is the default and ice is the option, in that order. An ice rim
// reads as a polar cap, and a polar cap is a promise about latitude that the
// climate model deliberately does not make: a player who sees ice at the edge
// will infer an axis, an equator, and a southern cap, and be wrong about all
// three. Choose ice only for a world whose climate model has been given a
// latitude to match it.
type RimKind uint8

// The rim kinds. These values are persisted, so each is written out explicitly
// rather than taken from iota: inserting one into the middle of an iota block
// would silently renumber everything after it with nothing in the diff that
// looks like a data change.
const (
	RimDeepOcean RimKind = 0
	RimPolarIce  RimKind = 1
)

// String returns the name of the rim kind.
func (k RimKind) String() string {
	switch k {
	case RimDeepOcean:
		return "deep-ocean"
	case RimPolarIce:
		return "polar-ice"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared rim kind.
func (k RimKind) Valid() bool {
	switch k {
	case RimDeepOcean, RimPolarIce:
		return true
	default:
		return false
	}
}
