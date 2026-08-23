# Colors currently in use

A reference of every color the TUI uses, what it means in each place it
appears, and — where the code documents it — why that particular color
was picked. Compiled from the current state of the code (`tui.go`,
`recap.go`, `host.go`, `template.go`, `treelist.go`, `tabs.go`), not
aspirational; update this alongside any future color change rather than
letting it drift.

## The five outcome colors

The single source of truth is `colorTag(o outcome) string` in `tui.go`:

| Outcome       | Color    |
|---------------|----------|
| OK            | green    |
| Changed       | yellow   |
| Skipped       | teal     |
| Failed        | red      |
| Unreachable   | maroon   |

`teal` is tcell's/W3C's closest named match for ANSI cyan. `maroon`
(not `brown`/`darkred`) is deliberately muted vs. `red`: both are
base-16 ANSI palette names (index 1 vs. index 9), not RGB-approximated
extended-W3C names, so they stay reliably distinct across terminal
themes that remap individual color slots.

Everywhere else in the app that colors something by outcome calls
`colorTag(o)` rather than hardcoding a name a second time:

- Collapsed task row's per-host list (`taskLabel`) — each hostname,
  foreground-colored normally, background-colored (`pureBlack` text on
  top) when the row is selected.
- Expanded host row (`hostLabel`) — the whole line, one outcome.
- The narrow-terminal/no-color-terminal `OK:x/Chgd:x/Skip:x/Fail:x/Unrch:x`
  summary that replaces the host list (design-docs/Morehosts.md),
  per-field, via `summaryFieldColor`.
- The output drill-down's Task tab Status line.
- The recap's per-host/per-category/per-task rows (`recapCategoryColor`
  maps its own label strings — "ok"/"skipped"/"changed"/"unreachable"/
  "failed" — straight to `colorTag`; "warnings" is its own case, see
  below; anything else falls back to plain white).

## Gray — two related but distinct meanings

`grayTag = "gray"`:

1. **Not yet reported.** On a collapsed task row's host list, a host
   that's in the run-wide `AllHosts` set but hasn't recorded a result
   for *this specific task* yet renders gray instead of an outcome
   color — "known about, nothing to report yet."
2. **Zero count, de-emphasized.** In both the recap summary line
   (`recapSummaryFieldColor`) and the Morehosts.md count-summary string
   (`summaryFieldColor`), a field whose count is 0 renders gray instead
   of its outcome color; only fields with something to report keep
   their real color. The recap version came first — a live experiment
   showed six always-colored `label=N` segments read as one uniform
   "wall of color" regardless of what actually happened, so graying out
   the zeros makes only the meaningful fields stand out at a glance.
   The Morehosts.md summary string deliberately reuses this exact
   convention rather than re-deriving it.

`gray` is also used, unrelated to either meaning above, as an
annotation color: the `[file, line N]` source-location note under the
Task definition tab, and `host.go`'s `(from fact cache)` annotation —
both "supplementary, not primary reading material," matching gray's
general de-emphasized role.

## Silver — task row titles

A task row's own title text (both host-list mode and Morehosts.md's
summary mode) is `"silver"`, deliberately distinct from both `gray`
(which already means "hasn't reported yet") and `white` (which already
means play rows) — a third, neutral shade so the title reads as its own
thing rather than blurring into either existing convention.

## White — several distinct roles

`white` is reused for more than one purpose, none of them outcome-related:

- **Play rows** — a play's name, bold, unselected.
- **Top/bottom chrome bars** — `barStyle`, `tcell.ColorWhite` on
  `tcell.ColorNavy`, applied as a real `tcell.Style` rather than a tag
  string.
- **Top bar's progress-sweep** — white bold text over both the filled
  (`progressFillColor`) and unfilled (`navy`) portions of the "headline
  as a progress bar" fill.
- **Recap heading** — "Summary" and its `====` underline.
- **Tab bar's inactive tabs** — `[white:navy:-]`, matching `barStyle`'s
  own scheme so inactive tabs read as the same chrome.
- **Host-verb view's plain hostname row** — unselected, uncolored-by-outcome.
- `recapCategoryColor`'s fallback case for an unrecognized label — not
  expected to actually be hit by any real category today.

## The three hand-picked exceptions

