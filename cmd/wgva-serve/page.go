// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package main

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/view"
)

//go:embed templates/*.gohtml
var templateFS embed.FS

var pageTemplate = template.Must(template.New("viewer.gohtml").ParseFS(templateFS, "templates/viewer.gohtml"))

// link is one control: a label and the URL that applies it.
//
// Every control here is a link. That is what makes the viewer stateless: the
// address bar keeps up, the back button works, and a window somebody wants to
// show somebody else is something they can paste.
type link struct {
	Label   string
	URL     string
	Title   string
	Current bool
}

// page is what the template is rendered with.
type page struct {
	SeedText string
	View     view.View

	Build            string
	AlgorithmVersion uint32
	RenderVersion    uint32
	PageVersion      uint32
	WorldRadius      int64
	Fingerprint      string
	FingerprintShort string
	ConfigLabel      string

	// World is empty in the diagnostic mode, and Diagnostic says so.
	World      string
	CreatedBy  string
	CreatedAt  string
	Diagnostic bool

	ImageURL string
	Layers   []link
	Scroll   []link
	Size     []link
	Zoom     []link
	Turns    []link

	LayerDoc    string
	Tiles       int
	Evaluations int

	// Centre is the tile the window is centred on, named rather than left for a
	// reader to count swatches against the key. See [centre].
	Centre centre
}

// centre is what the page says about the tile at the middle of the window.
//
// A link to a window is otherwise a link to a picture, and a reader has to count
// swatches against the key to find out what they are looking at. It costs one
// tile against the cols*rows the image beside it costs. DESIGN.md 29.3.
type centre struct {
	Coord     wgva.Coord
	S         wgva.Component
	RimHexes  int64
	Rim       bool
	Terrain   string
	Elevation string
	Heat      string
	Moisture  string

	// The scalars the classifications were made from. A band without the number
	// behind it cannot answer "how close to the next band is it?", which is the
	// question somebody moving a threshold is always asking.
	ElevationValue float64
	HeatValue      float64
	MoistureValue  float64
	ReliefValue    float64
}

// handlePage renders the viewer page.
func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
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

	etag := s.pageTag(req)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	p := s.newPage(req, viewport, layer)

	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// newPage assembles everything the page prints that is not the picture.
func (s *server) newPage(req request, viewport render.Viewport, layer render.Layer) page {
	v := req.view
	base := "/seed/" + s.seed.String()

	p := page{
		SeedText:         s.seed.String(),
		View:             v,
		Build:            wgva.Version().String(),
		AlgorithmVersion: wgva.AlgorithmVersion,
		RenderVersion:    render.RenderVersion,
		PageVersion:      PageVersion,
		WorldRadius:      wgva.WorldRadius,
		Fingerprint:      s.digest.String(),
		FingerprintShort: s.digest.Short(),
		ConfigLabel:      s.configurationLabel(),
		Diagnostic:       s.world == nil,
		ImageURL:         base + "/map.png?" + v.Query(),
		LayerDoc:         layer.Doc,
		Tiles:            viewport.Tiles(),
		Evaluations:      viewport.Evaluations(layer),
		Centre:           s.centreOf(v.Coord()),
	}
	if s.world != nil {
		w := s.world.World()
		p.World = s.world.Path()
		p.CreatedBy = w.CreatedBuild
		p.CreatedAt = w.CreatedAt.Format("2006-01-02 15:04 MST")
	}

	href := func(next view.View) string { return base + "?" + next.Query() }

	for _, l := range render.AllLayers() {
		next, err := v.With("layer", l.Name)
		if err != nil {
			continue
		}
		p.Layers = append(p.Layers, link{Label: l.Name, URL: href(next), Title: l.Doc, Current: l.Name == v.Layer})
	}

	// The six controls are the compass walk of appendix A in the admin frame,
	// which the renderer draws without rotation: N is absolute direction 2 and
	// the walk clockwise from there decreases the index. Both the direction and
	// the distance come from packages that own them — render.AdminCompass and
	// view.ScrollStep — because a front end that computed either would have a
	// second opinion about the grammar, and the first one that tried it got both
	// halves wrong.
	for _, point := range render.AdminCompass() {
		step := v.ScrollStep(point.Name)
		p.Scroll = append(p.Scroll, link{
			Label: point.Name,
			URL:   href(v.Scrolled(point.Name, step)),
			Title: fmt.Sprintf("%d hexes toward absolute direction %d", step, point.Direction),
		})
	}

	for _, size := range []struct {
		label      string
		cols, rows int
	}{
		{"small", 41, 31},
		{"medium", 61, 45},
		{"large", 101, 75},
	} {
		next := v
		next.Cols, next.Rows = size.cols, size.rows
		p.Size = append(p.Size, link{
			Label:   size.label,
			URL:     href(next),
			Current: v.Cols == size.cols && v.Rows == size.rows,
		})
	}

	p.Zoom = []link{
		{Label: "zoom in", URL: href(v.Zoomed(2))},
		{Label: "zoom out", URL: href(v.Zoomed(0.5))},
	}

	for turn := range 6 {
		next := v
		next.Turn = turn
		p.Turns = append(p.Turns, link{
			Label:   fmt.Sprint(turn),
			URL:     href(next),
			Current: v.Turn == turn,
		})
	}

	return p
}

// centreOf reads the one tile the page names.
//
// One Tile call and not a Sample beside it. A Tile already carries every scalar
// the readout prints as well as the four classifications, so asking for both
// would be seven evaluations plus one more for values the first call had
// already produced.
func (s *server) centreOf(c wgva.Coord) centre {
	tile := s.gen.Tile(c)
	return centre{
		Coord:          c,
		S:              c.S(),
		RimHexes:       c.RimDistance(),
		Rim:            tile.Rim,
		Terrain:        tile.Terrain.String(),
		Elevation:      tile.Elevation.String(),
		Heat:           tile.Climate.Heat.String(),
		Moisture:       tile.Climate.Moisture.String(),
		ElevationValue: tile.ElevationValue,
		HeatValue:      tile.HeatValue,
		MoistureValue:  tile.MoistureValue,
		ReliefValue:    tile.ReliefValue,
	}
}
