// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"embed"
	"fmt"
	"html/template"
	"image"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/config"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/view"
)

//go:embed templates/*.gohtml
var templateFS embed.FS

var templateFuncs = template.FuncMap{
	"percent": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a * 100 / b
	},
	// dict passes several values to one sub-template, which is how the four
	// readout ladders share a definition instead of being written out four
	// times. It panics on an odd argument list, which is a template error and
	// is caught the first time the page is rendered — the tool's own tests
	// render every tab.
	"dict": func(pairs ...any) map[string]any {
		if len(pairs)%2 != 0 {
			panic("wgva-tune: dict wants key/value pairs")
		}
		out := make(map[string]any, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			key, ok := pairs[i].(string)
			if !ok {
				panic(fmt.Sprintf("wgva-tune: dict key %v is not a string", pairs[i]))
			}
			out[key] = pairs[i+1]
		}
		return out
	},
}

// Each tab is parsed into its own template set rather than all of them into one.
//
// Every page file defines "body", which the shared layout fills in, and Go's
// template package lets a later definition silently replace an earlier one — so
// parsing all three into one set leaves every tab rendering whichever file was
// parsed last, with a page that still looks broadly right because the header and
// the controls are shared. One set per page is what makes the name mean what it
// says.
var (
	mapTemplate    = mustParsePage("map.gohtml")
	gridTemplate   = mustParsePage("grid.gohtml")
	configTemplate = mustParsePage("config.gohtml")
)

func mustParsePage(name string) *template.Template {
	return template.Must(template.New(name).Funcs(templateFuncs).ParseFS(
		templateFS,
		"templates/layout.gohtml",
		"templates/controls.gohtml",
		"templates/"+name,
	))
}

// link is one control: a label and the URL that applies it. Every control in
// this tool is a link or a GET form, which is what keeps the address bar in step
// with what is on screen.
type link struct {
	Label   string
	URL     string
	Title   string
	Current bool
}

// page is what every template is rendered with.
type page struct {
	Tab      string
	Seed     wgva.Seed
	SeedText string
	View     view.View

	Build            string
	AlgorithmVersion uint32
	WorldRadius      int64
	Fingerprint      string
	FingerprintShort string
	Label            string
	IsDefault        bool
	CreateLine       string

	// Distribution is what is in the window the map tab is drawing, and
	// ReadoutCost is what counting it cost. The grid tab leaves both empty: a
	// million tiles of readout is seven million evaluations for a second copy
	// of work the image already did. See DESIGN.md 29.1.
	Distribution render.Distribution
	ReadoutCost  int

	Layers      []link
	Turns       []link
	Scroll      []link
	Zoom        []link
	Size        []link
	Strides     []link
	Tabs        []link
	LayerDoc    string
	Evaluations int
	Tiles       int
	ImageURL    string
	FieldTree   string
	Warning     string

	Groups  []configGroup
	Notice  string
	Failure string
}

type configGroup struct {
	Name   string
	Fields []configField
}

type configField struct {
	Key     string
	Doc     string
	Value   string
	Choices []string
	Default string
	Moved   bool
}

