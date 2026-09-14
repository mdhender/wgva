// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Package render draws bounded windows of an unbounded world.
//
// Generation is effectively unbounded and rendering is not: every request
// defines a finite viewport and an explicit pixel scale, and renderer pixel
// coordinates never feed back into generation. A Viewport converts a cell of the
// window to a canonical coordinate, the coordinate is what is sampled, and
// nothing about the picture can reach the generator.
//
// This package owns the hexg dependency. It uses hexg for the flat-top layout,
// the offset-coordinate conversion a window rectangle is built from, and hit
// testing — and for nothing else. In particular it does not use hexg's
// wraparound, because ours is the only canonicalizer in the program and a second
// one that disagreed anywhere would be a second tile identity. See DESIGN.md
// 7.1 and 29.
//
// # Where north is
//
// The renderer is where north exists. It draws the admin frame — flat-top, image
// y downward, no rotation — which puts absolute direction 2 at the top of the
// image. Reading the compass clockwise from there walks the direction index
// backwards, because index order is counter-clockwise as a viewer sees it. See
// AdminFrameNorth and DESIGN.md appendix A.
package render

import (
	"errors"
	"fmt"
	"image"
	"math"
	"runtime"
	"sync"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/wgva"
)

// The render error model. Each reason is a sentinel so that a handler can turn
// one into a readable 400 and a test can assert with errors.Is.
var (
	// ErrWindowSize is returned for a column or row count outside its bounds, or
	// one that is even. A window with no center cell has no coordinate to
	// report, and every control in the tuning tool is expressed relative to one.
	ErrWindowSize = errors.New("window size out of range")

	// ErrPixelScale is returned for a hex radius or a pixels-per-cell outside
	// its bounds.
	ErrPixelScale = errors.New("pixel scale out of range")

	// ErrTurn is returned for a turn outside 0..5.
	ErrTurn = errors.New("turn out of range")

	// ErrStride is returned for a sampling stride outside its bounds, or for a
	// stride above one in a hex render, where the hexes would no longer tile.
	ErrStride = errors.New("sampling stride out of range")

	// ErrBudget is returned for a window that costs more generator evaluations
	// than the caller allowed.
	ErrBudget = errors.New("window exceeds the evaluation budget")
)

// RenderError names what was out of range and wraps the reason.
type RenderError struct {
	What   string
	Value  int
	Lo, Hi int
	Err    error
}

// Error renders the value, the reason, and whatever bound the reason has.
func (e *RenderError) Error() string {
	if e.Hi > e.Lo {
		return fmt.Sprintf("%s: %v: %d is outside [%d, %d]", e.What, e.Err, e.Value, e.Lo, e.Hi)
	}
	return fmt.Sprintf("%s: %v: %d", e.What, e.Err, e.Value)
}

// Unwrap returns the sentinel reason.
func (e *RenderError) Unwrap() error { return e.Err }

// The bounds every window is checked against. They are generous: the real bound
// on a window is the evaluation budget, which is what a caller actually cares
// about and what a 400 names. These exist so that an absurd request is refused
// before anything is allocated.
const (
	MaxCols    = 8191
	MaxRows    = 8191
	MaxStride  = 1024
	MaxHexRadi = 128
	MaxPixels  = 64
)

// AdminFrameNorth is the absolute direction at the top of a rendered image.
//
// The diagnostic renderer applies no rotation, and the flat-top layout puts
// direction 2 — axial (0, -1) — at the top, so what it draws is a viewer at
// rotation 2. Everything a person reads as a compass heading is derived from
// this one constant. See DESIGN.md appendix A, *Rotation senses*.
const AdminFrameNorth = 2

