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
config.toml`) — but, per the table below, only `default_playbook` actually
participates in a cascade across both files. Every other option is read
from the project-local file only; setting it in the global file has no
effect.

## `[general]`

| Key | Type | Default | Scope |
|---|---|---|---|
| `default_playbook` | string | *(none — falls through to `./site.yml`)* | Cascades: `$TANGSIBLE_PLAYBOOK` → project `.tangsible/config.toml` → global config.toml → `./site.yml` |
| `default_tree_state` | string: `"expanded"` \| `"collapsed"` | `"collapsed"` | Project-local only |
| `two_pane_layout` | bool | `true` | Project-local only |
| `color` | bool | `true` | Project-local only |
| `run_dialog` | string: `"default"` \| `"never"` \| `"always"` | `"default"` | Project-local only |

**`default_playbook`** — the playbook path used when a verb that needs one
(`run`, the fallback half of `rerun`) isn't given one explicitly on the
command line. The one option in this file that's genuinely resolved
across multiple sources, not just this one file — see `tangsible(1)`'s
CONFIGURATION section for the full cascade order.

**`default_tree_state`** — whether a freshly-started run's very first task
row starts out expanded or collapsed. Case-insensitive; any value other
than `"expanded"` (including a typo, or the key being absent) means
collapsed, applied silently rather than warning.

**`two_pane_layout`** — whether the output drill-down keeps the tree pane
visible alongside it on a wide enough terminal, instead of taking over the
whole screen. See `TwoPanedLayout.md`. A `*bool` under the hood, not a
plain `bool`: absent means enabled, so this is one of the options where
you have to write `two_pane_layout = false` explicitly to turn it off —
there's no way to distinguish "wrote `false`" from "didn't write anything"
with a plain bool.

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

## Example

```toml
[general]
default_playbook = "site.yml"
default_tree_state = "expanded"
two_pane_layout = true
color = true
run_dialog = "default"
```

Every key in the table is optional; an absent key always means "use the
documented default," never an error.

## Planned, not yet implemented

`ColorConfig.md` proposes a `[colors]` table for overriding roughly forty
individual UI colors (outcome colors, chrome bars, section headings, and
so on) — a design still under review, not built. `[general] color` above
is unrelated: it's a pre-existing on/off switch for one specific row's
color usage, not part of that proposal.
