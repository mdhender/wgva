// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

// Component is the storage type for one axial coordinate component.
//
// It is deliberately wider than the canonical domain, and that is the whole
// point: the domain is defined by WorldRadius, not by the type, so the two are
// separate decisions. See DESIGN.md 4.2.
//
// A wide type is also what makes a missing bound check loud. If componentOf's
// range check were wrong or absent, narrowing 98301 to an int16 would wrap to
// 32765 — an ordinary-looking coordinate in the wrong place. Here it stores
// 98301, which isCanonical rejects and RimDistance reports as negative on the
// next call.
type Component = int32

// WorldRadius is N in DESIGN.md 7.1: the radius of the canonical hexagon, the
// bound every component is range-checked against, and the single value that
// decides the world's topology.
//
// It is written out rather than derived from Component's range. The type is
// wider than this on purpose, so there is nothing to derive it from — and a
// world radius that could be read off a type is a world radius that changes
// when somebody changes the type. Anything that needs to identify a world's
// topology — world metadata, the configuration fingerprint, gate 5 — records
// this number, never a width in bits. See DESIGN.md 4.2 and 21.2.
//
// 32767 is the shipped radius: 3,221,127,169 tiles, about 526 times Earth's
// land area at a 30% land fraction. DESIGN.md 4.2 carries the arithmetic and
// the 18-bit contingency, which is a change to this constant and nothing else.
const WorldRadius int64 = 32767

// AlgorithmVersion identifies the generation algorithm. A world file records
// it, and a binary refuses a world it cannot reproduce.
//
// Changing a noise formula, a hash domain, a threshold, a weight, the
// axial-to-world embedding, the direction table, or WorldRadius changes
// existing worlds and requires a bump. So does Config gaining a field, because
// DESIGN.md 21.1 forbids defaulting a missing generation-affecting field: no
// tile moves, but an older file can no longer be read. Say which kind of bump
// it is in the commit message. See DESIGN.md 27.
//
// Version history:
//
//	1  Initial algorithm. Coordinates, the wrapped domain, the axial-to-world
//	   embedding, the direction table, and the domain-separated mixer. The
//	   world radius is 32767; DESIGN.md 4.2.
//	2  Continuous fields. The owned simplex and value noises, the Field
//	   composition tree with fbm and domain warping, and the seed-derived
//	   sampling offset. Config gained the four octave ladders, the detail scale,
//	   and the warp ladder, and lost the four bare wavelength fields, so no
//	   version 1 configuration can be read.
//	3  Elevation. The composite of DESIGN.md 10 — the weighted sum of the four
//	   scales, the contrast pass over the coarse half, regional uplift, the
//	   ridge structure and its directional blur, and the sea-level rescale that
//	   puts sea level at exactly zero — with local relief, land/water, and the
//	   elevation bands. Config gained ElevationConfig, and SeaLevel's interval
//	   closed at both ends, so no version 2 configuration can be read. No value
//	   this version already produced moved: the four scales and the region blend
//	   are untouched, and their golden tables stand.
//	4  Climate. The two independent axes of DESIGN.md 16 — the broad heat zone
//	   field with its regional bias and its elevation lapse rate, the broad
//	   moisture field with its regional bias and its local variation, and the
//	   two band ladders that cut them — under three new hashing domains that
//	   were already reserved. Config gained ClimateConfig, so no version 3
//	   configuration can be read. No value this version already produced moved:
//	   climate reads the elevation composite and nothing reads climate, so the
//	   scale, region, and elevation golden tables stand.
const AlgorithmVersion uint32 = 4