// Viewport is a rectangle of even-q offset cells around a center coordinate.
//
// It is the one place a window is described, so the hex path and the grid path
// walk the same cells: RenderGrid is the same walk with hit testing dropped.
type Viewport struct {
	// Center is the canonical coordinate at the middle cell.
	Center wgva.Coord

	// Cols and Rows are cell counts, both odd so a center cell exists.
	Cols, Rows int

	// Turn rotates the sampled region a sixth of a turn about the window
	// centre, in 0..5. A hex grid rotated by a sixth maps onto itself exactly,
	// so nothing is resampled and nothing interpolates: the rotation is integer
	// arithmetic on coordinates and none of DESIGN.md 25 is disturbed. Turn 0 is
	// the identity, which is what lets every golden stand unchanged.
	Turn int

	// Stride is how many offset cells apart the sampled cells are. One is every
	// cell. A stride above one is how the whole world fits in one image: 65,535
	// hexes across at a stride of sixteen is a four-thousand-pixel picture. It
	// is only meaningful for a grid render, because hexes drawn at a stride
	// would no longer tile.
	Stride int
}

// NewViewport returns a validated viewport.
//
// It does not clamp. Clamping a caller's request is the window grammar's job
// and belongs where the request is parsed; by the time a Viewport is built the
// numbers are supposed to be right, and silently changing one here would make
// two callers disagree about what window was drawn.
func NewViewport(center wgva.Coord, cols, rows, turn, stride int) (Viewport, error) {
	if cols < 1 || cols > MaxCols || cols%2 == 0 {
		return Viewport{}, &RenderError{What: "cols", Value: cols, Lo: 1, Hi: MaxCols, Err: ErrWindowSize}
	}
	if rows < 1 || rows > MaxRows || rows%2 == 0 {
		return Viewport{}, &RenderError{What: "rows", Value: rows, Lo: 1, Hi: MaxRows, Err: ErrWindowSize}
	}
	if turn < 0 || turn > 5 {
		return Viewport{}, &RenderError{What: "turn", Value: turn, Lo: 0, Hi: 5, Err: ErrTurn}
	}
	if stride < 1 || stride > MaxStride {
		return Viewport{}, &RenderError{What: "stride", Value: stride, Lo: 1, Hi: MaxStride, Err: ErrStride}
	}
	return Viewport{Center: center, Cols: cols, Rows: rows, Turn: turn, Stride: stride}, nil
}

// adminLayout is the flat-top, even-q layout every window is expressed in. Its
// size and origin are replaced per render; only the orientation and the offset
// convention are fixed here, and those are what "the admin frame" means.
var adminLayout = hexg.NewLayout(hexg.EvenQ, hexg.Point{X: 1, Y: 1}, hexg.Point{X: 0, Y: 0})

// hexOf adapts a canonical coordinate to hexg's Hex.
//
// hexg stores q, r, s as int and documents no bound, and two of its methods
// misbehave at the extremes: Length overflows past 2^62 and returns a negative
// distance, and at the extreme negative value abs is negative, so a hex nine
// quintillion steps out reports a length of zero and claims to be the origin
// (filed upstream as maloquacious/hexg#9). Neither is reachable from here. A
// wgva.Component is an int32 and the canonical domain is a great deal narrower
// than that, so every hex this function produces is small; and this package
// calls neither Length nor Distance in any case.
func hexOf(c wgva.Coord) hexg.Hex {
	return hexg.NewHex(int(c.Q()), int(c.R()))
}

// CoordAt returns the canonical coordinate at one cell of the window.
//
// The walk is in offset space so that a rectangle of pixels is a rectangle of
// cells; the result is converted back to axial, rotated about the window centre
// by the turn, and normalized. A window that runs off the edge of the map wraps,
// which is what the world's topology says it should do.
func (v Viewport) CoordAt(col, row int) wgva.Coord {
	center := hexOf(v.Center)
	oc := adminLayout.CubeToOffset(center)

	h := adminLayout.OffsetToCube(hexg.NewOffsetCoord(
		oc.Col+(col-v.Cols/2)*v.Stride,
		oc.Row+(row-v.Rows/2)*v.Stride,
	))

	// The offset from the centre is small — a window is thousands of cells, not
	// billions — so it is canonical on its own and Rotate is exact on it.
	rel := wgva.NewCoord(int64(h.Q()-center.Q()), int64(h.R()-center.R())).Rotate(v.Turn)
	return wgva.NewCoord(int64(v.Center.Q())+int64(rel.Q()), int64(v.Center.R())+int64(rel.R()))
}

