// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"runtime"
	"time"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/store"
	"github.com/mdhender/wgva/view"
)

// PageVersion is the revision of the HTML this server emits.
//
// It exists because the page is a second representation and needs a validator of
// its own. AlgorithmVersion describes the world and RenderVersion describes the
// pixels, and neither says anything about the markup — so a release that changed
// the page alone would emit the same strong tag for different bytes and a
// browser would go on showing the old page, which is the one thing a strong
// validator exists to prevent.
//
// It appears in the page tag and deliberately **not** in the image tag, so
// changing the markup does not invalidate a cached PNG. See DESIGN.md 29.3.
//
// Version history:
//
//	1  Initial viewer page: the window, the compass, the layer list, and the
//	   readout naming the tile at the centre.
const PageVersion uint32 = 1

// server is the viewer. It holds no state a URL does not describe.
//
// The one thing in here that is not derived from the request is the world, and
// that is fixed for the process's lifetime: it is opened before the port is
// bound, so a database that fails an opening gate is a server that does not
// start rather than a server that answers every request with a 500.
type server struct {
	// seed is the world's seed. Without a world it is the seed the process was
	// started with and every seed is servable; with one it is the world's, and
	// the seed in a route is a *check* against it rather than the source of it.
	seed wgva.Seed

	gen    *wgva.Generator
	cfg    wgva.Config
	digest config.Digest

	// world is nil in the diagnostic mode.
	world *store.Store

	// renders bounds concurrent renders. net/http starts a goroutine per
	// request and will happily start ten thousand; the work here is CPU-bound,
	// so unbounded concurrency turns one careless reload into a stalled machine.
	renders chan struct{}
}

func newDiagnosticServer(seed wgva.Seed, cfg wgva.Config) (*server, error) {
	gen, err := wgva.New(seed, cfg)
	if err != nil {
		return nil, err
	}
	digest, err := config.Of(cfg)
	if err != nil {
		return nil, err
	}
	return &server{
		seed:    seed,
		gen:     gen,
		cfg:     cfg,
		digest:  digest,
		renders: make(chan struct{}, runtime.GOMAXPROCS(0)),
	}, nil
}

func newWorldServer(s *store.Store) (*server, error) {
	w := s.World()
	gen, err := w.Generator()
	if err != nil {
		return nil, err
	}
	return &server{
		seed:    w.Seed,
		gen:     gen,
		cfg:     w.Config,
		digest:  w.Fingerprint,
		world:   s,
		renders: make(chan struct{}, runtime.GOMAXPROCS(0)),
	}, nil
}

func (s *server) close() {
	if s.world != nil {
		s.world.Close()
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /seed/{seed}", s.handlePage)
	mux.HandleFunc("GET /seed/{seed}/map.png", s.handlePNG)
	return mux
}

// handleRoot sends a visitor to the world this server holds. There is nothing to
// choose at the root: one process serves one seed.
func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/seed/"+s.seed.String(), http.StatusFound)
}

// request is everything a handler needs that came off the wire.
type request struct {
	seed wgva.Seed
	view view.View
}

