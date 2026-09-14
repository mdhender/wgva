// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"math"
	"math/bits"
)

// Component is the stored width of one axial coordinate component. It is the
// single place the world's size is decided: int16 for the alpha, int32 for the
// shipping world.
//
// The width appears in exactly two places in this module — here, paired with
// WorldRadius, and in the compatibility tests. Nothing else may name a width,
// and a literal 32767 anywhere else is a defect. See DESIGN.md 4.2.
type Component = int16

// WorldRadius is N in DESIGN.md 7.1: the largest value a Component can hold,
// and the radius of the canonical hexagon. It is paired with Component rather
// than chosen independently.
//
// Go cannot compute the maximum of a signed type in a constant expression
// without unsafe, which DESIGN.md 22 forbids, so the pair is written out and
// pinned by TestWorldRadiusMatchesComponent rather than derived.
const WorldRadius int64 = math.MaxInt16

// ComponentWidthBits returns the width of a Component in bits. It is part of
// world topology, so it is recorded in world metadata and hashed into the
// configuration fingerprint. See DESIGN.md 4.2 and 21.2.
//
// It is derived from WorldRadius rather than written down, because the width is
// one decision and naming it a third time is the defect DESIGN.md 4.2 describes.
// A two's complement type of w bits holds a maximum of 2^(w-1) - 1, whose bit
// length is w-1.
func ComponentWidthBits() uint32 {
	return uint32(bits.Len64(uint64(WorldRadius))) + 1
}

// AlgorithmVersion identifies the generation algorithm. A world file records
// it, and a binary refuses a world it cannot reproduce.
//
// Changing a noise formula, a hash domain, a threshold, a weight, the
// axial-to-world embedding, the direction table, or the component width changes
// existing worlds and requires a bump. So does Config gaining a field, because
// DESIGN.md 21.1 forbids defaulting a missing generation-affecting field: no
// tile moves, but an older file can no longer be read. Say which kind of bump
// it is in the commit message. See DESIGN.md 27.
//
// Version history:
//
//	1  Initial algorithm. Coordinates, the wrapped domain, the axial-to-world
//	   embedding, the direction table, and the domain-separated mixer.
const AlgorithmVersion uint32 = 1
