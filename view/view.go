// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package view owns the window grammar the web front ends present: what ?q=,
// ?cols=, and ?layer= mean, what a scroll step is, and which refusal a malformed
// one earns.
//
// It is one package rather than one copy per front end because the grammar
// carries invariants with tests behind it, and two copies is two places for
// those to drift. See DESIGN.md 28.
//
// Everything about a *view* is in the URL. The configuration is not, and cannot
// be: it is on the order of a hundred numeric fields and a hundred fields do not
// fit in an address bar. That split is the whole of DESIGN.md 29.1's argument
// for why the terrain tuning tool is stateful and the map viewer is not.
package view

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
)

// The view error model. A malformed request is the caller's, so each of these
// becomes a readable 400 rather than a 500: nothing a caller can type is the
// process's fault.
var (
	// ErrNotANumber is returned for a parameter that is not an integer.
	ErrNotANumber = errors.New("value is not a number")

	// ErrOutOfRange is returned for a coordinate outside the canonical domain.
	// A coordinate is refused rather than wrapped, because a person who typed
	// one digit too many wants to be told, not shown a window on the far side of
	// the world.
	ErrOutOfRange = errors.New("value out of range")
)

// ViewError names the parameter that failed and wraps the reason.
type ViewError struct {
	Param string
	Value string
	Err   error
}

// Error renders the parameter, its value, and the reason.
func (e *ViewError) Error() string {
	return fmt.Sprintf("%s=%q: %v", e.Param, e.Value, e.Err)
}

// Unwrap returns the sentinel reason.
func (e *ViewError) Unwrap() error { return e.Err }

// The bounds a request is clamped to. The counts are clamped rather than
// refused, and the clamps are odd, so a clamped request still has a center cell
// and every control that is expressed relative to one still works.
const (
	MinCols, MaxCols = 1, 1001
	MinRows, MaxRows = 1, 1001

	MinHexRadius, MaxHexRadius = 2, 64
	MinScale, MaxScale         = 1, 16
	MinStride, MaxStride       = 1, 1024
)

// View is one window, fully described. Every field is in the URL.
type View struct {
	Seed wgva.Seed

	// Q and R are the canonical coordinate at the window's center.
	Q, R int64

	Cols, Rows int
	Turn       int
	Layer      string

	// HexRadius is the pixel radius of a hex on the map tab.
	HexRadius int

	// Scale is pixels per cell on the grid tab, and Stride is how many hexes
	// apart the sampled cells are there.
	Scale  int
	Stride int
}

// Defaults returns the window a tab opens on: the origin, a window that fits on
// a screen, and the coarsest layer, which is the one that says whether the world
// has a shape at all.
func Defaults(seed wgva.Seed) View {
	return View{
		Seed:      seed,
		Cols:      41,
		Rows:      31,
		Layer:     render.DefaultLayer,
		HexRadius: 12,
		Scale:     2,
		Stride:    1,
	}
}

// ViewerDefaults returns the window the map viewer opens on.
//
// They differ from the tuning tool's on two counts, and both are about what the
// tool is for. The window is larger, because the viewer is looking at a world
// rather than at one field of it; and the layer is the elevation scalar rather
// than the coarsest noise scale, because somebody opening a saved world wants to
// see land and water, not the composition behind them. See DESIGN.md 29.3.
func ViewerDefaults(seed wgva.Seed) View {
	v := Defaults(seed)
	v.Cols, v.Rows = 61, 45
	v.HexRadius = 10
	v.Layer = "elevation"
	return v
}

// ScrollStep is how far one click of a compass control moves the map tab's
// window, in whole hexes. GridScrollStep is the same step on the grid tab.
//
// **Scroll distances are whole hexes**, counted in tiles and never in pixels, so
// a step means the same thing at every zoom and a step followed by its opposite
// returns to exactly the coordinate it started from. North and south move a
// third of the rows; the four diagonals move a third of the columns. Opposite
// points get the same distance, which is what makes the round trip exact rather
// than nearly exact. See DESIGN.md 29.3.
//
// A third rather than a half because of what a click is for. Half a window
// leaves one strip of what was on screen and puts the feature somebody was
// following at the far edge; a third keeps two thirds of the picture, so the
// thing being tracked stays visible and the eye can follow it across rather than
// re-finding it after every click.
func (v View) ScrollStep(point string) int { return v.scrollStep(point, 1) }

