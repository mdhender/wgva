// Copyright (c) 2026 Michael D Henderson. All rights reserved.

package wgva

// ---------------------------------------------------------------------------
// The vocabulary
// ---------------------------------------------------------------------------

// Terrain is the game-facing classification of a tile, derived from the
// physical fields and never from an independent per-tile draw.
//
// It is a classification rather than a description: the elevation scalar, the
// relief, and the two climate bands travel beside it in every Tile, so a game
// can render a forested hill, a glaciated mountain, or a volcanic island
// without this type needing a constant for every combination. See DESIGN.md 17.
type Terrain uint8

// The terrain vocabulary of DESIGN.md 17, by family: ocean, inland water,
// frozen, wetland, dry, open land, forest, elevated, volcanic, and the coast.
//
// These values are persisted and appear in cached tiles, so each is written out
// explicitly rather than taken from iota: inserting a terrain into the middle of
// an iota block would silently renumber everything after it and change the
// meaning of stored data, with nothing in the diff that looks like a data
// change.
//
// **TerrainInlandSea and TerrainLake are declared and are never produced.**
// DESIGN.md 17.1 omits inland water, and the two values stay in the vocabulary
// with their numbers pinned because they are part of the persisted value space:
// the version that does emit them must not renumber the eighteen variants that
// follow them. The distribution test asserts every other terrain is reachable
// and asserts these two are reached nowhere.
const (
	TerrainDeepOcean    Terrain = 0
	TerrainOcean        Terrain = 1
	TerrainShallowSea   Terrain = 2
	TerrainCoastalWater Terrain = 3

	TerrainInlandSea Terrain = 4
	TerrainLake      Terrain = 5

	TerrainGlacialIce Terrain = 6
	TerrainTundra     Terrain = 7

	TerrainMarsh Terrain = 8
	TerrainSwamp Terrain = 9
	TerrainBog   Terrain = 10

	TerrainDesert    Terrain = 11
	TerrainBadlands  Terrain = 12
	TerrainScrubland Terrain = 13

	TerrainPlains    Terrain = 14
	TerrainGrassland Terrain = 15
	TerrainSteppe    Terrain = 16
	TerrainSavanna   Terrain = 17

	TerrainBorealForest    Terrain = 18
	TerrainTemperateForest Terrain = 19
	TerrainRainforest      Terrain = 20

	TerrainHills    Terrain = 21
	TerrainMountain Terrain = 22
	TerrainAlpine   Terrain = 23

	TerrainVolcano          Terrain = 24
	TerrainVolcanicHighland Terrain = 25

	TerrainCoast Terrain = 26
)

// terrainCount is how many terrains are declared. It is the length of the slice
// Terrains returns, and TestTerrainVocabulary is what keeps it true.
const terrainCount = 27

// Terrains returns the declared terrains, in value order.
//
// The order is fixed: it is what a distribution readout is laid out in, what a
// legend lists, and what a test iterates. It includes the two inland-water
// values that are never produced, because a readout that omitted them would be
// a readout that could not show they are at zero.
func Terrains() []Terrain {
	return []Terrain{
		TerrainDeepOcean, TerrainOcean, TerrainShallowSea, TerrainCoastalWater,
		TerrainInlandSea, TerrainLake,
		TerrainGlacialIce, TerrainTundra,
		TerrainMarsh, TerrainSwamp, TerrainBog,
		TerrainDesert, TerrainBadlands, TerrainScrubland,
		TerrainPlains, TerrainGrassland, TerrainSteppe, TerrainSavanna,
		TerrainBorealForest, TerrainTemperateForest, TerrainRainforest,
		TerrainHills, TerrainMountain, TerrainAlpine,
		TerrainVolcano, TerrainVolcanicHighland,
		TerrainCoast,
	}
}

