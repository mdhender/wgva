// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"image/color"
	"math"
)

// RampStop is one anchor of a diagnostic ramp: a position in [0, 1] and the
// color there.
type RampStop struct {
	At    float64
	Color color.RGBA
}

// Ramp maps a normalized scalar to a color by interpolating between its stops.
// Stops ascend by At, the first is at 0 and the last at 1.
type Ramp []RampStop

// SignedRamp is the diagnostic ramp for a field in [-1, +1].
//
// It is diverging rather than sequential because the fields it draws are
// signed, and a sequential ramp hides the one feature that matters most when
// reading raw noise: where the field crosses zero. The midpoint is close to
// neutral so that a coastline-shaped contour is visible without squinting, and
// the two ends are far apart in both lightness and hue so the picture survives
// being looked at on a bad monitor.
var SignedRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0x05, G: 0x1a, B: 0x3d, A: 0xff}},
	{At: 0.25, Color: color.RGBA{R: 0x2a, G: 0x6f, B: 0xad, A: 0xff}},
	{At: 0.50, Color: color.RGBA{R: 0xf2, G: 0xef, B: 0xe6, A: 0xff}},
	{At: 0.75, Color: color.RGBA{R: 0xc0, G: 0x82, B: 0x3c, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0x4a, G: 0x22, B: 0x0e, A: 0xff}},
}

// Background is what a pixel outside the window's cells is painted. Nothing in a
// correct render reaches it; it is a color rather than transparency so that an
// off-by-one in the rasterizer is visible in the picture rather than invisible
// against whatever the viewer composites onto.
var Background = color.RGBA{R: 0x14, G: 0x14, B: 0x16, A: 0xff}

// At returns the color at t, clamped to [0, 1].
//
// The interpolation is linear in sRGB. That is not perceptually even, and for a
// diagnostic ramp it does not need to be: the stops are chosen so the bands read
// distinctly, and a color space conversion here would be a transcendental
// function in a package that draws pictures of a module that forbids them.
func (r Ramp) At(t float64) color.RGBA {
	if math.IsNaN(t) {
		return Background
	}
	t = min(max(t, 0), 1)

	for i := 1; i < len(r); i++ {
		if t > r[i].At {
			continue
		}
		lo, hi := r[i-1], r[i]
		span := hi.At - lo.At
		if span <= 0 {
			return hi.Color
		}
		u := (t - lo.At) / span
		return color.RGBA{
			R: lerpChannel(lo.Color.R, hi.Color.R, u),
			G: lerpChannel(lo.Color.G, hi.Color.G, u),
			B: lerpChannel(lo.Color.B, hi.Color.B, u),
			A: 0xff,
		}
	}
	return r[len(r)-1].Color
}

func lerpChannel(a, b uint8, t float64) uint8 {
	v := float64(a) + (float64(b)-float64(a))*t
	return uint8(min(max(math.Round(v), 0), 255))
}

// ElevationRamp is the diagnostic ramp for the elevation scalar of
// DESIGN.md 14, whose zero is sea level.
//
// The break at 0.5 is deliberate and is the whole reason this ramp exists
// rather than SignedRamp: the two stops either side of the midpoint are a step
// rather than a blend, so a coastline is drawn as a line at exactly the place
// the land/water rule puts it. A ramp that faded through the middle would make
// the one contour this layer is read for the one contour it could not show.
//
// Below the break it runs deep ocean to shelf; above it, coastal green through
// upland brown to snow. Those are conventions rather than data — the bands are
// cut by ElevationBands and a color is not a threshold — but a diagnostic ramp
// that disagreed with every atlas ever printed would be read wrong.
var ElevationRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0x04, G: 0x14, B: 0x33, A: 0xff}},
	{At: 0.35, Color: color.RGBA{R: 0x1d, G: 0x52, B: 0x8c, A: 0xff}},
	{At: 0.50, Color: color.RGBA{R: 0x74, G: 0xb3, B: 0xd4, A: 0xff}},
	{At: 0.5001, Color: color.RGBA{R: 0x3f, G: 0x6f, B: 0x3a, A: 0xff}},
	{At: 0.65, Color: color.RGBA{R: 0x8f, G: 0x9c, B: 0x4a, A: 0xff}},
	{At: 0.82, Color: color.RGBA{R: 0x9c, G: 0x6f, B: 0x40, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0xf4, G: 0xf4, B: 0xf0, A: 0xff}},
}

// UnitRamp is the diagnostic ramp for a field in [0, 1]. It is sequential
// rather than diverging because such a field has no zero crossing to find: what
// a reader wants from relief is where it is high, not where it changes sign.
var UnitRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0x10, G: 0x14, B: 0x20, A: 0xff}},
	{At: 0.35, Color: color.RGBA{R: 0x39, G: 0x5c, B: 0x7a, A: 0xff}},
	{At: 0.70, Color: color.RGBA{R: 0xc0, G: 0x9a, B: 0x54, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0xfb, G: 0xf3, B: 0xdc, A: 0xff}},
}
