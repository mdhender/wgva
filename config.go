// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

import (
	"errors"
	"fmt"
	"math"
)

// The configuration error model. Each reason is a sentinel so that a test can
// assert with errors.Is rather than on a message string: a test that compares an
// error's Error() string is a test that will pass while the validation is wrong.
// See DESIGN.md 19.1.
var (
	// ErrNotFinite is returned for a NaN or an infinity. NaN is rejected here so
	// that it can never reach the configuration fingerprint.
	ErrNotFinite = errors.New("value must be finite")

	// ErrNotPositive is returned for a scale, size, or wavelength of zero or
	// less.
	ErrNotPositive = errors.New("value must be positive")

	// ErrOutOfRange is returned for a value outside its documented bounds, such
	// as a normalized threshold outside [0, 1].
	ErrOutOfRange = errors.New("value out of range")

	// ErrOctaveCount is returned for an fbm octave count outside its bounds.
	// Reached once fields exist; see DESIGN.md 9.
	ErrOctaveCount = errors.New("octave count out of range")

	// ErrBelowNyquist is returned for a wavelength, or an fbm ladder that
	// descends to one, shorter than the tile grid can carry. Such a scale is
	// evaluable and wrong rather than unevaluable, which is why it is validated
	// rather than clamped. See DESIGN.md 9.3.
	ErrBelowNyquist = errors.New("octave ladder reaches below the Nyquist wavelength")

	// ErrPassCount is returned for a contrast pass count outside its bounds.
	// Reached once the elevation composite exists.
	ErrPassCount = errors.New("contrast pass count out of range")

	// ErrNotAscending is returned for an ordered ladder whose entries do not
	// ascend. An out-of-order threshold is a band that can never be reached,
	// which is the same shape of defect as ErrBelowNyquist.
	ErrNotAscending = errors.New("band thresholds must ascend")

	// ErrFieldShape is returned for a field node with values set for another
	// kind. Reached once fields exist; see DESIGN.md 9.1.
	ErrFieldShape = errors.New("field node has values set for another kind")
)

// ConfigError names the configuration field that failed and wraps the reason.
type ConfigError struct {
	Field string
	Value float64
	Lo    float64
	Hi    float64
	Below string // for ErrNotAscending: the entry this one must exceed
	Err   error
}

// Error renders the field, the reason, and whatever bound the reason has.
func (e *ConfigError) Error() string {
	switch {
	case errors.Is(e.Err, ErrNotAscending):
		return fmt.Sprintf("%s: %v: %g does not exceed %s", e.Field, e.Err, e.Value, e.Below)
	case errors.Is(e.Err, ErrBelowNyquist):
		return fmt.Sprintf("%s: %v: %g is shorter than %g miles", e.Field, e.Err, e.Value, e.Lo)
	case e.Hi > e.Lo:
		return fmt.Sprintf("%s: %v: %g is outside [%g, %g]", e.Field, e.Err, e.Value, e.Lo, e.Hi)
	default:
		return fmt.Sprintf("%s: %v: %g", e.Field, e.Err, e.Value)
	}
}

// Unwrap returns the sentinel reason.
func (e *ConfigError) Unwrap() error { return e.Err }

// NyquistWavelengthMiles is the shortest feature the tile grid can carry.
// Adjacent tile centers are 2*apothem apart, so the limit is 4*apothem. It is
// derived from the apothem rather than written as a literal. See DESIGN.md 9.3.
const NyquistWavelengthMiles = 4 * hexApothemMiles

// Config is the complete set of numbers that decide how a world looks. It is
// immutable after construction, and the complete effective configuration —
// including every defaulted value — is what a world file stores and what the
// fingerprint of DESIGN.md 21.2 covers.
//
// Every weight, scale, threshold, and feature toggle that can alter generated
// output belongs here and nowhere else. Scale fields name their unit in the
// identifier.
//
// Config gains fields as the later phases land. Adding one is an algorithm
// version change even when it moves no generated value, because DESIGN.md 21.1
// forbids defaulting a missing generation-affecting field, so a world file
// written under the older version genuinely cannot be reopened.
type Config struct {
	// SeaLevel is the normalized elevation at which land begins, in [0, 1].
	SeaLevel float64

	// The base wavelengths of the elevation scales, in miles. Each is the top of
	// an fbm ladder once fields exist. A wavelength is absolute and does not
	// scale with the world radius: a continent is a continent at either radius,
	// and what changes is how many of them there are.
	ContinentalWavelengthMiles float64
	RegionalWavelengthMiles    float64
	LocalWavelengthMiles       float64

	// Domain warping. A strength of zero disables the warp, which is a
	// legitimate setting rather than a missing one.
	WarpWavelengthMiles float64
	WarpStrengthMiles   float64

	// The three levels of the addressing hierarchy, in hexes along either axial
	// basis direction. They are addressing devices and must never be visible in
	// the output.
	MacroRegionSizeHexes uint32
	RegionSizeHexes      uint32
	ChunkSizeHexes       uint32

	// Rim is the outer band of the map. See DESIGN.md 15.1.
	Rim RimConfig
}

