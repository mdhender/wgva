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
		// The interval is open, and these two are the reason. Each makes one
		// side of the sea-level rescale a division by zero, and each names a
		// world that is entirely one thing with no scale left to measure it
		// against. This is the tightening TestValidConfigurations used to
		// forbid, and the argument is here rather than in a memory.
		{"sea level at zero", "SeaLevel", ErrOutOfRange,
			func(c *Config) { c.SeaLevel = 0 }},
		{"sea level at one", "SeaLevel", ErrOutOfRange,
			func(c *Config) { c.SeaLevel = 1 }},

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
		{"rim floor off the scale", "Rim.FloorElevation", ErrOutOfRange,
			func(c *Config) { c.Rim.FloorElevation = -1.5 }},
		{"rim floor not finite", "Rim.FloorElevation", ErrNotFinite,
			func(c *Config) { c.Rim.FloorElevation = math.Inf(-1) }},

		// The elevation composite. Every weight is positive, the amounts are
		// normalized, and the band ladder ascends from below sea level.
		{"a silenced scale", "Elevation.LocalWeight", ErrNotPositive,
			func(c *Config) { c.Elevation.LocalWeight = 0 }},
		{"a negative weight", "Elevation.ContinentalWeight", ErrNotPositive,
			func(c *Config) { c.Elevation.ContinentalWeight = -1 }},
		{"a weight that is not finite", "Elevation.DetailWeight", ErrNotFinite,
			func(c *Config) { c.Elevation.DetailWeight = math.Inf(1) }},
		{"too many contrast passes", "Elevation.ContrastPasses", ErrPassCount,
			func(c *Config) { c.Elevation.ContrastPasses = MaxContrastPasses + 1 }},
		{"uplift above one", "Elevation.UpliftWeight", ErrOutOfRange,
			func(c *Config) { c.Elevation.UpliftWeight = 1.5 }},
		{"roughness influence below zero", "Elevation.RoughnessInfluence", ErrOutOfRange,
			func(c *Config) { c.Elevation.RoughnessInfluence = -0.1 }},
		{"a negative ridge stride", "Elevation.RidgeStrideMiles", ErrNotPositive,
			func(c *Config) { c.Elevation.RidgeStrideMiles = -1 }},
		// The onset divides, so zero is refused rather than clamped.
		{"a ridge onset of zero", "Elevation.RidgeOnset", ErrNotPositive,
			func(c *Config) { c.Elevation.RidgeOnset = 0 }},
		{"a relief scale of zero", "Elevation.ReliefScale", ErrNotPositive,
			func(c *Config) { c.Elevation.ReliefScale = 0 }},
		// Deep water lies below sea level, which is zero. At or above it the
		// band could never be reached from the water side.
		{"deep water at sea level", "Elevation.Bands.DeepWater", ErrOutOfRange,
			func(c *Config) { c.Elevation.Bands.DeepWater = 0 }},
		{"deep water below the floor", "Elevation.Bands.DeepWater", ErrOutOfRange,
			func(c *Config) { c.Elevation.Bands.DeepWater = -1.5 }},
		{"upland at sea level", "Elevation.Bands.Upland", ErrNotAscending,
			func(c *Config) { c.Elevation.Bands.Upland = 0 }},
		{"highland below upland", "Elevation.Bands.Highland", ErrNotAscending,
			func(c *Config) { c.Elevation.Bands.Highland = 0.1 }},
		{"mountain equal to highland", "Elevation.Bands.Mountain", ErrNotAscending,
			func(c *Config) { c.Elevation.Bands.Mountain = c.Elevation.Bands.Highland }},
		{"a band above the ceiling", "Elevation.Bands.Mountain", ErrOutOfRange,
			func(c *Config) { c.Elevation.Bands.Mountain = 1.5 }},
		{"a ridge ladder below Nyquist", "Elevation.Ridge.WavelengthMiles", ErrBelowNyquist,
			func(c *Config) { c.Elevation.Ridge.WavelengthMiles = 1 }},

		// The two climate axes. Every weight is positive, the cooling is
		// normalized, and each band ladder ascends strictly inside (-1, +1).
		{"a heat ladder below Nyquist", "Climate.Heat.WavelengthMiles", ErrBelowNyquist,
			func(c *Config) { c.Climate.Heat.WavelengthMiles = 1 }},
		{"a moisture variation ladder with no octaves", "Climate.MoistureVariation.Octaves", ErrOctaveCount,
			func(c *Config) { c.Climate.MoistureVariation.Octaves = 0 }},
		{"a silenced heat field", "Climate.HeatFieldWeight", ErrNotPositive,
			func(c *Config) { c.Climate.HeatFieldWeight = 0 }},
		{"a silenced moisture variation", "Climate.MoistureVariationWeight", ErrNotPositive,
			func(c *Config) { c.Climate.MoistureVariationWeight = 0 }},
		{"a heat bias weight that is not finite", "Climate.HeatBiasWeight", ErrNotFinite,
			func(c *Config) { c.Climate.HeatBiasWeight = math.Inf(1) }},
		{"cooling above one", "Climate.ElevationCooling", ErrOutOfRange,
			func(c *Config) { c.Climate.ElevationCooling = 1.5 }},
		{"cooling below zero", "Climate.ElevationCooling", ErrOutOfRange,
			func(c *Config) { c.Climate.ElevationCooling = -0.1 }},
		{"too many heat contrast passes", "Climate.HeatContrastPasses", ErrPassCount,
			func(c *Config) { c.Climate.HeatContrastPasses = MaxContrastPasses + 1 }},
		{"too many moisture contrast passes", "Climate.MoistureContrastPasses", ErrPassCount,
			func(c *Config) { c.Climate.MoistureContrastPasses = MaxContrastPasses + 1 }},
		// The scalar is clamped to [-1, +1], so a threshold at either end is a
		// band reachable only by a value that saturated, or not at all.
		{"a heat band at the floor", "Climate.HeatBands.Polar", ErrNotAscending,
			func(c *Config) { c.Climate.HeatBands.Polar = -1 }},
		{"a heat band at the ceiling", "Climate.HeatBands.Warm", ErrOutOfRange,
			func(c *Config) { c.Climate.HeatBands.Warm = 1 }},
		{"heat bands out of order", "Climate.HeatBands.Temperate", ErrNotAscending,
			func(c *Config) { c.Climate.HeatBands.Temperate = c.Climate.HeatBands.Cold }},
		{"a heat band that is not finite", "Climate.HeatBands.Cold", ErrNotFinite,
			func(c *Config) { c.Climate.HeatBands.Cold = math.NaN() }},
		{"moisture bands out of order", "Climate.MoistureBands.Humid", ErrNotAscending,
			func(c *Config) { c.Climate.MoistureBands.Humid = c.Climate.MoistureBands.Moderate }},
		{"a moisture band above the ceiling", "Climate.MoistureBands.Arid", ErrOutOfRange,
			func(c *Config) { c.Climate.MoistureBands.Arid = 1.5 }},
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
		"no warp": func(c *Config) { c.WarpStrengthMiles = 0 },
		"wavelength at Nyquist": func(c *Config) {
			c.Local = LadderConfig{WavelengthMiles: NyquistWavelengthMiles, Octaves: 1, Lacunarity: 2, Gain: 0.5}
		},
		"single octave": func(c *Config) { c.Detail.Octaves = 1 },
		// Zero passes is the identity, which is a legitimate composite rather
		// than a missing setting.
		"no contrast": func(c *Config) { c.Elevation.ContrastPasses = 0 },
		// A stride of zero is the isotropic web of creases, which is a
		// legitimate ridge structure rather than a missing one.
		"no ridge stride": func(c *Config) { c.Elevation.RidgeStrideMiles = 0 },
		// And a ridge term worth nothing is a world without mountain belts,
		// which is a choice.
		"no ridges":   func(c *Config) { c.Elevation.RidgeWeight = 0 },
		"gain of one": func(c *Config) { c.Detail.Gain = 1 },
		// A world with no lapse rate is a world whose mountains are as warm as
		// the ground around them, which is a choice rather than an omission.
		"no elevation cooling": func(c *Config) { c.Climate.ElevationCooling = 0 },
		// And zero passes is the identity on either climate axis, the same way
		// it is on the elevation composite.
		"no climate contrast": func(c *Config) {
			c.Climate.HeatContrastPasses, c.Climate.MoistureContrastPasses = 0, 0
		},
		// An ice rim wants a floor above sea level, and nothing couples the two:
		// the pairing is the configuration's to get right, and an ice shelf over
		// water is an ordinary thing to ask for.
		"polar ice rim": func(c *Config) { c.Rim.Kind, c.Rim.FloorElevation = RimPolarIce, 0.6 },
		// The floor is a value rather than a threshold, so both ends of the
		// scale are legitimate settings: -1 is the bottom of the ocean and +1 is
		// an icefield at the top of the range.
		"rim floor at the top of the scale": func(c *Config) { c.Rim.FloorElevation = 1 },
		"rim floor at sea level":            func(c *Config) { c.Rim.FloorElevation = 0 },
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