// GridScrollStep is the scroll step on the grid tab, where a cell is not a hex.
//
// It is the same third of the same window, converted to hexes: a grid cell
// stands for Stride hexes, so a third of the columns is Cols/3*Stride hexes and
// a click moves a third of the picture at every stride. The map tab's step needs
// no such conversion because a hex render is a stride of one, which is also why
// this is a second method rather than ScrollStep reading v.Stride — a view
// carries the grid's stride in its URL whichever tab is drawing it, and a map
// scrolling by it would leap sixty-four screens on a click meant to move one.
func (v View) GridScrollStep(point string) int { return v.scrollStep(point, v.Stride) }

// scrollStep is a third of the window along the axis the point moves on, in
// cells, converted to hexes at the given stride.
func (v View) scrollStep(point string, stride int) int {
	cells := v.Cols
	if strings.EqualFold(point, "N") || strings.EqualFold(point, "S") {
		cells = v.Rows
	}
	return max(cells/3, 1) * max(stride, 1)
}

// Parse applies the query parameters that are present to base and returns the
// result. Absent parameters keep base's value, which is what makes every control
// a link that changes one thing.
func Parse(base View, q url.Values) (View, error) {
	v := base

	for _, p := range []struct {
		name  string
		field *int64
	}{
		{"q", &v.Q},
		{"r", &v.R},
	} {
		if text := q.Get(p.name); text != "" {
			n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
			if err != nil {
				return View{}, &ViewError{Param: p.name, Value: text, Err: ErrNotANumber}
			}
			if n < -wgva.WorldRadius || n > wgva.WorldRadius {
				return View{}, &ViewError{Param: p.name, Value: text, Err: ErrOutOfRange}
			}
			*p.field = n
		}
	}
	// s is accepted as a third component so a coordinate can be pasted whole.
	// It is checked for consistency rather than stored: q + r + s is zero, and a
	// triple that does not satisfy it is a typo worth naming.
	if text := q.Get("s"); text != "" {
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return View{}, &ViewError{Param: "s", Value: text, Err: ErrNotANumber}
		}
		if v.Q+v.R+n != 0 {
			return View{}, &ViewError{Param: "s", Value: text, Err: ErrOutOfRange}
		}
	}

	for _, p := range []struct {
		name    string
		field   *int
		lo, hi  int
		makeOdd bool
	}{
		{"cols", &v.Cols, MinCols, MaxCols, true},
		{"rows", &v.Rows, MinRows, MaxRows, true},
		{"hex-radius", &v.HexRadius, MinHexRadius, MaxHexRadius, false},
		{"scale", &v.Scale, MinScale, MaxScale, false},
		{"stride", &v.Stride, MinStride, MaxStride, false},
	} {
		if text := q.Get(p.name); text != "" {
			n, err := strconv.Atoi(strings.TrimSpace(text))
			if err != nil {
				return View{}, &ViewError{Param: p.name, Value: text, Err: ErrNotANumber}
			}
			n = min(max(n, p.lo), p.hi)
			if p.makeOdd && n%2 == 0 {
				n = min(n+1, p.hi)
			}
			*p.field = n
		}
	}

	if text := q.Get("turn"); text != "" {
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			return View{}, &ViewError{Param: "turn", Value: text, Err: ErrNotANumber}
		}
		// A turn is modular by construction — six sixths is the identity — so it
		// is reduced rather than refused, and a caller can write turn=7 or
		// turn=-1 and mean what they look like they mean.
		v.Turn = (n%6 + 6) % 6
	}

	if text := q.Get("layer"); text != "" {
		if _, err := render.LayerNamed(text); err != nil {
			return View{}, &ViewError{Param: "layer", Value: text, Err: err}
		}
		v.Layer = text
	}

	return v, nil
}

// Coord returns the window's center as a canonical coordinate.
func (v View) Coord() wgva.Coord { return wgva.NewCoord(v.Q, v.R) }

// Viewport returns the hex tab's window.
func (v View) Viewport() (render.Viewport, error) {
	return render.NewViewport(v.Coord(), v.Cols, v.Rows, v.Turn, 1)
}

// GridViewport returns the grid tab's window, which is the same walk at a
// stride.
func (v View) GridViewport() (render.Viewport, error) {
	return render.NewViewport(v.Coord(), v.Cols, v.Rows, v.Turn, v.Stride)
}

