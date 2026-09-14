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
// continuous scales coarse to fine, then the warp, then the addressing
// hierarchy, then the rim.
func Fields() []Field { return fieldTable }

var fieldTable = buildFieldTable()

func buildFieldTable() []Field {
	table := []Field{{
		Key: "sea_level", Path: "SeaLevel", Group: "world", Kind: ValueScalar,
		Doc: "normalized elevation at which land begins, in [0, 1]",
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

	return append(table, []Field{
		{
			Key: "warp_strength_miles", Path: "WarpStrengthMiles", Group: "warp", Kind: ValueScalar,
			Doc: "how far the warp displaces the sampling position; zero disables the warp",
		},
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
			Key: "rim_kind", Path: "Rim.Kind", Group: "rim", Kind: ValueChoice,
			Doc: "what the closed band is made of; an ice rim reads as a polar cap, which is a promise about latitude the climate model does not make",
			Choices: []Choice{
				{Name: wgva.RimDeepOcean.String(), Value: uint64(wgva.RimDeepOcean)},
				{Name: wgva.RimPolarIce.String(), Value: uint64(wgva.RimPolarIce)},
			},
		},
	}...)
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
