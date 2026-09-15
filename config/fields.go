// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package config

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/mdhender/wgva"
)

// ValueKind is how one configuration value is written, parsed, and presented.
type ValueKind uint8

// The value kinds.
const (
	// ValueScalar is a float64. It is written with strconv.FormatFloat(v, 'g',
	// -1, 64), the shortest representation that parses back to identical bits.
	ValueScalar ValueKind = 1

	// ValueCount is an unsigned integer — a hex count, an octave count.
	ValueCount ValueKind = 2

	// ValueChoice is one of a named set, written as its name rather than as the
	// number behind it. A file that said `rim_kind = 1` would be a file nobody
	// can read.
	ValueChoice ValueKind = 3
)

// Choice is one alternative of a ValueChoice field.
type Choice struct {
	Name  string
	Value uint64
}

// Field describes one key of the configuration file and one control on the
// tuning tool's form.
//
// The table below is the single description of what a configuration contains,
// and it is what makes the three places that need that description agree: the
// file writer, the file reader's presence check, and the HTML form. A reflective
// test asserts the table covers every leaf field of wgva.Config exactly once, so
// a field added to Config without a row here fails a test rather than silently
// becoming unwritable and unreachable. See DESIGN.md 21.1.
type Field struct {
	// Key is the name in the TOML file and in the form. It is flat and
	// snake_case; the file is one key per line with no tables, so that a diff
	// between two configurations reads as a list of changed numbers.
	Key string

	// Path is the dotted path to the value inside wgva.Config.
	Path string

	// Group is what the form lays out under one heading.
	Group string

	// Doc is the comment written above the key in the file and the help shown
	// beside the control.
	Doc string

	Kind    ValueKind
	Choices []Choice
}

// Fields returns every configuration key, in file order.
//
// The order is the order a person reads: what the world is, then the four
// continuous scales coarse to fine, then the warp, then what the elevation
// composite makes of them, then the ridge structure, then the two climate axes,
// then the basin scales and what they are worth, then the volcanic tendency,
// then where the terrain rules are cut, then the addressing hierarchy, then the
// rim.
func Fields() []Field { return fieldTable }

var fieldTable = buildFieldTable()

