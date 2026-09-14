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
	// SeaLevel is where land begins, as a fraction of the elevation
	// composite's range, in (0, 1). It is what decides the land fraction, and
	// it is not a threshold on the elevation scalar: the scalar puts sea level
	// at exactly zero whatever this is set to. See Config.seaLevelRescale.
	//
	// The interval is open because zero and one each make one side of that
	// rescale a division by zero, and each names a world that is entirely one
	// thing with no scale left to measure it against.
	SeaLevel float64

	// The four continuous scales of DESIGN.md 10, coarse to fine. Each is the
	// top of an fbm ladder. A wavelength is absolute and does not scale with the
	// world radius: a continent is a continent at either radius, and what
	// changes is how many of them there are.
	//
	// The octave counts are per scale and not shared, because a single count
	// would force the shortest wavelength to run the longest one's ladder and
	// the Nyquist rule of DESIGN.md 9.3 would then bound every scale by the
	// finest. Nyquist is a validation bound here, never the mechanism that picks
	// a count.
	Continental LadderConfig
	Regional    LadderConfig
	Local       LadderConfig
	Detail      LadderConfig

	// Domain warping. Warp is the ladder the two warp fields share; a strength
	// of zero disables the warp, which is a legitimate setting rather than a
	// missing one. DESIGN.md 13.
	Warp              LadderConfig
	WarpStrengthMiles float64

	// Elevation is everything the composite of DESIGN.md 10 is made of: what
	// each continuous scale is worth, how hard the coast is sharpened, what a
	// region's biases buy, the ridge structure, and where the bands fall.
	Elevation ElevationConfig

	// The three levels of the addressing hierarchy, in hexes along either axial
	// basis direction. They are addressing devices and must never be visible in
	// the output.
	MacroRegionSizeHexes uint32
	RegionSizeHexes      uint32
	ChunkSizeHexes       uint32

	// Rim is the outer band of the map. See DESIGN.md 15.1.
	Rim RimConfig
}

// ElevationConfig is what the elevation composite of DESIGN.md 10 weighs.
//
// The weights are relative and are normalized by their own total, so scaling
// all four changes nothing and only their ratios are a decision. Everything
// else here is either a normalized amount in [0, 1] or a threshold on the
// elevation scalar, and none of it is derived from a generated sample: a
// threshold taken from the minimum and maximum of a window would make the world
// depend on what has been looked at. DESIGN.md 14 and 33.5.
type ElevationConfig struct {
	// What each continuous scale is worth in the composite. Coarse to fine, and
	// descending, because a world whose detail outweighs its continents is
	// noise with a coastline drawn on it.
	ContinentalWeight float64
	RegionalWeight    float64
	LocalWeight       float64
	DetailWeight      float64

	// ContrastPasses is how many S-curve passes sharpen the coarse half before
	// the rest of the composite is added. Zero is the identity and is a
	// legitimate setting rather than a missing one. See contrast.
	ContrastPasses uint8

	// UpliftWeight is what a region's elevation bias is worth in elevation:
	// regional uplift is this times the blended bias. The bias is normalized
	// and this is the quantity, which is the division DESIGN.md 12 asks for.
	UpliftWeight float64

	// RoughnessInfluence is how far a region's roughness bias may exaggerate or
	// subdue the fine half of the composite and the ridges. At zero every
	// region has the same relief; at one a region can double it or flatten it.
	RoughnessInfluence float64

	// Ridge is the fbm ladder the ridge structure term is folded from. Its
	// wavelength is the spacing of a mountain belt, not of a peak.
	Ridge LadderConfig

	// RidgeStrideMiles is how far the directional blur reaches along the
	// region's ridge orientation. It is what turns an isotropic web of creases
	// into belts that run with the region's grain, and zero leaves the web.
	// See Generator.ridgeStructure.
	RidgeStrideMiles float64

	// RidgeWeight is what the ridge term is worth in the composite.
	RidgeWeight float64

	// RidgeOnset is how far above sea level the ridge term reaches full
	// strength, as elevation. Below it the ridges are masked off, so a mountain
	// belt does not surface as islands in open ocean, and the mask rises
	// smoothly so the coast is not a wall.
	RidgeOnset float64

	// ReliefScale turns the mean elevation difference between neighboring tiles
	// into the normalized relief value of DESIGN.md 18. It is dimensionless:
	// relief per unit of elevation difference across one hex.
	ReliefScale float64

	// Bands are the thresholds that cut the scalar into the bands of
	// DESIGN.md 14.1.
	Bands ElevationBands
}

