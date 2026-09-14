# WGVA — Effectively Unbounded Procedural World Generator Design

**Module:** `github.com/mdhender/wgva`
**Target language:** Go (1.26)
**Coordinate system:** axial hex coordinates `(q, r)`
**Component width:** signed 16-bit for alpha, signed 32-bit for the shipping world
**Hex scale:** 3-mile apothem
**World origin:** `(0, 0)`
**Primary goal:** Generate attractive, geographically coherent terrain on demand without exposing a practical map boundary.

WGVA is the Go generator. A second generator, WGVB, was written in Rust against
an earlier revision of this document and carried through seven implementation
phases to a working world. This revision folds back what that implementation
learned. It is a new world format, not a port that can read WGVB files: the
application id, the module name, and the algorithm version namespace are all
distinct.

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

The implementation uses a finite but enormous wrapped hexagonal address space. This is described as "unbounded" to players because normal play never encounters a terminal edge. Its large-scale geography is generated from deterministic continuous fields and hierarchical regions.

### 1.1 What the Rust implementation settled

Five findings from WGVB changed this document, and each is worked through where
it belongs. They are collected here because they are the reason to re-read a
document that was already written once.

1. **The terrain tuning tool was the most valuable artifact, and it was built
   too late.** It runs on a developer's machine, produces a settings file, and
   needs no database, no player, and no deployment. It is now phase 2 —
   everything player-facing and everything persistent is deferred behind it.
   See sections 29.1 and 32.

2. **32 bits is the shipping coordinate width, and 16 bits is the alpha one.**
   The width is one centralized decision, not a property of the algorithms. WGVB
   ran its whole implementation at `int16` deliberately, because a 3.2-billion-
   tile world is one whose edges a test can actually reach, and it shipped
   `int32` as the answer for a world players live in. WGVA does the same, in the
   same order. 64-bit components are explicitly rejected: some internal
   arithmetic needs 64 bits and some needs 128, but nothing a player ever reads
   does. See sections 4 and 7.2.

3. **Only the administrator is hyper-focused on performance**, and only when
   viewing large maps or iterating on a configuration. There is no player-facing
   throughput target in this document and there should not be one. What is
   measured is a *render*, not a tile. See section 31.

4. **The tuning and octave rules below were established by measurement and are
   not to be re-litigated without one.** The Nyquist bound on the octave ladder
   (9.3), the seed-derived per-domain sampling offset (9.4), barycentric
   blending on the triangular anchor lattice (11.2), doubled-angle blending of
   ridge orientation (12), basin influence entering terrain as a product and
   never entering elevation (17.1), and the pooled-seed origin test (30.13) all
   came from something looking wrong in a render and being chased down. Each
   carries the measurement that justifies it.

5. **The wrap is cheap and it looks interesting, and it is still better covered
   than exposed.** The wrapped topology stays — it is what makes normalization
   total and tile identity single-valued. But the last band of hexes before the
   seam is forced to deep ocean (or polar ice) and marked impassable, so the
   discontinuity is never rendered and never entered. This also retires a debt:
   the fields no longer owe periodicity across the seam. See section 15.1.

---

## 2. Design Goals

### 2.1 Deterministic

For a given world seed and tile coordinate, generated attributes must always be identical.

```go
g.Tile(c) == g.Tile(c)
```

Generation order must not affect results.

### 2.2 Effectively unbounded and wrapped

No API requires world width, height, radius, or bounding rectangle.

Canonical coordinates form a hexagonal map whose radius is derived from the component width (section 4). Coordinate operations that leave that map wrap to the corresponding tile on the opposite edge using the scheme in section 7.1. The outermost band of that map is the rim of section 15.1: generated, addressable, and closed to play.

### 2.3 Local

Generating tile `(q, r)` requires only a bounded amount of nearby procedural context.

Generation must not require:

- generating every tile between `(0,0)` and `(q,r)`,
- scanning the entire world,
- global normalization,
- global flood fill,
- a precomputed heightmap.

### 2.4 Geographically coherent

Nearby tiles normally have related elevation, climate, and terrain.

Large features cross implementation chunk and region boundaries without visible seams.

### 2.5 Reproducible across platforms

The same seed and coordinates must produce bit-identical results on every supported target.

Go can deliver this, but only under the arithmetic restrictions in section 25, and Go's default behavior works against one of them. **Read section 25 before writing any floating-point code.**

Output must never depend on:

- map iteration order,
- platform floating-point library differences,
- whether the target fuses a multiply-add,
- goroutine scheduling,
- mutable random-number-generator state,
- compiler or dependency version, within a fixed algorithm version.

### 2.6 Stateless by default

The core generator requires no persistent storage. In Go this is structural rather than advisory: `store`, `render`, and `config` all import `wgva`, so `wgva` cannot import any of them — the compiler rejects the cycle. See section 28.

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

---

## 4. Core Model

A tile is uniquely identified by its canonical axial coordinate.

```go
// Component is the stored width of one axial coordinate component. It is the
// single place the world's size is decided.
type Component = int16

// WorldRadius is N in section 7.1: the largest value a Component can hold.
// It is math.MaxInt16 at the alpha width and math.MaxInt32 at the shipping
// width, and it is paired with Component rather than chosen independently.
const WorldRadius int64 = math.MaxInt16
```

Go cannot compute the maximum of a signed type in a constant expression without
`unsafe`, which section 22 forbids, so the pair is written out and pinned by a
test rather than derived:

```go
func TestWorldRadiusMatchesComponent(t *testing.T) {
    if int64(Component(WorldRadius)) != WorldRadius {
        t.Fatal("WorldRadius does not fit in a Component")
    }
    if int64(Component(WorldRadius+1)) == WorldRadius+1 {
        t.Fatal("WorldRadius is smaller than the Component can hold")
    }
}
```

That is four lines and it fails the instant one half of the pair moves without
the other, which is the only failure mode worth defending against here.

**`Coord`'s fields are unexported and there is no exported constructor that
skips normalization.**

```go
// Coord is a canonical axial coordinate.
type Coord struct {
    q Component
    r Component
}
```

This is the single most important structural change from the previous revision
of this document, which could only say "values that normalize to the same
coordinate identify the same tile" and hope. A non-canonical `Coord` cannot be
constructed outside the package, so `==`, use as a map key, and `slices.Sort`
over a `[]Coord` are automatically correct for tile identity, persistence keys,
region lookup, and set membership.

Go gives this more cheaply than it looks, and one detail makes it work: **the
zero value `Coord{}` is the origin, which is canonical.** There is no invalid
zero value to defend against and no `IsValid` to forget to call.

The only ways to obtain a `Coord`:

```go
// NewCoord normalizes any axial pair into the canonical wrapped map.
func NewCoord(q, r int64) Coord

// Origin is the canonical origin, equal to the zero value.
var Origin Coord

func (c Coord) Q() Component
func (c Coord) R() Component

// S is the derived cube component, -q - r. Always in Component range for a
// canonical coordinate.
func (c Coord) S() Component
```

Compute `s`, mirror centers, differences, and every other intermediate with
`int64`; the one place that must widen further is named in section 7.1. Not
every pair of in-range `q` and `r` has an `s` within `±WorldRadius`;
normalization handles that, and the conversion back to `Component` is guarded by
a range check against `±WorldRadius` — not against the `Component` type's own
range, which is wider on the negative side (section 4.1). The panic documents an
invariant rather than hoping for one.

### 4.1 The canonical domain is symmetric

`Component` is a two's-complement type, so it can represent one more negative
value than positive: `math.MinInt16` at the alpha width, `math.MinInt32` at the
shipping width. **That value is not a coordinate.** The canonical domain is

```text
-WorldRadius <= q, r, s <= +WorldRadius
```

so at the alpha width every component satisfies `-32767 <= v <= 32767` and the
map is exactly symmetric about the origin. `NewCoord` never produces the extreme
negative value, the conversion from `int64` to `Component` rejects it, and a
stored one is malformed data rather than a distant tile.

**No tiles are lost by this.** The domain is the hexagon
`max(|q|, |r|, |s|) <= N` of section 7.1, which never contained the extreme
value in the first place, so the count in section 7.2 is unchanged and the
constraint costs nothing. What it buys is that three operations become **total**,
and all three are used in the generation path:

- **Negation.** `-c` is always representable, so the six-fold rotation of
  appendix A, reflection, and every symmetry of the domain map it onto itself
  with no special case. In Go, `-math.MinInt16` silently returns
  `math.MinInt16`.
- **Absolute value.** `|math.MinInt16|` is negative in two's complement. The rim
  test of section 15.1 is `WorldRadius - max(|q|, |r|, |s|)`, so a component at
  the extreme value contributes a *negative* magnitude: it loses the comparison
  it ought to win, and a tile at the very edge of the map is reported as
  interior. The rim never fires for it, and nothing overflows anywhere to catch
  the mistake.
- **Round-tripping through `int64`.** `int64(Component(v))` is the identity for
  every valid `v`, which is what lets the normalizer prove a value in range once
  and convert without re-checking.

The same constraint applies verbatim at the shipping width —
`-2147483647 <= q, r, s <= 2147483647`, with `math.MinInt32` excluded — and
because both bounds are `±WorldRadius`, nothing in the code changes when the
width does.

> This is the one place where the set of values a `Coord` may *hold* is smaller
> than the set its fields can *represent*, which is why the range check in the
> normalizer is a real assertion rather than a formality. It is also why the
> check cannot be skipped on the grounds that the value "came from a
> `Component` already". Test it directly; see section 30.4.

### 4.2 The component width is one decision, made twice

`Component` is `int16` for the alpha and `int32` for the shipping world. Both
widths are supported by the same code, and **the width appears in exactly two
places**: the `Component`/`WorldRadius` pair above, which is one decision, and
the compatibility tests. Nothing else may name a width — not a struct field, not
a SQLite column type, not a hash input, not a bounds check. A literal `32767`
anywhere outside those two places is a defect.

The reason to run the alpha at `int16` is that a 3.2-billion-tile world has
edges a test can reach:

- A scroll walk from the origin to the rim and back is a few thousand steps, so
  the wrap, the rim profile, and the normalizer's stage-2 fix-up are all covered
  by tests that finish in milliseconds rather than by argument.
- The whole world is 65,535 hexes across, so `render.Grid` can draw a picture of
  all of it at a coarse scale. At `int32` no image of the whole world exists at
  any scale that fits in memory, and "does the rim look right everywhere" stops
  being a question anybody can answer by looking.
- Distribution tests (30.8) over a sample that is a meaningful fraction of the
  world are affordable.

The reason to ship at `int32` is that the rim stops being reachable at all. See
the table in section 7.2.

> **Changing the width changes world topology and is an algorithm version
> change.** A world generated at `int16` cannot be reopened by a binary built at
> `int32`, and the gate in section 27.5 must refuse it rather than reinterpret
> the coordinates. The width is therefore recorded in the singleton world
> metadata alongside the algorithm version, and the compatibility test asserts
> that a file from the other width is rejected — the same way a WGVB file is.

> **64-bit components are rejected, not deferred.** Widening `Component` to
> `int64` multiplies the tile count by `4e18` and buys nothing a player can
> perceive — they would still never reach the rim, and every coordinate they
> read would grow to twenty digits. The costs are real: the lattice solve in
> section 7.1 would need 256-bit intermediates, the database would carry eight
> bytes per component instead of two or four in every overlay row, and the rim
> test would no longer fit in a register pair. Section 34.5 is where a genuinely
> different topology would go; this is not it.

### 4.3 Tile

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

    // Rim reports that this tile is inside the world rim: forced terrain,
    // closed to play. See section 15.1.
    Rim bool
}
```

`Tile` is a plain value with no pointers, small enough that batch APIs can fill a `[]Tile` with no allocation and no indirection. A test asserts its size with `reflect.TypeOf(Tile{}).Size()`; see section 30.12.

The physical values and their elevation, climate, and terrain classifications are part of the ordinary tile result. Games and renderers must not need a diagnostic API to recover them.

The generator may also expose intermediate values for diagnostics:

```go
type Sample struct {
    Coord Coord
    World Vec2

    // The four noise scales of section 10, before any composition.
    Continentalness float64
    Regional        float64
    Local           float64
    Detail          float64

    // The ridge structure term, before the region roughness scales it.
    Ridge float64

    // The weighted sum of the four noise scales alone.
    ElevationRaw float64

    // Blended region parameters, carried so a tuning layer can be drawn.
    RegionalUplift float64
    Roughness      float64

    // The classified scalars, bit-identical to the matching Tile fields.
    Elevation      float64
    Heat           float64
    Moisture       float64
    BasinInfluence float64
    Volcanic       float64

    // Hexes from the outer edge of the map; 0 on the outermost ring.
    // Section 15.1.
    RimDistance int64
}
```

Diagnostic values are not required for ordinary game use.

**A `Sample` does not carry a `Tile`, and that is the shape rather than an
omission.** A sample is one elevation evaluation; relief and terrain are seven,
because both read the six neighboring elevation scalars. Folding a tile into a
sample would make every diagnostic layer pay a terrain classification it is not
drawing, which is exactly the distinction `Layer.Cost` in `render` is built on. A
caller that wants both asks for both.

---

## 5. Recommended Architecture

WGVA uses four conceptual layers:

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
7. Apply the rim profile.
8. Classify elevation.
9. Classify climate.
10. Classify terrain.
11. Return immutable tile data with its canonical coordinate.
```

Conceptually:

```go
func (g *Generator) Tile(c Coord) Tile
```

is sufficient for callers. Note the pointer receiver on an immutable value: generation never mutates the generator. See section 22.

---

## 7. Hex Coordinates and World Space

WGVA uses axial hex coordinates `(q, r)`.

Axial coordinates define tile identity and adjacency, not how hexes must be drawn. Flat-top versus pointy-top orientation is a rendering and layout choice; it is not part of the generator's public model. A renderer may choose either orientation without changing generated tile data.

Noise functions operate on Cartesian coordinates, so axial coordinates are converted to continuous 2D world space before sampling.

Each regular hex has a 3-mile apothem. Therefore:

```text
neighboring center distance = 6 miles
flat-to-flat width          = 6 miles
point-to-point width        = 4 * sqrt(3) miles, approximately 6.93 miles
area                        = 18 * sqrt(3) square miles, approximately 31.18 square miles
```

Canonical world-space coordinates are measured in miles, in `float64`.

One convenient pointy-top embedding:

```text
x = 6 * (q + r/2)
y = 3 * sqrt(3) * r
```

This places adjacent hex centers exactly 6 miles apart. Any equivalent embedding is acceptable if it preserves that distance and uses miles. The chosen embedding is an internal detail but must remain stable wherever deterministic compatibility is promised, so it is pinned by the algorithm version.

Centralize the conversion:

```go
type Vec2 struct {
    X float64
    Y float64
}

func AxialToWorld(c Coord) Vec2
```

All continuous fields sample from this one coordinate system. This avoids the distortion caused by feeding `q` and `r` directly into Cartesian noise.

> **World space is `float64` at every scale, and at the shipping width that
> matters.** The far corner of an `int32` map is about `2.6e10` miles from the
> origin. A `float64` has 52 bits of mantissa, so the spacing of representable
> values there is about `4e-6` miles — roughly a quarter of an inch against a
> 6-mile hex. Ample headroom, but only in `float64`: in `float32` the spacing at
> that distance is about 2,000 miles, which is 300 hexes, and every tile in the
> outer world would collapse onto its neighbors. Nothing that touches generation
> may compute a world position in `float32`. This is a defect the alpha width
> would hide completely and the shipping width would expose everywhere, which is
> the argument for stating it now.

### 7.1 Wrapped coordinate domain

Let `N = WorldRadius`. The canonical map is the hexagonal cube-coordinate domain:

```text
-N <= q <= +N
-N <= r <= +N
-N <= s <= +N
q + r + s = 0
```

`-N` is `-32767` at the alpha width, not `math.MinInt16`; section 4.1 is why the
extreme negative value of the component type is excluded and what the symmetry
buys.

For cube coordinates satisfying `q + r + s = 0`, hex distance from the origin is
exactly `max(|q|, |r|, |s|)`, so this domain is the hexagon of radius `N` and
membership is three comparisons. Section 15.1 depends on that identity, and
section 4.1 is what makes the absolute values in it total.

Wrapping follows the hexagonal wraparound construction described by Red Blob Games. The six mirror centers are the rotations of:

```text
(2*N+1, -N, -N-1)
```

They are package-level data, computed once at initialization by the same `rotateOnce` used for player rotation (appendix A):

```go
// mirrorCenters holds the six rotations of (2N+1, -N, -N-1).
var mirrorCenters = [6][3]int64{ /* generated by rotateOnce */ }
```

Go has no `const` array and no `const` function call, so this is a `var` built in
`init` rather than a compile-time table. It is immutable by convention and by a
test that recomputes it; write the rotation once and derive the six triples
rather than transcribing them. Two of the six have components at `±(2N+1)`,
outside `Component` range at either width, which is what the `int64` intermediate
rule in section 4 protects.

When an operation produces a coordinate outside the canonical map, translate it by the appropriate mirror center until it is canonical. The implementation must use arithmetic normalization rather than a precomputed mirror table because this map is far too large to enumerate at either width. Use `int64` for mirror centers and every normalization intermediate, then convert canonical `q` and `r` to `Component`.

All public coordinate operations, neighbor sampling, persistence keys, region lookup, and rendering use the same canonicalizer. Because `Coord` cannot be constructed without it (section 4), this is structural rather than advisory.

The normalizer runs in three stages, because `NewCoord` accepts any `int64` pair and stepping one mirror center at a time would need on the order of `10^14` translations for an input near `int64` range:

1. **Already canonical.** Three comparisons, and the overwhelmingly common case.
   Return immediately.
