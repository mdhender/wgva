// Copyright (c) 2026 Michael D Henderson. All rights reserved.

// Command wgva-map is the map renderer. It writes one window to one image file.
//
// It sits beside the administrator's path rather than on it (DESIGN.md 29.5).
// What it is for is the times something outside a browser needs an image: an
// acceptance sheet under docs/renders/, a bug report, a golden.
//
//	wgva-map --db world.wgva --q -200 --r -150 --cols 401 --rows 301 \
//	    --hex-radius 8 --layer terrain --out map.png
//
// **It never creates or modifies a world.** An absent or empty file is a refusal
// naming `wgva-world create`, not an invitation. It does not read pixels out of
// one either: no tiles are stored, so --db supplies the world's *identity* —
// seed, algorithm version, world radius, configuration, fingerprint — and the
// image is regenerated from it every time. The only thing genuinely read from
// the file and drawn is the player overlays.
//
// The window grammar is the view package's, the same one both web front ends
// present, so the flags below mean exactly what the query parameters of the same
// name mean and a window moves between the tools without a conversion. That is
// also what makes "the CLI and the front ends agree byte for byte" one assertion
// rather than a second set of goldens.
//
// See DESIGN.md 29.2.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/mdhender/wgva"
	"github.com/mdhender/wgva/render"
	"github.com/mdhender/wgva/view"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("wgva-map: ")
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// options are the flags, with the window ones held as the text a caller wrote.
//
// Holding them as text is what lets the ones a caller actually set be told from
// the ones they did not, and that distinction is the window grammar's: a
// parameter that is present is applied and one that is absent keeps the default,
// which is what makes every control a link that changes one thing. A CLI that
// passed all nine values every time would be a CLI that could not have a
// default, and it would disagree with both front ends about what a bare request
// means.
type options struct {
	db     string
	file   string
	seed   string
	out    string
	grid   bool
	window map[string]string
}

// windowFlags are the flags that are window grammar. Each is the query parameter
// of the same name, parsed and clamped by view and not here.
var windowFlags = []string{"q", "r", "cols", "rows", "turn", "layer", "hex-radius", "scale", "stride"}

func parseFlags(args []string, errOut io.Writer) (options, error) {
	fs := flag.NewFlagSet("wgva-map", flag.ContinueOnError)
	fs.SetOutput(errOut)
	o := options{window: map[string]string{}}

	fs.StringVar(&o.db, "db", "", "world file to read the world's identity from")
	fs.StringVar(&o.file, "config", "", "configuration file to draw (diagnostic; no world)")
	fs.StringVar(&o.seed, "seed", wgva.Seed(0).String(), "world seed, in hex or decimal; ignored with --db")
	fs.StringVar(&o.out, "out", "map.png", "image file to write")
	fs.BoolVar(&o.grid, "grid", false, "draw the one-pixel-per-hex view instead of hexes")
	for _, name := range windowFlags {
		fs.String(name, "", "window "+name+"; the viewer's query parameter of the same name")
	}

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "db", "config", "seed", "out", "grid":
		default:
			o.window[f.Name] = f.Value.String()
		}
	})
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected argument %q; every setting is a flag", fs.Arg(0))
	}
	if o.db != "" && o.file != "" {
		return options{}, errors.New("--db and --config are two sources for one configuration; give one")
	}
	return o, nil
}

func run(args []string, stdout io.Writer) error {
	o, err := parseFlags(args, os.Stderr)
	if err != nil {
		return err
	}

	src, err := openSource(o)
	if err != nil {
		return err
	}
	defer src.close()

	v, err := view.Parse(view.ViewerDefaults(src.seed), windowValues(o))
	if err != nil {
		return err
	}
	layer, err := v.RenderLayer()
	if err != nil {
		return err
	}
	viewport, err := viewportOf(v, o.grid)
	if err != nil {
		return err
	}

	start := time.Now()
	img, err := src.draw(viewport, layer, v, o.grid)
	if err != nil {
		return err
	}
	generate := time.Since(start)

	start = time.Now()
	if err := writePNG(o.out, img); err != nil {
		return err
	}
	encode := time.Since(start)

	// DESIGN.md 31.1: measure a render, not a tile, and separate generate from
	// encode before calling anything slow. The two move for different reasons —
	// generate when the octave ladders or the core count move, encode when the
	// image size or the PNG settings do — and an "it got slower" report that
	// does not separate them is not yet a measurement.
	tiles := viewport.Tiles()
	perSecond := 0.0
	if generate > 0 {
		perSecond = float64(tiles) / generate.Seconds()
	}
	fmt.Fprintf(stdout, "%s: %d tiles, %d evaluations, %d ms generate, %d ms encode, %.0f tiles/s\n",
		o.out, tiles, viewport.Evaluations(layer),
		generate.Milliseconds(), encode.Milliseconds(), perSecond)
	for _, line := range src.provenance() {
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// windowValues turns the window flags into the query the view package parses.
func windowValues(o options) url.Values {
	q := url.Values{}
	for name, value := range o.window {
		q.Set(name, value)
	}
	return q
}

// viewportOf builds the window the render walks. The grid tab is the same walk
// at a stride, which is why one view yields both.
func viewportOf(v view.View, grid bool) (render.Viewport, error) {
	if grid {
		return v.GridViewport()
	}
	return v.Viewport()
}