// newPage assembles everything a tab prints that is not the picture.
func (s *server) newPage(tab string, req request) page {
	d, err := config.Of(req.cfg)
	if err != nil {
		// Unreachable: setConfig refuses a configuration that does not validate.
		panic(fmt.Sprintf("wgva-tune: the configuration in this process cannot be fingerprinted: %v", err))
	}

	p := page{
		Tab:              tab,
		Seed:             req.seed,
		SeedText:         view.FormatSeed(req.seed),
		View:             req.view,
		Build:            wgva.Version().String(),
		AlgorithmVersion: wgva.AlgorithmVersion,
		WorldRadius:      wgva.WorldRadius,
		Fingerprint:      d.String(),
		FingerprintShort: d.Short(),
		Label:            configurationLabel(req.cfg),
		IsDefault:        config.IsDefault(req.cfg),
	}

	// The identity pair wgva-world create takes. Nobody types it; the tool emits
	// the whole line. Neither half is redundant — the fingerprint cannot see a
	// generator fix nobody versioned, and the build identity cannot see a
	// setting nudged in this process. See DESIGN.md 29.5.
	p.CreateLine = fmt.Sprintf("wgva-world create --seed %s --expect %s/%s world.wgva",
		p.SeedText, p.Build, d.String())

	base := "/seed/" + p.SeedText
	p.Tabs = []link{
		{Label: "map", URL: base + "?" + req.view.Query(), Current: tab == "map"},
		{Label: "grid", URL: base + "/grid?" + req.view.Query(), Current: tab == "grid"},
		{Label: "configuration", URL: base + "/config", Current: tab == "config"},
	}

	if tab == "config" {
		p.Groups = configGroups(req.cfg)
		return p
	}

	path := base
	if tab == "grid" {
		path = base + "/grid"
	}
	href := func(v view.View) string { return path + "?" + v.Query() }

	for _, l := range render.AllLayers() {
		v, err := req.view.With("layer", l.Name)
		if err != nil {
			continue
		}
		p.Layers = append(p.Layers, link{
			Label: l.Name, URL: href(v), Title: l.Doc, Current: l.Name == req.view.Layer,
		})
	}

	for turn := range 6 {
		v, _ := req.view.With("turn", fmt.Sprint(turn))
		p.Turns = append(p.Turns, link{
			Label: fmt.Sprint(turn), URL: href(v), Current: turn == req.view.Turn,
		})
	}

	// One click moves a third of what is on screen, and the window grammar is
	// what decides how far that is. This used to be a quarter of the columns
	// computed here, which was wrong twice over: it was the same distance
	// whichever way the click went, so a tall window scrolled north by a
	// fraction of its width, and it counted cells as hexes, so a grid at a
	// stride of sixty-four moved a sixty-fourth of the picture it was showing.
	// Both are the front end having a second opinion about the grammar. See
	// DESIGN.md 29.3.
	for _, point := range render.AdminCompass() {
		step := req.view.ScrollStep(point.Name)
		if tab == "grid" {
			step = req.view.GridScrollStep(point.Name)
		}
		p.Scroll = append(p.Scroll, link{
			Label: point.Name,
			URL:   href(req.view.Scrolled(point.Name, step)),
			Title: fmt.Sprintf("%d hexes toward absolute direction %d", step, point.Direction),
		})
	}

	// The two tabs zoom along genuinely different axes, so the links differ.
	//
	// On the map, zoom is the hex radius: how large a hex is drawn. On the grid
	// one cell is one block of pixels whatever happens, so the hex radius means
	// nothing there — pointing these links at it left the grid tab with two
	// controls that changed the address bar and not the picture. Zoom on the
	// grid is the stride, which is the only thing that can change how much world
	// is on screen.
	//
	// Note the inversion: zooming in *lowers* the stride, because a smaller
	// stride samples more finely and shows less world.
	if tab == "grid" {
		p.Zoom = []link{
			{
				Label: "zoom in",
				URL:   href(req.view.Strided(0.5)),
				Title: strideTitle(req.view.Strided(0.5).Stride),
			},
			{
				Label: "zoom out",
				URL:   href(req.view.Strided(2)),
				Title: strideTitle(req.view.Strided(2).Stride),
			},
		}
	} else {
		p.Zoom = []link{
			{Label: "zoom in", URL: href(req.view.Zoomed(2)),
				Title: fmt.Sprintf("%d pixels from a hex's center to a corner", req.view.Zoomed(2).HexRadius)},
			{Label: "zoom out", URL: href(req.view.Zoomed(0.5)),
				Title: fmt.Sprintf("%d pixels from a hex's center to a corner", req.view.Zoomed(0.5).HexRadius)},
		}
	}
	for _, size := range []int{11, 41, 101, 301, 601, 1001} {
		v, _ := req.view.With("cols", fmt.Sprint(size))
		v, _ = v.With("rows", fmt.Sprint(size*3/4))
		p.Size = append(p.Size, link{
			Label: fmt.Sprintf("%d wide", size), URL: href(v), Current: size == req.view.Cols,
		})
	}

	// What this window costs, on the tab that is about to draw it. It is
	// reported and never enforced: a person tuning terrain who asks for the
	// whole world at the terrain layer is using the tool correctly, and what the
	// tool owes them is the number rather than a refusal.
	layer, err := req.view.RenderLayer()
	if err == nil {
		p.LayerDoc = layer.Doc
		viewport := req.view.Viewport
		if tab == "grid" {
			viewport = req.view.GridViewport
		}
		if vp, err := viewport(); err == nil {
			p.Evaluations = vp.Evaluations(layer)
			p.Tiles = vp.Tiles()
		}
	}

	tree := fieldTreeFor(req.gen, req.view.Layer)
	p.FieldTree = tree.Describe()

	switch tab {
	case "map":
		p.ImageURL = base + "/map.png?" + req.view.Query()
		if vp, err := req.view.Viewport(); err == nil {
			p.ReadoutCost = vp.Cost()
			start := time.Now()
			p.Distribution = render.Measure(req.gen, vp)
			logCost("readout "+base, renderCost{
				tiles:       vp.Tiles(),
				evaluations: vp.Cost(),
				generate:    time.Since(start),
			})
		}
	case "grid":
		p.ImageURL = base + "/grid.png?" + req.view.Query()
		for _, stride := range []int{1, 2, 4, 8, 16, 64, 256} {
			v, _ := req.view.With("stride", fmt.Sprint(stride))
			p.Strides = append(p.Strides, link{
				Label: fmt.Sprintf("1:%d", stride), URL: href(v), Current: stride == req.view.Stride,
			})
		}
		// Four of the six turns shear this picture. The grid's distortion has
		// two-fold symmetry and the hex grid has six-fold, so only turns 0 and 3
		// lie in both; the other four change angles and introduce apparent
		// directionality that varies with the turn — which is exactly the
		// artifact a turned view is usually being used to look for. The hex tab
		// needs no such warning at any turn.
		if req.view.Turn != 0 && req.view.Turn != 3 {
			p.Warning = fmt.Sprintf("Turn %d shears this picture. "+
				"The grid's distortion has two-fold symmetry and the hex grid has six-fold, "+
				"so only turns 0 and 3 lie in both; the other four change angles and add "+
				"apparent directionality that varies with the turn. The map tab is undistorted at every turn.",
				req.view.Turn)
		}
	}

	return p
}

