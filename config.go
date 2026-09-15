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

	// Climate is the two axes of DESIGN.md 16: the broad heat zones and what
	// altitude takes off them, the broad moisture field and its local
	// variation, what a region's climate biases are worth in each, and where
	// the two band ladders fall.
	Climate ClimateConfig

	// Basin is the enclosed low ground of DESIGN.md 17: three scales of it,
	// what the region's basin bias is worth against them, and what the whole
	// blend is worth as a multiplier on moisture.
	//
	// It is its own group rather than part of Terrain because of where it does
	// *not* appear. Basin influence never enters elevation, and a reader who
	// found these keys under the elevation composite would reasonably conclude
	// the opposite; see DESIGN.md 17.1.
	Basin BasinConfig

	// Terrain is what the classifier of DESIGN.md 17 reads: the volcanic
	// tendency field and its two thresholds, and the elevation, relief, heat,
	// and wetness thresholds each ordered rule is cut at.
	Terrain TerrainConfig

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

// ClimateConfig is what the climate composite of DESIGN.md 16 weighs.
//
// The two axes are configured apart because they are independent: nothing here
// lets a moisture setting move a temperature, and a type that mixed them would
// be the single combined climate value DESIGN.md 16.1 forbids. What they share
// is their shape — a weighted average of fields and region biases, a contrast
// pass to decide how much of [-1, +1] the world actually uses, and a ladder of
// four thresholds cutting the result into five bands.
//
// Each group's weights are relative and are normalized by their own total, so
// scaling a group changes nothing and only the ratios within it are a decision.
//
// There is no latitude here and no field that stands in for one. The wrapped
// world has no equator, so the broad zones are a field of their own; see
// DESIGN.md 16 and Generator.climateAt.
type ClimateConfig struct {
	// Heat is the fbm ladder the broad heat zones are drawn from. Its
	// wavelength is the width of a climate zone, and it is the coarsest scale
	// in the world for a reason: zones that are finer than continents read as
	// weather rather than as climate.
	Heat LadderConfig

	// HeatFieldWeight and HeatBiasWeight are what the broad field and the
	// region's heat bias are worth against each other.
	HeatFieldWeight float64
	HeatBiasWeight  float64

	// ElevationCooling is how much heat the highest ground loses, as heat per
	// unit of elevation above sea level. It reads max(elevation, 0), so it
	// cools land and leaves the ocean surface alone; see Generator.climateAt.
	ElevationCooling float64

	// HeatContrastPasses is how many S-curve passes spread the heat base before
	// the cooling is taken off it. Zero is the identity and is a legitimate
	// setting rather than a missing one, but a weighted average of fields that
	// are each concentrated about zero is a world that is temperate almost
	// everywhere. See contrast.
	HeatContrastPasses uint8

	// HeatBands are the thresholds that cut the heat scalar into the bands of
	// DESIGN.md 16.1.
	HeatBands HeatBands

	// Moisture is the fbm ladder the broad moisture field is drawn from, and
	// MoistureVariation is the local departure from it. The two are separate
	// ladders under separate hashing domains because they are separate scales:
	// one is where the wet part of the world is and the other is why two
	// neighboring valleys differ.
	Moisture          LadderConfig
	MoistureVariation LadderConfig

	// What the broad field, the region's moisture bias, and the local variation
	// are worth against each other. The variation weight is the one to watch
	// when a moisture map looks like speckle rather than like geography.
	MoistureFieldWeight     float64
	MoistureBiasWeight      float64
	MoistureVariationWeight float64

	// MoistureContrastPasses is how many S-curve passes spread the moisture
	// base. See HeatContrastPasses.
	MoistureContrastPasses uint8

	// MoistureBands are the thresholds that cut the moisture scalar into the
	// bands of DESIGN.md 16.1.
	MoistureBands MoistureBands
}

