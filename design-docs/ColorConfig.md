# Configurable colors

## Situation

Every color the TUI uses is a named Go var (design-docs/Colors.md) - one
`uikit.*Color` var per distinct semantic meaning (`internal/uikit/colors.go`,
the great majority of them), `diff.DiffChromeColor` (`internal/diff`, its
own package's fourth chrome variant), and four unexported `search*Color`
vars (`internal/uikit/search.go`), plus `uikit.BackgroundColor` - the
whole TUI's own base background, pushed into
`tview.Styles.PrimitiveBackgroundColor` rather than read per-row like
the other 39. All 40 are overridable from `config.toml`'s `[colors]`
table, cascading project → global → system
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
truth for what each var means and what it's currently set to. The 40
keys the rule produces:

```
background,
outcome_ok, outcome_changed, outcome_skipped, outcome_failed, outcome_unreachable,
nothing_reported, annotation,
task_title, play_title, standard_text, dialog_border,
control_text, control_background,
progress_text, tab_text,
chrome_live, chrome_revisit, chrome_check, diff_chrome,
selected_row_text, selected_row_background,
progress_fill, warning, ignored,
selection,
async_error,
diff_added, diff_removed, diff_hunk, diff_unmatched_task,
status_benign,
key_highlight, value_highlight, heading,
summary_failed,
search_match_foreground, search_match_background,
search_current_match_foreground, search_current_match_background
```

`background` is different in kind from the other 39: every one of those
is read per-row/per-element by whatever function renders that specific
thing, but nothing reads `uikit.BackgroundColor` directly - it's the
whole TUI's own base background (every unstyled cell), and the only key
whose resolved value gets pushed into `tview.Styles.PrimitiveBackgroundColor`
(`ApplyColorConfig`) rather than just sitting in a var for row-composition
code to read. tview's own hardcoded default there is `tcell.ColorBlack`,
never overridden by this app before this key existed - so the background
was always black regardless of `color_scheme`, independent of every
other color choice.

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

### Four resolution paths, matching how each color is actually consumed

Most of the 39 colors above are only ever used as **inline tag
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

One key, `background`, takes a third path entirely: nothing in this app
reads `uikit.BackgroundColor` per-row the way the other 39 are read -
`ApplyColorConfig` instead pushes its resolved value straight into
`tview.Styles.PrimitiveBackgroundColor` via `tcell.GetColor`, the one
global tview setting every `tview.Box` (essentially every primitive)
reads at construction and paints across its own rect as its own default
background. Without this, `background` would just be a var nothing
consulted - the same class of gap `SearchBarStyle` had before it was
fixed, caught here before shipping rather than after.

`dialog_border` (`uikit.DialogBorderColor`) takes a fourth path: every
modal dialog (the re-run form, the search/filter dialogs, template's
"Change host" dialog) previously had no configurable border color at
all, just tview's own hardcoded `tview.Styles.BorderColor` default
(white) - the same kind of gap `background` had before it existed. Each
of the four dialogs calls `tcell.GetColor(uikit.DialogBorderColor)`
directly at its own `Box.SetBorderColor` call site, once, at
construction - not folded into a shared `tcell.Style` the way the four
chrome bars are (a dialog border is one lone `tcell.Color`, not a
foreground/background pair), and not per-row like the tag-string colors
above (a border is drawn once by `tview.Box.Draw()`'s own logic, not
recomposed into a string on every redraw the way a row's text is).

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
this package owns (39 of the 40 keys, including `background`) and rebuilds `BarStyle`/
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
unrecognized name, a malformed hex string) warns by key name and falls
back to `fallback` - the same one-key-at-a-time tolerance
`ReadTOMLFile`'s own parse-failure path already has elsewhere in this
app: one bad value costs only that one key, never the whole config.

**`"default"` - the terminal's own color (2026-10-02).** `background`
and `standard_text` additionally accept `"default"`
(`uikit.TerminalDefaultColor`), i.e. `tcell.ColorDefault`: nothing is
sent, so the terminal's own configured background/foreground shows - an
exact match whatever the terminal's real colors are, unlike `"white"`/
`"black"`, which are ANSI palette slots a terminal theme can redefine
(`"white"` is commonly a light grey there, visibly off against a pure
white terminal background). Checked by name before the validity check
above, since `tcell.GetColor` returns that same `ColorDefault` for an
unknown name. It works through both consumption paths: `tcell.GetColor`
and as a tview tag (`"[default]"`, confirmed to render in the terminal's
default foreground). Only those two keys, deliberately - they're the
base pair (plain text on the plain background); every other key is an
accent meant to differ from plain text, or drawn as a background
somewhere (selected-row blocks, the active tab, chrome, search
highlights), where "the terminal's default" would make the highlight
vanish. Any other key set to `"default"` gets its own warning saying so.
Opt-in for now - the built-in defaults stay `black`/`silver`, since the
other built-in colors assume a dark background; with `background =
"default"`, `general.color_scheme` still has to be set to match the
terminal, as it's what picks `[colors.dark]`/`[colors.light]`.

**The warning doesn't print immediately** - a real user report: since
`ApplyColorConfig` runs before any TUI-showing Verb constructs its own
`tview.Application`, an immediate print sat on the terminal's normal
screen for well under a second before tview's alternate-screen swap
visually covered it, and wasn't reliably restored on quit either
(terminal-dependent - some clear scrollback on entering the alternate
screen), so the message was effectively never seen. `ResolveColorOverride`
instead appends to `uikit.pendingColorWarnings`, and `session.Main` calls
`uikit.PrintColorWarnings()` once, right after each Verb's own function
returns (i.e. once its TUI, if any, has actually closed) but before that
Verb's own `os.Exit` - the same "collect while the alternate screen might
be up, print for real only once it's gone" reasoning `runner.
StreamStderr`/`lastStderr` already apply to a generation's own stderr
(design-docs/ErrorOutput.md), applied here to config-validation warnings
instead.

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

## Addendum: dark/light mode and named themes

**Implemented**, same status as the rest of this doc now. Two related
asks: some colors (`selected_row_text`, the chrome bars, ...) are tuned
for one background brightness and read poorly against the other, and
separately, a reusable *named* set of color choices (a "theme") that
isn't tied to any one project's own `config.toml`.

### Dark/light mode

A new `[general] color_scheme = "dark" | "light"` key, defaulting to
`"dark"` (today's hardcoded colors are already tuned that way - see
`SelectedRowTextColor`'s own doc comment on being nudged for readability
against light backgrounds, which in context means a light *foreground
element*, i.e. the app already assumes a dark surrounding terminal).
Deliberately no `"auto"` option - this app already rejected guessing a
terminal's own capabilities once (`design-docs/Notifications.md`'s own
reasoning for not auto-detecting OSC support), and a terminal's
background color is, if anything, less reliably queryable than that.

`[colors]` itself gains two optional sub-tables, `[colors.dark]` and
`[colors.light]` (`ColorsConfig.Dark`/`Light`, `internal/config/colorsconfig.go`
- `map[string]string`, not 39 more `*string` fields each, so there's only
one 39-name list to maintain), holding the same 39 keys - only the ones
that actually need to differ by background, not all 39 duplicated in
both. Precedence within `[colors]` becomes: `[colors.<scheme>]`
(matching whichever `color_scheme` is active) → flat `[colors]`
(scheme-independent) → hardcoded default. Still merged project → global
→ system per key (`mergeColorMap`, the map analogue of `mergeColors`'
own `firstNonNil` rule), same mechanism as today, just one more tier to
check per key before falling through to the next config tier.

`background` is one of the 40 keys this applies to like any other - it
can be set flat, or split via `[colors.dark]`/`[colors.light]`, same as
every other key. Worth being explicit about what this does and doesn't
give you: setting `color_scheme = "light"` alone changes nothing about
the background (or anything else) by itself - there's no built-in light
palette shipped with this mechanism, only the *ability* to define one.
An actual light-mode experience needs `[colors.light] background =
"white"` (or similar) set explicitly, in `config.toml` or in a theme's
own `[light]` table - the same as every other color choice this doc
covers.

### Named themes

A theme is a reusable, shareable set of color choices, referenced by
name rather than pasted into every project's own `config.toml`:

```toml
[general]
theme = "solarized"
color_scheme = "dark"
```

**One file per theme**, not one shared `themes.toml` holding all of
them - matching the convention most terminal/editor color-scheme
ecosystems already use (Alacritty, iTerm2, Windows Terminal, kitty,
tmux-themepack), so sharing one theme is "send one file" or "one PR
adding one file," never an edit to something everyone else's theme also
lives in. Each theme file (`ThemeFile`, `internal/config/theme.go`) is
TOML, the exact same shape `[colors]` already has - flat keys plus
optional `[dark]`/`[light]` sub-tables (not nested under a `[colors]`
key this time, since the whole file already is one) - decoded generically
into three `map[string]string`s (`Flat`/`Dark`/`Light`) rather than
`ColorsConfig`'s own named fields, purely to avoid a second 39-name list:
a theme file is load-once, read-only, never round-tripped back to TOML
the way `SettingsConfig`'s own fields conceptually could be. Goes
through the identical "Key naming rule" as `[colors]` itself - one key
per `uikit.*Color`/`diff.DiffChromeColor` var - and the identical
per-key validation, `uikit.ResolveColorOverride`, at the exact same
final step `[colors]`'s own values already went through
(`ResolveEffectiveColors` below just picks *which* string wins per key;
it's still `ApplyColorConfig` that validates it) - one bad key warns and
falls back without invalidating the rest of the theme.

**Location** (`ResolveThemePath`): reusing this app's own existing
two-tier precedent for a bundled-but-user-extensible asset - the
callback plugin (`runner.ResolveCallbackPluginDir`,
design-docs/OwnCallbackPlugin.md), checked in this order:

1. `<prefix>/share/tangsible/themes/<name>.toml` - anything shipped with
   the install (a distro package, `install.sh`'s own `CALLBACK_DIR`-style
   layout).
2. `$XDG_DATA_HOME/tangsible/themes/<name>.toml` (`internal/config.DataHome`)
   - a user's own, writable regardless of how Tangsible itself was
     installed.

Deliberately `DataHome`, not `ConfigHome`: a theme is a shareable asset
someone else might have authored, closer in kind to the callback plugin
than to `config.toml`'s own per-user/per-project *preferences* - the
same distinction `resolve.go` already draws between the two XDG
directories for an unrelated reason. `LoadTheme(name)` wraps this: `name
== ""` (no theme configured) returns quietly; a set name that resolves
to nothing at either tier, or a file that fails to parse as TOML, warns
once to stderr and falls back to "no theme" - see Open questions #2,
now decided.

### Full precedence chain

Putting both together, resolving one color key now has three tiers
instead of two (the first two each additionally check the active
`color_scheme` before falling through, per the precedence given for
`[colors]` above) - `ResolveEffectiveColors` (`internal/config/colorsconfig.go`)
is where this is actually implemented, via a small `pick(key, flat)`
closure applying exactly this order per key; everything downstream
(`uikit.ApplyColorConfig`/`diff.ApplyColorConfig`) is unchanged, since a
`ColorsConfig` with every field already resolved is exactly what those
two already expected before this addendum existed:

1. `config.toml`'s own `[colors.<scheme>]`/`[colors]`, cascaded
   project → global → system (today's mechanism, unchanged) - the
   per-project/per-user override, optional.
2. The selected theme's own `[<scheme>]`/flat keys, if `general.theme`
   is set - optional, and itself just one file, not cascaded across
   multiple locations the way `config.toml` is (see Open questions).
3. This app's own hardcoded default (`internal/uikit/colors.go` et al.)
   - always available, what you get with no config and no theme at all;
   left as a still-nil field on `ResolveEffectiveColors`' own result,
   `ApplyColorConfig`'s existing fallback handles it exactly as before.

Nothing here has to be all-or-nothing: pick a theme and still override
one key in your own `config.toml` without forking the theme file.

### Open questions

1. **Do theme files themselves need a project/global/system cascade**,
   the way `config.toml` does - e.g. a `.tangsible/themes/` tier so a
   project can ship its own theme alongside the two `DataHome`/`prefix`
   tiers above? Not implemented - a theme is meant to be reusable
   *across* projects, which cuts against making it project-scoped at
   all - but worth an explicit call rather than a silent omission if
   real demand shows up.
2. ~~An unrecognized `general.theme` name~~ **Decided and implemented**:
   `LoadTheme` warns once to stderr and falls back to no theme (the same
   "swallow and fall back" convention `ReadDefaultPlaybook`/
   `DefaultTreeExpanded` already use elsewhere) - a bad theme reference
   costs only "no theme applied," the same tolerance a single bad color
   value already gets from `ResolveColorOverride`, not a harder failure.
   A theme file that resolves but fails to parse as TOML gets the
   identical treatment.
3. **Theme file metadata** - just the color keys as implemented above,
   or also a display name/description/author field for a future
   `tangsible themes list`-style command? Still out of scope for the
   mechanism itself; worth deciding once, not per-file, if it's wanted
   at all.
4. **Hot-reload** stays out of scope for the same reason it already is
   above - a theme/scheme change needs a restart, consistent with
   everything else configurable in this app.