// MaxContrastPasses bounds the contrast ladder. It is a sanity bound rather
// than a tuning one: past a few passes the composite is a two-valued mask and
// what a fifth pass does is not a thing anybody is choosing.
const MaxContrastPasses uint8 = 4

// LadderConfig is one fbm ladder: where it starts, how many octaves it runs,
// and how the wavelength and the amplitude change between them.
//
// Octave k has an effective wavelength of WavelengthMiles/Lacunarity^k, so the
// ladder's shortest octave is WavelengthMiles/Lacunarity^(Octaves-1) and that is
// what validation compares against NyquistWavelengthMiles.
type LadderConfig struct {
	// WavelengthMiles is the coarsest octave's wavelength.
	WavelengthMiles float64

	// Octaves is how many octaves the ladder runs, at least one.
	Octaves uint8

	// Lacunarity is the factor the frequency rises by between octaves. It
	// exceeds one, or the ladder would not descend.
	Lacunarity float64

	// Gain is the factor the amplitude falls by between octaves, in (0, 1].
	Gain float64
}

// ladderFor returns the ladder and the hashing domain for one scale.
//
// The domain is what separates each scale's lattice from every other's. Sharing
// one would give two scales the same gradient table and the same seed-derived
// sampling offset, so at the world origin they would land in the same lattice
// cell at the same fractional position and return the same value. See
// DESIGN.md 8.1.
func (c Config) ladderFor(s Scale) (LadderConfig, uint64) {
	switch s {
	case ScaleContinental:
		return c.Continental, DomContinentalness
	case ScaleRegional:
		return c.Regional, DomRegionalElevation
	case ScaleLocal:
		return c.Local, DomRelief
	case ScaleDetail:
		return c.Detail, DomTerrainDetail
	default:
		panic(fmt.Sprintf("wgva: %d is not a declared scale", uint8(s)))
	}
}

