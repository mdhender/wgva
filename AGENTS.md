# WGVA Project Guidance

WGVA is a deterministic, effectively unbounded procedural hex-world generator
written in Go. A Rust sibling, WGVB, implemented an earlier revision of this
design end to end; what it learned is folded into `DESIGN.md` section 1.1. WGVA
is a new world format, not a port that reads WGVB files.

**Read `DESIGN.md` before changing generator behavior or public APIs.** Read
section 25 before writing any floating-point code.

## What to build first

The order in `DESIGN.md` section 32 is deliberate and was changed because of
what WGVB found:

1. Coordinates and hashing.
2. **Fields and the terrain tuning tool** (`cmd/wgva-tune`). Everything after
   this is tuned through it.
3. Regions, elevation, climate, basins and terrain, the rim.
4. Persistence, the CLI, the viewer, player rendering — last, because a game
   needs them and deciding what a world looks like does not.

Do not build the database, the one-shot renderer, or the viewer early because
they seem foundational. They are not, and building them first is exactly the
mistake this ordering exists to avoid.

## Alpha workflow

While the project is in alpha, work lands directly on `main`. These are standing
authorizations and do not need to be re-confirmed per change.

- **Commit to `main`. Never branch.** No feature branches, no pull requests.
- **Push after committing**, with `git push origin main --follow-tags`.
- Both expire when alpha does.

### Versioning

`version.go` carries the module's semantic version. Major stays `0` and the
pre-release stays `alpha` for the whole of alpha.

| Change | Bump |
|---|---|
| A feature implemented | minor |
| A bug fixed | minor |
| Any other code change — a tweaked setting, a refactor, a test | patch |
| Documentation only | none |

**The bump goes in the same commit as the change it describes**, never in a
commit of its own: a version bump with nothing beside it is a number with no
referent. Tag that commit, annotated, and push the tag with it:

```sh
git tag -a v0.2.0-alpha -m 'one line on what moved'
git push origin main --follow-tags
```

A docs-only commit gets no bump and no tag, which is why the design revision
that rewrote `DESIGN.md` and this file carries neither.

### The generator's own version numbers do not get ceremony yet

`AlgorithmVersion`, `RenderVersion`, and `PageVersion` are world-format and
cache-validity counters rather than semantic versions, and `DESIGN.md` section
27 treats a bump as a serious compatibility event. **That seriousness is about
worlds somebody wants to keep, and during alpha there are none.** Every change
may be breaking, tearing down a database and rebuilding costs nothing, and the
terrain tuning tool is first in the build order precisely so the generation
algorithm can be iterated on with no database in the loop. There is very little
damage to contain.

So classify an algorithm change by what it *is*, and let `version.go` carry it:

- Fixing a bug in the algorithm is a **bug fix** — minor.
- Improving the algorithm is a **feature** — minor.
- Tweaking a setting is **any other code change** — patch.

Two things this does not license:

- **Build the gates anyway.** The seven opening gates of section 27.5, the
  fingerprint check, and the component-width refusal are phase 8 work and must
  behave as specified. What alpha relaxes is whether old worlds stay openable,
  not whether a binary refuses a world it cannot reproduce.
- **This expires with alpha.** The first world somebody is unwilling to throw
  away is the moment `AlgorithmVersion` starts costing what section 27 says it
  costs. Say so in the commit that leaves alpha, and delete this subsection then.

## Core invariants

- Preserve `Tile = F(seed, coordinate, algorithm version, component width,
  configuration)`. Generation order, goroutine scheduling, caches, explored
  area, and persisted mutable state must not affect the generated baseline
  world.
- Keep the core generator stateless. Persistence and rendering are application
  layers around it, and Go's import cycle rule enforces it: `store`, `render`,
  `config`, and `view` all import `wgva`, so `wgva` cannot import any of them.
- Keep raw scalar fields separate from game-facing classification. Never
  generate terrain by independent per-tile random selection.