2. **Greedy fix-up, bounded.** Subtract whichever mirror center most reduces
   `max(|q|, |r|, |s|)`, in fixed order `0..6`, until none does or a fixed
   iteration bound is reached. The six centers are the Voronoi-relevant vectors
   of the lattice and the canonical hexagon is an exact fundamental domain of it
   — `1 + 3N(N+1)` tiles for a lattice of the same index — so every coordinate
   has exactly one canonical representative, there is no tie to break, and a
   point no center improves is already canonical. Every coordinate the rest of
   the program actually produces — a neighbor, a scroll step, a region anchor, a
   rotation — lands here within a handful of steps.
3. **Lattice solve, for wild input only.** Mirror centers `0` and `1` generate
   the wraparound lattice. Inverting that two-vector basis gives the multiples
   directly; rounding each to the nearest integer leaves a residual within hex
   distance `2N+1` of the origin, which stage 2 then finishes.

> **The lattice solve is the one place that widens past `int64`.** Products such
> as `(2N+1) * q` reach `6e23` at the alpha width and `4e28` at the shipping
> width for `|q|` near `int64` range, and at `int32` the basis determinant alone
> — `3*(2N+1)^2`, about `5.5e19` — already does not fit. **Go wraps signed
> integer overflow silently**, so a solve written in `int64` produces a plausible
> wrong answer rather than a panic; that is the exact failure mode the rule in
> section 4 exists to prevent, and it is the mirror image of the hazard a Rust
> port hits, where the same code panics in a debug build. The solve is computed
> in exact 128-bit arithmetic by a small helper in `internal/mathx` built on
> `math/bits.Mul64`, `Add64`, `Sub64`, and `Div64`; the small residual returns to
> `int64`. `math/big` is an acceptable alternative in this branch and nowhere
> else — it allocates, and this branch is cold. Do not "simplify" the solve back
> to `int64`, and do not simplify it because it happens to fit at the alpha
> width.

The three stages exist in this order for a reason worth keeping: staging the cheap cases first means the exotic arithmetic sits in a branch that ordinary gameplay never takes, and a bug in it cannot be reached by a neighbor step. Test it directly rather than hoping to reach it (section 30.4).

**Field periodicity is no longer owed.** The previous revision asked for fields
periodic under the mirror translations on a best-effort basis and documented any
remainder as an accepted world-warp seam. WGVB implemented that and measured
what it costs to close: the wrap period is `2N+1` hexes, so every wavelength in
the configuration — and every fbm octave derived from it through the lacunarity
— would have to be drawn from the divisors of that number. At `int16` the period
factors as `3 * 5 * 17 * 257`; at `int32` it factors as `3 * 5 * 17 * 257 *
65537`, which is no more usable. The constraint is real, joint, and cuts straight
across the multi-scale table of section 10.

Section 15.1 retires the obligation instead of paying it. The rim covers the seam
with forced terrain and closes it to play, so a discontinuity that is never
rendered and never entered does not need to be smooth. Sampling remains a pure
function of the canonical coordinate, so a coordinate and its wrapped image
produce bit-identical values; that much is exact and is what tile identity
depends on. Two tiles that neighbor each other *across* the seam lie a world
diameter apart in canonical world space and their field values are uncorrelated.
Under the rim, that is invisible.

`coord_wrap_test.go` still asserts bit-exact wrap consistency across all six edges and all integer combinations of the mirror centers. That is a statement about identity, not about smoothness, and it holds whatever the rim does.

**Hex geometry dependency.** The core `wgva` package depends on **nothing
outside the standard library**. It needs only the six direction vectors and the
`float64` axial-to-world conversion above, both a few lines, and both pinned by
the algorithm version.