// DefaultConfig returns the configuration an algorithm version ships with.
//
// These are starting values drawn from the scale table in DESIGN.md 10, not
// settled ones. Phase 7 fixes them, and from then on the written-down constant
// in config/fingerprint_test.go is what settles them: moving any default fails
// that test, and updating the constant is the compatibility decision.
func DefaultConfig() Config {
	return Config{
		// About a third of the world is land, which is the fraction DESIGN.md 4.2
		// reasons about when it settles the radius.
		SeaLevel: 0.62,

		// 1,000 hexes down to about 62. Continents and their interiors.
		Continental: LadderConfig{WavelengthMiles: 6000, Octaves: 5, Lacunarity: 2, Gain: 0.5},
		// 120 hexes down to 15. Uplift and the shape of a coast.
		Regional: LadderConfig{WavelengthMiles: 720, Octaves: 4, Lacunarity: 2, Gain: 0.5},
		// 20 hexes down to 5. Hills and valleys.
		Local: LadderConfig{WavelengthMiles: 120, Octaves: 3, Lacunarity: 2, Gain: 0.5},
		// 6 hexes down to 3, which is the finest the tile grid can carry.
		Detail: LadderConfig{WavelengthMiles: 36, Octaves: 2, Lacunarity: 2, Gain: 0.5},

		// A low-frequency warp, a fifth of its own wavelength in strength.
		Warp:              LadderConfig{WavelengthMiles: 1800, Octaves: 3, Lacunarity: 2, Gain: 0.5},
		WarpStrengthMiles: 360,

		Elevation: ElevationConfig{
			// Coarse to fine and descending steeply: the continental scale
			// decides where the water is, the regional scale shapes the coast
			// and the interior, and the two fine scales are texture on top of
			// whatever those two settled.
			ContinentalWeight: 1,
			RegionalWeight:    0.3,
			LocalWeight:       0.12,
			DetailWeight:      0.04,

			// One pass. A weighted sum of four fields is concentrated about its
			// middle, and one S-curve is enough to make a coastline a line.
			ContrastPasses: 1,

			UpliftWeight:       0.25,
			RoughnessInfluence: 0.5,

			// 60 hexes down to about 8: the spacing of a belt, not of a peak.
			Ridge:            LadderConfig{WavelengthMiles: 360, Octaves: 4, Lacunarity: 2, Gain: 0.5},
			RidgeStrideMiles: 45,
			RidgeWeight:      0.15,
			RidgeOnset:       0.25,

			ReliefScale: 18,

			Bands: ElevationBands{
				DeepWater: -0.15,
				Upland:    0.25,
				Highland:  0.5,
				Mountain:  0.75,
			},
		},

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
	if err := checkOpenUnit("SeaLevel", c.SeaLevel); err != nil {
		return err
	}

	for _, f := range []struct {
		name   string
		ladder LadderConfig
	}{
		{"Continental", c.Continental},
		{"Regional", c.Regional},
		{"Local", c.Local},
		{"Detail", c.Detail},
		{"Warp", c.Warp},
		{"Elevation.Ridge", c.Elevation.Ridge},
	} {
		if err := f.ladder.validate(f.name); err != nil {
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

	if err := c.Elevation.validate(); err != nil {
		return err
	}

	return c.Rim.validate()
}

// validate reports the first reason the elevation configuration cannot be used.
//
// The ridge ladder is checked with the other ladders in Config.Validate, so
// what is left here is the weights, the amounts, and the band ladder.
func (ec ElevationConfig) validate() error {
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Elevation.ContinentalWeight", ec.ContinentalWeight},
		{"Elevation.RegionalWeight", ec.RegionalWeight},
		{"Elevation.LocalWeight", ec.LocalWeight},
		{"Elevation.DetailWeight", ec.DetailWeight},
	} {
		if !isFinite(f.value) {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotFinite}
		}
		// Each weight is positive rather than merely non-negative. A weight of
		// zero silences a whole scale, and a scale that contributes nothing is
		// an octave ladder still being evaluated for every tile in the world;
		// the setting that means it is the wavelength, not this.
		if f.value <= 0 {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotPositive}
		}
	}

	if ec.ContrastPasses > MaxContrastPasses {
		return &ConfigError{
			Field: "Elevation.ContrastPasses", Value: float64(ec.ContrastPasses),
			Lo: 0, Hi: float64(MaxContrastPasses), Err: ErrPassCount,
		}
	}

	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Elevation.UpliftWeight", ec.UpliftWeight},
		{"Elevation.RoughnessInfluence", ec.RoughnessInfluence},
		{"Elevation.RidgeWeight", ec.RidgeWeight},
	} {
		if err := checkUnit(f.name, f.value); err != nil {
			return err
		}
	}

	// A stride of zero is the isotropic web, which is a legitimate setting
	// rather than a missing one, so this is bounded below by zero rather than
	// by positivity.
	if !isFinite(ec.RidgeStrideMiles) {
		return &ConfigError{Field: "Elevation.RidgeStrideMiles", Value: ec.RidgeStrideMiles, Err: ErrNotFinite}
	}
	if ec.RidgeStrideMiles < 0 {
		return &ConfigError{Field: "Elevation.RidgeStrideMiles", Value: ec.RidgeStrideMiles, Err: ErrNotPositive}
	}

	// The onset is a divisor, so zero is refused rather than clamped.
	if err := checkUnit("Elevation.RidgeOnset", ec.RidgeOnset); err != nil {
		return err
	}
	if ec.RidgeOnset == 0 {
		return &ConfigError{Field: "Elevation.RidgeOnset", Value: 0, Err: ErrNotPositive}
	}

	if !isFinite(ec.ReliefScale) {
		return &ConfigError{Field: "Elevation.ReliefScale", Value: ec.ReliefScale, Err: ErrNotFinite}
	}
	if ec.ReliefScale <= 0 {
		return &ConfigError{Field: "Elevation.ReliefScale", Value: ec.ReliefScale, Err: ErrNotPositive}
	}

	return ec.Bands.validate()
}

// validate reports the first reason the band ladder cannot be used.
//
// The deep-water threshold is strictly below sea level and the three land
// thresholds strictly ascend above it. An out-of-order threshold is a band that
// can never be reached, which is evaluable and wrong — the defect
// ErrNotAscending exists for.
func (b ElevationBands) validate() error {
	if !isFinite(b.DeepWater) {
		return &ConfigError{Field: "Elevation.Bands.DeepWater", Value: b.DeepWater, Err: ErrNotFinite}
	}
	if b.DeepWater < -1 || b.DeepWater >= 0 {
		return &ConfigError{
			Field: "Elevation.Bands.DeepWater", Value: b.DeepWater,
			Lo: -1, Hi: 0, Err: ErrOutOfRange,
		}
	}

	ladder := []struct {
		name  string
		value float64
		below string
		floor float64
	}{
		{"Elevation.Bands.Upland", b.Upland, "sea level", 0},
		{"Elevation.Bands.Highland", b.Highland, "Elevation.Bands.Upland", b.Upland},
		{"Elevation.Bands.Mountain", b.Mountain, "Elevation.Bands.Highland", b.Highland},
	}
	for _, f := range ladder {
		if !isFinite(f.value) {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotFinite}
		}
		if f.value > 1 {
			return &ConfigError{Field: f.name, Value: f.value, Lo: 0, Hi: 1, Err: ErrOutOfRange}
		}
		if f.value <= f.floor {
			return &ConfigError{Field: f.name, Value: f.value, Below: f.below, Err: ErrNotAscending}
		}
	}
	return nil
}

