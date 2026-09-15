# `wgva-world` — the world builder

Creates a world file from a seed. It cannot draw one.

## Synopsis

```sh
wgva-world create --seed <seed> --expect <build>/<fingerprint> [--config <file>] <path>
wgva-world identity
wgva-world inspect <path>
wgva-world help
```

## Description

`wgva-world` is the only thing in the system that creates a world file. Every
other tool opens one and refuses an absent or empty file.

The package has no import path to `render`, and `cmd/wgva-world/deps_test.go`
fails if one appears: the tool that makes a world cannot draw one. Its mirror
image is [`wgva-tune`](wgva-tune.md), which has no import path to `store`.

A world file is a SQLite database holding one world: the seed, the algorithm
version, the world radius, the complete effective configuration, its fingerprint,
and the build that wrote it, plus the player frames and the sparse overlays.
There is no tile cache; tiles are regenerated from the world's identity.

See `DESIGN.md` 29.5.

## Subcommands

### `create`

Writes a new world file at `<path>` and prints what was written.

| Flag | Default | Meaning |
|---|---|---|
| `--seed <seed>` | — | the world's seed, in any spelling `ParseSeed` accepts. Required. |
| `--expect <build>/<fingerprint>` | — | the identity pair the world was chosen under. Required. |
| `--config <file>` | this binary's defaults | a TOML configuration file to create from |

`--expect` is required and there is no `--force`. It carries two values because
no single value catches every way the same seed yields two worlds:

| What went wrong between sampling and creating | build | fingerprint |
|---|---|---|
| a different build whose defaults moved | catches | catches |
| a setting nudged in the tuning tool's form | misses | catches |
| a generator change with no `AlgorithmVersion` bump | catches | misses |

Nobody types the pair. [`wgva-tune`](wgva-tune.md) emits the whole `create` line
for the window on screen, and `wgva-world identity` answers it without a browser.

`--config` is a developer affordance for exercising the gates. The administrator
never supplies a configuration; the only way one reaches a running binary is a
developer making it the built-in defaults and committing it. A supplied file's
fingerprint is recomputed rather than read from its comment header.

Output:

```text
created world.wgva
  seed              0x0123456789abcdef
  algorithm version 6
  world radius      32767
  build             0.25.1-alpha+e87614f
  fingerprint       f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b
```

### `identity`

Prints this binary's build identity and the fingerprint of its built-in defaults,
in exactly the form `--expect` takes, on one line:

```text
0.25.1-alpha+e87614f/f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b
```

Takes no flags and no arguments.

### `inspect`

Runs the seven opening gates against an existing file and prints its stored
metadata and overlay counts. It opens; it does not write.

```text
world.wgva
  seed              0x0123456789abcdef
  algorithm version 6
  world radius      32767
  fingerprint       f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b
  created by        0.25.1-alpha+e87614f
  created at        2026-09-15 03:53:34 UTC
  configuration     this binary's defaults
  players           0
  discovered        0
  settlements       0
  labels            0
```

The `configuration` line reads `this binary's defaults` when the stored
fingerprint equals this binary's default fingerprint, and `not this binary's
defaults` otherwise.

Takes exactly one argument, the path to read.

## The build provenance note

A build identity proves the code is the code only when it carries a commit hash
and no dirty marker. Two different uncommitted trees both report the same
`+<hash>-dirty`, a tree with no commit yet reports `+dirty`, and a binary built
with `-buildvcs=false` reports no build metadata at all.

When the build identity is not provable, `create` and `identity` print to standard
error:

```text
note: this binary is <build>, which does not identify the code it was built from;
      the build half of the comparison is run but proves nothing here
```

The comparison is still run. This affects developers only; an administrator never
has such a binary.

## Opening gates

`inspect`, and every other tool that opens a world, runs these in order. Each is
one `errors.Is`-able sentinel in package `store`. No gate performs an application
write before it passes.

| # | Gate | Sentinel |
|---|---|---|
| — | the file is absent or empty | `store.ErrNoWorld` |
| 1 | `PRAGMA application_id` is `0x57475641` (`"WGVA"`) | `store.ErrWrongApplicationID` |
| 2 | `PRAGMA user_version` is not newer than this binary | `store.ErrSchemaTooNew` |
| 3 | supported ordered schema migrations are applied | — |
| 4 | the singleton world metadata reads and the configuration validates | `store.ErrMalformedMetadata`, `store.ErrInvalidConfig` |
| 5 | the world radius is this binary's | `store.ErrWrongWorldRadius` |
| 6 | the algorithm version is reproducible and the configuration hashes to the stored fingerprint | `store.ErrUnsupportedGenVersion`, `store.ErrFingerprintMismatch` |
| 7 | normal reads and writes are permitted | — |

A stored coordinate outside the canonical domain is `store.ErrBadCoordinate` and a
stored player rotation outside `0..5` is `store.ErrBadRotation`. Neither is
repaired; both are refused.

A WGVB file is rejected at gate 1: its application id differs by one byte.

## Refusals

| Condition | Message |
|---|---|
| `create` on a path that exists | `<path>: a file already exists there` (`store.ErrWorldExists`) |
| `--seed` absent | `--seed is required` |
| `--seed` unparseable | `--seed: "<text>": seed must be sixteen hexadecimal digits, or a decimal number` |
| `--expect` absent | ``--expect is required; this binary's is <pair> (run `wgva-world identity`)`` |
| `--expect` not `build/fingerprint` | `--expect: "<text>": expected <build>/<fingerprint>` |
| build half differs | `this is not the build the world was chosen on: expected <want>, this binary is <have>` |
| fingerprint half differs | `this is not the configuration the world was chosen under: expected <want>, this configuration is <have>` |
| `--config` file unreadable | `--config: <reason>` |
| `--config` file contents rejected | `--config: <path>: <reason>` |
| `create` or `inspect` with the wrong argument count | `create takes exactly one argument, the path to write` / `inspect takes exactly one argument, the path to read` |

There is no `--force` on `create`. Removing a world is something a person does
deliberately, with `rm`.

Every message is prefixed `wgva-world: ` on standard error.

## Exit status

| Status | Condition |
|---|---|
| `0` | the subcommand succeeded, or `help` was asked for |
| `1` | the subcommand failed, or the subcommand name is unknown |
| `2` | no subcommand was given, or a flag was malformed |

## Examples

Create the world the tuning tool emitted a line for:

```sh
wgva-world create --seed 0x0123456789abcdef \
    --expect 0.25.1-alpha+e87614f/f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b \
    world.wgva
```

Answer `--expect` from a script, with no browser:

```sh
wgva-world create --seed 0x0123456789abcdef --expect "$(wgva-world identity)" world.wgva
```

Read a world somebody handed over:

```sh
wgva-world inspect world.wgva
```

## See also

- [`wgva-tune`](wgva-tune.md) — decides how worlds look; emits the `create` line
- [`wgva-map`](wgva-map.md) — writes one window of a world to one image file
- [`wgva-serve`](wgva-serve.md) — looks at a world that is already saved
- `DESIGN.md` 27 (persistence and versioning), 27.5 (the gates), 29.5 (the pair)