// String returns the name of the terrain.
func (t Terrain) String() string {
	switch t {
	case TerrainDeepOcean:
		return "deep-ocean"
	case TerrainOcean:
		return "ocean"
	case TerrainShallowSea:
		return "shallow-sea"
	case TerrainCoastalWater:
		return "coastal-water"
	case TerrainInlandSea:
		return "inland-sea"
	case TerrainLake:
		return "lake"
	case TerrainGlacialIce:
		return "glacial-ice"
	case TerrainTundra:
		return "tundra"
	case TerrainMarsh:
		return "marsh"
	case TerrainSwamp:
		return "swamp"
	case TerrainBog:
		return "bog"
	case TerrainDesert:
		return "desert"
	case TerrainBadlands:
		return "badlands"
	case TerrainScrubland:
		return "scrubland"
	case TerrainPlains:
		return "plains"
	case TerrainGrassland:
		return "grassland"
	case TerrainSteppe:
		return "steppe"
	case TerrainSavanna:
		return "savanna"
	case TerrainBorealForest:
		return "boreal-forest"
	case TerrainTemperateForest:
		return "temperate-forest"
	case TerrainRainforest:
		return "rainforest"
	case TerrainHills:
		return "hills"
	case TerrainMountain:
		return "mountain"
	case TerrainAlpine:
		return "alpine"
	case TerrainVolcano:
		return "volcano"
	case TerrainVolcanicHighland:
		return "volcanic-highland"
	case TerrainCoast:
		return "coast"
	default:
		return "unknown"
	}
}

// Valid reports whether the value is a declared terrain.
//
// It is a range check rather than a switch over twenty-seven cases, which is
// sound only because the declared values are contiguous from zero.
// TestTerrainVocabulary asserts they still are, so a terrain added at the end
// without this constant moving fails a test rather than becoming a value that
// exists and is not valid.
func (t Terrain) Valid() bool { return t <= TerrainCoast }

