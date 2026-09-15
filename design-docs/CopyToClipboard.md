# Copy to clipboard

## Situation

The different tabs in the drilldown view contain lots of valuable
information. Frequently it makes sense to copy this information into the
clipboard. While this is easy to do with standard select + Command-C, it
might be less easy in certain setups (e.g. with tmux) or when the two-pane
view is activated in tangsible.

## Idea

Add a keyboard shortcut (y) which will use OSC52 to copy the current tab's
content into the local clipboard.

## Design decisions

- **Scope**: `y` is wired into all four tab-bearing surfaces, not just the
  main drilldown — main run TUI (`tui.go`), the diff view (`diff.go`,
  opened with the `d` hotkey — not a CLI verb of its own), `tangsible
  hosts`/`host`, `tangsible template`. Mirrors how the earlier in-tab search
  (`/`, see `Search.md`) was rolled out consistently across every tab view
  rather than just one.
- **Content source**: same mechanism the search feature already uses —
  `TabbedPane.ActiveTextView()` → `GetText(true)` for the active tab's plain
  (tag-stripped) text. No new content-extraction path needed.
- **tmux**: auto-detected via the `$TMUX` env var. When set, the OSC52
  sequence is wrapped in tmux's DCS passthrough escapes before being written,
  so copying works out of the box under tmux without requiring the user to
  set `allow-passthrough on` themselves (this was the scenario called out
  explicitly in "Situation" above). Without `$TMUX`, plain OSC52 is emitted.
- **Feedback**: reuses the existing hint/footer-bar convention (the same one
  search's "no matches" / "match N of M" status uses) — a transient
  "Copied to clipboard" (or an error) is set on the relevant bottom bar via
  `SetText`, then reverts to the normal hint text.
- **Key binding**: `y` is currently unbound in all four views' input capture
  handlers, so no conflict.
