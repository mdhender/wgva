# Reference

Technical description of the four WGVA commands and the grammar they share.

Each manual describes one command: its flags, its routes, its output, its
refusals, and its exit status. They describe and do not instruct; `DESIGN.md` is
the specification and `AGENTS.md` is the working guidance.

| Manual | Command | In one line |
|---|---|---|
| [`wgva-tune`](wgva-tune.md) | the terrain tuning tool | decides how worlds look; cannot open or change one |
| [`wgva-world`](wgva-world.md) | the world builder | creates a world file from a seed; cannot draw one |
| [`wgva-map`](wgva-map.md) | the map renderer | writes one window to one image file |
| [`wgva-serve`](wgva-serve.md) | the map viewer | looks at a world that is already saved |
| [Window grammar](window-grammar.md) | — | the parameters and layers the three rendering tools share |

The four are described here as they behave at build `0.25.1-alpha`, algorithm
version 6, render version 1, world radius 32767.

## What each may touch

```text
cmd/wgva-tune   ->  view, render, config    (no store edge, by design)
cmd/wgva-world  ->  store, config           (no render edge, by design)
cmd/wgva-map    ->  render, store, config
cmd/wgva-serve  ->  view, render, store
```

The two edges the compiler cannot forbid are enforced by a `go list -deps` test in
each package: the tool that decides how worlds look cannot touch a world, and the
tool that makes a world cannot draw one.

Only `wgva-world create` creates a world file. Every other command opens one and
refuses an absent or empty file.
