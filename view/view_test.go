// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package view_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/view"
)

func parse(t *testing.T, q url.Values) view.View {
	t.Helper()
	v, err := view.Parse(view.Defaults(1), q)
	if err != nil {
		t.Fatalf("Parse(%v): %v", q, err)
	}
	return v
}

// TestDefaultsAreUsable is the cheapest assertion that matters: the window a tab
// opens on has to build.
func TestDefaultsAreUsable(t *testing.T) {
	d := view.Defaults(7)
	if _, err := d.Viewport(); err != nil {
		t.Fatalf("the default view does not make a viewport: %v", err)
	}
	if _, err := d.GridViewport(); err != nil {
		t.Fatalf("the default view does not make a grid viewport: %v", err)
	}
	if _, err := d.RenderLayer(); err != nil {
		t.Fatalf("the default layer does not exist: %v", err)
	}
}

// TestAbsentParametersAreKept is what makes every control a link that changes
// one thing.
func TestAbsentParametersAreKept(t *testing.T) {
	base := view.Defaults(1)
	base.Q, base.R, base.Turn, base.Cols = 40, -70, 3, 21

	got := parse(t, url.Values{"layer": {"local"}})
	if got.Layer != "local" {
		t.Fatalf("the layer is %q, want local", got.Layer)
	}

	got, err := view.Parse(base, url.Values{"layer": {"local"}})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Q != 40 || got.R != -70 || got.Turn != 3 || got.Cols != 21 {
		t.Errorf("changing the layer moved something else: %+v", got)
	}
}

// TestCountsAreClampedToOdd pins the rule of DESIGN.md 29.1. A clamped request
// still has a center cell, which is what every control in the tool is expressed
// relative to.
func TestCountsAreClampedToOdd(t *testing.T) {
	cases := map[string][2]int{
		"40":     {40, 41},
		"41":     {41, 41},
		"0":      {0, view.MinCols},
		"-5":     {-5, view.MinCols},
		"999999": {999999, view.MaxCols},
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			v := parse(t, url.Values{"cols": {text}, "rows": {text}})
			if v.Cols != want[1] || v.Rows != want[1] {
				t.Fatalf("%d clamped to %d columns and %d rows, want %d", want[0], v.Cols, v.Rows, want[1])
			}
			if v.Cols%2 == 0 || v.Rows%2 == 0 {
				t.Fatalf("a clamped window is %dx%d, which has no center cell", v.Cols, v.Rows)
			}
			if _, err := v.Viewport(); err != nil {
				t.Fatalf("a clamped window was refused by the renderer: %v", err)
			}
		})
	}
}

// TestClampingIsNeverRefusal asserts that no integer a person can type makes the
// tool refuse a window size. Sizes are clamped; only things that are not numbers
// and coordinates off the map are refused.
func TestClampingIsNeverRefusal(t *testing.T) {
	for _, text := range []string{"-2147483648", "0", "1", "2", "1000000"} {
		for _, param := range []string{"cols", "rows", "hex-radius", "scale", "stride"} {
			if _, err := view.Parse(view.Defaults(1), url.Values{param: {text}}); err != nil {
				t.Errorf("%s=%s was refused: %v", param, text, err)
			}
		}
	}
}

// TestTurnIsModular covers the one parameter that is reduced rather than
// clamped: six sixths of a turn is the identity, so turn=7 and turn=-1 mean what
// they look like they mean.
func TestTurnIsModular(t *testing.T) {
	cases := map[string]int{"0": 0, "5": 5, "6": 0, "7": 1, "-1": 5, "-7": 5, "13": 1}
	for text, want := range cases {
		if got := parse(t, url.Values{"turn": {text}}).Turn; got != want {
			t.Errorf("turn=%s became %d, want %d", text, got, want)
		}
	}
}

// TestRefusals covers what a 400 is for. Nothing a caller can type is the
// process's fault, so each of these is an errors.Is-able refusal naming the
// parameter.
func TestRefusals(t *testing.T) {
	cases := []struct {
		name  string
		query url.Values
		param string
		want  error
	}{
		{"q is a word", url.Values{"q": {"origin"}}, "q", view.ErrNotANumber},
		{"r is a float", url.Values{"r": {"1.5"}}, "r", view.ErrNotANumber},
		{"turn is a word", url.Values{"turn": {"north"}}, "turn", view.ErrNotANumber},
		{"cols is a word", url.Values{"cols": {"wide"}}, "cols", view.ErrNotANumber},
		{"q is off the map", url.Values{"q": {"32768"}}, "q", view.ErrOutOfRange},
		{"r is off the map", url.Values{"r": {"-40000"}}, "r", view.ErrOutOfRange},
		{"s disagrees", url.Values{"q": {"3"}, "r": {"4"}, "s": {"0"}}, "s", view.ErrOutOfRange},
		{"layer does not exist", url.Values{"layer": {"rim"}}, "layer", render.ErrUnknownLayer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := view.Parse(view.Defaults(1), tc.query)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Parse returned %v, want an error wrapping %v", err, tc.want)
			}
			ve, ok := errors.AsType[*view.ViewError](err)
			if !ok {
				t.Fatalf("Parse returned %T, want a *view.ViewError", err)
			}
			if ve.Param != tc.param {
				t.Fatalf("the refusal names %q, want %q", ve.Param, tc.param)
			}
		})
	}
}

