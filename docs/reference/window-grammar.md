# Window grammar

The set of parameters that name one window, and the layers a window can be drawn
as. It is owned by package `view` and package `render`, and it is one grammar
rather than one per front end: the flags of `wgva-map`, the query parameters of
`wgva-tune`, and the query parameters of `wgva-serve` are the same names with the
same meanings, and a window moves between them unchanged.

See `DESIGN.md` section 28.

## Parameters

Nine parameters, plus `s`. A parameter that is present is applied; a parameter
that is absent keeps the command's default.

| Parameter | Type | Meaning |
|---|---|---|
| `q` | integer | axial *q* of the coordinate at the window's center |
| `r` | integer | axial *r* of the coordinate at the window's center |
| `s` | integer | third component, checked and not stored; `q + r + s` must be `0` |
| `cols` | integer | window width in cells |
| `rows` | integer | window height in cells |
| `turn` | integer | rotation of the window, in sixths |
| `layer` | name | what the window is drawn as; see [Layers](#layers) |
| `hex-radius` | integer | pixels from a hex's center to a corner, hex renders only |
| `scale` | integer | pixels per cell, grid renders only |
| `stride` | integer | hexes between sampled cells, grid renders only |

`hex-radius` has no effect on a grid render and `scale` and `stride` have no
effect on a hex render. All four are carried in a window regardless of which
render is drawing it.

## Bounds

`q` and `r` are refused outside `-32767 .. 32767`, the canonical coordinate
domain. A coordinate is refused rather than wrapped.

`turn` is reduced modulo 6, so `turn=7` is `1` and `turn=-1` is `5`.

The remaining counts are clamped, never refused:

| Parameter | Minimum | Maximum | Also |
|---|---|---|---|
| `cols` | 1 | 1001 | rounded up to an odd number, so the window has a center cell |
| `rows` | 1 | 1001 | rounded up to an odd number |
| `hex-radius` | 2 | 64 | |
| `scale` | 1 | 16 | |
| `stride` | 1 | 1024 | |

## Refusals

| Condition | Error |
|---|---|
| a parameter that is not an integer | `view.ErrNotANumber` |
| `q` or `r` outside the coordinate domain | `view.ErrOutOfRange` |
| `s` inconsistent with `q` and `r` | `view.ErrOutOfRange` |
| a layer name this binary does not draw | `render.ErrUnknownLayer` |

Each is a `*view.ViewError` naming the parameter and wrapping the reason. The two
web front ends turn every one of them into a `400` with the message as the body.

## Layers

Seventeen. `Cost` is generator evaluations per tile, and it is the unit a window's
cost is counted in: `relief` and `terrain` read the six neighboring elevations and
cost seven apiece, every other layer costs one.

| Layer | Cost | Shows |
|---|---|---|
| `continentalness` | 1 | the coarsest scale: continents and their interiors |
| `regional` | 1 | uplift and the shape of a coast |
| `local` | 1 | hills and valleys |
| `detail` | 1 | the finest structure the tile grid can carry |
| `elevation-raw` | 1 | the weighted sum of the four continuous scales alone, before the contrast pass, the uplift, and the ridges |
| `elevation` | 1 | the elevation scalar: -1 deep ocean, 0 sea level, +1 extreme highland |
| `relief` | 7 | local steepness: the mean elevation difference to the six neighbors |
| `ridge` | 1 | the ridge structure term, before the region roughness scales it and before the land mask confines it to land |
| `region-influence` | 1 | the blended regional elevation bias: what regional uplift is made of |
| `roughness` | 1 | the blended regional roughness bias: where relief is exaggerated and where it is subdued |
| `temperature` | 1 | the heat scalar: -1 polar, 0 the middle of the temperate band, +1 hot, with the lapse rate already taken off the high ground |
| `moisture` | 1 | the moisture scalar: -1 arid, 0 the middle of the moderate band, +1 saturated |
| `climate` | 1 | the two-axis climate classification: which of the twenty-five heat and moisture cells a tile falls in |
| `basin` | 1 | the blended basin influence: -1 a rise that sheds water, 0 neutral ground, +1 a closed hollow; it multiplies the moisture terrain reads and never enters elevation |
| `volcanic` | 1 | the volcanic tendency, which terrain reads only the top of: a province is where the cones can be, not where they are |
| `rim` | 1 | the rim profile of `DESIGN.md` 15.1: 0 across the closed band that is forced terrain, rising across the falloff, and 1 over the whole of the world inside it |
| `terrain` | 7 | the game-facing terrain classification of `DESIGN.md` 17, which is every other layer in this list arriving at one answer |

`continentalness` is the default layer where nothing says otherwise.

## Scroll steps

A compass click moves the window a third of what is on screen, in whole hexes,
never in pixels. North and south move `rows/3`; the four diagonals move `cols/3`.
Opposite points move the same distance, so a step followed by its opposite
returns to exactly the coordinate it started from.

On a grid render the step is multiplied by `stride`, because one cell there
stands for `stride` hexes.

The six compass points are `N`, `NE`, `SE`, `S`, `SW`, `NW`, from
`render.AdminCompass`.

## Seed spellings

`wgva.ParseSeed` accepts three spellings:

- sixteen hexadecimal digits with an `0x` prefix — `0x0123456789abcdef`
- the same sixteen digits bare — `0123456789abcdef`
- a decimal number — `81985529216486895`

Exactly sixteen characters that are all hexadecimal is read as hex; anything else
that parses as decimal is decimal. Every tool prints a seed as sixteen
hexadecimal digits with the prefix.

## See also

- [`wgva-map`](wgva-map.md) — the flags of the same names
- [`wgva-serve`](wgva-serve.md) — the query parameters of the same names
- [`wgva-tune`](wgva-tune.md) — the query parameters of the same names
