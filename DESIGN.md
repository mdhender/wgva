# WGVA — Effectively Unbounded Procedural World Generator Design

**Module:** `github.com/mdhender/wgva`  
**Target language:** Go  
**Coordinate system:** axial hex coordinates `(q, r)`  
**Hex scale:** 3-mile apothem
**World origin:** `(0, 0)`  
**Primary goal:** Generate attractive, geographically coherent terrain on demand without exposing a practical map boundary.

---

## 1. Purpose

WGVA is a deterministic procedural world generator intended for games that need an effectively unbounded hex map.

The generator must preserve the strongest property of a simple coordinate-hash world:

```text
(seed, q, r) -> tile
```

A tile can be generated independently at any coordinate without first constructing or storing the rest of the map.

Unlike a naive coordinate-hash generator, however, adjacent tiles must participate in coherent geographic structures such as:

- oceans and continental interiors,
- mountain belts,
- uplands and lowlands,
- climate zones,
- forests, plains, deserts, marshes, and other terrain,
- region-scale geographic variation that crosses chunk boundaries naturally.

The implementation uses a finite but enormous wrapped hexagonal address space. This is described as “unbounded” to players because normal play should never encounter a terminal edge. Its large-scale geography is generated from deterministic continuous fields and hierarchical regions.

---

## 2. Design Goals

The generator should satisfy the following goals.

### 2.1 Deterministic

For a given world seed and tile coordinate, generated attributes must always be identical.

```go
Generate(seed, q, r) == Generate(seed, q, r)
```

Generation order must not affect results.

### 2.2 Effectively unbounded and wrapped

No API should require world width, height, radius, or bounding rectangle.

For the alpha generator, canonical coordinates form a hexagonal map with signed 16-bit cube components. Coordinate operations that leave that map wrap to the corresponding tile on the opposite edge using the scheme in section 7.1.

### 2.3 Local

Generating tile `(q, r)` should require only a bounded amount of nearby procedural context.

Generation must not require:

- generating every tile between `(0,0)` and `(q,r)`,
- scanning the entire world,
- global normalization,
- global flood fill,
- a precomputed heightmap.

### 2.4 Geographically coherent

Nearby tiles should normally have related elevation, climate, and terrain.

Large features should cross implementation chunk and region boundaries without visible seams.

### 2.5 Reproducible across platforms

The same seed and coordinates should produce the same result on all supported Go platforms.

Avoid algorithms whose output depends on:

- map iteration order,
- platform-specific floating-point behavior where practical,
- goroutine scheduling,
- mutable random-number-generator state.

### 2.6 Stateless by default

The core generator should not require persistent storage.

Applications may cache generated tiles or regions, but caching must be an optimization, not part of correctness.

---

## 3. Non-Goals

The first implementation does **not** need to guarantee:

- an exact global percentage of land versus water,
- a fixed number of continents,
- globally correct river drainage networks,
- plate tectonic simulation,
- erosion simulation,
- settlement placement,
- roads,
- political boundaries,
- resource placement,
- historical simulation.

Those may be layered onto the generator later.

Although the address space is finite, it is far too large to generate or normalize globally. Statements such as "the world is exactly 58% land" are therefore not practical requirements. The generator may instead target statistical properties over sufficiently large samples.

---

## 4. Core Model

A tile is uniquely identified by its canonical axial coordinate. Non-canonical aliases are normalized before lookup.

```go
type Component int16

type Coord struct {
    Q Component
    R Component
}
```

Store alpha axial coordinates as signed 16-bit `Component` values. Calculate `s = -q-r`, mirror centers, differences, and other intermediate coordinate arithmetic with `int64`. Not every pair of signed 16-bit `q` and `r` values has an `s` in the signed 16-bit range; normalize such coordinates into the canonical wrapped map before generation, persistence, or comparison.

Keep the component type and world-radius constant centralized. Expanding a later generator to `int32` should require changing those definitions and compatibility tests rather than rewriting algorithms or the database schema. The expansion still changes world topology and therefore requires a new generator version.

The origin is:

```text
(0, 0)
```

No special terrain meaning is assigned to the origin unless configured explicitly.

### 4.1 Tile

The minimum public tile representation is:

```go
type Tile struct {
    Coord Coord

    ElevationValue float64
    HeatValue      float64
    MoistureValue  float64
    ReliefValue    float64

    Elevation Elevation
    Climate   Climate
    Terrain   Terrain
}
```

The physical values and their elevation, climate, and terrain classifications are part of the ordinary tile result. Games and renderers should not need a diagnostic API to recover them.

The generator may also expose intermediate values for diagnostics.

Example:

```go
type Sample struct {
    Coord Coord

    Continentalness float64
    RegionalUplift  float64
    BasinInfluence  float64

    Tile Tile
}
```

Diagnostic values should not be required for ordinary game use.

---

## 5. Recommended Architecture

WGVA should use four conceptual layers:

```text
world seed
    |
    +-- macro-scale continuous fields
    |
    +-- deterministic hierarchical regions
    |
    +-- local detail fields
    |
    +-- classification
            |
            +-- elevation
            +-- climate
            +-- terrain
```