// BasinConfig is what the basin composite of DESIGN.md 17 weighs.
//
// The four weights are relative and are normalized by their own total, so
// scaling all four changes nothing and only their ratios are a decision. What
// is not one of them is MoistureWeight, which is an amount rather than a
// weight: it is how strongly the blended basin multiplies the moisture terrain
// reads, and it is the one dial that decides whether basins are geography or
// scenery.
type BasinConfig struct {
	// The three scales of enclosed low ground, coarse to fine. Broad is the
	// span of a continental interior that drains nowhere, Regional is a single
	// basin, and Local is the floor of one.
	//
	// Each has its own hashing domain, so no two of them share a lattice. Three
	// nodes under one domain would share a gradient table and a sampling
	// offset, so at the world origin all three would return the same value and
	// the composite would be one field counted three times.
	Broad    LadderConfig
	Regional LadderConfig
	Local    LadderConfig

	// What each scale and the region's basin bias are worth against each other.
	BroadWeight    float64
	RegionalWeight float64
	LocalWeight    float64
	BiasWeight     float64

	// MoistureWeight is how far a basin may move the moisture terrain is
	// classified from: wetness is moisture + MoistureWeight*basin*moisture.
	//
	// It multiplies rather than adds, which is what makes a basin deepen
	// whatever climate it is in instead of making every basin wet. At zero,
	// terrain reads the climate's moisture unchanged and the basin fields are
	// geography nothing consumes — which is a legitimate setting and is what a
	// bisecting tuner sets it to.
	MoistureWeight float64
}

