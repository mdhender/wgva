// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image/color"
	"runtime"
	"testing"

	"github.com/maloquacious/hexg"
	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
)

func mustViewport(t *testing.T, center wgva.Coord, cols, rows, turn, stride int) render.Viewport {
	t.Helper()
	v, err := render.NewViewport(center, cols, rows, turn, stride)
	if err != nil {
		t.Fatalf("NewViewport: %v", err)
	}
	return v
}

// TestAdminCompass pins the mapping DESIGN.md 29 asks this package to pin. A
// mirrored layout would keep every golden pixel passing while sending every
// printed heading the wrong way, so the assertion is on the heading rather than
// on the picture.
func TestAdminCompass(t *testing.T) {
	want := []struct {
		name string
		dir  int
		dq   int64
		dr   int64
	}{
		{"N", 2, 0, -1},
		{"NE", 1, +1, -1},
		{"SE", 0, +1, 0},
		{"S", 5, 0, +1},
		{"SW", 4, -1, +1},
		{"NW", 3, -1, 0},
	}

	got := render.AdminCompass()
	for i, w := range want {
		if got[i].Name != w.name || got[i].Direction != w.dir || got[i].DQ != w.dq || got[i].DR != w.dr {
			t.Errorf("compass point %d is %+v, want %s at direction %d, step (%d, %d)",
				i, got[i], w.name, w.dir, w.dq, w.dr)
		}
	}
}

// TestCompassWalkDecreasesTheIndex is the general statement the admin frame is
// one case of. Index order is counter-clockwise; the compass ring is clockwise;
// they therefore disagree by a sign, whatever the viewer's rotation is.
func TestCompassWalkDecreasesTheIndex(t *testing.T) {
	for rotation := range 6 {
		c := render.Compass(rotation)
		if c[0].Direction != rotation {
			t.Errorf("rotation %d: north is direction %d, want %d", rotation, c[0].Direction, rotation)
		}
		for i := 1; i < 6; i++ {
			want := ((c[i-1].Direction-1)%6 + 6) % 6
			if c[i].Direction != want {
				t.Errorf("rotation %d: %s is direction %d, want %d — the compass walk decreases the index",
					rotation, c[i].Name, c[i].Direction, want)
			}
		}
	}
}

// TestNorthIsUpInTheImage is the other half, and it is the half that catches a
// mirrored layout. North must be the neighbor whose center is above this one in
// a flat-top layout with image y increasing downward.
func TestNorthIsUpInTheImage(t *testing.T) {
	layout := hexg.NewLayout(hexg.EvenQ, hexg.Point{X: 10, Y: 10}, hexg.Point{X: 0, Y: 0})
	origin := layout.HexToPixel(hexg.NewHex(0, 0))

	north := render.AdminCompass()[0]
	p := layout.HexToPixel(hexg.NewHex(int(north.DQ), int(north.DR)))
	if p.Y >= origin.Y {
		t.Fatalf("north is at image y %v against the origin's %v; it is not up", p.Y, origin.Y)
	}
	if p.X != origin.X {
		t.Errorf("north is at image x %v against the origin's %v; it is not directly above", p.X, origin.X)
	}
}

// TestViewportCenterIsTheCenter is the property every control in the tuning tool
// is expressed against, and the reason the counts are clamped to odd numbers.
func TestViewportCenterIsTheCenter(t *testing.T) {
	for _, center := range []wgva.Coord{
		wgva.Origin,
		wgva.NewCoord(1, 0),
		wgva.NewCoord(-7, 11),
		wgva.NewCoord(12345, -6789),
	} {
		for _, size := range [][2]int{{1, 1}, {3, 3}, {9, 5}, {101, 77}} {
			for turn := range 6 {
				v := mustViewport(t, center, size[0], size[1], turn, 1)
				if got := v.CoordAt(v.Cols/2, v.Rows/2); got != center {
					t.Fatalf("a %dx%d window at %v turn %d has %v at its center", size[0], size[1], center, turn, got)
				}
			}
		}
	}
}