The important architectural rule is:

> Chunks and regions are addressing and organization devices, not visible geographic boundaries.

Noise fields and region influences must extend across them.

---

## 6. Generation Pipeline

For a requested tile `(q, r)`:

```text
1. Normalize the coordinate into the wrapped canonical map.
2. Convert the canonical coordinate to continuous world-space position.
3. Evaluate macro-scale fields.
4. Determine hierarchical regional influences.
5. Evaluate medium- and local-scale detail.
6. Combine fields into normalized physical values.
7. Classify elevation.
8. Classify climate.
9. Classify terrain.
10. Return immutable tile data with its canonical coordinate.
```

Conceptually:

```go
func (g *Generator) Tile(c Coord) Tile
```

should be sufficient for callers.

---

## 7. Hex Coordinates and World Space

WGVA uses axial hex coordinates `(q, r)`.

Axial coordinates define tile identity and adjacency, not how hexes must be drawn. Flat-top versus pointy-top orientation is a rendering and layout choice for images presented to players; it is not part of the generator's public model. A renderer may choose either orientation without changing generated tile data.

Noise functions usually operate on Cartesian coordinates, so axial coordinates should be converted to continuous 2D world space before sampling.

Each regular hex has a 3-mile apothem. Therefore:

```text
neighboring center distance = 6 miles
flat-to-flat width          = 6 miles
point-to-point width        = 4 * sqrt(3) miles, approximately 6.93 miles
area                        = 18 * sqrt(3) square miles, approximately 31.18 square miles
```

Canonical world-space coordinates should be measured in miles.

The generator may use whichever internal orientation makes the implementation simplest. For example, one convenient pointy-top embedding is:

```text
x = 6 * (q + r/2)
y = 3 * sqrt(3) * r
```

This places the centers of all adjacent hexes exactly 6 miles apart. Any equivalent embedding is acceptable if it preserves that distance and uses miles as its world-space unit. The chosen embedding is an internal implementation detail, although it must remain stable wherever deterministic compatibility is promised.

The generator should centralize this conversion:

```go
type Vec2 struct {
    X float64
    Y float64
}

func AxialToWorld(c Coord) Vec2
```

All continuous fields should sample from the same canonical world-space coordinate system.

This avoids distortion caused by directly feeding `q` and `r` into Cartesian noise.

### 7.1 Wrapped coordinate domain

For the alpha generator, let `N = math.MaxInt16`. The canonical map is the hexagonal cube-coordinate domain:

```text
-N <= q <= +N
-N <= r <= +N
-N <= s <= +N
q + r + s = 0
```

Wrapping follows the hexagonal wraparound construction described by Red Blob Games. The six mirror centers are the rotations of:

```text
(2*N+1, -N, -N-1)
```

When an operation produces a coordinate outside the canonical map, translate it by the appropriate mirror center until it is canonical. The implementation must use arithmetic normalization rather than a precomputed mirror table because this map is too large to enumerate. Use `int64` for the mirror centers and all normalization intermediates, then convert the canonical `q` and `r` to `Component`.

All public coordinate operations, neighbor sampling, persistence keys, region lookup, and rendering must use the same canonicalizer. Values that normalize to the same coordinate identify the same tile.

WGVA should make a best effort to make continuous fields periodic under the mirror translations so terrain joins naturally across wrapped edges. Exact periodicity must not delay the first implementation. If a field cannot be made periodic without disproportionate complexity or loss of quality, the discontinuity is an accepted world-warp seam and must be documented and tested as such.

Use `github.com/maloquacious/hexg` for hex directions, geometry, finite-area traversal, layouts, and polygon corners rather than implementing a competing hex library. Convert canonical `Component` values to `int` and then `hexg.Hex` through a centralized adapter. The alpha's `int16` values and derived cube component are losslessly representable by `int` on every Go target.

### 7.2 World scale

For a cube-coordinate hexagon of radius `N`, the number of tiles is:

```text
tiles = 1 + 3*N*(N+1)
```

With `N = math.MaxInt16`, the alpha world contains exactly `3,221,127,169` canonical tiles, approximately `3.2211e9`.

At a 3-mile apothem, each tile covers `18*sqrt(3)`, approximately `31.1769`, square miles. The alpha's total surface area is therefore approximately `1.00425e11` square miles. The center-to-center radius is `196,602` miles, and the opposite-corner center span is `393,204` miles. This is approximately 510 Earth surface areas and is large enough to exercise edge wrapping during alpha testing while remaining effectively unbounded for gameplay.

---

## 8. Coordinate-Based Randomness

Do not use math/rand - use only math/rand/v2.

WGVA should not use a mutable PRNG stream as the basis of terrain generation.

Avoid:

```go
rng := rand.New(...)
for each tile {
    value := rng.Float64()
}
```

That makes results dependent on traversal order.

Instead, derive deterministic values from semantic paths.

Conceptually:

```text
hash(seed, "macro-elevation", regionQ, regionR)
hash(seed, "mountain-axis", regionQ, regionR)
hash(seed, "local-detail", q, r)
```