- Use domain-separated coordinate hashing and deterministic continuous fields.
  No mutable PRNG in the generation path. `math/rand/v2` is permitted for
  tooling and test data only; `math/rand` v1 is forbidden everywhere.
- Treat changes to hashes, field composition, thresholds, defaults, the
  coordinate-to-world conversion, the component width, or classification as
  algorithm compatibility changes. Bump `AlgorithmVersion`, and say in the
  commit message whether the bump moved any generated value or was owed only to
  `Config` gaining a field.
- Support negative coordinates correctly. Go's `/` truncates and `%` takes the
  sign of the dividend; use `mathx.FloorDiv` and `mathx.FloorMod`, assert every
  divisor is positive, and keep the exhaustive around-zero tests.
- Follow the Red Blob Games six-mirror-center scheme for hexagonal wraparound,
  computed arithmetically. Never build a mirror lookup table.

## Coordinates

- **`Component` is `int16` for the alpha and `int32` for the shipping world**,
  and `WorldRadius` is its paired maximum. The alpha width exists so the rim,
  the wrap, and a whole-world grid render are things a test can reach. The
  width appears in exactly two places: that pair, and the compatibility tests.
  A literal `32767` anywhere else is a defect. **64-bit components are
  rejected**, not deferred; see section 4.2.
- **The canonical domain is `-WorldRadius <= q, r, s <= +WorldRadius`, and the
  extreme negative value of `Component` is not a coordinate** —
  `-32767 .. 32767` at the alpha width, `math.MinInt16` excluded. This costs no
  tiles and it is the constraint that makes negation, `abs`, and the
  `int64` round trip total; `-math.MinInt16` and `|math.MinInt16|` are both
  silently wrong in Go, and `RimDistance` reads such a tile as lying outside the
  map. Range-check against `±WorldRadius`, never against the type's own range.
  See `DESIGN.md` section 4.1.
- **Validate coordinates read from the database.** SQLite columns hold anything;
  an out-of-range `(q, r)` is malformed data, not a distant tile. Refuse it
  rather than passing it through `NewCoord`, which would silently relocate a
  player's settlement to a real coordinate somewhere else.
- Compute `s`, mirror centers, differences, and every normalization
  intermediate in `int64`. **The lattice solve is the one place that widens
  past `int64`** — its products and, at the shipping width, its determinant
  overflow. Go wraps silently, so the overflow is a plausible wrong answer
  rather than a panic. Use the exact 128-bit helper in `internal/mathx`.
- **`Coord` has unexported fields and no constructor that skips
  normalization.** This is deliberate and load-bearing: it makes "values that
  normalize to the same coordinate identify the same tile" a property of the
  type rather than a convention, so `==`, map keys, and sorting are correct for
  tile identity, persistence keys, and region lookup. The zero value is the
  origin, which is canonical. Do not add an exported field, an exported
  constructor, or a conversion that bypasses `NewCoord`.
- Coordinate-producing operations such as `Neighbor` normalize before
  returning.

## Determinism rules (DESIGN.md section 25)

These are the rules most likely to be violated by code that looks correct.

- **Go may fuse `a*b + c` into an FMA, and does on arm64.** Write
  `float64(a*b) + c`, or call `mathx.Mul`, at every multiply-add in the
  generation path. Missing one site is a silent cross-platform world
  divergence. Never call `math.FMA` in the generation path.
- **The generation path uses only `+`, `-`, `*`, `/`, `math.Sqrt`,
  `math.Floor`, `math.Abs`, `min`, `max`, and comparisons on `float64`.** No
  `math.Sin`, `math.Cos`, `math.Exp`, `math.Pow`, `math.Log`. Use polynomials;
  store ridge orientation as a unit vector, not an angle. `math.Pow(x, 3)` is
  not `x*x*x`.
- **Clamp before converting float to int.** Go leaves out-of-range conversion
  implementation-specific and it differs between amd64 and arm64.