Three colors are deliberately *not* plain named tcell colors, because a
value that exactly matches a base-16 ANSI slot's nominal RGB — even
given as a hex string — gets resolved to that slot on a reduced-palette
terminal, and some terminal themes remap individual slots to something
that reads badly (the same trap noted above for `maroon` vs. `red`):

- **`pureBlack = "#1a1a1a"`** — not `tcell.ColorBlack`/`"black"`, since
  some themes remap that slot to a dark gray rather than true black.
  Used as the foreground for *every* selected row's text, everywhere in
  the app (task/host/play rows, recap rows, the host-verb view) —
  always paired with `lightgray` as the background, or with an outcome
  color as the background for per-segment selected-row coloring.
- **`progressFillColor = "#146414"`** — not `"green"`; deliberately
  darker than the nominal xterm green (`#008000`) so white bold text
  stays readable on top of it, while still landing on a fixed,
  non-remappable extended-256 slot. Sole use: the top bar's
  progress-sweep fill background.
- **`warningColor = "hotpink"`** — the one exception that *is* a plain
  named tcell color rather than a hex value, specifically because
  `"hotpink"` doesn't equal any base-16 slot's nominal RGB in the first
  place, so it isn't subject to the same remapping risk. Chosen to echo
  real `ansible-playbook`'s own default `[WARNING]` color family
  (pink/magenta). Means "warning," never an outcome — three usages:
  the collapsed task row's aggregate ⚠ glyph, the expanded host row's
  per-host ⚠ glyph, and the output drill-down's Warnings section
  heading / the recap's "warnings" category color.

## Lightgray — selected-row background

Always paired with `pureBlack` foreground text: the background for a
selected play row, a selected task row's title portion, a selected host
row, and selected recap rows. Represents "this is the identifying
label," not an outcome — the parts of a selected row that *are* outcome
data get an outcome color as their background instead (see `pureBlack`
above and `halfBlock` below).

## Output drill-down tab section colors

Colors here are deliberately chosen *outside* the five-outcome palette,
so a reader never confuses "this is the STDERR section" with "this
host's outcome is Failed":

- **`aqua`** — the Output tab's own Output section (stdout/msg text).
- **`warningColor`** (hotpink) — the Output tab's Warnings section.
- **`yellow`** — the Output tab's Items section (loop-item bullets) —
  note this is the same tag name as `outcomeChanged`, but an unrelated
  context; no shared meaning intended.
- **`red`** — the Output tab's Error section (stderr) — same tag name
  as `outcomeFailed`, again an unrelated but not-coincidentally similar
  context (both mean "something's wrong").
- **`orange`** — key-highlighting in the Task definition tab's YAML
  source (`colorizeYAML`, `key:` portions only); also the host-verb
  view's own section headers (host-vars file, per-play sections).

The Resolved/Docs/Details tabs are plain, uncolored text — each is its
own tab now (see Tabbed UI.md) rather than a stacked section needing a
heading color to separate it from its neighbors.

## Diff tab colors

A direct port of `ansible-core`'s own `--diff` callback coloring:

- **`green`** — `+`-prefixed lines, including the `+++` file header.
- **`red`** — `-`-prefixed lines, including `---`.
- **`teal`** — `@@` hunk headers.
- Unprefixed context lines carry no color tag at all.

Note this reuses `green`/`red`/`teal`'s tag *names* from the outcome
palette, but in a completely unrelated semantic context (added/removed
diff lines, not host outcomes) — no shared helper function, no
cross-reference between the two meanings in the code.

## Run-completion status line

`statusRowText`, the line appended below the tree once a run finishes:

- **`green`** — genuine success (`exitCode == 0`): "Playbook completed successfully."
- **`yellow`** — benign unreachable-host case (`exitCode == 4` and at
  least one host really was unreachable): "Playbook completed - one or
  more hosts were unreachable."
- **`red`** — user-interrupted (`exitCode == 99`), and the generic
  failure default case: "Playbook failed (exit code N)."

## Tab bar

- **Active tab** — `[navy:white:B]`, navy text on a white background:
  fully inverted from the app's own navy-background chrome, and
  deliberately spelled out with explicit colors rather than a `[::R]`
  reverse-video tag.
- **Inactive tabs** — `[white:navy:-]`, matching `barStyle`'s own scheme.

## Chrome: navy

- **`barStyle`** — `tcell.ColorWhite` on `tcell.ColorNavy`, the real
  `tcell.Style` applied to the top and bottom bars.