Provide an internal hash primitive such as:

```go
func hash64(seed uint64, domain uint64, values ...int64) uint64
```

or a typed equivalent.

Different procedural systems must use independent domains so changes to one field do not accidentally perturb another.

Example domains:

```text
continentalness
regional-elevation
relief
moisture
temperature
terrain-detail
region-style
ridge-orientation
```

Do not rely on Go's built-in randomized map hashing.

---

## 9. Continuous Noise

The generator should use deterministic continuous noise for spatial coherence.

Suitable families include:

- gradient noise,
- simplex-style noise,
- OpenSimplex-style noise,
- value noise with interpolation,
- domain-warped combinations of the above.

The design does not require a specific library.

The noise implementation must support evaluation at arbitrary coordinates without requiring a finite array.

A useful interface is:

```go
type Field2D interface {
    Sample(x, y float64) float64
}
```

Expected normalized output:

```text
[-1, +1]
```

or another clearly documented range.

---

## 10. Multi-Scale Geography

A single noise frequency tends to look synthetic.

WGVA should combine several spatial scales.

Example conceptual formula:

```text
elevation =
    macro_continentalness
  + regional_uplift
  + ridge_structure
  + local_relief
  + fine_detail
```

A practical initial implementation might use the following approximate wavelengths. One hex of wavelength means 6 miles of center-to-center distance; it does not refer to edge length, point-to-point width, or area.

| Field | Approximate wavelength | Approximate distance |
|---|---:|---:|
| Continentalness | 500–2,000 hexes | 3,000–12,000 miles |
| Macro uplift | 150–600 hexes | 900–3,600 miles |
| Regional relief | 40–200 hexes | 240–1,200 miles |
| Hills | 8–40 hexes | 48–240 miles |
| Local detail | 3–12 hexes | 18–72 miles |

These are starting values, not requirements.

The exact constants should live in a configuration structure rather than being scattered through the implementation.

---

## 11. Hierarchical Regions

Continuous noise should be supplemented by deterministic regional influences.

Suggested hierarchy:

```text
macro region
    |
    +-- region
            |
            +-- chunk
                    |
                    +-- tile
```

Example sizes at the established 3-mile apothem:

| Level | Axial interval | Physical interval |
|---|---:|---:|
| Macro region | 512 hexes | 3,072 miles |
| Region | 128 hexes | 768 miles |
| Chunk | 32 hexes | 192 miles |

The intervals describe the spacing between boundaries or anchors along either axial basis direction. Regions and chunks produced by independent division of `q` and `r` are parallelograms in world space, not regular hexagons with the listed physical interval as a diameter.

These values may be tuned.

### 11.1 Why regions exist

Regions allow the generator to create persistent geographic character.

A region can deterministically derive parameters such as:

- average uplift,
- climate bias,
- roughness,
- ridge orientation,
- wet/dry tendency,
- volcanic tendency,
- terrain variation.

This gives large areas identity without requiring them to be stored.

### 11.2 No hard boundaries

A tile must not simply inherit parameters from exactly one region.

That would create visible seams.

Instead, regional parameters should be blended from neighboring region anchors.

For a point near four regional anchors:

```text
value =
    w00 * region00
  + w10 * region10
  + w01 * region01
  + w11 * region11
```

For hex-oriented or Voronoi-inspired schemes, blending may involve another fixed local neighborhood.

The important invariant is:

> Crossing a chunk or region boundary must not introduce a discontinuity merely because the addressing region changed.

---

## 12. Region Anchors

One straightforward implementation is a lattice of deterministic anchors.

For each regional grid coordinate:

```text
(regionQ, regionR)
```

derive stable attributes using coordinate hashing.

Example:

```go
type RegionParams struct {
    ElevationBias float64
    MoistureBias  float64
    HeatBias      float64
    Roughness     float64
    RidgeAngle    float64
}
```

When generating a tile, evaluate the nearest relevant anchors and smoothly interpolate their contribution.

This can produce geography with recognizable regional differences while retaining continuous transitions.

---

## 13. Domain Warping

Straight noise frequently reveals its mathematical origin.

Domain warping should be considered part of the recommended implementation.

Instead of evaluating:

```text
elevation = noise(x, y)
```

evaluate:

```text
wx = x + warpX(x, y) * strength
wy = y + warpY(x, y) * strength

elevation = noise(wx, wy)
```

This tends to create more irregular:

- coastlines,
- mountain belts,
- climatic boundaries,
- regional forms.

Warp fields must themselves be deterministic and continuous.

Use low-frequency warp fields for large geography and weaker higher-frequency warps for local irregularity.

---

## 14. Elevation

Elevation is the primary physical field.

The internal representation should initially be a normalized scalar.

Example:

```go
type ElevationValue float64
```

Suggested semantic range:

```text
-1.0 deep ocean
 0.0 sea level
+1.0 extreme highland
```

Values outside that range may be clamped or normalized.

Do not normalize elevation using the minimum and maximum values of a finite generated map. That would reintroduce a dependence on map bounds.

