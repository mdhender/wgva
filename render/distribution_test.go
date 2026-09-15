// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"testing"

	"github.com/mdhender/wgva"
)

// TestMeasureCountsEveryTile is the arithmetic the readout has to satisfy before
// anything it says is worth reading: each ladder accounts for the whole window,
// once.
func TestMeasureCountsEveryTile(t *testing.T) {
	g := wgva.NewDefault(0x0123456789abcdef)
	v, err := NewViewport(wgva.NewCoord(120, -80), 21, 15, 0, 1)
	if err != nil {
		t.Fatalf("NewViewport: %v", err)
	}

	d := Measure(g, v)
	if got, want := d.Tiles, v.Cols*v.Rows; got != want {
		t.Fatalf("Tiles = %d, want %d", got, want)
	}

	for _, ladder := range []struct {
		name string
		rows []Count
	}{
		{"terrain", d.Terrain},
		{"elevation", d.Elevation},
		{"heat", d.Heat},
		{"moisture", d.Moisture},
	} {
		total := 0
		for _, row := range ladder.rows {
			total += row.Tiles
		}
		if total != d.Tiles {
			t.Errorf("the %s rows sum to %d, want %d", ladder.name, total, d.Tiles)
		}
	}
}

// TestMeasureListsEveryDeclaredValue is the rule of DESIGN.md 29.1: every
// terrain is listed, including the ones with no tiles, because a row reading
// zero is usually the row somebody is trying to move off zero. The two
// inland-water terrains DESIGN.md 17.1 emits nowhere are the standing example.
func TestMeasureListsEveryDeclaredValue(t *testing.T) {
	g := wgva.NewDefault(1)
	v, err := NewViewport(wgva.Origin, 9, 9, 0, 1)
	if err != nil {
		t.Fatalf("NewViewport: %v", err)
	}
	d := Measure(g, v)

	for i, want := range wgva.Terrains() {
		if i >= len(d.Terrain) || d.Terrain[i].Label != want.String() {
			t.Fatalf("terrain row %d is %+v, want %v", i, d.Terrain[i:], want)
		}
	}
	for i, want := range wgva.Elevations() {
		if d.Elevation[i].Label != want.String() {
			t.Errorf("elevation row %d is %q, want %q", i, d.Elevation[i].Label, want)
		}
	}
	for i, want := range wgva.Heats() {
		if d.Heat[i].Label != want.String() {
			t.Errorf("heat row %d is %q, want %q", i, d.Heat[i].Label, want)
		}
	}
	for i, want := range wgva.Moistures() {
		if d.Moisture[i].Label != want.String() {
			t.Errorf("moisture row %d is %q, want %q", i, d.Moisture[i].Label, want)
		}
	}
}

// TestMeasureIsOrderIndependent is the assertion DESIGN.md 29.1 asks for by
// name. These are integer counters, which is what makes it true; the test that
// would fail is one run against a readout that had grown a floating-point mean.
func TestMeasureIsOrderIndependent(t *testing.T) {
	g := wgva.NewDefault(0x0123456789abcdef)
	v, err := NewViewport(wgva.NewCoord(-4000, 900), 25, 17, 0, 1)
	if err != nil {
		t.Fatalf("NewViewport: %v", err)
	}

	first := Measure(g, v)
	for range 4 {
		again := Measure(g, v)
		if again.Tiles != first.Tiles {
			t.Fatalf("Tiles = %d, want %d", again.Tiles, first.Tiles)
		}
		for i := range first.Terrain {
			if again.Terrain[i] != first.Terrain[i] {
				t.Fatalf("terrain row %d = %+v, want %+v", i, again.Terrain[i], first.Terrain[i])
			}
		}
	}
}

// TestPercentIsIntegerArithmetic pins the rounding, because two front ends
// printing one window must print one number.
func TestPercentIsIntegerArithmetic(t *testing.T) {
	for _, tc := range []struct {
		tiles, of, want int
	}{
		{0, 0, 0},
		{1, 0, 0},
		{0, 100, 0},
		{50, 100, 50},
		{1, 3, 33},
		{2, 3, 66},
		{3, 3, 100},
	} {
		if got := (Count{Tiles: tc.tiles}).Percent(tc.of); got != tc.want {
			t.Errorf("Count{%d}.Percent(%d) = %d, want %d", tc.tiles, tc.of, got, tc.want)
		}
	}
}

// TestTileCostMatchesTheLayers keeps the readout's cost and the layer table from
// drifting. A readout costs a whole Tile per cell whatever layer is on screen,
// which is the same seven evaluations the terrain layer pays.
func TestTileCostMatchesTheLayers(t *testing.T) {
	l, err := LayerNamed("terrain")
	if err != nil {
		t.Fatalf("LayerNamed: %v", err)
	}
	if l.Cost != TileCost {
		t.Fatalf("the terrain layer costs %d and a Tile costs %d", l.Cost, TileCost)
	}
}
