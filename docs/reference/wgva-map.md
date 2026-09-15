# `wgva-map` — the map renderer

Writes one window to one image file.

## Synopsis

```sh
wgva-map [--db <path> | --config <file> [--seed <seed>]] [--grid] [--out <file>] [window flags]
```

## Description

`wgva-map` renders one window of one world to one PNG. It sits beside the
administrator's path rather than on it: what it is for is the times something
outside a browser needs an image — an acceptance sheet under `docs/renders/`, a
bug report, a golden.

**It never creates or modifies a world.** An absent or empty `--db` file is a
refusal naming `wgva-world create`, not an invitation.

It does not read pixels out of a world either. No tiles are stored, so `--db`
supplies the world's *identity* — seed, algorithm version, world radius,
configuration, fingerprint — and the image is regenerated from it every time. The
only thing genuinely read from the file and drawn is the player overlays.

The window flags are the [window grammar](window-grammar.md): each is the query
parameter of the same name in both web front ends, parsed and clamped by the same
code, so a window moves between the tools without conversion.

See `DESIGN.md` 29.2.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--db <path>` | none | world file to read the world's identity from |
| `--config <file>` | none | configuration file to draw (diagnostic; no world) |
| `--seed <seed>` | `0x0000000000000000` | world seed; ignored with `--db` |
| `--out <file>` | `map.png` | image file to write |
| `--grid` | off | draw the one-pixel-per-cell view instead of hexes |

Window flags, all taking the value the query parameter of the same name takes:
`--q`, `--r`, `--cols`, `--rows`, `--turn`, `--layer`, `--hex-radius`, `--scale`,
`--stride`. A flag that is not given keeps its default; see
[Window defaults](#window-defaults).

Every setting is a flag. A positional argument is a refusal.

## Sources

Three, and the source decides what is drawn and what the provenance lines say.

| Flags | Seed and configuration from | Overlays | Provenance |
|---|---|---|---|
| `--db <path>` | the world file | composed, except on `--grid` | names the world and the build that created it |
| `--config <file>` | `--seed` and the file | none | marked diagnostic |
| neither | `--seed` and this binary's defaults | none | marked diagnostic |

`--db` and `--config` are two sources for one configuration and giving both is a
refusal. With `--db`, `--seed` is ignored rather than compared: one file holds one
world and the file was named.

A world draws through `render.RenderPlayer` with its overlays composed and
everything else through `render.Render`. With no overlays the two produce
identical pixels.

`--grid` composes no overlays even with `--db`: a grid cell stands for `stride`
hexes, and at a stride of sixty-four a settlement marker would be a claim about
sixty-four hexes.

## Window defaults

`wgva-map` opens on the viewer's defaults, not the tuning tool's.

| Parameter | Default |
|---|---|
| `q`, `r` | `0`, `0` |
| `cols`, `rows` | `61`, `45` |
| `turn` | `0` |
| `layer` | `elevation` |
| `hex-radius` | `10` |
| `scale` | `2` |
| `stride` | `1` |

## Output

One PNG at `--out`, and a cost line and provenance lines on standard output.

```text
map.png: 2745 tiles, 19215 evaluations, 13 ms generate, 12 ms encode, 202779 tiles/s
seed 0x0123456789abcdef, algorithm version 6, render version 1, world radius 32767
configuration f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b
world world.wgva, created by 0.25.1-alpha+e87614f
```

The cost line separates generate from encode, which move for different reasons:
generate when the octave ladders or the core count move, encode when the image
size or the PNG settings do. `evaluations` is `tiles` times the layer's cost, so a
`terrain` or `relief` window is seven evaluations per tile. Nothing is refused for
being expensive.

Without a world the last provenance line reads instead:

```text
diagnostic: this does not represent a saved world; `wgva-world create` makes one
```

The image is encoded to a temporary file in the target's directory and renamed, so
an interrupted render leaves the previous image rather than half of a new one. The
finished file is mode `0644`.

## Refusals

| Condition | Message |
|---|---|
| `--db` names an absent or empty file | ``<path>: no world file there`` followed by ``only `wgva-world create` makes a world file; this command opens one`` |
| `--db` fails an opening gate | the gate's message; see [`wgva-world`](wgva-world.md#opening-gates) |
| `--db` and `--config` both given | `--db and --config are two sources for one configuration; give one` |
| a positional argument | `unexpected argument "<text>"; every setting is a flag` |
| `--seed` unparseable | `--seed: "<text>": seed must be sixteen hexadecimal digits, or a decimal number` |
| `--config` unreadable | `--config: <reason>` |
| `--config` contents rejected | `--config: <path>: <reason>` |
| a window parameter out of range or misspelled | the window grammar's refusal, naming the parameter |

Every message is prefixed `wgva-map: ` on standard error.

## Exit status

| Status | Condition |
|---|---|
| `0` | the image was written |
| `1` | any refusal, including `-h` |

## Examples

A player-facing window of a saved world, overlays composed:

```sh
wgva-map --db world.wgva --q -200 --r -150 --cols 401 --rows 301 \
    --hex-radius 8 --layer terrain --out map.png
```

A whole-world overview from a seed alone, no world involved:

```sh
wgva-map --seed 0x0123456789abcdef --grid --cols 601 --rows 451 \
    --stride 8 --scale 1 --layer terrain --q 0 --r 0 --out world.png
```

A configuration under consideration, drawn without creating anything:

```sh
wgva-map --config config.toml --seed 0x0123456789abcdef --layer elevation --out try.png
```

## See also

- [Window grammar](window-grammar.md) — the flags, their bounds, and the layers
- [`wgva-world`](wgva-world.md) — the only thing that creates a world file
- [`wgva-serve`](wgva-serve.md) — the same windows in a browser
- `DESIGN.md` 29.2, 29.4 (overlays), 31.1 (measuring a render)
