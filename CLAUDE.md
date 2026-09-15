# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status

**Phases 1 through 8 have landed, and there is no phase 9.** The root `wgva`
package carries coordinates, the wraparound normalizer, hashing, the seed
grammar, the owned noise, the `Field` composition tree with fbm and domain
warping, the region anchors and their barycentric blend, the elevation composite
with its ridge structure and land/water classification, the two-axis climate
composite with its broad zone fields and elevation lapse rate, the basin
composite and the volcanic tendency, the ordered terrain classifier, the rim
profile, `Tile`, the batch API, and `Config`/`Generator`; `internal/mathx`
carries the floor helpers, `Mul`, and the exact 128-bit arithmetic. Around it,
`config` owns the canonical CBOR, the fingerprint, the TOML file, and the
`<build>/<fingerprint>` identity pair; `render` owns the viewport, all seventeen
layers, the three renders, the player frame, the overlays, and the distribution
readout; `view` owns the window grammar; `store` owns the single-world SQLite
file, the migration ladder, and the seven opening gates; and the four commands
are the terrain tuning tool, the world builder, the map renderer, and the map
viewer.

**The defaults are settled.** `config/fingerprint_test.go` carries the
written-down fingerprint of the default configuration, so moving any default
fails a test; updating that constant is a compatibility decision and belongs in
the commit message beside the `AlgorithmVersion` bump. `DESIGN.md` appendices
D.11, D.13, D.15, and D.17 are what each group was tuned to.

**Worlds exist now, so `AlgorithmVersion` is close to costing what `DESIGN.md`
section 27 says it costs.** `AGENTS.md`, *Alpha workflow*, relaxes that during
alpha because there were no worlds anybody wanted to keep. The first world
somebody is unwilling to throw away is when that stops being true; say so in the
commit that leaves alpha and delete the subsection then.

**Inland water is declared and deliberately emitted nowhere** — `DESIGN.md` 17.1
is the decision record, `TerrainInlandSea` and `TerrainLake` keep their numbers,
and the distribution test asserts both stay at zero. Of the region parameters
only the variation is still unread.

**There is no tile cache and building one is not deferred work.** WGVB measured
and the answer was no; `DESIGN.md` 27.6 is the record, and a world file holds a
metadata row, the player frames, and the sparse overlays and nothing else.

The golden tables in `golden_test.go` are recorded with the **rim switched off**,
and `DESIGN.md` appendix D.16 is why: eight of their coordinates are the rim's own
corners, a zero rim is the shipped world bit for bit everywhere the band does not
reach, and `TestGoldenRim` is where the shipped band's arithmetic is recorded
instead. Do not "fix" that by re-recording them under the default rim.

`DESIGN.md` is the specification and `AGENTS.md` is the working guidance. The
code has caught up with both, which changes what a disagreement means: it is now
more likely to be documentation the implementation moved past than a gap left to
fill. Read `DESIGN.md` appendix D first — D.18 and D.19 record what the front
ends and persistence settled, and an appendix entry is behind the code by
construction and outranks the body where it speaks. Where no appendix entry
covers it, still treat the disagreement as a gap to fill rather than drift to
correct, and write the entry when you settle it.

`DESIGN.md` section 32 was the build order and it paid for itself: coordinates
and hashing, then fields **and the terrain tuning tool**, then regions,
elevation, climate, terrain, the rim, and only then persistence and anything
player-facing. Six phases of deciding what worlds look like happened with no
database in the loop. Keep it in mind for whatever comes next — the temptation to
start with storage because it looks foundational is what the order exists
against.

@AGENTS.md

## Commands

```sh
go build ./...
go vet ./...
gofmt -l .                       # lists files needing formatting; -w to fix
go test ./...
go test -race ./...              # anything touching goroutines
go test -run TestNormalize ./...            # one test by name
go test -run 'TestRim/falloff' ./...        # one subtest
go test -bench . -run '^$' ./...            # benchmarks only, no tests
```

The four tools, and what each one can touch. See `AGENTS.md`, *Naming, outside
the repository*, for what to call them when project management is listening.