// validate reports the first reason the ladder cannot be used, as a
// *ConfigError naming the field under the given prefix.
func (l LadderConfig) validate(prefix string) error {
	if err := checkWavelength(prefix+".WavelengthMiles", l.WavelengthMiles); err != nil {
		return err
	}
	if l.Octaves < 1 || l.Octaves > MaxOctaves {
		return &ConfigError{
			Field: prefix + ".Octaves", Value: float64(l.Octaves),
			Lo: 1, Hi: float64(MaxOctaves), Err: ErrOctaveCount,
		}
	}
	if err := checkLacunarity(prefix+".Lacunarity", l.Lacunarity); err != nil {
		return err
	}
	if err := checkGain(prefix+".Gain", l.Gain); err != nil {
		return err
	}
	return checkLadder(prefix, l.WavelengthMiles, l.Octaves, l.Lacunarity)
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

// checkOpenUnit rejects a normalized value outside the open interval (0, 1).
// It is for a value that divides something, where an endpoint is not a
// degenerate setting but an unevaluable one.
func checkOpenUnit(field string, v float64) error {
	if err := checkUnit(field, v); err != nil {
		return err
	}
	if v == 0 || v == 1 {
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

// checkLacunarity rejects a frequency ratio that does not descend. A lacunarity
// of one is a ladder whose octaves are all the same scale, which is evaluable
// and wrong.
func checkLacunarity(field string, v float64) error {
	if !isFinite(v) {
		return &ConfigError{Field: field, Value: v, Err: ErrNotFinite}
	}
	if v <= 1 || v > maxLacunarity {
		return &ConfigError{Field: field, Value: v, Lo: 1, Hi: maxLacunarity, Err: ErrOutOfRange}
	}
	return nil
}

// checkGain rejects an amplitude ratio outside (0, 1]. A gain above one makes
// the finest octave the loudest, which inverts what an fbm ladder is for; a gain
// of zero silences every octave but the first.
func checkGain(field string, v float64) error {
	if !isFinite(v) {
		return &ConfigError{Field: field, Value: v, Err: ErrNotFinite}
	}
	if v <= 0 || v > 1 {
		return &ConfigError{Field: field, Value: v, Lo: 0, Hi: 1, Err: ErrOutOfRange}
	}
	return nil
}

// checkLadder rejects an octave ladder that descends below the shortest feature
// the tile grid can carry. See DESIGN.md 9.3.
//
// The shortest octave is wavelength/lacunarity^(octaves-1). It is computed by
// repeated division rather than by math.Pow, which is not one of the operations
// DESIGN.md 25.2 permits and would be a bad habit to form even here, where this
// runs once at construction and never in the generation path.
//
// This is a validation bound and never the mechanism that picks a count.
// Deriving each count by truncating at the limit would make it a step function
// of a float, so retuning a wavelength by a tenth of a mile would silently flip
// a count and move every value in the world.
func checkLadder(prefix string, wavelengthMiles float64, octaves uint8, lacunarity float64) error {
	shortest := wavelengthMiles
	for range octaves - 1 {
		shortest /= lacunarity
	}
	if shortest < NyquistWavelengthMiles {
		return &ConfigError{
			Field: prefix + ".Octaves", Value: shortest,
			Lo: NyquistWavelengthMiles, Err: ErrBelowNyquist,
		}
	}
	return nil
}

// maxLacunarity bounds the frequency ratio between octaves. A ratio this large
// skips whole scales, so a value beyond it is far likelier to be a typo than an
// intention.
const maxLacunarity = 8.0

// isFinite reports whether v is neither NaN nor an infinity. It is a comparison
// rather than a call to math.IsInf and math.IsNaN together because that is what
// it is.
func isFinite(v float64) bool {
	return v == v && !math.IsInf(v, 0)
}
