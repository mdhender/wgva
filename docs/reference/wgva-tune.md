# `wgva-tune` — the terrain tuning tool

Decides how worlds look. It cannot open or change one.

It runs on a developer's machine and produces a settings file. Nothing is deployed
and no player ever touches it. It binds to loopback, has no authentication, and has
exactly one user on one machine for as long as a browser tab is open.

## Synopsis

```sh
wgva-tune [--seed <seed>] [--config <file>] [--host <address>] [--port <port>]
```

## Description

`wgva-tune` presents the complete effective configuration as a form, draws any
window of the world that configuration produces, and turns the configuration back
into a TOML file.

It cannot touch a saved world, and that is a fact of the build rather than a
promise: the package has no import path to `store`, and `cmd/wgva-tune/deps_test.go`
fails if one appears. Its mirror image is [`wgva-world`](wgva-world.md), which has
no import path to `render`.

**It is stateful, and that is the one departure.** The thing it exists to change is
on the order of a hundred numeric fields, and a hundred fields do not fit in an
address bar, so the configuration lives in the process's memory and a form POST is
how it changes. Everything about the *view* is still in the URL — tabs are routes,
the window is query parameters, and every control is a link or a GET form. What
that gives up is that a link shows what this process is drawing now, not what it
drew when the link was copied. What it keeps is the fingerprint: `/config.toml`
turns the configuration behind any picture back into a file.

The deliverable is not the tool. It is that a world's appearance becomes a versioned
file with a fingerprint on it, so any picture can be traced to the configuration
that produced it and reproduced elsewhere. The web interface is only how somebody
drives it.

See `DESIGN.md` 29.1.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--seed <seed>` | `0x0123456789abcdef` | the seed a bare visit opens on |
| `--config <file>` | this binary's defaults | configuration file to start from |
| `--host <address>` | `127.0.0.1` | address to bind |
| `--port <port>` | `8180` | port to bind |

The loopback default is the decision, not the flag. There is no authentication here
and the cost of an endpoint is chosen by the caller.

The seed is not fixed for the process's lifetime: it is a route segment, and any
seed can be visited. `--seed` only decides where `/` lands.

## Routes

| Route | Response |
|---|---|
| `GET /` | `303` to the map tab of a seed |
| `GET /seed/{seed}` | the map tab, `text/html` |
| `GET /seed/{seed}/map.png` | the hex render, `image/png` |
| `GET /seed/{seed}/grid` | the grid tab, `text/html` |
| `GET /seed/{seed}/grid.png` | the grid render, `image/png` |
| `GET /seed/{seed}/config` | the configuration tab, `text/html` |
| `POST /seed/{seed}/config/fields` | applies the form, `303` back to the configuration tab |
| `POST /seed/{seed}/config/upload` | adopts an uploaded or pasted file, `303` back |
| `POST /seed/{seed}/config/reset` | restores this binary's defaults, `303` back |
| `GET /config.toml` | the current configuration as a file, `application/toml` |

`GET /` accepts `seed` and `tab` query parameters, because an HTML GET form cannot
write a path segment. `tab` is `grid` or `config`; anything else lands on the map
tab. Both are stripped from the query and every other parameter is forwarded.

The four window-bearing routes take the [window grammar](window-grammar.md) as query
parameters.

## Window defaults

| Parameter | Default |
|---|---|
| `q`, `r` | `0`, `0` |
| `cols`, `rows` | `41`, `31` |
| `turn` | `0` |
| `layer` | `continentalness` |
| `hex-radius` | `12` |
| `scale` | `2` |
| `stride` | `1` |

The coarsest layer is the default because it is the one that says whether the world
has a shape at all.

## The map tab

- the hex render of the window
- the identity header: build, algorithm version, world radius, the configuration
  fingerprint, and whether it is this binary's defaults or `modified`
- **the `wgva-world create` line** for the seed and configuration on screen, whole
  and ready to paste, including `--expect`
- the layer list, all seventeen, each link titled with what the layer shows
- the six compass links, each a third of the window
- `zoom in` and `zoom out`, which double and halve `hex-radius`
- six window widths: 11, 41, 101, 301, 601, 1001, each with rows at three quarters of
  the width, rounded up to odd
- the six turns, `0` through `5`
- the derived field tree the layer is drawn from
- the window's tile count and evaluation count
- **the distribution readout**: what terrain, elevation and climate is actually in
  the window

The readout costs a whole `Tile` per cell — seven evaluations whatever layer is on
screen — so a large window on this tab is genuinely slow. Nothing refuses a window
for being expensive; what the tool does about it is say what it cost.

## The grid tab

The same walk at a stride, one cell per `scale × scale` block of pixels.

- `zoom in` and `zoom out` change the **stride**, not the hex radius, because a grid
  cell is one block of pixels whatever happens. Zooming *in* **lowers** the stride.
- a stride list: 1:1, 1:2, 1:4, 1:8, 1:16, 1:64, 1:256
- compass steps are multiplied by the stride, so a click moves a third of the picture
  at every stride
- no distribution readout: a million tiles of readout is seven million evaluations
  for a second copy of work the image already did
- at any turn other than `0` or `3`, a warning that the turn shears the picture. The
  grid's distortion has two-fold symmetry and the hex grid has six-fold, so only
  turns 0 and 3 lie in both. The map tab is undistorted at every turn.

## The configuration tab

Every field of the complete effective configuration, laid out under eleven headings,
in this order:

`world`, `warp`, `ridge`, `hierarchy`, `rim`, `elevation`, `basin`, `volcanic`,
`terrain`, `heat`, `moisture`

Each field shows its key, its one-line documentation, its current value, this
binary's default, and whether it has been moved off that default. A field with a
fixed set of values is a choice list.

Three forms:

| Form | Effect | Notice on success |
|---|---|---|
| the field table | every field is read and the whole configuration is replaced at once | `applied` |
| upload or paste | a TOML file replaces the configuration whole | `adopted` |
| reset | this binary's defaults are restored | `back to this binary's defaults` |

