# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status

**Phase 1 has landed.** The root `wgva` package carries coordinates, the
wraparound normalizer, hashing, and the `Config`/`Generator` skeleton, and
`internal/mathx` carries the floor helpers, `Mul`, and the exact 128-bit
arithmetic. Nothing else in the layout below exists yet — no fields, no noise, no
`render`, `config`, `view`, `store`, or `cmd`.

`DESIGN.md` is the specification and `AGENTS.md` is the working guidance, and
both are still well ahead of the code. Treat a disagreement between them and the
code as a gap to fill, not as documentation drift to correct — except where
`DESIGN.md` appendix D says otherwise, which records what the implementation
settled and is behind the code by construction.

`DESIGN.md` section 32 is the build order, and it is deliberate: coordinates and
hashing, then fields **and the terrain tuning tool**, then regions, elevation,
climate, terrain, the rim, and only then persistence and anything player-facing.
Do not start with the database or a one-shot renderer because they look
foundational.

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
Tile = F(seed, q, r, algorithmVersion, componentWidth, configuration)
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

The world is a wrapped hexagon of radius `WorldRadius`, paired with `Component`:
`int16` for the alpha (edges a test can walk to, whole world drawable in one
grid image) and `int32` for shipping. The canonical domain excludes the extreme
negative value of the component type, which is what makes negation and `abs`
total. The wrap seam is covered by the rim rather than smoothed, which is why
the fields owe no periodicity.

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