// strideTitle says what a stride means in hexes, because "1:16" does not say it
// on its own.
func strideTitle(stride int) string {
	if stride == 1 {
		return "1:1 — every hex sampled"
	}
	return fmt.Sprintf("1:%d — one sampled cell every %d hexes", stride, stride)
}

// scaleOf maps a layer name back to the scale whose tree it draws. Every layer
// in this phase is one of the four raw scales; the fallback is the coarsest,
// which is what a tree panel should show when a layer has no single tree.
// fieldTreeFor returns the derived field tree a layer is drawn from, so the
// page can print what was actually evaluated beside the window.
//
// Most layers are not one field — the elevation composite is four of them and a
// fold, and a region bias is no field at all — so the fallback is the coarsest
// scale, which is the one that says whether the world has a shape at all. That
// is a display choice and nothing reads it.
func fieldTreeFor(g *wgva.Generator, layer string) wgva.Field {
	if layer == "ridge" {
		return g.RidgeField()
	}
	for _, s := range wgva.Scales() {
		if s.String() == layer {
			return g.ScaleField(s)
		}
	}
	return g.ScaleField(wgva.ScaleContinental)
}

// configGroups lays the field table out for the form, marking every field that
// has been moved off this binary's default.
func configGroups(cfg wgva.Config) []configGroup {
	defaults := wgva.DefaultConfig()

	var groups []configGroup
	for _, f := range config.Fields() {
		if len(groups) == 0 || groups[len(groups)-1].Name != f.Group {
			groups = append(groups, configGroup{Name: f.Group})
		}
		g := &groups[len(groups)-1]

		var choices []string
		for _, c := range f.Choices {
			choices = append(choices, c.Name)
		}
		value, def := f.Get(cfg), f.Get(defaults)
		g.Fields = append(g.Fields, configField{
			Key:     f.Key,
			Doc:     f.Doc,
			Value:   value,
			Choices: choices,
			Default: def,
			Moved:   value != def,
		})
	}
	return groups
}