Instead, field composition and thresholds must be stable globally.

### 14.1 Elevation Classification

Expose both a physical scalar and a game classification if useful.

Example:

```go
type Elevation uint8

const (
    ElevationDeepWater Elevation = iota
    ElevationShallowWater
    ElevationLowland
    ElevationUpland
    ElevationHighland
    ElevationMountain
)
```

The exact names are game-facing decisions and should not constrain the internal scalar field.

---

## 15. Water and Land

Land-versus-ocean water should be determined by comparing elevation against a fixed sea-level threshold.

Example:

```text
elevation <= 0 -> ocean water
elevation >  0 -> potential land
```

Sea level may be configurable.

Deterministic basin fields may classify some potential-land tiles as lakes or inland seas if the coherence requirements in section 17 can be met. Otherwise, the first implementation retains basin geography without inland-water classification. Both approaches must use bounded local sampling rather than global connectivity or flood fill.

If a target land fraction is desired, tune the distribution of the continentalness field and sea-level threshold statistically.

Do not calculate sea level from a finite sample at runtime.

---

## 16. Climate

Climate should be computed from at least:

- temperature,
- moisture,
- elevation.

Conceptually:

```text
temperature =
    broad_heat_field
  + regional_heat_bias
  - elevation_cooling

moisture =
    broad_moisture_field
  + regional_moisture_bias
  + local_variation
```

Because the wrapped world has no inherent equator, avoid assuming that `r == 0` represents a planetary equator unless that is an explicit world rule.

The first version should therefore use procedural broad heat zones rather than global latitude.

If Marajanda later requires latitude, world topology can add it as a separate layer.

### 16.1 Climate classification

Climate retains independent heat and moisture classifications. Do not use a single enum that mixes values such as cold, arid, and humid, because those properties are not mutually exclusive.

Example:

```go
type HeatBand uint8

const (
    HeatPolar HeatBand = iota
    HeatCold
    HeatTemperate
    HeatWarm
    HeatHot
)

type MoistureBand uint8

const (
    MoistureArid MoistureBand = iota
    MoistureDry
    MoistureModerate
    MoistureHumid
    MoistureSaturated
)

type Climate struct {
    Heat     HeatBand
    Moisture MoistureBand
}
```

The exact bands and thresholds may be tuned, but the two-axis representation is part of the public model. `Tile` also retains the normalized heat and moisture values from which these bands were classified.

---

## 17. Terrain

Terrain is a classification derived from physical fields.

It should not be generated as an independent random lookup.

Conceptually:

```text
terrain = f(
    elevation,
    slope-or-relief,
    temperature,
    moisture,
    inland-water influence,
    volcanic tendency,
    regional character,
    local variation,
)
```

The initial terrain vocabulary should be broad enough to produce a varied fantasy map without requiring every distinction to be implemented at once:

| Family | Suggested terrain types | Typical evidence |
|---|---|---|
| Ocean | deep ocean, ocean, shallow sea, coastal water | Elevation below sea level, depth, and adjacency to land |
| Inland water | inland sea, lake | Deterministic basin fields, basin scale, depth, and low local relief |
| Frozen | glacial ice, tundra | Low temperature, with elevation and moisture distinguishing persistent ice from tundra |
| Wetland | marsh, swamp, bog | Saturated moisture, low elevation, low relief, and temperature |
| Dry | desert, badlands, scrubland | Low moisture, heat, exposed relief, and regional character |
| Open land | plains, grassland, steppe, savanna | Moderate moisture and temperature, with regional variation |
| Forest | boreal forest, temperate forest, tropical rainforest or jungle | Sufficient moisture combined with the appropriate heat band |
| Elevated | hills, mountain, alpine terrain | Elevation, relief, slope, and temperature |
| Volcanic | volcano, volcanic highland | Strong volcanic tendency combined with uplift and concentrated relief |
| Coastal land | coast | Land near sea level with an adjacent ocean-water tile |

These are primary game-facing classifications. Elevation, relief, and climate should remain available so a game can render combinations such as forested hills, glaciated mountains, or a volcanic island without requiring a distinct terrain constant for every combination.

Classification rules should be ordered so exceptional terrain is not hidden by a broad biome rule. A reasonable precedence is:

```text
ocean and inland water
    -> glacial ice
    -> volcano
    -> mountain and alpine terrain
    -> wetland
    -> climate-driven land cover
```

Ocean water still comes from the primary elevation field and sea-level threshold. The first implementation should make a best effort to derive coherent depressions from deterministic basin fields at broad, regional, and local scales. Basin influence is useful even when no water is assigned: dry endorheic regions such as the Great Basin are valid geographic results.

Classify a basin as a lake or inland sea only if bounded local generation can give neighboring water tiles coherent membership, surface elevation, depth, and shorelines. The distinction between lake and inland sea is then based on generated basin scale and depth, not global connectivity or flood fill. If those invariants cannot be achieved simply and deterministically, omit inland-water terrain from the first implementation rather than emitting inconsistent per-tile water. A generated inland sea is a very large basin lake, not water proven to be disconnected from every ocean in the entire wrapped world.

