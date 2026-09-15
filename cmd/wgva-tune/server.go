// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/view"
)

// server holds the one thing this tool has that the map viewer does not: state.
//
// The thing it exists to change is the complete effective configuration, on the
// order of a hundred numeric fields, and a hundred fields do not fit in an
// address bar. So the configuration lives in this process's memory, a form
// changes it, and a POST is how. Everything about the *view* is still in the
// URL — tabs are routes, the turn and the scale are parameters, and the controls
// are links and GET forms, so the address bar keeps up and the back button still
// works.
//
// What that gives up is real and every page says so: a link to a window shows
// what this process is drawing now, not what it drew when the link was copied.
// What it keeps is the fingerprint. See DESIGN.md 29.1.
type server struct {
	defaultSeed wgva.Seed

	// mu guards cfg. It is a mutex on a *tool*, not on a Generator: a Generator
	// is immutable and is built fresh from a snapshot of cfg per request, so
	// nothing shared is ever read while it is being written.
	mu  sync.RWMutex
	cfg wgva.Config

	// renders bounds concurrent renders. net/http starts a goroutine per
	// request and will happily start ten thousand; the work here is CPU-bound,
	// so unbounded concurrency turns one careless reload into a stalled machine.
	renders chan struct{}
}

func newServer(seed wgva.Seed, cfg wgva.Config) *server {
	return &server{
		defaultSeed: seed,
		cfg:         cfg,
		renders:     make(chan struct{}, runtime.GOMAXPROCS(0)),
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /seed/{seed}", s.handleMap)
	mux.HandleFunc("GET /seed/{seed}/map.png", s.handleMapPNG)
	mux.HandleFunc("GET /seed/{seed}/grid", s.handleGrid)
	mux.HandleFunc("GET /seed/{seed}/grid.png", s.handleGridPNG)
	mux.HandleFunc("GET /seed/{seed}/config", s.handleConfig)
	mux.HandleFunc("POST /seed/{seed}/config/fields", s.handleConfigFields)
	mux.HandleFunc("POST /seed/{seed}/config/upload", s.handleConfigUpload)
	mux.HandleFunc("POST /seed/{seed}/config/reset", s.handleConfigReset)
	mux.HandleFunc("GET /config.toml", s.handleDownload)

	return mux
}

// config returns a snapshot of the configuration. A Generator is built from the
// snapshot, so a form POST arriving mid-render cannot change what that render is
// drawing.
func (s *server) config() wgva.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// setConfig replaces the configuration if the replacement is usable. An invalid
// configuration is refused whole rather than applied field by field, so the tool
// is never drawing something that could not be written to a file.
func (s *server) setConfig(cfg wgva.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	return nil
}

// request is everything a handler needs that came off the wire.
type request struct {
	seed wgva.Seed
	view view.View
	cfg  wgva.Config
	gen  *wgva.Generator
}

// parse reads the seed from the path and the window from the query, and builds
// the generator for them.
//
// Every refusal here is a 400 with a readable message, never a 500. Nothing a
// caller can type is the process's fault.
func (s *server) parse(w http.ResponseWriter, r *http.Request) (request, bool) {
	seed, err := view.ParseSeed(r.PathValue("seed"))
	if err != nil {
		refuse(w, err)
		return request{}, false
	}

	v, err := view.Parse(view.Defaults(seed), r.URL.Query())
	if err != nil {
		refuse(w, err)
		return request{}, false
	}

	cfg := s.config()
	gen, err := wgva.New(seed, cfg)
	if err != nil {
		// The configuration is validated on the way in, so this is unreachable
		// unless the two validations have been changed apart.
		http.Error(w, fmt.Sprintf("the configuration in this process is not usable: %v", err), http.StatusInternalServerError)
		return request{}, false
	}

	return request{seed: seed, view: v, cfg: cfg, gen: gen}, true
}

func refuse(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusBadRequest)
}

// withRender runs one render under the concurrency bound.
func (s *server) withRender(fn func() (*image.RGBA, error)) (*image.RGBA, error) {
	s.renders <- struct{}{}
	defer func() { <-s.renders }()
	return fn()
}

// renderCost is what one render actually cost.
//
// It is the line DESIGN.md 31.1 asks every front end to log, and it is what
// stands where an evaluation budget used to: nothing refuses a window for being
// expensive, so what an administrator gets instead is the measurement. Generate
// and encode are separated because they move for different reasons — generate
// when the octave ladders or the core count move, encode when the image size or
// the PNG settings do — and an "it got slower" report that does not separate the
// two is not yet a measurement.
type renderCost struct {
	tiles       int
	evaluations int
	generate    time.Duration
	encode      time.Duration
}

// logCost prints one render's cost line to the console.
func logCost(what string, c renderCost) {
	perSecond := 0.0
	if c.generate > 0 {
		perSecond = float64(c.tiles) / c.generate.Seconds()
	}
	log.Printf("%s (%d tiles, %d evaluations, %d ms generate, %d ms encode, %.0f tiles/s)",
		what, c.tiles, c.evaluations,
		c.generate.Milliseconds(), c.encode.Milliseconds(), perSecond)
}

// writePNG encodes and writes an image, with a strong entity tag.
//
// The tag stops being a pure function of the URL, which is the departure, but
// the configuration's fingerprint is in it: move a field and every tag moves
// with it, so a browser holding an old image asks again. Eight bytes rather than
// the four a page prints, because a tuning session walks through hundreds of
// configurations under otherwise identical URLs.
//
// The cost line is logged here rather than in the handler because the encode is
// here, and a render cost that left the encode out would be the half of the
// figure that does not move when the generator does.
func (s *server) writePNG(w http.ResponseWriter, r *http.Request, img *image.RGBA, cfg wgva.Config, cost renderCost) {
	d, err := config.Of(cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	etag := fmt.Sprintf("%q", d.Tag()+"-"+r.URL.RequestURI())
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	start := time.Now()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cost.encode = time.Since(start)
	logCost(r.URL.Path, cost)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// renderRefusal turns a render refusal into a 400 and anything else into a 500.
func renderRefusal(w http.ResponseWriter, err error) {
	var re *render.RenderError
	if errors.As(err, &re) {
		refuse(w, err)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// configurationLabel says which side of this binary's defaults a session is on.
//
// Every tab prints it, conspicuously, beside the identity pair. Neither half of
// that pair is redundant: the fingerprint cannot see a generator fix nobody
// versioned, and the build identity cannot see a setting nudged in this process.
// See DESIGN.md 29.5.
func configurationLabel(cfg wgva.Config) string {
	if config.IsDefault(cfg) {
		return "this binary's defaults"
	}
	return "modified"
}