// Evaluations is what one window of one layer costs, in generator evaluations.
//
// This is the unit a budget is counted in rather than tiles, because a tile
// count cannot tell a cheap window from one seven times longer. See
// DESIGN.md 29.1.
func (v Viewport) Evaluations(l Layer) int {
	return v.Cols * v.Rows * l.Cost
}

// CheckBudget reports whether the window is affordable, as an errors.Is-able
// refusal naming the number. A caller turns it into a 400; measuring how long a
// large window takes is a thing the tuning tool is for, so the budget is the
// caller's to raise.
func (v Viewport) CheckBudget(l Layer, budget int) error {
	if n := v.Evaluations(l); n > budget {
		return &RenderError{What: "evaluations", Value: n, Lo: 0, Hi: budget, Err: ErrBudget}
	}
	return nil
}

// sampleCells fills one value per cell, in a fixed cell order.
//
// Goroutines parallelize it across columns. That is safe and deterministic for a
// structural reason rather than by luck: every cell is a pure function of its own
// coordinate written to its own slot, so no scheduling split can reach any value,
// and nothing here accumulates across cells. See DESIGN.md 20 and 22.
func sampleCells(g *wgva.Generator, v Viewport, l Layer) []float64 {
	out := make([]float64, v.Cols*v.Rows)

	workers := min(runtime.GOMAXPROCS(0), v.Cols)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for col := w; col < v.Cols; col += workers {
				for row := range v.Rows {
					out[row*v.Cols+col] = l.Sample(g, v.CoordAt(col, row))
				}
			}
		})
	}
	wg.Wait()
	return out
}

// normalize maps a layer value to the ramp's [0, 1].
func normalize(k Key, value float64) float64 {
	if k.Hi == k.Lo {
		return 0
	}
	return (value - k.Lo) / (k.Hi - k.Lo)
}

// Render draws the window as hexes, at hexRadius pixels from a hex's center to
// one of its corners.
//
// The mosaic is drawn by hit testing rather than by filling polygons: every
// pixel is converted back to the cell that contains it and painted that cell's
// color. That is the one thing hexg's layout is genuinely better at than we
// would be, it leaves no seams between neighbors and no double-painted edges,
// and it keeps the output independent of the order the cells are visited.
func Render(g *wgva.Generator, v Viewport, l Layer, hexRadius int) (*image.RGBA, error) {
	if hexRadius < 1 || hexRadius > MaxHexRadi {
		return nil, &RenderError{What: "hex-radius", Value: hexRadius, Lo: 1, Hi: MaxHexRadi, Err: ErrPixelScale}
	}
	if v.Stride != 1 {
		return nil, &RenderError{What: "stride", Value: v.Stride, Lo: 1, Hi: 1, Err: ErrStride}
	}

	r := float64(hexRadius)
	unplaced := hexg.NewLayout(hexg.EvenQ, hexg.Point{X: r, Y: r}, hexg.Point{X: 0, Y: 0})

	// A flat-top hex reaches hexRadius left and right of its center and
	// sqrt(3)/2 of that above and below. The extremes are all on the border
	// cells, so the border is what is walked.
	halfHeight := math.Sqrt(3) / 2 * r
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, cell := range borderCells(v) {
		p := unplaced.HexToPixel(unplaced.OffsetToCube(hexg.NewOffsetCoord(cell[0], cell[1])))
		minX, maxX = min(minX, p.X-r), max(maxX, p.X+r)
		minY, maxY = min(minY, p.Y-halfHeight), max(maxY, p.Y+halfHeight)
	}

	layout := hexg.NewLayout(hexg.EvenQ, hexg.Point{X: r, Y: r}, hexg.Point{X: -minX, Y: -minY})
	width := int(math.Ceil(maxX - minX))
	height := int(math.Ceil(maxY - minY))

	values := sampleCells(g, v, l)
	key := l.Key()
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	workers := min(runtime.GOMAXPROCS(0), height)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for y := w; y < height; y += workers {
				for x := range width {
					oc := layout.CubeToOffset(layout.PixelToHexRounded(
						hexg.Point{X: float64(x) + 0.5, Y: float64(y) + 0.5},
					))
					c := Background
					if oc.Col >= 0 && oc.Col < v.Cols && oc.Row >= 0 && oc.Row < v.Rows {
						c = key.Ramp.At(normalize(key, values[oc.Row*v.Cols+oc.Col]))
					}
					img.SetRGBA(x, y, c)
				}
			}
		})
	}
	wg.Wait()
	return img, nil
}