// RenderLayer returns the layer the view names.
func (v View) RenderLayer() (render.Layer, error) { return render.LayerNamed(v.Layer) }

// Values renders the view as query parameters, in a fixed order so that two
// links to the same window are the same string.
func (v View) Values() url.Values {
	q := url.Values{}
	q.Set("q", strconv.FormatInt(v.Q, 10))
	q.Set("r", strconv.FormatInt(v.R, 10))
	q.Set("cols", strconv.Itoa(v.Cols))
	q.Set("rows", strconv.Itoa(v.Rows))
	q.Set("turn", strconv.Itoa(v.Turn))
	q.Set("layer", v.Layer)
	q.Set("hex-radius", strconv.Itoa(v.HexRadius))
	q.Set("scale", strconv.Itoa(v.Scale))
	q.Set("stride", strconv.Itoa(v.Stride))
	return q
}

// Query returns the view's parameters as an encoded query string.
func (v View) Query() string { return v.Values().Encode() }

// Scrolled returns the view moved by steps hexes along one compass point of the
// admin frame.
//
// The compass is the renderer's, which is the only place north exists. A scroll
// control that computed its own direction index would be a second opinion about
// which way is up, and it would be wrong in exactly the way a mirrored layout is
// wrong: every picture would still look right.
func (v View) Scrolled(point string, steps int) View {
	for _, p := range render.AdminCompass() {
		if !strings.EqualFold(p.Name, point) {
			continue
		}
		moved := wgva.NewCoord(v.Q+p.DQ*int64(steps), v.R+p.DR*int64(steps))
		v.Q, v.R = int64(moved.Q()), int64(moved.R())
		return v
	}
	return v
}

// Zoomed returns the view with the hex radius stepped by a factor, clamped.
//
// It is the map tab's zoom, and only the map tab's: the hex radius is the pixel
// radius of a drawn hex and the grid tab draws no hexes. See [View.Strided].
func (v View) Zoomed(factor float64) View {
	v.HexRadius = min(max(int(float64(v.HexRadius)*factor+0.5), MinHexRadius), MaxHexRadius)
	return v
}

// Strided returns the view with the sampling stride stepped by a factor,
// clamped.
//
// It is the grid tab's zoom, and the two tabs zoom along genuinely different
// axes. On the map, zooming changes how large a hex is drawn and the window
// holds the same tiles either way. On the grid, one cell is one block of pixels
// whatever happens, so the only thing that can change how much world is on
// screen is how many hexes each sampled cell stands for — which is the stride.
//
// Zooming *in* therefore **lowers** the stride, because a smaller stride samples
// more finely and shows less world. That inversion is why this is a separate
// method rather than Zoomed with another field name: a caller passing 2 to both
// and expecting both to zoom in would be wrong about exactly one of them.
func (v View) Strided(factor float64) View {
	v.Stride = min(max(int(float64(v.Stride)*factor+0.5), MinStride), MaxStride)
	return v
}

// With returns the view with one parameter replaced, for building a link that
// changes exactly one thing.
func (v View) With(param, value string) (View, error) {
	return Parse(v, url.Values{param: []string{value}})
}

// ParseSeed reads a seed written in decimal or, with an 0x prefix, in hex.
//
// A seed is the one number in this grammar that people copy between a browser,
// a terminal, and a commit message, so both spellings are accepted and the hex
// one is what the tool prints: a world identified as 0x0123456789abcdef is
// recognizable at a glance in a way that its decimal expansion is not.
func ParseSeed(text string) (wgva.Seed, error) {
	text = strings.TrimSpace(text)
	if lower := strings.ToLower(text); strings.HasPrefix(lower, "0x") {
		n, err := strconv.ParseUint(lower[2:], 16, 64)
		if err != nil {
			return 0, &ViewError{Param: "seed", Value: text, Err: ErrNotANumber}
		}
		return wgva.Seed(n), nil
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, &ViewError{Param: "seed", Value: text, Err: ErrNotANumber}
	}
	return wgva.Seed(n), nil
}

// FormatSeed renders a seed the way the tool prints it.
func FormatSeed(s wgva.Seed) string { return fmt.Sprintf("0x%016x", uint64(s)) }