func (s *server) renderPage(w http.ResponseWriter, t *template.Template, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := t.ExecuteTemplate(w, t.Name(), p); err != nil {
		// The response has already begun, so there is nothing useful to send.
		// Saying so on the console is what a developer's tool should do.
		log.Printf("rendering %s: %v", t.Name(), err)
	}
}

// handleRoot redirects to a seed's map tab. It doubles as the target of the
// seed form, because an HTML GET form cannot write a path segment.
func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	seed := s.defaultSeed
	if text := r.URL.Query().Get("seed"); text != "" {
		parsed, err := view.ParseSeed(text)
		if err != nil {
			refuse(w, err)
			return
		}
		seed = parsed
	}

	target := "/seed/" + view.FormatSeed(seed)
	switch r.URL.Query().Get("tab") {
	case "grid":
		target += "/grid"
	case "config":
		target += "/config"
	}

	q := r.URL.Query()
	q.Del("seed")
	q.Del("tab")
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleMap draws the map tab: the picture, the controls, and the readout of
// what is in the window.
//
// Nothing here refuses a window for being expensive. The readout costs a whole
// Tile per cell — seven evaluations whatever layer is on screen — so a large
// window on this tab is genuinely slow, and what the page does about that is say
// what it cost. See DESIGN.md 29.1 and 31.1.
func (s *server) handleMap(w http.ResponseWriter, r *http.Request) {
	req, ok := s.parse(w, r)
	if !ok {
		return
	}
	s.renderPage(w, mapTemplate, s.newPage("map", req))
}

func (s *server) handleGrid(w http.ResponseWriter, r *http.Request) {
	req, ok := s.parse(w, r)
	if !ok {
		return
	}
	s.renderPage(w, gridTemplate, s.newPage("grid", req))
}

func (s *server) handleMapPNG(w http.ResponseWriter, r *http.Request) {
	req, ok := s.parse(w, r)
	if !ok {
		return
	}
	layer, err := req.view.RenderLayer()
	if err != nil {
		refuse(w, err)
		return
	}
	vp, err := req.view.Viewport()
	if err != nil {
		renderRefusal(w, err)
		return
	}
	start := time.Now()
	img, err := s.withRender(func() (*image.RGBA, error) {
		return render.Render(req.gen, vp, layer, req.view.HexRadius)
	})
	if err != nil {
		renderRefusal(w, err)
		return
	}
	s.writePNG(w, r, img, req.cfg, renderCost{
		tiles:       vp.Tiles(),
		evaluations: vp.Evaluations(layer),
		generate:    time.Since(start),
	})
}

func (s *server) handleGridPNG(w http.ResponseWriter, r *http.Request) {
	req, ok := s.parse(w, r)
	if !ok {
		return
	}
	layer, err := req.view.RenderLayer()
	if err != nil {
		refuse(w, err)
		return
	}
	vp, err := req.view.GridViewport()
	if err != nil {
		renderRefusal(w, err)
		return
	}
	start := time.Now()
	img, err := s.withRender(func() (*image.RGBA, error) {
		return render.RenderGrid(req.gen, vp, layer, req.view.Scale)
	})
	if err != nil {
		renderRefusal(w, err)
		return
	}
	s.writePNG(w, r, img, req.cfg, renderCost{
		tiles:       vp.Tiles(),
		evaluations: vp.Evaluations(layer),
		generate:    time.Since(start),
	})
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	req, ok := s.parse(w, r)
	if !ok {
		return
	}
	p := s.newPage("config", req)
	p.Notice = r.URL.Query().Get("notice")
	p.Failure = r.URL.Query().Get("failure")
	s.renderPage(w, configTemplate, p)
}