// TestViewportCellsAreDistinct asserts that a window does not sample a
// coordinate twice. It is cheap and it is the assertion that fails first if the
// offset conversion is ever replaced with arithmetic that looks equivalent.
func TestViewportCellsAreDistinct(t *testing.T) {
	v := mustViewport(t, wgva.NewCoord(500, -300), 31, 21, 0, 1)
	seen := map[wgva.Coord][2]int{}
	for col := range v.Cols {
		for row := range v.Rows {
			c := v.CoordAt(col, row)
			if prev, ok := seen[c]; ok {
				t.Fatalf("%v appears at (%d, %d) and (%d, %d)", c, prev[0], prev[1], col, row)
			}
			seen[c] = [2]int{col, row}
		}
	}
}

// TestViewportTurnRotatesAboutTheCenter pins what a turn is: the same cell of
// the same window, with the offset from the centre rotated in integers. Nothing
// is resampled and nothing interpolates.
func TestViewportTurnRotatesAboutTheCenter(t *testing.T) {
	center := wgva.NewCoord(-40, 90)
	base := mustViewport(t, center, 15, 15, 0, 1)

	for turn := range 6 {
		v := mustViewport(t, center, 15, 15, turn, 1)
		for col := range v.Cols {
			for row := range v.Rows {
				flat := base.CoordAt(col, row)
				rel := wgva.NewCoord(
					int64(flat.Q())-int64(center.Q()),
					int64(flat.R())-int64(center.R()),
				).Rotate(turn)
				want := wgva.NewCoord(int64(center.Q())+int64(rel.Q()), int64(center.R())+int64(rel.R()))
				if got := v.CoordAt(col, row); got != want {
					t.Fatalf("turn %d at (%d, %d) is %v, want %v", turn, col, row, got, want)
				}
			}
		}
	}
}

// TestViewportWrapsAtTheEdge asserts that a window running off the edge of the
// map wraps, which is what the world's topology says it should do. It is also
// the only cheap check that the renderer never hands the generator a
// non-canonical coordinate — it cannot, because Coord has no such constructor,
// but a window that produced nonsense there would be visible here.
func TestViewportWrapsAtTheEdge(t *testing.T) {
	edge := wgva.NewCoord(wgva.WorldRadius, 0)
	v := mustViewport(t, edge, 9, 9, 0, 1)
	for col := range v.Cols {
		for row := range v.Rows {
			c := v.CoordAt(col, row)
			if c.RimDistance() < 0 {
				t.Fatalf("(%d, %d) produced %v, which is outside the map", col, row, c)
			}
		}
	}
}

// TestViewportStrideSkipsCells covers the whole-world grid's mechanism.
func TestViewportStrideSkipsCells(t *testing.T) {
	one := mustViewport(t, wgva.Origin, 9, 9, 0, 1)
	four := mustViewport(t, wgva.Origin, 9, 9, 0, 4)
	if one.CoordAt(4, 4) != four.CoordAt(4, 4) {
		t.Fatal("a stride moved the center cell")
	}
	if one.CoordAt(5, 4) == four.CoordAt(5, 4) {
		t.Fatal("a stride of four sampled the same cell as a stride of one")
	}
	if four.CoordAt(5, 4) != mustViewport(t, wgva.Origin, 33, 9, 0, 1).CoordAt(20, 4) {
		t.Error("a stride of four does not land on every fourth cell of the unstrided window")
	}
}

// TestNewViewportRefusals covers the bounds. A window with an even side has no
// center cell, and every control in the tuning tool is expressed relative to one.
func TestNewViewportRefusals(t *testing.T) {
	cases := []struct {
		name                     string
		cols, rows, turn, stride int
		want                     error
	}{
		{"even columns", 8, 9, 0, 1, render.ErrWindowSize},
		{"even rows", 9, 8, 0, 1, render.ErrWindowSize},
		{"no columns", 0, 9, 0, 1, render.ErrWindowSize},
		{"too many rows", 9, render.MaxRows + 2, 0, 1, render.ErrWindowSize},
		{"negative turn", 9, 9, -1, 1, render.ErrTurn},
		{"turn of six", 9, 9, 6, 1, render.ErrTurn},
		{"no stride", 9, 9, 0, 0, render.ErrStride},
		{"stride too large", 9, 9, 0, render.MaxStride + 1, render.ErrStride},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := render.NewViewport(wgva.Origin, tc.cols, tc.rows, tc.turn, tc.stride)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewViewport returned %v, want an error wrapping %v", err, tc.want)
			}
			if _, ok := errors.AsType[*render.RenderError](err); !ok {
				t.Fatalf("NewViewport returned %T, want a *render.RenderError", err)
			}
		})
	}
}

