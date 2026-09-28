# Colors currently in use

Every color the TUI uses. Compiled from the current code - update this
alongside any future color change rather than letting it drift. The
quick reference table at the end is the fast lookup; everything above it
is just the handful of choices that need more than one line to explain.
The same color name is deliberately reused across unrelated contexts
throughout the app (e.g. `green`/`red`/`yellow` mean an outcome in one
place and something else entirely in another) - that's normal here, not
a sign of drift, as long as the two meanings never appear together where
they could be confused for each other.

## The five outcome colors

`uikit.ColorTag(o Outcome)`, the single source of truth every other
outcome-coloring call site (task/host rows, the recap, `tangsible diff`'s
own rows) calls rather than hardcoding a name again:

| Outcome     | Color  |
|-------------|--------|
| OK          | green  |
| Changed     | yellow |
| Skipped     | teal   |
| Failed      | red    |
| Unreachable | maroon |

`teal` is the closest named match for ANSI cyan; `maroon` (not
`brown`/`darkred`) stays a base-16 ANSI slot like the other four, so it
survives terminal-theme remapping the same way they do.

## Why a few colors are hex literals instead of names

A value that exactly matches a base-16 slot's nominal RGB gets resolved
back to that (remappable) slot even given as hex - these deliberately
don't:

- `uikit.PureBlack = "#1a1a1a"` - every selected row's text, app-wide.
- `uikit.ProgressFillColor = "#146414"` - darker than nominal green so
  white text stays readable on top of it.
- `uikit.WarningColor = "hotpink"` - the one exception that's a plain
  name, since "hotpink" isn't near any base-16 slot's RGB to begin with.

`orange`, `lightgray`, `aqua`, `lightsteelblue`, and `silver` are also
non-base-16 named colors (extended/X11, resolved by RGB) - same
non-remappable guarantee, just no hand-picked hex needed.

## Notes worth keeping

- **Gray** carries several unrelated meanings depending on context (not
  reported yet, zero-count de-emphasis, a moot annotation/outcome, a
  ping still in flight) - see the table for which is which.
- **In-tab search**'s current-match highlight writes its fg/bg
  pre-swapped, because `TextView.Highlight()` itself swaps whatever a
  region already has - without the pre-swap the current match rendered
  inverted.
- **Halfblock** (▌) isn't a color - it's how two adjacent hostnames'
  outcome colors blend at their boundary on a selected row (left host's
  color as foreground, right's as background). Selected rows only.
- The search feature's own black is plain `"black"`, not `PureBlack` -
  the one place that skips the remapping safeguard above. Not a reported
  problem, just a latent inconsistency.
- The two-pane divider and inactive tab bar always track whichever
  chrome color the surrounding session is currently using (navy/purple/
  olive/fuchsia) - never hardcoded separately.
- The green-key/lightsteelblue-value highlighters (`uikit.HighlightYAMLKeys`/
  `HighlightJSONKeys`/`HighlightKeyValueLines`) are one shared
  implementation in `internal/uikit`, used by both `host`/`hosts` and the
  run drill-down's own tabs - not two copies that happen to agree.

## Quick reference

"ANSI" means one of tcell's fixed 16 base ECMA/XTerm palette slots (the
ones a terminal theme can remap). "No" means an explicit RGB value
instead (a named-but-still-RGB color, or one of this app's own hex
literals) - always renders the same regardless of the terminal's theme.

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
| Chrome bars: live session | white on navy | Yes |
| Chrome bars: revisited session | white on purple | Yes |
| Chrome bars: `tangsible diff` | white on fuchsia | Yes |
| Chrome bars: `--check` session | white on olive | Yes |
| Progress-sweep text (top bar) | white | Yes |
| Recap heading ("Summary") | white | Yes |
| Inactive tab | white on session chrome | Yes |
| Selected-row text (app-wide) | PureBlack (`#1a1a1a`) | No |
| Selected-row background, label portion | lightgray | No |
| Progress-sweep fill background | ProgressFillColor (`#146414`) | No |
| Warning glyph/section; recap "warnings" category | WarningColor = hotpink | No |
| Ignored failure (tree rows, recap, drill-down Task tab Status line) | IgnoredColor = orange | No |
| Filter dialog: current-selection marker | aqua | Yes |
| Output tab's Error section | red | Yes |
| Async fetch/render error heading (`host`/`hosts`, `template`) | red | Yes |
| Diff tab: added / removed lines / hunk headers | green / red / teal | Yes |
| `tangsible diff` verb: unmatched-task note | yellow | Yes |
| Status line: success / benign-unreachable / failure | green / yellow / red | Yes |
| Revisit list: success / failed / aborted | green / red / gray | Yes |
| Active tab | session chrome on white | Yes |
| Search bar / non-current match | black on yellow | Yes (bg) |
| Search: current match | black on orange | No (bg) |
| `host`/`hosts`: every tab's own keys/headings | green | Yes |
| `host`/`hosts`: every tab's own values | lightsteelblue | No |
| `host`/`hosts`: list's connectivity dot (pending / reachable / down) | gray / green / red | Yes |
| `host`/`hosts`: Summary's live Unreachable/Failed heading | maroon / red | Yes |
| `host`/`hosts`: Summary's "Host unavailable." skip note | maroon | Yes |
| Drill-down: Task/Output/Task definition/Resolved/Details tabs' own keys/headings | green | Yes |
| Drill-down: same tabs' own values (incl. Output's own "Label: value" lines, Items bullets) | lightsteelblue | No |