// handleConfigFields applies the form.
//
// Every field is read from the form and the whole configuration is replaced at
// once, so a POST that would make an unusable world changes nothing at all.
func (s *server) handleConfigFields(w http.ResponseWriter, r *http.Request) {
	seed, err := view.ParseSeed(r.PathValue("seed"))
	if err != nil {
		refuse(w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		refuse(w, err)
		return
	}

	cfg := s.config()
	for _, f := range config.Fields() {
		text := r.PostForm.Get(f.Key)
		if text == "" {
			continue
		}
		if err := f.Set(&cfg, text); err != nil {
			s.backToConfig(w, r, seed, "", err.Error())
			return
		}
	}
	if err := s.setConfig(cfg); err != nil {
		s.backToConfig(w, r, seed, "", err.Error())
		return
	}
	s.backToConfig(w, r, seed, "applied", "")
}

// handleConfigUpload adopts an uploaded or pasted file.
func (s *server) handleConfigUpload(w http.ResponseWriter, r *http.Request) {
	seed, err := view.ParseSeed(r.PathValue("seed"))
	if err != nil {
		refuse(w, err)
		return
	}
	// A configuration file is a few kilobytes. The bound is here because this
	// endpoint reads a body, not because anybody expects to reach it.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if err := r.ParseForm(); err != nil {
			refuse(w, err)
			return
		}
	}

	data := []byte(r.PostForm.Get("contents"))
	if file, _, err := r.FormFile("file"); err == nil {
		defer func() { _ = file.Close() }()
		uploaded, err := io.ReadAll(file)
		if err != nil {
			s.backToConfig(w, r, seed, "", err.Error())
			return
		}
		if len(strings.TrimSpace(string(uploaded))) > 0 {
			data = uploaded
		}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		s.backToConfig(w, r, seed, "", "nothing was uploaded or pasted")
		return
	}

	cfg, err := config.Unmarshal(data)
	if err != nil {
		s.backToConfig(w, r, seed, "", err.Error())
		return
	}
	if err := s.setConfig(cfg); err != nil {
		s.backToConfig(w, r, seed, "", err.Error())
		return
	}
	s.backToConfig(w, r, seed, "adopted", "")
}

// handleConfigReset goes back to this binary's defaults.
func (s *server) handleConfigReset(w http.ResponseWriter, r *http.Request) {
	seed, err := view.ParseSeed(r.PathValue("seed"))
	if err != nil {
		refuse(w, err)
		return
	}
	if err := s.setConfig(wgva.DefaultConfig()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.backToConfig(w, r, seed, "back to this binary's defaults", "")
}

func (s *server) backToConfig(w http.ResponseWriter, r *http.Request, seed wgva.Seed, notice, failure string) {
	q := url.Values{}
	if notice != "" {
		q.Set("notice", notice)
	}
	if failure != "" {
		q.Set("failure", failure)
	}
	target := "/seed/" + view.FormatSeed(seed) + "/config"
	if encoded := q.Encode(); encoded != "" {
		target += "?" + encoded
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleDownload turns the configuration behind the picture back into a file.
//
// This is what makes the tool's one concession — that a link shows what this
// process is drawing now rather than what it drew when the link was copied —
// survivable: a picture can always be traced to the configuration that produced
// it and reproduced outside the tool.
func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	cfg := s.config()
	data, err := config.Marshal(cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/toml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config.toml"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
