# Colors currently in use

A reference of every color the TUI uses, what it means in each place it
appears, and — where the code documents it — why that particular color
was picked. Compiled from the current state of the code
(`internal/uikit/tui_style.go`, `tui_layout.go`, `tui_rows.go`,
`tui_dialogs.go`, `tui_drilldown.go`, `search.go`, `tabsearchbar.go`,
`tabs.go`, `treelist.go`; `internal/session/tui.go`, `recap.go`;
`internal/host/host.go`; `internal/template/template.go`;
`internal/diff/diff.go`; `internal/revisit/revisit.go`), not aspirational;
update this alongside any future color change rather than letting it
drift. (Last swept end-to-end after the `--check` chrome/tab-bar-color
work and the Filters.md "Interesting" filter — neither added a new color,
but the sweep also caught a few pre-existing gaps: `aqua`'s second
meaning, the shared "async fetch/render failed" red heading, and
`host.go`'s own reuse of the outcome palette, none previously
documented here. Reviewed and refreshed once before that too, after the
Phase 4/5 package restructuring moved every file above out of a flat
`package main`, and after the in-tab-search feature added several new
colors — see their own sections below.)

## The five outcome colors

The single source of truth is `uikit.ColorTag(o Outcome) string`
(`tui_layout.go`):

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
`ColorTag(o)` rather than hardcoding a name a second time:

- Collapsed task row's per-host list (`TaskLabel`) — each hostname,
  foreground-colored normally, background-colored (`PureBlack` text on
  top) when the row is selected.
- Expanded host row (`HostLabel`) — the whole line, one outcome.
- The narrow-terminal/no-color-terminal `OK:x/Chgd:x/Skip:x/Fail:x/Unrch:x`
  summary that replaces the host list (design-docs/Morehosts.md),
  per-field, via `SummaryFieldColor`.
- The output drill-down's Task tab Status line.
- The recap's per-host/per-category/per-task rows (`recapCategoryColor`
  maps its own label strings — "ok"/"skipped"/"changed"/"unreachable"/
  "failed" — straight to `ColorTag`; "warnings" is its own case, see
  below; anything else falls back to plain white).
- `tangsible diff`'s own collapsed/expanded host rows (`DiffColorTag`,
  see "Diff verb: content coloring" below) — the *same* five colors,
  reused rather than re-derived, with attribute flags layered on top to
  mark what changed.
- `tangsible host`/`tangsible hosts`' own Summary tab (`internal/host/host.go`)
  — a throwaway connectivity probe against the target host reuses `maroon`/
  `red` literally (not via `ColorTag`, but the same two outcome colors) to
  head a `[maroon::b]Unreachable[-::-]`/`[red::b]Failed[-::-]` block when
  the probe itself couldn't reach or run against the host, before falling
  back to a plain facts summary otherwise.

## Gray — two related but distinct meanings

`uikit.GrayTag = "gray"`:

1. **Not yet reported.** On a collapsed task row's host list, a host
   that's in the run-wide `AllHosts` set but hasn't recorded a result
   for *this specific task* yet renders gray instead of an outcome
   color — "known about, nothing to report yet."
2. **Zero count, de-emphasized.** In both the recap summary line
   (`recapSummaryFieldColor`) and the Morehosts.md count-summary string
   (`SummaryFieldColor`), a field whose count is 0 renders gray instead
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
general de-emphasized role. `tangsible revisit`'s own status label for
an aborted (Ctrl-C'd) run also uses gray (`RevisitStatusColor`, see
"Revisit list: run-status colors" below) — a third, distinct meaning
("this outcome is neither good nor bad, just moot").

## Silver — task row titles

A task row's own title text (both host-list mode and Morehosts.md's
summary mode) is `"silver"`, deliberately distinct from both `gray`
(which already means "hasn't reported yet") and `white` (which already
means play rows) — a third, neutral shade so the title reads as its own
thing rather than blurring into either existing convention. `tangsible
diff`'s own task rows (`DiffTaskRowText`, `internal/diff/diff.go`) reuse
the identical convention rather than re-deriving it.