func buildFieldTable() []Field {
	table := []Field{{
		Key: "sea_level", Path: "SeaLevel", Group: "world", Kind: ValueScalar,
		Doc: "where land begins, as a fraction of the elevation composite's range, in (0, 1); it decides the land fraction, and the elevation scalar puts sea level at zero whatever it is set to",
	}}

	table = append(table, ladderFields("continental", "Continental",
		"the continental scale: continents and their interiors")...)
	table = append(table, ladderFields("regional", "Regional",
		"the regional scale: uplift and the shape of a coast")...)
	table = append(table, ladderFields("local", "Local",
		"the local scale: hills and valleys")...)
	table = append(table, ladderFields("detail", "Detail",
		"the detail scale: the finest structure the tile grid can carry")...)
	table = append(table, ladderFields("warp", "Warp",
		"the domain warp's own field; DESIGN.md 13")...)

	table = append(table, Field{
		Key: "warp_strength_miles", Path: "WarpStrengthMiles", Group: "warp", Kind: ValueScalar,
		Doc: "how far the warp displaces the sampling position; zero disables the warp",
	})

	table = append(table, elevationFields()...)

	table = append(table, ladderFields("ridge", "Elevation.Ridge",
		"the ridge structure's own field; its wavelength is the spacing of a mountain belt, not of a peak")...)

	table = append(table, []Field{
		{
			Key: "ridge_stride_miles", Path: "Elevation.RidgeStrideMiles", Group: "ridge", Kind: ValueScalar,
			Doc: "how far the directional blur reaches along the region's ridge orientation; zero leaves an isotropic web of creases",
		},
		{
			Key: "ridge_weight", Path: "Elevation.RidgeWeight", Group: "ridge", Kind: ValueScalar,
			Doc: "what the ridge term is worth in the elevation composite, in [0, 1]",
		},
		{
			Key: "ridge_onset", Path: "Elevation.RidgeOnset", Group: "ridge", Kind: ValueScalar,
			Doc: "how far above sea level the ridge term reaches full strength; below it ridges are masked off so a belt does not surface as islands in open ocean",
		},
	}...)

	table = append(table, ladderFields("heat", "Climate.Heat",
		"the broad heat zones; its wavelength is the width of a climate zone, and it is the coarsest scale in the world because zones finer than continents read as weather")...)
	table = append(table, heatFields()...)

	table = append(table, ladderFields("moisture", "Climate.Moisture",
		"the broad moisture field: where the wet part of the world is")...)
	table = append(table, moistureFields()...)
	table = append(table, ladderFields("moisture_variation", "Climate.MoistureVariation",
		"the local moisture variation of DESIGN.md 16: why two neighboring valleys differ")...)

	table = append(table, ladderFields("basin_broad", "Basin.Broad",
		"the broadest scale of enclosed low ground: a continental interior that drains nowhere")...)
	table = append(table, ladderFields("basin_regional", "Basin.Regional",
		"the middle scale of enclosed low ground: one basin")...)
	table = append(table, ladderFields("basin_local", "Basin.Local",
		"the finest scale of enclosed low ground: the floor of one basin")...)
	table = append(table, basinFields()...)

	table = append(table, ladderFields("volcanic", "Terrain.Volcanic",
		"the volcanic tendency; its wavelength is the span of a province rather than of a cone, because what draws one cone out of a province is the uplift and the relief terrain also reads")...)
	table = append(table, volcanicFields()...)

	table = append(table, terrainFields()...)

	return append(table, []Field{
		{
			Key: "macro_region_size_hexes", Path: "MacroRegionSizeHexes", Group: "hierarchy", Kind: ValueCount,
			Doc: "macro region spacing along either axial basis direction",
		},
		{
			Key: "region_size_hexes", Path: "RegionSizeHexes", Group: "hierarchy", Kind: ValueCount,
			Doc: "region spacing; must exceed the chunk size",
		},
		{
			Key: "chunk_size_hexes", Path: "ChunkSizeHexes", Group: "hierarchy", Kind: ValueCount,
			Doc: "chunk spacing; an addressing device that must never be visible in the output",
		},
		{
			Key: "rim_closed_hexes", Path: "Rim.ClosedHexes", Group: "rim", Kind: ValueCount,
			Doc: "outermost band, forced terrain and closed to play",
		},
		{
			Key: "rim_falloff_hexes", Path: "Rim.FalloffHexes", Group: "rim", Kind: ValueCount,
			Doc: "band inside the closed one, over which elevation is driven down to the forced value",
		},
		{
			Key: "rim_floor_elevation", Path: "Rim.FloorElevation", Group: "rim", Kind: ValueScalar,
			Doc: "the forced elevation the closed band is pinned at and the falloff blends toward, in [-1, +1]; below sea level for a deep-ocean rim and above it for an icefield",
		},
		{
			Key: "rim_kind", Path: "Rim.Kind", Group: "rim", Kind: ValueChoice,
			Doc: "what the closed band is made of; an ice rim reads as a polar cap, which is a promise about latitude the climate model does not make",
			Choices: []Choice{
				{Name: wgva.RimDeepOcean.String(), Value: uint64(wgva.RimDeepOcean)},
				{Name: wgva.RimPolarIce.String(), Value: uint64(wgva.RimPolarIce)},
			},
		},
	}...)
}

