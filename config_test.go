// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"errors"
	"math"
	"testing"
)

func TestDefaultConfigValidates(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig() is invalid: %v", err)
	}
}

// TestNyquistWavelengthIsDerived pins the bound to the hex metric rather than to
// a literal. Adjacent tile centers are two apothems apart, so the shortest
// feature the grid can carry is four.
func TestNyquistWavelengthIsDerived(t *testing.T) {
	if got, want := float64(NyquistWavelengthMiles), 2*hexCenterDistanceMiles; got != want {
		t.Fatalf("NyquistWavelengthMiles = %v, want twice the center distance, %v", got, want)
	}
}

// TestConfigValidation drives each rejection with a configuration that differs
// from the defaults in exactly one field, so the field named in the error is the
// field that was broken.
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  error
		spoil func(*Config)
	}{
		{"sea level NaN", "SeaLevel", ErrNotFinite,
			func(c *Config) { c.SeaLevel = math.NaN() }},
		{"sea level infinite", "SeaLevel", ErrNotFinite,
			func(c *Config) { c.SeaLevel = math.Inf(1) }},
		{"sea level above one", "SeaLevel", ErrOutOfRange,
			func(c *Config) { c.SeaLevel = 1.5 }},
		{"sea level below zero", "SeaLevel", ErrOutOfRange,
			func(c *Config) { c.SeaLevel = -0.001 }},

		{"continental wavelength NaN", "Continental.WavelengthMiles", ErrNotFinite,
			func(c *Config) { c.Continental.WavelengthMiles = math.NaN() }},
		{"continental wavelength zero", "Continental.WavelengthMiles", ErrNotPositive,
			func(c *Config) { c.Continental.WavelengthMiles = 0 }},
		{"continental wavelength negative", "Continental.WavelengthMiles", ErrNotPositive,
			func(c *Config) { c.Continental.WavelengthMiles = -1 }},
		{"regional wavelength below Nyquist", "Regional.WavelengthMiles", ErrBelowNyquist,
			func(c *Config) { c.Regional.WavelengthMiles = NyquistWavelengthMiles - 0.5 }},
		{"local wavelength below Nyquist", "Local.WavelengthMiles", ErrBelowNyquist,
			func(c *Config) { c.Local.WavelengthMiles = 1 }},
		{"warp wavelength zero", "Warp.WavelengthMiles", ErrNotPositive,
			func(c *Config) { c.Warp.WavelengthMiles = 0 }},

		// The ladder, which is what DESIGN.md 9.3 is actually about. A count of
		// zero is a field that evaluates to nothing; a count that descends past
		// the grid's Nyquist wavelength is per-tile noise paid for and thrown
		// away, and it is the octave count that is named because that is the
		// value somebody typed.
		{"no octaves", "Continental.Octaves", ErrOctaveCount,
			func(c *Config) { c.Continental.Octaves = 0 }},
		{"too many octaves", "Continental.Octaves", ErrOctaveCount,
			func(c *Config) { c.Continental.Octaves = MaxOctaves + 1 }},
		{"ladder below Nyquist", "Detail.Octaves", ErrBelowNyquist,
			func(c *Config) { c.Detail.Octaves = 4 }},
		{"lacunarity of one", "Regional.Lacunarity", ErrOutOfRange,
			func(c *Config) { c.Regional.Lacunarity = 1 }},
		{"lacunarity NaN", "Regional.Lacunarity", ErrNotFinite,
			func(c *Config) { c.Regional.Lacunarity = math.NaN() }},
		{"gain of zero", "Local.Gain", ErrOutOfRange,
			func(c *Config) { c.Local.Gain = 0 }},
		{"gain above one", "Local.Gain", ErrOutOfRange,
			func(c *Config) { c.Local.Gain = 1.5 }},

		{"warp strength NaN", "WarpStrengthMiles", ErrNotFinite,
			func(c *Config) { c.WarpStrengthMiles = math.NaN() }},
		{"warp strength negative", "WarpStrengthMiles", ErrNotPositive,
			func(c *Config) { c.WarpStrengthMiles = -1 }},

		{"chunk size zero", "ChunkSizeHexes", ErrNotPositive,
			func(c *Config) { c.ChunkSizeHexes = 0 }},
		{"region size zero", "RegionSizeHexes", ErrNotPositive,
			func(c *Config) { c.RegionSizeHexes = 0 }},
		{"macro region size zero", "MacroRegionSizeHexes", ErrNotPositive,
			func(c *Config) { c.MacroRegionSizeHexes = 0 }},
		{"chunk size larger than the map", "ChunkSizeHexes", ErrOutOfRange,
			func(c *Config) { c.ChunkSizeHexes = uint32(WorldRadius) + 1 }},

		{"region no larger than chunk", "RegionSizeHexes", ErrNotAscending,
			func(c *Config) { c.RegionSizeHexes = c.ChunkSizeHexes }},
		{"macro region no larger than region", "MacroRegionSizeHexes", ErrNotAscending,
			func(c *Config) { c.MacroRegionSizeHexes = c.RegionSizeHexes - 1 }},

		{"rim kind undeclared", "Rim.Kind", ErrOutOfRange,
			func(c *Config) { c.Rim.Kind = RimKind(7) }},
		{"rim wider than the map", "Rim.ClosedHexes+Rim.FalloffHexes", ErrOutOfRange,
			func(c *Config) { c.Rim.ClosedHexes = uint32(WorldRadius) }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.spoil(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate accepted the configuration")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate returned %v, want an error wrapping %v", err, tc.want)
			}
			ce, ok := errors.AsType[*ConfigError](err)
			if !ok {
				t.Fatalf("Validate returned %T, want a *ConfigError", err)
			}
			if ce.Field != tc.field {
				t.Fatalf("error names field %q, want %q", ce.Field, tc.field)
			}
			if ce.Error() == "" {
				t.Fatal("error message is empty")
			}
		})
	}
}

