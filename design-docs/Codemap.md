# Codemap

A brief per-package, per-file map of the source tree, for orientation -
not a replacement for CLAUDE.md (which documents design decisions and
the "why") or the individual design docs (which document features).
Reflects the current package layout established by Restructuring.md.

Each non-test `.go` file listed below has a matching `_test.go` in the
same directory unless noted otherwise - not itemized separately.

## internal/playbook

The core event/aggregation data model - the play → task → host tree
built from the live jsonl stream. Depends on nothing else in this
module; nearly everything else depends on it.

- **aggregate.go** - `PlaybookState`/`TaskNode`/`PlayNode`/`Outcome`;
  `Apply()` consumes one `RawEvent` at a time and grows the tree.
- **events.go** - `RawEvent` and friends: the jsonl schema types and
  their decoding.

## internal/uikit

Generic tree/row/tab rendering toolkit - knows how to render a
`PlaybookState` into styled rows/tabs, nothing about verbs or
subprocesses.

- **treelist.go** - `TreeList`, a hand-rolled `tview.Primitive`
  replacing `tview.List` (needed for unbounded mouse-wheel panning and
  custom per-row coloring `tview.List` couldn't do).
- **tabs.go** - `TabbedPane`, the shared tab-bar widget (drill-down
  view, template page).
- **tui_style.go** - palette/style constants (colors, indents,
  bottom-bar text).
- **tui_layout.go** - row-text rendering: `TaskLabel`/`HostLabel`/
  `PlayRowText`/`ColorTag`/host-column layout.
- **tui_rows.go** - the `Row` type, `FlattenRows` (tree → flat row
  list), status-row text.
- **tui_filter.go** - `FilterQuery`/`TaskVisible` and friends: the
  All/Changed/Failed/search filters.
- **tui_dialogs.go** - `CenteredModal`/`FilterDialogText`/`InRect`:
  small dialog-overlay helpers.
- **tui_drilldown.go** - `BuildOutputTabs` and the whole per-tab
  builder family for the output drill-down view (Output/Task/Resolved/
  Docs/Diff/Details).

## internal/config

User-authored settings (`.tangsible/config.toml`) plus app-owned
invocation history/state (`.tangsible/state.toml`) plus passthrough-arg
parsing. Zero dependencies on anything else here - the hub every verb
package reads.

- **resolve.go** - `SettingsConfig`, `Verb` parsing, the playbook
  resolution cascade (env var → project config → XDG config →
  `site.yml`).
- **history.go** - `StateConfig`, invocation history read/write
  (`AppendInvocation`/`FinalizeInvocation`/`LastTarget`).
- **rerunargs.go** - `ParsedPassthroughArgs`: splitting/reassembling
  `--tags`/`--limit`/etc for the rerun dialog and history.
- **rerunresolve.go** - `RerunResolution`: what a bare `tangsible
  rerun` resolves to.
- **runlog.go** - per-generation saved run data
  (`.tangsible/runs/<id>.jsonl`/`.stderr`) backing "revisit".

## internal/role

- **role.go** - the "role" verb: generates/removes a throwaway stub
  playbook for `roles: [name]`.

## internal/source

- **source.go** - `BuildTaskSourceIndex`: finds a task's own YAML
  source text (the drill-down's TASK: section) by indexing every
  `.yml`/`.yaml` file under the playbook's directory tree.

## internal/runner

The "how does one ansible-playbook generation actually run" mechanism -
spawn, stream, track progress, record outcome. Shared by the live
run/rerun/role session and by revisit's own rerun.

- **process.go** - `ProcHandle`/`StreamItem`/`GenerationOutcome`,
  `SpawnGeneration`/`ScanEvents`/`StreamStderr`, exit-code handling.
- **generation.go** - `RunOneGeneration`/`NewRequestRerun`: draining
  one generation to completion and starting a fresh one on request.
- **progress.go** - `ProgressTracker`: the "Task x/y" top-bar progress
  indicator, predicted from a throwaway `--list-tasks --list-hosts`
  run.

## internal/inventory

- **inventory.go** - small `ansible-inventory --list` client
  (`AnsibleInventoryGroup`/`FlattenInventoryHosts`/
  `ListInventoryHosts`), shared by `host` and `template` (they used to
  depend on each other for this; this package is what broke that
  cycle).

## internal/host

- **host.go** - the "host"/"hosts" verbs: a standalone five-tab view
  of everything Tangsible can determine about one host (live facts,
  inventory group chain, which plays would run, host_vars files, raw
  `ansible-inventory --host` dump).

## internal/template

- **template.go** - the "template" verb: standalone Jinja2 template
  debugger, one synchronous render per host switch.

## internal/ansibledoc

- **ansibledoc.go** - `FetchAnsibleDoc`: shells out to `ansible-doc`
  for the drill-down's "Docs" tab.

## internal/revisit

- **revisit.go** - the "revisit" verb: browse previous runs, reopen
  one via a replayed (frozen) `NewLiveTUI`. Owns `NewLiveTUIFunc`, the
  callback type that lets it call into `session.NewLiveTUI` without an
  import cycle.
- **revisitresolve.go** - `RevisitEntry`, filtering/sorting invocation
  history into a revisit-able list.

## internal/diff

- **diff.go** - the `d` hotkey: pick a past run, show only what
  differs from the current tree, drill into per-tab unified diffs.
- **diffmatch.go** - the matching engine: aligning two `PlaybookState`
  trees (pure - no I/O, no UI).
- **diffresolve.go** - candidate-run resolution for the diff picker
  (reuses `revisit`'s own list-picker UI).

## internal/session

"The live tangsible session": verb orchestration for run/rerun/role,
plus the live TUI itself and its recap/resolved-values views.
Restructuring.md's own Phase 3 (now done) split `NewLiveTUI` itself
across the `livesession*.go` files below - `tui.go` is now just its
thin constructor/wiring shell.

- **main.go** - `Main()`: verb dispatch, generation lifecycle for
  run/rerun/role. Called by root `main.go`.
- **tui.go** - `NewLiveTUI`: builds every widget, populates a
  `liveSession`, wires up callbacks, returns `(*tview.Application,
  func(playbook.RawEvent))`. 879 lines (was ~3400 before Phase 3) -
  everything it used to contain directly now lives in one of:
  - **livesession.go** - the `liveSession` struct itself (every piece
    of state/widget more than one method touches) and `resolveKey`
    (the Resolved/Docs/File tab cache key type).
  - **livesession_rebuild.go** - `rebuild()`, the ~380-line method
    nearly everything else calls to trigger a redraw, plus the small
    chrome/style helpers that exist only to serve it
    (`progressPosition`/`activeTaskNow`/`currentMainBottomBarText`/
    `chromeColorName`/`showElapsed`/`outputBottomBarNormalStyle`/
    `revealExpandedTask`/`switchPage`).
  - **livesession_tabsearch.go** (+ `_test.go`) - `tabSearchPanel`, a
    dedicated type owning the in-tab search subsystem (design-docs/
    Search.md) - genuinely self-contained, so it's a real type with
    its own unit tests rather than `liveSession` methods.
  - **livesession_rerundialog.go** (+ `_test.go`) - `rerunFieldSync`,
    a dedicated type owning the re-run dialog's four input fields,
    three checkboxes, and the sync/autocomplete logic between them
    (design-docs/Rerun.md's "Extend rerun dialog") - the other
    genuinely self-contained group. `openRerunDialog`/`submitRerun`
    stay as `liveSession` methods in this same file (not part of the
    dedicated type) since they reach into the rest of the shared state
    pool on a rerun.
  - **livesession_output.go** - the output drill-down: `renderOutputTabs`,
    `showOutputWithOrigin` (+ its three async resolve/docs/file fetch
    goroutines), `navigateOutputTask`/`navigateOutputHost`, `closeOutput`.
  - **livesession_filter.go** - the tree-level filter (`f`) and search
    (`/`) dialogs: `openFilterDialog`/`openSearchDialog`/`closeDialogs`/
    `applyFilter`.
  - **livesession_nav.go** - tree expand/collapse/navigation:
    `expandAll`/`collapseAll`/`handleRight`/`handleLeft`/
    `navigateMainTask`.
  - **livesession_events.go** - `PlaybookState`'s own event hooks
    (`onPlayAdded`/`onPlayStarted`/`onTaskAdded`/`onHostRecorded`) and
    `applyLive`, the function `NewLiveTUI` returns to feed live
    `RawEvent`s in.
  - **livesession_heartbeat.go** - `startHeartbeat`/`startResizeWatcher`,
    the two permanent background tickers.
  - **livesession_input.go** - `handleKey`, the keyboard dispatcher, and
    its 12 named sub-methods (dialog guards, universal key aliases,
    output-view keys, main-tree keys - see CLAUDE.md's "Keyboard
    shortcuts" section for the tier breakdown).
  - **livesession_mouse.go** - `handleMouse`, the mouse dispatcher, and
    its 7 named sub-methods (same per-mode split as the keyboard side).
- **recap.go** - the post-run recap summary section.
- **resolved.go** - the drill-down's "Resolved" tab (re-renders a task
  with variables filled in, via a real `ansible.builtin.template`
  task).
- **render.go** - plain-text tree dump; unused by the live flow, kept
  as a dependency-free debug aid.
- **autocomplete.go** - re-run dialog field autocomplete matching
  (`matchToken`/`matchTaskName`/`replaceLastToken`), used by
  `rerunFieldSync` above.
- **rerundialog.go** - pure re-run dialog logic with no `tview`
  dependency: `resolvedRerunHosts` (checkbox-to-host-list union/dedupe)
  and `resumablePlayName` (guards "Resume where failed" against
  offering a play name `TrimPlaybookToPlay` could never match). Not to
  be confused with `livesession_rerundialog.go` above, which holds the
  *stateful* widget-wiring type that calls into these.
- **fetchfile.go** - the drill-down's "File" tab: local/remote file
  content fetching, binary detection.
- **version.go** - the `version` verb: build stamps plus a best-effort
  Ansible/`ansible.posix` environment report.

## package main (repo root)

- **main.go** - 26 lines: `func main() { session.Main() }`. Nothing
  else lives here.
- **e2e_rerun_test.go** - `//go:build e2e`; the one exception to
  "tests live next to their code" - builds the real binary and drives
  it inside a real tmux pane, so it has no direct dependency on any
  package's internals and stays at the root regardless of what moves
  around underneath it.