// elevationFields returns the keys of the elevation composite: what each
// continuous scale is worth, how hard the coast is sharpened, what a region's
// biases buy, and where the bands fall. The ridge structure is its own group
// and follows this one.
func elevationFields() []Field {
	return []Field{
		{
			Key: "elevation_continental_weight", Path: "Elevation.ContinentalWeight", Group: "elevation", Kind: ValueScalar,
			Doc: "what the continental scale is worth in the composite; the weights are relative and are normalized by their own total",
		},
		{
			Key: "elevation_regional_weight", Path: "Elevation.RegionalWeight", Group: "elevation", Kind: ValueScalar,
			Doc: "what the regional scale is worth in the composite",
		},
		{
			Key: "elevation_local_weight", Path: "Elevation.LocalWeight", Group: "elevation", Kind: ValueScalar,
			Doc: "what the local scale is worth in the composite",
		},
		{
			Key: "elevation_detail_weight", Path: "Elevation.DetailWeight", Group: "elevation", Kind: ValueScalar,
			Doc: "what the detail scale is worth in the composite",
		},
		{
			Key: "elevation_contrast_passes", Path: "Elevation.ContrastPasses", Group: "elevation", Kind: ValueCount,
			Doc: "how many S-curve passes sharpen the coarse half, pulling the coastline off a wide band of near-sea-level ground; zero is the identity",
		},
		{
			Key: "elevation_uplift_weight", Path: "Elevation.UpliftWeight", Group: "elevation", Kind: ValueScalar,
			Doc: "what a region's elevation bias is worth in elevation, in [0, 1]",
		},
		{
			Key: "elevation_roughness_influence", Path: "Elevation.RoughnessInfluence", Group: "elevation", Kind: ValueScalar,
			Doc: "how far a region's roughness bias may exaggerate or subdue the fine scales and the ridges, in [0, 1]",
		},
		{
			Key: "relief_scale", Path: "Elevation.ReliefScale", Group: "elevation", Kind: ValueScalar,
			Doc: "relief per unit of elevation difference across one hex; it turns the mean neighbor difference into the normalized relief value",
		},
		{
			Key: "elevation_deep_water", Path: "Elevation.Bands.DeepWater", Group: "elevation", Kind: ValueScalar,
			Doc: "the elevation at or below which water is deep; negative, because zero is sea level",
		},
		{
			Key: "elevation_upland", Path: "Elevation.Bands.Upland", Group: "elevation", Kind: ValueScalar,
			Doc: "where land stops being lowland; above sea level and below the highland threshold",
		},
		{
			Key: "elevation_highland", Path: "Elevation.Bands.Highland", Group: "elevation", Kind: ValueScalar,
			Doc: "where upland becomes highland",
		},
		{
			Key: "elevation_mountain", Path: "Elevation.Bands.Mountain", Group: "elevation", Kind: ValueScalar,
			Doc: "where highland becomes mountain",
		},
	}
}

// basinFields returns the keys of the basin composite: what the three scales
// and the region's basin bias are worth against each other, and what the blend
// is worth as a multiplier on the moisture terrain reads.
//
// They are their own group rather than part of the terrain one because of where
// basin influence does not appear. It never enters elevation, and a reader who
// found these keys under the elevation composite would reasonably conclude the
// opposite; see DESIGN.md 17.1.
func basinFields() []Field {
	return []Field{
		{
			Key: "basin_broad_weight", Path: "Basin.BroadWeight", Group: "basin", Kind: ValueScalar,
			Doc: "what the broadest basin scale is worth in the composite; the weights are relative and are normalized by their own total",
		},
		{
			Key: "basin_regional_weight", Path: "Basin.RegionalWeight", Group: "basin", Kind: ValueScalar,
			Doc: "what the middle basin scale is worth in the composite",
		},
		{
			Key: "basin_local_weight", Path: "Basin.LocalWeight", Group: "basin", Kind: ValueScalar,
			Doc: "what the finest basin scale is worth in the composite",
		},
		{
			Key: "basin_bias_weight", Path: "Basin.BiasWeight", Group: "basin", Kind: ValueScalar,
			Doc: "what a region's basin bias is worth against the three fields",
		},
		{
			Key: "basin_moisture_weight", Path: "Basin.MoistureWeight", Group: "basin", Kind: ValueScalar,
			Doc: "how far a basin may move the moisture terrain is classified from, in [0, 1]; wetness is moisture + this*basin*moisture, so a basin deepens whatever climate it is in rather than making every basin wet, and zero leaves the climate's moisture unchanged",
		},
	}
}

