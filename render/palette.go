// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"image/color"
	"math"

	"github.com/mdhender/wgva"
)

// RenderVersion identifies the pixels this package produces. A cached image
// records it, and a binary discards a cache it cannot reproduce.
//
// It is a cache-validity counter and not a semantic version. It covers the
// palettes, the layer table, the hex canvas, and the offset scheme — everything
// that decides what color a tile is drawn and where. It does **not** cover the
// world: a change to the generator moves wgva.AlgorithmVersion, and an image is
// a function of both.
//
// Version history:
//
//	1  Initial renderer. The seventeen layers, the diagnostic ramps, the climate
//	   table, the terrain list, the flat-top even-q canvas, and the grid render.
//
// Overlay work does not move this. Every pixel the terrain renderer produces is
// bit-identical to what it produced before overlays existed, and bumping it
// would claim a cache of terrain PNGs is stale when it is not. Note also what it
// does not reach: a player image depends on the overlays as well as the palette,
// and overlays are mutable player state with no version anywhere in the system.
// That is a reason not to cache one, and it is why a world-backed image carries
// no entity tag. See DESIGN.md 29.3 and 29.4.
const RenderVersion uint32 = 1

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

// The player overlay palette. These three are the only colors a player render
// adds, and they are deliberately outside every terrain and every ramp: a fog
// that could be mistaken for ocean, or a settlement that could be mistaken for
// a volcano, would be a map that lied by resembling something.
var (
	// Fog is an undiscovered tile. It replaces the terrain color for every
	// layer identically, because a fogged tile that leaked its heat band would
	// be a map telling the player the climate of ground they have never seen.
	Fog = color.RGBA{R: 0x2b, G: 0x2d, B: 0x33, A: 0xff}

	// Settlement is a place the player built. It is drawn whether or not the
	// tile under it is discovered; hiding it would be the map lying to its
	// owner.
	Settlement = color.RGBA{R: 0xf2, G: 0xe8, B: 0xd5, A: 0xff}

	// Label is a place the player named.
	Label = color.RGBA{R: 0xf0, G: 0xc6, B: 0x74, A: 0xff}
)

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

// TemperatureRamp is the diagnostic ramp for the heat scalar of DESIGN.md 16,
// which runs -1 polar to +1 hot.
//
// It is its own ramp rather than SignedRamp because a temperature has a
// convention and a reader brings it to the picture: cold is blue and hot is
// red, and a scale that put brown at the hot end and navy at the cold one —
// which is what SignedRamp does — would be read as the right answer by accident
// and the wrong one wherever the convention is what somebody is relying on.
//
// The midpoint is close to neutral, so the middle of the temperate band reads
// as the middle of the scale rather than as a color of its own.
var TemperatureRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0x16, G: 0x2c, B: 0x63, A: 0xff}},
	{At: 0.25, Color: color.RGBA{R: 0x4d, G: 0x8f, B: 0xc4, A: 0xff}},
	{At: 0.50, Color: color.RGBA{R: 0xf0, G: 0xea, B: 0xdc, A: 0xff}},
	{At: 0.75, Color: color.RGBA{R: 0xdd, G: 0x8b, B: 0x3a, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0x8c, G: 0x1f, B: 0x1a, A: 0xff}},
}

// MoistureRamp is the diagnostic ramp for the moisture scalar of DESIGN.md 16,
// which runs -1 arid to +1 saturated.
//
// It runs desert ochre through neutral to deep green, which is the other
// convention a reader brings. It is deliberately *not* the temperature ramp
// with different stops: the two axes are independent and are read side by side,
// and two pictures in the same colors invite somebody to compare them as though
// they were one quantity. See DESIGN.md 16.1.
var MoistureRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0x8a, G: 0x67, B: 0x24, A: 0xff}},
	{At: 0.25, Color: color.RGBA{R: 0xd2, G: 0xb1, B: 0x6a, A: 0xff}},
	{At: 0.50, Color: color.RGBA{R: 0xef, G: 0xec, B: 0xe0, A: 0xff}},
	{At: 0.75, Color: color.RGBA{R: 0x5a, G: 0x9e, B: 0x86, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0x10, G: 0x3f, B: 0x45, A: 0xff}},
}

