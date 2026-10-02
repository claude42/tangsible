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

## Every color is a named, configurable Go var

Every color listed in the Quick reference table below is backed by an
exported Go var (or, for the four chrome bars, a `tcell.Style` built from
one) - no bare string literals (`"green"`, `"maroon"`, ...) scattered at
call sites. Almost all of them live in `internal/uikit/colors.go`, one
var per genuinely distinct semantic meaning; `diff.DiffChromeColor`
(`internal/diff/diff.go`) is its own package's fourth chrome variant, and
search.go's own unexported `searchMatchForegroundColor`/
`searchMatchBackgroundColor`/`searchCurrentMatchForegroundColor`/
`searchCurrentMatchBackgroundColor` stay right next to the one thing that
reads them besides `colors.go`'s own `ApplyColorConfig`.

`var`, not `const`: this was a three-step plan (per design-docs/
ColorConfig.md), now complete - name every color as its own constant,
decide which of the resulting duplicate-valued constants were worth
collapsing (see "Merged so far" below), then make the surviving set
overridable from `config.toml`'s `[colors]` table (`uikit.ApplyColorConfig`/
`diff.ApplyColorConfig`, called once at startup - design-docs/
ColorConfig.md has the full mechanism). Merged so far:

- `uikit.GrayTag` renamed to `uikit.NothingReportedColor` and kept as the
  shared constant for both "host hasn't reported for this task yet" and
  recap's own "zero-count de-emphasis" - the two meanings were already
  reading identically ("nothing to show here").
- `uikit.RecapHeadingColor` removed outright; the recap's own "Summary"
  heading and the error-output block's own "Error output:" heading (its
  per-host summary rows are a separate case, see `uikit.StandardTextColor`
  below) started using `uikit.KeyHighlightColor` (green) instead of a
  dedicated white - the same green every other section heading in this
  app (drill-down/`host`/`hosts` tabs) already used at the time. This was
  a real, visible color change (white → green for those two headings),
  not just a rename. Since split again into its own `uikit.HeadingColor`
  (still green by default) once it became clear "a key's own label" and
  "a section heading" wanted independently overridable config keys - see
  the Quick reference table below for the current state.
- `uikit.AnnotationColor` (source location, fact-cache note,
  search-dialog hint) stays its own constant, deliberately not folded
  into `NothingReportedColor` even though the default value is identical
  - an annotation is present-but-secondary, not a "nothing yet"
    placeholder.