// TerrainConfig is where the ordered rules of DESIGN.md 17 are cut.
//
// Every value here is a threshold on a scalar the generator already produces,
// and none of it is derived from a generated sample: a threshold taken from the
// minimum and maximum of a window would make the world depend on what has been
// looked at. DESIGN.md 33.5.
//
// The fields are grouped by the rule that reads them, in the order the rules
// run, so the struct and Config.classifyTerrain can be read side by side.
type TerrainConfig struct {
	// The two depth thresholds that cut ocean water into deep ocean, ocean, and
	// shallow sea. Both are negative, because zero is sea level, and they are
	// separate from ElevationBands.DeepWater on purpose: the elevation band is
	// what a game reads as "how deep is this", and moving one of these must not
	// move that.
	//
	// Coastal water has no threshold. It is water with a land neighbor, which
	// is a fact about the six tiles around it rather than about its depth.
	DeepOceanDepth float64
	OceanDepth     float64

	// IceHeat is the heat at or below which land is under permanent ice, and
	// AlpineHeat is the heat at or below which a mountain is alpine rather than
	// bare rock. The first is above the elevated rules and the second inside
	// them, so an icecap on a mountain is ice and alpine terrain is the cold
	// mountain that is not under one.
	IceHeat    float64
	AlpineHeat float64

	// Volcanic is the fbm ladder the volcanic tendency is drawn from. Its
	// wavelength is the span of a volcanic province rather than of a cone; see
	// Generator.volcanicAt.
	Volcanic LadderConfig

	// VolcanicFieldWeight and VolcanicBiasWeight are what the field and the
	// region's volcanic bias are worth against each other.
	VolcanicFieldWeight float64
	VolcanicBiasWeight  float64

	// VolcanicElevation is the uplift volcanic terrain needs: below it the
	// tendency produces nothing, however strong it is. A volcanic province
	// under the sea is a real thing and it is not a thing this vocabulary has a
	// word for.
	VolcanicElevation float64

	// VolcanoThreshold is the tendency a cone needs and VolcanoRelief is the
	// concentrated relief it needs beside it; VolcanicHighlandThreshold is the
	// lower tendency that makes the ground around one volcanic highland.
	//
	// The first two together are what make a volcano rare without anything
	// being rolled for: three conditions on three continuous fields, which is
	// the distinction DESIGN.md 33.1 draws between a classification and a
	// per-tile draw.
	VolcanoThreshold          float64
	VolcanoRelief             float64
	VolcanicHighlandThreshold float64

	// HillsRelief is the steepness that makes ground hills below the highland
	// band. The highland band itself is hills whatever its relief.
	HillsRelief float64

	// The three conditions a wetland needs, all at once: wet enough, low
	// enough, and flat enough. Water stands where it is neither drained by a
	// slope nor run off the edge of a highland, so a rule that read only the
	// moisture would put a swamp on a hillside.
	//
	// WetlandWetness is read against the wetness of DESIGN.md 17.1 — moisture
	// after the basin product — which is what makes a wet basin read as marsh,
	// swamp, or bog, and is the whole of what basin geography ships as.
	WetlandWetness   float64
	WetlandElevation float64
	WetlandRelief    float64

	// BogHeat and SwampHeat divide the wetlands by climate, which is what
	// DESIGN.md 17 distinguishes them by: a bog is the cold one, a swamp the
	// warm one, and a marsh the open saturated lowland in between.
	BogHeat   float64
	SwampHeat float64

	// BadlandsRelief is the steepness that makes a desert badlands. It is the
	// one exception inside the climate cover table, and it is there because the
	// dry family's own evidence in DESIGN.md 17 is "low moisture, heat, exposed
	// relief".
	BadlandsRelief float64
}

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
// **These are settled.** The written-down fingerprint constant in
// config/fingerprint_test.go is what settles them, so moving any default fails
// that test, and updating the constant is the compatibility decision — it
// belongs in a commit message beside the AlgorithmVersion bump that goes with
// it. Appendices D.11, D.13, D.15, and D.17 record what each group was tuned
// to and what moving one costs.
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

		Climate: ClimateConfig{
			// 2000 hexes down to 500. Twice the continental wavelength, so a
			// climate zone is larger than a continent rather than a feature of
			// one — which is what a latitude band would be if the world had a
			// latitude, and is why the first version needs none.
			Heat:            LadderConfig{WavelengthMiles: 12000, Octaves: 3, Lacunarity: 2, Gain: 0.5},
			HeatFieldWeight: 1,
			HeatBiasWeight:  0.35,

			// A tile at the top of the scale is a whole band colder than the
			// same ground at sea level, which is what puts snow on a mountain
			// standing in a temperate zone.
			ElevationCooling:   0.5,
			HeatContrastPasses: 1,

			// Even fifths of the scalar's range. The middle band is temperate
			// and the two ends are the extremes; nothing here is fixed by
			// construction the way sea level is.
			HeatBands: HeatBands{Polar: -0.6, Cold: -0.2, Temperate: 0.2, Warm: 0.6},

			// 670 hexes down to 83: broad enough to be geography, finer than
			// heat because a rain shadow is smaller than a climate zone.
			Moisture: LadderConfig{WavelengthMiles: 4000, Octaves: 4, Lacunarity: 2, Gain: 0.5},
			// 20 hexes down to 5. Why two neighboring valleys differ.
			MoistureVariation: LadderConfig{WavelengthMiles: 120, Octaves: 3, Lacunarity: 2, Gain: 0.5},

			MoistureFieldWeight: 1,
			MoistureBiasWeight:  0.35,
			// Small, and deliberately so: this is the one term in either axis
			// that varies tile to tile, and it is what turns a climate map into
			// speckle if it is allowed to matter.
			MoistureVariationWeight: 0.12,
			MoistureContrastPasses:  1,

			MoistureBands: MoistureBands{Arid: -0.6, Dry: -0.2, Moderate: 0.2, Humid: 0.6},
		},

		Basin: BasinConfig{
			// 670 hexes down to 167: a continental interior that drains
			// nowhere.
			Broad: LadderConfig{WavelengthMiles: 4000, Octaves: 3, Lacunarity: 2, Gain: 0.5},
			// 130 hexes down to 16: one basin.
			Regional: LadderConfig{WavelengthMiles: 780, Octaves: 4, Lacunarity: 2, Gain: 0.5},
			// 20 hexes down to 5: the floor of one.
			Local: LadderConfig{WavelengthMiles: 120, Octaves: 3, Lacunarity: 2, Gain: 0.5},

			// Coarse to fine and descending, for the reason the elevation
			// weights are: a basin field whose local scale outweighed its
			// regional one would be speckle rather than a hollow.
			BroadWeight:    1,
			RegionalWeight: 0.6,
			LocalWeight:    0.25,
			BiasWeight:     0.4,

			// A deep basin moves moisture by about a third of what it already
			// is, which is enough to take a humid lowland to saturated and an
			// arid one to the bottom of its scale without either becoming a
			// different climate.
			MoistureWeight: 0.6,
		},

		Terrain: TerrainConfig{
			DeepOceanDepth: -0.35,
			OceanDepth:     -0.1,

			IceHeat:    -0.82,
			AlpineHeat: -0.3,

			// 150 hexes down to 37: a province, not a cone.
			Volcanic:            LadderConfig{WavelengthMiles: 900, Octaves: 3, Lacunarity: 2, Gain: 0.5},
			VolcanicFieldWeight: 1,
			VolcanicBiasWeight:  0.5,

			VolcanicElevation:         0.3,
			VolcanoThreshold:          0.65,
			VolcanoRelief:             0.45,
			VolcanicHighlandThreshold: 0.55,

			HillsRelief: 0.6,

			WetlandWetness:   0.4,
			WetlandElevation: 0.15,
			WetlandRelief:    0.3,
			BogHeat:          -0.25,
			SwampHeat:        0.3,

			BadlandsRelief: 0.4,
		},

		MacroRegionSizeHexes: 512,
		RegionSizeHexes:      128,
		ChunkSizeHexes:       32,

		Rim: RimConfig{
			// Twenty-four rings of forced water with ninety-six of shelf
			// inside them: six days' walk of closed band at DESIGN.md 7.2's
			// twenty-four miles a day, reached across twenty-four days of
			// ground that is visibly running out. Both are hexes and neither
			// scales with the radius — the rim is a place a traveller arrives
			// at, not a fraction of the map. Appendix D.17 is what they were
			// tuned to.
			ClosedHexes:  24,
			FalloffHexes: 96,

			// The bottom of the scale, which is the one value that cannot
			// raise a tile: blending toward it can only lower the composite, so
			// the world outside the band is the world and the band is its
			// floor. It is also comfortably below Terrain.DeepOceanDepth, so
			// the shelf has become deep ocean well before the closed band
			// forces it.
			FloorElevation: -1,

			Kind: RimDeepOcean,
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
		{"Climate.Heat", c.Climate.Heat},
		{"Climate.Moisture", c.Climate.Moisture},
		{"Climate.MoistureVariation", c.Climate.MoistureVariation},
		{"Basin.Broad", c.Basin.Broad},
		{"Basin.Regional", c.Basin.Regional},
		{"Basin.Local", c.Basin.Local},
		{"Terrain.Volcanic", c.Terrain.Volcanic},
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

	if err := c.Climate.validate(); err != nil {
		return err
	}

	if err := c.Basin.validate(); err != nil {
		return err
	}

	if err := c.Terrain.validate(c.Elevation.Bands); err != nil {
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

// validate reports the first reason the climate configuration cannot be used.
//
// The three ladders are checked with the others in Config.Validate, so what is
// left here is the weights, the cooling, the pass counts, and the two band
// ladders.
func (cc ClimateConfig) validate() error {
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Climate.HeatFieldWeight", cc.HeatFieldWeight},
		{"Climate.HeatBiasWeight", cc.HeatBiasWeight},
		{"Climate.MoistureFieldWeight", cc.MoistureFieldWeight},
		{"Climate.MoistureBiasWeight", cc.MoistureBiasWeight},
		{"Climate.MoistureVariationWeight", cc.MoistureVariationWeight},
	} {
		if !isFinite(f.value) {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotFinite}
		}
		// Positive rather than merely non-negative, for the reason the
		// elevation weights are: a weight of zero silences a term whose field
		// is still evaluated for every tile in the world, and the setting that
		// means "do not read this" is the wavelength rather than this.
		if f.value <= 0 {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotPositive}
		}
	}

	// Zero cooling is a world with no lapse rate, which is a legitimate setting
	// rather than a missing one, so this is bounded below by zero rather than
	// by positivity.
	if err := checkUnit("Climate.ElevationCooling", cc.ElevationCooling); err != nil {
		return err
	}

	for _, f := range []struct {
		name  string
		value uint8
	}{
		{"Climate.HeatContrastPasses", cc.HeatContrastPasses},
		{"Climate.MoistureContrastPasses", cc.MoistureContrastPasses},
	} {
		if f.value > MaxContrastPasses {
			return &ConfigError{
				Field: f.name, Value: float64(f.value),
				Lo: 0, Hi: float64(MaxContrastPasses), Err: ErrPassCount,
			}
		}
	}

	if err := checkBandLadder("Climate.HeatBands", []string{"Polar", "Cold", "Temperate", "Warm"},
		[]float64{cc.HeatBands.Polar, cc.HeatBands.Cold, cc.HeatBands.Temperate, cc.HeatBands.Warm}); err != nil {
		return err
	}
	return checkBandLadder("Climate.MoistureBands", []string{"Arid", "Dry", "Moderate", "Humid"},
		[]float64{cc.MoistureBands.Arid, cc.MoistureBands.Dry, cc.MoistureBands.Moderate, cc.MoistureBands.Humid})
}