// borderCells returns the offset cells on the window's border, in a fixed order.
func borderCells(v Viewport) [][2]int {
	var out [][2]int
	for col := range v.Cols {
		out = append(out, [2]int{col, 0}, [2]int{col, v.Rows - 1})
	}
	for row := range v.Rows {
		out = append(out, [2]int{0, row}, [2]int{v.Cols - 1, row})
	}
	return out
}

// RenderGrid draws the window at pixelsPerCell pixels a side, one square block
// per cell.
//
// It is the same window walk as Render with hit testing dropped, so a million
// tiles can be looked at in one image. Together with Viewport.Stride it is how
// the whole world is drawn at a coarse scale, which is one of the reasons
// DESIGN.md 4.2 settles the radius where it does: "is the rim right everywhere"
// and "do the continents cover the world plausibly" stay questions somebody
// answers by looking.
//
// What it distorts, and the page that shows it must say so:
//
//   - A hex row's centers are sqrt(3)*r apart and a column's are 1.5*r, so
//     drawing both as one pixel stretches the image vertically by about fifteen
//     percent and flattens the half-hex column stagger. That is the price of the
//     view.
//
//   - A stride is point sampling, not averaging. It samples every 6*stride
//     miles or so, which decides how many pixels a feature gets: wavelength
//     divided by 6*stride. The default continental scale is 6000 miles, so it is
//     eighty-three pixels across at a stride of twelve and eleven at a stride of
//     eighty-eight. Below a few tens of pixels a feature stops being a shape and
//     becomes a dot, and that is a property of the window rather than a defect
//     in the render.
//
//   - Aliasing is a second and smaller effect. The shortest wavelength a stride
//     can carry is twice its spacing, 12*stride miles, and an octave below that
//     limit arrives aliased. This is DESIGN.md 9.3's rule with the sampling grid
//     changed: there the tile grid's own Nyquist wavelength of 12 miles bounds
//     the octave ladder, here this stride's bounds what the ladder can be looked
//     at through. At a stride of eighty-eight the limit is 1056 miles and the
//     continental ladder's finest two octaves, at 750 and 375, are under it —
//     which measurement puts at about half again the pixel-to-pixel variation.
//     It roughens the edges of features; it does not create them. A single
//     unaliasable octave at the same stride draws the same picture.

func RenderGrid(g *wgva.Generator, v Viewport, l Layer, pixelsPerCell int) (*image.RGBA, error) {
	if pixelsPerCell < 1 || pixelsPerCell > MaxPixels {
		return nil, &RenderError{What: "scale", Value: pixelsPerCell, Lo: 1, Hi: MaxPixels, Err: ErrPixelScale}
	}

	values := sampleCells(g, v, l)
	key := l.Key()
	img := image.NewRGBA(image.Rect(0, 0, v.Cols*pixelsPerCell, v.Rows*pixelsPerCell))

	workers := min(runtime.GOMAXPROCS(0), v.Rows)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for row := w; row < v.Rows; row += workers {
				for col := range v.Cols {
					c := key.Ramp.At(normalize(key, values[row*v.Cols+col]))
					for dy := range pixelsPerCell {
						for dx := range pixelsPerCell {
							img.SetRGBA(col*pixelsPerCell+dx, row*pixelsPerCell+dy, c)
						}
					}
				}
			}
		})
	}
	wg.Wait()
	return img, nil
}
