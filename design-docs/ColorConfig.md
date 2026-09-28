# Configurable colors

## Situation

Every color the TUI uses is a named Go var (design-docs/Colors.md) - one
`uikit.*Color` var per distinct semantic meaning (`internal/uikit/colors.go`,
the great majority of them), `diff.DiffChromeColor` (`internal/diff`, its
own package's fourth chrome variant), and four unexported `search*Color`
vars (`internal/uikit/search.go`). All 34 are overridable from
`config.toml`'s `[colors]` table, cascading project → global → system
exactly like every `[general]` key (`internal/config.MergedSettingsConfig`
- see design-docs/ConfigOptions.md), accepting either a name from tview's
own recognized color set or a `#rrggbb` hex literal.

**Implemented.** This doc is now a reference for how it works, not a
proposal - kept in the same shape (situation, shape, key list, open
questions) it had while under review, since that history is still useful
context for anything left open below.

## Shape

```toml
[colors]
outcome_ok = "green"
outcome_failed = "#ff0000"
chrome_live = "navy"
```

Each value is **one color**, either:

- a name from tview's own recognized set (the same names already used
  throughout this app's own source - `"green"`, `"orange"`, `"maroon"`,
  etc.), or
- a `#rrggbb` hex literal (not `#rgb` - `tcell.GetColor`, the resolver
  this uses throughout, only ever matches a 7-character `#rrggbb` string;
  a 4-character short form falls through to "invalid," same as any other
  unrecognized value).

Both forms already work verbatim inside tview's own `[color]...[-]` tag
syntax (confirmed directly against tview's tag parser) and via
`tcell.GetColor(value)` for the handful of places that need an actual
`tcell.Color` rather than a tag string (see "Two resolution paths"
below) - so the config layer doesn't need to distinguish the two forms
itself; whatever the user types is just handed onward.

## Key naming rule

**Every key is the owning var's own name, snake_cased, with the trailing
`Color` dropped** - e.g. `uikit.OutcomeOKColor` → `outcome_ok`,
`uikit.SelectedRowBackgroundColor` → `selected_row_background`,
`diff.DiffChromeColor` → `diff_chrome`. No separate key list is kept
here: design-docs/Colors.md's own Quick reference table (and its
"Remaining constants, grouped by value" section) is the single source of
truth for what each var means and what it's currently set to. The 34
keys the rule produces:

```
outcome_ok, outcome_changed, outcome_skipped, outcome_failed, outcome_unreachable,
nothing_reported, annotation,
task_title, play_title,
progress_text, tab_text,
chrome_live, chrome_revisit, chrome_check, diff_chrome,
selected_row_text, selected_row_background,
progress_fill, warning, ignored,
selection,
async_error,
diff_added, diff_removed, diff_hunk, diff_unmatched_task,
status_benign,
key_highlight, value_highlight,
summary_failed,
search_match_foreground, search_match_background,
search_current_match_foreground, search_current_match_background
```

One thing this mechanical rule does *not* cover, a deliberate, still-open
gap: **the chrome bars' own shared foreground.** `BarStyle`/
`ReplayBarStyle`/`CheckBarStyle`/`DiffChromeStyle` (`tui_style.go`/
`diff.go`) each hardcode a literal `"white"` foreground - there's no
`uikit.ChromeTextColor` (or similar) backing it, unlike every background
half of the same four bars (`chrome_live`/`chrome_revisit`/
`chrome_check`/`diff_chrome`, all real, configurable vars). Leaving this
hardcoded (not configurable) was the deliberate call, not an oversight -
see "Known gaps" below.

`SearchBarStyle`'s own black-on-yellow, by contrast, *is* fully covered:
it used to hardcode `tcell.ColorBlack`/`tcell.ColorYellow` directly
rather than reading `searchMatchForegroundColor`/
`searchMatchBackgroundColor` even though its own doc comment claimed
they matched - fixed (independently of this proposal, before it was
implemented) to actually build from those vars via `tcell.GetColor`, so
`search_match_foreground`/`search_match_background` now genuinely cover
this bar too, no separate key needed.

