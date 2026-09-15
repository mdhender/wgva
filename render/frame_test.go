// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"errors"
	"testing"

	"github.com/mdhender/wgva"
)

func TestNewPlayerFrameRefusesARotation(t *testing.T) {
	for _, rotation := range []int{-1, 6, 7, 1 << 20} {
		_, err := NewPlayerFrame(wgva.Origin, rotation)
		if !errors.Is(err, ErrRotation) {
			t.Errorf("NewPlayerFrame(rotation %d) returned %v, want an error wrapping %v", rotation, err, ErrRotation)
		}
	}
	for rotation := range 6 {
		if _, err := NewPlayerFrame(wgva.Origin, rotation); err != nil {
			t.Errorf("NewPlayerFrame(rotation %d): %v", rotation, err)
		}
	}
}

// TestFrameRoundTrips is the property every player-facing coordinate depends on.
// A settlement one player calls (3, -1) is a different tile under a different
// frame, so a conversion that was not exactly invertible would move somebody's
// town.
func TestFrameRoundTrips(t *testing.T) {
	origins := []wgva.Coord{
		wgva.Origin,
		wgva.NewCoord(17, -400),
		wgva.NewCoord(-wgva.WorldRadius, 3),
		wgva.NewCoord(wgva.WorldRadius, -wgva.WorldRadius),
	}
	for _, origin := range origins {
		for rotation := range 6 {
			f, err := NewPlayerFrame(origin, rotation)
			if err != nil {
				t.Fatalf("NewPlayerFrame: %v", err)
			}
			for _, c := range []wgva.Coord{
				wgva.Origin,
				wgva.NewCoord(1, 0),
				wgva.NewCoord(-3, 9),
				wgva.NewCoord(1234, -5678),
				origin,
			} {
				if got := f.ToCanonical(f.ToFrame(c)); got != c {
					t.Errorf("origin %v rotation %d: ToCanonical(ToFrame(%v)) = %v", origin, rotation, c, got)
				}
				if got := f.ToFrame(f.ToCanonical(c)); got != c {
					t.Errorf("origin %v rotation %d: ToFrame(ToCanonical(%v)) = %v", origin, rotation, c, got)
				}
			}
			if got := f.ToCanonical(wgva.Origin); got != origin {
				t.Errorf("origin %v rotation %d: the player's (0, 0) is %v", origin, rotation, got)
			}
		}
	}
}

// TestFrameNorthIsTheNeighborAhead states what a rotation means: the tile a
// player calls "one step north" is the neighbor in absolute direction k, and
// the player's own numbering for it is relative direction 0.
func TestFrameNorthIsTheNeighborAhead(t *testing.T) {
	for rotation := range 6 {
		f, err := NewPlayerFrame(wgva.NewCoord(50, 60), rotation)
		if err != nil {
			t.Fatalf("NewPlayerFrame: %v", err)
		}
		if got := f.Direction(0); got != rotation {
			t.Errorf("rotation %d: relative direction 0 is absolute %d", rotation, got)
		}
		if got := f.RelativeDirection(rotation); got != 0 {
			t.Errorf("rotation %d: absolute direction %d is relative %d", rotation, rotation, got)
		}

		north := f.Compass()[0]
		if north.Name != "N" || north.Direction != rotation {
			t.Errorf("rotation %d: the compass begins at %+v", rotation, north)
		}
		// The player-relative compass walk is always 0, 5, 4, 3, 2, 1, whatever
		// the rotation is, and 3 is always behind them.
		want := [6]int{0, 5, 4, 3, 2, 1}
		for i, p := range f.Compass() {
			if got := f.RelativeDirection(p.Direction); got != want[i] {
				t.Errorf("rotation %d: compass point %s is relative direction %d, want %d",
					rotation, p.Name, got, want[i])
			}
		}
	}
}

// TestFrameTurnIsTheDrawingHalf pins the layout constant. Screen-up is absolute
// direction AdminFrameNorth and a frame's north is relative direction 0, so the
// window a player sees is turned by rotation - 2. The admin frame is rotation 2
// and therefore turns by nothing, which is what lets every golden stand.
func TestFrameTurnIsTheDrawingHalf(t *testing.T) {
	f, err := NewPlayerFrame(wgva.Origin, AdminFrameNorth)
	if err != nil {
		t.Fatalf("NewPlayerFrame: %v", err)
	}
	if got := f.Turn(); got != 0 {
		t.Fatalf("the admin frame turns by %d, want 0", got)
	}
	for rotation := range 6 {
		f, err := NewPlayerFrame(wgva.Origin, rotation)
		if err != nil {
			t.Fatalf("NewPlayerFrame: %v", err)
		}
		if got, want := f.Turn(), ((rotation-AdminFrameNorth)%6+6)%6; got != want {
			t.Errorf("rotation %d turns by %d, want %d", rotation, got, want)
		}
	}
}

// TestFrameViewportKeepsTheCentre says which pivot a player window uses: the
// tile the player is looking at stays where it is, and everything around it is
// oriented by their north.
func TestFrameViewportKeepsTheCentre(t *testing.T) {
	f, err := NewPlayerFrame(wgva.NewCoord(-300, 45), 5)
	if err != nil {
		t.Fatalf("NewPlayerFrame: %v", err)
	}
	relative := wgva.NewCoord(4, -2)
	v, err := f.Viewport(relative, 11, 9)
	if err != nil {
		t.Fatalf("Viewport: %v", err)
	}
	if got, want := v.Center, f.ToCanonical(relative); got != want {
		t.Errorf("the window is centered on %v, want %v", got, want)
	}
	if got, want := v.CoordAt(v.Cols/2, v.Rows/2), v.Center; got != want {
		t.Errorf("the centre cell is %v, want %v", got, want)
	}
	if got := v.Turn; got != f.Turn() {
		t.Errorf("the window is turned by %d, want the frame's %d", got, f.Turn())
	}
}
