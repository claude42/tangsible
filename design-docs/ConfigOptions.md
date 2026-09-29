# Configuration options (`.tangsible/config.toml`)

## Situation

`.tangsible/config.toml` is Tangsible's user-authored settings file — safe
to hand-edit, including comments, since nothing in Tangsible ever opens it
for writing (see `Dottangsible-directory.md` for why it's split out from
`.tangsible/state.toml`, which *is* app-owned and rewritten every run).
This doc is a flat reference of every option it currently supports, since
that list is otherwise only discoverable by reading `internal/config/
resolve.go`'s `SettingsConfig` struct directly.

The same shape is also read from the global
`$XDG_CONFIG_HOME/tangsible/config.toml` (default `~/.config/tangsible/
config.toml`) and the system-wide `/etc/tangsible/config.toml`. **Every
key in `[general]` cascades the same way**: the project-local file wins if
it sets the key at all, else the global file, else the system file, else
the documented default (`MergedSettingsConfig`). This used to be true only
of `default_playbook`, with every other key read from the project-local
file alone — deliberately changed so no key behaves differently from its
neighbor for a reason a user has no way to discover just from reading
`config.toml`, regardless of whether any individual key's own case for
staying project-local was reasonable on its own.

`default_playbook` still has two sources none of the others do — see its
own row below.

## `[general]`

| Key | Type | Default | Notes |
|---|---|---|---|
| `default_playbook` | string | *(none — falls through to `./site.yml`)* | Also checks `$TANGSIBLE_PLAYBOOK` first, above every config tier - see its own entry below |
| `default_tree_state` | string: `"expanded"` \| `"collapsed"` | `"collapsed"` | |
| `two_pane_layout` | bool | `true` | |
| `color` | bool | `true` | |
| `run_dialog` | string: `"default"` \| `"never"` \| `"always"` | `"default"` | |
| `notify_playbook_finished` | string: `"off"` \| `"osc9"` \| `"osc777"` \| `"osc99"` \| `"bell"` | `"off"` | |
| `notify_task_failed` | string: `"off"` \| `"osc9"` \| `"osc777"` \| `"osc99"` \| `"bell"` | `"off"` | |
| `notify_task_failed_max` | int | `5` | |
| `template_hosts_max` | int | `10` | |
| `color_scheme` | string: `"dark"` \| `"light"` | `"dark"` | See `[colors]` below |
| `theme` | string (a theme name) | *(none — no theme)* | See `[colors]` below |

All eleven cascade project → global → system → documented default, as
described above.

**`default_playbook`** — the playbook path used when a verb that needs one
(`run`, the fallback half of `rerun`) isn't given one explicitly on the
command line. Checked before any config tier at all: `$TANGSIBLE_PLAYBOOK`
wins over every file below it — see `tangsible(1)`'s CONFIGURATION section
for the full order, or `ResolvePlaybook`.

**`default_tree_state`** — whether a freshly-started run's very first task
row starts out expanded or collapsed. Case-insensitive; any value other
than `"expanded"` (including a typo, or the key being absent everywhere)
means collapsed, applied silently rather than warning.

**`two_pane_layout`** — whether the output drill-down keeps the tree pane
visible alongside it on a wide enough terminal, instead of taking over the
whole screen. See `TwoPanedLayout.md`. A `*bool` under the hood, not a
plain `bool`: absent (everywhere) means enabled, so this is one of the
options where you have to write `two_pane_layout = false` explicitly to
turn it off — there's no way to distinguish "wrote `false`" from "didn't
write anything" with a plain bool, and no way to distinguish "set to
`false` in one tier" from "unset there, but set elsewhere" without keeping
that tri-state through the whole cascade (`MergedSettingsConfig`'s own
`firstNonNil`).

**`color`** — whether a task row's collapsed per-host summary may render
in color at all, independent of terminal capability and `$NO_COLOR` (all
three must allow color for that row specifically to render in it — see
`Morehosts.md`). Same absent-means-enabled `*bool` shape as
`two_pane_layout`. Doesn't affect any other color in the app — outcome
colors, chrome bars, etc. are unaffected either way (see "Planned, not
yet implemented" below).

**`run_dialog`** — whether the re-run dialog opens at a session's startup
(`RerunDialog.md`), independent of which Verb started it. `"default"`
keeps the pre-existing per-Verb behavior (shown for `rerun`, not for
`run`/`role`); `"never"`/`"always"` force it off/on regardless of Verb.
Case-insensitive; any unrecognized value (including a typo) falls back to
`"default"`, same convention as `default_tree_state`. An explicit
`--dialog`/`--no-dialog` on the command line overrides this per
invocation, for `run`/`rerun`/`role` only — every other Verb rejects
either flag outright.

**`notify_playbook_finished`/`notify_task_failed`** — which terminal
notification (if any) fires when a playbook finishes, or when a task
fails, respectively (`Notifications.md`). Case-insensitive; any
unrecognized value (including absence everywhere) means `"off"`.

**`notify_task_failed_max`** — caps how many `notify_task_failed`
notifications fire per generation before further failures are counted but
not individually notified (`Notifications.md`).

**`template_hosts_max`** — how many hosts the `template` Verb's own
resolved host list (from its comma-separated hostname/group positional,
including the `all` keyword) can reach before it asks for confirmation
rather than rendering against all of them silently (`Tangsible
template.md`).

**`color_scheme`/`theme`** — design-docs/ColorConfig.md's own addendum:
which of `[colors]`'s own `[dark]`/`[light]` sub-tables applies, and an
optional named theme file to load. See `[colors]` below rather than
duplicating that doc's own reference here.

## Example

```toml
[general]
default_playbook = "site.yml"
default_tree_state = "expanded"
two_pane_layout = true
color = true
run_dialog = "default"
notify_playbook_finished = "off"
notify_task_failed = "off"
notify_task_failed_max = 5
template_hosts_max = 10
color_scheme = "dark"
# theme = "solarized"
```

Every key in the table is optional; an absent key always means "use the
documented default," never an error.

## `[colors]`

`design-docs/ColorConfig.md` documents a separate `[colors]` table for
overriding all 37 individual UI colors (outcome colors, chrome bars,
section headings, and so on) — implemented, cascading project → global →
system exactly like `[general]` above. Not repeated in this file's own
table: `ColorConfig.md`'s own "Key naming rule" section is the reference
for that table's key list (one key per `uikit.*Color`/`diff.DiffChromeColor`
var, design-docs/Colors.md). `[general] color` above is unrelated to
`[colors]` either way: it's a pre-existing on/off switch for one specific
row's color usage, not one of its 37 keys.

`ColorConfig.md`'s own addendum adds `[colors.dark]`/`[colors.light]`
sub-tables (only the keys that need to differ by `color_scheme`) and
named themes - one TOML file per theme, `general.theme`-selected, found
under `<prefix>/share/tangsible/themes/` or
`$XDG_DATA_HOME/tangsible/themes/`, same shape as `[colors]` itself.
`ColorConfig.md`'s own "Full precedence chain" section is the reference
for how `[colors]`, a theme, and the hardcoded defaults combine.
