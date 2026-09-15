// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/store"
	"github.com/mdhender/wgva/view"
)

const testSeed = "0x0123456789abcdef"

// newWorld creates a world and returns its path.
func newWorld(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "world.wgva")
	s, err := store.Create(t.Context(), path, 0x0123456789abcdef, wgva.DefaultConfig())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// decode reads back a written image.
func decode(t *testing.T, path string) *image.RGBA {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		// image/png may hand back another concrete type; convert rather than
		// fail, because what is being compared is pixels.
		out := image.NewRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				out.Set(x, y, img.At(x, y))
			}
		}
		return out
	}
	return rgba
}

// samePixels compares decoded buffers rather than file bytes, because
// image/png's filter and compression choices can change between Go releases and
// what is being asserted is the picture.
func samePixels(a, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

// TestCLIDrawsWhatTheRendererDraws is this command's half of the exit
// condition's last clause: the CLI and both web front ends agree byte for byte.
//
// It is one assertion against render rather than a second set of goldens. The
// golden image in render already pins what the renderer draws, and a second copy
// of it here would only pin it twice; what is worth asserting is that this
// command adds nothing of its own between a window and the pixels. The viewer's
// half of the same claim is in cmd/wgva-serve.
func TestCLIDrawsWhatTheRendererDraws(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "map.png")

	args := []string{
		"--seed", testSeed, "--out", out,
		"--q", "-200", "--r", "-150", "--cols", "41", "--rows", "31",
		"--hex-radius", "8", "--layer", "terrain",
	}
	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The same window, described the way the front ends describe one.
	v, err := view.Parse(view.ViewerDefaults(0x0123456789abcdef), url.Values{
		"q": {"-200"}, "r": {"-150"}, "cols": {"41"}, "rows": {"31"},
		"hex-radius": {"8"}, "layer": {"terrain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	vp, err := v.Viewport()
	if err != nil {
		t.Fatal(err)
	}
	layer, err := v.RenderLayer()
	if err != nil {
		t.Fatal(err)
	}
	want, err := render.Render(wgva.NewDefault(0x0123456789abcdef), vp, layer, v.HexRadius)
	if err != nil {
		t.Fatal(err)
	}

	if !samePixels(decode(t, out), want) {
		t.Error("the CLI's image is not the renderer's image for the same window")
	}
}

// TestWindowGrammarIsTheViewers is why the flags are collected as text and
// handed to view: a flag a caller did not set must keep the default, exactly as
// an absent query parameter does, or a bare `wgva-map` and a bare viewer request
// would draw two different windows.
func TestWindowGrammarIsTheViewers(t *testing.T) {
	o, err := parseFlags([]string{"--cols", "21"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got := o.window; len(got) != 1 || got["cols"] != "21" {
		t.Fatalf("the window flags are %v, want only the one that was set", got)
	}

	v, err := view.Parse(view.ViewerDefaults(0), windowValues(o))
	if err != nil {
		t.Fatal(err)
	}
	defaults := view.ViewerDefaults(0)
	if v.Cols != 21 {
		t.Errorf("cols = %d, want 21", v.Cols)
	}
	if v.Rows != defaults.Rows || v.Layer != defaults.Layer || v.HexRadius != defaults.HexRadius {
		t.Errorf("a flag that was not set moved the window: %+v", v)
	}
}

// TestClampsAreTheViewers is the other half of the same point. An even column
// count is clamped to odd rather than refused, because that is what the window
// grammar does with one — a window needs a center cell.
func TestClampsAreTheViewers(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "map.png")
	if err := run([]string{"--seed", testSeed, "--out", out, "--cols", "400", "--rows", "300", "--hex-radius", "2"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run with even counts: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no image was written: %v", err)
	}
}

// TestNeverCreatesAWorld is the refusal DESIGN.md 29.2 asks for by name: an
// absent or empty file is a refusal naming `wgva-world create`, not an
// invitation.
func TestNeverCreatesAWorld(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.wgva")

	err := run([]string{"--db", absent, "--out", filepath.Join(dir, "map.png")}, &bytes.Buffer{})
	if !errors.Is(err, store.ErrNoWorld) {
		t.Fatalf("run: %v, want ErrNoWorld", err)
	}
	if !strings.Contains(err.Error(), "wgva-world create") {
		t.Errorf("the refusal does not name the command that makes one: %v", err)
	}
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused render created %s", absent)
	}
}

// TestWorldBackedRenderDoesNotWrite is the other promise: `wgva-map --db` opens
// a world and does not modify it.
func TestWorldBackedRenderDoesNotWrite(t *testing.T) {
	path := newWorld(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "map.png")
	if err := run([]string{"--db", path, "--out", out, "--cols", "21", "--rows", "15", "--hex-radius", "4"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("rendering a world wrote to the world file")
	}
}

// TestOverlaysAreComposedFromTheWorld is the one thing genuinely read out of a
// world file and drawn. No tiles are stored, so --db supplies the world's
// identity and the terrain is regenerated; the overlays are the exception.
func TestOverlaysAreComposedFromTheWorld(t *testing.T) {
	path := newWorld(t)
	dir := t.TempDir()

	args := []string{"--db", path, "--cols", "21", "--rows", "15", "--hex-radius", "6", "--layer", "terrain"}

	plain := filepath.Join(dir, "plain.png")
	if err := run(append(args, "--out", plain), &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	// With no overlays at all, a world-backed render is the renderer's render.
	// That is render.RenderPlayer's own guarantee and it is asserted there; what
	// matters here is that this command does not reach for a different one.
	v, err := view.Parse(view.ViewerDefaults(0x0123456789abcdef), url.Values{
		"cols": {"21"}, "rows": {"15"}, "hex-radius": {"6"}, "layer": {"terrain"},
	})
	if err != nil {
		t.Fatal(err)
	}
	vp, _ := v.Viewport()
	layer, _ := v.RenderLayer()
	want, err := render.Render(wgva.NewDefault(0x0123456789abcdef), vp, layer, v.HexRadius)
	if err != nil {
		t.Fatal(err)
	}
	if !samePixels(decode(t, plain), want) {
		t.Error("a world with no overlays does not draw what the renderer draws")
	}

	// One settlement and one discovery, and the picture must change: fog hides
	// terrain and a mark is drawn on top of it.
	s, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDiscovered(t.Context(), wgva.Origin); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettlement(t.Context(), wgva.Origin, "Landing"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	marked := filepath.Join(dir, "marked.png")
	if err := run(append(args, "--out", marked), &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if samePixels(decode(t, marked), want) {
		t.Error("the overlays in the world file were not drawn")
	}
}

// TestDiagnosticOutputSaysSo is DESIGN.md 29.2: --config and the bare form draw
// no saved world, and the output should say so where it can. An acceptance sheet
// that does not say what it came from is a picture.
func TestDiagnosticOutputSaysSo(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"--seed", testSeed, "--out", filepath.Join(dir, "map.png"), "--cols", "11", "--rows", "9", "--hex-radius", "2"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "diagnostic") || !strings.Contains(text, "wgva-world create") {
		t.Errorf("a diagnostic render does not say so:\n%s", text)
	}
	if !strings.Contains(text, config.DefaultDigest().String()) {
		t.Errorf("the output does not name the configuration it drew under:\n%s", text)
	}

	// A world-backed render names the world instead.
	out.Reset()
	if err := run([]string{"--db", newWorld(t), "--out", filepath.Join(dir, "world.png"), "--cols", "11", "--rows", "9", "--hex-radius", "2"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(out.String(), "diagnostic") {
		t.Errorf("a world-backed render calls itself diagnostic:\n%s", out.String())
	}
}

// TestConfigFileIsDrawnWithoutADatabase is step 2 of the administrator's path:
// confirm the file reproduces what the tab showed, with no database anywhere.
func TestConfigFileIsDrawnWithoutADatabase(t *testing.T) {
	dir := t.TempDir()

	cfg := wgva.DefaultConfig()
	cfg.SeaLevel += 0.05
	data, err := config.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	args := []string{"--config", file, "--seed", testSeed, "--cols", "21", "--rows", "15", "--hex-radius", "4", "--layer", "elevation"}
	if err := run(append(args, "--out", filepath.Join(dir, "a.png")), &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	want, err := config.Of(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), want.String()) {
		t.Errorf("the output does not name the file's fingerprint:\n%s", out.String())
	}

	// And the picture is the file's, not the defaults'.
	if err := run([]string{"--seed", testSeed, "--cols", "21", "--rows", "15", "--hex-radius", "4", "--layer", "elevation", "--out", filepath.Join(dir, "b.png")}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if samePixels(decode(t, filepath.Join(dir, "a.png")), decode(t, filepath.Join(dir, "b.png"))) {
		t.Error("--config drew the defaults")
	}
}

// TestTwoSourcesAreRefused: --db and --config are two sources for one
// configuration, and silently preferring one would be a picture nobody can
// account for.
func TestTwoSourcesAreRefused(t *testing.T) {
	_, err := parseFlags([]string{"--db", "w.wgva", "--config", "c.toml"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "two sources") {
		t.Fatalf("parseFlags: %v", err)
	}
}
