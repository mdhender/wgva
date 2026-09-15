// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package render

import (
	"errors"
	"fmt"
	"image/color"
	"math"
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

// Swatch is one entry of a band table or a vocabulary legend: a label and the
// color the layer paints that value.
type Swatch struct {
	Label string
	Color color.RGBA
}

// Key is what a front end draws beside a window so the picture can be read.
type Key struct {
	Kind KeyKind

	// Ramp, and the values its ends stand for. KeyRamp only.
	Ramp             Ramp
	Lo, Hi           float64
	LoLabel, HiLabel string

	// Swatches are what the layer paints, indexed by the value Layer.Sample
	// returns. KeyClimate and KeyTerrain only.
	//
	// The index is the value rather than a position in a sorted list, which is
	// what lets the legend and the picture come from one table: a terrain's
	// swatch is at its own persisted number, and a climate cell's is at its
	// row-major position in the two band ladders.
	Swatches []Swatch

	// Rows and Cols label the two axes of a band table, and the swatches are
	// row-major over them. KeyClimate only.
	//
	// They are what makes the legend a table rather than a list of
	// twenty-five names: the axes are independent, and a legend that ran them
	// together in one column would be the single combined climate value
	// DESIGN.md 16.1 forbids, drawn as a picture rather than declared as a
	// type.
	Rows, Cols []string
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

// colorOf turns one sampled value into the pixel the layer paints.
//
// The two shapes are genuinely different operations rather than one with a
// parameter: a ramp interpolates a continuous scalar and a band table looks up
// a discrete one. Running a classification through a ramp would blend two
// terrains into a third color that names nothing.
//
// The index is clamped before it is converted, which is DESIGN.md 25.5 applied
// where it usually bites least and would bite hardest: an out-of-range
// float-to-int conversion is implementation-specific in Go and differs between
// amd64 and arm64, and here it would index a slice.
func colorOf(k Key, value float64) color.RGBA {
	switch k.Kind {
	case KeyClimate, KeyTerrain:
		if math.IsNaN(value) || len(k.Swatches) == 0 {
			return Background
		}
		i := int(min(max(value, 0), float64(len(k.Swatches)-1)))
		return k.Swatches[i].Color
	default:
		return k.Ramp.At(normalize(k, value))
	}
}

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
// DESIGN.md 29 lists seventeen and all seventeen are here: the raw noise scales
// of DESIGN.md 10, which separate "the noise is wrong" from "the composition is
// wrong" when a window looks off; the four the elevation composite adds, which
// separate it further into the four scales alone, the finished scalar, the slope,
// and the ridge term; the two that draw the blended region influence of
// DESIGN.md 11.2 — which is what the anchor lattice has to be looked for in,
// because a lattice nothing draws is a lattice nobody sees until it is under a
// coastline; the two climate axes, drawn apart because they are apart, and the
// band table that puts them back together; the two fields terrain reads that
// nothing else does; the rim profile; and terrain itself.
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
	out = append(out,
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

	// The two climate axes, drawn one at a time and never together. They are
	// independent — a polar desert and a polar rainforest are both ordinary
	// places — so two pictures is what the model actually says, and a single
	// picture of "climate" would have to invent a combined quantity to color by.
	// Each gets its own ramp for the same reason: a reader brings a convention
	// to a temperature and another one to rainfall, and two layers in one set of
	// colors invite a comparison the axes do not support. See DESIGN.md 16.1.
	//
	// Both cost one evaluation a tile. The climate composite reads the elevation
	// scalar at the tile and nothing around it; nothing here looks at a
	// neighbor, which is what separates these from relief.
	return append(out,
		Layer{
			Name: "temperature",
			Doc:  "the heat scalar: -1 polar, 0 the middle of the temperate band, +1 hot, with the lapse rate already taken off the high ground",
			Cost: 1,
			key: Key{
				Kind: KeyRamp, Ramp: TemperatureRamp,
				Lo: -1, Hi: +1, LoLabel: "polar", HiLabel: "hot",
			},
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.HeatAt(c) },
		},
		Layer{
			Name: "moisture",
			Doc:  "the moisture scalar: -1 arid, 0 the middle of the moderate band, +1 saturated",
			Cost: 1,
			key: Key{
				Kind: KeyRamp, Ramp: MoistureRamp,
				Lo: -1, Hi: +1, LoLabel: "arid", HiLabel: "saturated",
			},
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.MoistureAt(c) },
		},
		Layer{
			Name: "climate",
			Doc:  "the two-axis climate classification: which of the twenty-five heat and moisture cells a tile falls in",
			// One evaluation a tile, and the reason is the model rather than
			// the arithmetic: nothing in DESIGN.md 16 gives either climate
			// axis a term that reads a neighbor, so the composite reads the
			// elevation scalar at this tile and nothing around it. Only relief
			// and terrain read the six.
			//
			// DESIGN.md 29, 29.1, and 31 costed this at seven until phase 6
			// measured it; appendix D.14 records what settled it and what
			// would make the seven right again.
			Cost:   1,
			key:    climateKey(),
			sample: climateIndexOf,
		},
		Layer{
			Name: "basin",
			Doc:  "the blended basin influence: -1 a rise that sheds water, 0 neutral ground, +1 a closed hollow; it multiplies the moisture terrain reads and never enters elevation",
			Cost: 1,
			key: Key{
				Kind: KeyRamp, Ramp: BasinRamp,
				Lo: -1, Hi: +1, LoLabel: "rise", HiLabel: "hollow",
			},
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.BasinAt(c) },
		},
		Layer{
			Name: "volcanic",
			Doc:  "the volcanic tendency, which terrain reads only the top of: a province is where the cones can be, not where they are",
			Cost: 1,
			key: Key{
				Kind: KeyRamp, Ramp: VolcanicRamp,
				Lo: -1, Hi: +1, LoLabel: "quiet", HiLabel: "volcanic",
			},
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.VolcanicAt(c) },
		},
		Layer{
			Name: "rim",
			Doc:  "the rim profile of DESIGN.md 15.1: 0 across the closed band that is forced terrain, rising across the falloff, and 1 over the whole of the world inside it",
			// One evaluation of nothing at all. The profile is a function of the
			// coordinate's distance from the edge of the map and reads no field,
			// which is the property DESIGN.md 17.1 contrasts inland water
			// against; the cost is one because the unit is evaluations a tile
			// and zero would read as a layer that draws nothing.
			Cost: 1,
			key: Key{
				Kind: KeyRamp, Ramp: UnitRamp,
				Lo: 0, Hi: 1, LoLabel: "forced", HiLabel: "the world",
			},
			// What this draws is RimDistance run through the falloff, rather
			// than the distance itself. The distance is a number up to the world
			// radius and its picture is a hexagonal gradient over the whole map
			// that says nothing about the band; the profile is flat everywhere
			// the rim does not reach, so what a window shows is the band and the
			// shape of its shelf. That is the question this layer exists to
			// answer — how wide is the rim and how hard does it arrive — and it
			// is why the tuning tool can be pointed at a corner of the map
			// rather than hunting for the edge in the terrain layer.
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return g.RimProfileAt(c) },
		},
		Layer{
			Name: "terrain",
			Doc:  "the game-facing terrain classification of DESIGN.md 17, which is every other layer in this list arriving at one answer",
			// Seven evaluations a tile, for the reason relief costs seven: the
			// classifier reads the six neighboring elevation scalars, once for
			// the slope and once for the water's edge. It is the same walk and
			// it is paid for once.
			Cost:   7,
			key:    terrainKey(),
			sample: func(g *wgva.Generator, c wgva.Coord) float64 { return float64(g.TerrainAt(c)) },
		},
	)
}

