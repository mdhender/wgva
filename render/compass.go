// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import "github.com/mdhender/wgva"

// Point is one compass point: its name, the absolute direction index it means
// for the viewer it was built for, and the axial step that takes a coordinate
// one hex that way.
type Point struct {
	Name      string
	Direction int
	DQ, DR    int64
}

// compassNames are the six points in clockwise order, starting at the viewer's
// north.
//
// The clockwise walk **decreases** the direction index. Index order is
// counter-clockwise as a viewer sees the world — direction 1 is a sixth of a
// turn counter-clockwise from direction 0 — and the compass is the ordinary
// clockwise ring a person expects, so the two disagree by a sign. This is the
// one place in the program that knows it, and the table below is the whole
// statement. See DESIGN.md appendix A, *Rotation senses*.
//
// Never abbreviate either sense. With both named, two letters read as
// "clockwise" or as "compass walk", which run opposite ways through the index —
// the worst possible ambiguity in the shortest possible identifier.
var compassNames = [6]string{"N", "NE", "SE", "S", "SW", "NW"}

// Compass returns the six compass points for a viewer at the given rotation, in
// clockwise order beginning at that viewer's north.
//
// A viewer at rotation k perceives absolute direction k as north, so point i is
// absolute direction k-i mod 6.
func Compass(rotation int) [6]Point {
	var out [6]Point
	for i, name := range compassNames {
		dir := ((rotation-i)%6 + 6) % 6
		step := wgva.Origin.Neighbor(dir)
		out[i] = Point{Name: name, Direction: dir, DQ: int64(step.Q()), DR: int64(step.R())}
	}
	return out
}

// AdminCompass returns the compass a diagnostic render is read with. The
// renderer applies no rotation and its flat-top layout puts absolute direction
// AdminFrameNorth at the top of the image, so this is Compass(AdminFrameNorth)
// and the walk is 2, 1, 0, 5, 4, 3.
func AdminCompass() [6]Point { return Compass(AdminFrameNorth) }
