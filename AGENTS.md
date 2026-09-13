# WGVA Project Guidance

WGVA is a deterministic, effectively unbounded procedural hex-world generator written in Go. Read `DESIGN.md` before changing generator behavior or public APIs.

## Core invariants

- Preserve `Tile = F(seed, coordinate, algorithm version, configuration)`. Generation order, goroutine scheduling, caches, explored area, and persisted mutable state must not affect the generated baseline world.
- Keep the core generator stateless. Persistence and rendering are application layers around it.
- Keep raw scalar fields separate from game-facing classification. Do not generate terrain with independent per-tile random selection.
- Use domain-separated coordinate hashing and deterministic continuous fields. Do not use mutable PRNG streams for world generation. If random APIs are needed elsewhere, use `math/rand/v2`, never `math/rand`.
- Treat changes to hashes, field composition, thresholds, defaults, coordinate-to-world conversion, or classification as algorithm compatibility changes. Update the algorithm version or deliberately preserve the old implementation.
- Support negative coordinates correctly. Use mathematical floor division/modulo where required, not Go's truncating division or signed remainder by assumption.
- For alpha, define a centralized `Component` type backed by `int16` and derive the fixed world radius from that choice. Use `int64` for `s = -q-r`, differences, mirror centers, normalization, and other intermediate arithmetic. Normalize coordinates into the wrapped hexagonal domain before generation, persistence, or comparison.
- Keep coordinate algorithms and SQLite columns independent of the component width so moving to `int32` is localized. A width change alters topology and requires a new generator version even when no schema migration is needed.
- Follow the Red Blob Games six-mirror-center scheme for hexagonal wraparound. Make procedural fields periodic across wrapped edges on a best-effort basis; document and test any remaining discontinuity as a world-warp seam rather than delaying the first implementation indefinitely.
- A configured generator must be immutable and safe for concurrent reads. Add caches only after profiling, and never make correctness depend on cache contents or history.

## Hex geometry

- Use `github.com/maloquacious/hexg` for hex geometry, directions, finite-area traversal, layout, hit testing, and polygon corners instead of creating a competing hex library.
- WGVA's persisted alpha-world coordinates are canonical signed 16-bit axial `(q, r)` values. Convert `Component` to `int` and then `hexg.Hex` through one adapter; this conversion and the derived alpha cube component are lossless on every Go target.
- Keep the canonical world-space conversion used by procedural fields independent from renderer orientation and pixel layout. Flat-top versus pointy-top is a presentation choice.
- `hexg.HexSet` is map-backed. Sort it before any operation whose output must be reproducible, including rendering with overlapping edges or labels.

## SQLite persistence

- Use `zombiezen.com/go/sqlite`; do not introduce a `database/sql` SQLite driver.
- Manage schemas with `zombiezen.com/go/sqlite/sqlitemigration`. Add migrations to `sqlitemigration.Schema.Migrations` in order; never edit an already-released migration. Use repeatable migrations only for objects such as views and triggers that are intentionally recreated.
- Every WGVA database contains exactly one world and must set `sqlitemigration.Schema.AppID` to the ASCII application ID `WGVA`:

  ```go
  const databaseApplicationID int32 = 0x57475641 // "WGVA"; decimal 1464292929
  ```

  Do not change or reuse this value for an unrelated database format. Reject a non-empty database with any other application ID before migration or application writes.
- Reserve `PRAGMA user_version` for `sqlitemigration`. Explicitly reject a schema newer than the binary before invoking migrations; `sqlitemigration` does not perform this newer-schema rejection.
- Store the generator version in singleton world metadata; SQLite does not support an application-defined `PRAGMA generator_version`. Reject a generator version the binary cannot reproduce before normal writes.
- Persist the world seed, generator version, complete effective generator configuration, and stable configuration fingerprint in singleton metadata. Persist mutable game/player state as sparse overlays keyed by canonical axial coordinate; do not add a `world_id` to a single-world database.
- Generated tiles, generated chunks, and PNG files are reproducible caches, not authoritative state. If cached, validate them against the configuration fingerprint and relevant rendering or palette version.
- Return every non-nil connection obtained from a `sqlitemigration.Pool` to that same pool. A `*sqlite.Conn` must not be used concurrently.
- Use foreign keys for relational integrity and enable them on every connection. Migration and database tests must use the same connection preparation as production.

## Rendering

- Rendering is bounded even though generation is effectively unbounded. A render request must define a finite viewport and explicit pixel scale.
- Use `hexg` layouts and polygon corners for image geometry and Go's `image/png` encoder for PNG output. Do not couple renderer pixel coordinates back into terrain generation.
- Render coordinates in a stable order. Keep palettes and symbol rules versioned when rendered output is cached or compared as a golden artifact.
- Keep generated terrain separate from player overlays such as discoveries, fog of war, settlements, labels, and annotations; compose those layers while rendering.

## Public generated data

- A normal `Tile` retains normalized elevation, heat, moisture, and relief values in addition to elevation, climate, and terrain classifications. These physical values are not diagnostics-only data.
- Represent climate with independent `HeatBand` and `MoistureBand` classifications. Do not create one enum that mixes temperature and moisture categories.
- Generate deterministic basin influence even if coherent inland water is not feasible. Emit lakes or inland seas only when bounded local generation can provide consistent membership, surface elevation, depth, and shorelines; otherwise omit inland-water terrain from the first version.

## Implementation and verification

- Prefer small deterministic functions, immutable values, and existing standard-library or selected dependency APIs over new wrappers and frameworks.
- Expose diagnostic scalar layers early and use the diagnostic renderer while tuning fields. Visual quality cannot be established by unit tests alone.
- Derive expected test values independently. Test determinism, order independence, concurrent generation, negative coordinates, all six wrapped edges, region/chunk continuity, and every database compatibility gate.
- Add golden coordinates only for intentionally stable algorithm versions. Updating a golden world requires an explicit compatibility decision.
- Run `gofmt` on changed Go files and `go test ./...`. Run focused benchmarks when changing generation hot paths or caches.
