// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import "github.com/mdhender/wgva/internal/mathx"

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

	// FloorElevation is the forced value: the elevation scalar every tile of
	// the closed band is pinned at, and the value the falloff blends toward.
	// It is in [-1, +1] on the scale of DESIGN.md 14, where -1 is deep ocean,
	// 0 is sea level, and +1 is extreme highland.
	//
	// It belongs with Kind rather than being derived from it, and the pairing is
	// the configuration's to get right: a deep-ocean rim wants a floor well
	// below sea level and a polar-ice rim wants one above it, which is the
	// difference between a shelving coast and a rising icefield. Nothing here
	// couples them, because an ice shelf over water is an ordinary thing to ask
	// for and a rule that forbade it would be inventing geography.
	FloorElevation float64

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

// ---------------------------------------------------------------------------
// The profile
// ---------------------------------------------------------------------------

// closed reports whether a rim distance is inside the closed band.
//
// The band is the outermost ClosedHexes rings, and the outermost ring is
// distance zero, so it is the tiles with a distance below the count. A count of
// zero closes nothing, which is the configuration that restores the raw wrapped
// world. A negative distance cannot arrive from a Coord — the type is canonical
// by construction — and is read as closed rather than as inside, because a tile
// outside the map is not a tile of the world.
func (rc RimConfig) closed(d int64) bool { return d < int64(rc.ClosedHexes) }

// profile returns what the generated world is worth at a rim distance: 0 inside
// the closed band, a smoothstep across the falloff, and exactly 1 inside both.
//
// It is a weight rather than an elevation because that is what makes the two
// ends exact. At 0 the tile is the forced floor and at 1 it is the composite
// untouched, and "untouched" has to mean the same bits rather than the same
// number — see apply.
func (rc RimConfig) profile(d int64) float64 {
	closed := int64(rc.ClosedHexes)
	if d >= closed+int64(rc.FalloffHexes) {
		return 1
	}
	if rc.closed(d) {
		return 0
	}
	// The divisor is positive here: a zero falloff leaves no distance between
	// the two branches above.
	return smoothstepUnit(float64(d-closed) / float64(rc.FalloffHexes))
}

// apply returns the elevation the rim leaves at a rim distance, and whether the
// tile is inside the closed band.
//
// Three cases, and the first is the one with an invariant attached:
//
//   - Inside both bands the composite is returned **unchanged**, not multiplied
//     by a weight of one. That is what makes ClosedHexes = 0, FalloffHexes = 0
//     reproduce the unrimmed world bit for bit: floor + 1*(e - floor) is not e
//     in floating point, and a rim that was arithmetically transparent rather
//     than structurally transparent would move every tile in the world by an
//     ulp and invalidate every golden value. DESIGN.md 15.1 and 30.14.
//   - Inside the closed band the elevation is pinned at the floor and the flag
//     is set. Terrain is forced from the flag rather than from this value; see
//     Config.classifyTerrain.
//   - In the falloff the composite is blended toward the floor, so a traveller
//     approaching the rim sees a shelving coast or a rising icefield rather
//     than a wall. Nothing else about that terrain is special: it is classified
//     from the depressed elevation by the ordinary rules, so a cold falloff
//     band produces tundra and then ice on its own.
//
// The blend is continuous at both joins by construction. At the outer join the
// weight is zero and the value is the floor, which is what the closed band
// holds; at the inner join the weight is one and the value is the composite,
// which is what the world holds one ring further in.
func (rc RimConfig) apply(d int64, elevation float64) (float64, bool) {
	if d >= int64(rc.ClosedHexes)+int64(rc.FalloffHexes) {
		return elevation, false
	}
	if rc.closed(d) {
		return rc.FloorElevation, true
	}
	w := rc.profile(d)
	return rc.FloorElevation + mathx.Mul(w, elevation-rc.FloorElevation), false
}