- `uikit.InactiveTabTextColor`/`uikit.ActiveTabBackground` merged into
  one constant, `uikit.TabTextColor` - unlike the look-alikes above,
  these weren't just coincidentally the same value: `TabbedPane.
  renderHeader` renders the active tab as chrome-color-on-this-color and
  the inactive tab as this-color-on-chrome-color, a literal fg/bg swap
  of the same two colors. There was never a code path where the two
  halves of that swap could sensibly diverge.
- `uikit.PingPendingColor`/`PingReachableColor`/`PingDownColor` removed
  outright; `host`/`hosts`' own connectivity dot now reuses
  `uikit.NothingReportedColor` (pending)/`uikit.OutcomeOKColor`
  (reachable)/`uikit.OutcomeFailedColor` (down) directly - a ping's own
  three states already map cleanly onto "nothing to show yet"/OK/Failed,
  so a dedicated ping palette added a name without adding a distinction.
  Kept `uikit.AsyncErrorColor` separate, though, on the same call: a
  fetch/render machinery failure (Tangsible's own Go code erroring out)
  isn't the same kind of thing as a task outcome, unlike a ping result.
- `uikit.RevisitSuccessColor`/`RevisitFailureColor`/`RevisitAbortedColor`
  removed outright; `classifyExit` now returns `uikit.OutcomeOKColor`/
  `uikit.OutcomeFailedColor`/`uikit.NothingReportedColor` directly - not
  an exact conceptual match (a past run's exit code isn't literally a
  `playbook.Outcome`), but close enough to read the same way, the same
  call made for the ping-dot colors above.
- `uikit.StatusSuccessColor`/`StatusFailureColor` removed outright;
  `StatusRowText`'s success/failure cases return `uikit.OutcomeOKColor`/
  `uikit.OutcomeFailedColor` directly, same reasoning as the revisit
  list above. `uikit.StatusBenignColor` (the "exit 4, but a real
  unreachable host" case) stays its own constant, deliberately not
  folded into `OutcomeChangedColor` even though both are `"yellow"` -
  "some hosts were unreachable" and "a task reported changed" are
  unrelated facts that just happen to share an attention-color today.
- `uikit.SummaryUnreachableColor`/`HostUnavailableColor` (`host`/`hosts`'
  Summary tab: a genuine live Unreachable result, and the "Host
  unavailable." note shown when the live gather is skipped entirely
  because the ping dot already found the host down) both removed;
  both call sites now use `uikit.OutcomeUnreachableColor` directly - a
  real failure and the skip-it-entirely placeholder read the same way as
  the tree's own Unreachable outcome, whether or not a live attempt
  actually happened this time. `uikit.SummaryFailedColor` (the sibling
  "genuinely Failed" case, right next to these two in the same function)
  stays its own constant for now - not asked about, not merged.
- `uikit.OutputErrorColor` (the drill-down Output tab's own Error
  section, i.e. a task's own stderr) removed; that call site now uses
  `uikit.OutcomeFailedColor` directly - this is genuinely a ran task's
  own Failed-outcome content. `uikit.AsyncErrorColor` (host/hosts'/
  template's own fetch/render failure heading) stays separate: that one
  is Tangsible's own tooling failing to get a result at all, not a
  `playbook.Outcome`.

## The five outcome colors

`uikit.ColorTag(o Outcome)`, the single source of truth every other
outcome-coloring call site (task/host rows, the recap, `tangsible diff`'s
own rows) calls rather than hardcoding a name again - its own switch now
reads from `uikit.OutcomeOKColor`/`OutcomeChangedColor`/
`OutcomeSkippedColor`/`OutcomeFailedColor`/`OutcomeUnreachableColor`
instead of a bare literal per case:

| Outcome     | Color  | Constant                    |
|-------------|--------|------------------------------|
| OK          | green  | `uikit.OutcomeOKColor`          |
| Changed     | yellow | `uikit.OutcomeChangedColor`     |
| Skipped     | teal   | `uikit.OutcomeSkippedColor`     |
| Failed      | red    | `uikit.OutcomeFailedColor`      |
| Unreachable | maroon | `uikit.OutcomeUnreachableColor` |

`teal` is the closest named match for ANSI cyan; `maroon` (not
`brown`/`darkred`) stays a base-16 ANSI slot like the other four, so it
survives terminal-theme remapping the same way they do.

## Why a few colors are hex literals instead of names

A value that exactly matches a base-16 slot's nominal RGB gets resolved
back to that (remappable) slot even given as hex - these deliberately
don't:

- `uikit.SelectedRowTextColor = "#1a1a1a"` - every selected row's text, app-wide.
- `uikit.ProgressFillColor = "#146414"` - darker than nominal green so
  white text stays readable on top of it.
- `uikit.WarningColor = "hotpink"` - the one exception that's a plain
  name, since "hotpink" isn't near any base-16 slot's RGB to begin with.

`orange` (`uikit.IgnoredColor`), `lightgray` (`uikit.SelectedRowBackgroundColor`),
`aqua` (`uikit.SelectionColor`), `lightsteelblue`
(`uikit.ValueHighlightColor`), and `silver` (`uikit.TaskTitleColor`,
`uikit.StandardTextColor`) are also non-base-16 named colors (extended/
X11, resolved by RGB) - same non-remappable guarantee, just no
hand-picked hex needed.

## Notes worth keeping

- **Gray** carries several unrelated meanings depending on context - not
  reported yet / zero-count de-emphasis / a ping still in flight / a
  revisit list's aborted run (all `uikit.NothingReportedColor`), and a
  supplementary annotation (`uikit.AnnotationColor`) - two constants
  sharing one default value today; see the table for which is which.
- **In-tab search**'s current-match highlight writes its fg/bg
  pre-swapped, because `TextView.Highlight()` itself swaps whatever a
  region already has - without the pre-swap the current match rendered
  inverted.
- **Halfblock** (▌) isn't a color - it's how two adjacent hostnames'
  outcome colors blend at their boundary on a selected row (left host's
  color as foreground, right's as background). Selected rows only.
- The search feature's own black is plain `"black"` (`searchMatchForegroundColor`/
  `searchCurrentMatchForegroundColor`), not `SelectedRowTextColor` - the one place that skips the
  remapping safeguard above. Not a reported problem, just a latent
  inconsistency, left as-is by this pass (a behavior question, not a
  missing-constant one).
- The two-pane divider and inactive tab bar always track whichever
  chrome color the surrounding session is currently using -
  `uikit.ChromeLiveColor` ("navy") / `uikit.ChromeRevisitColor`
  ("purple") / `uikit.ChromeCheckColor` ("olive") /
  `diff.DiffChromeColor` ("fuchsia") - never hardcoded separately.
- The green-key/lightsteelblue-value highlighters (`uikit.HighlightYAMLKeys`/
  `HighlightJSONKeys`/`HighlightKeyValueLines`) are one shared
  implementation in `internal/uikit`, used by both `host`/`hosts` and the
  run drill-down's own tabs - not two copies that happen to agree. Both
  colors are `uikit.KeyHighlightColor`/`uikit.ValueHighlightColor`.

## Quick reference

"ANSI" means one of tcell's fixed 16 base ECMA/XTerm palette slots (the
ones a terminal theme can remap). "No" means an explicit RGB value
instead (a named-but-still-RGB color, or one of this app's own hex
literals) - always renders the same regardless of the terminal's theme.

"Source" is where the color actually comes from in code - every row is
an exported Go var (or `tcell.Style`, for the four chrome bars) rather
than a bare string literal, all live in `internal/uikit` unless noted
otherwise, and every one is individually configurable via
design-docs/ColorConfig.md's own `[colors]` table (same key as the var's
own name, snake_cased, minus the trailing "Color"). A row that
previously listed several colors separated by `/` is one line per color,
each with its own var/key, so a config override can target exactly one
of them.

| Semantic meaning | Color name | Source | ANSI (base-16)? |
|---|---|---|---|
| Whole-TUI base background (every unstyled cell) | black | `uikit.BackgroundColor`, pushed into `tview.Styles.PrimitiveBackgroundColor` (`ApplyColorConfig`) | Yes |
| OK outcome | green | `uikit.OutcomeOKColor` (read by `ColorTag`) | Yes |
| Changed outcome | yellow | `uikit.OutcomeChangedColor` | Yes |
| Skipped outcome | teal | `uikit.OutcomeSkippedColor` | Yes |
| Failed outcome | red | `uikit.OutcomeFailedColor` | Yes |
| Unreachable outcome | maroon | `uikit.OutcomeUnreachableColor` | Yes |
| Host hasn't reported for this task yet | gray | `uikit.NothingReportedColor` | Yes |
| Zero-count de-emphasis (recap / Morehosts.md summary string) | gray | `uikit.NothingReportedColor` | Yes |
| Supplementary annotation (source location, fact-cache note, search-dialog hint) | gray | `uikit.AnnotationColor` | Yes |
| Task row title | silver | `uikit.TaskTitleColor` | Yes |
| Play row title | white | `uikit.PlayTitleColor` | Yes |
| Plain "just text" content: `host`/`hosts` list hostnames, revisit list timestamp/command-line, recap's per-host hostname+duration prefix; plus every text that would otherwise be tview's white - tab/view bodies (drill-down, diff, host tabs, the `o` view), untagged tree rows (error output, recap summary sentence), dialog titles/labels/plain text, help dialog descriptions | silver | `uikit.StandardTextColor` | Yes |
| Modal dialog borders: re-run form, search/filter dialogs, template's "Change host" dialog, `?` help dialog | silver | `uikit.DialogBorderColor` | Yes |
| Chrome bars: live session | white on navy | `uikit.BarStyle` | Yes |
| Chrome bars: revisited session | white on purple | `uikit.ReplayBarStyle` | Yes |
| Chrome bars: `tangsible diff` | white on fuchsia | `diff.DiffChromeStyle` | Yes |
| Chrome bars: `--check` session | white on olive | `uikit.CheckBarStyle` | Yes |
| Progress-sweep text (top bar) | white | `uikit.ProgressTextColor` | Yes |
| Inactive tab | white on session chrome | fg `uikit.TabTextColor`, bg `chromeColorName()` (`TabbedPane.renderHeader`) | Yes |
| Selected-row text (app-wide) | near-black (`#1a1a1a`) | `uikit.SelectedRowTextColor` | No |
| Selected-row background, label portion; completion drop-down's highlighted entry | lightgray | `uikit.SelectedRowBackgroundColor` | No |
| Progress-sweep fill background | ProgressFillColor (`#146414`) | `uikit.ProgressFillColor` | No |
| Warning glyph/section; recap "warnings" category | WarningColor = hotpink | `uikit.WarningColor` | No |
| Ignored failure (tree rows, recap, drill-down Task tab Status line) | IgnoredColor = orange | `uikit.IgnoredColor` | No |
| Filter dialog: current-selection marker | aqua | `uikit.SelectionColor` | Yes |
| Output tab's Error section | red | `uikit.OutcomeFailedColor` (same constant as "Failed outcome") | Yes |
| Async fetch/render error heading (`host`/`hosts`, `template`) | red | `uikit.AsyncErrorColor` | Yes |
| Diff tab: added lines | green | `uikit.DiffAddedColor` | Yes |
| Diff tab: removed lines | red | `uikit.DiffRemovedColor` | Yes |
| Diff tab: hunk headers | teal | `uikit.DiffHunkColor` | Yes |
| `tangsible diff` verb: unmatched-task note | yellow | `uikit.DiffUnmatchedTaskColor` | Yes |
| Status line: success | green | `uikit.OutcomeOKColor` (same constant as "OK outcome") | Yes |
| Status line: benign-unreachable | yellow | `uikit.StatusBenignColor` | Yes |
| Status line: failure | red | `uikit.OutcomeFailedColor` (same constant as "Failed outcome") | Yes |
| Revisit list: success | green | `uikit.OutcomeOKColor` (same constant as "OK outcome") | Yes |
| Revisit list: failed | red | `uikit.OutcomeFailedColor` (same constant as "Failed outcome") | Yes |
| Revisit list: aborted | gray | `uikit.NothingReportedColor` (same constant as "Host hasn't reported...") | Yes |
| Active tab | session chrome on white | fg `chromeColorName()`, bg `uikit.TabTextColor` (same constant as "Inactive tab", swapped - `TabbedPane.renderHeader`) | Yes |
| Search bar / non-current match | black on yellow | `searchMatchForegroundColor`/`searchMatchBackgroundColor` (`internal/uikit/search.go`, unexported) | Yes (bg) |
| Search: current match | black on orange | `searchCurrentMatchForegroundColor`/`searchCurrentMatchBackgroundColor` (`internal/uikit/search.go`, unexported) | No (bg) |
| `host`/`hosts`: every tab's own "Label: value" keys; `?` help dialog's key column | green | `uikit.KeyHighlightColor` | Yes |
| `host`/`hosts`: every tab's own values | lightsteelblue | `uikit.ValueHighlightColor` | No |
| `host`/`hosts`: every tab's own section headings | green | `uikit.HeadingColor` | Yes |
| `host`/`hosts`: connectivity dot - pending | gray | `uikit.NothingReportedColor` (same constant as "Host hasn't reported...") | Yes |
| `host`/`hosts`: connectivity dot - reachable | green | `uikit.OutcomeOKColor` (same constant as "OK outcome") | Yes |
| `host`/`hosts`: connectivity dot - down | red | `uikit.OutcomeFailedColor` (same constant as "Failed outcome") | Yes |
| `host`/`hosts`: Summary's live Unreachable heading | maroon | `uikit.OutcomeUnreachableColor` (same constant as "Unreachable outcome") | Yes |
| `host`/`hosts`: Summary's live Failed heading | red | `uikit.SummaryFailedColor` | Yes |
| `host`/`hosts`: Summary's "Host unavailable." skip note | maroon | `uikit.OutcomeUnreachableColor` (same constant as "Unreachable outcome") | Yes |
| Drill-down: Task/Details tabs' own "Label: value" keys (Role/Task/Action/Host/Status, Output's own "Label: value" lines) | green | `uikit.KeyHighlightColor` (same constant as `host`/`hosts` - shared implementation) | Yes |
| Drill-down: same tabs' own values (incl. Output's own "Label: value" lines, Items bullets) | lightsteelblue | `uikit.ValueHighlightColor` (same constant as `host`/`hosts`) | No |
| Drill-down: Output/Items section headings; recap's own "Summary" heading; error-output heading; host.go's own section headings; `?` help dialog's section titles | green | `uikit.HeadingColor` (same constant as `host`/`hosts` - shared implementation) | Yes |
| Chrome color's own plain tag-name equivalent: live/revisit/check | navy / purple / olive | `uikit.ChromeLiveColor` / `ChromeRevisitColor` / `ChromeCheckColor` | Yes |