## White — several distinct roles

`white` is reused for more than one purpose, none of them outcome-related:

- **Play rows** — a play's name, bold, unselected.
- **Top/bottom chrome bars** — `uikit.BarStyle`, `tcell.ColorWhite` on
  `tcell.ColorNavy`, applied as a real `tcell.Style` rather than a tag
  string.
- **Top bar's progress-sweep** — white bold text over both the filled
  (`ProgressFillColor`) and unfilled (`navy`) portions of the "headline
  as a progress bar" fill.
- **Recap heading** — "Summary" and its `====` underline.
- **Tab bar's inactive tabs** — `[white:navy:-]`, matching `BarStyle`'s
  own scheme so inactive tabs read as the same chrome.
- **Host-verb view's plain hostname row** — unselected, uncolored-by-outcome.
- `recapCategoryColor`'s fallback case for an unrecognized label — not
  expected to actually be hit by any real category today.
- **`tangsible diff`'s "task only present in one run" note**
  (`UnmatchedTaskNote`, `internal/diff/diff.go`) uses **yellow**, not
  white — see "Diff verb: content coloring" below; noted here only to
  head off the assumption that every diff-verb annotation shares one
  color.

## The three hand-picked exceptions

Three colors are deliberately *not* plain named tcell colors, because a
value that exactly matches a base-16 ANSI slot's nominal RGB — even
given as a hex string — gets resolved to that slot on a reduced-palette
terminal, and some terminal themes remap individual slots to something
that reads badly (the same trap noted above for `maroon` vs. `red`):

- **`uikit.PureBlack = "#1a1a1a"`** — not `tcell.ColorBlack`/`"black"`,
  since some themes remap that slot to a dark gray rather than true
  black. Used as the foreground for *every* selected row's text,
  everywhere in the app (task/host/play rows, recap rows, the host-verb
  view, the revisit list) — always paired with `lightgray` as the
  background, or with an outcome color as the background for
  per-segment selected-row coloring.
- **`uikit.ProgressFillColor = "#146414"`** — not `"green"`; deliberately
  darker than the nominal xterm green (`#008000`) so white bold text
  stays readable on top of it, while still landing on a fixed,
  non-remappable extended-256 slot. Sole use: the top bar's
  progress-sweep fill background.
- **`uikit.WarningColor = "hotpink"`** — the one exception that *is* a
  plain named tcell color rather than a hex value, specifically because
  `"hotpink"` doesn't equal any base-16 slot's nominal RGB in the first
  place, so it isn't subject to the same remapping risk. Chosen to echo
  real `ansible-playbook`'s own default `[WARNING]` color family
  (pink/magenta). Means "warning," never an outcome — three usages:
  the collapsed task row's aggregate ⚠ glyph, the expanded host row's
  per-host ⚠ glyph, and the output drill-down's Warnings section
  heading / the recap's "warnings" category color.

## Lightgray — selected-row background

Always paired with `PureBlack` foreground text: the background for a
selected play row, a selected task row's title portion, a selected host
row, and selected recap rows. Represents "this is the identifying
label," not an outcome — the parts of a selected row that *are* outcome
data get an outcome color as their background instead (see `PureBlack`
above and `halfBlock` below).

## Output drill-down tab section colors

Colors here are deliberately chosen *outside* the five-outcome palette,
so a reader never confuses "this is the STDERR section" with "this
host's outcome is Failed":

- **`aqua`** — the Output tab's own Output section (stdout/msg text). A
  second, unrelated meaning: `FilterDialogText`'s own `*` marker
  (`internal/uikit/tui_dialogs.go`) next to whichever filter is currently
  active, in the `f` filter dialog — chosen from outside both the
  five-outcome palette and the section-color set above so it doesn't read
  as either; happens to share `aqua`'s own name with the Output section
  purely by reuse, not by any shared meaning between the two.