A configuration is validated and replaced **whole**. A POST that would make an
unusable world changes nothing at all, and the failure comes back as a `failure`
notice on the tab. The tool is therefore never drawing something that could not be
written to a file.

The upload body is bounded at 1 MiB. A configuration file is a few kilobytes; the
bound is there because the endpoint reads a body. An empty upload and an empty paste
are refused with `nothing was uploaded or pasted`.

## `/config.toml`

The configuration currently in the process, as the TOML file a person edits: flat,
one key per line, with a comment header carrying the algorithm version, the world
radius, the fingerprint, and the build.

```text
Content-Type: application/toml; charset=utf-8
Content-Disposition: attachment; filename="config.toml"
Cache-Control: no-store
```

The header is a comment and is not read back. Values are written directly rather
than through an encoder, because an encoder's default float formatting is not
guaranteed to be the shortest representation that round-trips and a file that loses
a low bit is a silently different world.

A file emitted here is what `wgva-world create --config` and `wgva-map --config`
read, and what `wgva-tune --config` starts from.

## Caching

| Response | `ETag` | `Cache-Control` |
|---|---|---|
| any image | `"<8-byte fingerprint tag>-<request URI>"` | `no-cache` |
| any page | none | `no-store` |
| `/config.toml` | none | `no-store` |

The image tag is not a pure function of the URL, which is the departure, but the
configuration's fingerprint is in it: move a field and every tag moves with it, so a
browser holding an old image asks again. Eight bytes rather than the four a page
prints, because a tuning session walks through hundreds of configurations under
otherwise identical URLs.

A matching `If-None-Match` is answered `304`.

## Concurrency

Concurrent renders are bounded to `GOMAXPROCS`. The configuration is guarded by a
read-write mutex and a `Generator` is built fresh from a snapshot per request, so a
form POST arriving mid-render cannot change what that render is drawing.

## Console output

At startup:

```text
wgva-tune: the terrain tuning tool, on a developer's machine and nowhere else
wgva-tune: build 0.25.1-alpha+e87614f, algorithm version 6, world radius 32767
wgva-tune: configuration f32d2b72 (this binary's defaults)
wgva-tune: seed 0x0123456789abcdef, 10 concurrent renders, every render's cost logged below
wgva-tune: http://127.0.0.1:8180/
```

Then one line per render, and one per distribution readout:

```text
wgva-tune: /seed/0x0123456789abcdef/map.png (1271 tiles, 1271 evaluations, 4 ms generate, 5 ms encode, 302619 tiles/s)
wgva-tune: readout /seed/0x0123456789abcdef (1271 tiles, 8897 evaluations, 6 ms generate, 0 ms encode, 199382 tiles/s)
```

Generate and encode are separated because they move for different reasons, and an
"it got slower" report that does not separate them is not yet a measurement.

## HTTP refusals

| Condition | Status |
|---|---|
| a malformed seed | `400` |
| a window parameter out of range or misspelled | `400`, body naming the parameter |
| a viewport the renderer refuses | `400` |
| a form value or file that will not parse | `303` back to the configuration tab with a `failure` notice |
| anything else | `500` |

## Startup refusals

| Condition | Message |
|---|---|
| `--seed` unparseable | `--seed: seed="<text>": value is not a number` |
| `--config` unreadable | `--config: <reason>` |
| `--config` contents rejected | `--config: <path>: <reason>` |
| the address is in use | `listen: <reason>` |

Every message is prefixed `wgva-tune: ` on standard error, and the process exits
`1`. It otherwise runs until interrupted.

## Examples

Open on the default seed:

```sh
wgva-tune --seed 0x0123456789abcdef
```

Resume from a configuration file:

```sh
wgva-tune --config config.toml
```

Take the file back out, without a browser:

```sh
curl -sO http://127.0.0.1:8180/config.toml
```

## See also

- [Window grammar](window-grammar.md) — the query parameters and the layers
- [`wgva-world`](wgva-world.md) — consumes the `create` line this tool emits
- [`wgva-serve`](wgva-serve.md) — the stateless sibling, which looks at saved worlds
- `DESIGN.md` 29.1, 29.5 (the identity pair), 31.1 (measuring a render)