// volcanicFields returns the keys of the volcanic tendency: what the field and
// the region's bias are worth against each other, and the three thresholds that
// turn the result into terrain.
//
// They sit under the same heading as the field's own ladder, for the reason the
// heat band keys sit under the heat field's.
func volcanicFields() []Field {
	return []Field{
		{
			Key: "volcanic_field_weight", Path: "Terrain.VolcanicFieldWeight", Group: "volcanic", Kind: ValueScalar,
			Doc: "what the volcanic field is worth against the region's volcanic bias; the weights are relative and are normalized by their own total",
		},
		{
			Key: "volcanic_bias_weight", Path: "Terrain.VolcanicBiasWeight", Group: "volcanic", Kind: ValueScalar,
			Doc: "what a region's volcanic bias is worth against the field",
		},
		{
			Key: "volcanic_elevation", Path: "Terrain.VolcanicElevation", Group: "volcanic", Kind: ValueScalar,
			Doc: "the uplift volcanic terrain needs; below it the tendency produces nothing, however strong it is",
		},
		{
			Key: "volcano_threshold", Path: "Terrain.VolcanoThreshold", Group: "volcanic", Kind: ValueScalar,
			Doc: "the tendency a cone needs, in (-1, +1); it exceeds the volcanic highland threshold, and together with the relief it is what makes a volcano rare without anything being rolled for",
		},
		{
			Key: "volcano_relief", Path: "Terrain.VolcanoRelief", Group: "volcanic", Kind: ValueScalar,
			Doc: "the concentrated relief a cone needs beside the tendency, in [0, 1]",
		},
		{
			Key: "volcanic_highland_threshold", Path: "Terrain.VolcanicHighlandThreshold", Group: "volcanic", Kind: ValueScalar,
			Doc: "the lower tendency that makes raised ground volcanic highland",
		},
	}
}

// terrainFields returns the thresholds the ordered rules of DESIGN.md 17 are
// cut at, in the order the rules run: the ocean depths, the two heat
// thresholds the frozen family is drawn from, the steepness that makes hills,
// the three conditions a wetland needs together with the two heats that divide
// them, and the steepness that makes a desert badlands.
func terrainFields() []Field {
	return []Field{
		{
			Key: "terrain_deep_ocean_depth", Path: "Terrain.DeepOceanDepth", Group: "terrain", Kind: ValueScalar,
			Doc: "the elevation at or below which ocean water is deep ocean; negative, because zero is sea level, and separate from the deep-water elevation band so that moving one does not move the other",
		},
		{
			Key: "terrain_ocean_depth", Path: "Terrain.OceanDepth", Group: "terrain", Kind: ValueScalar,
			Doc: "where deep ocean becomes ocean; above it is shallow sea, and water with a land neighbor is coastal water whatever its depth",
		},
		{
			Key: "terrain_ice_heat", Path: "Terrain.IceHeat", Group: "terrain", Kind: ValueScalar,
			Doc: "the heat at or below which land is under permanent ice, in (-1, +1); below the alpine threshold, or every alpine tile would already be ice",
		},
		{
			Key: "terrain_alpine_heat", Path: "Terrain.AlpineHeat", Group: "terrain", Kind: ValueScalar,
			Doc: "the heat at or below which a mountain is alpine rather than bare rock",
		},
		{
			Key: "terrain_hills_relief", Path: "Terrain.HillsRelief", Group: "terrain", Kind: ValueScalar,
			Doc: "the steepness that makes ground hills below the highland band, in [0, 1]; the highland band itself is hills whatever its relief",
		},
		{
			Key: "terrain_wetland_wetness", Path: "Terrain.WetlandWetness", Group: "terrain", Kind: ValueScalar,
			Doc: "how wet a wetland is, in (-1, +1); it is read against the wetness of DESIGN.md 17.1 — moisture after the basin product — which is what makes a wet basin read as marsh, swamp, or bog",
		},
		{
			Key: "terrain_wetland_elevation", Path: "Terrain.WetlandElevation", Group: "terrain", Kind: ValueScalar,
			Doc: "how low a wetland is; water stands where it has not run off the edge of a highland",
		},
		{
			Key: "terrain_wetland_relief", Path: "Terrain.WetlandRelief", Group: "terrain", Kind: ValueScalar,
			Doc: "how flat a wetland is, in [0, 1]; a rule that read only the moisture would put a swamp on a hillside",
		},
		{
			Key: "terrain_bog_heat", Path: "Terrain.BogHeat", Group: "terrain", Kind: ValueScalar,
			Doc: "the heat at or below which a wetland is a bog; below the swamp threshold, or the marsh between them is unreachable",
		},
		{
			Key: "terrain_swamp_heat", Path: "Terrain.SwampHeat", Group: "terrain", Kind: ValueScalar,
			Doc: "the heat at or above which a wetland is a swamp; between the two is a marsh",
		},
		{
			Key: "terrain_badlands_relief", Path: "Terrain.BadlandsRelief", Group: "terrain", Kind: ValueScalar,
			Doc: "the steepness that makes a desert badlands, in [0, 1]; the dry family's own evidence is low moisture, heat, and exposed relief",
		},
	}
}