// parse reads the seed from the path and the window from the query.
//
// The seed in the route is a check against the one this server holds, not the
// source of it. With a world that is the whole point — the database supplies the
// seed, and another seed is a 404 naming the one this server has, because a link
// to a world that is not this one is a broken link rather than a different
// world. Without a world the two are the same value by construction, since the
// process was started with it.
func (s *server) parse(w http.ResponseWriter, r *http.Request) (request, bool) {
	seed, err := wgva.ParseSeed(r.PathValue("seed"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request{}, false
	}
	if seed != s.seed {
		http.Error(w, fmt.Sprintf("this server holds seed %s, not %s", s.seed, seed), http.StatusNotFound)
		return request{}, false
	}

	v, err := view.Parse(view.ViewerDefaults(seed), r.URL.Query())
	if err != nil {
		// Every refusal here is a 400 with a readable message, never a 500.
		// Nothing a caller can type is the process's fault.
		http.Error(w, err.Error(), http.StatusBadRequest)
		return request{}, false
	}
	return request{seed: seed, view: v}, true
}

// handlePNG renders the window itself.
func (s *server) handlePNG(w http.ResponseWriter, r *http.Request) {
	req, ok := s.parse(w, r)
	if !ok {
		return
	}

	viewport, err := req.view.Viewport()
	if err != nil {
		refuseRender(w, err)
		return
	}
	layer, err := req.view.RenderLayer()
	if err != nil {
		refuseRender(w, err)
		return
	}

	// The tag goes out before the work is done, so a browser holding the image
	// gets a 304 rather than a re-render.
	//
	// **A world-backed image carries no tag at all.** It depends on the overlays
	// as well, and overlays are mutable player state with no version anywhere in
	// the system; a tag that ignored them would go on serving an unexplored map
	// after the player explored it.
	if s.world == nil {
		etag := s.imageTag(req)
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
	}

	start := time.Now()
	img, err := s.withRender(func() (*image.RGBA, error) { return s.draw(r.Context(), viewport, layer, req.view) })
	if err != nil {
		refuseRender(w, err)
		return
	}
	generate := time.Since(start)

	start = time.Now()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	encode := time.Since(start)

	logCost(r.URL.RequestURI(), viewport.Tiles(), viewport.Evaluations(layer), generate, encode)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// draw renders the window, composing the world's overlays when there is a world.
//
// Overlays are read fresh from the database on every request, so exploring a
// world and refreshing shows the exploration. With no overlays at all this draws
// exactly what Render draws, which render asserts — so this is not two
// renderers, it is one renderer asked whether anybody has been here.
func (s *server) draw(ctx context.Context, v render.Viewport, l render.Layer, window view.View) (*image.RGBA, error) {
	if s.world == nil {
		return render.Render(s.gen, v, l, window.HexRadius)
	}

	minQ, maxQ, minR, maxR := v.CoordBounds()
	loaded, err := s.world.Overlays(ctx, store.Box{MinQ: minQ, MaxQ: maxQ, MinR: minR, MaxR: maxR})
	if err != nil {
		return nil, err
	}
	return render.RenderPlayer(s.gen, v, l, window.HexRadius, render.Overlays{
		Discovered:  loaded.Discovered,
		Settlements: markersOf(loaded.Settlements),
		Labels:      markersOf(loaded.Labels),
	})
}

// markersOf converts the store's markers to the renderer's. The two packages are
// siblings and neither imports the other, so the command converts. DESIGN.md
// 29.4.
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

// imageTag is the strong validator for a diagnostic image.
//
// The PNG is a pure function of the seed, the window, the layer, the algorithm
// version, the render version, and the configuration fingerprint, so the tag is
// built from exactly those and from nothing else. PageVersion is deliberately
// absent: changing the markup must not invalidate a cached image.
func (s *server) imageTag(req request) string {
	return fmt.Sprintf("%q", fmt.Sprintf("a%d-r%d-c%s-%s-%s",
		wgva.AlgorithmVersion, render.RenderVersion, s.digest.Tag(),
		req.seed, req.view.Query()))
}

// pageTag is the strong validator for the HTML.
//
// It carries PageVersion, which the image tag does not, because the page is a
// second representation of the same state and a release that changed only the
// markup would otherwise emit the same tag for different bytes.
func (s *server) pageTag(req request) string {
	return fmt.Sprintf("%q", fmt.Sprintf("p%d-a%d-r%d-c%s-%s-%s",
		PageVersion, wgva.AlgorithmVersion, render.RenderVersion, s.digest.Tag(),
		req.seed, req.view.Query()))
}

// withRender runs one render under the concurrency bound.
func (s *server) withRender(fn func() (*image.RGBA, error)) (*image.RGBA, error) {
	s.renders <- struct{}{}
	defer func() { <-s.renders }()
	return fn()
}

// refuseRender turns a render refusal into a 400 and anything else into a 500.
func refuseRender(w http.ResponseWriter, err error) {
	var re *render.RenderError
	if errors.As(err, &re) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// logCost prints one render's cost line, which is what DESIGN.md 31.1 asks every
// front end for: generate and encode separated, because they move for different
// reasons and an "it got slower" report that does not separate them is not yet a
// measurement.
func logCost(what string, tiles, evaluations int, generate, encode time.Duration) {
	perSecond := 0.0
	if generate > 0 {
		perSecond = float64(tiles) / generate.Seconds()
	}
	log.Printf("%s (%d tiles, %d evaluations, %d ms generate, %d ms encode, %.0f tiles/s)",
		what, tiles, evaluations, generate.Milliseconds(), encode.Milliseconds(), perSecond)
}

// configurationLabel says whether what is being served is this binary's
// defaults. A world created under a configuration nobody shipped is not wrong,
// and it is worth saying out loud.
func (s *server) configurationLabel() string {
	if s.digest == config.DefaultDigest() {
		return "this binary's defaults"
	}
	return "not this binary's defaults"
}
