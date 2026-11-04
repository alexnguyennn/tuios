# Copy-mode seek and two-pick copies

`s` and `S` are fixed copy-mode keys: seek one or two characters respectively.
Lowercase queries use smartcase. Labels use `[hints] alphabet`, nearest row
first. A pick moves the **real copy cursor**, including in scrollback and in
a visual selection. Escape cancels without moving it.

The unbound actions `copy_mode_easycopy` and `copy_mode_linecopy` can be bound
under `[keybindings.prefix_mode]`, for example to `S` and `W`. Easycopy picks
two character positions inclusively. Linecopy picks the first and last lines;
a capital label copies just that logical line. A pick on a wrapped row starts
at the logical line's beginning and the final pick extends through its hard
line break. Extraction joins soft-wrapped rows without inserting newlines.

## Architecture and extension points

```
copy-mode key / bound action
  -> internal/input/copymode_seek.go (shared IO lock)
  -> terminal.CopyMode seek state and absolute-row SeekTargets
  -> app/copymode_seek_render.go -> render_terminal.go cell loop
  -> label pick -> seekMoveTo -> updateVisualEnd -> normal motions/yank
```

State belongs to the client-side `terminal.CopyMode`, not the daemon. Targets
use absolute scrollback+screen row indices; viewport mapping uses
`ScrollOffset`. `CancelSeek` clears the transient state. Label drawing never
rewrites terminal cells: it overlays their rendered cells and invalidates the
pane cache through copy-mode effects. Labels do not dim the rest of the pane.

`seekTargets` reuses `internal/hints.Assign`, drops overlapping labels and
wide-cell placements, then relabels. `seekRequest.lines` selects row starts;
`after` filters candidates for the second pick. `pickSeekTarget` dispatches
by `SeekPurpose`; clipboard effects are applied after releasing the IO lock,
using the same OSC 52 / paste-buffer path as normal yank. Linecopy's logical
bounds use the existing extraction soft-wrap metadata (including scrollback).

To add a variant, define its key/action grammar first, translate it into a
request, keep targets in absolute coordinates, and reuse `seekMoveTo` rather
than drawing a second cursor. Add E2E assertions for both cursor placement and
exact clipboard bytes, on both backends, with a negative-control build.

Not implemented: copytk `S j/k/n`, directional seek, minimum match spacing,
dimming, multi-pane copy mode. Although row/after filters exist, the current
`S` grammar always consumes two query characters; reserving j/k/n or direction
keys would change literal matching and needs an explicit grammar decision.
These are deferred rather than silently taking away two-character queries.
Labels are snapshots: rewritten pane text is not revalidated on pick.