// heatFields returns the keys of the heat axis: what the broad field and the
// region's bias are worth against each other, what altitude takes off the
// result, and where the bands fall.
//
// The two axes are separate groups because they are independent. A form that
// laid heat and moisture out together under one heading would be the single
// combined climate value DESIGN.md 16.1 forbids, drawn as a page rather than
// declared as a type.
func heatFields() []Field {
	return []Field{
		{
			Key: "climate_heat_field_weight", Path: "Climate.HeatFieldWeight", Group: "heat", Kind: ValueScalar,
			Doc: "what the broad heat field is worth against the region's heat bias; the weights are relative and are normalized by their own total",
		},
		{
			Key: "climate_heat_bias_weight", Path: "Climate.HeatBiasWeight", Group: "heat", Kind: ValueScalar,
			Doc: "what a region's heat bias is worth against the broad field",
		},
		{
			Key: "climate_elevation_cooling", Path: "Climate.ElevationCooling", Group: "heat", Kind: ValueScalar,
			Doc: "how much heat the highest ground loses, per unit of elevation above sea level, in [0, 1]; it reads max(elevation, 0), so it cools land and leaves the ocean surface alone",
		},
		{
			Key: "climate_heat_contrast_passes", Path: "Climate.HeatContrastPasses", Group: "heat", Kind: ValueCount,
			Doc: "how many S-curve passes spread the heat base before the cooling is taken off it; zero is the identity, and a world with none is temperate almost everywhere",
		},
		{
			Key: "climate_heat_polar", Path: "Climate.HeatBands.Polar", Group: "heat", Kind: ValueScalar,
			Doc: "the heat at or below which a tile is polar; in (-1, +1), and below the cold threshold",
		},
		{
			Key: "climate_heat_cold", Path: "Climate.HeatBands.Cold", Group: "heat", Kind: ValueScalar,
			Doc: "where polar becomes cold",
		},
		{
			Key: "climate_heat_temperate", Path: "Climate.HeatBands.Temperate", Group: "heat", Kind: ValueScalar,
			Doc: "where cold becomes temperate",
		},
		{
			Key: "climate_heat_warm", Path: "Climate.HeatBands.Warm", Group: "heat", Kind: ValueScalar,
			Doc: "where warm becomes hot; above it is the top band",
		},
	}
}

// moistureFields returns the keys of the moisture axis. See heatFields.
//
// It sits under the same heading as the broad moisture field's ladder and ahead
// of the local variation's, so the group is one contiguous run: a group name
// that appeared twice would be written as two sections of one file with the
// same title, which is a file nobody can diff.
func moistureFields() []Field {
	return []Field{
		{
			Key: "climate_moisture_field_weight", Path: "Climate.MoistureFieldWeight", Group: "moisture", Kind: ValueScalar,
			Doc: "what the broad moisture field is worth against the other two terms",
		},
		{
			Key: "climate_moisture_bias_weight", Path: "Climate.MoistureBiasWeight", Group: "moisture", Kind: ValueScalar,
			Doc: "what a region's moisture bias is worth",
		},
		{
			Key: "climate_moisture_variation_weight", Path: "Climate.MoistureVariationWeight", Group: "moisture", Kind: ValueScalar,
			Doc: "what the local variation is worth; this is the one term in either axis that varies tile to tile, and it is what turns a climate map into speckle if it is allowed to matter",
		},
		{
			Key: "climate_moisture_contrast_passes", Path: "Climate.MoistureContrastPasses", Group: "moisture", Kind: ValueCount,
			Doc: "how many S-curve passes spread the moisture base; zero is the identity",
		},
		{
			Key: "climate_moisture_arid", Path: "Climate.MoistureBands.Arid", Group: "moisture", Kind: ValueScalar,
			Doc: "the moisture at or below which a tile is arid; in (-1, +1), and below the dry threshold",
		},
		{
			Key: "climate_moisture_dry", Path: "Climate.MoistureBands.Dry", Group: "moisture", Kind: ValueScalar,
			Doc: "where arid becomes dry",
		},
		{
			Key: "climate_moisture_moderate", Path: "Climate.MoistureBands.Moderate", Group: "moisture", Kind: ValueScalar,
			Doc: "where dry becomes moderate",
		},
		{
			Key: "climate_moisture_humid", Path: "Climate.MoistureBands.Humid", Group: "moisture", Kind: ValueScalar,
			Doc: "where humid becomes saturated; above it is the top band",
		},
	}
}

