// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mdhender/wgva"
)

// KeyKind is the shape of a layer's legend.
//
// There are three rather than a ramp with two labels because the layers are
// genuinely three different things: most are scalar ramps, climate is a
// two-axis band table, and terrain is a vocabulary of swatches. A front end that
// had to guess which it was drawing would guess wrong on the day a layer was
// added. See DESIGN.md 29.
type KeyKind uint8

// The key kinds.
const (
	KeyRamp    KeyKind = 1
	KeyClimate KeyKind = 2
	KeyTerrain KeyKind = 3
)

// Key is what a front end draws beside a window so the picture can be read.
type Key struct {
	Kind KeyKind

	// Ramp, and the values its ends stand for. KeyRamp only.
	Ramp             Ramp
	Lo, Hi           float64
	LoLabel, HiLabel string
}

// Layer is one thing a window can be drawn as.
type Layer struct {
	// Name is what a URL says and what a page prints.
	Name string

	// Doc is one line on what the layer shows and what it is for.
	Doc string

	// Cost is how many generator evaluations one tile of this layer takes.
	//
	// It is the unit the tuning tool's budget is counted in, and it has to be,
	// because a tile count cannot tell a cheap window from one seven times
	// longer: relief, climate, and terrain read the six neighboring elevations
	// and cost seven apiece, and every other layer costs one. See DESIGN.md 29.
	Cost int

	key    Key
	sample func(*wgva.Generator, wgva.Coord) float64
}

// Key returns the layer's legend.
func (l Layer) Key() Key { return l.key }

// Sample returns the layer's value at a coordinate.
//
// Renderer pixel coordinates never reach this: a Viewport converts a cell to a
// canonical coordinate and the coordinate is what is sampled, so nothing about
// the picture can feed back into what is generated. See DESIGN.md 29.
func (l Layer) Sample(g *wgva.Generator, c wgva.Coord) float64 {
	return l.sample(g, c)
}

// ErrUnknownLayer is returned for a layer name this binary does not draw.
var ErrUnknownLayer = errors.New("unknown layer")

// AllLayers returns every layer, in a fixed order.
//
// DESIGN.md 29 lists seventeen. Ten of them are here, and they are the ten that
// exist: the raw noise scales of DESIGN.md 10, which separate "the noise is
// wrong" from "the composition is wrong" when a window looks off; the four the
// elevation composite adds, which separate it further into the four scales
// alone, the finished scalar, the slope, and the ridge term; and the two that
// draw the blended region influence of DESIGN.md 11.2 — which is what the
// anchor lattice has to be looked for in, because a lattice nothing draws is a
// lattice nobody sees until it is under a coastline. Climate, terrain, the
// basins, the volcanic field, and the rim arrive with the phases that compute
// them; a layer that named a field nothing generates yet would be a control
// that draws an error.
func AllLayers() []Layer { return slices.Clone(layers) }

// LayerNamed returns the layer with that name.
func LayerNamed(name string) (Layer, error) {
	for _, l := range layers {
		if l.Name == name {
			return l, nil
		}
	}
	return Layer{}, fmt.Errorf("%q: %w", name, ErrUnknownLayer)
}

// DefaultLayer is what a window is drawn as when nothing says otherwise. It is
// the coarsest scale, because that is the one that says whether the world has a
// shape at all.
const DefaultLayer = "continentalness"

var layers = buildLayers()

func buildLayers() []Layer {
	signedKey := Key{
		Kind: KeyRamp, Ramp: SignedRamp,
		Lo: -1, Hi: +1, LoLabel: "-1", HiLabel: "+1",
	}

	docs := map[wgva.Scale]string{
		wgva.ScaleContinental: "the coarsest scale: continents and their interiors",
		wgva.ScaleRegional:    "uplift and the shape of a coast",
		wgva.ScaleLocal:       "hills and valleys",
		wgva.ScaleDetail:      "the finest structure the tile grid can carry",
	}

	var out []Layer
	for _, s := range wgva.Scales() {
		out = append(out, Layer{
			Name: s.String(),
			Doc:  docs[s],
			// One evaluation a tile. The seven-evaluation layers arrive with
			// relief, which is a first difference between neighbors.
			Cost:   1,
			key:    signedKey,
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.ScaleAt(s, c) },
		})
	}

	// The four elevation layers, in the order a window is read when it looks
	// wrong: what the noise summed to, what the composite made of it, how steep
	// the result is, and what the ridge term was contributing. Only the first
	// two draw the same quantity at two stages, and that pair is the point —
	// between them sit the contrast pass, the regional uplift, and the ridges,
	// so a coastline that looks wrong in one and right in the other has named
	// its own cause.
	out = append(out,
		Layer{
			Name:   "elevation-raw",
			Doc:    "the weighted sum of the four continuous scales alone, before the contrast pass, the uplift, and the ridges",
			Cost:   1,
			key:    signedKey,
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.Sample(c).ElevationRaw },
		},
		Layer{
			Name: "elevation",
			Doc:  "the elevation scalar: -1 deep ocean, 0 sea level, +1 extreme highland",
			Cost: 1,
			key: Key{
				Kind: KeyRamp, Ramp: ElevationRamp,
				Lo: -1, Hi: +1, LoLabel: "deep ocean", HiLabel: "extreme highland",
			},
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.ElevationAt(c) },
		},
		Layer{
			Name: "relief",
			Doc:  "local steepness: the mean elevation difference to the six neighbors",
			// Seven evaluations a tile, because it reads the six neighboring
			// elevation scalars as well as its own. This is the whole reason
			// the tuning tool's budget is counted in evaluations rather than in
			// tiles. DESIGN.md 18 and 29.
			Cost: 7,
			key: Key{
				Kind: KeyRamp, Ramp: UnitRamp,
				Lo: 0, Hi: 1, LoLabel: "flat", HiLabel: "steep",
			},
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.Relief(c) },
		},
		Layer{
			Name:   "ridge",
			Doc:    "the ridge structure term, before the region roughness scales it and before the land mask confines it to land",
			Cost:   1,
			key:    signedKey,
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.Sample(c).Ridge },
		},
	)

	// The two region layers. Both draw one blended anchor parameter, and both
	// are here for the same reason: the anchor lattice is invisible in a
	// composed field and obvious in the parameter it biases, so this is where a
	// row of lozenges or a Voronoi plateau shows up while it can still be
	// fixed. Look for them in the grid tab at a scale where a region is a few
	// pixels across. See DESIGN.md 11.2.
	return append(out,
		Layer{
			Name:   "region-influence",
			Doc:    "the blended regional elevation bias: what regional uplift is made of",
			Cost:   1,
			key:    signedKey,
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.RegionInfluence(c).ElevationBias },
		},
		Layer{
			Name:   "roughness",
			Doc:    "the blended regional roughness bias: where relief is exaggerated and where it is subdued",
			Cost:   1,
			key:    signedKey,
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.RegionInfluence(c).Roughness },
		},
	)
}
