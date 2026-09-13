# WGVA — Unbounded Procedural World Generator Design

**Module:** `github.com/mdhender/wgva`  
**Target language:** Go  
**Coordinate system:** axial hex coordinates `(q, r)`  
**Hex scale:** 3-mile apothem
**World origin:** `(0, 0)`  
**Primary goal:** Generate attractive, geographically coherent terrain on demand without requiring a finite map boundary.

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

The implementation should avoid a fixed rectangular or circular world boundary.

The world is therefore **unbounded in address space**, while its large-scale geography is generated from deterministic continuous fields and hierarchical regions.

---

## 2. Design Goals

The generator should satisfy the following goals.

### 2.1 Deterministic

For a given world seed and tile coordinate, generated attributes must always be identical.

```go
Generate(seed, q, r) == Generate(seed, q, r)
```

Generation order must not affect results.

### 2.2 Unbounded

No API should require world width, height, radius, or bounding rectangle.

Any valid axial coordinate should be generatable, subject only to integer limits.

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

Because the world is unbounded, global statements such as "the world is exactly 58% land" are not meaningful. The generator may instead target statistical properties over sufficiently large samples.

---

## 4. Core Model

A tile is uniquely identified by its axial coordinate.

```go
type Coord struct {
    Q int64
    R int64
}
```

Use signed 64-bit coordinates unless there is a compelling implementation reason not to.

The origin is:

```text
(0, 0)
```

No special terrain meaning is assigned to the origin unless configured explicitly.

### 4.1 Tile

The minimum public tile representation is:

```go
type Tile struct {
    Coord     Coord
    Elevation Elevation
    Climate   Climate
    Terrain   Terrain
}
```

The exact underlying types may evolve, but elevation, climate, and terrain are required attributes.

The generator may also expose intermediate values for diagnostics.

Example:

```go
type Sample struct {
    Coord Coord

    ElevationValue float64
    MoistureValue  float64
    HeatValue      float64

    Continentalness float64
    Relief          float64

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
1. Convert axial coordinate to continuous world-space position.
2. Evaluate macro-scale fields.
3. Determine hierarchical regional influences.
4. Evaluate medium- and local-scale detail.
5. Combine fields into normalized physical values.
6. Classify elevation.
7. Classify climate.
8. Classify terrain.
9. Return immutable tile data.
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

---

## 8. Coordinate-Based Randomness

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

Land-versus-water should be determined by comparing elevation against a fixed sea-level threshold.

Example:

```text
elevation <= 0 -> water
elevation >  0 -> land
```

Sea level may be configurable.

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

Because an unbounded plane has no inherent equator, avoid assuming that `r == 0` represents a planetary equator unless that is an explicit world rule.

The first version should therefore use procedural broad heat zones rather than global latitude.

If Marajanda later requires latitude, world topology can add it as a separate layer.

### 16.1 Climate Type

A categorical climate can be derived from heat and moisture.

Example:

```go
type Climate uint8

const (
    ClimatePolar Climate = iota
    ClimateCold
    ClimateTemperate
    ClimateWarm
    ClimateHot
    ClimateArid
    ClimateHumid
)
```

This enumeration is illustrative only.

A two-axis classification may be superior:

```go
type Climate struct {
    Heat     HeatBand
    Moisture MoistureBand
}
```

The design should favor retaining information rather than prematurely compressing it into one enum.

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
    regional character,
    local variation,
)
```

Examples:

```text
water + deep elevation                  -> deep ocean
water + near sea level                  -> coastal water
land + high elevation                   -> mountain
land + cold + moderate moisture         -> tundra
land + hot + dry                        -> desert
land + temperate + wet                  -> forest
land + moderate moisture                -> grassland
low elevation + very wet                -> marsh
```

This makes terrain explainable and easier to tune.

---

## 18. Local Relief and Slope

Some terrain decisions require knowing whether the location is flat, hilly, or steep.