## Implementation

### Config struct

`ColorsConfig` (`internal/config/colorsconfig.go`) is one `*string`
field per key above, nested directly as `SettingsConfig.Colors` -
alongside `General`, in the same TOML file, not a separate type read
separately:

```go
type SettingsConfig struct {
    General struct { ... } `toml:"general"`
    Colors  ColorsConfig   `toml:"colors"`
}
```

`*string`, not `string` - same reasoning as `General.TwoPaneLayout`/
`General.Color`'s own `*bool` fields: a zero-value `string` ("") can't
be told apart from "key genuinely absent," so an unset key needs its
own distinguishable nil state to correctly fall through to the
hardcoded default rather than resolving to an empty (invalid) color.

`mergeColors` (same file) is `MergedSettingsConfig`'s own per-`[colors]`-field
cascade - one `firstNonNil(project.X, global.X, system.X)` call per
field, reusing the exact same helper `General`'s own merge already uses.
Not a separate `MergedColorsConfig()` function: `MergedSettingsConfig()`
returns one `SettingsConfig` with both `General` and `Colors` already
merged, so a caller needing both (there's exactly one, `main.go`) reads
config once, not twice.

### Two resolution paths, matching how each color is actually consumed

Most of the 34 colors above are only ever used as **inline tag
strings** (`fmt.Sprintf("[%s]...[-]", colorName)`) - for these, the
configured string (or the hardcoded default, if unset) is substituted
directly wherever the literal sat before this existed. No conversion
needed at all: tview's own tag parser already accepts both a plain name
and a `#rrggbb` hex form in that position, so whatever the user typed
just flows through unchanged.

A handful are used to build real `tcell.Style` values instead
(`BarStyle`, `ReplayBarStyle`, `CheckBarStyle`, `DiffChromeStyle`,
`SearchBarStyle`) - these resolve the same configured-or-default string
via `tcell.GetColor(value)`, which handles both forms uniformly
(confirmed directly: `GetColor` resolves both plain names and hex
strings to a real `tcell.Color`, unlike tview's own tag-parser
color-name lookup, which only checks a plain name map and handles hex
through a separate code path). `uikit.buildChromeStyle(fg, bg string)
tcell.Style` is the one place that actually builds these four -
`BarStyle`/`ReplayBarStyle`/`CheckBarStyle`'s own package-init
expressions already call it (so a fresh process with no config at all
still gets today's defaults, same as before this existed), and
`ApplyColorConfig` (below) calls it again with the resolved values once
config is read - the two call sites use the identical formula, so
there's no way for them to silently drift apart the way `SearchBarStyle`
already had once.

### Where the resolved palette lives

A package-level, write-once palette in `internal/uikit` (and one more
var in `internal/diff`) - **Option A** from this doc's own original
proposal, chosen over threading a `*Palette` explicitly through every
color-emitting function (`ColorTag`, `TaskLabel`, `BuildOutputTabs`,
`RevisitStatusColor`, ...) for the scale difference: none of those
~15-20 functions across `uikit`/`session`/`host`/`template`/`diff`/
`revisit` needed any signature change at all, since only the literal
each one already referenced moved (first from a literal to a `const`,
then from `const` to `var`).

`uikit.ApplyColorConfig(cfg config.ColorsConfig)` overwrites every var
this package owns (33 of the 34 keys) and rebuilds `BarStyle`/
`ReplayBarStyle`/`CheckBarStyle`/`SearchBarStyle` from the resolved
values. `diff.ApplyColorConfig(cfg config.ColorsConfig)` is `internal/diff`'s
own one-field equivalent for `DiffChromeColor`/`DiffChromeStyle` - a
second, separate function (not folded into `uikit.ApplyColorConfig`)
since `internal/uikit` can't reach into another package's own vars.
Both are called from `session.Main`, once, right after the `--version`/
`--dialog` early-exits and before any Verb-specific dispatch (template,
host, hosts, revisit, vault, run/rerun/role) - config is read once there
(`config.MergedSettingsConfig().Colors`, shared between both calls) and
applied before any of those Verbs gets a chance to construct a TUI.

### Validation

`uikit.ResolveColorOverride(key string, override *string, fallback
string) string` (exported so `diff.ApplyColorConfig` can reuse it for
its own one field, rather than a second copy of this logic) - `nil`
override means "absent everywhere in the cascade," returns `fallback`
unchanged; a non-nil override that `tcell.GetColor` resolves to anything
but `tcell.ColorDefault` is used as-is; anything else (a typo, an
unrecognized name, a malformed hex string) warns once to stderr by key
name and falls back to `fallback` - the same one-key-at-a-time tolerance
`ReadTOMLFile`'s own parse-failure path already has elsewhere in this
app: one bad value costs only that one key, never the whole config.

## Known gaps

- **The chrome bars' own shared "white" foreground** (see "Key naming
  rule" above) has no backing var, so it isn't configurable - the
  backgrounds (`chrome_live`/`chrome_revisit`/`chrome_check`/
  `diff_chrome`) already cover the visible "which mode am I in" signal
  these bars exist for; a changed foreground on top of an
  already-changed background would be a smaller, more marginal
  addition. Revisit if real demand shows up - it would need a new var
  purely to be configurable, unlike everything else in this file, which
  reused a var that already existed for an unrelated reason.
- **No hot-reload.** Every other setting in this app (`default_playbook`,
  `two_pane_layout`, `color`) is read once at startup, and there's no
  existing mechanism to watch `config.toml` for changes mid-session. A
  color change needs a restart, same as everything else configurable
  today.
- **Color values only, not attributes** (bold/underline/reverse) -
  things like the active tab's bold reverse-video or the diff verb's
  underline/strikethrough-for-changed-hosts are structural to what they
  communicate, not a stylistic preference the way color choice is.
  Deliberately out of scope.

## Decisions made along the way

Kept for context - these were open questions while this was still a
proposal; each is settled now, and the reasoning is why the shape above
looks the way it does.

- **Everything at once, not tiered.** All 34 keys shipped in one pass
  rather than a first tier (the five outcome colors, three chrome-bar
  backgrounds, the selected-row pair) followed by a later extension -
  consistent with the same "every setting should behave the same way"
  call that resolved global-vs-project-local below.
- **The `gray`/success/failure look-alikes don't get their own keys.**
  `nothing_reported` covers "host hasn't reported," "zero-count
  de-emphasis," "ping still in flight," and "revisit list aborted" as
  one shared var/key; the revisit list's own success/failure colors
  reuse `outcome_ok`/`outcome_failed` directly rather than getting their
  own - see design-docs/Colors.md's own "Merged so far" list for the
  full reasoning behind each individual merge. `annotation` stays its
  own key despite an identical default to `nothing_reported`, for the
  opposite reason: a genuinely distinct meaning that just happens to
  share a value today.
- **fg/bg as two keys, not a combined `"fg:bg"` value.** Settled
  implicitly rather than decided outright: every genuine fg/bg pair left
  (`selected_row_text`/`selected_row_background`,
  `search_match_foreground`/`search_match_background`,
  `search_current_match_foreground`/`search_current_match_background`)
  was already backed by two separately-named vars by the time this was
  implemented, not one combined value - so "one key per var" already
  settled it. `tab_text` (the tab bar's fg/bg swap) is the one case that
  stayed a single var/key, because - unlike these pairs - its two roles
  were never independently meaningful (see Colors.md's own note on why
  that one merged).
- **Global vs. project-local: resolved for every `[general]`/`[colors]`
  key at once, not just for colors.** `general.color`/
  `general.two_pane_layout`/etc. used to be project-local only,
  deliberately - but a key behaving differently from its neighbor with
  no way for a user to discover why, from `config.toml` alone, turned
  out to be worse than whatever rationale existed for any one key
  staying project-local. See design-docs/ConfigOptions.md and
  `internal/config.MergedSettingsConfig`. `[colors]` inherited this for
  free once it landed - no special-casing needed.