// DefaultConfig returns the configuration an algorithm version ships with.
//
// These are starting values drawn from the scale table in DESIGN.md 10, not
// settled ones. Phase 7 fixes them, and from then on the written-down constant
// in config/fingerprint_test.go is what settles them: moving any default fails
// that test, and updating the constant is the compatibility decision.
func DefaultConfig() Config {
	return Config{
		SeaLevel: 0.5,

		ContinentalWavelengthMiles: 6000, // 1,000 hexes
		RegionalWavelengthMiles:    720,  // 120 hexes
		LocalWavelengthMiles:       48,   // 8 hexes

		WarpWavelengthMiles: 1800, // 300 hexes
		WarpStrengthMiles:   120,  // 20 hexes

		MacroRegionSizeHexes: 512,
		RegionSizeHexes:      128,
		ChunkSizeHexes:       32,

		Rim: RimConfig{
			ClosedHexes:  24,
			FalloffHexes: 96,
			Kind:         RimDeepOcean,
		},
	}
}

// Validate reports the first reason the configuration cannot be used, as a
// *ConfigError naming the field.
//
// It rejects non-finite floats, non-positive scales and sizes, normalized
// thresholds outside their range, wavelengths the tile grid cannot carry, a
// hierarchy whose levels do not ascend, and a rim wider than the map.
func (c Config) Validate() error {
	if err := checkUnit("SeaLevel", c.SeaLevel); err != nil {
		return err
	}

	for _, f := range []struct {
		name  string
		value float64
	}{
		{"ContinentalWavelengthMiles", c.ContinentalWavelengthMiles},
		{"RegionalWavelengthMiles", c.RegionalWavelengthMiles},
		{"LocalWavelengthMiles", c.LocalWavelengthMiles},
		{"WarpWavelengthMiles", c.WarpWavelengthMiles},
	} {
		if err := checkWavelength(f.name, f.value); err != nil {
			return err
		}
	}

	// A warp strength of zero disables warping, so it is bounded below by zero
	// rather than by positivity.
	if !isFinite(c.WarpStrengthMiles) {
		return &ConfigError{Field: "WarpStrengthMiles", Value: c.WarpStrengthMiles, Err: ErrNotFinite}
	}
	if c.WarpStrengthMiles < 0 {
		return &ConfigError{Field: "WarpStrengthMiles", Value: c.WarpStrengthMiles, Err: ErrNotPositive}
	}

	for _, f := range []struct {
		name  string
		value uint32
	}{
		{"MacroRegionSizeHexes", c.MacroRegionSizeHexes},
		{"RegionSizeHexes", c.RegionSizeHexes},
		{"ChunkSizeHexes", c.ChunkSizeHexes},
	} {
		if f.value == 0 {
			return &ConfigError{Field: f.name, Value: 0, Err: ErrNotPositive}
		}
		if int64(f.value) > WorldRadius {
			return &ConfigError{
				Field: f.name, Value: float64(f.value),
				Lo: 1, Hi: float64(WorldRadius), Err: ErrOutOfRange,
			}
		}
	}

	// The hierarchy is chunk inside region inside macro region. A level equal to
	// the one below it is a level that does nothing, so the ascent is strict.
	if c.RegionSizeHexes <= c.ChunkSizeHexes {
		return &ConfigError{
			Field: "RegionSizeHexes", Value: float64(c.RegionSizeHexes),
			Below: "ChunkSizeHexes", Err: ErrNotAscending,
		}
	}
	if c.MacroRegionSizeHexes <= c.RegionSizeHexes {
		return &ConfigError{
			Field: "MacroRegionSizeHexes", Value: float64(c.MacroRegionSizeHexes),
			Below: "RegionSizeHexes", Err: ErrNotAscending,
		}
	}

	return c.Rim.validate()
}

func (rc RimConfig) validate() error {
	if !rc.Kind.Valid() {
		return &ConfigError{
			Field: "Rim.Kind", Value: float64(rc.Kind),
			Lo: float64(RimDeepOcean), Hi: float64(RimPolarIce), Err: ErrOutOfRange,
		}
	}
	// The two bands together must fit inside the map, or the forced terrain
	// would reach the origin and there would be no world left to generate.
	total := int64(rc.ClosedHexes) + int64(rc.FalloffHexes)
	if total > WorldRadius {
		return &ConfigError{
			Field: "Rim.ClosedHexes+Rim.FalloffHexes", Value: float64(total),
			Lo: 0, Hi: float64(WorldRadius), Err: ErrOutOfRange,
		}
	}
	return nil
}

func checkUnit(field string, v float64) error {
	if !isFinite(v) {
		return &ConfigError{Field: field, Value: v, Err: ErrNotFinite}
	}
	if v < 0 || v > 1 {
		return &ConfigError{Field: field, Value: v, Lo: 0, Hi: 1, Err: ErrOutOfRange}
	}
	return nil
}

func checkWavelength(field string, v float64) error {
	if !isFinite(v) {
		return &ConfigError{Field: field, Value: v, Err: ErrNotFinite}
	}
	if v <= 0 {
		return &ConfigError{Field: field, Value: v, Err: ErrNotPositive}
	}
	if v < NyquistWavelengthMiles {
		return &ConfigError{
			Field: field, Value: v,
			Lo: NyquistWavelengthMiles, Err: ErrBelowNyquist,
		}
	}
	return nil
}

// isFinite reports whether v is neither NaN nor an infinity. It is a comparison
// rather than a call to math.IsInf and math.IsNaN together because that is what
// it is.
func isFinite(v float64) bool {
	return v == v && !math.IsInf(v, 0)
}