// BasinRamp is the diagnostic ramp for the basin composite of DESIGN.md 17,
// which runs -1 a rise that sheds water to +1 a closed hollow.
//
// It is diverging about zero because the quantity is signed and the interesting
// contour is where it changes sign: a basin map is read for where the hollows
// are, and a sequential ramp would draw the neutral two thirds of the world as
// a gradient and leave the boundary nowhere in particular.
//
// The high end is the wet end, which is the convention the moisture ramp
// already establishes, and the low end is the bare dome of a rise.
var BasinRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0xb0, G: 0x8e, B: 0x54, A: 0xff}},
	{At: 0.35, Color: color.RGBA{R: 0xd8, G: 0xcd, B: 0xb4, A: 0xff}},
	{At: 0.50, Color: color.RGBA{R: 0xef, G: 0xec, B: 0xe4, A: 0xff}},
	{At: 0.70, Color: color.RGBA{R: 0x69, G: 0x8f, B: 0xa8, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0x14, G: 0x33, B: 0x52, A: 0xff}},
}

// VolcanicRamp is the diagnostic ramp for the volcanic tendency, which runs -1
// to +1.
//
// It is nearly flat over most of its range and rises sharply at the top, which
// is deliberate and is the one ramp in this file that is not chosen for
// legibility across the whole scale. The tendency is a field everywhere and
// terrain reads only its top few percent, so what somebody drawing this window
// wants to see is where the provinces are — and a ramp that spent half its
// contrast on the difference between -0.8 and -0.2 would draw that in a color
// nobody can pick out.
var VolcanicRamp = Ramp{
	{At: 0.00, Color: color.RGBA{R: 0x1a, G: 0x1a, B: 0x1e, A: 0xff}},
	{At: 0.55, Color: color.RGBA{R: 0x3a, G: 0x36, B: 0x38, A: 0xff}},
	{At: 0.78, Color: color.RGBA{R: 0x7a, G: 0x4a, B: 0x30, A: 0xff}},
	{At: 0.90, Color: color.RGBA{R: 0xc4, G: 0x5c, B: 0x22, A: 0xff}},
	{At: 1.00, Color: color.RGBA{R: 0xff, G: 0xd2, B: 0x6a, A: 0xff}},
}

// terrainColors is the color each terrain is painted, by value.
//
// These are conventions rather than data — the classification is what a game
// reads and a color is not a threshold — but a legend that disagreed with every
// atlas ever printed would be read wrong, so water is blue, forest is green,
// desert is sand, and ice is white. Within a family the variants differ in
// lightness rather than in hue, so that "which family is this" survives being
// looked at on a bad monitor and "which variant" is available to anyone
// looking closely.
//
// The two inland-water entries are here even though DESIGN.md 17.1 produces
// neither, because the legend lists every declared terrain: a row reading zero
// is usually the row somebody is trying to move off zero, and a swatch table
// with a hole in it would index wrong besides.
var terrainColors = map[wgva.Terrain]color.RGBA{
	wgva.TerrainDeepOcean:    {R: 0x04, G: 0x14, B: 0x2b, A: 0xff},
	wgva.TerrainOcean:        {R: 0x0d, G: 0x3a, B: 0x6b, A: 0xff},
	wgva.TerrainShallowSea:   {R: 0x2f, G: 0x7f, B: 0xb5, A: 0xff},
	wgva.TerrainCoastalWater: {R: 0x74, G: 0xb3, B: 0xd4, A: 0xff},

	wgva.TerrainInlandSea: {R: 0x1f, G: 0x5e, B: 0x8c, A: 0xff},
	wgva.TerrainLake:      {R: 0x3d, G: 0x86, B: 0xb8, A: 0xff},

	wgva.TerrainGlacialIce: {R: 0xee, G: 0xf4, B: 0xf8, A: 0xff},
	wgva.TerrainTundra:     {R: 0x9a, G: 0xa7, B: 0x9a, A: 0xff},

	wgva.TerrainMarsh: {R: 0x5d, G: 0x7a, B: 0x52, A: 0xff},
	wgva.TerrainSwamp: {R: 0x3f, G: 0x5c, B: 0x3a, A: 0xff},
	wgva.TerrainBog:   {R: 0x6b, G: 0x6f, B: 0x4e, A: 0xff},

	wgva.TerrainDesert:    {R: 0xd9, G: 0xc0, B: 0x7a, A: 0xff},
	wgva.TerrainBadlands:  {R: 0xb0, G: 0x7a, B: 0x4e, A: 0xff},
	wgva.TerrainScrubland: {R: 0xa8, G: 0x9a, B: 0x5e, A: 0xff},

	wgva.TerrainPlains:    {R: 0xa7, G: 0xbd, B: 0x72, A: 0xff},
	wgva.TerrainGrassland: {R: 0x8f, G: 0xb4, B: 0x5c, A: 0xff},
	wgva.TerrainSteppe:    {R: 0xb9, G: 0xb0, B: 0x71, A: 0xff},
	wgva.TerrainSavanna:   {R: 0xc9, G: 0xb4, B: 0x5a, A: 0xff},

	wgva.TerrainBorealForest:    {R: 0x2f, G: 0x57, B: 0x41, A: 0xff},
	wgva.TerrainTemperateForest: {R: 0x3f, G: 0x7a, B: 0x3a, A: 0xff},
	wgva.TerrainRainforest:      {R: 0x1f, G: 0x5a, B: 0x2c, A: 0xff},

	wgva.TerrainHills:    {R: 0x8a, G: 0x82, B: 0x57, A: 0xff},
	wgva.TerrainMountain: {R: 0x8a, G: 0x8a, B: 0x8a, A: 0xff},
	wgva.TerrainAlpine:   {R: 0xc7, G: 0xcc, B: 0xd1, A: 0xff},

	wgva.TerrainVolcano:          {R: 0x7a, G: 0x2a, B: 0x24, A: 0xff},
	wgva.TerrainVolcanicHighland: {R: 0x5c, G: 0x40, B: 0x38, A: 0xff},

	wgva.TerrainCoast: {R: 0xd8, G: 0xcf, B: 0xa5, A: 0xff},
}