// checkBandLadder rejects a climate band ladder that does not strictly ascend
// inside the open interval (-1, +1).
//
// The interval is open at both ends because the scalar it cuts is clamped to
// [-1, +1]: a threshold at -1 would leave the bottom band reachable only by a
// value that had saturated, and one at +1 would make the top band unreachable
// altogether. Each is a band that can never be reached in any ordinary way,
// which is the defect ErrNotAscending and this bound exist for — and the one
// that the distribution test of DESIGN.md 30.8 would otherwise be the only
// thing to catch.
//
// Unlike the elevation ladder there is no threshold fixed by construction here.
// Elevation has sea level at zero whatever SeaLevel is set to; heat has no such
// anchor, so all four entries are free and all four are checked the same way.
func checkBandLadder(prefix string, names []string, values []float64) error {
	floor, below := -1.0, "-1"
	for i, v := range values {
		name := prefix + "." + names[i]
		if !isFinite(v) {
			return &ConfigError{Field: name, Value: v, Err: ErrNotFinite}
		}
		if v >= 1 {
			return &ConfigError{Field: name, Value: v, Lo: -1, Hi: 1, Err: ErrOutOfRange}
		}
		if v <= floor {
			return &ConfigError{Field: name, Value: v, Below: below, Err: ErrNotAscending}
		}
		floor, below = v, name
	}
	return nil
}