- **The two-pane drill-down's vertical divider** — `tcell.ColorNavy`
  background, explicitly matching `barStyle` rather than a brighter
  named "blue," so the divider reads as the same chrome rather than a
  clashing second shade.

## Chrome: purple (proposed, design-docs/Revisit.md)

`purple` — `tcell.ColorWhite` text on `tcell.ColorPurple` — is proposed
as the chrome color for a revisited (historical) run: the top bar, the
bottom bar, and the two-pane drill-down's vertical divider all switch
from `barStyle`'s navy to this instead, for the duration of viewing
saved data, so the "this isn't live" distinction is visible everywhere
the chrome itself appears, not just in one spot. Reverts to ordinary
navy the moment a real re-run is kicked off from within a revisited
session — from that point on there's a live process again, not archived
data.

`purple` (not a hex literal) for the same reason `maroon` was chosen
over `brown`/`darkred` elsewhere in this doc: it's one of the fixed
base-16 ANSI palette slots, so it renders reliably across terminal
themes rather than being approximated from RGB. Picked over the other
unused base-16 names (`fuchsia`, `olive`, `lime`) for being close enough
in tone to navy to still read as "chrome," while being unambiguously a
different hue at a glance — the same "clearly chrome, clearly not the
same chrome" balance the divider's own navy-vs-brighter-blue reasoning
above already goes for.

## Halfblock blending — not a color, a mechanism

`halfBlock` (U+258C LEFT HALF BLOCK) isn't a color of its own — it's how
`hostTransition(leftTag, rightTag)` blends two *adjacent* hostnames'
outcome colors together on a selected task row: left host's color as
foreground, right host's color as background, so the cell between them
reads as a gradient rather than an abrupt jump. Selected-row-only — it
was tried on unselected rows too and reverted, since unselected
hostnames are foreground-colored text on a plain background, and the
same blend didn't read well there.

## Quick reference table

"ANSI" means one of tcell's fixed 16 base ECMA/XTerm palette slots
(`ColorBlack` … `ColorWhite`, defined as a plain enum with no RGB value
of its own — the ones a terminal theme can remap). "No" means the color
is defined by an explicit RGB value instead — either a named-but-still-
RGB tcell color (`orange`, `lightgray`, `hotpink`) or one of this app's
own hand-picked hex literals (`pureBlack`, `progressFillColor`) — and so
always renders as the same fixed color regardless of the terminal's own
theme/palette remapping.

| Semantic meaning | Color name | ANSI (base-16)? |
|---|---|---|
| OK outcome | green | Yes |
| Changed outcome | yellow | Yes |
| Skipped outcome | teal | Yes |
| Failed outcome | red | Yes |
| Unreachable outcome | maroon | Yes |
| Host hasn't reported for this task yet | gray | Yes |
| Zero-count de-emphasis (recap / Morehosts.md summary string) | gray | Yes |
| Supplementary annotation (source location, fact-cache note) | gray | Yes |
| Task row title | silver | Yes |
| Play row title | white | Yes |
| Top/bottom chrome bars (`barStyle`) | white on navy | Yes |
| Progress-sweep text (top bar) | white | Yes |
| Recap heading ("Summary") | white | Yes |
| Inactive tab | white on navy | Yes |
| Host-verb view's plain hostname row | white | Yes |
| Selected-row text (every selected row, app-wide) | pureBlack (`#1a1a1a`) | No |
| Selected-row background, label portion | lightgray | No |
| Progress-sweep fill background (top bar) | progressFillColor (`#146414`) | No |
| Warning glyph (task/host rows); Warnings section; "warnings" recap category | warningColor = hotpink | No |
| Output tab's Output section | aqua | Yes |
| Output tab's Items section | yellow | Yes |
| Output tab's Error section | red | Yes |
| Task-definition tab's YAML key highlighting | orange | No |
| Host-verb view's section headers | orange | No |
| Diff tab: added lines | green | Yes |
| Diff tab: removed lines | red | Yes |
| Diff tab: hunk headers | teal | Yes |
| Status line: genuine success | green | Yes |
| Status line: benign unreachable-host completion | yellow | Yes |
| Status line: user-interrupted / generic failure | red | Yes |
| Active tab | navy on white | Yes |
| Two-pane divider | navy | Yes |
| Revisit chrome (top/bottom bars, divider) — proposed | purple | Yes |