// TestConsistentTripleIsAccepted is the other half of the s check: a coordinate
// pasted whole must work.
func TestConsistentTripleIsAccepted(t *testing.T) {
	v := parse(t, url.Values{"q": {"3"}, "r": {"4"}, "s": {"-7"}})
	if v.Q != 3 || v.R != 4 {
		t.Fatalf("the view is at (%d, %d), want (3, 4)", v.Q, v.R)
	}
}

// TestValuesRoundTrip asserts that a link the tool writes parses back to the
// window it was written from. That is what makes the address bar keep up and the
// back button still work.
func TestValuesRoundTrip(t *testing.T) {
	base := view.Defaults(9)
	base.Q, base.R = -1234, 5678
	base.Cols, base.Rows, base.Turn = 77, 55, 4
	base.Layer, base.HexRadius, base.Scale, base.Stride = "detail", 7, 4, 16

	back, err := view.Parse(view.Defaults(9), base.Values())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if back != base {
		t.Errorf("the round trip changed the view:\ngot  %+v\nwant %+v", back, base)
	}
}

// TestScrolledUsesTheRenderersCompass asserts that a scroll control does not
// have a second opinion about which way is up. A control that computed its own
// direction index would be wrong in exactly the way a mirrored layout is wrong:
// every picture would still look right.
func TestScrolledUsesTheRenderersCompass(t *testing.T) {
	base := view.Defaults(1)
	for _, p := range render.AdminCompass() {
		got := base.Scrolled(p.Name, 3)
		want := wgva.NewCoord(base.Q+p.DQ*3, base.R+p.DR*3)
		if got.Q != int64(want.Q()) || got.R != int64(want.R()) {
			t.Errorf("scrolling %s three hexes reached (%d, %d), want %v", p.Name, got.Q, got.R, want)
		}
	}
	if got := base.Scrolled("up", 1); got != base {
		t.Error("scrolling toward a point that is not on the compass moved the view")
	}
}

// TestScrolledWraps asserts a scroll off the edge of the map arrives somewhere
// real, because the world's topology says it should.
func TestScrolledWraps(t *testing.T) {
	v := view.Defaults(1)
	v.Q, v.R = wgva.WorldRadius, 0
	for _, p := range render.AdminCompass() {
		got := v.Scrolled(p.Name, 5)
		if c := wgva.NewCoord(got.Q, got.R); c.RimDistance() < 0 {
			t.Errorf("scrolling %s off the edge reached %v, which is outside the map", p.Name, c)
		}
	}
}

// TestZoomedStaysInRange covers the zoom controls.
func TestZoomedStaysInRange(t *testing.T) {
	v := view.Defaults(1)
	for range 20 {
		v = v.Zoomed(2)
	}
	if v.HexRadius != view.MaxHexRadius {
		t.Errorf("zooming in repeatedly reached %d, want the maximum %d", v.HexRadius, view.MaxHexRadius)
	}
	for range 20 {
		v = v.Zoomed(0.5)
	}
	if v.HexRadius != view.MinHexRadius {
		t.Errorf("zooming out repeatedly reached %d, want the minimum %d", v.HexRadius, view.MinHexRadius)
	}
}

// TestSeedRoundTrip covers the one number people copy between a browser, a
// terminal, and a commit message.
func TestSeedRoundTrip(t *testing.T) {
	for _, s := range []wgva.Seed{0, 1, 42, 0x0123456789abcdef, ^wgva.Seed(0)} {
		text := view.FormatSeed(s)
		back, err := view.ParseSeed(text)
		if err != nil {
			t.Fatalf("ParseSeed(%q): %v", text, err)
		}
		if back != s {
			t.Errorf("%q parsed back to %d, want %d", text, back, s)
		}
	}
	if got, err := view.ParseSeed("42"); err != nil || got != 42 {
		t.Errorf("ParseSeed(\"42\") = %d, %v", got, err)
	}
	if _, err := view.ParseSeed("not a seed"); !errors.Is(err, view.ErrNotANumber) {
		t.Errorf("ParseSeed of a word returned %v, want ErrNotANumber", err)
	}
}