// validate reports the first reason the basin configuration cannot be used.
//
// The three ladders are checked with the others in Config.Validate, so what is
// left here is the four weights and the moisture amount.
func (bc BasinConfig) validate() error {
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Basin.BroadWeight", bc.BroadWeight},
		{"Basin.RegionalWeight", bc.RegionalWeight},
		{"Basin.LocalWeight", bc.LocalWeight},
		{"Basin.BiasWeight", bc.BiasWeight},
	} {
		if !isFinite(f.value) {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotFinite}
		}
		// Positive rather than merely non-negative, for the reason the
		// elevation weights are: a weight of zero silences a term whose field
		// is still evaluated for every tile in the world, and the setting that
		// means "do not read this" is MoistureWeight, which silences the whole
		// composite at once.
		if f.value <= 0 {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotPositive}
		}
	}

	// Bounded above by one, which is not an arbitrary round number: wetness is
	// moisture*(1 + w*basin) and basin reaches -1, so at w = 1 a rise erases
	// the climate's moisture entirely and past it the product changes sign.
	// A rise that made a dry place wet would be the composition running
	// backwards, which is exactly the defect the product form exists to avoid.
	// Zero is the other end and is legitimate: terrain then reads the climate's
	// moisture unchanged.
	return checkUnit("Basin.MoistureWeight", bc.MoistureWeight)
}

