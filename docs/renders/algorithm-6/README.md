# Acceptance sheet — algorithm version 6, render version 1

|  |  |
|---|---|
| algorithm version | 6 |
| render version | 1 |
| world radius | 32767 |
| configuration | `f32d2b728906de9c7f0921b2ca652b778551abaa390a5741da62371bcfd3430b` — this binary's defaults |
| build | `0.24.0-alpha` |

These are the images phase 8's exit condition is about: *multiple seeds and
distant coordinate windows produce varied but coherent maps and player PNGs.*
They are evidence, not goldens — a golden is a decoded RGBA buffer compared by a
test, and these are pictures for a person to look at. Nothing reads them.

Regenerate them with the commands below. The configuration is the binary's
built-in defaults, so no `--config` is needed and none should be given: the
fingerprint above is what `config/fingerprint_test.go` pins, and a sheet drawn
under anything else is a sheet of a world nobody can make.

## Two seeds, the same window

`world-0x0123456789abcdef.png`, `world-0xdeadbeefcafef00d.png`

```sh
wgva-map --seed 0x0123456789abcdef --grid --cols 601 --rows 451 \
    --stride 8 --scale 1 --layer terrain --q 0 --r 0 --out world.png
```

Continents with coastlines, ice at the cold end, and varied interiors — and two
different worlds at the same window, which is what a seed is for. A stride of
eight puts 4808 hexes across the image.

**A coarser stride is not a better overview.** At a stride of 128 the sampling
falls far below the continental wavelength's Nyquist and the image is aliasing
rather than geography. That is point sampling doing what point sampling does, not
a defect in the fields; `DESIGN.md` 9.3 is the same bound from the other end.

## A distant window

`distant-window-0x0123456789abcdef.png`, centred on `(30000, -20000)`

```sh
wgva-map --seed 0x0123456789abcdef --grid --cols 601 --rows 451 \
    --stride 8 --scale 1 --layer terrain --q 30000 --r -20000 --out distant.png
```

The point of this one is that it is not interesting. It is 36,000 hexes from the
origin and reads exactly like the window above: same kind of coastline, same
range of interiors, no seam and no drift. That is `DESIGN.md` 30.13, *no place is
special*, as a picture rather than as a test.

## A player's window

`player-0x0123456789abcdef.png`

Generated terrain and player overlays are stored apart and meet only here, at
render time, in pixels. Two settlements, one label, and a radius-nine disc of
discovered tiles around `(2456, -1200)`:

```sh
wgva-world create --seed 0x0123456789abcdef --expect "$(wgva-world identity)" world.wgva
# the game engine writes the overlays; store.SetDiscovered and friends are the API
wgva-map --db world.wgva --q 2456 --r -1200 --cols 41 --rows 31 \
    --hex-radius 12 --layer terrain --out player.png
```

Fog hides terrain and fog does not hide the player's own marks — the two rules
differ on purpose, and a settlement outside the discovered disc is still drawn
because it is something the player built. An empty discovery set means fog is
switched *off*, not that nothing has been seen.