// climateColors is the color each cell of the two-axis band table is painted,
// as five rows of five: polar to hot down, arid to saturated across.
//
// It is one table rather than two ramps multiplied together, and that is the
// point of drawing climate at all. The two axes are independent, so a cell is
// not a blend of a temperature color and a rainfall color — a polar desert and
// a polar rainforest are both ordinary places and a multiplied palette would
// make them two shades of the same thing.
//
// Read across for moisture: pale and dry on the left, deep and wet on the
// right. Read down for heat: the blues of a polar row, through the greens of
// the temperate rows, to the ochres of a hot one. The two directions use
// different properties — lightness across, hue down — so that a reader can tell
// which axis a difference is on.
var climateColors = [5][5]color.RGBA{
	{ // polar
		{R: 0xc9, G: 0xd4, B: 0xe0, A: 0xff}, {R: 0xb3, G: 0xc4, B: 0xd8, A: 0xff},
		{R: 0x9a, G: 0xb2, B: 0xcd, A: 0xff}, {R: 0x7e, G: 0x9c, B: 0xc0, A: 0xff},
		{R: 0x62, G: 0x88, B: 0xb4, A: 0xff},
	},
	{ // cold
		{R: 0xcb, G: 0xd3, B: 0xc9, A: 0xff}, {R: 0xae, G: 0xc3, B: 0xb6, A: 0xff},
		{R: 0x8f, G: 0xb2, B: 0xa2, A: 0xff}, {R: 0x6f, G: 0xa0, B: 0x8e, A: 0xff},
		{R: 0x4f, G: 0x8e, B: 0x7b, A: 0xff},
	},
	{ // temperate
		{R: 0xde, G: 0xd9, B: 0xb4, A: 0xff}, {R: 0xc8, G: 0xcf, B: 0x93, A: 0xff},
		{R: 0xa9, G: 0xc1, B: 0x76, A: 0xff}, {R: 0x83, G: 0xb2, B: 0x5c, A: 0xff},
		{R: 0x5d, G: 0xa2, B: 0x44, A: 0xff},
	},
	{ // warm
		{R: 0xe6, G: 0xcf, B: 0x94, A: 0xff}, {R: 0xd6, G: 0xc4, B: 0x73, A: 0xff},
		{R: 0xbd, G: 0xb9, B: 0x5a, A: 0xff}, {R: 0x94, G: 0xad, B: 0x4d, A: 0xff},
		{R: 0x6a, G: 0x9f, B: 0x40, A: 0xff},
	},
	{ // hot
		{R: 0xe8, G: 0xbb, B: 0x6e, A: 0xff}, {R: 0xdb, G: 0xa8, B: 0x5a, A: 0xff},
		{R: 0xc7, G: 0x9a, B: 0x4c, A: 0xff}, {R: 0x8f, G: 0x9c, B: 0x3c, A: 0xff},
		{R: 0x4f, G: 0x8f, B: 0x33, A: 0xff},
	},
}