// TestValidConfigurations covers the settings that look wrong and are not, so a
// later tightening of the validation has to argue with a test rather than with
// a memory.
func TestValidConfigurations(t *testing.T) {
	cases := map[string]func(*Config){
		"no warp":           func(c *Config) { c.WarpStrengthMiles = 0 },
		"sea level at zero": func(c *Config) { c.SeaLevel = 0 },
		"sea level at one":  func(c *Config) { c.SeaLevel = 1 },
		"wavelength at Nyquist": func(c *Config) {
			c.Local = LadderConfig{WavelengthMiles: NyquistWavelengthMiles, Octaves: 1, Lacunarity: 2, Gain: 0.5}
		},
		"single octave": func(c *Config) { c.Detail.Octaves = 1 },
		"gain of one":   func(c *Config) { c.Detail.Gain = 1 },
		"polar ice rim": func(c *Config) { c.Rim.Kind = RimPolarIce },
		// DESIGN.md 15.1: the unrimmed world must stay a valid configuration,
		// because it is how the wrap tests see the seam they assert on.
		"no rim": func(c *Config) { c.Rim.ClosedHexes, c.Rim.FalloffHexes = 0, 0 },
		"rim exactly the radius": func(c *Config) {
			c.Rim.ClosedHexes, c.Rim.FalloffHexes = uint32(WorldRadius)-1, 1
		},
	}
	for name, adjust := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultConfig()
			adjust(&cfg)
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate rejected a legitimate configuration: %v", err)
			}
		})
	}
}

func TestRimKindStringAndValid(t *testing.T) {
	// Every declared value is reachable, named, and valid; nothing else is.
	declared := map[RimKind]string{
		RimDeepOcean: "deep-ocean",
		RimPolarIce:  "polar-ice",
	}
	for k, want := range declared {
		if !k.Valid() {
			t.Errorf("%v reports itself invalid", k)
		}
		if got := k.String(); got != want {
			t.Errorf("RimKind(%d).String() = %q, want %q", k, got, want)
		}
	}
	for v := 2; v < 256; v++ {
		k := RimKind(v)
		if k.Valid() {
			t.Errorf("RimKind(%d) reports itself valid", v)
		}
		if k.String() != "unknown" {
			t.Errorf("RimKind(%d).String() = %q, want %q", v, k.String(), "unknown")
		}
	}
}
