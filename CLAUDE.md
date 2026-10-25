# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository. This file
holds what isn't obvious from the code: invariants, gotchas, and where things
live. The *why* behind features lives in `design-docs/` - each section below
names its doc; read it before changing that feature.

## What this is

Tangsible is a TUI wrapper for `ansible-playbook` for interactive use (e.g.
during development). `design-docs/Purpose.md` is the "why" - read it before
design choices. `README.md` is the user-facing tour; `design-docs/Codemap.md`
maps packages/files (verify against `grep`, it can drift);
`design-docs/Keyboard-shortcuts.md` is the full key reference.

One Go binary, eleven verbs (`internal/config/resolve.go`'s `ParseVerb`):
`run`, `rerun`, `role`, `revisit`, `debug` share the live tree/TUI
machinery (`internal/session`) - `debug` (`design-docs/DebugMode.md`)
adds breakpoint/pause state on top, not a second TUI of its own;
`template`, `host`/`hosts`/`inventory`, `vault`, `version` are standalone
programs that never build the live tree. Requires `ansible-playbook` and
a real TTY. Root `main.go` just calls `session.Main`.

## Commands

```
go build ./...
go vet ./...
go test ./...                     # unit tests; -run <Name> for one
go test -tags e2e ./...           # + tmux-driven e2e (e2e_rerun_test.go; needs tmux, ansible-playbook)
go run . run testdata/outcomes.yml -i localhost,
go run . run testdata/multihost.yml -i testdata/multihost-inventory.ini
```

During development set `TANGSIBLE_CALLBACK_DIR=$PWD/callback` - the bundled
callback plugin is found next to an installed binary, not next to `go run`'s
temp binary. Fixtures in `testdata/` are described in its files' own comments
(`free-strategy.yml`, `hostnames.yml` with an unreachable host, `diff.yml`,
vault fixtures, ...).

`completions/` (bash, fish) is hand-maintained: update it whenever verbs or
flags change.

Testing philosophy: `design-docs/TESTING.md` (pure functions first; tmux for
the UI). When checking the TUI live in tmux: `capture-pane -e` emits
attribute changes relative to the previous cell *across lines*, so a bold line
after a bold line shows no `ESC[1m` - capture single lines (`-S n -E n`) to
check styling; and `pgrep -f <pattern>` matches your own shell command if the
pattern appears in it.

## Packages

`internal/playbook` (event model + aggregation, dependency-free) ·
`internal/uikit` (generic rendering toolkit: rows, `TreeList`, `TabbedPane`,
dialogs, search, help, colors - nothing verb-specific) · `internal/config`
(settings, history, arg parsing, run logs) · `internal/runner` (spawn/stream/
progress/outcome of one `ansible-playbook` generation) · `internal/source`
(task YAML source lookup) · `internal/session` (run/rerun/role/debug
orchestration + the live TUI) · one package per standalone verb (`role`,
`template`, `host`, `revisit`, `diff`, `vault*`) · `internal/debugcopy`/
`internal/debugrun`/`internal/debugverb` (debug mode's own headless
primitives: shadow-copy/splice, the live process driver + breakpoint
policy, and skeleton-seeded state/event resolution, respectively -
`internal/session` is their only caller, same as any other verb-specific
package above) · `internal/inventory`, `internal/ansibledoc`, `internal/rolepath` (small
shared clients). Only `internal/session` (its `tui.go` and `livesession*.go`) and
`internal/uikit` touch `tview`/`tcell`; `session/main.go` doesn't, and
neither does any of debug mode's own three packages.

The live TUI is a `liveSession` struct (`livesession.go`) built by `NewLiveTUI`
(`tui.go`), with methods split by concern across `livesession_*.go` (rebuild,
input, mouse, output drill-down, filter, nav, events, heartbeat, rerun dialog,
tab search, stdout view). Only `tabSearchPanel` and `rerunFieldSync` are unit-
testable in isolation; the rest needs tmux.

## Configuration

`config.toml` in `./.tangsible/` (project), `$XDG_CONFIG_HOME/tangsible/`
(global), `/etc/tangsible/` (system); every key cascades project -> global ->
system -> built-in default. Unknown/invalid values fall back silently
(`[general]`) or warn after exit (`[colors]`). Reference:
`design-docs/ConfigOptions.md`, `examples/config.toml`.

The playbook argument is optional for `run`/`rerun`/`host`: `$TANGSIBLE_PLAYBOOK`
-> `default_playbook` (cascaded) -> `./site.yml` (`ResolvePlaybook`).
`SplitPlaybookArgs` decides by shape: empty or starting with `-` means "no
playbook, all passthrough".

`.tangsible/state.toml` (history, app-owned, gitignored) is deliberately
separate from `config.toml` (user-authored, never written by tangsible).

**Colors** (`design-docs/ColorConfig.md`, `Colors.md`): `[colors]` has one key
per `uikit.*Color` var (snake_case, `Color` dropped), resolved once by
`uikit.ApplyColorConfig`/`diff.ApplyColorConfig` in `session.Main` before any
TUI exists - read the vars at construction time, not at package init.
`[colors.dark]`/`[colors.light]` and theme files are selected by
`general.color_scheme`/`theme`. `background` is pushed into
`tview.Styles.PrimitiveBackgroundColor`; `background` and `standard_text` alone
accept `"default"` (terminal's own color, `uikit.TerminalDefaultColor`).
**Never leave text on tview's defaults** (white text, yellow labels, white
titles, blue/white controls): they ignore themes and vanish on light
backgrounds. Plain text uses `standard_text` (view-wide via `SetTextColor`, or
`TreeList`'s default), titles/labels set it explicitly, controls use
`uikit.StyleForm`/`StyleInputField`/`StyleButton`/`StyleCheckbox`/
`StyleAutocomplete` (`control_text`/`control_background`). Named colors like
`"white"`/`"black"` are terminal palette slots a theme can remap; hex values
(`#1a1a1a`) aren't.

## Core invariants

- **One event-loop goroutine.** `PlaybookState.Apply`, every `TaskSourceIndex`
  read/merge, `rebuild()`, and clipboard writes to `os.Stdout` all run on
  tview's event loop (via `QueueUpdateDraw`/input capture), which is why none
  of them lock. Keep it that way.
- **Generation outcome ordering.** `runner.RunOneGeneration` stores
  `exitCode`, `lastStderr` and finishes the `OutputLog` *before* setting
  `processDone`; readers check `processDone` first. Atomics only, no mutex.
- **`quitting`** is checked before queueing anything into tview: its update
  queue is a 100-slot buffer nobody drains after `app.Run()` returns.
- **Never print while the TUI runs.** Stderr, warnings (`PrintColorWarnings`)
  and wait errors are collected and printed after `app.Run()` returns.
- **Ctrl-C/q**: tcell raw mode disables SIGINT, so tangsible forwards it to the
  child (`ProcHandle`, always the current generation) while running and quits
  once finished - matching plain `ansible-playbook`. `debug` carves out one
  exception: `q` (not Ctrl-C) while its own F5/F10/F11 drive loop is in
  flight requests a soft stop instead of touching the real process -
  `design-docs/DebugMode.md`'s "q's own soft stop while actively driving".
- **Exit codes**: 99 = user interrupt (unambiguous, only tangsible sends
  SIGINT); 4 = unreachable *or* parse error, disambiguated by
  `state.HadUnreachable`. `uikit.GenuineFailure` is the shared predicate.
- **Target scale ~10 hosts**: eager fetches, full rebuilds per event, no
  pagination are deliberate.

## Event pipeline (`internal/runner`, `internal/playbook`)

- **Callback plugin** (`callback/tangsible_jsonl.py`,
  `design-docs/OwnCallbackPlugin.md`): a GPL fork of `ansible.posix.jsonl`
  (schema-compatible), `aggregate` type, shipped as a sibling file (never
  `//go:embed` - keeps the binary Apache-2.0). `ResolveCallbackPluginDir`
  finds it; `CallbackPluginEnv` *unions* it into the user's
  `ANSIBLE_CALLBACK_PLUGINS`/`ANSIBLE_CALLBACKS_ENABLED` (via `ansible-config
  dump --only-changed`), never overwrites. `ANSIBLE_JSON_INDENT=0` everywhere.
- **Transport**: live runs get events on a dedicated fd (`TANGSIBLE_EVENT_FD`,
  `cmd.ExtraFiles`); the child's stdout stays the user's own callback output
  (`OutputLog`, the `o` view). One-shot scrapes (host, template, resolved) read
  mixed stdout and pick out their one event.
- **Pre-flight gate**: `StartFirstGeneration` peeks the first event before
  building any TUI; no event at all (bad playbook, parse error, `--list-tasks`)
  means print captured stdout/stderr and exit. Only `run`/`role`'s first
  generation goes through it.
- **Events** (`events.go`): fields are empirically confirmed - re-verify
  against real output before relying on new ones. `Hosts` stays
  `map[string]json.RawMessage`; raw bytes are kept for display.
- **Aggregation** (`aggregate.go`): tasks resolve by `task.id`, falling back to
  source `path` per play (dynamic includes get per-host ids under `strategy:
  free`; a `loop:`ed include must still yield separate rows - see
  `findOrCreateTask`). Task-start and handler-task-start are handled
  identically. `IncompleteTasks()` can hold several tasks (`strategy: free`).
  `ignore_errors` failures stay `OutcomeFailed` for coloring/rerun but carry
  `TaskNode.Ignored` (recap, notifications).
- **Run logs** (`config/runlog.go`): per generation `.tangsible/runs/<RunID>`
  `.jsonl` (byte-for-byte tee), `.stderr`, `.stdout`; best-effort, 0600. Back
  `revisit` and diff mode; history keeps 20 per playbook and deletes evicted
  logs.

## Live TUI (`internal/session`, `internal/uikit`)

- **rebuild()** recomputes and redraws everything; navigation calls it too
  (the cursor's styling is baked into row text). Selection is tracked by row
  *identity*, not index; `s.rebuilding` guards the spurious change events
  `Clear()`/`AddItem()` fire.
- **Following**: `s.following` follows the newest row; any navigation stops
  it, only `F` resumes (End/G are plain navigation). The `o` view mirrors this
  with `s.stdoutFollowing`.
- **Heartbeat** (200 ms, `uikit.SpinnerInterval`) drives redraws while live and
  stops after one frozen frame; a separate resize watcher redraws on terminal
  resize once frozen and must use `ForceDraw()` - `Draw()` inside
  `QueueUpdate` deadlocks.
- **Expand state** is per `*TaskNode`; a new task inherits the previous task's
  state (that's what makes `E` sticky while running).
- **Drill-down** (`livesession_output.go`, `uikit/tui_drilldown.go`): tabs
  Task, Output, Diff, File, Task definition, Resolved, Docs, Details - empty
  ones are omitted; File/Resolved/Docs fetch asynchronously on open. Two-pane
  mode (`design-docs/TwoPanedLayout.md`) is decided live per `rebuild()` from
  `pages`' width. The single-run **Diff tab** (Ansible's `--diff`, always
  passed) is unrelated to **diff mode** (`d`, comparing two runs).
- **Filtering** (`design-docs/Filters.md`): `uikit.TaskVisible` is the single
  predicate; the in-progress task always matches; navigation skips hidden
  tasks.
- **Keyboard** (`livesession_input.go`): `handleKey`'s body is the
  authoritative, order-critical dispatch - help overlay, Ctrl-C, dialog/prompt
  guards, `o` view, quit, aliases, then page-specific keys. Aliases come from
  `uikit.TranslateNavAlias`, shared by every app; apply them only after text
  fields had their chance (typed `b`/`G` must stay text).
- **Help + bottom bars** (`design-docs/Help.md`): each package's
  `helptables.go` holds every screen's help table and, right next to it, its
  hand-written bottom bar. Changing a binding means updating both there (and
  `Keyboard-shortcuts.md`). `uikit.HelpOverlay` is the shared runtime;
  the re-run dialog opens help on F1 only.

## tview/tcell gotchas (each was a real bug)

- `tview.Pages.InputHandler` dispatches keys to the first page whose item
  `HasFocus()`, visible or not, and a `TabbedPane` keeps a stale focus flag -
  `closeOutput` must call `s.outputTabs.Clear()` or the next dialog loses its
  keystrokes (`design-docs/RerunDialogFocusBug.md`).
- Nested primitives (a field several `Pages`/`Flex` deep, the `o` view) don't
  reliably get forwarded keys: call their `InputHandler()` directly from the
  input capture, as `tabSearchPanel.handleComposingKey` does.
- Mouse: tview synthesizes one click from several capture calls, threading
  each returned event into the next - returning `nil` for a `MouseMove`
  kills the click. Only swallow `MouseLeftClick` (after handling it); always
  nil-check the event.
- `InputField`/`Checkbox.SetDisabled` replay a stray navigation key inside a
  `Form` - never toggle fields that way (the re-run dialog omits checkboxes
  instead; e2e tests guard this).
- `TextView.SetText` keeps the scroll position (call `ScrollToBeginning`);
  `trackEnd` makes End/G/scroll-to-bottom follow appended text (pin with
  `ScrollTo` when not following); `Highlight()` swaps colors on top of a
  region's tag; `SetLabelColor` sets foreground only.
- `tview.List` re-clamps its scroll offset to the cursor on every draw -
  that's why the tree is a custom `uikit.TreeList`.
- `SetScreen` before `EnableMouse` (tview only enables the mouse on an existing
  screen).
- Literal `[` in dynamic text is a tag: `tview.Escape` everything external.
  `tview.TranslateANSI` doesn't escape - escape only the plain segments
  (`runner.SplitANSI`).

## Features (one line each; details in the named doc)

- **Rerun** (`design-docs/Rerun.md`, `RerunDialog.md`): one `app.Run()` per
  process; `runner.NewRequestRerun` resets state, reassembles args (dialog
  fields + original `Rest`), merges a fresh source index (the playbook may have
  been edited), and spawns. The dialog is a `tview.Form`; checkbox/field sync
  lives in `rerunFieldSync`. `rerun` verb precedence: `ResolveRerun`.
- **Start with a play** (`StartWithPlay.md`): an ephemeral trimmed copy of the
  playbook *in the playbook's own directory* (relative includes must still
  resolve), removed after the generation. `--start-at-play`, `--dialog`/
  `--no-dialog` and similar synthetic flags are stripped before passthrough and
  never recorded in history.
- **Progress** (`ProgressIndicator.md`): predicted from a `--list-tasks
  --list-hosts` probe with a bounded forward-matching window; shown as a fill
  and a percentage (marked temporary in code). The recap's count is the
  trustworthy number.
- **Recap** (`Recap.md`): per-host counts incl. `warnings`/`ignored`, built
  from `PlaybookState`; `rescued` isn't derivable from the event stream.
- **--check** (`CheckMode.md`): detected once from passthrough args; olive
  chrome and a `Check:` top-bar label. Revisit's purple wins over it.
- **Revisit** (`Revisit.md`): replays a saved `.jsonl` into a frozen
  `NewLiveTUI` (injected as `NewLiveTUIFunc` to avoid an import cycle).
- **Diff mode** (`Diff.md`): aligns plays/tasks by *name* (paths shift
  between edits) with difflib opcodes; runs in `app.Suspend` so the tree
  resumes afterwards.
- **Role** (`Tangsible role.md`): stub `.tangsible-role-<role>.yml` in the
  *cwd* (source lookup walks the playbook's directory, plus referenced
  roles found via `internal/rolepath` - display only, `debug` stays in-tree).
- **Template** (`Tangsible template.md`): stub + output in one private scratch
  dir (removed as a whole, also on SIGTERM/SIGHUP - a late write from an
  interrupted render then fails instead of leaking); `hosts: all` + `--limit`
  per render, `delegate_to: localhost`; renders are serialized; role-owned
  templates get the role's vars via `vars_files`, never `roles:`.
- **Host/hosts/inventory** (`HostVerb.md`): five tabs fetched concurrently; the
  Summary tab uses an explicit `setup:` task (an implicit gather is silently
  skipped with a warm fact cache) with a fact-cache fast path
  (`FetchHostFacts`). `hosts`/`inventory` also keep their own session-level
  `HostDetailCache`, keyed by hostname - revisiting a host already opened
  this session (`Left`/`Right`/`n`/`N`, a click, Esc-and-reopen) renders all
  five tabs instantly from it instead of re-fetching, errors never cached.
  `inventory` is a collapsible group/host tree (`ansible-inventory
  --graph`-shaped) in front of the same detail view; a host that's a direct
  member of several groups gets one node per occurrence but one shared,
  deduplicated ping (and detail cache entry).
- **Autocomplete** (`Autocomplete.md`): `uikit.FilteredMatch`/
  `MatchSingleValue`; tview's callback sees only the full text, so only the
  last comma-separated token completes. Enter/Esc go to an open drop-down
  first (`autocompleteOpenNow`).
- **File/Resolved tabs** (`ShowFileContents.md`, `Drilldown, Resolved
  Values.md`): extra `ansible-playbook` runs with the original args, skipped
  under `--ask-become-pass`/`--ask-vault-pass` (a prompt would fight the TUI).
- **Vault** (`Vault.md`): native AES-CTR (`internal/vaultcrypto`); re-encrypts
  only changed keys and splices original bytes for unchanged ones (yaml.v3
  can't reproduce ansible-vault's indentation); never touches tview.
- **Debug** (`DebugMode.md`): a `liveSession` with breakpoint/pause state
  layered on, not a second TUI - the real tree, two-pane layout, and
  drill-down are the exact same ones `run`/`rerun`/`role` use. Breakpoints
  + step on ansible-core's own `debugger:` keyword, spliced onto every
  task in a disposable hardlink shadow copy (`internal/debugcopy`);
  `internal/debugrun` drives it over a second pipe pair alongside the
  normal jsonl event stream, deciding which pauses to surface
  (`BreakpointSet`); `internal/debugverb.SeedState` pre-builds the whole
  predicted tree from a `--list-tasks` skeleton up front (so breakpoints
  can be armed before anything runs), and `debugverb.Cursor` resolves
  each live event against it - deliberately never through
  `PlaybookState.Apply`, which can't reuse those pre-built nodes at all
  once a real run starts. F5/F9/F10/F11/F12 run/toggle-breakpoint/
  next-task/step/redo (`livesession_debug.go`); two extra chrome colors
  (`chrome_debug_running`/`chrome_debug_paused`) mark a session actually
  running vs. paused, on top of the four other chrome states. `v` opens
  a persistent variable watch explorer (`livesession_watch.go`,
  `debugwatch_eval.go`) - a text field with live autocomplete (name +
  current value, sourced from the selected host/task's own captured
  `task_vars`) for adding a watched expression (a bare name, or a
  dotted/bracket-index path like `foo.bar`/`foo[0]` - a tiny hand-written
  grammar, not full Jinja), re-evaluated live as the tree selection
  moves, rendered unavailable (not an error) for an expression that
  doesn't currently resolve. The same field also *sets* one: `expr =
  value` writes `value` (forwarded verbatim, as raw Python) straight into
  the paused task's own live `task_vars`, taking effect on the very next
  redo/continue/step - only valid while the current selection is exactly
  the live pause (`debugIsPaused`), since that's the only task/host
  `task_vars` exists for right now.
- **Notifications** (`Notifications.md`): OSC 9/777/99 or BEL; never for
  something the user just did; ignored failures don't notify.
- **In-tab search** (`Search.md`) and **copy** (`CopyToClipboard.md`):
  `uikit.TextSearch` re-renders from plain text (tab colors vanish while
  searching); OSC 52 written to stdout directly, wrapped for tmux passthrough.
- **Raw-output view** (`o`, `livesession_stdout.go`, OwnCallbackPlugin.md
  Phase 2): stdout+stderr merged per line in arrival order; color forced via
  `ANSIBLE_FORCE_COLOR` unless the user opted out, stripped again for every
  other stderr consumer; current generation only; lines held back during a
  search. Banners stay 79 columns (ansible sizes them via ioctl on fd 1 only).

## Scope (deliberate)

- Interactive input *after* the TUI starts is unsupported in `run`/`rerun`/
  `role` (`--step` crashes, a bare `pause:` is skipped); become/vault
  password prompts work because they happen before the TUI. `debug`
  (`DebugMode.md`) is the dedicated way to drive ansible-core's own task
  debugger instead, via its own F5/F9/F10/F11/F12/v keys - it rejects
  ansible-playbook's own `--step` outright (a second, unhandled pause
  mechanism, `debugverb.PrepareSession`).
- `strategy: free` is supported (`StrategyFree.md`); a globally configured
  `free` isn't seen by the progress probe.
- No execution environments / ansible-runner; only a subset of
  `ansible-playbook`'s CLI is surfaced.

## Housekeeping

- Changelog: one terse bullet per user-visible change.
- New/renamed files: update `design-docs/Codemap.md`.
- A CLI change: update `completions/`, `README.md`, `man/`.