- **`WarningColor`** (hotpink) — the Output tab's Warnings section.
- **`yellow`** — the Output tab's Items section (loop-item bullets) —
  note this is the same tag name as the Changed outcome, but an
  unrelated context; no shared meaning intended.
- **`red`** — the Output tab's Error section (stderr) — same tag name
  as the Failed outcome, again an unrelated but not-coincidentally
  similar context (both mean "something's wrong").
- **`orange`** — key-highlighting in the Task definition tab's YAML
  source (`ColorizeYAML`, `key:` portions only); also the host-verb
  view's own section headers (host-vars file, per-play sections).

The Resolved/Docs/Details tabs are plain, uncolored text — each is its
own tab now (see Tabbed UI.md) rather than a stacked section needing a
heading color to separate it from its neighbors.

## Async fetch/render errors — `[red::b]Error[-::-]`

A shared, unlabeled convention (no constant of its own — each call site
spells out the literal tag) for a background operation that failed
outright, distinct from both the Failed outcome and the Output tab's own
Error section above: a bold red "Error" heading followed by the error
text itself, replacing whatever the view would otherwise show.

- `internal/host/host.go`'s `fetch` helper — shared by all five of the
  host-verb view's tabs (Summary, Groups, Plays, host_vars, Everything),
  each populated by its own async goroutine; a failure in any one of them
  renders this in just that tab, independent of the other four.
- `internal/template/template.go` — the Source tab, if the template file
  itself can't be read; the Rendered tab, if the synchronous
  `ansible.builtin.template` re-render itself fails or returns its own
  `ErrMsg`.

## Diff tab colors (the output drill-down's own Diff tab)

A direct port of `ansible-core`'s own `--diff` callback coloring:

- **`green`** — `+`-prefixed lines, including the `+++` file header.
- **`red`** — `-`-prefixed lines, including `---`.
- **`teal`** — `@@` hunk headers.
- Unprefixed context lines carry no color tag at all.

Note this reuses `green`/`red`/`teal`'s tag *names* from the outcome
palette, but in a completely unrelated semantic context (added/removed
diff lines, not host outcomes) — no shared helper function, no
cross-reference between the two meanings in the code. (Not to be
confused with the *`tangsible diff` verb* below, a different feature
entirely that happens to share the word "diff.")

## The `tangsible diff` verb

Two entirely separate concerns: the verb's own **chrome** (a third
top/bottom-bar color, alongside navy and purple) and how its tree's
**content** marks what changed.

### Chrome: fuchsia

`internal/diff/diff.go`'s own `DiffChromeStyle` — `tcell.ColorWhite` on
`tcell.ColorFuchsia`, bold — colors the verb's own top/bottom bars,
fully implemented and live (not a proposal). Picked from the same
"unused base-16 ANSI slot" shortlist (`fuchsia`, `olive`, `lime`) the
navy-vs-purple chrome decision below already considered and passed
over — a third, unambiguously distinct chrome color for a third
"you are not looking at a live run" context (comparing two saved runs,
neither of which is "the current session," unlike revisit's single
historical run). `tangsible diff` has no two-pane/split mode of its
own, so there's no divider-color question here the way there is for
navy/purple. (`olive` has since been claimed too, by `--check` mode
below — only `lime` is still unused.)

### Content coloring: reuse, plus attribute flags

`tangsible diff`'s own tree/host rows are **not** recolored into a
fourth palette - they reuse the identical five outcome colors via
`DiffColorTag(o Outcome, flag string) string`, which just calls
`uikit.ColorTag(o)` and, if `flag` is non-empty, appends `"::" + flag` -
layering a **style attribute**, not a different color, on top:
underline/strikethrough mark which hosts differ between the two runs
being compared. Deliberate: color still means exactly what it means
everywhere else in the app (that host's own outcome); the *diff itself*
is conveyed by a completely orthogonal visual channel (attributes), so
the two kinds of information never compete for the same signal.

## Run-completion status line

`uikit.StatusRowText`, the line appended below the tree once a run
finishes:

- **`green`** — genuine success (`exitCode == 0`): "Playbook completed successfully."
- **`yellow`** — benign unreachable-host case (`exitCode == 4` and at
  least one host really was unreachable): "Playbook completed - one or
  more hosts were unreachable."
- **`red`** — user-interrupted (`exitCode == 99`), and the generic
  failure default case: "Playbook failed (exit code N)."

## Revisit list: run-status colors

`internal/revisit/revisit.go`'s `RevisitStatusColor` (backing
`classifyExit`'s own label, see design-docs/Revisit.md's own bitmask
write-up) colors each entry in the `tangsible revisit` list by how that
*run as a whole* ended, not by any single host's outcome - a genuinely
different granularity from the five-outcome palette above, even though
it reuses two of the same tag names:

- **`green`** — "Success."
- **`red`** — "Failed" / "Host failed."
- **`gray`** — "Aborted" (the user's own q/Ctrl-C, Rerun.md) - the same
  "neither good nor bad, just moot" reading `gray` already carries for
  the annotation cases above, extended here to a whole run rather than
  one note.

## Tab bar

`uikit.TabbedPane`'s own tab-bar row (`renderHeader`) tracks whichever
chrome color the surrounding session is actually using, via
`SetHeaderStyle(style, colorName)` — `navy` by default (matching
`BarStyle`, for callers that never call it: the host-verb view, the
template page), explicitly set to `purple`/`olive`/`fuchsia` by
`internal/session/tui.go` (revisit/`--check`) and `internal/diff/diff.go`
(the verb's own chrome) respectively. Not merely proposed — fixed live
after a report that a `--check` session's two-pane divider correctly
showed olive while the tab bar right next to it stayed navy; the same
gap existed, previously unnoticed, for the diff verb's own fuchsia chrome
and was fixed at the same time.

- **Active tab** — `[<colorName>:white:B]`, chrome-colored text on a
  white background: fully inverted from the app's own chrome, and
  deliberately spelled out with explicit colors rather than a `[::R]`
  reverse-video tag.
- **Inactive tabs** — `[white:<colorName>:-]`, matching whichever chrome
  style the session's other bars are currently using.

## Chrome: navy

- **`uikit.BarStyle`** — `tcell.ColorWhite` on `tcell.ColorNavy`, the
  real `tcell.Style` applied to the top and bottom bars for a live or
  finished (not historical, not comparison) session.
- **The two-pane drill-down's vertical divider** — `tcell.ColorNavy`
  background, explicitly matching `BarStyle` rather than a brighter
  named "blue," so the divider reads as the same chrome rather than a
  clashing second shade.
- **`tangsible host`/`tangsible hosts` and `tangsible template`** — both
  standalone verbs (no revisit/`--check`/comparison mode of their own)
  apply `uikit.BarStyle` directly to their own top/bottom bars, always,
  with no chrome-switching logic at all - the same navy every other
  verb's default state uses, just never anything else for these two.

## Chrome: purple (design-docs/Revisit.md)

`uikit.ReplayBarStyle` — `tcell.ColorWhite` text on `tcell.ColorPurple`
— is the chrome color for a revisited (historical) run: the top bar,
the bottom bar, and the two-pane drill-down's vertical divider all
switch from `BarStyle`'s navy to this instead, for the duration of
viewing saved data, so the "this isn't live" distinction is visible
everywhere the chrome itself appears, not just in one spot. Fully
implemented and wired live via `revisitActive` (`internal/session/tui.go`),
not merely proposed. Reverts to ordinary navy the moment a real re-run
is kicked off from within a revisited session — from that point on
there's a live process again, not archived data.

`purple` (not a hex literal) for the same reason `maroon` was chosen
over `brown`/`darkred` elsewhere in this doc: it's one of the fixed
base-16 ANSI palette slots, so it renders reliably across terminal
themes rather than being approximated from RGB. Picked over the other
unused base-16 names (`fuchsia`, `olive`, `lime`) for being close enough
in tone to navy to still read as "chrome," while being unambiguously a
different hue at a glance — the same "clearly chrome, clearly not the
same chrome" balance the divider's own navy-vs-brighter-blue reasoning
above already goes for. (`fuchsia` was still unused at the time this
choice was made; the diff verb's own chrome, above, has since claimed
it for a third, sibling meaning.)

## Chrome: olive (`--check` mode)

`uikit.CheckBarStyle` — `tcell.ColorWhite` text on `tcell.ColorOlive`
— is the chrome for a session whose current generation was invoked
with `ansible-playbook --check`: the top bar, the bottom bar, the
output drill-down's own top/bottom bars, and the two-pane drill-down's
vertical divider all switch from `BarStyle`'s navy to this instead,
for the entire lifetime of the session (`checkMode` is computed once
in `internal/session/tui.go`, from whether `--check`/`-C` appears in
`passthroughArgs` — see `config.HasCheckFlag` — and never re-derived,
since a rerun always carries the original invocation's passthrough
args forward unedited; the re-run dialog has no way to toggle it).
Fully implemented and live, not merely proposed. Unlike revisit's
purple, this can combine with a real, still-running generation — a
`--check` run still genuinely executes `ansible-playbook`, just mostly
without effect — so nothing here reverts partway through a session the
way `revisitActive` does once a real rerun starts.

Independent of, and takes lower precedence than, revisit's purple: if
a *revisited* run happens to have been a `--check` one, the chrome
still shows purple (revisit already communicates "this isn't live" on
its own, whereas a stale dry-run distinction reads as lower-stakes) —
but `currentMainBottomBarText` still appends the textual `[CHECK MODE
- dry run]` note regardless of `revisitActive`, since that's the one
channel that also reaches a NO_COLOR/monochrome terminal (the same
"color isn't the only channel" concern design-docs/Morehosts.md
already raised for the collapsed-row summary).

`olive` (not a hex literal), picked from the same still-unused
base-16 shortlist (`olive`, `lime`) the purple/fuchsia choices above
already worked from — leaving `lime` as the only base-16 chrome color
still unclaimed.

## In-tab search (design-docs/Search.md)

Three related but distinct pieces, all introduced together, all
deliberately reusing the *same* fixed pair of colors so the whole
feature reads as one consistent "mode":

- **The composing/active search bar** — `uikit.SearchBarStyle`, black
  on yellow, bold. Replaces the normal chrome-colored footer bar (navy,
  purple, or fuchsia, whichever the surrounding session currently uses)
  for as long as a search prompt is open or a search is active, on all
  four search-enabled surfaces (the main drill-down, `tangsible diff`'s
  drill-down, `tangsible hosts`/`tangsible host`, `tangsible template`).
  The composing `InputField` itself (`tabSearchInput` in
  `internal/session/tui.go`/`internal/diff/diff.go`, `TabSearchBar.input`
  in `internal/uikit/tabsearchbar.go` for the other two surfaces) is
  styled identically by hand at each of its three construction sites
  (`SetLabelStyle`/`SetFieldBackgroundColor`/`SetFieldTextColor`/
  `SetBackgroundColor`, all black-on-yellow) rather than sharing
  `SearchBarStyle` directly, since `InputField` styling is per-channel
  method calls, not a single `tcell.Style` value the way a plain
  `TextView`'s bar text is.
- **Non-current matches** — `uikit.searchMatchFg`/`searchMatchBg` =
  black on yellow (`internal/uikit/search.go`) - matches the search bar's
  own color exactly, on purpose, so a highlighted match visually
  "belongs to" the same mode the bar announces.
- **The current match** — `searchCurrentMatchFg`/`searchCurrentMatchBg`
  = black on **orange**, a deliberately different hue from the other
  matches specifically so the one you're currently stepped to stands
  out from the rest, not just from the unhighlighted text around it.
  Implementation note, not a color choice: the tag actually written for
  this one region is the *pre-swapped* pair (background value in the
  foreground slot and vice versa), because `TextView.Highlight()`
  itself swaps whatever fg/bg a highlighted region already has - see
  `search.go`'s own `render()` doc comment for the full story (a real
  bug caught live: without the pre-swap, the current match rendered
  "orange text on black," not the intended solid orange block).

Both match colors are picked outside the five-outcome palette (yellow
is shared with the Changed outcome and the Output tab's Items section,
same "reused tag name, unrelated context" pattern already established
elsewhere in this doc; orange is shared with the YAML key-highlighting/
host-verb section-header meaning above) - no new hues introduced, only
new combinations of existing ones.

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
own hand-picked hex literals (`PureBlack`, `ProgressFillColor`) — and so
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
| Top/bottom chrome bars, live session (`BarStyle`) | white on navy | Yes |
| Top/bottom chrome bars, revisited session (`ReplayBarStyle`) | white on purple | Yes |
| Top/bottom chrome bars, `tangsible diff` (`DiffChromeStyle`) | white on fuchsia | Yes |
| Top/bottom chrome bars, `--check` session (`CheckBarStyle`) | white on olive | Yes |
| Progress-sweep text (top bar) | white | Yes |
| Recap heading ("Summary") | white | Yes |
| Inactive tab | white on session chrome (navy/purple/olive/fuchsia) | Yes |
| Host-verb view's plain hostname row | white | Yes |
| Selected-row text (every selected row, app-wide) | PureBlack (`#1a1a1a`) | No |
| Selected-row background, label portion | lightgray | No |
| Progress-sweep fill background (top bar) | ProgressFillColor (`#146414`) | No |
| Warning glyph (task/host rows); Warnings section; "warnings" recap category | WarningColor = hotpink | No |
| Output tab's Output section | aqua | Yes |
| Filter dialog: current-selection marker | aqua | Yes |
| Output tab's Items section | yellow | Yes |
| Output tab's Error section | red | Yes |
| Async fetch/render error heading (`host`/`hosts`, `template`) | red | Yes |
| Host-verb view's Summary tab: probe Unreachable/Failed heading | maroon/red | Yes |
| Task-definition tab's YAML key highlighting | orange | No |
| Host-verb view's section headers | orange | No |
| Diff tab: added lines | green | Yes |
| Diff tab: removed lines | red | Yes |
| Diff tab: hunk headers | teal | Yes |
| `tangsible diff` verb: unmatched-task note | yellow | Yes |
| Status line: genuine success | green | Yes |
| Status line: benign unreachable-host completion | yellow | Yes |
| Status line: user-interrupted / generic failure | red | Yes |
| Revisit list: run succeeded | green | Yes |
| Revisit list: run failed / host(s) failed | red | Yes |
| Revisit list: run aborted (user interrupt) | gray | Yes |
| Active tab | session chrome (navy/purple/olive/fuchsia) on white | Yes |
| Two-pane divider | session chrome (navy/purple/olive) | Yes |
| Search bar (composing or active) | black on yellow | Yes (bg); black is No (`PureBlack`-adjacent risk not taken here - see open question below) |
| Search match (not current) | black on yellow | Yes/Yes |
| Search match (current) | black on orange | Yes/No |

Note on the last three rows' "black": the in-tab search feature uses
plain named `"black"`, not `PureBlack`, in every place it needs a black
foreground - unlike the selected-row convention elsewhere in this
document, which deliberately avoids named black for exactly the
remapping risk described under "The three hand-picked exceptions"
above. Not yet hit in practice, but worth flagging as a latent
inconsistency rather than silently leaving it unexamined: an update to
this feature that also switches these to `PureBlack` would make the
app's own "avoid named black" rule apply uniformly.

## Thoughts about colors and how to reduce

Random thoughts first, basis for discussion, don't implement yet...

chrome_divier -> remove, divider should always be the same color as
current chrome_..._bg
(already true as of the `--check` chrome work: `splitDivider` always
tracks `chromeBg`/`liveChromeBg` now, in every one of navy/purple/olive -
see the Chrome sections above)

PureBlack seems to be hardly different to #000000 so probably not necessary
to differentiate.