Marsh and swamp should be distinguished primarily by climate and vegetation tendency: marshes favor open, saturated lowlands, while swamps favor warmer or forested saturated lowlands. Volcanoes should be rare products of regional volcanic tendency, uplift, and local peak structure rather than independent random tile assignments.

This makes terrain explainable and easier to tune.

---

## 18. Local Relief and Slope

Some terrain decisions require knowing whether the location is flat, hilly, or steep.

Slope can be estimated by sampling elevation at the six neighboring hexes.

See Directions in Appendix A for Axial neighbors.

For example:

```go
func (g *Generator) Relief(c Coord) float64
```

may compare the center elevation with neighboring elevations.

Because all tile generation is deterministic and stateless, sampling neighbors does not create a dependency problem.

Be careful to avoid recursive terrain classification if terrain generation itself asks for relief.

Preferred structure:

```text
raw fields
    -> elevation scalar
    -> neighbor elevation samples
    -> derived relief
    -> climate
    -> terrain
```

Do not call `Tile()` recursively from inside `Tile()`.

---

## 19. Public API

A minimal initial API:

```go
package wgva

type Seed uint64

type Component int16

type Coord struct {
    Q Component
    R Component
}

type Generator struct {
    // immutable configuration
}

func Normalize(q, r int64) Coord

func (c Coord) Neighbor(direction int) Coord

func New(seed Seed, opts ...Option) *Generator

func (g *Generator) Tile(c Coord) Tile

func (g *Generator) ElevationAt(c Coord) float64
```

`Normalize` is the entry point for unwrapped or intermediate coordinates and returns their canonical wrapped representative. Coordinate-producing operations such as `Neighbor` must normalize before returning. `Tile` also normalizes its input so a non-canonical `Coord` whose `q` and `r` fields are individually representable cannot create a second identity for the same tile.

Optional:

```go
func (g *Generator) Sample(c Coord) Sample
```

for diagnostics and visualization.

Do not expose chunk generation as the primary abstraction.

Applications should be able to ask directly for a tile.

---

## 20. Batch API

Rendering and simulation will often request rectangular or hexagonal groups.

A batch API can reduce repeated setup work.

Example:

```go
func (g *Generator) Tiles(coords []Coord) []Tile
```

or:

```go
func (g *Generator) Region(center Coord, radius int) []Tile
```

The batch API must produce exactly the same tile values as individual `Tile()` calls.

Batch generation is an optimization only.

---

## 21. Configuration

Use an immutable configuration structure.

Example:

```go
type Config struct {
    SeaLevel float64

    ContinentalScale float64
    RegionalScale    float64
    LocalScale       float64

    WarpScale    float64
    WarpStrength float64

    RegionSize int32
    ChunkSize  int32
}
```

Field names above are illustrative; the implemented configuration must include every weight, scale, threshold, and feature toggle that can alter generated output. Scale fields must document whether they are wavelengths or frequencies and which unit they use. Validation must reject non-finite floating-point values, non-positive sizes and scales, out-of-range normalized thresholds, and combinations that cannot be evaluated safely.

Provide stable defaults.

After a `Generator` is constructed, its configuration should not mutate.

This makes concurrent use safe and deterministic.

The complete effective configuration, including values supplied by defaults, is authoritative database data for the single world. A newly created database writes that complete configuration before gameplay; reopening never silently substitutes current program defaults for missing stored values.

Each algorithm version must define a canonical serialization for its effective configuration. Compute a stable fingerprint, such as SHA-256 over the algorithm version and canonical configuration bytes, for cache identity and diagnostics. Do not derive the fingerprint from Go map iteration, textual debug output, or a serialization with unspecified field ordering.

---

## 22. Concurrency

A configured `Generator` should be safe for concurrent read-only use.

Desired property:

```go
g := wgva.New(seed)

go g.Tile(a)
go g.Tile(b)
go g.Tile(c)
```

must be valid without external locking.

Avoid mutable shared PRNG state.

Caches, if added, must either:

- be concurrency-safe,
- be optional,
- or exist outside the core generator.

---

## 23. Chunks

Chunks are useful for callers and caches but should not define geography.

Recommended starting size:

```text
32 x 32 axial-addressed cells
```

The exact geometric interpretation of an axial chunk must be defined carefully.

The simplest implementation may use floor-division independently on `q` and `r`:

```go
chunkQ := floorDiv(q, ChunkSize)
chunkR := floorDiv(r, ChunkSize)
```

Use mathematical floor division, not Go integer truncation, because coordinates can be negative.

For example with chunk size `32`:

```text
q =   0 -> chunk  0
q =  31 -> chunk  0
q =  32 -> chunk  1

q =  -1 -> chunk -1
q = -32 -> chunk -1
q = -33 -> chunk -2
```

Implement and test this explicitly.

---

## 24. Negative Coordinates

Negative coordinates are first-class.

All helper math must behave correctly across the origin.

Particular attention is required for:

- floor division,
- modulo,
- region lookup,
- interpolation cells,
- chunk boundaries.

Do not assume `%` implements mathematical modulo for negative values in the desired way.

Provide helpers such as:

```go
func floorDiv(a, b int64) int64
func floorMod(a, b int64) int64
```

and test them exhaustively around zero.

---

## 25. Floating-Point Stability

Noise evaluation will likely use `float64`.

To maximize repeatability:

- use `float64` consistently,
- avoid architecture-dependent fast-math behavior,
- avoid accumulating values in nondeterministic iteration order,
- keep formulas explicit,
- avoid reducing over Go maps.

Golden tests should verify representative coordinates.

Bit-for-bit reproducibility across all architectures is desirable, but if a chosen third-party noise library cannot guarantee it, document the limitation.

Classification thresholds should avoid pathological sensitivity to microscopic floating-point differences.

---

## 26. Caching

The generator must work correctly with no cache.

Applications may cache:

- individual tiles,
- elevation samples,
- region parameter blocks,
- rendered chunks.

Region parameter caching is likely the best initial optimization because many nearby tiles reuse the same regional anchors.

Suggested internal cache key:

```go
type RegionCoord struct {
    Q int32
    R int32
}
```

Caching should be introduced only after profiling.

Do not make generated world correctness depend on cache history.

---

## 27. Persistence and Versioning

Generated terrain is a function of:

```text
generator version
+ seed
+ configuration
+ coordinates
```

A WGVA database contains exactly one world and must contain everything needed to reproduce that world's generated baseline.

WGVA should expose a generator algorithm version.

Example:

```go
const AlgorithmVersion = 1
```

The database must persist in singleton world metadata:

```text
world seed
algorithm version
complete effective generator configuration
configuration fingerprint
```

Changing noise formulas, thresholds, or hash domains can change existing worlds.

Treat such changes as generation-version changes unless compatibility is intentionally preserved.

Use `zombiezen.com/go/sqlite` for SQLite access and `zombiezen.com/go/sqlite/sqlitemigration` for ordered schema migrations. Do not use a `database/sql` SQLite driver. Every database uses:

```go
const databaseApplicationID int32 = 0x57475641 // ASCII "WGVA"
```

Set this through `sqlitemigration.Schema.AppID`. The binary must reject a non-empty database whose `PRAGMA application_id` does not equal this value before applying migrations or performing application writes.

`sqlitemigration` owns `PRAGMA user_version` as the schema migration version. SQLite does not provide application-defined pragmas, so store the generator algorithm version in the singleton world metadata rather than attempting `PRAGMA generator_version`.

Database opening has explicit compatibility gates:

```text
1. Verify PRAGMA application_id is WGVA.
2. Read PRAGMA user_version and reject schemas newer than the binary.
3. Apply supported ordered schema migrations.
4. Read and validate the singleton world metadata and complete configuration.
5. Reject a generator version the binary cannot reproduce.
6. Permit normal reads and writes.
```

Older generator versions are rejected unless the binary deliberately retains their implementations. A generator incompatibility must never be handled by silently regenerating the world with current rules.

Persist mutable game and player state as sparse overlays keyed by canonical `(q, r)` coordinates. No `world_id` is needed because one database contains one world. Generated tiles, chunks, and PNGs are reproducible caches rather than authoritative records and may be discarded. Cache entries must carry or be invalidated against the configuration fingerprint and any relevant rendering/palette version.

---

## 28. Suggested Package Layout

One possible layout:

```text
wgva/
    coord.go
    generator.go
    config.go
    tile.go

    hash.go

    field.go
    noise.go
    warp.go

    elevation.go
    climate.go
    terrain.go
    relief.go

    region.go
    chunk.go

    internal/
        noise/
        mathx/

    cmd/
        wgva-map/
```

A small diagnostic command is strongly recommended.

---

## 29. Diagnostic Renderer

Create a command such as:

```text
cmd/wgva-map
```

It should generate a bounded image of an arbitrary window into the effectively unbounded wrapped world.

Example:

```bash
wgva-map \
    -db world.wgva \
    -q -200 \
    -r -150 \
    -cols 400 \
    -rows 300 \
    -hex-radius 8 \
    -layer terrain \
    -out map.png
```

The database supplies the seed, generator version, and effective configuration. If this command supports creating a new database, creation must write all of those values before rendering; it must not override them when opening an existing database. `cols` and `rows` are tile counts, while `hex-radius` is a pixel dimension.

During phases before persistence exists, the same rendering code may be driven by an explicitly constructed in-memory generator as a development harness. Such output is diagnostic and does not represent a saved world. Player-facing rendering must always load its effective configuration from the database.

Useful layers:

```text
elevation
continentalness
temperature
moisture
relief
climate
terrain
region influence
```

This tool is essential for tuning procedural generation.

The renderer is bounded; generation does not require callers to choose or approach the world's enormous radius.

---

## 30. Testing Strategy

Testing procedural generation should focus on invariants rather than whether a map "looks right."

### 30.1 Determinism

```go
func TestTileDeterministic(t *testing.T)
```