- **Fixed accumulation order.** Floating-point addition is not associative.
  Iterate directions `0..6` and octaves coarse-to-fine, always. Never range
  over a map anywhere the order can be observed — including `hexg.HexSet`, which
  is map-backed. Sort a `[]Coord` instead.
- **Nothing version-unstable may reach a persisted value.** No `hash/maphash`
  (per-process seed), no `hash/fnv` in the generation path. Fingerprints are
  SHA-256 over canonical CBOR; the mixer is written out in `hash.go`.
- Do not use `GOAMD64` or any microarchitecture level for a build compared
  against goldens.
- **Run goldens on `GOARCH=amd64` and `GOARCH=arm64`.** That is the only thing
  that actually proves the FMA rule is being honored.

## Hex geometry

- The `wgva` package depends on the **standard library and nothing else**. It
  needs only the six direction vectors and the `float64` axial-to-world
  conversion, both pinned by the algorithm version.
- `render` uses `hexg` for the flat-top layout, offset-coordinate conversion,
  and hit testing — **not** for its wraparound (ours is the only canonicalizer),
  not for polygon corners, not `HexSet`. Convert `Component` to `hexg.Hex`
  through one adapter function.
- Nothing that touches generation may compute a world position in `float32`. At
  the shipping width, `float32` spacing at the map's far corner is about 300
  hexes.

## Noise and fields

- **Implement the noise in this module.** Do not add a noise dependency. A
  dependency's minor release can change output and silently invalidate every
  world with no version bump on our side, and anything with runtime CPU feature
  dispatch produces different results on different machines.
- Field composition is a tagged struct with a `switch` on `Kind`, not an
  interface with a type switch: one shape on the wire and one in memory.
- **The octave ladder must not reach below the tile grid's Nyquist
  wavelength.** Octave counts are per field and stated in configuration;
  Nyquist is a validation bound, never the mechanism that picks the counts.
- **The sampling position is displaced by a seed-derived, per-domain offset,
  applied above the fbm and scaled by the wavelength.** Without it the world
  origin is a lattice point of every scale at once and is visibly steeper than
  the rest of the world.

## Configuration

- An older binary must **reject** a newer world file, not ignore fields it does
  not understand. `DisallowUnknownFields`, `ExtraDecErrorUnknownField`, and
  `MetaData.Undecoded()` are all opt-in; turn them on.
- **A missing field must be a refusal, never a zero value.** This is the Go
  hazard and it is worse than the Rust one: a configuration missing `SeaLevel`
  decodes to `0.0` and generates a different world under an unchanged version
  number, with no syntax to grep for. Compare the decoded key set against the
  reflected field set and name the missing field. No `omitempty` on anything
  that affects generation.
- Fingerprint is SHA-256 over algorithm version, component width, and canonical
  CBOR. Hash `float64` as bits, normalize `-0.0`, reject `NaN` during
  validation.
- Scale fields name their unit in the identifier: `WavelengthMiles`, `Hexes`.
  Validation rejects non-finite, non-positive, out-of-range, non-ascending
  band ladders, and any fbm ladder that reaches below Nyquist.
- The defaults are settled by a written-down fingerprint constant in
  `config/fingerprint_test.go`. Moving a default fails that test; updating the
  constant is the compatibility decision and belongs in the commit message.

## The rim

- The outermost band of the map is forced terrain and closed to play
  (`DESIGN.md` 15.1). `RimDistance` is `WorldRadius - max(|q|,|r|,|s|)` — `O(1)`,
  local, no neighbor sampling.
- **The generator marks; the game enforces.** `Tile.Rim` is a statement about
  the world. Nothing in `wgva` knows what a move is.
- Deep ocean is the default and polar ice is the option. An ice rim reads as a
  polar cap, and a polar cap is a promise about latitude that the climate model
  does not make.
- `ClosedHexes = 0, FalloffHexes = 0` must reproduce the unrimmed world bit for
  bit. Keep it working; the wrap tests need it.
- **The rim is why field periodicity is no longer owed.** Do not reintroduce
  the requirement, and do not weaken the wrap-identity tests, which are about
  identity rather than smoothness.