// climateIndexOf is the two-axis classification flattened to one index, row
// major over heat and then moisture.
//
// Flattening is what lets one []float64 carry every layer's samples, and it is
// not a combined climate value of the kind DESIGN.md 16.1 forbids: nothing
// compares two of these or orders them, and the key turns the number straight
// back into the pair it came from.
func climateIndexOf(g *wgva.Generator, c wgva.Coord) float64 {
	cl := g.ClimateAt(c)
	return float64(int(cl.Heat)*len(wgva.Moistures()) + int(cl.Moisture))
}

// climateKey builds the two-axis band table legend.
//
// The swatches are row major over the two ladders and the axes are labelled
// separately, so a front end draws a five by five table rather than a list of
// twenty-five names. That is the shape of the model: the axes are independent,
// and a legend that ran them together in one column would be asserting an order
// over the pairs that does not exist.
func climateKey() Key {
	heats, moistures := wgva.Heats(), wgva.Moistures()

	k := Key{Kind: KeyClimate}
	for _, h := range heats {
		k.Rows = append(k.Rows, h.String())
	}
	for _, m := range moistures {
		k.Cols = append(k.Cols, m.String())
	}
	for i, h := range heats {
		for j, m := range moistures {
			k.Swatches = append(k.Swatches, Swatch{
				Label: wgva.Climate{Heat: h, Moisture: m}.String(),
				Color: climateColors[i][j],
			})
		}
	}
	return k
}

// terrainKey builds the terrain vocabulary legend.
//
// The swatches are in wgva.Terrains order, which is value order, so a swatch
// sits at its own persisted number and the picture and the legend index the
// same table. Every declared terrain is listed including the two inland-water
// values DESIGN.md 17.1 produces nowhere, because a row reading zero is usually
// the row somebody is trying to move off zero.
//
// It panics on a terrain with no color, which is a table error rather than a
// caller error: both tables are package data, the check runs once at
// construction, and the alternative is a window with a transparent hole in it
// that nobody notices until it is in a bug report.
func terrainKey() Key {
	k := Key{Kind: KeyTerrain}
	for _, t := range wgva.Terrains() {
		c, ok := terrainColors[t]
		if !ok {
			panic(fmt.Sprintf("render: %v has no color", t))
		}
		k.Swatches = append(k.Swatches, Swatch{Label: t.String(), Color: c})
	}
	return k
}
