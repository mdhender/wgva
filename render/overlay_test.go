// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"image"
	"testing"

	"github.com/mdhender/wgva"
)

func playerWindow(t *testing.T) (*wgva.Generator, Viewport, Layer) {
	t.Helper()
	g := wgva.NewDefault(0x0123456789abcdef)
	v, err := NewViewport(wgva.NewCoord(60, -20), 11, 9, 0, 1)
	if err != nil {
		t.Fatalf("NewViewport: %v", err)
	}
	l, err := LayerNamed("terrain")
	if err != nil {
		t.Fatalf("LayerNamed: %v", err)
	}
	return g, v, l
}

func sameImage(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

// TestRenderPlayerWithoutOverlaysIsTheTerrainRender is what "RenderVersion does
// not move for overlay work" means as an assertion. Every pixel the terrain
// renderer produces is bit-identical to what it produced before overlays
// existed, so bumping that version would claim a cache of terrain images is
// stale when it is not.
func TestRenderPlayerWithoutOverlaysIsTheTerrainRender(t *testing.T) {
	g, v, l := playerWindow(t)

	plain, err := Render(g, v, l, 6)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	player, err := RenderPlayer(g, v, l, 6, Overlays{})
	if err != nil {
		t.Fatalf("RenderPlayer: %v", err)
	}
	if !sameImage(plain, player) {
		t.Fatal("a player render with no overlays is not the terrain render")
	}
}

// TestEmptyDiscoverySetMeansFogIsOff is DESIGN.md 29.4 stated as a test. A world
// that records no discoveries is one where exploration is not being tracked, and
// rendering it as a solid rectangle of fog would be an alarming way to say so.
// One discovered tile switches it on.
func TestEmptyDiscoverySetMeansFogIsOff(t *testing.T) {
	g, v, l := playerWindow(t)

	off := Overlays{}
	if off.FogIsOn() {
		t.Fatal("an empty discovery set switched fog on")
	}
	on := Overlays{Discovered: []wgva.Coord{v.Center}}
	if !on.FogIsOn() {
		t.Fatal("one discovered tile did not switch fog on")
	}

	img, err := RenderPlayer(g, v, l, 6, on)
	if err != nil {
		t.Fatalf("RenderPlayer: %v", err)
	}
	// With one tile discovered, the rest of the window is fog. The centre cell
	// is the discovered one, so it is the one place that is not.
	fogged, lit := 0, 0
	for y := range img.Bounds().Dy() {
		for x := range img.Bounds().Dx() {
			switch img.RGBAAt(x, y) {
			case Fog:
				fogged++
			case Background:
			default:
				lit++
			}
		}
	}
	if fogged == 0 {
		t.Error("nothing was fogged")
	}
	if lit == 0 {
		t.Error("the discovered tile was fogged too")
	}
}

// TestFogHidesTerrainForEveryLayerIdentically is the first composition rule. A
// fogged tile that leaked its heat band would be a map telling the player the
// climate of ground they have never seen, so the fogged pixels must not depend
// on which layer was asked for.
func TestFogHidesTerrainForEveryLayerIdentically(t *testing.T) {
	g, v, _ := playerWindow(t)
	o := Overlays{Discovered: []wgva.Coord{v.Center}}

	var first *image.RGBA
	for _, name := range []string{"terrain", "elevation", "temperature", "climate", "rim"} {
		l, err := LayerNamed(name)
		if err != nil {
			t.Fatalf("LayerNamed(%q): %v", name, err)
		}
		img, err := RenderPlayer(g, v, l, 6, o)
		if err != nil {
			t.Fatalf("RenderPlayer(%q): %v", name, err)
		}
		if first == nil {
			first = img
			continue
		}
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				if first.RGBAAt(x, y) == Fog && img.RGBAAt(x, y) != Fog {
					t.Fatalf("layer %q draws %v where the fog is, at (%d, %d)", name, img.RGBAAt(x, y), x, y)
				}
			}
		}
	}
}

// TestFogDoesNotHideThePlayersOwnMarks is the second composition rule, and it
// differs from the first on purpose: a settlement is something the player built,
// and hiding it would be the map lying to its owner.
func TestFogDoesNotHideThePlayersOwnMarks(t *testing.T) {
	g, v, l := playerWindow(t)

	// A settlement on an undiscovered tile, one cell off the centre.
	town := v.CoordAt(v.Cols/2+2, v.Rows/2)
	o := Overlays{
		Discovered:  []wgva.Coord{v.Center},
		Settlements: []Marker{{Coord: town, Name: "Far Hold"}},
	}
	if o.IsDiscovered(town) {
		t.Fatal("the test's settlement is on a discovered tile")
	}

	img, err := RenderPlayer(g, v, l, 8, o)
	if err != nil {
		t.Fatalf("RenderPlayer: %v", err)
	}
	found := false
	for y := range img.Bounds().Dy() {
		for x := range img.Bounds().Dx() {
			if img.RGBAAt(x, y) == Settlement {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the settlement on an undiscovered tile was not drawn")
	}
}

// TestOverlaysSortedIsAStableRenderOrder covers why the slices are sorted rather
// than map-backed: markers overlap pixels, and a map would decide which of two
// touching markers wins by whatever the runtime felt like this iteration.
func TestOverlaysSortedIsAStableRenderOrder(t *testing.T) {
	g, v, l := playerWindow(t)

	a := v.CoordAt(3, 3)
	b := v.CoordAt(4, 3)
	c := v.CoordAt(5, 5)

	forward := Overlays{
		Discovered:  []wgva.Coord{a, b, c},
		Settlements: []Marker{{Coord: a, Name: "one"}, {Coord: b, Name: "two"}},
		Labels:      []Marker{{Coord: c, Name: "three"}},
	}
	backward := Overlays{
		Discovered:  []wgva.Coord{c, b, a},
		Settlements: []Marker{{Coord: b, Name: "two"}, {Coord: a, Name: "one"}},
		Labels:      []Marker{{Coord: c, Name: "three"}},
	}

	first, err := RenderPlayer(g, v, l, 8, forward)
	if err != nil {
		t.Fatalf("RenderPlayer: %v", err)
	}
	second, err := RenderPlayer(g, v, l, 8, backward)
	if err != nil {
		t.Fatalf("RenderPlayer: %v", err)
	}
	if !sameImage(first, second) {
		t.Fatal("the order the overlays arrived in changed the picture")
	}

	// The discovery lookup is a binary search, so it is only correct on a
	// sorted slice. Sorted is what puts it in that order; an unsorted one is a
	// caller's mistake this method exists to absorb.
	sorted := backward.Sorted()
	for _, want := range []wgva.Coord{a, b, c} {
		if !sorted.IsDiscovered(want) {
			t.Errorf("%v is not reported as discovered after sorting", want)
		}
	}
	if sorted.IsDiscovered(v.CoordAt(0, 0)) {
		t.Error("an undiscovered tile is reported as discovered")
	}
}
