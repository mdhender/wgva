// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"errors"
	"fmt"

	"github.com/mdhender/wgva"
)

// ErrRotation is returned for a rotation outside 0..5.
//
// A rotation is a direction offset and there are six directions, so a seventh
// is not a value that needs reducing — it is a value that came from somewhere
// wrong. DESIGN.md 28 says the rotation is validated on write and again on read
// and that an out-of-range stored value is a typed refusal, never a FloorMod:
// the origin and rotation are what every coordinate and heading a player has
// ever been given *mean*, so quietly repairing one would silently relabel all
// of it.
var ErrRotation = errors.New("rotation out of range")

// FrameError names the rotation that failed and wraps the reason.
type FrameError struct {
	Rotation int
	Err      error
}

func (e *FrameError) Error() string {
	return fmt.Sprintf("rotation %d: %v: not in 0..5", e.Rotation, e.Err)
}

func (e *FrameError) Unwrap() error { return e.Err }

// PlayerFrame is the frame one player sees the world through: an origin hex and
// a rotation, assigned at creation and never moved.
//
// It lives in render rather than in wgva because a frame exists to present the
// world to somebody, and it lives here rather than in store because store
// persists only the three scalars. A player frame must never reach the
// Generator: fields are sampled on canonical coordinates only, or
// Tile = F(seed, coordinate, version, radius, configuration) would acquire a
// per-player term. See DESIGN.md 28 and appendix A, *Coordinate frames*.
//
// The fields are unexported and the constructor validates, for the reason
// wgva.Coord's are: a frame that could be built with a rotation of nine would be
// a frame every conversion had to re-check.
type PlayerFrame struct {
	origin   wgva.Coord
	rotation int
}

// NewPlayerFrame returns the frame for an origin and a rotation, or a typed
// refusal.
func NewPlayerFrame(origin wgva.Coord, rotation int) (PlayerFrame, error) {
	if rotation < 0 || rotation > 5 {
		return PlayerFrame{}, &FrameError{Rotation: rotation, Err: ErrRotation}
	}
	return PlayerFrame{origin: origin, rotation: rotation}, nil
}

// Origin is the canonical coordinate the player calls (0, 0).
func (f PlayerFrame) Origin() wgva.Coord { return f.origin }

// Rotation is the absolute direction the player calls north.
func (f PlayerFrame) Rotation() int { return f.rotation }

// ToCanonical converts a player-relative coordinate to the canonical one.
//
//	absolute = normalize( rotate^k( relative ) + playerOrigin )
//
// See DESIGN.md appendix A, *Coordinate frames*.
func (f PlayerFrame) ToCanonical(relative wgva.Coord) wgva.Coord {
	turned := relative.Rotate(f.rotation)
	return wgva.NewCoord(
		int64(turned.Q())+int64(f.origin.Q()),
		int64(turned.R())+int64(f.origin.R()),
	)
}

// ToFrame converts a canonical coordinate to the player-relative one. It is the
// exact inverse of ToCanonical, which a test asserts in both directions.
//
// The difference is taken through NewCoord, so what comes back is the canonical
// representative of "how far away is this" on a wrapped map — the short way
// round rather than the long one. On a wrapped world there is no other honest
// answer, and it is the same answer every player gets for the same pair.
func (f PlayerFrame) ToFrame(absolute wgva.Coord) wgva.Coord {
	difference := wgva.NewCoord(
		int64(absolute.Q())-int64(f.origin.Q()),
		int64(absolute.R())-int64(f.origin.R()),
	)
	return difference.Rotate(-f.rotation)
}

// Direction converts a player-relative direction index to the absolute one:
//
//	absoluteDirection = (playerDirection + k) mod 6
func (f PlayerFrame) Direction(relative int) int {
	return ((relative+f.rotation)%6 + 6) % 6
}

// RelativeDirection converts an absolute direction index to the player's.
func (f PlayerFrame) RelativeDirection(absolute int) int {
	return ((absolute-f.rotation)%6 + 6) % 6
}

// Compass returns the six compass points as this player reads them: clockwise
// from their own north.
//
// The clockwise walk decreases the absolute index, because index order is
// counter-clockwise as a viewer sees the world. In the player's own numbering
// the walk is always 0, 5, 4, 3, 2, 1 whatever their rotation is, and 3 is
// always behind them. See DESIGN.md appendix A, *Rotation senses*.
func (f PlayerFrame) Compass() [6]Point { return Compass(f.rotation) }

// Turn is the drawing half of the frame: how many sixths of a turn a window has
// to be rotated so that this player's north is at the top of the image.
//
// It is rotation - 2, and the 2 is the layout constant: screen-up is absolute
// direction AdminFrameNorth and a frame's north is relative direction 0. See
// DESIGN.md 29.1.
func (f PlayerFrame) Turn() int {
	return ((f.rotation-AdminFrameNorth)%6 + 6) % 6
}

// Viewport returns the window this player sees around one coordinate of their
// own frame.
//
// The turn is the frame's and the pivot is the window's centre rather than the
// frame's origin, which is what a player window should do: the tile they are
// looking at stays where it is and everything around it is oriented by their
// north. Pivoting on the origin instead would move the centre somewhere else
// every time the player was not standing on it.
//
// Nothing about this reaches the generator. A Viewport converts a cell to a
// canonical coordinate and the coordinate is what is sampled; the frame decides
// which coordinates, never what they generate.
func (f PlayerFrame) Viewport(relativeCenter wgva.Coord, cols, rows int) (Viewport, error) {
	return NewViewport(f.ToCanonical(relativeCenter), cols, rows, f.Turn(), 1)
}