```sh
# decide how worlds look — cannot open or create a world
go run ./cmd/wgva-tune --seed 0x0123456789abcdef

# what --expect takes, and the world it makes
go run ./cmd/wgva-world identity
go run ./cmd/wgva-world create --seed <hex> --expect <build>/<fingerprint> world.wgva
go run ./cmd/wgva-world inspect world.wgva

# one window to one image file — cannot create or modify a world
go run ./cmd/wgva-map --db world.wgva --q 0 --r 0 --layer terrain --out map.png
go run ./cmd/wgva-map --seed <hex> --grid --stride 8 --out grid.png    # diagnostic

# look at a saved world — opens, never writes
go run ./cmd/wgva-serve --db world.wgva
```

Goldens must pass on both architectures, because the FMA hazard in `DESIGN.md`
section 25.1 shows up on exactly one of them:

```sh
GOARCH=arm64 go test -run TestGolden ./...
GOARCH=amd64 go test -run TestGolden ./...
```

Do not set `GOAMD64` or any microarchitecture level for a build whose output is
compared against goldens.

Work lands on `main` directly — never branch, push after committing — and a code
change bumps `version.go` in the same commit, tagged. The bump table and what it
means for `AlgorithmVersion` are in `AGENTS.md`, *Alpha workflow*.

```sh
git tag -a v0.2.0-alpha -m 'one line on what moved'
git push origin main --follow-tags
```

## Architecture

The whole design turns on one invariant:

```text
Tile = F(seed, q, r, algorithmVersion, worldRadius, configuration)
```

No dependency on generation order, on previously generated tiles, on what has
been explored, or on which machine is running. Everything below exists to make
that hold or to keep it honest.

### The dependency graph is load-bearing

```text
cmd/wgva-tune   ->  view, render, config    ->  wgva   (no store edge, by design)
cmd/wgva-world  ->  store, config           ->  wgva   (no render edge, by design)
cmd/wgva-map    ->  render, store, config   ->  wgva
cmd/wgva-serve  ->  view, render, store     ->  wgva
```

`store`, `render`, `config`, and `view` all import `wgva`, so **`wgva` cannot
import any of them** — Go rejects the cycle. That is what makes "persistence and
rendering are not in the core" a fact of the build rather than a review
convention. The `wgva` package depends on the standard library and nothing else;
`hexg`, SQLite, CBOR, TOML, and PNG all live outside it.

The two edges the compiler cannot forbid are `cmd/wgva-tune` importing `store`
and `cmd/wgva-world` importing `render`, so a `go list -deps` test in each
package enforces them. The tool that decides how worlds look cannot touch a
world; the tool that makes a world cannot draw one. `DESIGN.md` section 29.5 is
the order an administrator uses them in and why the split exists.

### Four layers, in order

Seed → macro continuous fields → deterministic hierarchical regions → local
detail → classification (elevation, climate, terrain). Chunks and regions are
addressing devices and must never be visible in the output; a boundary you can
see in a render is a bug, not a seam to be tuned around.

### Coordinates

`Coord` has unexported fields and only a normalizing constructor, so a
non-canonical coordinate cannot be built outside the package and `==`, map keys,
and sorting are automatically correct for tile identity. The zero value is the
origin, which is canonical.

The world is a wrapped hexagon of radius `WorldRadius`, which is `32767` and
settled — edges a test can walk to, whole world drawable in one grid image, and
about 526 times Earth's land area at a 30% land fraction. There is no migration
to a larger world.

`Component` is `int32`, deliberately wider than the domain: the radius defines
the world and the type is only storage. That is what makes a missing bound check
store a visibly out-of-range value instead of silently truncating it into a
plausible one, and it is why anything identifying a world records `WorldRadius`
rather than a width in bits. The domain is symmetric about the origin, which is
what makes negation and `abs` total. The wrap seam is covered by the rim rather
than smoothed, which is why the fields owe no periodicity.

### Where the numbers that decide how a world looks live

The complete effective configuration is flat, validated, fingerprinted with
SHA-256 over canonical CBOR, and stored in the world file. The `Field`
composition tree is *derived* from it, never a second source of truth. A
fingerprint constant in `config/fingerprint_test.go` is what settles the
defaults an algorithm version ships with: moving any default fails that test, and
updating the constant is the compatibility decision.

## Imports from other agent tools

An OpenAI Codex config exists at `~/.codex/config.toml`. To bring over anything
importable from it — MCP servers, slash commands, subagents, skills,
instructions — reply `/import` to scan and list what is available, then
`/import --yes=<digest>` using the digest the scan prints. If `/import` is not
available on this surface, run `claude import` from a terminal instead.
