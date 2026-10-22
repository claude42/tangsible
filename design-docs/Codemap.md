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
- **help.go** - `HelpScreen`/`HelpSection`/`HelpEntry`: a screen's
  shortcut table, rendered as the `?` help dialog
  (`HelpText`/`NewHelpDialog`); `HelpOverlay`, the dialog's runtime
  (open/close, key/mouse handling, focus restore) every app shares.
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
  (`.tangsible/runs/<id>.jsonl`/`.stderr`/`.stdout`) backing "revisit".

## internal/role

- **role.go** - the "role" verb: generates/removes a throwaway stub
  playbook for `roles: [name]`.

## internal/source

- **source.go** - `BuildTaskSourceIndex`: finds a task's own YAML
  source text (the drill-down's TASK: section) by indexing every
  `.yml`/`.yaml` file under the playbook's directory tree.
  `ListTaskLocations` walks the same tree for `TaskLocation` (file/line/
  indent, not text) - debug mode's own splice-point data
  (`design-docs/DebugMode.md`), not used by the drill-down itself.

## internal/debugcopy

Debug mode's shadow-copy primitive (`design-docs/DebugMode.md`'s M0 build
step) - headless, consumed by `internal/debugverb.PrepareSession`.

- **shadowcopy.go** - `BuildShadowCopy`: a disposable, hardlinked copy of
  a playbook's whole directory tree under `.tangsible/debug/<RunID>/`, so
  a breakpointed run never touches the user's own tracked files and cost
  is independent of repo size.
- **splice.go** - `SpliceDebuggerAlways`: inserts a `debugger: always`
  sibling key into every `source.TaskLocation` found, unlinking each
  hardlinked file before writing so the original is never mutated through
  the shared inode.

## internal/debugrun

Debug mode's live process driver (`design-docs/DebugMode.md`'s M1 build
step) - headless; the two previous primitives (`internal/debugcopy`,
`internal/source.ListTaskLocations`) feed it a spliced shadow copy to
run, and `internal/session`'s own orchestration goroutine
(`livesession_debug.go`'s `debugStartOrContinue`) drives it.

- **session.go** - `Session`/`Spawn`: runs one `ansible-playbook`
  invocation against a shadow copy, with the bundled jsonl callback wired
  on one pipe (`Events`, reusing `runner.ScanEvents` unchanged) and
  ansible-core's own built-in task debugger driven synchronously over a
  second pipe pair (`Start`/`Continue`/`Query`) - plain pipes, not a pty;
  `Query` sidesteps parsing the debugger's native Python-repr output by
  having the query itself print JSON. `Session.Process()` exposes the
  real child `*os.Process` - stored straight into `liveSession`'s own
  `procH` the moment `Spawn` succeeds, so q/Ctrl-C reaches it through the
  exact same generic path a plain run's own child already uses, no
  debug-specific signal handling needed at all.
- **breakpoints.go** - M2: `BreakpointSet` (live-togglable, mutex-guarded
  set of armed task labels) and `StepMode`, consumed by
  `RunWithBreakpoints`/`ContinueWithBreakpoints` to decide, per `Pause`,
  whether to surface it or auto-`Continue` past it - `onPause`'s own
  `willSurface` parameter is that exact decision, passed down so a caller
  (`internal/session`'s `debugOnPause`) doesn't have to recompute it with
  no access to `bps`/`mode` of its own (`design-docs/DebugMode.md`'s
  "Stage 3 follow-up round 6").

## internal/debugverb

The "debug" verb's own headless glue (`design-docs/DebugMode.md`'s
"Architecture decision (2026-10-18)" and its "Stage 2" plan) - no
`tview`/`tcell`/`uikit` dependency at all, same boundary
`internal/debugcopy`/`internal/debugrun` already keep. `debug` used to be
a second, standalone TUI of its own (M3: its own `debugApp`/`tui.go`/
`helptables.go`, since removed); it's now built as an ordinary
`internal/session` `liveSession` instead, with breakpoint/pause state
layered on top - this package is exactly the glue that makes that
possible, and `internal/session` (`main.go`'s `VerbDebug` case,
`livesession_debug.go`) is its only caller.

- **debugverb.go** - `PrepareSession`: resolves "tangsible debug"'s own
  arguments (rejects `--step` outright - a second, unhandled pause
  mechanism; extracts its own `--all-vars`/`-a` via
  `extractAllVarsFlag`), builds the shadow copy + `debugger:
  always` splice (`internal/debugcopy`), and probes the spliced copy for
  its task skeleton (`runner.BuildProgressSkeleton`) - everything up
  through having a real, spawnable playbook ready, returned as
  `SessionPrep` for `internal/session` to build a `liveSession` around.
- **cursor.go** - `SeedState` pre-builds the whole predicted
  `*playbook.PlaybookState` directly from `PrepareSession`'s own skeleton
  (so breakpoints can be armed before anything has run) - deliberately
  never through `PlaybookState.Apply`, which can't reuse those pre-built
  nodes at all once a real run starts (see its own doc comment for why
  not). `Cursor.Apply` resolves each live jsonl event against that same
  skeleton-seeded state instead, via the same name-collision-safe
  `runner.ProgressTracker` forward-matching the progress bar already
  uses, writing straight into the right pre-built `*playbook.TaskNode`.
  `TaskLabel` is a task's own breakpoint-set key (matching the debugger's
  own prompt text); `DebugConfig` bundles everything `internal/session`'s
  `NewLiveTUI` needs from this package (`BPS`, `Cursor`,
  `AllVars`, ...) into one parameter.

## internal/runner

The "how does one ansible-playbook generation actually run" mechanism -
spawn, stream, track progress, record outcome. Shared by the live
run/rerun/role session and by revisit's own rerun.