## Concurrency

- `Generator` is immutable and safe for concurrent reads. Go cannot check this
  at compile time, which is a real loss: keep no mutex, channel, map, or
  captured-state function value in the struct, run `go test -race`, and keep any
  cache outside `Generator`.
- Goroutines may parallelize batch fills because each tile is a pure function of
  its own coordinate written to its own slot. **Never add a batch operation
  that accumulates across tiles** — a sum, min/max, or histogram — because the
  result would depend on the split.
- Do not call `Tile()` from inside `Tile()`. Both `Tile` and `Relief` call an
  unexported `elevationScalar`.
- Do not import `unsafe` anywhere in this module.

## Persistence

- `zombiezen.com/go/sqlite`; no `database/sql` driver, no ORM, no query builder.
- Migrations through `zombiezen.com/go/sqlite/sqlitemigration`, in order.
  **Never edit a released migration.** `sqlitemigration` does not reject a
  schema newer than the binary — that gate is ours, before it runs.
- Application id is `0x57475641` (ASCII `"WGVA"`), set through
  `sqlitemigration.Schema.AppID`. A WGVB file must be rejected at the first
  gate.
- Every coordinate-keyed table is `WITHOUT ROWID` with `PRIMARY KEY (q, r)`, so
  viewport and chunk loads are one ordered range scan.
- **No foreign keys to generated data.** Authoritative player state must not
  have a referential dependency on a discardable cache. Still enable
  `PRAGMA foreign_keys = ON` on every connection, and use the same connection
  preparation in tests as in production.
- One database, one world, one game. No `world_id` column.
- Seven opening gates, in order, each an `errors.Is`-able sentinel. **Tests
  assert with `errors.Is`, never on message strings.** No gate performs an
  application write before it passes. Gate 5 rejects a world generated at a
  different component width.
- Generated tiles, chunks, and PNGs are reproducible caches, not authoritative
  records. If cached, validate against the configuration fingerprint and the
  render version. **Do not build the tile cache in the first implementation;
  WGVB measured and the answer was no.**
- Return every non-nil connection to the pool it came from, including on error
  paths.

## Rendering

- Rendering is bounded even though generation is effectively unbounded. Every
  render request defines a finite viewport and explicit pixel scale. Renderer
  pixel coordinates never feed back into generation.
- **Only `cmd/wgva-world` creates a world file.** Everything else opens one and
  refuses an absent or empty file rather than initializing it. `wgva-map --db`
  reads a world's *identity* — seed, version, width, configuration — and
  regenerates; it does not read stored tiles, because there are none.
- The tuning tool labels its configuration **defaults** or **modified** on every
  tab, against the fingerprint constant of `DESIGN.md` 21.2. That label is what
  keeps an administrator sampling seeds from creating a world that is not the one
  they chose; see `DESIGN.md` 29.5.
- Render coordinates in a stable sorted order; overlapping edges and labels make
  output order-dependent otherwise.
- Keep generated terrain separate from player overlays (discoveries, fog of
  war, settlements, labels, annotations) and compose at render time. Fog hides
  terrain; fog does not hide the player's own marks. An empty discovery set
  means fog is off, not that nothing has been seen.
- **Golden-compare decoded RGBA buffers, not PNG file bytes.** `image/png`'s
  filter and compression choices can change between Go releases.
- Front ends are front ends, not second renderers. Whatever they draw goes into
  `render` or into the URL, and a test asserts the CLI and each front end
  produce identical bytes for the same window.
- Bind to `127.0.0.1` by default and bound concurrent renders to
  `GOMAXPROCS`; `net/http` starts a goroutine per request and the work is
  CPU-bound.

## Public generated data

- A normal `Tile` carries normalized elevation, heat, moisture, and relief
  values *in addition to* the elevation, climate, and terrain classifications,
  and a `Rim` flag. These are not diagnostics-only data.
- Climate is independent `HeatBand` and `MoistureBand`. Do not create one type
  mixing temperature and moisture.