// TestEvaluationsCountTheWindow pins the unit a window's cost is reported in. A
// tile count cannot tell a cheap window from one seven times longer, which is
// why the figure a front end prints is not one.
//
// There was once a CheckBudget beside this that refused a window costing more
// than a caller allowed. It is gone: the terrain tuning tool exists to be
// pointed at expensive windows, and a person who asks for the whole world at the
// terrain layer is using it correctly. What is owed is the number, not a
// refusal. See DESIGN.md appendix D.18.
func TestEvaluationsCountTheWindow(t *testing.T) {
	v := mustViewport(t, wgva.Origin, 101, 51, 0, 1)

	if got, want := v.Tiles(), 101*51; got != want {
		t.Fatalf("Tiles = %d, want %d", got, want)
	}
	for _, name := range []string{render.DefaultLayer, "terrain", "relief", "climate"} {
		l, err := render.LayerNamed(name)
		if err != nil {
			t.Fatalf("LayerNamed(%q): %v", name, err)
		}
		if got, want := v.Evaluations(l), 101*51*l.Cost; got != want {
			t.Errorf("the %s layer over this window is %d evaluations, want %d", name, got, want)
		}
	}
}

// TestNothingRefusesAnExpensiveWindow states the rule directly: the only bounds
// on a window are the ones that make it unrepresentable.
func TestNothingRefusesAnExpensiveWindow(t *testing.T) {
	g := wgva.NewDefault(1)

	// The largest window the grammar allows, at the most expensive layer. It is
	// drawn at a stride so the test does not generate seven million tiles to
	// prove the point; what is under test is that nothing refuses it.
	v := mustViewport(t, wgva.Origin, 1001, 751, 0, 256)
	l, err := render.LayerNamed("terrain")
	if err != nil {
		t.Fatalf("LayerNamed: %v", err)
	}
	if v.Evaluations(l) < 5_000_000 {
		t.Fatalf("this window costs %d evaluations; the test wants an expensive one", v.Evaluations(l))
	}

	img, err := render.RenderGrid(g, v, l, 1)
	if err != nil {
		t.Fatalf("RenderGrid refused an expensive window: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 1001 || b.Dy() != 751 {
		t.Fatalf("the image is %dx%d, want 1001x751", b.Dx(), b.Dy())
	}
}

// TestLayersAreNamedAndCosted covers the registry.
func TestLayersAreNamedAndCosted(t *testing.T) {
	all := render.AllLayers()
	if len(all) == 0 {
		t.Fatal("there are no layers")
	}
	seen := map[string]bool{}
	for _, l := range all {
		if seen[l.Name] {
			t.Errorf("%q names two layers", l.Name)
		}
		seen[l.Name] = true
		if l.Cost < 1 {
			t.Errorf("%q costs %d evaluations a tile", l.Name, l.Cost)
		}
		if l.Doc == "" {
			t.Errorf("%q has nothing to say about itself", l.Name)
		}
		switch key := l.Key(); key.Kind {
		case render.KeyRamp:
			if len(key.Ramp) == 0 {
				t.Errorf("%q is a scalar ramp with no stops", l.Name)
			}
			if key.Hi <= key.Lo {
				t.Errorf("%q ramps from %v to %v", l.Name, key.Lo, key.Hi)
			}
		case render.KeyClimate:
			// A five by five table, labelled on both axes, row major over the
			// two band ladders. The count is what colorOf indexes, so a legend
			// that lost an entry would paint the wrong cells rather than fail.
			if want := len(key.Rows) * len(key.Cols); len(key.Swatches) != want {
				t.Errorf("%q has %d swatches for a %d by %d table", l.Name, len(key.Swatches), len(key.Rows), len(key.Cols))
			}
			if len(key.Rows) != len(wgva.Heats()) || len(key.Cols) != len(wgva.Moistures()) {
				t.Errorf("%q is a %d by %d table for %d heat and %d moisture bands",
					l.Name, len(key.Rows), len(key.Cols), len(wgva.Heats()), len(wgva.Moistures()))
			}
		case render.KeyTerrain:
			// One swatch per declared terrain, at its own value, which is what
			// makes the legend and the picture one table.
			if len(key.Swatches) != len(wgva.Terrains()) {
				t.Errorf("%q has %d swatches for %d declared terrains", l.Name, len(key.Swatches), len(wgva.Terrains()))
			}
			for i, tr := range wgva.Terrains() {
				if i < len(key.Swatches) && key.Swatches[i].Label != tr.String() {
					t.Errorf("%q swatch %d is %q, want %q; the swatches are indexed by terrain value",
						l.Name, i, key.Swatches[i].Label, tr)
				}
			}
		default:
			t.Errorf("%q has key kind %d, which is not a declared one", l.Name, key.Kind)
		}
		found, err := render.LayerNamed(l.Name)
		if err != nil || found.Name != l.Name {
			t.Errorf("LayerNamed(%q) returned %v, %v", l.Name, found.Name, err)
		}
	}
	if !seen[render.DefaultLayer] {
		t.Errorf("the default layer %q is not in the registry", render.DefaultLayer)
	}
	// A name nothing draws is a refusal rather than a blank window. "rivers" is
	// DESIGN.md 34.1, which is a future extension and not a layer.
	if _, err := render.LayerNamed("rivers"); !errors.Is(err, render.ErrUnknownLayer) {
		t.Fatalf("LayerNamed of a layer that does not exist returned %v, want ErrUnknownLayer", err)
	}
	if got, want := len(all), 17; got != want {
		t.Errorf("there are %d layers, want the %d of DESIGN.md 29", got, want)
	}
}

// TestRampEndpointsAndClamping covers the diagnostic ramp.
func TestRampEndpointsAndClamping(t *testing.T) {
	r := render.SignedRamp
	if got, want := r.At(0), r[0].Color; got != want {
		t.Errorf("At(0) = %v, want %v", got, want)
	}
	if got, want := r.At(1), r[len(r)-1].Color; got != want {
		t.Errorf("At(1) = %v, want %v", got, want)
	}
	if got, want := r.At(-5), r[0].Color; got != want {
		t.Errorf("At(-5) = %v, want the low end %v", got, want)
	}
	if got, want := r.At(5), r[len(r)-1].Color; got != want {
		t.Errorf("At(5) = %v, want the high end %v", got, want)
	}
	for _, stop := range r {
		if got := r.At(stop.At); got != stop.Color {
			t.Errorf("At(%v) = %v, want the stop's own color %v", stop.At, got, stop.Color)
		}
	}
	if got := r.At(0.125); got == r[0].Color || got == r[1].Color {
		t.Error("a value between two stops returned one of them rather than a blend")
	}
	if a := r.At(0.5).A; a != 0xff {
		t.Errorf("the ramp returned alpha %d, want opaque", a)
	}
}

// TestRenderGridMatchesTheCells asserts the grid draws what the layer says, one
// square block per cell, with no interpolation anywhere.
func TestRenderGridMatchesTheCells(t *testing.T) {
	g := wgva.NewDefault(99)
	v := mustViewport(t, wgva.NewCoord(60, -20), 11, 7, 0, 1)
	l, err := render.LayerNamed("regional")
	if err != nil {
		t.Fatalf("LayerNamed: %v", err)
	}

	const scale = 3
	img, err := render.RenderGrid(g, v, l, scale)
	if err != nil {
		t.Fatalf("RenderGrid: %v", err)
	}
	if b := img.Bounds(); b.Dx() != v.Cols*scale || b.Dy() != v.Rows*scale {
		t.Fatalf("the image is %dx%d, want %dx%d", b.Dx(), b.Dy(), v.Cols*scale, v.Rows*scale)
	}

	key := l.Key()
	for col := range v.Cols {
		for row := range v.Rows {
			value := l.Sample(g, v.CoordAt(col, row))
			want := key.Ramp.At((value - key.Lo) / (key.Hi - key.Lo))
			for dy := range scale {
				for dx := range scale {
					got := img.RGBAAt(col*scale+dx, row*scale+dy)
					if got != want {
						t.Fatalf("pixel (%d, %d) of cell (%d, %d) is %v, want %v",
							dx, dy, col, row, got, want)
					}
				}
			}
		}
	}
}

// TestRenderCoversEveryPixel asserts the hex mosaic leaves no gap. Hit testing
// is what makes that true rather than hoped for: every pixel resolves to exactly
// one cell, so there are no seams between neighbors and no double-painted edges.
func TestRenderCoversEveryPixel(t *testing.T) {
	g := wgva.NewDefault(5)
	v := mustViewport(t, wgva.Origin, 9, 9, 0, 1)
	l, _ := render.LayerNamed(render.DefaultLayer)

	img, err := render.Render(g, v, l, 6)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	b := img.Bounds()
	if b.Dx() < 9 || b.Dy() < 9 {
		t.Fatalf("the image is %dx%d, which is too small to hold the window", b.Dx(), b.Dy())
	}

	// The corners of the bounding box fall outside the hex mosaic, so some
	// background is expected. What must not happen is background in the middle.
	if got := img.RGBAAt(b.Dx()/2, b.Dy()/2); got == render.Background {
		t.Error("the center of the image is background, so the mosaic has a hole in it")
	}
}

// TestRenderIsDeterministic is the claim goroutines across columns rest on:
// every cell is a pure function of its own coordinate written to its own slot,
// so no scheduling split can reach any value.
func TestRenderIsDeterministic(t *testing.T) {
	g := wgva.NewDefault(1234)
	v := mustViewport(t, wgva.NewCoord(-900, 450), 25, 17, 2, 1)
	l, _ := render.LayerNamed("local")

	first, err := render.Render(g, v, l, 5)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	restore := runtime.GOMAXPROCS(1)
	serial, err := render.Render(g, v, l, 5)
	runtime.GOMAXPROCS(restore)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(first.Pix) != string(serial.Pix) {
		t.Fatal("the render changed when it was run on one processor; the split is reaching the result")
	}

	again, err := render.Render(g, v, l, 5)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(first.Pix) != string(again.Pix) {
		t.Fatal("two renders of the same window produced different pixels")
	}
}

// TestRenderGolden compares decoded RGBA buffers, never PNG file bytes:
// image/png chooses filters and compression settings that can change between Go
// releases and produce different bytes for identical images. What is recorded is
// a digest of the pixel buffer, plus the image's dimensions, because a digest
// alone says a render changed and not how much.
//
// Updating these values is a rendering compatibility decision and belongs in a
// commit message. They move when the generator moves, when the ramp moves, or
// when hexg's layout moves — the first of those is an AlgorithmVersion change
// and the other two are not, which is why the layer, the seed, and the window
// are all named here.
//
// Run this on both architectures along with the generator's goldens. The
// generator is written to DESIGN.md 25's rules and hexg's layout arithmetic is
// not — its pixel-to-hex solve is a pair of plain multiply-adds, which arm64 is
// free to fuse and amd64 is not — so if a hit test ever lands on the other side
// of a hex boundary because of it, this is the test that says so.
func TestRenderGolden(t *testing.T) {
	// Not the seed the tuning tool opens on, for the reason wgva.goldenSeed
	// gives: re-recording a golden table to tidy a constant spends the one
	// signal a golden table carries.
	g := wgva.NewDefault(0x5747564100000001)
	v := mustViewport(t, wgva.NewCoord(101, -57), 9, 7, 0, 1)
	l, _ := render.LayerNamed(render.DefaultLayer)

	t.Run("hex", func(t *testing.T) {
		img, err := render.Render(g, v, l, 4)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		assertPixels(t, img.Bounds().Dx(), img.Bounds().Dy(), img.Pix,
			56, 52, "573accecb6d022f08bc6dbbe6d1883675d0a8c8649a523aa3c5a7718917ab12a")
	})

	t.Run("grid", func(t *testing.T) {
		img, err := render.RenderGrid(g, v, l, 2)
		if err != nil {
			t.Fatalf("RenderGrid: %v", err)
		}
		assertPixels(t, img.Bounds().Dx(), img.Bounds().Dy(), img.Pix,
			18, 14, "8a6c5dee7846c889e03016118ba6d442ad40f52feb1c07d51b89934b714b41ca")
	})
}

func assertPixels(t *testing.T, width, height int, pix []byte, wantW, wantH int, wantDigest string) {
	t.Helper()
	if width != wantW || height != wantH {
		t.Errorf("the image is %dx%d, want %dx%d", width, height, wantW, wantH)
	}
	sum := sha256.Sum256(pix)
	if got := hex.EncodeToString(sum[:]); got != wantDigest {
		t.Errorf("the decoded RGBA buffer digests to\n\t%s\nwant\t%s", got, wantDigest)
	}
}

// TestBackgroundIsOpaque keeps the one color that is not a ramp value honest. It
// is a color rather than transparency so an off-by-one in the rasterizer shows
// in the picture instead of disappearing into whatever the viewer composites
// onto.
func TestBackgroundIsOpaque(t *testing.T) {
	if render.Background.A != 0xff {
		t.Errorf("the background is %v, which is not opaque", render.Background)
	}
	var _ color.RGBA = render.Background
}

func BenchmarkRenderGrid(b *testing.B) {
	g := wgva.NewDefault(1)
	v, err := render.NewViewport(wgva.Origin, 257, 257, 0, 1)
	if err != nil {
		b.Fatalf("NewViewport: %v", err)
	}
	l, _ := render.LayerNamed(render.DefaultLayer)
	for b.Loop() {
		if _, err := render.RenderGrid(g, v, l, 1); err != nil {
			b.Fatalf("RenderGrid: %v", err)
		}
	}
}

// TestBandLayersPaintSwatchesAndNeverBlends is the assertion the two band
// layers exist for.
//
// A classification is discrete, so every pixel of a `climate` or `terrain`
// window must be exactly one of the legend's colors. A blend between two of
// them would be a color that names nothing — and it is precisely what running a
// classification through a ramp produces, which is the mistake colorOf is
// shaped to prevent.
func TestBandLayersPaintSwatchesAndNeverBlends(t *testing.T) {
	g := wgva.NewDefault(0x0123456789abcdef)
	v := mustViewport(t, wgva.NewCoord(4000, -2500), 41, 41, 0, 7)

	for _, name := range []string{"climate", "terrain"} {
		t.Run(name, func(t *testing.T) {
			l, err := render.LayerNamed(name)
			if err != nil {
				t.Fatalf("LayerNamed: %v", err)
			}
			img, err := render.RenderGrid(g, v, l, 1)
			if err != nil {
				t.Fatalf("RenderGrid: %v", err)
			}

			allowed := map[color.RGBA]string{}
			for _, s := range l.Key().Swatches {
				allowed[s.Color] = s.Label
			}

			seen := map[color.RGBA]bool{}
			for row := range v.Rows {
				for col := range v.Cols {
					got := img.RGBAAt(col, row)
					if _, ok := allowed[got]; !ok {
						t.Fatalf("cell (%d, %d) is %v, which is not one of the %d swatches; a classification must never be blended",
							col, row, got, len(allowed))
					}
					seen[got] = true
				}
			}
			if len(seen) < 2 {
				t.Fatalf("the whole window is one color; the assertion above is vacuous")
			}

			// The picture and the legend index one table, so a cell's color is
			// the swatch at the value the layer sampled.
			for _, cell := range [][2]int{{0, 0}, {v.Cols / 2, v.Rows / 2}, {v.Cols - 1, v.Rows - 1}} {
				value := l.Sample(g, v.CoordAt(cell[0], cell[1]))
				want := l.Key().Swatches[int(value)].Color
				if got := img.RGBAAt(cell[0], cell[1]); got != want {
					t.Errorf("cell (%d, %d) sampled %v and was painted %v, want the swatch %v",
						cell[0], cell[1], value, got, want)
				}
			}
		})
	}
}

// TestClimateLayerIsTheTwoAxesAndNotACombinedValue asserts the flattened index
// the climate layer samples is exactly the pair it came from.
//
// Flattening is how one []float64 carries every layer's samples, and it is the
// one place a combined climate value of the kind DESIGN.md 16.1 forbids could
// creep in. What keeps it honest is that the number is never compared or
// ordered and the key turns it straight back into two bands.
func TestClimateLayerIsTheTwoAxesAndNotACombinedValue(t *testing.T) {
	g := wgva.NewDefault(0x0123456789abcdef)
	l, err := render.LayerNamed("climate")
	if err != nil {
		t.Fatalf("LayerNamed: %v", err)
	}
	key := l.Key()

	v := mustViewport(t, wgva.Origin, 21, 21, 0, 101)
	for row := range v.Rows {
		for col := range v.Cols {
			c := v.CoordAt(col, row)
			tile := g.Tile(c)
			want := wgva.Climate{Heat: tile.Climate.Heat, Moisture: tile.Climate.Moisture}.String()
			if got := key.Swatches[int(l.Sample(g, c))].Label; got != want {
				t.Fatalf("the climate layer at (%d, %d) indexes the swatch %q, want %q", c.Q(), c.R(), got, want)
			}
		}
	}
}

// TestTerrainLayerCostsSeven pins the one thing a front end takes from the
// layer list besides the name.
//
// Terrain and relief read the six neighboring elevation scalars and cost seven
// evaluations apiece; everything else costs one, including climate, which reads
// the elevation at its own tile and nothing around it. A cost counted in tiles
// could not tell a cheap window from one seven times longer, which is the whole
// reason Layer.Cost exists.
func TestTerrainLayerCostsSeven(t *testing.T) {
	for name, want := range map[string]int{
		"terrain": 7, "relief": 7,
		"climate": 1, "basin": 1, "volcanic": 1, "temperature": 1, "moisture": 1,
		// The rim reads no field at all — the profile is a function of the
		// coordinate's distance from the edge of the map — and still costs one,
		// because the unit is evaluations a tile and a zero would read as a
		// layer that draws nothing.
		"rim": 1,
	} {
		l, err := render.LayerNamed(name)
		if err != nil {
			t.Fatalf("LayerNamed(%q): %v", name, err)
		}
		if l.Cost != want {
			t.Errorf("%q costs %d evaluations a tile, want %d", name, l.Cost, want)
		}
	}
}

// TestRimLayerDrawsTheBand is the layer of DESIGN.md 29 that exists so the rim
// can be tuned without hunting for the world's edge in the terrain layer.
//
// A window at a corner of the map has to show all three parts of the profile:
// the forced band at the bottom of the ramp, the shelf climbing across the
// falloff, and the world at the top. A layer that drew the distance itself
// instead would be a gradient over the whole map with the band nowhere in it,
// and it would pass a test that only asked whether the numbers changed.
func TestRimLayerDrawsTheBand(t *testing.T) {
	g := wgva.NewDefault(0x0123456789abcdef)
	rc := g.Config().Rim
	l, err := render.LayerNamed("rim")
	if err != nil {
		t.Fatalf("LayerNamed: %v", err)
	}

	closed, inner := int64(rc.ClosedHexes), int64(rc.ClosedHexes)+int64(rc.FalloffHexes)
	if closed == 0 || inner == closed {
		t.Fatal("the defaults leave no band to draw")
	}
	for _, tc := range []struct {
		distance int64
		want     float64
	}{
		{0, 0},
		{closed - 1, 0},
		{closed, 0},
		{inner, 1},
		{inner + 1000, 1},
		{wgva.WorldRadius, 1},
	} {
		c := wgva.NewCoord(wgva.WorldRadius-tc.distance, 0)
		if got := l.Sample(g, c); got != tc.want {
			t.Errorf("the rim layer %d hexes from the edge is %v, want %v", tc.distance, got, tc.want)
		}
	}

	// And in between it climbs, which is the part a picture shows as a shelf.
	previous := 0.0
	rising := 0
	for d := closed; d <= inner; d++ {
		got := l.Sample(g, wgva.NewCoord(wgva.WorldRadius-d, 0))
		if got < previous {
			t.Fatalf("the rim layer fell from %v to %v, %d hexes from the edge", previous, got, d)
		}
		if got > previous {
			rising++
		}
		previous = got
	}
	if rising < int(inner-closed)/2 {
		t.Errorf("the rim layer rose at %d of %d rings of the falloff, which is not a shelf", rising, inner-closed)
	}

	// The band is drawn, not merely sampled: a grid window on the corner paints
	// more than one color, and one of them is the bottom of the ramp.
	v := mustViewport(t, wgva.NewCoord(wgva.WorldRadius-60, 0), 121, 121, 0, 1)
	img, err := render.RenderGrid(g, v, l, 1)
	if err != nil {
		t.Fatalf("RenderGrid: %v", err)
	}
	colors := map[[4]byte]int{}
	for i := 0; i+3 < len(img.Pix); i += 4 {
		colors[[4]byte(img.Pix[i:i+4])]++
	}
	if len(colors) < 3 {
		t.Errorf("a window on the rim painted %d colors, which is not a band and a shelf", len(colors))
	}
	bottom := l.Key().Ramp.At(0)
	forced := [4]byte{bottom.R, bottom.G, bottom.B, bottom.A}
	if colors[forced] == 0 {
		t.Error("a window on the rim painted none of the forced band")
	}
}