## Remaining vars, grouped by value

Every `uikit.*Color`/`diff.DiffChromeColor` var still standing, grouped
by today's default value - the fastest way to see which groups still
hold more than one distinct label (candidates for a future merge)
versus which are already down to one. Not counted here: the `tcell.Style`
chrome vars (`BarStyle`/`ReplayBarStyle`/`CheckBarStyle`/
`diff.DiffChromeStyle`/`SearchBarStyle` - each built from one of the
vars below, not an independent value of its own), `uikit.HalfBlock` (not
a color), and any already-merged var (see "Merged so far" above) - this
is the surviving set only. Every one of these is also its own
`config.toml` `[colors]` key - see design-docs/ColorConfig.md.

| Value | Constants sharing it |
|---|---|
| `black` | `BackgroundColor` |
| `green` | `OutcomeOKColor`, `DiffAddedColor`, `KeyHighlightColor`, `HeadingColor` |
| `yellow` | `OutcomeChangedColor`, `DiffUnmatchedTaskColor`, `StatusBenignColor` |
| `teal` | `OutcomeSkippedColor`, `DiffHunkColor` |
| `red` | `OutcomeFailedColor`, `AsyncErrorColor`, `DiffRemovedColor`, `SummaryFailedColor` |
| `maroon` | `OutcomeUnreachableColor` |
| `gray` | `NothingReportedColor`, `AnnotationColor` |
| `silver` | `TaskTitleColor`, `StandardTextColor`, `DialogBorderColor` |
| `white` | `PlayTitleColor`, `ProgressTextColor`, `TabTextColor` |
| `lightgray` | `SelectedRowBackgroundColor` |
| `lightsteelblue` | `ValueHighlightColor` |
| `aqua` | `SelectionColor` |
| `navy` | `ChromeLiveColor` |
| `purple` | `ChromeRevisitColor` |
| `olive` | `ChromeCheckColor` |
| `fuchsia` | `diff.DiffChromeColor` (own package) |
| `#1a1a1a` | `SelectedRowTextColor` |
| `#146414` | `ProgressFillColor` |
| `hotpink` | `WarningColor` |
| `orange` | `IgnoredColor` |
| `black` (unexported, `search.go`) | `searchMatchForegroundColor`, `searchCurrentMatchForegroundColor` |
| `yellow`/`orange` (unexported bg roles, `search.go`) | `searchMatchBackgroundColor`, `searchCurrentMatchBackgroundColor` - not folded into the exported `yellow`/`orange` rows above since these are background roles on a fixed black-text search highlight, a different kind of use than any row above |

Four value-groups still hold more than one label: `green` (3),
`yellow` (3), `teal` (2), `red` (4), and `gray` (2) - everything else is
already a singleton (nothing left in that value to merge into it) or has
an already-documented reason to stay split (see "Merged so far" and the
per-constant doc comments in `internal/uikit/colors.go` for the ones
already considered and kept apart).