Slope can be estimated by sampling elevation at the six neighboring hexes.

Axial neighbors:

```text
(+1,  0)
(+1, -1)
( 0, -1)
(-1,  0)
(-1, +1)
( 0, +1)
```

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

type Coord struct {
    Q int64
    R int64
}

type Generator struct {
    // immutable configuration
}

func New(seed Seed, opts ...Option) *Generator

func (g *Generator) Tile(c Coord) Tile

func (g *Generator) ElevationAt(c Coord) float64
```

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

    RegionSize int64
    ChunkSize  int64
}
```

`New()` should validate configuration.

Provide stable defaults.

After a `Generator` is constructed, its configuration should not mutate.

This makes concurrent use safe and deterministic.

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
    Q int64
    R int64
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

A saved game therefore needs to know which generation rules produced its world.

WGVA should expose a generator algorithm version.

Example:

```go
const AlgorithmVersion = 1
```

Applications should persist:

```text
world seed
algorithm version
relevant generator configuration
```

Changing noise formulas, thresholds, or hash domains can change existing worlds.

Treat such changes as generation-version changes unless compatibility is intentionally preserved.

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

It should generate a bounded image of an arbitrary window into the otherwise unbounded world.

Example:

```bash
wgva-map \
    -seed 12345 \
    -q -200 \
    -r -150 \
    -width 400 \
    -height 300 \
    -layer terrain \
    -out map.png
```

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

The renderer is bounded; the world generator is not.

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
- axial-to-world conversion
- floor division helpers
- deterministic domain-separated hashing
- generator/config skeleton

Exit condition:

> Coordinate math and deterministic hashing have complete unit tests.

### Phase 2 — Continuous scalar fields

Implement:

- one stable 2D noise source,
- octave/fractal composition,
- domain warping,
- diagnostic scalar sampling.

Exit condition:

> Arbitrary coordinates can be sampled with no seams or bounds.

### Phase 3 — Elevation

Implement:

- continentalness,
- regional uplift,
- local relief,
- sea level,
- elevation scalar,
- land/water classification.

Exit condition:

> Diagnostic elevation maps show coherent oceans, coastlines, lowlands, and uplands across multiple windows.

### Phase 4 — Hierarchical region influence

Implement:

- deterministic region parameters,
- interpolation/blending across regional anchors,
- regional roughness and elevation biases.

Exit condition:

> Different large areas have distinct geographic character, with no visible region boundaries.

### Phase 5 — Climate

Implement:

- heat field,
- elevation cooling,
- moisture field,
- regional climate bias,
- climate classification.

Exit condition:

> Climate maps form coherent broad zones rather than tile-level speckle.

### Phase 6 — Terrain

Implement terrain classification from physical fields.

Exit condition:

> Terrain maps visually correspond to elevation and climate, and boundaries appear geographically plausible.

### Phase 7 — Renderer and tuning

Add `cmd/wgva-map`.

Tune frequencies, weights, thresholds, and warp strengths.

Exit condition:

> Multiple seeds and distant coordinate windows produce varied but coherent maps.

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

### 34.5 World topology

The current design describes an infinite plane.

If later desired, another world implementation could map axial coordinates onto:

- a cylinder,
- torus,
- sphere-like topology,
- finite wrapped world.

The tile classification pipeline should be kept sufficiently modular that topology and geography remain separable concepts.

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
- No finite world dimensions are configured.
- The returned tile contains elevation, climate, and terrain.
- The same seed and coordinate always return the same tile.
- Adjacent tiles form visually coherent geographic features.
- Large-scale terrain differs across distant portions of the map.
- Region and chunk boundaries cannot be identified by looking at generated terrain.
- Negative coordinates work correctly.
- The world can be rendered in arbitrary windows for inspection.
- Generating a distant tile does not require generating the intervening world.

At that point WGVA will retain the operational simplicity of the original Marajanda coordinate-path generator while producing terrain with the visual coherence of a modern noise-based map.
