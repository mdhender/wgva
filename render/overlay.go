// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"cmp"
	"image"
	"image/color"
	"slices"

	"github.com/mdhender/wgva"
)

// Marker is one thing a player put on the map: a coordinate and the name they
// gave it.
//
// The name is carried and is not drawn. Drawing a name needs a font, and the
// dependency map of DESIGN.md appendix B has none — so what this package puts
// on the image is a mark, and the name is for the page beside it. See
// DESIGN.md appendix D.18.
type Marker struct {
	Coord wgva.Coord
	Name  string
}

// Overlays is what the player knows and what the player has built: generated
// terrain and player overlays are stored apart and meet in exactly one place,
// which is [RenderPlayer], at render time, in pixels.
//
// It is a plain value — sorted slices — and this package does not import store.
// The two are siblings, and a renderer that could open a database would be a
// renderer that could be handed a world rather than a viewport. The command does
// the loading. See DESIGN.md 29.4.
//
// Sorted rather than map-backed, because markers overlap pixels: DESIGN.md 29
// requires a stable render order, and a Go map would decide which of two
// touching markers wins by whatever the runtime felt like this iteration.
// [Overlays.Sorted] is what puts a caller's slices in that order, and
// RenderPlayer calls it rather than trusting them.
type Overlays struct {
	// Discovered is every tile the player has seen.
	//
	// **An empty set means fog is switched off, not that nothing has been
	// seen.** A world that records no discoveries is one where exploration is
	// not being tracked, and rendering it as a solid rectangle of fog would be
	// an alarming way to say so. One discovered tile switches it on.
	Discovered []wgva.Coord

	Settlements []Marker
	Labels      []Marker
}

// Sorted returns the overlays with every slice in coordinate order, by q and
// then by r.
func (o Overlays) Sorted() Overlays {
	out := Overlays{
		Discovered:  slices.Clone(o.Discovered),
		Settlements: slices.Clone(o.Settlements),
		Labels:      slices.Clone(o.Labels),
	}
	slices.SortFunc(out.Discovered, compareCoord)
	slices.SortFunc(out.Settlements, func(a, b Marker) int { return compareCoord(a.Coord, b.Coord) })
	slices.SortFunc(out.Labels, func(a, b Marker) int { return compareCoord(a.Coord, b.Coord) })
	return out
}

// FogIsOn reports whether fog of war is being tracked in this world.
func (o Overlays) FogIsOn() bool { return len(o.Discovered) > 0 }

// IsDiscovered reports whether the player has seen a tile. It assumes the
// discovery slice is sorted, which [Overlays.Sorted] is what guarantees.
func (o Overlays) IsDiscovered(c wgva.Coord) bool {
	_, ok := slices.BinarySearchFunc(o.Discovered, c, compareCoord)
	return ok
}

// compareCoord orders coordinates by q and then by r. It is a total order over
// canonical coordinates, which is all a stable render order needs; it is not a
// claim that one tile is north of another.
func compareCoord(a, b wgva.Coord) int {
	if c := cmp.Compare(a.Q(), b.Q()); c != 0 {
		return c
	}
	return cmp.Compare(a.R(), b.R())
}

// RenderPlayer draws the window as a player sees it: the terrain layer under
// fog, with the player's own marks on top.
//
// Two composition rules, and they differ on purpose:
//
//   - **Fog hides terrain.** An undiscovered tile is drawn as the fog color
//     rather than as what is there, for every layer identically. A fogged tile
//     that leaked its heat band would be a map telling the player the climate of
//     ground they have never seen.
//
//   - **Fog does not hide the player's own marks.** A settlement marker is drawn
//     whether or not the tile under it is discovered. It is something the player
//     built; hiding it would be the map lying to its owner.
//
// Nothing composed here can reach back into generation, and a tile's terrain is
// the same whether or not anybody has ever looked at it. RenderVersion does not
// move for overlay work: every pixel the terrain renderer produces is
// bit-identical to what it produced before overlays existed, and with no
// overlays at all this draws exactly what [Render] draws, which a test asserts.
//
// Note what no version here covers: a cached player image depends on the
// overlays as well as the palette, and overlays are mutable player state with no
// version anywhere in the system. That is a reason not to cache one, and it is
// why a world-backed image carries no entity tag. See DESIGN.md 29.3 and 29.4.
func RenderPlayer(g *wgva.Generator, v Viewport, l Layer, hexRadius int, o Overlays) (*image.RGBA, error) {
	canvas, err := newHexCanvas(v, hexRadius)
	if err != nil {
		return nil, err
	}

	o = o.Sorted()
	fog := o.FogIsOn()

	values := sampleCells(g, v, l)
	key := l.Key()
	img := canvas.fill(v, func(col, row int) color.RGBA {
		if fog && !o.IsDiscovered(v.CoordAt(col, row)) {
			return Fog
		}
		return colorOf(key, values[row*v.Cols+col])
	})

	// The marks go on in a fixed order — settlements, then labels, each in
	// coordinate order — so two that touch the same pixels always compose the
	// same way.
	for _, m := range o.Settlements {
		canvas.mark(img, v, m.Coord, Settlement, settlementMark)
	}
	for _, m := range o.Labels {
		canvas.mark(img, v, m.Coord, Label, labelMark)
	}
	return img, nil
}

// markShape reports whether a pixel at (dx, dy) from a hex's center, measured in
// units of the mark's half-width, belongs to the mark.
type markShape func(dx, dy int, half int) bool

// settlementMark is a filled square: the built thing, squared off, so that it
// reads as a structure rather than as terrain.
func settlementMark(dx, dy, half int) bool {
	return abs(dx) <= half && abs(dy) <= half
}

// labelMark is a filled diamond, which is the same size and is not a square, so
// that a label and a settlement on adjacent tiles are told apart at a glance.
func labelMark(dx, dy, half int) bool {
	return abs(dx)+abs(dy) <= half
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// mark paints one marker on the image, if its coordinate is in the window.
//
// The window is walked rather than the coordinate converted, because a window on
// a wrapped world can contain the same canonical coordinate more than once and
// the honest answer is to draw the mark at every cell that is that tile.
func (hc hexCanvas) mark(img *image.RGBA, v Viewport, c wgva.Coord, fill color.RGBA, shape markShape) {
	half := max(hc.hexRadius/2, 1)
	for row := range v.Rows {
		for col := range v.Cols {
			if v.CoordAt(col, row) != c {
				continue
			}
			center := hc.centerOf(col, row)
			for dy := -half; dy <= half; dy++ {
				for dx := -half; dx <= half; dx++ {
					if !shape(dx, dy, half) {
						continue
					}
					x, y := center.X+dx, center.Y+dy
					if x < 0 || x >= hc.width || y < 0 || y >= hc.height {
						continue
					}
					img.SetRGBA(x, y, fill)
				}
			}
		}
	}
}