// ladderFields returns the four keys of one fbm ladder.
//
// They are generated rather than written out five times because the shape is
// genuinely identical and a copied block is where a doc string goes stale.
func ladderFields(key, path, doc string) []Field {
	return []Field{
		{
			Key: key + "_wavelength_miles", Path: path + ".WavelengthMiles", Group: key, Kind: ValueScalar,
			Doc: doc + " — the coarsest octave's wavelength, in miles",
		},
		{
			Key: key + "_octaves", Path: path + ".Octaves", Group: key, Kind: ValueCount,
			Doc: "how many octaves the ladder runs; validation rejects one that reaches below the tile grid's Nyquist wavelength",
		},
		{
			Key: key + "_lacunarity", Path: path + ".Lacunarity", Group: key, Kind: ValueScalar,
			Doc: "the factor the frequency rises by between octaves; exceeds one",
		},
		{
			Key: key + "_gain", Path: path + ".Gain", Group: key, Kind: ValueScalar,
			Doc: "the factor the amplitude falls by between octaves, in (0, 1]",
		},
	}
}

// Get returns the field's value from a configuration, formatted as it appears in
// the file.
//
// A float64 is written with strconv.FormatFloat(v, 'g', -1, 64): the shortest
// representation that parses back to the identical bits. Writing it through an
// encoder's default float formatting is how a file comes to lose a low bit, and
// a file that loses a low bit is a silently different world.
func (f Field) Get(cfg wgva.Config) string {
	v := f.resolve(reflect.ValueOf(&cfg).Elem())
	switch f.Kind {
	case ValueScalar:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case ValueCount:
		return strconv.FormatUint(v.Uint(), 10)
	case ValueChoice:
		for _, c := range f.Choices {
			if c.Value == v.Uint() {
				return c.Name
			}
		}
		return strconv.FormatUint(v.Uint(), 10)
	default:
		panic(fmt.Sprintf("config: %s has no value kind", f.Key))
	}
}

// Set parses text and writes it into the configuration.
//
// It does not validate the result beyond the syntax: a value can be individually
// well formed and still make an unusable configuration, and wgva.Config.Validate
// is the one place that decides. Setting a field and validating are separate so
// that a form can report every bad field rather than only the first.
func (f Field) Set(cfg *wgva.Config, text string) error {
	v := f.resolve(reflect.ValueOf(cfg).Elem())
	text = strings.TrimSpace(text)

	switch f.Kind {
	case ValueScalar:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return &FileError{Key: f.Key, Detail: text, Err: ErrBadValue}
		}
		v.SetFloat(n)
		return nil

	case ValueCount:
		n, err := strconv.ParseUint(text, 10, 64)
		if err != nil || v.OverflowUint(n) {
			return &FileError{Key: f.Key, Detail: text, Err: ErrBadValue}
		}
		v.SetUint(n)
		return nil

	case ValueChoice:
		for _, c := range f.Choices {
			if c.Name == text {
				v.SetUint(c.Value)
				return nil
			}
		}
		return &FileError{Key: f.Key, Detail: text, Err: ErrBadValue}

	default:
		panic(fmt.Sprintf("config: %s has no value kind", f.Key))
	}
}

// resolve walks the dotted path to the addressable value inside a Config.
//
// It panics on a path that does not exist, which is a table error rather than a
// caller error: the table is package data and a reflective test walks all of it.
func (f Field) resolve(root reflect.Value) reflect.Value {
	v := root
	for name := range strings.SplitSeq(f.Path, ".") {
		v = v.FieldByName(name)
		if !v.IsValid() {
			panic(fmt.Sprintf("config: %s names %s, which wgva.Config does not have", f.Key, f.Path))
		}
	}
	return v
}