- **process.go** - `ProcHandle`/`StreamItem`/`GenerationOutcome`,
  `SpawnGeneration`/`ScanEvents`/`StreamStderr`, exit-code handling.
- **outputlog.go** - `OutputLog`: one generation's console output
  (the user's stdout callback merged with stderr) for the `o` view,
  plus `StripANSI`/`SplitANSI`.
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
- **inventory.go** - the "inventory" verb: a collapsible tree view of the
  whole inventory (groups as nodes, hosts as leaves), reusing host.go's
  own stub-playbook/ping/five-tab detail machinery to open a host.
- **helptables.go** - shared by all three verbs: help tables and bottom
  bars.

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
  - **helptables.go** - the live session's shortcut tables
    (tree, drill-down, `o` view, re-run dialog) and, next to each, its
    bottom bar string - the place to edit a binding's help text and bar
    hint. Every other verb package has its own `helptables.go` the same
    way (revisit, diff, host, template).
  - **livesession_stdout.go** - the raw-output view (`o`):
    `openStdoutView`/`syncStdoutView`/`handleStdoutViewKey`, following
    and holding lines back during a search.
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
  - **livesession_debug.go** - everything specific to `debugMode`
    (`design-docs/DebugMode.md`): `handleDebugKey`
    (F5/F9/F10/F11/F12, `livesession_input.go`'s own dispatcher calls into
    this one), `debugToggleBreakpoint(For)`/`debugTaskAtFirstColumn` (F9 and
    its mouse equivalent), `debugStartOrContinue` (the orchestration
    goroutine driving `debugrun.Spawn`/`RunWithBreakpoints`/
    `ContinueWithBreakpoints`, reporting back via `QueueUpdateDraw`),
    `debugRedo` (F12, drives `debugrun.RedoUntilTarget`),
    `debugOnPause`/`debugCaptureSnapshot`/`mergeRegisteredResult`
    (captures `task_vars`, plus the paused task's own `register:` result
    if it has one, into `debugTaskVarsCache` - skipped for a pause
    `willSurface` says won't be shown, unless `--all-vars`/`-a` is set),
    `debugSetWatch` ("Stage 5," the watch panel's `expr = value` action -
    same `debugDriving`-guarded background-goroutine shape as
    `debugRedo`, re-running `debugCaptureSnapshot` on success to refresh
    the cache with the newly-set value), `debugApplyPauseResult`/
    `debugFinish` (mirrors `runner.RunOneGeneration`'s own "exitCode/
    lastStderr before processDone" ordering; also where the watch panel's
    own `justSet` marker is cleared), `debugChromeStyle`/
    `debugChromeBg`/`debugRefreshChrome` (the two extra chrome states on
    top of the four plain-run ones).
  - **livesession_watch.go** (+ `_test.go` is `debugwatch_eval_test.go`
    below, not this file) - `watchPanel`, design-docs/DebugMode.md's
    "Stage 4" variable watch explorer: `newWatchPanel`/`render`/`height`
    (the panel's own two tview primitives, inserted as a new trailing
    Flex item in `s.flex`/`s.outputFlex`/`s.outputBody` - never
    `s.treeBody`, the spec's own "bottom of the right pane" scope),
    `watchCurrentTaskHost`/`watchCurrentSnapshot` (reuses
    `showOutputWithOrigin`'s own `debugTaskVarsCache` lookup for whatever
    host/task is currently selected, not fixed to wherever a watch was
    added), `watchCandidates`/`watchMatchCandidates` (autocomplete,
    formatted `"name = value"` since tview's native drop-down has no
    second column), `handleWatchKey`/`handleWatchOpenKey` (the `v`-opens/
    modal-while-open key state machine, including the Esc-staging rules
    design-docs/DebugMode.md's own UX section spells out),
    `watchAttemptSet`/`markJustSet`/`clearJustSet` ("Stage 5": the
    `expr = value` gate - requires `debugIsPaused` against
    `watchCurrentTaskHost`'s own resolution - and the `(just set)` row
    marker).
- **debugwatch_eval.go** (+ `_test.go`) - `parseWatchExpr`/
  `evaluateWatchExpr`, a tiny hand-written grammar/evaluator (not
  `resolve_condition.py`'s jinja2-AST mechanism, deliberately: that
  one's "echo back unchanged on any failure" contract can't distinguish
  "doesn't parse" from "doesn't resolve," which the watch panel's own
  reject/unavailable/resolved 3-way split needs) for a bare name or a
  dotted/bracket-index path (`foo.bar`, `foo[0]`) against a captured
  `task_vars` snapshot - pure, no `tview` import, pulled out of
  `livesession_watch.go` for the same "testable in isolation" reason
  `uikit.FilteredMatch` is its own file. Also `splitWatchSetAssignment`/
  `watchSetTarget` (design-docs/DebugMode.md's "Stage 5"): recognizing the
  watch panel's `expr = value` form and rendering a parsed `watchExpr` as
  the Python assignment target `internal/debugrun.Session.Set` writes
  into the live debugger's `task_vars`.
- **recap.go** - the post-run recap summary section.
- **resolved.go** - the drill-down's "Resolved" tab (re-renders a task
  with variables filled in, via a real `ansible.builtin.template`
  task).
- **resolve_condition.go**/**resolve_condition.py** - resolves the
  variable references within a task's `when:`/`failed_when:`/
  `changed_when:`/`until:` conditions (debug mode only, called from
  `resolveTaskValuesFromSnapshot` above): a real Jinja AST walk (the
  `jinja2` library ansible itself depends on, run as `python3 -c
  <go:embed'd script>`), substituting only variable-reference subtrees
  that fully resolve - every operator/filter/test/literal passes
  through untouched.
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
