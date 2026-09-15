// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/store"
	"github.com/mdhender/wgva/view"
)

// source is where a window's world comes from, and there are three.
//
// Two of them are diagnostic and one is a saved world, and the difference is
// worth carrying in a type rather than in a pair of nil checks: only the world
// has overlays to compose, and only the world is a thing somebody can point at
// later and ask what it was. The provenance lines say which one drew an image,
// because an acceptance sheet that does not say whether it came from a world is
// a picture.
type source struct {
	seed  wgva.Seed
	gen   *wgva.Generator
	cfg   wgva.Config
	world *store.Store // nil unless --db
}

// openSource resolves the three ways a configuration and a seed reach this
// command.
func openSource(o options) (*source, error) {
	switch {
	case o.db != "":
		// With --db the database supplies the seed, the algorithm version, the
		// world radius, and the complete effective configuration. --seed is
		// ignored rather than compared, because there is nothing here for it to
		// disambiguate: one file holds one world, and the file was named.
		s, err := store.Open(context.Background(), o.db)
		if errors.Is(err, store.ErrNoWorld) {
			return nil, fmt.Errorf("%w\nonly `wgva-world create` makes a world file; this command opens one", err)
		}
		if err != nil {
			return nil, err
		}
		w := s.World()
		gen, err := w.Generator()
		if err != nil {
			s.Close()
			return nil, err
		}
		return &source{seed: w.Seed, gen: gen, cfg: w.Config, world: s}, nil

	case o.file != "":
		data, err := os.ReadFile(o.file)
		if err != nil {
			return nil, fmt.Errorf("--config: %w", err)
		}
		cfg, err := config.Unmarshal(data)
		if err != nil {
			return nil, fmt.Errorf("--config: %s: %w", o.file, err)
		}
		return newSource(o.seed, cfg)

	default:
		return newSource(o.seed, wgva.DefaultConfig())
	}
}

func newSource(seedText string, cfg wgva.Config) (*source, error) {
	seed, err := wgva.ParseSeed(seedText)
	if err != nil {
		return nil, fmt.Errorf("--seed: %w", err)
	}
	gen, err := wgva.New(seed, cfg)
	if err != nil {
		return nil, err
	}
	return &source{seed: seed, gen: gen, cfg: cfg}, nil
}

func (s *source) close() {
	if s.world != nil {
		s.world.Close()
	}
}

// draw renders the window.
//
// A world draws through RenderPlayer with its overlays composed, and everything
// else through Render. With no overlays the two produce identical pixels, which
// a test in render asserts — so this is not two renderers, it is one renderer
// asked whether anybody has been here.
func (s *source) draw(vp render.Viewport, l render.Layer, v view.View, grid bool) (*image.RGBA, error) {
	// The pixel scale comes from the view rather than the viewport, and that is
	// the separation DESIGN.md 29 asks for: a Viewport describes cells, renderer
	// pixel coordinates never feed back into generation, and the pixel scale is
	// therefore not part of a window's identity.
	if grid {
		// The grid view draws one pixel per cell and has no hexes to put a mark
		// on. Overlays are not composed there; at a stride of sixty-four a
		// settlement marker would be a claim about sixty-four hexes.
		return render.RenderGrid(s.gen, vp, l, v.Scale)
	}
	if s.world == nil {
		return render.Render(s.gen, vp, l, v.HexRadius)
	}

	overlays, err := s.overlays(vp)
	if err != nil {
		return nil, err
	}
	return render.RenderPlayer(s.gen, vp, l, v.HexRadius, overlays)
}

// overlays loads every player mark that might fall inside the window.
//
// One ordered range scan over the smallest (q, r) box holding the window's
// tiles. For a window that crosses the wrap seam the box is a superset of what
// is drawn, which is correct for the same reason it is cheap: an overlay outside
// the window is never drawn. DESIGN.md 29.4.
func (s *source) overlays(v render.Viewport) (render.Overlays, error) {
	minQ, maxQ, minR, maxR := v.CoordBounds()
	loaded, err := s.world.Overlays(context.Background(), store.Box{
		MinQ: minQ, MaxQ: maxQ, MinR: minR, MaxR: maxR,
	})
	if err != nil {
		return render.Overlays{}, err
	}
	return render.Overlays{
		Discovered:  loaded.Discovered,
		Settlements: markersOf(loaded.Settlements),
		Labels:      markersOf(loaded.Labels),
	}, nil
}

// markersOf converts the store's markers to the renderer's.
//
// The two types are the same three fields and are deliberately not shared:
// render does not import store and store does not import render, because a
// renderer that could open a database would be a renderer that could be handed a
// world rather than a viewport. The command does the loading, which means the
// command does this. DESIGN.md 29.4.
func markersOf(in []store.Marker) []render.Marker {
	if len(in) == 0 {
		return nil
	}
	out := make([]render.Marker, len(in))
	for i, m := range in {
		out[i] = render.Marker{Coord: m.Coord, Name: m.Name}
	}
	return out
}

// provenance is what an image came from, in lines a report can paste.
//
// Two of the three sources are diagnostic and say so. An acceptance sheet that
// does not say whether it came from a saved world is a picture, and the question
// it raises six months later — which configuration was that? — is exactly the
// one the fingerprint exists to answer.
func (s *source) provenance() []string {
	digest, err := config.Of(s.cfg)
	if err != nil {
		return []string{fmt.Sprintf("the configuration cannot be fingerprinted: %v", err)}
	}
	lines := []string{
		fmt.Sprintf("seed %s, algorithm version %d, render version %d, world radius %d",
			s.seed, wgva.AlgorithmVersion, render.RenderVersion, wgva.WorldRadius),
		fmt.Sprintf("configuration %s", digest),
	}
	if s.world == nil {
		lines = append(lines,
			"diagnostic: this does not represent a saved world; `wgva-world create` makes one")
		return lines
	}
	w := s.world.World()
	lines = append(lines, fmt.Sprintf("world %s, created by %s", s.world.Path(), w.CreatedBuild))
	return lines
}

// writePNG encodes to a temporary file in the target's directory and renames it,
// so an interrupted render leaves the previous image rather than half of a new
// one.
func writePNG(path string, img *image.RGBA) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".wgva-map-*.png")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := png.Encode(tmp, img); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp makes a private file, which is right for a temporary one and
	// wrong for the image somebody asked for. An acceptance sheet lands in a
	// repository and a bug report gets attached to an issue.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