// validate reports the first reason the terrain configuration cannot be used.
//
// The volcanic ladder is checked with the others in Config.Validate, so what is
// left here is the thresholds. Three pairs of them must ascend, and each pair
// is a rule that would otherwise be unreachable rather than merely odd — which
// is the defect ErrNotAscending exists for and the one the distribution test of
// DESIGN.md 30.8 would otherwise be the only thing to catch.
//
// It takes the elevation band ladder because the wetland rule and the volcanic
// rule are both cut on the elevation scalar, and a threshold above the mountain
// band is a rule that can only fire where another rule has already claimed the
// tile.
func (tc TerrainConfig) validate(bands ElevationBands) error {
	// The two ocean depths. Both are below sea level, because water is, and
	// deep ocean is deeper than ocean.
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Terrain.DeepOceanDepth", tc.DeepOceanDepth},
		{"Terrain.OceanDepth", tc.OceanDepth},
	} {
		if !isFinite(f.value) {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotFinite}
		}
		if f.value < -1 || f.value >= 0 {
			return &ConfigError{Field: f.name, Value: f.value, Lo: -1, Hi: 0, Err: ErrOutOfRange}
		}
	}
	if tc.OceanDepth <= tc.DeepOceanDepth {
		return &ConfigError{
			Field: "Terrain.OceanDepth", Value: tc.OceanDepth,
			Below: "Terrain.DeepOceanDepth", Err: ErrNotAscending,
		}
	}

	// The heat thresholds, each strictly inside the clamped scalar's range for
	// the reason checkBandLadder's are.
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Terrain.IceHeat", tc.IceHeat},
		{"Terrain.AlpineHeat", tc.AlpineHeat},
		{"Terrain.BogHeat", tc.BogHeat},
		{"Terrain.SwampHeat", tc.SwampHeat},
		{"Terrain.WetlandWetness", tc.WetlandWetness},
		{"Terrain.VolcanoThreshold", tc.VolcanoThreshold},
		{"Terrain.VolcanicHighlandThreshold", tc.VolcanicHighlandThreshold},
	} {
		if err := checkSignedThreshold(f.name, f.value); err != nil {
			return err
		}
	}

	// Ice is colder than alpine, or every alpine tile is already under ice and
	// alpine terrain exists in the vocabulary and nowhere else.
	if tc.AlpineHeat <= tc.IceHeat {
		return &ConfigError{
			Field: "Terrain.AlpineHeat", Value: tc.AlpineHeat,
			Below: "Terrain.IceHeat", Err: ErrNotAscending,
		}
	}
	// A bog is colder than a swamp, or the marsh between them is unreachable.
	if tc.SwampHeat <= tc.BogHeat {
		return &ConfigError{
			Field: "Terrain.SwampHeat", Value: tc.SwampHeat,
			Below: "Terrain.BogHeat", Err: ErrNotAscending,
		}
	}
	// A cone needs a stronger tendency than the ground around it, or the
	// highland rule is a cone rule that also fires on flat ground.
	if tc.VolcanoThreshold <= tc.VolcanicHighlandThreshold {
		return &ConfigError{
			Field: "Terrain.VolcanoThreshold", Value: tc.VolcanoThreshold,
			Below: "Terrain.VolcanicHighlandThreshold", Err: ErrNotAscending,
		}
	}

	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Terrain.VolcanicFieldWeight", tc.VolcanicFieldWeight},
		{"Terrain.VolcanicBiasWeight", tc.VolcanicBiasWeight},
	} {
		if !isFinite(f.value) {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotFinite}
		}
		if f.value <= 0 {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotPositive}
		}
	}

	// The relief thresholds. Relief is already in [0, 1] and one is reachable,
	// so both ends are legitimate settings.
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Terrain.VolcanoRelief", tc.VolcanoRelief},
		{"Terrain.HillsRelief", tc.HillsRelief},
		{"Terrain.WetlandRelief", tc.WetlandRelief},
		{"Terrain.BadlandsRelief", tc.BadlandsRelief},
	} {
		if err := checkUnit(f.name, f.value); err != nil {
			return err
		}
	}

	// The two elevation thresholds. Both are land, so both are above sea level,
	// and both are below the mountain band: a volcanic rule above it could only
	// fire where the mountain rule has already claimed the tile, and a wetland
	// rule above it is a marsh on a summit.
	for _, f := range []struct {
		name  string
		value float64
	}{
		{"Terrain.VolcanicElevation", tc.VolcanicElevation},
		{"Terrain.WetlandElevation", tc.WetlandElevation},
	} {
		if err := checkUnit(f.name, f.value); err != nil {
			return err
		}
		if f.value <= 0 {
			return &ConfigError{Field: f.name, Value: f.value, Err: ErrNotPositive}
		}
		if f.value >= bands.Mountain {
			return &ConfigError{
				Field: f.name, Value: f.value,
				Lo: 0, Hi: bands.Mountain, Err: ErrOutOfRange,
			}
		}
	}

	return nil
}

// checkSignedThreshold rejects a threshold on a clamped [-1, +1] scalar that is
// not strictly inside that range.
//
// The interval is open at both ends for the reason checkBandLadder's is: the
// scalars these cut are clamped, so a threshold at -1 fires only for a value
// that has saturated and one at +1 fires for nothing at all. Either is a rule
// that can never be reached in any ordinary way.
func checkSignedThreshold(field string, v float64) error {
	if !isFinite(v) {
		return &ConfigError{Field: field, Value: v, Err: ErrNotFinite}
	}
	if v <= -1 || v >= 1 {
		return &ConfigError{Field: field, Value: v, Lo: -1, Hi: 1, Err: ErrOutOfRange}
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
	if err := checkSignedUnit("Rim.FloorElevation", rc.FloorElevation); err != nil {
		return err
	}
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

// checkSignedUnit rejects a value outside the closed interval [-1, +1].
//
// It is closed at both ends where checkSignedThreshold is open, and the
// difference is what the number is for: a threshold at an end is a rule nothing
// can reach, while a value at an end is an ordinary setting. The rim's forced
// elevation is the case — -1 is the bottom of the scale and is exactly what a
// deep-ocean rim is pinned at.
func checkSignedUnit(field string, v float64) error {
	if !isFinite(v) {
		return &ConfigError{Field: field, Value: v, Err: ErrNotFinite}
	}
	if v < -1 || v > 1 {
		return &ConfigError{Field: field, Value: v, Lo: -1, Hi: 1, Err: ErrOutOfRange}
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