The `render` package uses [`hexg`](https://github.com/maloquacious/hexg) for the flat-top layout, the offset-coordinate conversion a viewport rectangle is built from, and hit testing a pixel back to a cell. Convert `Component` to `hexg.Hex` through one adapter function.

It is deliberately *not* used for three things it offers:

- **Its wraparound.** Section 7.1's normalizer is the only canonicalizer in the
  program, and a second one that disagrees anywhere is a second tile identity.
- **Polygon corners.** Pixels are assigned by hit testing rather than by filling
  polygons, so no corner list is ever built.
- **`hexg.HexSet`.** It is map-backed, and Go map iteration is randomized. Any
  output whose order can be observed — overlapping edges, labels, markers — must
  come from a sorted slice. See section 25.3.

Keeping `hexg` out of the core package is what makes it impossible to route a generation coordinate through renderer layout math by accident, and the import cycle rule in section 28 is what keeps it out.

### 7.2 World scale

For a cube-coordinate hexagon of radius `N`, the number of tiles is:

```text
tiles = 1 + 3*N*(N+1)
```

| | `int16`, the alpha | `int32`, shipping |
|---|---:|---:|
| `N` | `32,767` | `2,147,483,647` |
| Canonical tiles | `3,221,127,169` | `13,835,058,048,839,712,769` |
| Total area, square miles | `1.0042e11` | `4.3133e20` |
| Earth surface areas | ~510 | ~2.19 trillion |
| Center-to-center radius, miles | `196,602` | `12,884,901,882` |
| Opposite-corner span, miles | `393,204` | `25,769,803,764` |
| Days to walk origin to rim at 24 miles | ~8,200 | ~537,000,000 |

At a 3-mile apothem each tile covers `18*sqrt(3)`, approximately `31.1769`, square miles, at either width.

Two consequences worth writing down:

- **At the alpha width the rim is reachable** — about twenty-two years of
  continuous walking — which is precisely why it is the width to develop and
  test at. At the shipping width nothing reaches it, and the rim of section 15.1
  exists for the map and the renderer rather than for the walker.
- **At the shipping width the tile count does not fit in an `int64`.** It
  exceeds `math.MaxInt64` by about 50%. Any expression that computes it — a
  progress total, a percentage, a test — must use `uint64`. This is a small
  thing that will be got wrong once, and it will be got wrong during the width
  change rather than before it, because at `int16` an `int64` is comfortable.


---

## 8. Coordinate-Based Randomness

WGVA does not use a mutable PRNG stream as the basis of terrain generation. That would make results depend on traversal order.

`math/rand/v2` may be used for things that are *not* world generation — test data, sampling for statistical checks, tie-breaking in tooling. It must never appear in the generation path. `math/rand` (v1) must not appear anywhere: its top-level functions share global state, and its `Seed` behavior changed across releases.

Derive deterministic values from semantic paths instead:

```text
hash(seed, "macro-elevation", regionQ, regionR)
hash(seed, "mountain-axis",   regionQ, regionR)
hash(seed, "local-detail",    q, r)
```

Different procedural systems must use independent domains so a change to one field cannot perturb another.

### 8.1 Domains are constants, pinned by a test

Domain identifiers are FNV-1a over the domain name. Go cannot call a function in
a constant expression, so the values are written out as constants and a test
asserts each one against the function:

```go
// domain is FNV-1a over the domain name. Stable by construction: the algorithm
// is written here, so no dependency can change it.
func domain(name string) uint64 {
    h := uint64(0xcbf29ce484222325)
    for i := 0; i < len(name); i++ {
        h ^= uint64(name[i])
        h *= 0x00000100000001b3
    }
    return h
}

const (
    DomContinentalness    uint64 = 0x... // domain("continentalness")
    DomRegionalElevation  uint64 = 0x... // domain("regional-elevation")
    DomRelief             uint64 = 0x... // domain("relief")
    DomTerrainDetail      uint64 = 0x... // domain("terrain-detail")
    DomTemperature        uint64 = 0x... // domain("temperature")
    DomMoisture           uint64 = 0x... // domain("moisture")
    DomMoistureVariation  uint64 = 0x... // domain("moisture-variation")
    DomRegionStyle        uint64 = 0x... // domain("region-style")
    DomRidgeOrientation   uint64 = 0x... // domain("ridge-orientation")
    DomRidgeStructure     uint64 = 0x... // domain("ridge-structure")
    DomBasin              uint64 = 0x... // domain("basin")
    DomBasinRegional      uint64 = 0x... // domain("basin-regional")
    DomBasinLocal         uint64 = 0x... // domain("basin-local")
    DomVolcanic           uint64 = 0x... // domain("volcanic")
    DomWarpX              uint64 = 0x... // domain("warp-x")
    DomWarpY              uint64 = 0x... // domain("warp-y")
    DomDetailWarpX        uint64 = 0x... // domain("detail-warp-x")
    DomDetailWarpY        uint64 = 0x... // domain("detail-warp-y")
    DomFieldOffset        uint64 = 0x... // domain("field-offset")
)
```

The alternative — package-level `var`s computed in `init` — is shorter and worse: a `var` can be assigned, the values never appear in a diff, and a renamed domain silently changes every world with nothing in the commit to see. Constants plus a test make both halves visible, and the test is four lines.

Adding a domain never disturbs an existing one. Renaming one changes the world and is an algorithm version change.

**One field node, one domain.** Where a family has several scales — the three basin fields, the broad moisture field and its variation term — each gets its own domain rather than sharing one. Sharing would give two nodes the same gradient table *and* the same seed-derived sampling offset (section 9.4), so at the world origin they would land in the same lattice cell at the same fractional position and return the same value. Separate domains make that impossible rather than unlikely.

### 8.2 The hash primitive

Fixed arity, so each call site is explicit about how many coordinates feed the hash:

```go
func Hash2(seed, domain uint64, a, b int64) uint64
func Hash3(seed, domain uint64, a, b, c int64) uint64
```

Go's variadics would work and are the wrong choice here for the same reason: a call that passes the wrong number of coordinates should not compile.

> **Go wraps integer arithmetic silently, which makes the mixer easy and the
> normalizer dangerous.** Plain `*`, `+`, and `^` in a mixer are exactly right
> and need no `wrapping_` ceremony — this is the one place Go's overflow
> behavior is the behavior you want. The same silence is why the lattice solve
> in section 7.1 must widen explicitly: there, a wrapped product is a wrong
> answer that nothing reports.

Use a well-understood finalizer — SplitMix64 or the `xxh3` avalanche step — written out in this package rather than pulled from a dependency, for the same reason the noise is (section 9.2). Convert to `float64` in `[0, 1)` by taking the top 53 bits:

```go
func toUnit(h uint64) float64 { return float64(h>>11) / float64(1<<53) }
```

Never `float64(h) / float64(math.MaxUint64)`, which loses the low bits to rounding and is not uniform.

Do not use `hash/maphash` for anything persisted: its seed is per-process by design. Do not use `hash/fnv` in the generation path either — not because it is unstable, but because the mixer must be readable in one place and owned here.

---

## 9. Continuous Noise

The generator uses deterministic continuous noise for spatial coherence.

Suitable families:

- gradient noise,
- simplex-style noise,
- OpenSimplex-style noise,
- value noise with quintic interpolation,
- domain-warped combinations of the above.

The noise must evaluate at arbitrary coordinates with no finite array.

### 9.1 Field composition is a tagged struct, not an interface

The set of field kinds is *closed* and comes from configuration. Go offers an interface with a type switch, or a tagged struct with a kind switch:

```go
type FieldKind uint8

const (
    FieldValue   FieldKind = 1
    FieldSimplex FieldKind = 2
    FieldFBM     FieldKind = 3
    FieldWarp    FieldKind = 4
    FieldOffset  FieldKind = 5
    FieldSum     FieldKind = 6
)

type Field struct {
    Kind FieldKind

    // Value, Simplex
    Seed            uint64
    Domain          uint64
    WavelengthMiles float64

    // FBM
    Octaves    uint8
    Lacunarity float64
    Gain       float64

    // Warp
    StrengthMiles float64
    WarpX, WarpY  *Field

    // Offset
    DXMiles, DYMiles float64

    // FBM, Warp, Offset
    Source *Field

    // Sum
    Terms []WeightedField
}

type WeightedField struct {
    Weight float64
    Field  Field
}

func (f *Field) Sample(x, y float64) float64 // switch on f.Kind
```

The tagged struct is chosen over an interface because it has one shape on the
wire and one in memory, and because a `switch` on a `uint8` is a jump table
rather than an indirect call per octave per tile. That second reason is real but
minor and should not be argued about: per section 31, the only party who notices
is the administrator drawing a very large window.

The first reason is the one that matters. The tree must be inspectable and
printable — the tuning tool draws it — and an interface with six implementing
types needs custom marshalling to round-trip, which is a second place for the
field graph to be described and therefore a second place for it to be wrong.

Normalized output range is `[-1, +1]`, documented per kind. Validation rejects a node whose fields are set for a kind other than its own; see section 21.

> **Where the field graph comes from, and where it does not.** WGVB serialized
> the `Field` tree directly into the world file. WGVA does not: the tree is
> *derived* from the flat `Config` of section 21 by one pure function, and the
> flat configuration is the authoritative, fingerprinted thing. Two serialized
> descriptions of the same numbers is two places to disagree, and a human editing
> a configuration edits wavelengths and weights, not a tree. The derived tree is
> still written into the world file — as a diagnostic record, excluded from the
> fingerprint — so a reader can see exactly what was evaluated without
> reconstructing it.

### 9.2 Implement the noise in this module

**Do not take a noise dependency.** Three reasons:

1. Section 27 requires that any change to a noise formula be an algorithm
   version change. A dependency's minor release can change its output and
   silently invalidate every existing world with no version bump on our side.
2. Anything that dispatches on runtime CPU features produces **different results
   on different machines**. That breaks `Tile = F(seed, coord, version, config)`
   outright, not subtly.
3. OpenSimplex2 or value-noise-with-quintic is roughly 200 lines. Owning it
   means the algorithm version genuinely pins the output, which is the whole
   point.

Put it in an unexported file of the `wgva` package, or under `internal/noise`. Cite the reference implementation and its license in a file comment.

### 9.3 The octave ladder stops at the tile grid's Nyquist wavelength

Adjacent tile centers are `2 * ApothemMiles` apart, so the shortest feature the grid can carry is `4 * ApothemMiles` — twelve miles at this scale. An fbm octave below that limit is not detail. It cannot be represented at all, and what reaches the tile is an aliased sample of it: per-tile noise, paid for with a noise evaluation and then thrown away.

Relief is where this shows. Relief is a first difference between neighbors, which is exactly the operation that amplifies content near the limit, so the aliased octaves dominate it and the layer reads as speckle with the real ridge structure buried underneath. Elevation itself survives, because the aliased amplitude is small against the coarse scales — which is precisely why nothing catches this without a rule.

Two consequences for the implementation:

- **The octave count is per field, not global.** `Field` already carries it per
  node; a single configuration value shared by every scale is an artificial
  coupling that forces the shortest field to run the longest field's ladder.
- **Nyquist is a validation bound, not the mechanism that picks the counts.**
  Deriving each count by truncating at the limit would make it a step function
  of a float, so retuning a wavelength by a tenth of a mile would silently flip a
  count and move every value in the world — the knife-edge threshold section 25.6
  warns about. Configuration states the counts; validation rejects a ladder that
  reaches below the limit; the limit is derived from `ApothemMiles` rather than
  written as a literal.

### 9.4 Displace the sampling position by a seed-derived offset

Every noise lattice has its origin at the coordinate origin, so without an offset the world origin is a lattice point of *every* scale at once and every field is exactly zero there. Simplex noise has its steepest gradient at a lattice point, so the region around the origin is measurably steeper than the rest of the world — a permanent, visible anomaly at the one coordinate every player frame is expressed against, every worked example uses, and every diagnostic render defaults to. It is the same objection section 11.2 raises to the anchor lattice and section 33.4 raises to region-owned terrain: no place may be special because of how the implementation addresses it.

The fix is a translation of the sample position. It must be:

- **derived from the seed**, so two worlds do not share the anomaly's new
  location;
- **applied per domain**, so the scales do not all land on their own lattice
  points at some *other* single coordinate — moving the defect is not fixing it;
- **applied above the fbm rather than inside the leaf.** The fbm scales the
  sample position by the octave frequency before the leaf divides by the
  wavelength, so an offset folded into the leaf is the same fraction of a cell at
  every octave: at the origin every octave would then sample the same cell, with
  the same gradients, and their slopes would add constructively. That is the same
  defect with a smaller coefficient.
- **scaled by the wavelength**, so it is irrational-looking relative to that
  field's lattice instead of a round number of miles that some other wavelength
  divides, and **kept clear of the cell corners** — a hash is free to come back
  near zero, and a fix that works for most seeds is not a fix.

A single rendered window does not demonstrate any of this. At one seed the origin is a bright dot among a handful of others. Only a measurement pooled over many seeds separates it from terrain, because ordinary terrain is uncorrelated between worlds and cancels, leaving whatever is a function of position relative to the centre. Section 30.13 is that measurement.

---

## 10. Multi-Scale Geography

A single noise frequency looks synthetic. WGVA combines several spatial scales.

```text
elevation =
    macro_continentalness
  + regional_uplift
  + ridge_structure
  + local_relief
  + fine_detail
```

Approximate starting wavelengths. One hex of wavelength means 6 miles of center-to-center distance; it does not refer to edge length, point-to-point width, or area.

| Field | Approximate wavelength | Approximate distance |
|---|---:|---:|
| Continentalness | 500–2,000 hexes | 3,000–12,000 miles |
| Macro uplift | 150–600 hexes | 900–3,600 miles |
| Regional relief | 40–200 hexes | 240–1,200 miles |
| Hills | 8–40 hexes | 48–240 miles |
| Local detail | 3–12 hexes | 18–72 miles |

These are starting values, not requirements. The exact constants live in the configuration structure, not scattered through the implementation.

The wavelengths above are each scale's *base*. Each is the top of an fbm ladder that descends by the lacunarity, so the octave count decides how far into the next scale's band a field reaches, and section 9.3 bounds how far it may. Once that bound truncates the shortest ladder hardest, the fields stop being strictly ordered by how fast they vary even though their base wavelengths still are; that is a consequence of the bound and not a defect to design around.

Note that these wavelengths are absolute, in miles, and **do not scale with the
component width**. A continent is a continent at either width. What changes when
the width changes is how many of them there are, which is the whole point.

---

## 11. Hierarchical Regions

Continuous noise is supplemented by deterministic regional influences.

```text
macro region
    |
    +-- region
            |
            +-- chunk
                    |
                    +-- tile
```

Example sizes at the 3-mile apothem:

| Level | Axial interval | Physical interval |
|---|---:|---:|
| Macro region | 512 hexes | 3,072 miles |
| Region | 128 hexes | 768 miles |
| Chunk | 32 hexes | 192 miles |

The intervals describe spacing between anchors along either axial basis direction. Regions and chunks produced by independent division of `q` and `r` are parallelograms in world space, not regular hexagons with the listed physical interval as a diameter.

### 11.1 Why regions exist

Regions create persistent geographic character. A region deterministically derives parameters such as:

- average uplift,
- climate bias,
- roughness,
- ridge orientation,
- wet/dry tendency,
- volcanic tendency,
- terrain variation.

This gives large areas identity without storing anything.

### 11.2 No hard boundaries

A tile must not inherit parameters from exactly one region. That creates visible seams.

Blend regional parameters from neighboring anchors:

```text
value = w00*region00 + w10*region10 + w01*region01 + w11*region11
```

For hex-oriented or Voronoi-inspired schemes, blending may use another fixed local neighborhood.

WGVA takes the hex-oriented option, because the four-corner form makes an artifact that is easy to miss in a unit test and obvious in a wide render. The axial basis vectors are 60 degrees apart and equal in length, so anchors at `(i * size, j * size)` form a *triangular* lattice in world space and each cell is two equilateral triangles. Interpolating bilinearly across the cell privileges the cell's long diagonal, and a field built that way draws the lattice as rows of aligned lozenges. Interpolating across the containing triangle has no preferred diagonal and carries the lattice's own six-fold symmetry.

Barycentric coordinates on that triangle are already a partition of unity. Squaring each and renormalizing keeps the sum at one and makes a corner's weight and its first derivative vanish as the corner leaves the neighborhood, so a triangle edge — and a cell boundary, which is one — is a smooth join rather than a crease. Cubing joins smoothly too and looks worse: each anchor acquires a plateau and the plateaus meet along the hexagonal boundaries of the lattice's Voronoi cells, which is the lattice made visible by another route.

> Crossing a chunk or region boundary must not introduce a discontinuity merely because the addressing region changed.

---

## 12. Region Anchors

A lattice of deterministic anchors is the straightforward implementation. For each regional grid coordinate `(regionQ, regionR)`, derive stable attributes by coordinate hashing:

```go
type RegionParams struct {
    ElevationBias float64
    MoistureBias  float64
    HeatBias      float64
    Roughness     float64
    BasinBias     float64
    Volcanic      float64
    Variation     float64
    Ridge         UnitVec2
}
```

Ridge orientation is stored and consumed as a **unit direction vector** `(cos, sin)`, not as an angle in radians, so that no trigonometric function appears in the generation path. See section 25.2. Derive the vector by hashing two values and normalizing, or by hashing a point and rejecting until it lands in the unit disc — both use only multiply, add, and `sqrt`. The field is named `Ridge` and typed `UnitVec2` for that reason: a field called `RidgeAngle` holding a vector invites someone to put an angle in it, and a `float64` there accepts one without complaint.

A ridge orientation is a **line, not an arrow**: `v` and `-v` name the same orientation, and consumers must take `|dot|` rather than `dot`. This is not a detail — orientations cannot be averaged as vectors. Two anchors whose ridges run the same way but hashed to opposite arrows would blend to nothing, and a tile between them would get an orientation unrelated to either. Blend the doubled-angle form `(x^2 - y^2, 2xy)` instead, in which `v` and `-v` are identical, and recover the orientation with the half-angle identities `cos t = sqrt((1 + cos 2t) / 2)` and `sin t = sqrt((1 - cos 2t) / 2)`, taking the sign of `sin t` from `sin 2t`. Every step of that is multiply, add, divide, and `sqrt`. Where the blended doubled-angle vector cancels to nothing — two anchor ridges at right angles — no orientation exists; fall back deterministically, as the noise gradients do.

When generating a tile, evaluate the nearest relevant anchors and smoothly interpolate their contribution.

Region parameters are normalized *biases* in `[-1, +1]`, not quantities. How much uplift an elevation bias is worth, or how many degrees a heat bias moves a tile, belongs to the phase that consumes it — which is what lets elevation, climate, and terrain be tuned independently without redefining what a region is.

---

## 13. Domain Warping

Straight noise reveals its mathematical origin. Domain warping is part of the recommended implementation.

Instead of:

```text
elevation = noise(x, y)
```

evaluate:

```text
wx = x + warp_x(x, y) * strength
wy = y + warp_y(x, y) * strength

elevation = noise(wx, wy)
```

This produces more irregular coastlines, mountain belts, climatic boundaries, and regional forms.

Warp fields must themselves be deterministic and continuous. Use low-frequency warps for large geography and weaker high-frequency warps for local irregularity.

---

## 14. Elevation

Elevation is the primary physical field, represented as a normalized `float64` scalar:

```text
-1.0 deep ocean
 0.0 sea level
+1.0 extreme highland
```

Values outside that range may be clamped.

Do not normalize elevation using the minimum and maximum of a finite generated map. That reintroduces a dependence on map bounds. Field composition and thresholds must be stable globally.

### 14.1 Elevation Classification

```go
type Elevation uint8

const (
    ElevationDeepWater    Elevation = 0
    ElevationShallowWater Elevation = 1
    ElevationLowland      Elevation = 2
    ElevationUpland       Elevation = 3
    ElevationHighland     Elevation = 4
    ElevationMountain     Elevation = 5
)
```

> **Write every value explicitly. Do not use `iota`.** These values are
> persisted and appear in cached tiles. Inserting a band into the middle of an
> `iota` block silently renumbers everything after it and changes the meaning of
> stored data, with nothing in the diff that looks like a data change. Explicit
> values make the hazard visible in review, and an inserted value with an
> explicit number is obviously either a new number or a collision.

Go cannot make classification total the way a sum type can. Two things stand in for that:

- Every classification `switch` has an explicit `default` that is a documented
  fallback, never a silent zero value.
- Every enum has `String()` and `Valid()` methods, and a test enumerates the
  declared values and asserts both round-trip. Section 30.8's distribution test
  then asserts each one is actually reachable, which is the property exhaustive
  matching would have given.

The exact names are game-facing decisions and do not constrain the internal scalar field.

---

## 15. Water and Land

Land versus ocean water is determined by comparing elevation against a fixed sea-level threshold:

```text
elevation <= 0 -> ocean water
elevation >  0 -> potential land
```

Sea level may be configurable.

Deterministic basin fields may classify some potential-land tiles as lakes or inland seas if the coherence requirements in section 17 are met. Otherwise the first implementation retains basin geography without inland-water classification. Both approaches must use bounded local sampling, never global connectivity or flood fill.

WGVB took the second path, and the decision record in section 17.1 is why.

If a target land fraction is desired, tune the continentalness distribution and sea-level threshold statistically. Do not calculate sea level from a finite sample at runtime.

### 15.1 The world rim

**Decision: the outermost band of the canonical hexagon is forced terrain and is
closed to play.** The wrapped topology of section 7.1 stays exactly as it is;
what changes is that the seam is covered rather than smoothed.

The motivation is the one in section 1.1: wrapping is cheap and it looks
interesting, and neither of those is a reason to let a player walk across a
discontinuity or to let a renderer draw one. Forcing the band is cheaper than
making the fields periodic, and unlike periodicity it does not constrain a single
wavelength in section 10.

**The test is three comparisons and no neighbor sampling.** Because hex distance
from the origin is `max(|q|, |r|, |s|)` for cube coordinates summing to zero
(section 7.1), distance from the rim is:

```go
// RimDistance reports hexes from the outer edge of the canonical map:
// 0 on the outermost ring, positive inside, and never negative for a
// canonical coordinate.
func (c Coord) RimDistance() int64 {
    q, r, s := int64(c.q), int64(c.r), int64(c.s())
    return WorldRadius - max(abs64(q), abs64(r), abs64(s))
}
```

That is `O(1)`, purely local, and requires nothing outside the tile itself — so
the rim satisfies section 2.3 in a way inland water (section 17.1) could not.

The band has two parts, both configured in hexes:

```go
type RimConfig struct {
    // ClosedHexes is the outermost band: terrain forced, passage refused.
    ClosedHexes uint32

    // FalloffHexes is the band inside it, over which elevation is driven
    // smoothly down to the forced value. Terrain there is ordinary terrain,
    // classified from the depressed elevation.
    FalloffHexes uint32

    // Kind is what the closed band is made of.
    Kind RimKind
}

type RimKind uint8

const (
    RimDeepOcean RimKind = 0
    RimPolarIce  RimKind = 1
)
```

The falloff exists so the rim is not a cliff. Inside `FalloffHexes` the elevation
composite is blended toward the forced value by a smoothstep in
`RimDistance`, using only the polynomial forms section 25.2 permits, so a
traveller approaching the rim sees a shelving coast or a rising icefield rather
than a wall. Nothing else about that terrain is special: it is classified from
the depressed elevation by the ordinary rules, so a cold falloff band produces
tundra and then ice on its own.

Inside `ClosedHexes` the tile is forced: `Rim` is true, elevation is pinned at
the configured floor, and terrain is `TerrainDeepOcean` or `TerrainGlacialIce`
according to `Kind`. Heat and moisture are still generated and still reported,
because a forced tile that reported a fabricated climate would corrupt every
distribution measurement that includes it.

**The generator marks; it does not enforce.** `Tile.Rim` is a statement about the
world, and refusing a move is a game rule. Nothing in `wgva` knows what a move
is. The renderer draws the band like any other terrain and the game refuses
entry; the two need not agree about anything except the flag.

**Deep ocean is the default and ice is the option, in that order.** An ice rim
reads as a polar cap, and a polar cap is a promise about latitude that section 16
deliberately does not make — a player who sees ice at the edge will infer an
axis, an equator, and a southern cap, and be wrong about all three. Deep ocean
promises nothing except that the water keeps going. Choose ice only for a world
whose climate model has been given a latitude to match it.

Consequences elsewhere in this document:

- **Section 7.1 no longer owes periodic fields.** This is the whole return on
  the decision, and it is large: the alternative was drawing every wavelength and
  every octave from the divisors of `2N+1`.
- **The rim is part of the world, so it is part of the fingerprint.**
  `ClosedHexes`, `FalloffHexes`, and `Kind` are generation-affecting
  configuration under section 21, and changing any of them is an algorithm
  version change.
- **`ClosedHexes = 0` and `FalloffHexes = 0` is a valid configuration** and
  restores the raw wrapped world exactly. Keep it working: it is how the wrap
  tests in section 30.10 see the seam they are asserting on, and the alpha width
  is the only setting where looking at it is practical.
- **At the alpha width the rim is a feature you can visit**; at the shipping
  width it is a guarantee nobody collects. Tune it at `int16`.

---

## 16. Climate

Climate is computed from at least temperature, moisture, and elevation:

```text
temperature = broad_heat_field + regional_heat_bias - elevation_cooling
moisture    = broad_moisture_field + regional_moisture_bias + local_variation
```

Because the wrapped world has no inherent equator, do not assume `r == 0` is a planetary equator unless that becomes an explicit world rule. The first version uses procedural broad heat zones rather than global latitude.

> If latitude is added later, express its falloff as a polynomial, not a cosine.
> See section 25.2. And see section 15.1 on why a polar ice rim without a
> latitude model tells the player something untrue.

### 16.1 Climate classification

Climate retains independent heat and moisture classifications. Do not use a single type mixing values such as cold, arid, and humid; those properties are not mutually exclusive.

```go
type HeatBand uint8

const (
    HeatPolar     HeatBand = 0
    HeatCold      HeatBand = 1
    HeatTemperate HeatBand = 2
    HeatWarm      HeatBand = 3
    HeatHot       HeatBand = 4
)

type MoistureBand uint8

const (
    MoistureArid      MoistureBand = 0
    MoistureDry       MoistureBand = 1
    MoistureModerate  MoistureBand = 2
    MoistureHumid     MoistureBand = 3
    MoistureSaturated MoistureBand = 4
)

type Climate struct {
    Heat     HeatBand
    Moisture MoistureBand
}
```

Explicit values, not `iota`, for the reason in section 14.1.

Bands and thresholds may be tuned, but the two-axis representation is part of the public model. `Tile` also retains the normalized heat and moisture values the bands were classified from.

---

## 17. Terrain

Terrain is a classification derived from physical fields, never an independent random lookup.

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

The initial vocabulary is broad enough to produce a varied fantasy map without requiring every distinction at once:

| Family | Suggested terrain types | Typical evidence |
|---|---|---|
| Ocean | deep ocean, ocean, shallow sea, coastal water | Elevation below sea level, depth, adjacency to land |
| Inland water | inland sea, lake | Basin fields, basin scale, depth, low local relief |
| Frozen | glacial ice, tundra | Low temperature, with elevation and moisture distinguishing persistent ice from tundra |
| Wetland | marsh, swamp, bog | Saturated moisture, low elevation, low relief, temperature |
| Dry | desert, badlands, scrubland | Low moisture, heat, exposed relief, regional character |
| Open land | plains, grassland, steppe, savanna | Moderate moisture and temperature, regional variation |
| Forest | boreal forest, temperate forest, tropical rainforest or jungle | Sufficient moisture with the appropriate heat band |
| Elevated | hills, mountain, alpine terrain | Elevation, relief, slope, temperature |
| Volcanic | volcano, volcanic highland | Strong volcanic tendency with uplift and concentrated relief |
| Coastal land | coast | Land near sea level with an adjacent ocean-water tile |

These are primary game-facing classifications. Elevation, relief, and climate remain available so a game can render forested hills, glaciated mountains, or a volcanic island without a distinct terrain constant for every combination.

Order classification rules so exceptional terrain is not hidden by a broad biome rule:

```text
rim
    -> ocean and inland water
    -> glacial ice
    -> volcano
    -> mountain and alpine terrain
    -> wetland
    -> coastal land
    -> climate-driven land cover
```

The rim is first because it is not a judgement about the ground; it is a statement that this tile is outside the world. Everything below it is the ordinary classifier.

Coastal land sits between wetland and the climate cover rather than above both. A saturated shoreline is a marsh, which says more than *coast* does; anything else at the water's edge is a coast before it is a grassland.

Ocean water still comes from the primary elevation field and sea-level threshold. Make a best effort to derive coherent depressions from deterministic basin fields at broad, regional, and local scales. Basin influence is useful even when no water is assigned: dry endorheic regions such as the Great Basin are valid geographic results.

Classify a basin as a lake or inland sea only if bounded local generation can give neighboring water tiles coherent membership, surface elevation, depth, and shorelines. The lake/inland-sea distinction is then based on generated basin scale and depth, not global connectivity or flood fill. If those invariants cannot be achieved simply and deterministically, omit inland-water terrain from the first implementation rather than emitting inconsistent per-tile water. A generated inland sea is a very large basin lake, not water proven disconnected from every ocean in the wrapped world.

Marsh and swamp are distinguished primarily by climate and vegetation tendency: marshes favor open saturated lowlands, swamps favor warmer or forested saturated lowlands. Volcanoes are rare products of regional volcanic tendency, uplift, and local peak structure, not independent random tile assignments.

`Terrain` is a `uint8` with explicit values, on the same terms as section 14.1.

### 17.1 Decision record — inland water is omitted

**WGVB's phase 6 shipped basin geography and no inland water, and WGVA inherits
that decision.** The two variants stay in the vocabulary with their values
pinned, because they are part of the persisted value space and the version that
does emit them must not renumber the variants after them.

The paragraph above permits inland water only where bounded local generation can give neighboring water tiles coherent membership, surface elevation, depth, and shorelines, and requires omission rather than approximation otherwise. It cannot, and the obstruction is structural:

- A lake has **one** surface elevation. Every tile of one lake must agree on it,
  or the water runs downhill inside itself and a tile's depth is not a depth.
- Which tiles are "one lake" is a **connected component** of the ground below
  that surface. Finding it is a traversal whose extent is the lake's, which is
  unbounded in principle — and sections 2.3 and 15 forbid global connectivity and
  flood fill outright.
- A *smooth* water-surface field, with a tile under water where its elevation
  falls below it, is bounded and deterministic and visibly wrong: the surface
  varies across the lake, so the lake is tilted, two hollows a few hexes apart
  have different water levels, and the shoreline is where two smooth fields
  happen to cross rather than a level line.
- Giving each addressing cell a lake with a hashed surface elevation
  reintroduces the hard region boundary of section 33.4, draws the lattice on the
  map, and puts water on hillsides wherever the cell's elevation does not match
  the ground.

Note how differently the rim of section 15.1 sits against the same three tests: its membership is a function of the tile alone, its forced elevation is a constant rather than a consensus, and its boundary is a ring of constant hex distance rather than a level line. That is why one ships and the other does not, and it is worth keeping in mind when the next "just force some tiles" idea arrives.

What ships instead is the geography. Basin influence exists at broad, regional, and local scales, blended with the region basin bias, and terrain reads it through a *product* rather than a sum: terrain wetness is `moisture + weight * basin * moisture`, so a basin makes a wet climate wetter and a dry one drier, and a rise sheds water either way. That is this section's own example — dry endorheic regions such as the Great Basin are valid geographic results — arriving as a consequence of the composition rather than as a special case. A wet basin reads as marsh, swamp, or bog. What is missing is open water in the middle.

**Basin influence does not feed elevation, and must not.** A basin term inside the elevation composite is the obvious way to make a depression *be* lower ground, and it would move every tile in every world. The golden table of section 30.9 is what enforces the placement: adding basin influence must add columns and move none of the ones already there.

---

## 18. Local Relief and Slope

Some terrain decisions require knowing whether a location is flat, hilly, or steep. Estimate slope by sampling elevation at the six neighboring hexes.

```go
func (g *Generator) Relief(c Coord) float64
```

Because generation is deterministic and stateless, sampling neighbors creates no dependency problem.

Avoid recursive terrain classification:

```text
raw fields
    -> elevation scalar
    -> neighbor elevation samples
    -> derived relief
    -> climate
    -> terrain
```

**Do not call `Tile()` from inside `Tile()`.** Structure the internals as an unexported `elevationScalar(c Coord) float64` that both `Tile` and `Relief` call, so the recursion cannot be written. Go will not catch this for you; it will simply recurse until the stack is gone.

The neighbor loop must accumulate in a fixed direction order, `0` through `5`. See section 25.3.

Relief inside the rim is relief of the forced elevation, which is flat. That is correct and is not a special case: a forced constant has no slope.


---

## 19. Public API

A minimal initial API for the `wgva` package:

```go
package wgva

type Seed uint64

type Component = int16 // int32 for the shipping world; section 4.2

type Coord struct{ /* unexported fields; see section 4 */ }

type Generator struct{ /* immutable configuration */ }

func NewCoord(q, r int64) Coord

func (c Coord) Q() Component
func (c Coord) R() Component
func (c Coord) S() Component
func (c Coord) Neighbor(direction int) Coord

// Rotate turns by steps sixths of a turn, in index order. Appendix A.
func (c Coord) Rotate(steps int) Coord

// Cell and CellOffset serve chunk, region, and macro-region addressing.
// Section 23.
func (c Coord) Cell(sizeHexes uint32) (int64, int64)
func (c Coord) CellOffset(sizeHexes uint32) (int64, int64)

// RimDistance reports hexes from the outer edge of the map. Section 15.1.
func (c Coord) RimDistance() int64

func New(seed Seed, cfg Config) (*Generator, error)
func NewDefault(seed Seed) *Generator

func (g *Generator) Tile(c Coord) Tile
func (g *Generator) ElevationAt(c Coord) float64
func (g *Generator) Relief(c Coord) float64
func (g *Generator) Sample(c Coord) Sample

// RegionParamsAt returns the blended region parameters at a coordinate.
// Sections 11 and 12.
func (g *Generator) RegionParamsAt(c Coord) RegionParams

func (g *Generator) Seed() Seed
func (g *Generator) Config() Config
```

`NewCoord` is the entry point for unwrapped or intermediate coordinates and returns the canonical representative. `Neighbor` normalizes before returning. Because `Coord` has no other constructor, `Tile` has nothing to re-normalize — the type already guarantees canonical input.

`New` returns an error because configuration validation can fail; `NewDefault` cannot fail and is the ergonomic path for tests and the tuning tool.

`Config()` returns a value, not a pointer. `Config` contains a slice or two, so the copy is shallow and a caller could in principle reach into it; the value return states the intent and a test asserts that mutating the returned value does not change the generator. Returning a pointer would state the opposite.

**The configuration fingerprint is not a method on `Generator`.** Section 21.2 defines it over a `Config`, and nothing in the generation path reads it, so it belongs to the package that owns what a configuration weighs on disk: `config.Fingerprint(algorithmVersion, cfg)`, re-exported by `store` because a world file is what writes one. Putting it in `wgva` would have given the core package a CBOR dependency, which appendix B does not allow.

Do not expose chunk generation as the primary abstraction. Applications ask directly for a tile.

### 19.1 Error model

The previous revision left errors implicit. Make them inspectable values.

```go
var (
    ErrNotFinite    = errors.New("value must be finite")
    ErrNotPositive  = errors.New("value must be positive")
    ErrOutOfRange   = errors.New("value out of range")
    ErrOctaveCount  = errors.New("octave count out of range")
    ErrBelowNyquist = errors.New("octave ladder reaches below the Nyquist wavelength")
    ErrPassCount    = errors.New("contrast pass count out of range")
    ErrNotAscending = errors.New("band thresholds must ascend")
    ErrFieldShape   = errors.New("field node has values set for another kind")
)

// ConfigError names the field that failed and wraps the reason.
type ConfigError struct {
    Field string
    Value float64
    Lo    float64
    Hi    float64
    Below string  // for ErrNotAscending: the threshold this one must exceed
    Err   error
}

func (e *ConfigError) Error() string { /* ... */ }
func (e *ConfigError) Unwrap() error { return e.Err }
```

`ErrNotAscending` is worth its own sentinel: the elevation, heat, and moisture ladders are compared in order, so an out-of-order threshold is a band that can never be reached — evaluable and wrong, which is the same shape of defect as `ErrBelowNyquist`.

The database compatibility gates in section 27 get the same treatment, one sentinel per gate, so section 30.11 asserts with `errors.Is` instead of matching message strings. **A test that compares an error's `Error()` string is a test that will pass while the gate is wrong.**

---

## 20. Batch API

Rendering and simulation request rectangular or hexagonal groups. Batch APIs reduce repeated setup.

```go
// TilesInto fills out with one tile per coordinate. It panics if the lengths
// differ.
func (g *Generator) TilesInto(coords []Coord, out []Tile)

func (g *Generator) Tiles(coords []Coord) []Tile
func (g *Generator) Region(center Coord, radius uint32) []Tile
```

Prefer the `Into` form in hot paths: `Tile` is a pointer-free value, so a caller can reuse one buffer across frames with no allocation and no work for the garbage collector.

Batch results must be bit-identical to individual `Tile()` calls. Batch generation is an optimization only.

Goroutines may parallelize batch fills. This is safe *and* deterministic here for a structural reason: every tile is a pure function of its own coordinate and writes to its own slot, so scheduling order cannot affect any value. Split the coordinate slice into contiguous ranges, one per worker, and let each write only its own range.

**Do not introduce any batch operation that accumulates across tiles** — a running sum, a min/max, a histogram — because floating-point addition is not associative and the result would depend on the split. Section 30.3 becomes near-trivially true under this rule, and a test that passes while an accumulation exists is passing by luck.

Integer counts are the exception and are still not exempt from thought: section 29.1's distribution readout accumulates `int` counters, which is order-independent, and a test asserts it.

---

## 21. Configuration

Configuration is immutable after construction.

```go
type Config struct {
    SeaLevel float64

    ContinentalWavelengthMiles float64
    RegionalWavelengthMiles    float64
    LocalWavelengthMiles       float64

    WarpWavelengthMiles float64
    WarpStrengthMiles   float64

    RegionSizeHexes uint32
    ChunkSizeHexes  uint32

    Rim RimConfig
    // ...
}
```

Field names above are illustrative. The implemented configuration must include **every** weight, scale, threshold, and feature toggle that can alter generated output. Scale fields must state whether they are wavelengths or frequencies and in what unit — the names above use `WavelengthMiles` and `Hexes` suffixes for exactly this reason.

Validation must reject non-finite floats, non-positive sizes and scales, out-of-range normalized thresholds, and combinations that cannot be evaluated safely. It must also reject an fbm ladder whose octaves reach below the tile grid's Nyquist wavelength (section 9.3), because that combination is evaluable and wrong rather than unevaluable.

The complete effective configuration, including defaulted values, is authoritative database data for the single world. Creating a database writes that complete configuration before gameplay; reopening never silently substitutes current program defaults for missing stored values.

### 21.1 Decoding rules that protect the world

Two rules carry real weight here, and Go's defaults are against both of them.

- **An older binary reading a newer world file must *reject* it, not silently
  ignore the fields it does not understand.** This is the same guarantee section
  27 wants from the generator version gate, enforced one level down. Use
  `json.Decoder.DisallowUnknownFields()` for JSON, `cbor.DecOptions{ExtraReturnErrors:
  cbor.ExtraDecErrorUnknownField}` for CBOR, and check `toml.MetaData.Undecoded()`
  for TOML. None of the three is the default.

- **A missing field must be a refusal, never a zero value.** This is the Go
  hazard, and it is worse than the Rust one it replaces. Rust had to be told not
  to write `#[serde(default)]`; Go defaults *every* absent field silently, so a
  configuration file missing `SeaLevel` decodes to sea level `0.0` and generates
  a different world under an unchanged version number — precisely the failure the
  whole versioning scheme exists to prevent, arriving with no syntax to grep for.

  The loader must therefore require presence explicitly. Decode into the
  concrete type *and* collect the set of keys the decoder actually consumed,
  then compare that set against the exported field set obtained by reflection;
  any field not present is an error naming it. `toml.MetaData.Keys()` gives the
  consumed set directly; for JSON, decode a second time into
  `map[string]json.RawMessage`. A test constructs a file with each field removed
  in turn and asserts a refusal naming that field.

  Presence checking is acceptable to skip only on fields that cannot alter
  output, such as a human-readable world name.

- **No `omitempty` on anything that affects generation**, for the same reason
  read from the writing side: a zero weight that is legitimately zero must appear
  in the file, or the reader cannot distinguish it from an absent one.

### 21.2 Configuration fingerprint

Each algorithm version defines a canonical serialization of its effective configuration. Compute a stable fingerprint for cache identity and diagnostics:

```text
fingerprint = SHA-256( algorithmVersion LE bytes || componentWidthBits LE bytes || canonical CBOR of Config )
```

The component width is in the fingerprint because it is part of world topology (section 4.2), and a world file that says `int16` must never validate against a binary built at `int32`.

Rules:

- Serialize to **CBOR**, not JSON. JSON invites whitespace, float-formatting, and
  key-ordering variance; CBOR from a struct is deterministic in field order.
  `github.com/fxamacker/cbor/v2` in canonical/deterministic encoding mode.
- Hash `float64` values as their IEEE bits, never as a formatted string.
  Normalize `-0.0` to `0.0` before hashing, and reject `NaN` during validation so
  it can never reach the fingerprint.
- Do not derive the fingerprint from `fmt` output, Go map iteration, or any
  serialization with unspecified field ordering. Go map iteration is randomized
  per range statement; a fingerprint built over one is a different number every
  run.
- The fingerprint lives in package `config`, which is the only package that
  imports a CBOR library.

**The defaults are settled by this, not by intention.** Phase 7's remaining
tuning task is to fix the defaults an algorithm version ships with, and a
fingerprint over the complete effective configuration is what fixes them:
`config/fingerprint_test.go` carries the fingerprint of the default `Config` as a
written-down constant, so moving any default — a wavelength, a weight, a
threshold, an octave count, a rim width — fails that test on the spot. Updating
the constant is the compatibility decision, and it belongs in a commit message
alongside the `AlgorithmVersion` bump that goes with it.

The CBOR library's float encoding is worth knowing about: canonical mode writes each float in the shortest form that represents it exactly, so the encoding is a dependency's policy rather than this module's. That is still binary and still exact — the rule that a `float64` is hashed as its bits is kept — and the written constant is the tripwire if a release ever changes the policy.

---

## 22. Concurrency

A configured `Generator` is safe for concurrent read-only use.

```go
g := wgva.NewDefault(seed)

var wg sync.WaitGroup
for _, c := range coords {
    wg.Add(1)
    go func(c wgva.Coord) { defer wg.Done(); _ = g.Tile(c) }(c)
}
wg.Wait()
```

must be valid without external locking.

**Go cannot check this at compile time, and that is a real loss worth naming.** A Rust implementation gets a `Send + Sync` assertion that fails to compile the moment somebody adds interior mutability to the generator. Go has no equivalent, so three things stand in for it:

1. `Generator` holds no pointer to mutable state, no `sync.Mutex`, no channel,
   no map, and no function value with captured state. A review that sees any of
   those in the struct definition should stop.
2. `go test -race ./...` runs in CI over the concurrency test in section 30.3.
   The race detector finds the violation the type system would have refused.
3. Section 26's rule that caches must be concurrency-safe, optional, or outside
   the core stays a review checklist item rather than becoming a compile error.
   Write it down in the commit message when it comes up.

Do not import `unsafe` anywhere in this module. There is no reason for it in a pure-function world generator, and it is the only way to defeat the guarantees above.

---

## 23. Chunks

Chunks are useful for callers and caches but do not define geography.

Recommended starting size: `32 x 32` axial-addressed cells.

The simplest implementation divides `q` and `r` independently, with mathematical floor division:

```go
// Coord.Cell, which chunk, region, and macro-region addressing all share.
chunkQ := mathx.FloorDiv(int64(c.q), int64(size))
chunkR := mathx.FloorDiv(int64(c.r), int64(size))
```

One method, `Coord.Cell(sizeHexes uint32) (int64, int64)`, serves all three levels, with `Coord.CellOffset` for the position inside a cell. The size is `uint32` and zero is rejected, which is section 24's "assert positivity at the configuration boundary" made structural as far as Go allows: floor division agrees with truncating division only for a positive divisor, and an unsigned size with a zero check is the one place that can be guaranteed.

With chunk size `32`:

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

Negative coordinates are first-class. All helper math must behave correctly across the origin: floor division, modulo, region lookup, interpolation cells, chunk boundaries.

**Go's `/` truncates toward zero and `%` takes the sign of the dividend.** Neither is what any of the above wants. Go's standard library has no `div_euclid`, so the helpers stay:

```go
package mathx

func FloorDiv(a, b int64) int64
func FloorMod(a, b int64) int64
```

Two things follow:

- Test them exhaustively around zero, including `a` negative with `b` positive,
  which is the case that differs and the case that occurs.
- **Every divisor in this design is positive** — chunk size, region size, `6`.
  Assert that at the configuration boundary so the helpers never see a negative
  divisor, where floor and Euclidean division part company as well.

A `go vet`-style check that flags bare `/` and `%` on coordinate-typed values would be worth writing if this is ever got wrong twice.

---

## 25. Floating-Point Stability

**Read this section before writing any floating-point code.** Go can deliver
bit-for-bit reproducibility across targets, but the language specification
permits one thing that quietly breaks it, and the generation path is made almost
entirely of that one thing.

### 25.1 Go may contract a multiply-add into an FMA, and that is the hazard

The Go specification *permits* an implementation to fuse `x*y + z` into a single fused multiply-add with only one rounding. Noise code is almost entirely multiply-adds. On arm64 the compiler fuses; on amd64 targets without FMA it does not — so the same tile differs in its low bits between a developer laptop and a server, with nothing in the source to see.

The specification also gives the escape: **an explicit floating-point type conversion rounds to the precision of the target type, and the compiler must honor it.** So:

```go
// Fused on some targets. Never write this in the generation path.
v := a*b + c

// Rounded twice on every target. This is the rule.
v := float64(a*b) + c
```

Missing one site is a silent cross-platform world divergence. Two things make that less likely:

- **A named helper, used everywhere in the generation path:**

  ```go
  // Mul returns a*b rounded to float64, defeating any fused multiply-add the
  // compiler might otherwise form with a following addition. See DESIGN.md 25.1.
  func Mul(a, b float64) float64 { return float64(a * b) }
  ```

  The conversion is what carries the guarantee, and it survives inlining. A
  helper makes the rule greppable and makes its absence visible in review, which
  a bare conversion scattered through an expression does not.

- **The golden test runs on `GOARCH=amd64` and `GOARCH=arm64`.** That is the
  only thing that actually proves the rule is being honored. See section 30.9.

`math.FMA` is the explicit opt-in to fusion. **Do not call it anywhere in the generation path.** It is a different function with different results, and using it in some places and not others is how the hazard gets reintroduced by hand.

### 25.2 Restrict the generation path to exact operations

> **The generation path uses only `+`, `-`, `*`, `/`, `math.Sqrt`, `math.Floor`,
> `math.Abs`, `min`, `max`, and comparisons on `float64`. No transcendental
> functions.**

`math.Sqrt`, `math.Floor`, and `math.Abs` compile to exact operations or to a single hardware instruction specified by IEEE-754, and are bit-identical everywhere. `math.Sin`, `math.Cos`, `math.Exp`, `math.Pow`, and `math.Log` are not: Go's implementations are portable Go for some functions and architecture-specific assembly for others, and that split has moved between releases. Go is better placed here than a language that routes to the platform libm, but "better placed" is not "pinned", and a value that moves on a compiler upgrade invalidates every world.

The restriction is not a hardship. Every operation the design actually needs satisfies it:

- gradient, simplex, and value noise: multiply, add, floor, and table lookup;
- quintic and smoothstep interpolation: polynomials;
- the rim falloff (section 15.1): a smoothstep polynomial;
- domain warping (section 13): addition of field outputs;
- vector normalization: `math.Sqrt`;
- the multi-scale sum (section 10): weighted addition;
- ridge orientation (section 12): a stored unit vector, not an angle;
- climate falloff (section 16): a polynomial, not a cosine.

Two Go-specific notes:

- `math.Pow(x, 3)` is not repeated multiplication; write `x*x*x`. Go has no
  `powi`.
- The `min` and `max` builtins on floats propagate `NaN` and treat `-0.0` as less
  than `+0.0`, deterministically. That is fine, and it is one more reason
  validation must reject `NaN` before it can reach a comparison.

### 25.3 Fixed accumulation order

Never accumulate `float64` in an order that can vary. Floating-point addition is not associative, so a different order is a different number.

- Iterate the six directions in fixed order `0..6`, never over a map.
- Iterate octaves from coarsest to finest, always.
- Do not split a sum across goroutines in the generation path (section 20).
- Where a set of coordinates must be reduced, sort it first. `Coord` is a
  comparable value of two small integers; `slices.SortFunc` over a `[]Coord`
  beats a `map[Coord]struct{}` here and is the only form with a defined order.

**Go map iteration is randomized per range statement, by design.** Any reduction, any rendering order, any fingerprint input that ranges over a map is nondeterministic between runs of the same binary. This includes `hexg.HexSet` (section 7.1).

### 25.4 Nothing unstable may reach a persisted value

`hash/maphash` is seeded per process and is documented as such. `hash/fnv` is stable but belongs to the standard library's release policy rather than ours. Neither may be used for a fingerprint, a cache key that outlives the process, or anything in the generation path.

Section 21.2 specifies SHA-256 over canonical CBOR; section 8 specifies a mixer written out in this module. Both are stable because we own them.

### 25.5 Float-to-integer conversion

**Go leaves out-of-range float-to-integer conversion implementation-specific**, and in practice the result differs between amd64 and arm64. A classifier that converts a scalar to a band index without clamping first is a cross-platform divergence waiting for one tile to land out of range.

Always clamp explicitly, then convert:

```go
i := int(min(max(v, 0), float64(n-1)))
```

The clamp also makes the intended range visible at the call site, which the conversion alone does not.

### 25.6 Thresholds

Classification thresholds must not be pathologically sensitive to microscopic differences. Golden tests (section 30.9) verify representative coordinates bit-exactly; distribution tests (section 30.8) verify that thresholds are not sitting on a knife edge.

### 25.7 Build settings

There is no Go flag that relaxes floating-point semantics, which is a genuine advantage — the hazard in 25.1 is in the specification rather than in a build flag, so there is nothing to accidentally turn on. Do not chase it with `//go:nosplit`, assembly, or `unsafe`. Do not use `GOAMD64=v3` or any other microarchitecture level for a build whose output is compared against goldens; use it for local benchmarking if it helps and never quote a figure from it beside one without it.

---

## 26. Caching

The generator must work correctly with no cache. Applications may cache individual tiles, elevation samples, region parameter blocks, or rendered chunks.

Region parameter caching is the likeliest first optimization, since many nearby tiles reuse the same anchors:

```go
type RegionCoord struct {
    Q int32
    R int32
}
```

Introduce caching only after profiling, and be prepared for the profile to say *don't* — section 31.1 is how that question gets asked. WGVB asked it and the answer was no; see section 27.6.

If it is ever needed, a small direct-mapped array — `[64]struct{ key RegionCoord; ok bool; params RegionParams }` indexed by a few hash bits — beats a `map` for this access pattern and avoids hashing entirely. Either way it is mutable state reached through a read-only method, so it needs a lock or per-goroutine ownership and therefore belongs **outside** `Generator` per section 22.

Correctness must never depend on cache history.

---

## 27. Persistence and Versioning

Generated terrain is a function of:

```text
algorithm version + component width + seed + configuration + coordinates
```

A WGVA database contains exactly one world and everything needed to reproduce that world's generated baseline.

```go
const AlgorithmVersion uint32 = 1
```

The constant lives in the root of the `wgva` package, and its doc comment carries the version history: what each bump changed and why it had to be a bump. Read that before adding an entry.

Two kinds of bump are worth distinguishing in advance, because WGVB found that most of its bumps were the second kind and the first reading of a version number is usually wrong:

- A bump that **moves generated values** — a noise formula, a threshold, a hash
  domain, a weight, the axial-to-world embedding, the component width.
- A bump that **moves no value at all** and is owed entirely to `Config` gaining
  a field. Section 21.1 forbids defaulting a missing generation-affecting field,
  so a world file written under the older version genuinely cannot be reopened
  even though every tile in it would be identical. Say which kind it is in the
  commit message.

The database persists, in singleton world metadata:

```text
world seed
algorithm version
component width in bits
complete effective generator configuration
configuration fingerprint
```

Changing noise formulas, thresholds, or hash domains changes existing worlds. Treat such changes as generation-version changes unless compatibility is intentionally preserved.

### 27.1 Store: SQLite via `zombiezen.com/go/sqlite`

Use `zombiezen.com/go/sqlite`. Do not introduce a `database/sql` SQLite driver, an ORM, or a query builder.

Every database sets:

```go
const ApplicationID int32 = 0x57475641 // ASCII "WGVA"; decimal 1464292929
```

Set this through `sqlitemigration.Schema.AppID`. Note this differs from WGVB's `0x57475642`. A WGVB file must be rejected by a WGVA binary at the first gate, and vice versa.

### 27.2 Coordinate-keyed tables are `WITHOUT ROWID`

Every table keyed by canonical `(q, r)` — overlays and any cache — is declared:

```sql
CREATE TABLE overlay_settlement (
    q INTEGER NOT NULL,
    r INTEGER NOT NULL,
    -- ...
    PRIMARY KEY (q, r)
) WITHOUT ROWID;
```

This matters more than it looks. A `WITHOUT ROWID` table *is* a B-tree keyed by the composite primary key, so loading every overlay in a viewport or a chunk is one ordered range scan rather than a rowid lookup per row through a secondary index. It gives the coordinate locality that would otherwise be the main reason to reach for a dedicated key-value store. See appendix C.

Columns are `INTEGER` regardless of component width, because SQLite has one integer type. The width therefore never appears in the schema — it appears once in the world metadata and once in `Component` (section 4.2) — and a width change needs no migration, only a refusal.

### 27.3 No foreign keys to generated data

The previous revision required foreign keys for relational integrity. **That requirement is dropped.** In a single-world database whose only relational structure is coordinate-keyed sparse overlays, the only plausible foreign-key target is a tiles table that this very document calls a discardable cache. Authoritative player state must never have a referential dependency on regenerable data — dropping the cache would then require dropping the player's settlements.

Enable `PRAGMA foreign_keys = ON` on every connection anyway, so that any constraint deliberately added later between two authoritative tables is actually enforced. Migration and database tests must use the same connection preparation as production, or the pragma is on in one and off in the other.

### 27.4 Migrations

Use `zombiezen.com/go/sqlite/sqlitemigration` and add migrations to `sqlitemigration.Schema.Migrations` in order. **Never edit an already-released migration.** Use repeatable migrations only for objects such as views and triggers that are intentionally recreated.

`sqlitemigration` owns `PRAGMA user_version` as the schema migration version, and it does **not** reject a schema newer than the binary — gate 2 below is ours to implement before calling it. SQLite has no application-defined pragmas, so the generator algorithm version and the component width live in the singleton world metadata rather than in a pragma.

This is one of the few places WGVA keeps a dependency WGVB dropped. WGVB wrote its own forty-line ladder because Rust had no equivalent; Go has a good one from the same author as the SQLite binding, and rewriting it would be owning code for its own sake. The rule that matters — the migration ladder is part of the file format and must not move because a dependency did — is served by pinning the version and by section 30.11.

### 27.5 Opening gates

```text
1. Verify PRAGMA application_id is WGVA (an empty database is initialized instead).
2. Read PRAGMA user_version and reject a schema newer than the binary.
3. Apply supported ordered schema migrations.
4. Read and validate the singleton world metadata and complete configuration.
5. Reject a component width this binary was not built for.
6. Reject a generator version the binary cannot reproduce.
7. Permit normal reads and writes.
```

Each gate is one sentinel error (section 19.1):

```go
var (
    ErrWrongApplicationID    = errors.New("not a WGVA database")
    ErrSchemaTooNew          = errors.New("schema is newer than this binary supports")
    ErrWrongComponentWidth   = errors.New("world was generated at a different coordinate width")
    ErrUnsupportedGenVersion = errors.New("world was generated by an algorithm version this binary cannot reproduce")
    ErrMalformedMetadata     = errors.New("world metadata is missing or malformed")
    ErrFingerprintMismatch   = errors.New("stored configuration does not match its fingerprint")
)

// OpenError carries the specifics and wraps one of the above.
type OpenError struct {
    Found    int64
    Expected int64
    Detail   string
    Err      error
}
```

No gate may perform an application write before it passes. Older generator versions are rejected unless the binary deliberately retains their implementations. **A generator incompatibility must never be handled by silently regenerating the world with current rules** — every overlay coordinate would then point at terrain that no longer exists there, silently, because a coordinate still resolves.

Gate 5 is new in this revision and exists because of section 4.2: the alpha and the shipping world are the same code at two widths, and a file from one must not open under the other.

### 27.6 What is authoritative and what is not

Persist mutable game and player state as sparse overlays keyed by canonical `(q, r)`. No `world_id` column: one database holds one world — and one world's game. The world and the game are not separate files; appendix C, *One file, not two*, records why.

SQLite columns are `INTEGER` and will hold anything, so **validate coordinates on read.** A row whose `q`, `r`, or derived `s` falls outside `±WorldRadius` is malformed data, not a distant tile: refuse it rather than passing it through `NewCoord`, which would silently relocate a settlement to a real coordinate somewhere else. Section 4.1 is the range; a normalizing read is the one shape of repair that must not happen here.

Generated tiles, generated chunks, and PNG files are reproducible caches, not authoritative records, and may be discarded at any time. Any cache entry must carry — and be validated against — the configuration fingerprint and, for rendered output, the palette/render version.

**Do not build the tile cache in the first implementation.** Measure first, with section 31.1. A cache that is never faster than regeneration is pure liability: a fingerprint to validate and a staleness bug to hit.

**Measured in WGVB, phase 7: no tile cache.** The three tile rows of its harness agreed within ten percent of each other, which is what a pure function with no shared state should look like — single tiles, a chunk fill, and a radius-32 region all cost about the same per tile, because there is nothing shared between tiles to amortize. Two things follow, and neither depends on the absolute figure or on the language. A cache would have to be validated against the configuration fingerprint on every hit. And the work it would save is already embarrassingly parallel: section 20 permits a goroutine batch fill, and the core count is a larger and simpler win than any cache with a coherency story. Revisit this when a profile shows the same coordinates being generated repeatedly, which a bounded viewport render does not do — not when a tile is merely slower than somebody hoped.

**A player's frame is authoritative on the same terms.** The origin hex and rotation each player is assigned at creation are not derived from the seed, are never regenerated, and survive discarding every cache — and they are what gives every other piece of player-facing state its meaning, since a settlement one player calls `(3, -1)` is a different tile under a different frame. Section 28 says where the three scalars live and appendix A has the transform.

### 27.7 Connection handling

A `*sqlite.Conn` must not be used concurrently. Either give each goroutine its own connection or hold one behind a mutex. Return every non-nil connection obtained from a `sqlitemigration.Pool` to that same pool, including on the error paths — a `defer` immediately after the successful `Take` is the only form that survives a later edit.

Do not build a pool beyond what `sqlitemigration` already provides before there is a measured reason for one; the read volume against authoritative data is small, and the generator — the part that is actually hot — touches no database at all.


---

## 28. Package Layout

```text
wgva/
    go.mod                      one module; dependency versions pinned here
    DESIGN.md
    AGENTS.md
    README.md
    docs/renders/               acceptance sheets, by algorithm version
    version.go                  semantic version of the binary
    algorithm.go                Component, WorldRadius, AlgorithmVersion
    coord.go                    Coord, normalization, neighbors, rotation, cells, rim distance
    hash.go                     domain constants, mixers
    noise.go                    owned noise implementation (unexported)
    field.go                    Field, fbm, warp, sampling offset
    config.go                   Config, ConfigError, validation
    generator.go                Generator, Tile, Sample, batch
    tile.go                     Tile and every classification type
    elevation.go
    climate.go
    basin.go                    basin influence and volcanic tendency
    terrain.go
    relief.go
    region.go
    rim.go                      the rim profile; section 15.1
    internal/
        mathx/                  FloorDiv, FloorMod, Mul, exact 128-bit helper
    config/                     what a configuration weighs: bytes and file
        fingerprint.go          canonical CBOR and SHA-256
        file.go                 the TOML file a person edits
    render/                     bounded viewport rendering; owns the hexg dependency
        render.go               Viewport, Grid, Layer, Render, RenderGrid, RenderPlayer
        palette.go              the diagnostic ramp, climate table, terrain list
        overlay.go              Overlays; what the player knows
        frame.go                PlayerFrame; player-relative <-> canonical
        distribution.go         Distribution; what is in a window
    view/                       the window grammar both web front ends present
        view.go                 View, Compass, ViewError, seed and query parsing
    store/                      single-world SQLite persistence
        migrations/
        store.go                ApplicationID, OpenError, the migration ladder
        world.go                World; creation and the opening gates
        overlay.go              sparse coordinate-keyed player overlays
        player.go               player frames: origin q, origin r, rotation
    cmd/
        wgva-tune/              the terrain tuning tool          — phase 2
        wgva-map/               one window to one image file     — phase 8
        wgva-serve/             the map viewer                   — phase 8
```

Dependencies flow one direction only:

```text
cmd/wgva-tune   ->  view  ->  render  ->  wgva
      |                          |
      +-> config ----------------+------>  wgva
      (deliberately no store edge)

cmd/wgva-map    ->  render  ->  wgva
      +-> store  ------------->  wgva
      +-> config ------------->  wgva

cmd/wgva-serve  ->  view  ->  render  ->  wgva
      +-> store  ------------->  wgva
```

**Go's import cycle rule is doing real work here.** `store`, `render`, `config`, and `view` all import `wgva`, so `wgva` **cannot** import any of them — the compiler refuses, with no lint and no review needed. "Do not introduce persistence into the core package" stops being advice and becomes a fact of the build. The previous revision could only ask.

What the cycle rule does *not* give is the other direction, so one rule needs a test:

> **`cmd/wgva-tune` must never import `store`, directly or transitively.** It
> cannot open a world, create one, or write to one, and that is what makes the
> claim safe to make out loud rather than carefully. Nothing in the language
> enforces it, so `cmd/wgva-tune/deps_test.go` runs `go list -deps` over the
> package and fails if `github.com/mdhender/wgva/store` appears.

Two intermediate packages exist because a thing would otherwise be written twice:

- **`view`** owns the window grammar — what `?q=`, `?cols=`, and `?layer=` mean,
  what a scroll step is, and which refusal a malformed one earns. There are two
  web front ends and that grammar carries invariants with tests behind it, so it
  is one package rather than one copy each.
- **`config`** owns what a configuration *weighs*: the canonical CBOR of section
  21.2, the fingerprint over it, and the TOML file a person edits. It is separate
  from `store` so that a tuning tool can name a fingerprint without linking
  SQLite — a tuner that linked SQLite would be a tuner that could open a world.

**Where the player frame lives.** `Coord.Rotate` belongs in the core package: it is a pure coordinate operation, and the rotation is required there anyway to generate the mirror centers. `PlayerFrame` belongs in `render`, because a frame exists to present the world to somebody and that is where the flat-top and screen-axis offsets already compose. `store` persists only the three scalars — origin `q`, origin `r`, rotation — so it needs no dependency on the frame type. `render` and `store` are siblings, so this placement is also what keeps a player concept out of `wgva` itself; see appendix A.

The store's half is a `player` table keyed by player identity rather than by `(q, r)`, because the origin is a value there and not a key. It is written once, when the player is created, and there is no operation that moves a frame afterwards — origin and rotation are what every coordinate and heading that player has ever been given *mean*, so changing one would silently relabel all of it. The rotation is validated to `0..6` on write and again on read; an out-of-range stored value is a typed refusal, never a `FloorMod`.

`cmd/wgva-serve` is a separate command rather than a mode of `cmd/wgva-map`, and both are separate from `cmd/wgva-tune`. Each has a different standing — one writes a file, one shows a saved world, one changes a configuration — and collapsing them would make `--help` a lie about what the tool can touch.

---

## 29. Rendering and the Tools Around It

Rendering is bounded even though generation is effectively unbounded. Every render request defines a finite viewport and an explicit pixel scale. Renderer pixel coordinates never feed back into generation.

`render` owns the window and everything measured over one. The layers, which are `render.AllLayers` and the same names every front end takes:

```text
continentalness, regional, local, detail, elevation-raw,
elevation, relief, ridge, roughness, region-influence,
temperature, moisture, climate,
basin, volcanic,
rim,
terrain
```

Fifteen of those are scalar ramps, `climate` is the two-axis band table, and `terrain` is a vocabulary of swatches — which is why `Layer.Key` returns one of three shapes rather than a ramp with two labels. Four of the ramps are the raw noise scales of section 10 and exist to separate "the noise is wrong" from "the composition is wrong" when a window looks off; nothing in the generator reads them. The `rim` layer draws `RimDistance` and the falloff profile, so the band can be tuned without hunting for the world's edge in the terrain layer.

`Layer.Cost` is the other thing a front end needs from this list: `relief`, `climate`, and `terrain` cost seven generator evaluations a tile because they read the six neighboring elevations, and every other layer costs one. The tuning tool's budget is counted in those, not in tiles.

Rendering notes that apply to every front end:

- **The renderer is where north exists.** It draws the admin frame: flat-top,
  image `y` downward, no rotation, which puts absolute direction `2` at the top
  of the image. Reading the compass clockwise from there — N, NE, SE, S, SW, NW —
  walks the direction index backwards, `2, 1, 0, 5, 4, 3`, because index order is
  counter-clockwise as a viewer sees it. Appendix A's *Rotation senses* is the
  whole story and the one place to change it; a test in `render` pins the
  mapping, because a mirrored layout would keep every golden pixel passing while
  sending every printed heading the wrong way.
- Use `hexg` layouts and hit testing for pixel geometry, and `image/png` for
  encoding.
- Render coordinates in a stable sorted order. Overlapping edges and labels make
  output order-dependent otherwise, and a `hexg.HexSet` is map-backed.
- Keep generated terrain separate from player overlays — discoveries, fog of war,
  settlements, labels, annotations — and compose the layers at render time.
- **Golden-compare decoded RGBA buffers, not PNG file bytes.** Go's `image/png`
  encoder chooses filters and compression settings that can change between
  releases, producing different bytes for identical images. Comparing pixels
  tests what we actually care about.

### 29.1 The terrain tuning tool

**This is the first thing to build after the fields exist, and the single most
valuable artifact the Rust implementation produced.** It was built in phase 7
there, and everything before it was tuned by rendering a PNG, opening it,
working out the next window's coordinates by hand, editing a constant, and
rebuilding. Building it in phase 2 here is the main structural change in this
revision.

`cmd/wgva-tune` **runs on a developer's machine and produces a settings file.
Nothing is deployed, no player ever touches it, and it does not create or open a
database.** That sentence is the tool's boundary and is worth repeating whenever
it comes up; see the naming guidance in `AGENTS.md`.

```text
GET  /seed/{seed}                  the hex map, drawn as the viewer draws it
GET  /seed/{seed}/map.png          that window's image
GET  /seed/{seed}/grid             one pixel per hex, for a very large area
GET  /seed/{seed}/grid.png         that window's image
GET  /seed/{seed}/config           the complete effective configuration, as a form
POST /seed/{seed}/config/fields    apply the form
POST /seed/{seed}/config/upload    adopt an uploaded or pasted file
POST /seed/{seed}/config/reset     go back to this binary's defaults
GET  /config.toml                  download what is being drawn
```

**It is stateful, and that is the one thing it cannot share with the viewer.**
The thing it exists to change is the complete effective configuration — on the
order of a hundred numeric fields — and a hundred fields do not fit in an
address bar. So the configuration lives in the process's memory, a form changes
it, and a `POST` is how. Trying to make one tool do both this and the viewer's
job is where most of the complexity went the first time, and all of that
complexity came from the same place: an override mechanism expressive enough to
tune with *is* a configuration file, and a configuration file does not belong in
a query string.

**Everything about the view is still in the URL.** Only the configuration is not. Tabs are routes, the turn and the scale are parameters, and the controls a person clicks — zoom, window size, seed, jump-to-coordinate — are links and `GET` forms, so the address bar keeps up and the back button still works.

What it gives up is real and is named on every page: a link to a window shows what that process is drawing *now*, not what it drew when the link was copied. What it keeps is the fingerprint. Every tab prints it, the download turns the configuration behind it back into a file, and `wgva-map --config` draws that file — so a picture can always be traced to the configuration that produced it and reproduced outside the tool.

#### The grid

One pixel per hex — or N, at an explicit scale — so that a million tiles can be looked at in one image. `Viewport` is a rectangle of even-`q` offset cells and `Viewport.CoordAt` is the offset conversion, so the grid is the same window walk with hex hit testing dropped, sharing one function with the hex path. It lives in `render` as `RenderGrid`, and `wgva-map --grid` draws it too, which is how the sheets under `docs/renders/` are made.

At the alpha component width the grid can draw a recognizable picture of the *whole world* at a coarse scale, which is the other reason to develop at `int16`: "is the rim right everywhere" and "do the continents cover the world plausibly" are questions you can answer by looking. At the shipping width no such image exists.

What it distorts: a hex row's centers are `sqrt(3) r` apart and a column's are `1.5 r`, so drawing both as one pixel stretches the image vertically by about 15% and flattens the half-hex column stagger. That is the price of the view and it is stated on the page.

Rendering is parallel — goroutines across columns — which is exactly the permission section 20 grants: every cell is a pure function of its own coordinate written to its own slot, so the scheduling split cannot reach the result, and nothing here accumulates across tiles.

**The bound is a budget counted in generator evaluations, not in tiles.** `relief`, `climate`, and `terrain` cost seven evaluations apiece and every other layer costs one, so a tile count could not tell a cheap window from one seven times longer. Over budget is a 400 naming the number, and `--budget` raises it, because measuring how long a large window takes is a thing this tool is for.

#### The turn

A `turn` in `0..6` rotates the sampled region a sixth of a turn about the window centre. A hex grid rotated by a sixth maps onto itself exactly, so nothing is resampled and nothing interpolates: `Coord.Rotate` does it in integers, and none of section 25 is disturbed. `turn = 0` is the identity, which is what lets every golden and every byte-agreement assertion stand unchanged.

It is the drawing half of `PlayerFrame`, which converts coordinates and does not draw. Rendering through a player's frame is this primitive with the pivot at the frame's origin and `t = rotation - 2`, where the `-2` is the layout constant — screen-up is absolute direction 2 and a frame's north is relative direction 0.

On the **grid**, four of the six turns shear the picture, and the page says so. The grid's distortion has two-fold symmetry while the hex grid has six-fold, so only turns 0 and 3 lie in both; the other four change angles and introduce apparent directionality that varies with the turn — which is exactly the artifact a turned view is usually being used to look for. The hex tab needs no such warning at any turn.

#### The distribution readout

The map tab counts what is in the window it is drawing: the terrain mix, the elevation band histogram, and the two climate ladders. The statistical tests of section 30.8 measure this globally — every terrain reachable, none dominating — and what they cannot say is whether the threshold somebody just moved did what they meant *here*. In WGVB, moving one dryness threshold by `0.1` took a coastal window from 6% dry to 62%, and from 70% plains to 33% plains and 38% grassland; the picture says something changed and the readout says what.

Every terrain is listed, including the ones with no tiles, because a row reading zero is usually the row somebody is trying to move off zero.

`Distribution` lives in `render` — it is a measurement over a window, and a window is that package's — so a front end and the CLI cannot disagree about what a window contains. It costs a whole `Tile` per cell, which is seven evaluations whatever layer is on screen, so the map *page* goes through the same evaluation budget the images do, and the grid tab does not show one: a million tiles of readout is seven million evaluations for a second copy of work the image already did.

These are integer counts accumulated in one goroutine. That is not the accumulation section 20 forbids in a batch fill — that rule is about a scheduling split deciding a floating-point sum — and a test asserts the count does not depend on the order the coordinates arrive in.

#### The configuration file

TOML, flat, one key per line, with the algorithm version, the component width, and the fingerprint in a comment header.

Write it directly rather than through an encoder's default float formatting: emit each `float64` with `strconv.FormatFloat(v, 'g', -1, 64)`, which is the shortest representation that parses back to the identical bits. Parse with a TOML library, check `MetaData.Undecoded()` for unknown keys and `MetaData.Keys()` for missing ones (section 21.1), and reject either. The settling test is the round trip asserted on the fingerprint as well as on the values, because a file that loses a low bit is a silently different world.

#### Serving

`net/http` from the standard library. There is no dependency to choose here and no async runtime question to have — this is one of the places Go is simply better placed than the Rust implementation, which spent a paragraph justifying `tiny_http` over `axum`.

What Go's ease does not remove is the need to bound the work:

- `cols`, `rows`, and `hex-radius` are clamped before a `Viewport` is built. The
  clamps are odd numbers, so a clamped request still has a center cell.
- Concurrent renders are limited to `runtime.GOMAXPROCS(0)` by a buffered
  channel used as a semaphore. `net/http` starts a goroutine per request and
  will happily start ten thousand; the work is CPU-bound, so unbounded
  concurrency turns one careless reload into a stalled machine.
- A render error becomes a 400 with a readable message, never a 500. Nothing a
  caller can type is the process's fault.
- **Bind to `127.0.0.1` by default.** There is no authentication and the
  endpoint's cost is chosen by the caller. A `--host` flag exists; the default
  must not be `0.0.0.0`.

**The `ETag` stays strong.** It stops being a pure function of the URL, which is the departure, but the configuration's fingerprint is in the tag: move a field and every tag moves with it, so a browser holding an old image asks again. Use eight bytes of it rather than the four a page prints, because a tuning session walks through hundreds of configurations under otherwise identical URLs.

#### Same standing as every other front end

`cmd/wgva-tune` is not a second renderer. Whatever it draws goes into `render` or into the URL, and a test in `cmd/wgva-map` asserts it: the bytes the CLI writes and the bytes the tool returns for one window — grid and hex, every turn, and a tuned configuration through a file — are the same bytes.

### 29.2 The map renderer

`cmd/wgva-map` renders a bounded image of an arbitrary window to a file. It is **deferred to phase 8**: the tuning tool renders the same windows through the same code and answers the questions that come up while tuning. Build it when something outside a browser needs an image — an acceptance sheet under `docs/renders/`, a bug report, a golden.

```bash
wgva-map \
    --db world.wgva \
    --q -200 \
    --r -150 \
    --cols 400 \
    --rows 300 \
    --hex-radius 8 \
    --layer terrain \
    --out map.png
```

Use the standard library `flag` package. The database supplies the seed, the algorithm version, the component width, and the effective configuration. If this command creates a new database, creation writes all of those values before rendering; it must never override them when opening an existing database. `cols` and `rows` are tile counts; `hex-radius` is a pixel dimension.

`--config` draws a configuration file from the tuning tool with no database at all, and `--grid` draws the one-pixel-per-hex view. Both are diagnostic and neither represents a saved world; the output should say so where it can.

### 29.3 The map viewer

`cmd/wgva-serve` shows a world somebody saved, without starting the game engine. It is **deferred to phase 8**, because it needs a saved world and nothing before phase 8 has one.

It is not the tuning tool and does not replace it, and the difference is one promise:

- **The viewer is stateless.** Every state it can be in is a URL, so a link means
  the same thing to everybody who opens it and the same thing tomorrow.
- **The tuning tool is stateful**, for the reason in section 29.1.

```text
GET /seed/{seed}                        the viewer page, centered on the origin
GET /seed/{seed}?q=-87&r=6543           the viewer page, centered on (-87, 6543)
GET /seed/{seed}/map.png?q=..&r=..      the rendered window itself
```

**This is deliberately not a single-page application.** No client-side panning, no canvas, no script needed to move the view; page refresh is fine. Every state the viewer can be in is a URL, which also makes "look at this" a link somebody can paste into an issue, and makes the round trip testable: the links the page emits must parse back to the state that produced them.

`{seed}` is sixteen hexadecimal digits, case-insensitive, with no `0x`, because that is how a seed is written everywhere else. Note the spelling disagreement this creates with `wgva-map --seed`, which takes decimal because that is what `flag` does with a `uint64`. Teaching the CLI the hex spelling too is worth doing.

Coordinates in the URL are **canonical**, not player-relative. This viewer has no player, a canonical link means the same tile to everybody who opens it, and it is the same number `wgva-map --q --r` takes, so a window moves between the two tools without a conversion. A frame-relative viewer would have to say so in the URL — `?pq=&pr=` — rather than leaving two readings of the same link.

Defaults, all overridable by query string and all clamped: `q` and `r` at the origin, `cols` 61, `rows` 45, `hex-radius` 10 pixels, `layer` `elevation`.

**Odd tile counts matter more than they look.** `NewViewport` takes the window's *first* tile, not its center, so a viewer has to convert; with an even count there is no center cell, the conversion has to round, and a rounded conversion makes a scroll step that returns to where it started stop returning to where it started. The conversion is `Viewport.CenteredOn`, in `render`, because it is offset-scheme arithmetic and `render` owns the offset scheme. It rejects an even count rather than rounding one.

**Scroll distances are whole hexes**, counted in tiles and never in pixels, so a step means the same thing at every zoom and a step followed by its opposite returns to exactly the coordinate it started from. North and south move `rows / 2` hexes; the four diagonals move `cols / 2`. Both are integer division of an odd count. The new center is `n` whole steps of a direction vector, and there is no offset-coordinate arithmetic in the server at all: `NewCoord` normalizes, so scrolling off an edge of the canonical hexagon wraps to the opposite edge with no special case. Under a configured rim the traveller runs into forced terrain before reaching the seam, which is the point of section 15.1; with the rim switched off it looks like a seam, because it is one.

The six controls are the compass walk of appendix A in the admin frame, which the renderer draws without rotation: N is absolute direction 2, and the walk clockwise from there *decreases* the index, `2, 1, 0, 5, 4, 3`. When a player frame arrives, north becomes absolute direction `k` and the same walk applies. No rotation reaches the generator.

Bounded like everything else that renders, on the same terms as section 29.1, plus:

- **The page names the tile at its center**, not only the coordinate: the terrain,
  the elevation band, the two climate bands, and the scalars they were classified
  from. A link to a window is otherwise a link to a picture, and a reader has to
  count swatches against the key to find out what they are looking at. It costs
  one tile against the `cols * rows` the image beside it costs.
- The PNG is a pure function of seed, center, window, layer, `AlgorithmVersion`,
  `RenderVersion`, and the configuration fingerprint, so it carries a strong
  `ETag` built from exactly those.
- **The page is a second representation and needs a revision of its own.** Those
  versions describe the world and the pixels and say nothing about the HTML, so
  a release that changes the markup alone would emit the same strong tag for
  different bytes and a browser would go on showing the old page — which is the
  one thing a strong validator exists to prevent. `PageVersion` is that revision;
  it appears in the page tag and deliberately not in the image tag, so changing
  the markup does not invalidate a cached PNG.
- **A world-backed image carries no tag at all**: it also depends on the
  overlays, which are mutable player state with no version anywhere in the
  system, and a tag that ignored them would go on serving an unexplored map after
  the player explored it.

Two modes, and the seed means something different in each. Without `--db` the generator is constructed in memory from the seed in the route and the default configuration, so **output is diagnostic and does not represent a saved world**, and the page says so; every seed is servable, because the route is where the world comes from. With `--db` the database supplies the seed, the algorithm version, the component width, and the complete effective configuration, and **the seed in the route is a check against the stored one rather than the source of it** — another seed is a 404 naming the one this server holds. Overlays are read fresh from the database on every request, so exploring a world and refreshing shows the exploration.

Open one connection per worker **before the port is bound**: a database that fails an opening gate is a server that does not start rather than a server that answers every request with a 500. The server opens and never creates — `wgva-map --db` creates a world, because creating one is a decision rather than a side effect of a typo.

**The server and the CLI must agree byte for byte.** Two front ends over one renderer must not be allowed to drift, and that is one assertion rather than a second set of goldens: the existing golden image already pins what the renderer draws, and a second copy of it in the server's package would only pin it twice.

### 29.4 Player overlays

Generated terrain and player overlays are stored apart (section 27.6) and meet in exactly one place: `render.RenderPlayer`, at render time, in pixels. Nothing composed there can reach back into generation, and a tile's terrain is the same whether or not anybody has ever looked at it.

`Overlays` is a plain value — sorted slices of coordinates and of coordinate/name pairs — and `render` does not import `store`. The two are siblings, and a renderer that could open a database would be a renderer that could be handed a world rather than a viewport. The command does the loading: one range scan over the smallest `(q, r)` box holding the window's tiles, which is a superset for a wrapped window and correct for the same reason, since an overlay outside the window is never drawn.

Sorted rather than map-backed, because markers overlap pixels. Section 29 requires a stable render order, and a Go map would decide which of two touching markers wins by whatever the runtime felt like this iteration.

Two composition rules, and they differ on purpose:

- **Fog hides terrain.** An undiscovered tile is drawn as the fog color rather
  than as what is there, for every layer identically — a fogged tile that leaked
  its heat band would be a map telling the player the climate of ground they have
  never seen.
- **Fog does not hide the player's own marks.** A settlement marker is drawn
  whether or not the tile under it is discovered. It is something the player
  built; hiding it would be the map lying to its owner.

**An empty discovery set means fog is switched off, not that nothing has been seen.** A world that records no discoveries is one where exploration is not being tracked, and rendering it as a solid rectangle of fog would be an alarming way to say so. One discovered tile switches it on.

`RenderVersion` does not move for overlay work. Every pixel the terrain renderer produces stays bit-identical to what it produced before overlays existed; bumping it would claim a cache of terrain PNGs is stale when it is not. Note also what that version does *not* cover: a cached player PNG depends on the overlays as well as the palette, and overlays are mutable player state with no version at all. That is a reason not to cache one.


---

## 30. Testing Strategy

Test invariants, not whether a map "looks right".

Derive expected values independently rather than recording whatever the code currently produces. A test written by running the code and pasting the output asserts that the code does what it does.

### 30.1 Determinism

The same seed and coordinate return an identical tile, including every `float64` bit. Compare with `math.Float64bits`, not with `==`, so a `NaN` that should never exist cannot pass by comparing unequal to itself and being skipped.

### 30.2 Order Independence

Generating a set of coordinates forward, backward, and shuffled produces identical results.

### 30.3 Concurrent Determinism

A goroutine-parallel fill and a sequential fill of the same coordinates agree bit-for-bit, under `go test -race`. Section 20's no-accumulation rule is what makes this hold; a test that passes here while an accumulation exists is passing by luck.

### 30.4 Negative Coordinates

Test `mathx.FloorDiv` and `mathx.FloorMod` exhaustively around zero for chunk assignment, region assignment, interpolation cell selection, and direction normalization.

Test each of the normalizer's three stages **directly** rather than hoping a coordinate reaches it. Stage 3 in particular is unreachable from any coordinate the program produces, so it needs inputs chosen for it: values near `math.MaxInt64` and `math.MinInt64` on each axis and on both, checked against an independently computed answer.

Test the symmetric-domain constraint of section 4.1 in its own right, because it is the one invariant that a two's-complement type will not enforce for you:

- Over a large sweep of inputs, no `NewCoord` result has any of `q`, `r`, `s`
  equal to the extreme negative value of `Component`, and every result satisfies
  `|v| <= WorldRadius`.
- `NewCoord(-WorldRadius, 0)` and its five rotations are canonical and distinct,
  and negating any canonical coordinate componentwise stays in range.
- A `Coord` built inside the package with a component at the extreme negative
  value is rejected by the normalizer's range check rather than accepted, and
  `RimDistance` is never negative for any canonical coordinate.
- The tile count implied by `WorldRadius` matches `1 + 3N(N+1)`, which is the
  arithmetic statement that the excluded value costs no tiles.

### 30.5 Region Boundary Continuity

Elevation, heat, and moisture change smoothly across region anchor boundaries. No step change correlates with a region index change.

### 30.6 Chunk Boundary Continuity

The same, for chunk boundaries. Chunks are addressing only.

### 30.7 Neighbor Coherence

Adjacent tiles have related values. Assert a bound on the distribution of neighbor deltas, not on any individual pair.

### 30.8 Distribution Tests

Over a large sample of coordinates, land fraction, elevation histogram, heat bands, moisture bands, and terrain frequencies fall in expected ranges. These catch a threshold sitting on a knife edge.

This test also stands in for the exhaustiveness Go's type system cannot give (section 14.1): **every declared band and every declared terrain must be produced somewhere in the sample**, except the two inland-water values that section 17.1 deliberately omits, which must be produced nowhere. A terrain that is unreachable because of a threshold typo is otherwise invisible.

Sample the rim as well as the interior, with the rim configured and with it switched off.

### 30.9 Golden Coordinates

A table of representative coordinates and their exact expected tiles, including `float64` bit patterns. Add goldens only for an intentionally stable algorithm version; updating them requires an explicit compatibility decision recorded in the commit message.

**Run the golden test on `GOARCH=amd64` and `GOARCH=arm64`.** It is the only thing that actually proves section 25 is being honored, and section 25.1's fused multiply-add is exactly the defect that shows up on one of the two and nowhere else. A golden suite that has only ever run on one architecture is a golden suite that has not been run.

### 30.10 Wrapped-Edge Continuity

Compare physical fields on corresponding tiles at all six wrapped edge pairs. A coordinate and its wrapped image must be bit-identical — that is tile identity and it must hold unconditionally.

Smoothness across the seam is a separate claim and is **not** asserted, because section 7.1 no longer promises it. What is asserted instead is that the discontinuity exists only where it is expected to, and that a future change that makes the fields periodic fails this test loudly rather than passing quietly — so the documentation and the code cannot drift apart.

These tests run with the rim switched off, which is the configuration that exposes the seam. At the alpha component width they can walk to it.

### 30.11 Database Compatibility

Verify that opening rejects each of: a non-WGVA application id (including a WGVB file), a schema newer than the binary, a world generated at a different component width, an unsupported generator version, malformed or incomplete singleton metadata, a configuration whose fingerprint does not match, and an invalid configuration — each **without performing any application write**. Verify that supported older schemas migrate in order and retain the same single-world metadata.

Assert with `errors.Is` against the sentinels of section 27.5, never on message strings.

### 30.12 Shape Assertions

Go has no compile-time assertion, so these are ordinary tests:

- `reflect.TypeOf(Tile{}).Size()` is within its documented bound.
- `Tile` contains no pointer, so a `[]Tile` is one allocation and the garbage
  collector never scans it. Walk the type with `reflect` and fail on any
  pointer-shaped field.
- `Generator` contains no mutex, channel, map, or function value. The same walk.

The third is the closest thing available to the `Send + Sync` assertion a Rust implementation gets for free (section 22), and unlike that one it can be defeated. It is still worth having: it catches the cache somebody adds to `Generator` in a hurry.

### 30.13 No Place Is Special

Section 9.4's rule — that no coordinate may be distinguishable by how the implementation addresses it — needs a test of its own, and it cannot be a single window. At one seed the origin is a bright dot among a handful of others. The measurement has to **pool many seeds and compare rings** rather than the centre tile alone: ordinary terrain is uncorrelated between worlds and cancels, leaving whatever is a function of position relative to the centre, and a single-tile assertion would pass as soon as the defect's peak moved one hex.

Assert more than the absence of a halo: that no continuous field is exactly zero at the origin, that two worlds do not share a sampling offset, and that the offset does not disturb tile identity there. The same shape of test is what section 11.2 would need if the anchor lattice ever became visible.

### 30.14 The Rim

The rim of section 15.1 is cheap to test and easy to get subtly wrong:

- `RimDistance` agrees with an independently computed hex distance from the
  origin, at all six corners, on all six edges, and for coordinates just inside
  and just outside the closed band.
- The falloff is monotonic and continuous: elevation over a radial walk into the
  rim never rises, and the join at `FalloffHexes` has no step.
- Every tile with `Rim` true has the configured terrain and the configured
  elevation floor, and no tile with `Rim` false has either by accident.
- Heat and moisture inside the rim are still the generated values, not zeros.
- `ClosedHexes = 0, FalloffHexes = 0` reproduces the unrimmed world **bit for
  bit**, which is what lets sections 30.10 and the goldens coexist with the rim.

Run these at the alpha width, where a radial walk to the rim is a few thousand steps.

---

## 31. Performance

**There is no player-facing throughput target in this document, and there should
not be one.** A threshold written into a design becomes a thing to argue with
rather than a thing to learn from, and the questions this document actually
defers to a measurement — the tile cache in section 27.6, the region cache in
section 26, the render budget in section 29.1 — each need a number rather than a
verdict against a number.

**Only the administrator is hyper-focused on performance, and only in two
situations:** viewing a large map, and iterating on a configuration. Both are
renders. A player asks for a viewport and waits for a page; an administrator asks
for a million tiles and waits for a picture, then changes one number and asks
again. The second is where the seconds are noticed, and it is the only place
worth optimizing for.

So the primary measurement is a **render**, not a tile.

### 31.1 Measuring a render

Every front end and `wgva-map --grid` logs what each render actually cost — tiles, generate milliseconds, encode milliseconds, tiles per second — so a window is measured where it is served:

```text
(1002001 tiles, 405 ms generate, 190 ms encode, 2471232 tiles/s)
```

That line is also why the budget in section 29.1 is counted in generator evaluations rather than tiles: `relief`, `climate`, and `terrain` cost seven apiece and every other layer costs one, so a tile count cannot tell a cheap window from one seven times longer. `--budget` raises the bound precisely so that measuring a large window is something these tools can be asked to do.

Two figures worth watching separately, because they respond to different changes:

- **Generate milliseconds** move when the octave ladders move (section 9.3),
  because the ladder length is most of the arithmetic, and when the core count
  moves, because the grid render is parallel across columns.
- **Encode milliseconds** move when the image size or the PNG settings move and
  have nothing to do with the generator. An "it got slower" report that does not
  separate the two is not yet a measurement.

### 31.2 Decomposing a tile, when the render is not enough

Four timing loops live in `generator_bench_test.go` and exist to explain a render figure rather than to stand beside it:

```text
BenchmarkTile              single tiles, one at a time
BenchmarkChunk             a 32x32 chunk fill through TilesInto
BenchmarkRegionRadius32    a radius-32 region
BenchmarkRelief            relief alone, which is seven elevation evaluations
```

Use `go test -bench`. Differences between the rows are what make this a measurement rather than a stopwatch:

- **`BenchmarkTile` against the two batch rows** is the batching question. A pure
  function of its own coordinate has nothing shared to amortize, so these should
  agree; if batching ever starts winning, something has been introduced that is
  shared between tiles, and section 20's no-accumulation rule is the first place
  to look. WGVB measured all three within ten percent of each other.
- **`BenchmarkRelief` against `BenchmarkTile`** splits the cost in two. Relief is
  seven elevation evaluations and nothing else; a whole `Tile` is those same
  seven plus climate, basin influence, volcanic tendency, and terrain. The
  difference is what classification costs and the quotient is what one elevation
  evaluation costs, which is the decomposition a profiler would otherwise have to
  be opened to get.

### 31.3 What a comparison has to hold fixed

A number from this harness is meaningless beside a number from a different build. State all of these with any figure that is going to be compared:

- **The algorithm version**, because a version bump can change how many field
  evaluations a tile costs.
- **The component width.** An `int16` world and an `int32` world run the same
  code, but the normalizer's fast path and the rim test both touch different
  magnitudes, and the tile counts they are quoted against differ by ten orders of
  magnitude.
- **The configuration**, because octave counts are per field (section 9.3) and
  the ladder length is most of the arithmetic. A figure measured under the
  default configuration should say so, with its fingerprint.
- **The Go version and `GOARCH`.** Section 25.1 means arm64 and amd64 may not even
  be computing the same numbers if the rule has been violated somewhere; they are
  certainly not computing them the same way.
- **The machine and `GOMAXPROCS`.** Every row in 31.2 is single-goroutine on
  purpose; the parallel fill is a separate measurement and a separate claim.

Correctness and visual quality come first, and section 35 still says not to optimize before profiling. What this section adds is that the profile should start from one of these numbers, and that the number should be a render.

---

## 32. Initial Implementation Plan

**The ordering changed in this revision, and it is the most important change in
it.** The previous plan built persistence and a player-facing renderer in phase
7, after six phases of tuning done by editing constants and rebuilding. WGVB
followed that plan and found the tool it should have started with was the one it
finished with.

So: **build the terrain tuning tool second, and defer everything that is
deployed, persisted, or player-facing until the world looks right.** Phases 3
through 7 are then each tuned as they land, in a browser, against a live
configuration form, with a distribution readout beside the picture.

### Phase 1 — Coordinate and hashing foundation

Implement `Coord` with unexported fields and a normalizing constructor, the three-stage wraparound normalizer with its exact 128-bit lattice solve, axial-to-world conversion, chunk and region assignment via floor division, rim distance, the domain constants and their pinning test, the domain-separated mixer, and the `Config`/`Generator` skeleton with validation.

> **Exit:** coordinate math, six-edge wrapping, all three normalizer stages,
> configuration validation, and deterministic hashing have complete unit tests. A
> non-canonical `Coord` cannot be constructed outside the package. `mathx.Mul`
> exists and section 25.1 is written into the package documentation.

### Phase 2 — Fields and the terrain tuning tool

Implement one owned 2D noise source, the `Field` tagged struct with fbm composition, domain warping, and the seed-derived sampling offset; the `render` package's `Viewport`, `Layer`, hex render, and grid render; and `cmd/wgva-tune` with the map tab, the grid tab, the configuration form, and the TOML download.

This is a large phase and it is meant to be. Everything after it is tuned through it.

> **Exit:** arbitrary canonical coordinates can be sampled and inspected in a
> browser, at hex scale and at one pixel per hex, with the configuration editable
> live and downloadable as a fingerprinted file. A golden test runs on a second
> `GOARCH` and passes bit-exactly. No database exists and none is needed.

### Phase 3 — Hierarchical region influence

Implement deterministic region parameters, barycentric blending across regional anchors on the triangular lattice, and regional roughness, climate, basin, and elevation biases.

> **Exit:** distant areas have distinct geographic character with no visible
> implementation-region boundaries, checked in the grid view at a scale where the
> anchor lattice would be obvious if it were there.

### Phase 4 — Elevation

Implement continentalness, regional uplift, ridge structure, local relief, sea level, the elevation scalar, and land/water classification.

> **Exit:** diagnostic elevation maps show coherent oceans, coastlines, lowlands,
> and uplands across multiple windows and multiple seeds.

### Phase 5 — Climate

Implement the heat field, elevation cooling, the moisture field, regional climate bias, and two-axis climate classification.

> **Exit:** climate maps form coherent broad zones rather than tile-level
> speckle.

### Phase 6 — Basins and terrain

Implement broad, regional, and local basin influence; coherent inland water if and only if it satisfies section 17; terrain classification from physical fields; and the terrain and climate layers.

> **Exit:** terrain maps visually correspond to elevation and climate, and
> boundaries look geographically plausible. Basin geography exists; inland water
> is either coherent or deliberately omitted with a decision record.

### Phase 7 — The rim, and settling the defaults

Implement the rim profile of section 15.1 — distance, falloff, forced band, the `Rim` flag, and the `rim` layer — and then settle the defaults this algorithm version ships with.

Settling them is a specific act, not a feeling: write the fingerprint of the default configuration into `config/fingerprint_test.go` as a constant. From that moment, moving any default fails a test, and updating the constant is the compatibility decision.

> **Exit:** a radial walk to the rim shows a shelving coast or a rising
> icefield and then forced terrain, at the alpha component width. The whole world
> can be drawn in one grid image and the rim is uniform all the way round.
> `ClosedHexes = 0` reproduces the unrimmed world bit for bit. The default
> configuration has a written-down fingerprint.

### Phase 8 — Persistence, the CLI, the viewer, and player rendering

Implement the single-world SQLite database, the migration ladder, all seven compatibility gates, canonical configuration persistence and fingerprint validation; `cmd/wgva-map`; `cmd/wgva-serve`; and player-facing terrain and overlay composition.

Everything in this phase exists because a *game* needs it. None of it is needed to decide what a world looks like, which is why it is last.

> **Exit:** a database can create, reopen, and reproduce one world safely. Every
> gate is asserted with `errors.Is`. Multiple seeds and distant coordinate
> windows produce varied but coherent maps and player PNGs, and the CLI and both
> front ends agree byte for byte.

### Phase 9 — The component width change

Change `Component` to `int32`, bump `AlgorithmVersion`, and run everything again.

It is its own phase because it is a topology change and deserves to be a separate commit with a separate set of renders. Expect three things to surface, all of them named already: the tile count outgrowing `int64` (section 7.2), the lattice solve's determinant outgrowing `int64` (section 7.1), and any `float32` that crept into a world-space calculation (section 7). Expect the tests that walk to the rim to become impractical, which is why they were written at the alpha width.

> **Exit:** the shipping world generates, renders, and persists; a world file
> from the alpha width is refused by gate 5 rather than misread.

---

## 33. Avoid These Designs

### 33.1 Independent random tile classification

Do not derive terrain from `hash(seed, q, r) % terrainCount`. That recreates the incoherent appearance the whole design exists to fix.

### 33.2 Finite global heightmaps

Do not generate a fixed `width x height` array and normalize it. That makes the world bounded.

### 33.3 Mutable PRNG traversal

Do not make tile values depend on the order tiles were generated. Nothing in `Generator` may be mutable, and `math/rand/v2` may not appear in the generation path.

### 33.4 Region-owned terrain

Do not assign every tile inside a region one set of hard parameters without blending. That creates seams.

### 33.5 Runtime global normalization

Do not compute min/max elevation or histogram thresholds from the currently explored area. Exploration order would change the world.

### 33.6 Go-specific traps

- `a*b + c` in the generation path — write `float64(a*b) + c` or `mathx.Mul`
  (section 25.1).
- `math.FMA` anywhere in the generation path (section 25.1).
- Transcendental functions from `math` in the generation path (section 25.2).
- `iota` for a persisted enumeration (section 14.1).
- Ranging over a map anywhere the order can be observed — including
  `hexg.HexSet` (sections 7.1, 25.3).
- A missing configuration field decoding to a zero value (section 21.1).
- Unclamped `float64`-to-`int` conversion (section 25.5).
- `int64` arithmetic in the lattice solve (section 7.1).
- `math/rand` v1 anywhere at all (section 8).
- `hash/maphash` for anything persisted (section 25.4).
- `unsafe`, anywhere (section 22).
- Writing a coordinate width as a literal outside `Component` and the
  compatibility tests (section 4.2).
- Assuming a `Component` may hold any value it can represent. The extreme
  negative value — `math.MinInt16`, `math.MinInt32` — is not a coordinate, and
  `abs` and negation are wrong for it (section 4.1).

---

## 34. Future Extensions

### 34.1 Rivers

Derive deterministic watershed structure from a coarser hydrology field, trace river paths locally from stable source features, and ensure any path can be reconstructed from coordinates alone. Deferred because globally coherent drainage is substantially harder than scalar field generation — and note that it runs into the same obstruction section 17.1 records for lakes, one dimension down.

### 34.2 Biomes

Terrain classification can evolve into richer biome classification.

### 34.3 Resources

Resources use the same hierarchical deterministic path approach: `seed + resource domain + region + coordinate`.

### 34.4 Named geographic features

Macro regions can provide deterministic identities for mountain systems, deserts, forests, and seas. Names are a separate layer from physical generation.

### 34.5 Alternative world topology

The current design uses the finite hexagonal wraparound topology of section 7.1. A future generator version could introduce a cylinder, torus, sphere-like topology, or a differently sized wrapped world. Coordinate normalization must remain a distinct layer from physical fields and classification so such a change is possible. Topology is an algorithm compatibility decision and cannot change for an existing world.

**The rim opens one such door cheaply.** With a closed band already in place, a world with a genuinely hard edge — no wrapping at all, the normalizer replaced by a clamp or a refusal — would look identical to a player and would simplify the normalizer to three comparisons. That is not proposed here, because wrapping is what makes `NewCoord` total and tile identity single-valued, and losing totality means every coordinate-producing operation grows an error path. But if that trade ever looks worth making, section 15.1 is the half of the work that is already done.

---

## 35. Coding-Agent Guidance

1. Prefer small deterministic functions over stateful generator steps.
2. Keep raw scalar fields separate from classification.
3. Expose diagnostic sampling early.
4. **Build the tuning tool before extensive aesthetic tuning**, not after. This
   is phase 2 and the reason is section 1.1.
5. Test negative coordinates from the beginning.
6. Do not optimize before profiling, and profile a render rather than a tile
   (section 31).
7. Do not add a dependency to the `wgva` package. It depends on the standard
   library and nothing else; state the reason in the commit message if that ever
   has to change.
8. Treat changes that alter existing generated worlds as versioned algorithm
   changes, and say in the commit message whether the bump moved any value.
9. Keep all geographic boundaries emergent; implementation regions and chunks
   must remain invisible.
10. Read section 25 before writing any floating-point code.
11. **The rules in sections 9.3, 9.4, 11.2, 12, and 17.1 came from a rendered
    artifact looking wrong and being chased down.** Each carries its measurement.
    Do not relax one on the grounds that it looks unnecessary — reproduce the
    measurement first.
12. Preserve the central invariant:

```text
Tile = F(seed, q, r, algorithmVersion, componentWidth, configuration)
```

with no dependency on generation order or previously generated tiles.

---

## 36. Definition of Success

The first major WGVA milestone is complete when all of the following hold:

- A caller can request any axial coordinate `(q, r)`.
- The world boundary is never encountered as a terminal gameplay edge: the rim
  is reached before the seam, and the rim is closed.
- The returned tile contains normalized elevation, heat, moisture, and relief
  values plus elevation, climate, and terrain classifications, and says whether
  it is rim.
- The same seed and coordinate always return the same tile, bit-for-bit, on both
  supported architectures.
- Adjacent tiles form visually coherent geographic features.
- Large-scale terrain differs across distant portions of the map.
- Region and chunk boundaries cannot be identified by looking at generated
  terrain.
- Negative coordinates work correctly.
- All six world edges wrap correctly and the seam is covered.
- The world can be rendered in arbitrary windows for inspection, and at the alpha
  width in its entirety.
- Generating a distant tile does not require generating the intervening world.
- A non-canonical `Coord` cannot be constructed outside the package.
- The configuration that produced any picture can be recovered from a file with a
  fingerprint on it.

At that point WGVA will retain the operational simplicity of the original Marajanda coordinate-path generator while producing terrain with the visual coherence of a modern noise-based map.


---

## Appendix A — Direction Vectors

The six canonical directions are numbered `0` through `5`, in the order Red Blob Games gives them. Increasing the index by one steps to the next neighbor **counter-clockwise**; decreasing it steps clockwise. This ordering is independent of whether a renderer draws flat-top or pointy-top hexes.

> **This corrects the previous revision of this document**, which said increasing
> the index moves clockwise. It does not, and the error is the kind that survives
> review because both senses are internally consistent until something is drawn.
> *Rotation senses* below is why, and is the section to read before writing
> either word in a comment.

The six vectors are pinned by the algorithm version. They are world data, not a rendering detail, because the per-player rotation below is defined as arithmetic over these indices:

| Direction | Cube `(q, r, s)` | Axial `(q, r)` |
|---:|---|---|
| 0 | `(+1,  0, -1)` | `(+1,  0)` |
| 1 | `(+1, -1,  0)` | `(+1, -1)` |
| 2 | `( 0, -1, +1)` | `( 0, -1)` |
| 3 | `(-1,  0, +1)` | `(-1,  0)` |
| 4 | `(-1, +1,  0)` | `(-1, +1)` |
| 5 | `( 0, +1, -1)` | `( 0, +1)` |

The cube form is `(q, r, -q - r)`. Changing the table, or renumbering it, changes every world and is an algorithm compatibility change.

**Directions carry no compass names in the `wgva` package.** North is a property of a viewing player, not of the world; see *Coordinate frames* below.

Callers may supply any integer direction. Normalize before indexing:

```go
func normalizeDirection(dir int) int {
    dir %= 6
    if dir < 0 {
        dir += 6
    }
    return dir
}
```

Go's `%` returns a remainder with the sign of the dividend, so `-7 % 6` is `-1`, not `5`, and the adjustment branch is required. This is one of the places a Rust implementation gets a one-expression `rem_euclid` and Go does not; it is five lines, it has a test, and it must be the only place in the module that does this.

Values differing by a multiple of six identify the same direction:

| Input | Normalized | Movement from direction 0 |
|---:|---:|---|
| `7` | `1` | One step in index order |
| `6` | `0` | Full turn |
| `-1` | `5` | One step against index order |
| `-2` | `4` | Two steps against index order |
| `-6` | `0` | Full turn |
| `-7` | `5` | Full turn plus one step against index order |

Direction iteration in any accumulating context must use fixed order `0..6`. See section 25.3.

### Rotation

One step in index order — direction `d` to `d + 1` — is an exact permutation with sign changes on the cube form:

```go
func rotateOnce(x, y, z int64) (int64, int64, int64) {
    return -z, -x, -y
}
```

`rotateOnce(direction[d]) == direction[(d+1)%6]` for all six, and six applications are the identity. The inverse step is `(x, y, z) -> (-y, -z, -x)`. `Coord.Rotate(steps)` applies it `steps` times, for any signed `steps`.

**Both are named for the index, not for a rotation sense**, and that is deliberate. The step is clockwise only under a plot of canonical world space with `+y` upward, which nothing here draws, and an abbreviation like `cw` reads as either "clockwise" or "compass walk" now that both exist and run opposite ways. **Do not introduce the abbreviation in either sense.** Write "index order" or write the compass names out.

This is integer-exact, so it is available everywhere in the generation path without violating section 25.2 — there is no rotation matrix and no angle.

**The same function generates the mirror centers.** The six centers in section 7.1 are the six rotations of `(2N+1, -N, -N-1)` under `rotateOnce`, so write the rotation once and derive the table from it rather than transcribing six triples. Two of the six have components outside `Component` range at either width, which is what the `int64` intermediate rule in section 4 protects.

Because the canonical domain is six-fold symmetric about the origin, the rotation maps it onto itself: it commutes with normalization. It also commutes with `RimDistance`, since `max(|q|,|r|,|s|)` is invariant under a permutation with sign changes — which is why a rotated grid view (section 29.1) shows the rim in the same place.

### Rotation senses

Two different rotations are in play and they run opposite ways. Conflating them has already cost one round of confusion and one wrong sentence in the previous revision of this document, so both are written out here.

**Index order is counter-clockwise.** The table above is Red Blob Games' order: `direction[0]` is `(+1, 0, -1)` and `direction[1]` is `(+1, -1, 0)`, and as any viewer sees the world, that second vector is one sixth of a turn *counter-clockwise* from the first. `Coord.Rotate(1)` and the `d + 1` in `Neighbor(d + 1)` both move that way.

**The compass ring is clockwise.** A viewer's six neighbors, named the way a person names them, are N, NE, SE, S, SW, NW — a clockwise walk that starts at whatever direction is *that viewer's* north. Because index order runs the other way, the compass walk **decreases** the index:

| Compass | Absolute direction | Player-relative direction |
|---|---|---|
| N  | `k`     | `0` |
| NE | `k - 1` | `5` |
| SE | `k - 2` | `4` |
| S  | `k - 3` | `3` |
| SW | `k - 4` | `2` |
| NW | `k - 5` | `1` |

all `mod 6`, for a viewer at rotation `k`. The player-relative column is the same for every viewer, which is the point: **the clockwise compass walk is always `0, 5, 4, 3, 2, 1` in the viewer's own numbering**, whatever their rotation is, and `3` is always behind them.

**The admin frame is rotation 2.** The diagnostic renderer applies no rotation, and its flat-top layout puts absolute direction `2` at the top of the image, so what it draws is a viewer at `k = 2`. The compass walk there is absolute `2, 1, 0, 5, 4, 3`:

| Compass | N | NE | SE | S | SW | NW |
|---|---:|---:|---:|---:|---:|---:|
| Absolute direction | 2 | 1 | 0 | 5 | 4 | 3 |
| Axial step | `(0, -1)` | `(+1, -1)` | `(+1, 0)` | `(0, +1)` | `(-1, +1)` | `(-1, 0)` |

Rules that follow:

- **The generator says neither word.** Nothing in the `wgva` package has a north,
  so nothing in it needs "clockwise": the core deals in direction *indices* and
  the arithmetic `(d + 1) mod 6`. A comment there that says clockwise is
  describing a picture the package cannot see.
- **Presentation and player-facing text say only "clockwise", never an index
  direction.** A heading printed to a player, a compass rose, a scroll control, a
  "turn right" — all of them mean the compass walk above, which is index minus
  one.
- Both senses are exact integer arithmetic on indices. Neither is an angle, and
  section 25.2 is untroubled by either.
- **Never abbreviate either one.** With both senses named, two letters read as
  "clockwise" or as "compass walk", which are opposite directions through the
  index — the worst possible ambiguity in the shortest possible identifier. Say
  "index order" or name the compass points.

Also resist describing the presented frame by its handedness. It is true that plotting a `+y`-up plane into a `+y`-down raster reverses the apparent sense of a turn, and it is true that this is what puts index order and the compass walk on opposite paths — but "the image frame is left-handed" invites the reader to conclude that the compass walk is backwards, when the compass walk is the ordinary right-handed one and reads exactly as a person expects. Describe what is seen: index up is counter-clockwise, the compass runs clockwise, and the two therefore disagree by a sign.

### Coordinate frames

Two distinct frames exist, and confusing them is a real hazard.

**Canonical, or absolute.** The wrapped hexagonal domain of section 7.1. This is the only frame the generator ever sees, the only frame used as a persistence key, and what `Coord` represents. Unless a passage says otherwise, coordinates in this document are canonical.

**Player-relative.** Every player is assigned an origin hex and a rotation when they are created, and sees the world through that frame on a **flat-top** layout. One player's `(0, 0)` is not another's, and their norths may differ: rotation is a direction offset, so a player at rotation `k` perceives absolute direction `k` as north. Two players can describe the same tile with different coordinates and the same heading with different direction numbers.

The presented layout is pinned, because "north" is meaningless without it: **flat-top hexes, image `y` increasing downward, the frame's north at the top of the image.** Flat-top is what makes north a neighbor at all — a flat-top hex has neighbors directly above and below it, a pointy-top one does not — so the six compass names in *Rotation senses* exist only in this layout.

The transform is exact integer arithmetic:

```text
absolute          = normalize( rotate^k ( relative ) + playerOrigin )
absoluteDirection = (playerDirection + k) mod 6
```

Rules:

- A player frame must never reach the `Generator`. Fields are sampled on
  canonical coordinates only, or `Tile = F(seed, coordinate, version, width,
  configuration)` would acquire a per-player term.
- Player origin and rotation are authoritative player state, never generated
  data.
- Compass names, and the conversion in either direction, belong to the rendering
  and presentation layers.

In discussion the shorthand `(q, r)` often means a player-relative coordinate and `(q, r, s)` an absolute one. This document does not rely on that convention: canonical is the default, and player-relative coordinates are always labeled.

---

## Appendix B — Dependency Map

Every version is pinned in `go.mod`. The `wgva` package itself takes none of these.

| WGVB (Rust) | WGVA (Go) | Notes |
|---|---|---|
| `hexx` | `github.com/maloquacious/hexg` | `render` only, and only for layout, offset conversion, and hit testing. Not its wraparound, not its polygon corners, not `HexSet`. Section 7.1. |
| *owned noise* | *owned noise* | Section 9.2, unchanged: the reason is the algorithm version, not the language. |
| `rusqlite` (bundled) | `zombiezen.com/go/sqlite` | Same idiom: direct, non-ORM, no `database/sql` driver. |
| *owned migration ladder* | `zombiezen.com/go/sqlite/sqlitemigration` | Go has a good one; WGVB wrote its own only because Rust had none. Section 27.4. |
| `png` | `image/png` (std) | Golden-compare decoded RGBA. Section 29. |
| `clap` | `flag` (std) | — |
| `tiny_http` | `net/http` (std) | No dependency and no async runtime question. Section 29.1. |
| `rand` | `math/rand/v2` (std) | Permitted for tooling and test data by section 8, never in the generation path. `math/rand` v1 is forbidden outright. |
| `sha2` | `crypto/sha256` (std) | Fingerprint; section 21.2. |
| `ciborium` | `github.com/fxamacker/cbor/v2` | Canonical config bytes, in `config` only; section 21.2. |
| `toml` | `github.com/BurntSushi/toml` | Parsing the configuration file; writing is done directly for exact float formatting. Sections 21.1 and 29.1. |
| `serde` | `encoding/json` (std) | Diagnostic serialization; `DisallowUnknownFields` is not the default. |
| `thiserror` | `errors` (std) | Sentinels plus a wrapping struct; section 19.1. |
| `rayon` | goroutines (std) | Pure per-slot fills only. Sections 20 and 29.1. |
| — | `github.com/maloquacious/semver` | `version.go` only. |

**The `wgva` package depends on the standard library and nothing else.** Keep it
that way; section 28's import-cycle argument is what makes the boundary real,
and a third-party import in the core is the one thing that can quietly erode it.

Three dependency edges point in directions worth naming, and each would be wrong as an ordinary one:

- `cmd/wgva-tune` and `cmd/wgva-serve` appear in `cmd/wgva-map`'s **tests**, so
  that sections 29.1 and 29.3's byte-agreement assertions can run the real
  binaries. No cycle exists — neither front end imports `wgva-map` — and neither
  appears in anything a consumer builds.
- `github.com/fxamacker/cbor/v2` appears in the **tests** of the `wgva` package.
  Proving that section 21.1's unknown-field and missing-field rules actually fire
  needs a real serialization format, and this is the format section 21.2 chose.
- Nothing else in `wgva`'s test tree may take a dependency the package itself
  does not, without the same kind of justification.

---

## Appendix C — Storage Decision Record

**Decision: SQLite via `zombiezen.com/go/sqlite`, with `WITHOUT ROWID`
coordinate-keyed tables. A dedicated B-tree key-value store was evaluated and
rejected for authoritative data.**

### The workload

Reading section 27, the store handles three payloads: singleton world metadata (one record), sparse mutable overlays keyed by canonical `(q, r)`, and reproducible caches keyed by coordinate or chunk. Access is point lookup and range scan by region. There is not a single join in this document. That is a key-value workload wearing a SQL costume, and the question deserved a real answer rather than an inherited default.

### What was evaluated

| Candidate | Verdict |
|---|---|
| **`go.etcd.io/bbolt`** | The right pick *if* going key-value. Pure Go, copy-on-write B+tree, ACID, one writer with unblocked concurrent readers, single file, a format that has not moved in years, and ordered cursors that give coordinate range scans directly. |
| **`badger`** | Rejected. LSM, tuned for write-heavy workloads; wrong shape for read-and-scan, and it manages a directory rather than a file. |
| **`pebble`** | Rejected for the same shape reason, and it is a storage engine for a database rather than an application-facing store. |
| **A hand-rolled file** | Rejected. Durability, crash consistency, and concurrent readers are the entire problem, and they are exactly what is hard. |

### Why SQLite wins here

1. **File format longevity.** SQLite's on-disk format has been stable since 2004
   with a published commitment through 2050. Any key-value library has its own
   internal format version, and a major release can require a format migration
   *on top of* our schema migrations — two formats to version instead of one, for
   data measured in megabytes.
2. **Inspectability.** Any world file opens in the `sqlite3` CLI. Debugging a
   corrupt world from a player's bug report is a `SELECT`, not a custom dump tool
   that has to be written and kept working.
3. **`WITHOUT ROWID` already gives us the B-tree.** The coordinate locality that
   motivates a key-value store is available inside SQLite (section 27.2). The
   remaining advantages — no SQL parse/bind/step per row, zero-copy reads —
   matter for bulk cache writes and essentially nothing else here.
4. **Query patterns will grow.** Overlays are coordinate-keyed today. "All
   settlements owned by player X", "everything changed since turn N", "labels
   within this chunk range" are each a `WHERE` clause in SQLite and a
   hand-maintained, transactionally-consistent secondary index in a key-value
   store.
5. **The authoritative data is small.** Metadata plus settlements, labels, fog,
   and discoveries. SQLite's throughput is irrelevant at that size; its tooling
   and durability record are not.

### What SQLite costs us

Honestly: a pure-Go store would remove the cgo-adjacent build story entirely, and `bbolt`'s single-writer/many-reader transaction model is a slightly better match for section 22's immutable-generator design than per-goroutine connections are. That cost is real but small, because the hot path — generation — touches no database at all (section 27.7).

### One file, not two

**Decision: the world and the game live in one database.** The alternative — a
world file and a game file, opened separately — is rejected.

The argument for splitting was a testing one: map work should not have to stand up a game. That advantage is already available and costs nothing, because the package graph gives it. Map tests import `wgva` and `render` and never `store`; `wgva.NewDefault(seed)` needs no database to exist at all, and a generator test that touched SQLite would be a design error today. Store tests use an in-memory database and touch no file either. **The tuning tool proves the point hardest: it is the tool that decides what worlds look like, and section 28 forbids it from importing `store` at all.** Splitting the file would buy nothing the package boundary has not already bought.

The argument against splitting is asymmetric and worse than it first looks. Restoring an older *world* file is harmless when the seed, algorithm version, component width, and configuration match, because terrain is reproducible — that is the whole point of section 2.1. The danger is a world file whose seed or configuration differs: every player overlay and every player frame is then anchored to terrain that no longer exists at those coordinates, **silently**, because a settlement is a coordinate and a coordinate still resolves. One database makes that unrepresentable. World and game are backed up, restored, and gated together, and the fingerprint gate in section 27.5 already covers both. Splitting would require inventing an eighth gate that stores world identity in the game file and checks it on every open — a new mechanism to protect against a hazard that not splitting does not have.

Revisit only if a genuinely read-only world is shared across several games, which is the one shape where the split pays for its gate.

### If this is revisited

The place where a key-value store genuinely wins is the tile/chunk cache, and section 27.6 already declares that disposable and defers building it. If profiling ever justifies one, the clean answer is a **separate `bbolt` file beside the world**: disposable data in a disposable-format store, authoritative data in SQLite. Do not migrate the authoritative side.

Two other conditions would reopen the decision:

- **A WebAssembly target.** SQLite in the browser is genuinely painful; `bbolt`
  is pure Go. If a browser renderer becomes a goal, re-evaluate.
- **A hard requirement for a pure-Go dependency tree.**

If `bbolt` is ever adopted, the required changes are bounded and known:

- The pragma-based gates become a reserved `meta` bucket: `magic` →
  `0x57475641`, `format_version` replacing `user_version`, plus
  `generator_version`, `component_width`, `seed`, `config`, and
  `config_fingerprint`. The **gate order in section 27.5 does not change.**
- Write the `Coord` key encoding by hand rather than relying on any library's
  tuple encoding, so on-disk key bytes are pinned by our code. Encode each
  component big-endian with the sign bit flipped — `uint16(q) ^ 0x8000` at the
  alpha width, `uint32(q) ^ 0x80000000` at the shipping width — so lexicographic
  byte order matches numeric order and range scans work.
- Pin the version exactly and treat a major upgrade as a format migration.
- Sections 30.11 and 27.3 survive unchanged in substance.