- **Write every enumerated value explicitly. Do not use `iota`.** These values
  are persisted; inserting a band into the middle of an `iota` block silently
  renumbers everything after it with nothing in the diff that looks like a data
  change. Give each type `String()` and `Valid()`, give each classification
  `switch` an explicit documented `default`, and let the distribution test
  assert every declared value is reachable — that is what stands in for
  exhaustive matching.
- Generate deterministic basin influence even when coherent inland water is not
  feasible. Basin influence enters terrain as a **product** with moisture and
  **never enters elevation**. Emit lakes or inland seas only when bounded local
  generation gives consistent membership, surface elevation, depth, and
  shorelines; otherwise omit them, as WGVB did.

## Performance

- **There is no player-facing throughput target and there should not be one.**
  Only the administrator is hyper-focused on performance, and only when viewing
  large maps or iterating on a configuration. Both are renders.
- Measure a render, not a tile: tiles, generate milliseconds, encode
  milliseconds, tiles per second. Separate generate from encode before calling
  anything slow.
- The per-tile benchmarks exist to *explain* a render figure, not to stand
  beside one. Quote algorithm version, component width, configuration
  fingerprint, Go version, `GOARCH`, machine, and `GOMAXPROCS` with any number
  that is going to be compared.

## Naming, outside the repository

Command names are for the repository. In a status report, a roadmap line, or any
conversation with project management, name a tool by **what it does**, not by
what it is built as.

| command | say | in one line |
|---|---|---|
| `cmd/wgva-tune` | the terrain tuning tool | decides how worlds look; cannot open or change one |
| `cmd/wgva-world` | the world builder | creates a world file from a seed; cannot draw one |
| `cmd/wgva-serve` | the map viewer | looks at a world that is already saved |
| `cmd/wgva-map` | the map renderer | writes one window to one image file |

- **Never call `wgva-tune` a server.** It is one, and the word is the problem:
  it invites hosting cost, uptime, scaling, and a security review for something
  that binds to `127.0.0.1`, has no authentication, and has exactly one user on
  one machine for as long as a browser tab is open. Same for `wgva-serve`.
- **Never call it "the tuner" on its own.** That reads as a runtime knob some
  operator or player adjusts, which is the opposite of what it is.
- Pair the name with its boundary the first time it comes up: *it runs on a
  developer's machine and produces a settings file; nothing is deployed and no
  player ever touches it.*
- **Volunteer that it cannot touch a saved world.** That question gets asked
  eventually, and the answer is better than "we are careful": `cmd/wgva-tune`
  has no import path to `store`, and `deps_test.go` fails if one appears. Its
  mirror image is `cmd/wgva-world`, which has no import path to `render`: the
  tool that decides how worlds look cannot touch a world, and the tool that
  makes a world cannot draw one.
- **Report the output, not the tool.** The deliverable is that the world's
  appearance is a versioned file with a fingerprint on it, so any picture can be
  traced to the configuration that produced it and reproduced elsewhere. The web
  interface is only how somebody drives it.
- If a roadmap line needs one word of hedge, it is **internal** tooling —
  signals "not shipped" without implying "not important".

## Verification

- Run `gofmt`, `go vet ./...`, and `go test ./...` before reporting work
  complete. Run `go test -race ./...` when touching anything concurrent.
- Derive expected test values independently rather than recording whatever the
  code currently produces.
- Test determinism, order independence, concurrent generation, negative
  coordinates, all three normalizer stages, all six wrapped edges, the rim
  profile, region and chunk continuity, and every database compatibility gate.
- Add golden coordinates only for an intentionally stable algorithm version.
  Updating a golden world requires an explicit compatibility decision recorded
  in the commit message. Run goldens on both architectures.
- The rules in `DESIGN.md` sections 9.3, 9.4, 11.2, 12, and 17.1 came from a
  rendered artifact looking wrong and being chased down. Do not relax one on the
  grounds that it looks unnecessary — reproduce the measurement first.