Generate the same tile repeatedly and compare all values.

### 30.2 Order Independence

Generate a set of coordinates in different orders and verify identical results.

### 30.3 Concurrent Determinism

Generate the same large coordinate set serially and concurrently.

Results must match.

### 30.4 Negative Coordinates

Test coordinates around:

```text
(-1, -1)
(-64, -64)
(-65, -65)
(0, 0)
(63, 63)
(64, 64)
```

Also test canonical coordinates and operations near all six wrapped edges. Use `int64` inputs that cross each edge and corner, and verify normalization, neighbor reciprocity, alias identity, and safe intermediate arithmetic.

### 30.5 Region Boundary Continuity

Sample long lines crossing region boundaries.

The boundary itself should not create a statistical jump.

### 30.6 Chunk Boundary Continuity

Likewise verify that chunk borders are invisible in raw fields.

### 30.7 Neighbor Coherence

Adjacent tiles should, statistically, be more similar than widely separated tiles.

This can be tested over a large sample.

### 30.8 Distribution Tests

For a large deterministic sample, check broad expectations such as:

- land fraction within an acceptable range,
- all climate types occur if intended,
- elevation distribution is plausible,
- terrain classification does not collapse into one dominant type.

Use broad tolerances so tests verify gross regressions rather than freeze aesthetic tuning.

### 30.9 Golden Coordinates

Maintain a small table:

```text
seed
q
r
expected elevation
expected climate
expected terrain
```

Golden tests detect accidental world changes.

Changing them should be an explicit generation-version decision.

### 30.10 Wrapped-edge continuity

Compare physical fields on corresponding tiles at all six wrapped edge pairs. Prefer the same continuity expectations used for ordinary neighbors. If exact periodicity is not implemented for a field, record the known warp seam explicitly rather than weakening unrelated continuity tests.

### 30.11 Database compatibility

Verify that opening rejects a non-WGVA application ID, a schema newer than the binary, an unsupported generator version, malformed or incomplete singleton metadata, and an invalid configuration without applying application writes. Verify that supported older schemas migrate in order and retain the same single-world metadata.

---

## 31. Performance Expectations

Tile generation should be cheap enough for interactive scrolling.

A reasonable first target on commodity hardware is:

```text
tens of thousands of tiles per second
```

but correctness and visual quality come first.

Benchmark:

```go
func BenchmarkTile(b *testing.B)
func BenchmarkChunk(b *testing.B)
```

Measure before adding caches or complexity.

---

## 32. Initial Implementation Plan

### Phase 1 — Coordinate and hashing foundation

Implement:

- `Coord`
- arithmetic wraparound normalization
- checked `hexg` conversion
- axial-to-world conversion
- floor division helpers
- deterministic domain-separated hashing
- generator/config skeleton

Exit condition:

> Coordinate math, six-edge wrapping, `hexg` conversion, configuration validation, and deterministic hashing have complete unit tests.

### Phase 2 — Continuous scalar fields and diagnostic renderer

Implement:

- one stable 2D noise source,
- octave/fractal composition,
- domain warping,
- diagnostic scalar sampling,
- an initial `cmd/wgva-map` capable of rendering scalar layers.

Exit condition:

> Arbitrary canonical coordinates can be sampled and inspected visually. Ordinary field sampling has no seams; wrapped-edge continuity is implemented on a best-effort basis and any remaining warp seam is documented.

### Phase 3 — Hierarchical region influence

Implement:

- deterministic region parameters,
- interpolation/blending across regional anchors,
- regional roughness, climate, basin, and elevation biases.

Exit condition:

> Different large areas have distinct geographic character, with no visible implementation-region boundaries.

### Phase 4 — Elevation

Implement:

- continentalness,
- regional uplift,
- local relief,
- sea level,
- elevation scalar,
- land/water classification.

Exit condition:

> Diagnostic elevation maps show coherent oceans, coastlines, lowlands, and uplands across multiple windows.

### Phase 5 — Climate

Implement:

- heat field,
- elevation cooling,
- moisture field,
- regional climate bias,
- climate classification.

Exit condition:

> Climate maps form coherent broad zones rather than tile-level speckle.

### Phase 6 — Basins and terrain

Implement:

- broad, regional, and local basin influence,
- coherent inland water if it satisfies the invariants in section 17,
- terrain classification from physical fields,
- terrain layers in the diagnostic renderer.

Exit condition:

> Terrain maps visually correspond to elevation and climate, and boundaries appear geographically plausible. Basin geography exists; inland water is either coherent or deliberately omitted.

### Phase 7 — Persistence, player rendering, and tuning

Implement:

- the single-world ZombieZen SQLite database,
- ordered schema migrations and all compatibility gates,
- canonical configuration persistence and fingerprinting,
- player-facing terrain and overlay PNG composition.

Tune frequencies, weights, thresholds, and warp strengths using the renderer throughout the preceding phases, then settle the defaults stored for the first algorithm version.

Exit condition:

> A database can create, reopen, and reproduce one world safely. Multiple seeds and distant coordinate windows produce varied but coherent maps and player PNGs.

