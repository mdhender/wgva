# `wgva-serve` — the map viewer

Looks at a world that is already saved. It opens and never writes.

It runs on one machine for one person for as long as a browser tab is open, binds
to loopback, and has no authentication. Nothing is deployed and no player touches
it.

## Synopsis

```sh
wgva-serve --db <path> [--host <address>] [--port <port>]
wgva-serve [--seed <seed>] [--host <address>] [--port <port>]
```

## Description

`wgva-serve` serves one world, or one seed diagnostically, as plain HTML pages and
PNGs.

**It is stateless.** Every state it can be in is a URL, so a link means the same
thing to everybody who opens it and the same thing tomorrow. It is deliberately not
a single-page application: no client-side panning, no canvas, no script needed to
move the view, and page refresh is fine. That is the difference from
[`wgva-tune`](wgva-tune.md), which is stateful.

**It opens and never creates.** `wgva-world create` is the only thing that does.

The world is opened before the port is bound, so a file that fails an opening gate
is a server that does not start rather than a server that answers every request
with a `500`.

See `DESIGN.md` 29.3.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--db <path>` | none | world file to serve |
| `--seed <seed>` | `0x0000000000000000` | seed to serve diagnostically when there is no `--db` |
| `--host <address>` | `127.0.0.1` | address to bind |
| `--port <port>` | `8181` | port to bind |

The loopback default is the decision, not the flag. There is no authentication
here and the cost of an endpoint is chosen by the caller.

## Modes

| Flags | Seed and configuration from | Overlays | Image `ETag` |
|---|---|---|---|
| `--db <path>` | the world file | read fresh from the database on every request | none |
| `--seed <seed>` | the flag and this binary's defaults | none | a strong validator |

A world-backed image carries no entity tag at all, because it depends on the
overlays as well, and overlays are mutable player state with no version anywhere
in the system. A tag that ignored them would go on serving an unexplored map after
the player explored it.

## Routes

| Route | Response |
|---|---|
| `GET /` | `302` to `/seed/<this server's seed>` |
| `GET /seed/{seed}` | the viewer page, `text/html` |
| `GET /seed/{seed}/map.png` | the window, `image/png` |

The seed in the route is a check against the one this server holds, not the source
of it. Another seed is a `404` reading `this server holds seed <have>, not <want>`.

Both routes take the [window grammar](window-grammar.md) as query parameters. Any
parameter that is absent keeps the server's default.

## Window defaults

| Parameter | Default |
|---|---|
| `q`, `r` | `0`, `0` |
| `cols`, `rows` | `61`, `45` |
| `turn` | `0` |
| `layer` | `elevation` |
| `hex-radius` | `10` |
| `scale` | `2` |
| `stride` | `1` |

The window is larger than the tuning tool's and the layer is the elevation scalar
rather than the coarsest noise scale, because somebody opening a saved world wants
to see land and water, not the composition behind them.

## The page

Every control is a link. The page carries:

- the picture, at `/seed/{seed}/map.png` with the same query
- the build, algorithm version, render version, page version, world radius, the
  configuration fingerprint, and whether it is this binary's defaults
- the world's path, the build that created it, and when — or, with no world, that
  this is diagnostic
- the layer list, all seventeen, each a link that changes only the layer
- the six compass links, `N` `NE` `SE` `S` `SW` `NW`, each a third of the window
- three window sizes: `small` 41×31, `medium` 61×45, `large` 101×75
- `zoom in` and `zoom out`, which double and halve `hex-radius`
- the six turns, `0` through `5`
- the layer's one-line documentation, the window's tile count, and its evaluation
  count
- **the centre readout**: the tile at the middle of the window, named — its
  coordinate and `s`, its rim distance and rim flag, its terrain, elevation, heat
  and moisture classifications, and the elevation, heat, moisture and relief
  scalars behind them

The readout costs one tile against the `cols × rows` the image beside it costs. A
link to a window is otherwise a link to a picture, and a reader would have to count
swatches against the key to find out what they are looking at.

## Caching

| Response | `ETag` | `Cache-Control` |
|---|---|---|
| the page | `"p<page>-a<algorithm>-r<render>-c<fingerprint tag>-<seed>-<query>"` | `no-cache` |
| a diagnostic image | `"a<algorithm>-r<render>-c<fingerprint tag>-<seed>-<query>"` | `no-cache` |
| a world-backed image | none | `no-cache` |

`PageVersion` is in the page tag and deliberately not in the image tag, so changing
the markup does not invalidate a cached PNG. `PageVersion` is `1`.

A matching `If-None-Match` is answered `304`.

## Concurrency

Concurrent renders are bounded to `GOMAXPROCS`. `net/http` starts a goroutine per
request and will happily start ten thousand; the work here is CPU-bound.

## Console output

At startup:

```text
wgva-serve: the map viewer, on one machine for one person for as long as a browser tab is open
wgva-serve: build 0.25.1-alpha+e87614f, algorithm version 6, render version 1, world radius 32767
wgva-serve: configuration f32d2b72 (this binary's defaults)
wgva-serve: world world.wgva, seed 0x0123456789abcdef, created by 0.25.1-alpha+e87614f
wgva-serve: this server opens and never writes; overlays are read fresh on every request
wgva-serve: 10 concurrent renders, every render's cost logged below
wgva-serve: http://127.0.0.1:8181/seed/0x0123456789abcdef
```

With no world the last two world lines are replaced by:

```text
wgva-serve: seed 0x0000000000000000 — diagnostic: no world is saved, and every seed is servable
wgva-serve: `wgva-world create` is what makes a world; this server only looks at one
```

Then one line per render:

```text
wgva-serve: /seed/0x0123456789abcdef/map.png (2745 tiles, 2745 evaluations, 5 ms generate, 17 ms encode, 525413 tiles/s)
```

Nothing refuses a window for being expensive; what every render costs is logged as
it is served.

## HTTP refusals

| Condition | Status |
|---|---|
| a seed that is not this server's | `404` |
| a malformed seed | `400` |
| a window parameter out of range or misspelled | `400`, body naming the parameter |
| a viewport the renderer refuses | `400` |
| anything else | `500` |

Nothing a caller can type is the process's fault, so no typed request produces a
`500`.

## Startup refusals

| Condition | Message |
|---|---|
| `--db` names an absent or empty file | ``<path>: no world file there`` followed by ``only `wgva-world create` makes a world file; this server opens one`` |
| `--db` fails an opening gate | the gate's message; see [`wgva-world`](wgva-world.md#opening-gates) |
| `--seed` unparseable | `--seed: "<text>": seed must be sixteen hexadecimal digits, or a decimal number` |
| the address is in use | `listen: <reason>` |

Every message is prefixed `wgva-serve: ` on standard error, and the process exits
`1`. It otherwise runs until interrupted.

## Examples

Serve a saved world:

```sh
wgva-serve --db world.wgva
```

Serve a seed with no world behind it, on another port:

```sh
wgva-serve --seed 0x0123456789abcdef --port 9001
```

## See also

- [Window grammar](window-grammar.md) — the query parameters and the layers
- [`wgva-world`](wgva-world.md) — the only thing that creates a world file
- [`wgva-tune`](wgva-tune.md) — the stateful sibling, which decides how worlds look
- `DESIGN.md` 29.3