// IsWater reports whether the terrain is open water of any kind.
//
// It is a property of the terrain rather than a comparison against the
// elevation scalar so that a caller holding a classified tile does not have to
// remember which four variants are wet. The two inland-water values are
// included even though nothing produces them: what this answers is what the
// value means, not what this version emits.
func (t Terrain) IsWater() bool {
	switch t {
	case TerrainDeepOcean, TerrainOcean, TerrainShallowSea, TerrainCoastalWater,
		TerrainInlandSea, TerrainLake:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// The tile
// ---------------------------------------------------------------------------

// Tile is everything the generator says about one coordinate.
//
// The physical values travel with the classifications deliberately: a game or a
// renderer that wants to draw a forested hill or a glaciated mountain must not
// need a diagnostic API to recover the numbers the classification was made
// from. See DESIGN.md 4.3.
//
// It is a plain value with no pointers, small enough that a batch API can fill a
// []Tile with no allocation and no indirection. TestTileShape asserts its size.
type Tile struct {
	Coord Coord

	// ElevationValue is the elevation scalar: -1 deep ocean, 0 sea level, +1
	// extreme highland. It is bit-identical to ElevationAt.
	ElevationValue float64

	// HeatValue and MoistureValue are the two climate scalars: -1 polar and
	// arid, 0 the middle of the temperate and moderate bands, +1 hot and
	// saturated. Each is bit-identical to HeatAt and MoistureAt.
	//
	// MoistureValue is the climate's moisture and not the wetness terrain was
	// classified from. The basin product of DESIGN.md 17.1 belongs to terrain,
	// and a tile that reported a moisture its climate model had never produced
	// would corrupt every distribution measurement that reads this field.
	HeatValue     float64
	MoistureValue float64

	// ReliefValue is the local steepness, in [0, 1]. It is bit-identical to
	// Relief.
	ReliefValue float64

	Elevation Elevation
	Climate   Climate
	Terrain   Terrain

	// Rim reports that this tile is inside the world rim: forced terrain,
	// closed to play. See DESIGN.md 15.1.
	//
	// **The generator marks; the game enforces.** This is a statement about the
	// world, and refusing a move is a game rule; nothing in wgva knows what a
	// move is.
	//
	// The rim profile is phase 7 of DESIGN.md 32, so until it lands this is
	// false everywhere and the classifier's rim rule is reached by nothing. The
	// rule is written because the order is what this phase settles, not because
	// there is something to force yet.
	Rim bool
}

// Tile returns everything the generator says about one coordinate.
//
// It costs seven elevation evaluations: its own, and the six neighbors that
// relief and the water's edge are read from. That is the whole reason a render
// budget is counted in evaluations rather than in tiles, and it is why Sample
// does not carry one. See DESIGN.md 4.3 and 29.
//
// **Nothing here calls Tile, and nothing here calls Relief.** Both would
// recurse until the stack was gone — relief reads six neighbors and each
// neighbor would read six more — and Go will not catch it. The internals are
// structured around elevationScalar so that the recursion cannot be written; see
// DESIGN.md 18.
func (g *Generator) Tile(c Coord) Tile {
	e := g.elevationAt(c)
	n := g.neighborhoodAt(c, e.elevation)
	climate := g.climateAt(e.pos, e.region, e.elevation)
	basin := g.basinAt(e.pos, e.region)
	volcanic := g.volcanicAt(e.pos, e.region)

	band := g.cfg.Elevation.Bands.Classify(e.elevation)
	wetness := g.cfg.wetness(climate.moisture, basin.basin)

	return Tile{
		Coord:          c,
		ElevationValue: e.elevation,
		HeatValue:      climate.heat,
		MoistureValue:  climate.moisture,
		ReliefValue:    n.relief,
		Elevation:      band,
		Climate:        g.cfg.Climate.classify(climate),
		Terrain: g.cfg.classifyTerrain(terrainInputs{
			elevation:     e.elevation,
			band:          band,
			relief:        n.relief,
			heat:          climate.heat,
			wetness:       wetness,
			heatBand:      g.cfg.Climate.HeatBands.Classify(climate.heat),
			wetnessBand:   g.cfg.Climate.MoistureBands.Classify(wetness),
			volcanic:      volcanic,
			landNeighbor:  n.landNeighbor,
			waterNeighbor: n.waterNeighbor,
		}),
	}
}

// TerrainAt returns the terrain classification at a coordinate. It costs what
// Tile costs, because it is Tile with the rest of the answer thrown away.
func (g *Generator) TerrainAt(c Coord) Terrain { return g.Tile(c).Terrain }

// ---------------------------------------------------------------------------
// The classifier
// ---------------------------------------------------------------------------

// terrainInputs is everything the classifier of DESIGN.md 17 reads.
//
// It is a struct rather than a dozen parameters because the order of a dozen
// float64 arguments is a defect waiting to happen: two of these are in [0, 1],
// four are in [-1, +1], and the compiler would accept any permutation of them.
type terrainInputs struct {
	// rim reports that the tile is inside the closed band of DESIGN.md 15.1.
	// Phase 7 is what sets it; see Tile.Rim.
	rim bool

	// rimKind is what the closed band is made of, read only when rim is set.
	rimKind RimKind

	// elevation is the elevation scalar and band is what it classifies to. Both
	// are carried because the classifier reads the scalar where it wants a
	// threshold of its own and the band where it wants the configured ladder.
	elevation float64
	band      Elevation

	// relief is the local steepness, in [0, 1].
	relief float64

	// heat is the heat scalar and heatBand is what it classifies to.
	heat     float64
	heatBand HeatBand

	// wetness is the moisture scalar after the basin product of
	// DESIGN.md 17.1, and wetnessBand is what the moisture ladder cuts it to.
	// It is not the tile's reported moisture; see Tile.MoistureValue.
	wetness     float64
	wetnessBand MoistureBand

	// volcanic is the volcanic tendency, in [-1, +1].
	volcanic float64

	// landNeighbor and waterNeighbor report what the six neighboring elevation
	// scalars found. They are what makes coastal water and the coast possible
	// at all, and they are the only thing in the classifier that is not a
	// function of this tile alone — which is affordable precisely because
	// relief already pays for the same six evaluations.
	landNeighbor  bool
	waterNeighbor bool
}

// classifyTerrain returns the terrain for one tile's physical fields.
//
// **The order of the rules is the algorithm.** Each one claims the tiles it is
// about before a broader one can swallow them, and the broadest — the climate
// cover — runs last with whatever is left:
//
//	rim
//	  -> ocean and inland water
//	  -> glacial ice
//	  -> volcano
//	  -> mountain and alpine terrain
//	  -> wetland
//	  -> coastal land
//	  -> climate-driven land cover
//
// Three of those placements are worth saying out loud:
//
//   - **The rim is first because it is not a judgement about the ground.** It
//     is a statement that this tile is outside the world, and everything below
//     it is the ordinary classifier.
//   - **Coastal land sits between wetland and the climate cover rather than
//     above both.** A saturated shoreline is a marsh, which says more than
//     *coast* does; anything else at the water's edge is a coast before it is a
//     grassland.
//   - **Hills sit with the mountains rather than in the cover.** What makes a
//     tile hills is its elevation and its slope, and a rule that let the cover
//     claim it first would mean the elevated family only existed above the
//     mountain threshold.
//
// Inland water is absent. DESIGN.md 17.1 records why at length, and the short
// version is that a lake has one surface elevation, that which tiles are one
// lake is a connected component, and that bounded local generation can supply
// neither. What ships instead is the geography: the basin influence is in
// terrainInputs.wetness, where a wet basin reads as marsh, swamp, or bog.
func (c Config) classifyTerrain(in terrainInputs) Terrain {
	tc := &c.Terrain

	// The rim. Forced terrain, according to what the closed band is made of.
	if in.rim {
		switch in.rimKind {
		case RimPolarIce:
			return TerrainGlacialIce
		case RimDeepOcean:
			return TerrainDeepOcean
		default:
			// A validated configuration cannot reach this, and the default is
			// the documented one of DESIGN.md 14.1 rather than a panic: an
			// unknown rim is still a closed rim, and deep ocean is what the
			// default rim is made of.
			return TerrainDeepOcean
		}
	}

	// Ocean. The land/water rule of DESIGN.md 15 is the whole test — elevation
	// at or below zero is water — and the four variants are depth and the
	// water's edge. Coastal water comes first because adjacency to land says
	// more about a tile than its depth does.
	if in.elevation <= 0 {
		switch {
		case in.landNeighbor:
			return TerrainCoastalWater
		case in.elevation <= tc.DeepOceanDepth:
			return TerrainDeepOcean
		case in.elevation <= tc.OceanDepth:
			return TerrainOcean
		default:
			return TerrainShallowSea
		}
	}

	// Glacial ice: land cold enough that the ice is permanent. It is above the
	// elevated rules, so an icecap on a mountain is ice rather than alpine
	// rock; alpine terrain is the cold mountain that is not under ice.
	if in.heat <= tc.IceHeat {
		return TerrainGlacialIce
	}

	// Volcanic terrain: a strong regional tendency, standing on ground the
	// tendency has raised. The cone needs concentrated relief as well, which is
	// what makes it rare without anything being rolled for.
	if in.elevation >= tc.VolcanicElevation {
		switch {
		case in.volcanic >= tc.VolcanoThreshold && in.relief >= tc.VolcanoRelief:
			return TerrainVolcano
		case in.volcanic >= tc.VolcanicHighlandThreshold:
			return TerrainVolcanicHighland
		}
	}

	// The elevated family. Alpine terrain is the cold mountain; hills are the
	// highland band or anything steep enough below it.
	if in.band == ElevationMountain {
		if in.heat <= tc.AlpineHeat {
			return TerrainAlpine
		}
		return TerrainMountain
	}
	if in.band == ElevationHighland || in.relief >= tc.HillsRelief {
		return TerrainHills
	}

	// Wetland: saturated, low, and flat, all three. Water stands where it is
	// neither drained by a slope nor run off the edge of a highland, so a
	// wetland rule that read only the moisture would put a swamp on a hillside.
	if in.wetness >= tc.WetlandWetness && in.elevation <= tc.WetlandElevation && in.relief <= tc.WetlandRelief {
		switch {
		case in.heat <= tc.BogHeat:
			return TerrainBog
		case in.heat >= tc.SwampHeat:
			return TerrainSwamp
		default:
			return TerrainMarsh
		}
	}

	// Coastal land: land at the water's edge that is not a wetland.
	if in.waterNeighbor {
		return TerrainCoast
	}

	// Whatever is left is ordinary ground, and what grows on it is climate.
	return tc.cover(in.heatBand, in.wetnessBand, in.relief)
}

// cover returns the climate-driven land cover for a heat band and a wetness
// band, with the one relief-driven exception the dry family needs.
//
// Badlands are desert that is not flat. They are in DESIGN.md 17's dry family
// beside desert and scrubland, and their distinguishing evidence there is
// "exposed relief" — so the cover table places the desert and this lifts the
// steep part of it out. Doing it here rather than as a rule of its own keeps
// the exception where the thing it is an exception to is written down.
func (tc *TerrainConfig) cover(h HeatBand, m MoistureBand, relief float64) Terrain {
	t := coverFor(h, m)
	if t == TerrainDesert && relief >= tc.BadlandsRelief {
		return TerrainBadlands
	}
	return t
}

// coverFor looks the two bands up in the cover table.
//
// The default is plains, and it is documented rather than a panic for the
// reason DESIGN.md 14.1 gives: an undeclared band is a programming error
// upstream, and the classifier's job is to return a terrain. Go cannot make
// this total the way a sum type would, and TestCoverTableIsTotal is what stands
// in for the exhaustiveness — it asserts every declared pair has an entry and
// that every entry is a declared terrain.
func coverFor(h HeatBand, m MoistureBand) Terrain {
	if !h.Valid() || !m.Valid() {
		return TerrainPlains
	}
	return coverTable[h][m]
}

// coverTable is the land cover for each of the twenty-five climate cells, heat
// down and moisture across: polar to hot, arid to saturated.
//
// It is a table rather than a ladder of comparisons because that is what it is:
// the two axes are independent, so what grows somewhere is a function of the
// pair and not of either alone. A polar desert and a polar rainforest are both
// ordinary places, and a rule that read the axes in sequence would have to
// decide which of them outranked the other.
//
// It is indexed by the band values, which are 0 through 4 on both axes by
// declaration. TestCoverTableIsTotal asserts the indices and the declared bands
// still agree, so renumbering a band fails a test rather than quietly
// transposing the map.
//
// Reading the rows: the polar row is tundra whatever the moisture, because
// nothing else grows and the ice rule above has already taken the ground cold
// enough to stay frozen. The cold row runs tundra, steppe, plains, and boreal
// forest. The temperate and warm rows are the ordinary spread from desert
// through grass to forest, with savanna standing in for grassland once it is
// warm. The hot row is desert until there is real water and then rainforest.
var coverTable = [heatBandCount][moistureBandCount]Terrain{
	HeatPolar: {
		MoistureArid:      TerrainTundra,
		MoistureDry:       TerrainTundra,
		MoistureModerate:  TerrainTundra,
		MoistureHumid:     TerrainTundra,
		MoistureSaturated: TerrainTundra,
	},
	HeatCold: {
		MoistureArid:      TerrainTundra,
		MoistureDry:       TerrainSteppe,
		MoistureModerate:  TerrainPlains,
		MoistureHumid:     TerrainBorealForest,
		MoistureSaturated: TerrainBorealForest,
	},
	HeatTemperate: {
		MoistureArid:      TerrainDesert,
		MoistureDry:       TerrainScrubland,
		MoistureModerate:  TerrainGrassland,
		MoistureHumid:     TerrainTemperateForest,
		MoistureSaturated: TerrainTemperateForest,
	},
	HeatWarm: {
		MoistureArid:      TerrainDesert,
		MoistureDry:       TerrainScrubland,
		MoistureModerate:  TerrainSavanna,
		MoistureHumid:     TerrainTemperateForest,
		MoistureSaturated: TerrainRainforest,
	},
	HeatHot: {
		MoistureArid:      TerrainDesert,
		MoistureDry:       TerrainDesert,
		MoistureModerate:  TerrainSavanna,
		MoistureHumid:     TerrainRainforest,
		MoistureSaturated: TerrainRainforest,
	},
}

// The dimensions of the cover table. They are written out rather than taken
// from len(Heats()) because an array length must be a constant, and a test
// asserts the two agree.
const (
	heatBandCount     = 5
	moistureBandCount = 5
)