---

## 33. Avoid These Designs

### 33.1 Independent random tile classification

Do not derive terrain directly from:

```text
hash(seed, q, r) % terrainCount
```

That recreates the original incoherent appearance.

### 33.2 Finite global heightmaps

Do not generate a fixed `width x height` array and normalize it.

That makes the world bounded.

### 33.3 Mutable PRNG traversal

Do not make tile values depend on the order tiles were generated.

### 33.4 Region-owned terrain

Do not assign every tile inside a region one set of hard parameters without blending.

That creates seams.

### 33.5 Runtime global normalization

Do not compute min/max elevation or histogram thresholds from the currently explored area.

Exploration order would change the world.

---

## 34. Future Extensions

The architecture should leave room for the following.

### 34.1 Rivers

Possible future approach:

- derive deterministic watershed structure from a coarser hydrology field,
- trace river paths locally from stable source features,
- ensure path generation can be reconstructed from coordinates.

Rivers are deliberately deferred because globally coherent drainage is substantially harder than scalar field generation.

### 34.2 Biomes

Terrain classification can evolve into richer biome classification.

### 34.3 Resources

Resources can use the same hierarchical deterministic path approach:

```text
seed
+ resource domain
+ region
+ coordinate
```

### 34.4 Named geographic features

Macro regions can provide deterministic identities for:

- mountain systems,
- deserts,
- forests,
- seas.

Names should be a separate layer from physical generation.

### 34.5 Alternative world topology

The current design uses the finite hexagonal wraparound topology in section 7.1. Another generator version could introduce a cylinder, torus, sphere-like topology, or a differently sized wrapped world. Coordinate normalization must remain a distinct layer from physical fields and classification so such a change is possible, but topology is an algorithm compatibility decision and cannot change for an existing world.

---

## 35. Coding-Agent Guidance

When implementing this design:

1. Prefer small deterministic functions over stateful generator steps.
2. Keep raw scalar fields separate from classification.
3. Expose diagnostic sampling early.
4. Build the renderer before extensive aesthetic tuning.
5. Test negative coordinates from the beginning.
6. Do not optimize before profiling.
7. Do not introduce persistence into the core package.
8. Treat changes that alter existing generated worlds as versioned algorithm changes.
9. Keep all geographic boundaries emergent from fields; implementation regions and chunks must remain invisible.
10. Preserve the central invariant:

```text
Tile = F(seed, q, r, algorithmVersion, configuration)
```

with no dependency on generation order or previously generated tiles.

---

## 36. Definition of Success

The first major WGVA milestone is successful when all of the following are true:

- A caller can request any axial coordinate `(q, r)`.
- The fixed wrapped boundary is not encountered as a terminal gameplay edge.
- The returned tile contains normalized elevation, heat, moisture, and relief values plus elevation, climate, and terrain classifications.
- The same seed and coordinate always return the same tile.
- Adjacent tiles form visually coherent geographic features.
- Large-scale terrain differs across distant portions of the map.
- Region and chunk boundaries cannot be identified by looking at generated terrain.
- Negative coordinates work correctly.
- All six world edges wrap correctly; any best-effort geographic discontinuity is documented as a world-warp seam.
- The world can be rendered in arbitrary windows for inspection.
- Generating a distant tile does not require generating the intervening world.

At that point WGVA will retain the operational simplicity of the original Marajanda coordinate-path generator while producing terrain with the visual coherence of a modern noise-based map.

---

## Appendix A

### Direction Vectors

The six canonical directions are numbered `0` through `5`. Increasing the direction by one moves clockwise to the next neighbor; decreasing it by one moves counter-clockwise. This cyclic ordering is independent of whether a renderer draws flat-top or pointy-top hexes.

Callers may supply any integer direction. Normalize it to the range `[0, 5]` before using it as an index. In Go, `%` computes a remainder with the same sign as the dividend, so `-7 % 6` is `-1`, not `5`. A negative remainder therefore requires adjustment:

```go
func normalizeDirection(dir int) int {
    dir %= 6
    if dir < 0 {
        dir += 6
    }
    return dir
}
```

Values that differ by a multiple of six identify the same direction. For example:

| Input | Normalized | Movement from direction 0 |
|---:|---:|---|
| `7` | `1` | One step clockwise |
| `6` | `0` | Full turn clockwise |
| `-1` | `5` | One step counter-clockwise |
| `-2` | `4` | Two steps counter-clockwise |
| `-6` | `0` | Full turn counter-clockwise |
| `-7` | `5` | Full turn plus one step counter-clockwise |

| Direction | Cube Vector  | Axial Vector |
| --------- | ------------ | ------------ |
|         0 | (+1,  0, -1) | (+1,  0)     |
|         1 | (+1, -1,  0) | (+1, -1)     |
|         2 | ( 0, -1, +1) | ( 0, -1)     |
|         3 | (-1,  0, +1) | (-1,  0)     |
|         4 | (-1, +1,  0) | (-1, +1)     |
|         5 | ( 0, +1, -1) | ( 0, +1)     |
