# Restructuring

## Situation

The whole codebase lives as one flat `package main` in the repo root -
58 `.go` files (26 non-test), ~19,400 lines total. That's gotten hard to
navigate, and the concern isn't just file count: `tui.go` alone is 5082
lines, and everything else (`diff.go`, `host.go`, `template.go`,
`revisit.go`) reaches directly into its private helpers rather than
through any real boundary.

The instinct that prompted this doc was "Go projects have an `internal/`
directory for everything that isn't part of the public API - shouldn't
we use one?" Worth being precise about what that buys here before
planning around it: Go's `internal/` rule stops *other modules* from
importing a package. A `package main` can never be imported by anything,
ever, regardless of which directory it lives in - so moving today's
files into `internal/` as-is, unchanged, would add a directory level and
buy zero actual enforcement. The convention applies to projects with an
importable API surface; a single-binary CLI doesn't have one until it's
actually split into real, separately-named packages. So "restructure
into `internal/`" and "restructure `tui.go`" turn out to be the same
underlying problem, not two independent ones - real package boundaries
are what both need.

`cmd/<binary>/main.go` was considered and rejected: that pattern earns
its keep when a module produces multiple binaries, each getting its own
subdirectory under `cmd/`. Tangsible only ever produces one. For a
single-binary module the simpler, equally idiomatic shape is a thin
`main.go` at the repo root (`package main`) importing whatever top-level
package the real logic ends up in - no `cmd/` needed.

## What the code actually looks like (checked, not assumed)

Before planning further, `tui.go`'s real shape was checked directly
(`grep -n "^func \|^type \|^var \|^const "`, plus a look at what each
other file reaches into) rather than guessed from its line count alone.
Two findings drive everything below:

- `tui.go` is ~100 top-level declarations, most of them normal-sized
  functions - but one of them, `NewLiveTUI`, is by itself roughly 2430
  lines (line 809 to line 3240 - not quite half the file). It's built
  from ~35 closures (`rebuild`, `showOutput`, `applyFilter`,
  `handleRight`/`handleLeft`, `navigateMainTask`, `submitRerun`, etc.),
  all capturing a shared pool of ~30 local variables (`currentID`,
  `following`, `viewingOutput`, `splitMode`, `expanded`, `outputTask`,
  and so on). That closure-over-shared-mutable-state shape - not "the
  file has a lot of lines" - is the real structural problem.
- `diff.go`, `host.go`, and `template.go` all reach directly into
  `tui.go`'s unexported helpers already - `diff.go` alone has 37
  references to things like `buildOutputTabs`, `taskAction`, `colorTag`,
  `sectionLabel`, `pureBlack`. `revisit.go` and the rerun dialog already
  avoid a package-boundary problem the right way, though: `NewLiveTUI`
  takes `revisitReturn func()` and `requestRerun func(...)` as injected
  callback parameters rather than `tui.go` calling into `revisit.go` by
  name. `diff.go` is the one exception - `tui.go`'s `'d'` key handler
  calls `runDiffFlow` directly. That's the only hard import-cycle risk
  found so far, and it has a small, already-precedented fix: make it a
  callback parameter too, matching `requestRerun`/`revisitReturn`.
- The core event/aggregation model (`playbookState`/`taskNode`/
  `playNode`/`rawEvent`, `aggregate.go`/`events.go`) is used by every
  other file in the project and depends on nothing else in it - already
  a clean, naturally isolated boundary.

## The plan

Phases are ordered by risk/effort, each independently valuable - it's
fine to stop after any of them and reassess rather than committing to
the whole thing up front.

### Phase 0 - split `tui.go` into several files, same package, same directory

Purely mechanical: move top-level declarations into new files, all
staying `package main`, all in the repo root. Go doesn't care how many
files a package spans, so this changes zero behavior - verified entirely
by `go build`/`go vet`/`go test`/`gofmt -l` after each cut, no manual
testing needed. Proposed grouping, based on the declarations actually
found:

- Layout/row-rendering: `topBarText`/`composeTopBarLine`/
  `composeSplitHeaderLine`/`progressFill*`/`truncateHostsList`/
  `computeHostColumnLayout`/`taskLabel`/`hostLabel`/`playRowText`/
  `colorTag`/`summaryFieldColor`/`hostSummaryColoredText`/
  `hostTransition`/`splitTreeWidth`.
- Filtering: `filterMode`/`filterQuery`/`taskVisible`/
  `taskMatchesSearch`/`taskOutputText`/`visibleTasks`/
  `visibleTasksForHost`/`nearestVisibleTask`/`firstVisibleTask`/
  `taskSet`.
- Drill-down tab building: `buildOutputTabs` and the whole `buildXTab`
  family, `sectionLabel`, `colorizeYAML`, `primaryOutputField`,
  `outputSummary`, `skipDetail`, `buildDiffTab`/`colorizedUnifiedDiff`.
- Dialogs/modals: `centeredModal`, `filterDialogText`, `inRect`.
- Palette/style constants: `colorTag`'s own color names, `pureBlack`,
  `taskIndent`/`hostIndent`, the bottom-bar text constants.
- `NewLiveTUI` itself (and its ~35 closures) stays in `tui.go` for
  now - still one big file, but clearly labeled and no longer
  surrounded by everything else.

This alone turns "one unnavigable 5082-line file" into roughly 7 files
of a few hundred lines each, and is the lowest-risk way to relieve the
specific pain that prompted this doc. Worth doing first regardless of
how far the later phases go, since it's also a prerequisite for seeing
the package boundaries in Phase 1 clearly.

### Phase 1 - extract a shared rendering toolkit as its own package

The pieces `diff.go`/`host.go`/`template.go` reach into aren't really
"the live tree's own internals" - they're generic UI machinery
(`treeList`, `centeredModal`, the color palette/selected-row convention,
`sectionLabel`, the whole drill-down tab-building family). Moving that
into its own package (`internal/uikit` or similar working name) fixes
the coupling architecturally rather than just capitalizing names, and
resolves the `runDiffFlow` cycle for free as a side effect - `tui` and
`diff` can both depend on this toolkit without depending on each other.
Moderate mechanical effort: every symbol this package exposes needs to
go from unexported to exported, repo-wide, then re-verified.

**Revised before starting (see Phase 2's own note below): this phase
turned out to depend on Phase 2, not the other way around** - most of
`tui_layout.go`/`tui_rows.go`/`tui_filter.go`/`tui_drilldown.go` take
`*playbookState`/`*taskNode`/`outcome`/`taskSourceIndex` directly as
parameters or return values, which a standalone package can't do until
those types themselves live somewhere importable by both `main` and the
toolkit. Phase 2 now runs first for exactly this reason. Once Phase 2
is done, this phase proceeds as originally scoped, with `internal/
playbook`'s exported types available to depend on.

### Phase 2 - extract the core data model into its own package

`aggregate.go`/`events.go` (`outcome`/`taskNode`/`playNode`/
`playbookState`/`rawEvent`/`playRef`/`taskRef`/`hostResult`) into
`internal/playbook`. Already a clean, naturally isolated boundary per
the check above - nothing else needs to change conceptually, just
exporting the fields other files touch directly (a few of these already
have an accessor for exactly this reason - `playbookState.CurrentTask()`
exists specifically so `tui.go` doesn't reach into the private
`currentTask` field directly, per its own doc comment - so some of this
groundwork already exists).

**`recap.go` dropped from this phase's scope, unlike this section's
original text above** - checked directly (grep, not assumed) before
starting: `recap.go` calls `colorTag`/uses `grayTag`/`pureBlack`/the
`row` type throughout (`flattenRecapRows`, `recapHostRowText`, etc.) -
that's real UI-rendering coupling to the *other* toolkit-candidate
files (`tui_layout.go`/`tui_style.go`/`tui_rows.go`), not "depends on
nothing else" the way `aggregate.go`/`events.go` do. Moving it now would
either drag `tui_layout.go`/`tui_style.go`/`tui_rows.go` into
`internal/playbook` too (wrong boundary - those are rendering, not data
model) or leave `recap.go` half-migrated. Left in `package main` for
now; it belongs with the Phase 1 UI-toolkit cluster conceptually, once
that phase's own data-model dependency (this phase) is resolved.

### Phase 3 - break `NewLiveTUI` itself apart (the big one, likely later/separate)

Turn its ~35 closures into methods on a real struct (e.g.
`type liveSession struct { app *tview.Application; list *treeList;
currentID any; following bool; viewingOutput bool; splitMode bool;
... }`), each closure becoming `func (s *liveSession) rebuild() { ... }`
and so on. This is where the actual risk lives - 2430 lines of closures
capturing shared mutable state is a rewrite, not a move, with real
potential for subtle bugs around capture timing/ordering that a
mechanical move can't introduce. Treated as optional and separate from
"fix the navigability problem": Phases 0-2 already make the codebase
legible even if `NewLiveTUI` stays one big (but now isolated, clearly
labeled) file. Worth doing eventually, but shouldn't block the rest, and
should lean heavily on the existing test suite and go incrementally
rather than all at once if/when it happens.

### Phase 4 - group the verb files into their own packages under `internal/`

Once Phases 1-2 give them something clean to depend on: `diff.go`/
`diffmatch.go`/`diffresolve.go`, `revisit.go`/`revisitresolve.go`/
`generation.go`, `host.go`, `template.go`, `role.go`, and
`resolve.go`/`history.go`/`rerunargs.go`/`rerunresolve.go`/`runlog.go`
each into their own package. This is the point where "too many files in
one directory" actually gets resolved, and where a real `internal/`
boundary starts paying for itself - by this point there are genuine
importable packages worth protecting from some hypothetical future
second binary in this module, not just `package main` renamed for its
own sake.

**Revised once checked directly (grep, not assumed) before starting -
this scope turned out optimistic in two ways:**
- `host.go`/`template.go` are a genuine two-way cycle - both call each
  other's inventory-JSON-parsing helpers (`flattenInventoryHosts`/
  `ansibleInventoryGroup` from `host.go`, `listInventoryHosts` from
  `template.go`). Can't become two independent packages as proposed
  without either merging them or extracting the shared inventory logic
  into a third package both depend on - not decided yet, see Status.
- `main.go` isn't just an entrypoint - it owns process-lifecycle
  plumbing (`procHandle`/`streamItem`/`generationOutcome`/`exitCodeOf`/
  `ansibleUserInterruptedExitCode`/`scanEvents`/`spawnGeneration`/
  `streamStderr`), plus `progress.go`'s tracker and `source.go`'s
  `taskSourceIndex`, that `diff.go`/`revisit.go`/`generation.go` call
  *directly*, not via callback. Since `package main` can never be
  imported, none of that can stay main-only once those three files
  move out - it needs its own package(s), not named in this phase's
  original scope above. `revisit.go`'s `openRevisitEntry` also calls
  `NewLiveTUI` (`tui.go`, staying `package main` until Phase 3) directly
  - that one has to become an injected callback, the same pattern
  `requestRerun`/`revisitReturn` already use elsewhere in this codebase.
  Not decided yet either - see Status.

Given the scope growth, this phase is now being done incrementally
rather than as one pass - safe, confirmed-zero-back-reference clusters
first, the harder two (above) once there's a concrete proposal for
them. See Status for what's done and what's still open.

### Phase 5 - thin root `main.go` (trivial, any time once Phase 4 exists)

`main.go` (`package main`) at the repo root, importing whatever
top-level package Phase 4 leaves as the entry point. No `cmd/`
subdirectory - single binary, so it doesn't earn its keep here.

## Status

Phase 0 done. `tui.go` (was 5082 lines) is now split, same package/
directory, into `tui.go` (2537 lines - just `NewLiveTUI` and its ~35
closures, per this phase's own decision to leave that one alone for now)
plus six new files grouped by the declarations actually found:
`tui_layout.go` (920 - row-text rendering: `taskLabel`/`hostLabel`/
`playRowText`/`colorTag`/`computeHostColumnLayout`/top-bar composition),
`tui_drilldown.go` (1018 - `buildOutputTabs` and the whole `buildXTab`
family, `sectionLabel`, `colorizeYAML`, diff rendering), `tui_filter.go`
(314 - `filterQuery`/`taskVisible`/search matching/task-navigation
helpers), `tui_rows.go` (188 - `row`/`flattenRows`/status-row text),
`tui_style.go` (152 - palette/style constants), `tui_dialogs.go` (81 -
`centeredModal`/`filterDialogText`/`inRect`). Extraction was AST-driven
(each declaration cut by its own exact source range, doc comment
included) rather than manual, so nothing was dropped, reordered within
its group, or reformatted beyond `goimports` reconciling each new file's
own import list. `go build`/`go vet`/`gofmt -l`/`go test ./...`/
`go test -tags e2e ./...` all pass unchanged.

Phase 2 done next, ahead of Phase 1 (see that phase's own revised note
above) - `aggregate.go`/`events.go` now live in `internal/playbook`
(`package playbook`), with every symbol another file touches exported:
`Outcome`/`OutcomeOK`/.../`OutcomeUnreachable`, `TaskNode` (+ `Counts()`,
exported from `counts()` since `tui_layout.go`/`render.go` call it
externally), `PlayNode`, `PlaybookState` (+ `Reset`/`Apply`/
`CurrentTask`), `RawEvent` (+ `Timestamp()`), `PlayRef`, `TaskRef`,
`HostResult`, `DecodeHostResult`. `recap.go` stayed in `package main` -
see Phase 2's own note above for why.

Mechanics: renamed in place first with `gorename` (semantic, scope-aware
- safe against the English word "outcome" appearing throughout comments
where the identifier `outcome` does not), then moved the two files and
re-qualified every remaining cross-package reference with a small
`go/packages`+`go/types`-driven tool (matches call sites by their
resolved `types.Object`, not text matching, so it can't confuse e.g. a
struct's `Name`/`Path` fields with the moved types). One real snag:
`playbook` collides with the pervasive local-variable name for "which
playbook file to run" in `main.go`/`revisit.go` specifically (nowhere
else) - fixed with a `pb` import alias in just those two files, plain
`playbook.` everywhere else. Went back through by hand afterward and
capitalized the now-stale lowercase type-name mentions left in
unrelated files' doc comments (`taskNode` -> `TaskNode` etc., ~14 files)
- deliberately left the bare word "outcome" alone wherever grep found it
in prose ("recording its outcome," "colored by its outcome") since
every one of those is the ordinary English word, not the type.
`aggregate_test.go`/`events_test.go` moved to `internal/playbook/` too
(package `playbook`, matching); `recap_test.go` kept its own copies of
the three tiny `playStartEvent`/`taskStartEvent`/`hostResultEvent` test
helpers, since Go test helpers aren't visible across a package boundary
and it was the only other file using them. `go build`/`go vet`/
`gofmt -l`/`go test ./...`/`go test -tags e2e ./...` all pass on both
packages.

Phase 1 done. `internal/uikit` (`package uikit`) now holds the pieces
`diff.go`/`host.go`/`template.go`/`recap.go` reach into: `treelist.go`,
`tabs.go`, and the whole Phase 0 grouping - `tui_style.go`,
`tui_layout.go`, `tui_rows.go`, `tui_filter.go`, `tui_dialogs.go`,
`tui_drilldown.go` - moved there wholesale rather than split more
narrowly. `tui_rows.go`/`tui_filter.go` weren't in this phase's
original named list (`treeList`/`centeredModal`/palette/`sectionLabel`/
the tab-building family), but turned out to belong: `diff.go`/`host.go`/
`template.go` already reach directly into `taskLabel`/`row`/`hostRowID`/
`barStyle`/`treeList`/`tabbedPane` (confirmed by grep before starting,
not assumed), and `FlattenRows` (`tui_rows.go`) itself calls
`TaskVisible`/`FilterQuery` (`tui_filter.go`) directly - moving one
without the other would have meant inventing an interface layer just to
avoid a five-file mechanical move. `tui.go` (just `NewLiveTUI`) and
`recap.go` are the only tree/row-rendering-adjacent files left in
`package main`, per Phase 0's/Phase 2's own prior decisions.

Every symbol another file touches was exported (~115 across the 8
files, plus struct fields where external code constructs them: `Row`'s
`Text`/`Selected`/`ID`, `HostRowID`'s `Task`/`Host`, `FilterQuery`'s
`Mode`/`Search` - found by grepping for keyed/positional composite
literals of each type from outside its own file, not guessed).
`treeListRow`/`TabbedPane`'s and `TreeList`'s own internal fields
(`rows`/`currentItem`/`itemOffset`/`header`/`pages`/`root`/`names`/
`active`) stayed unexported - confirmed via the same grep that nothing
outside `tabs.go`/`treelist.go` ever touches them directly, only through
methods. One method needed exporting despite looking file-local at
first glance: `TreeList.restoreCurrentItem` -> `RestoreCurrentItem`,
called directly from `tui.go`.

Two real cross-package-boundary snags, both fixed by removing the
dependency rather than dragging more code into the toolkit:
- `tui_rows.go`'s `StatusRowText`/`GenuineFailure` read `main.go`'s
  `ansibleUserInterruptedExitCode` constant directly - not a rendering
  concern this package should own. Both gained a third
  `userInterruptedCode int` parameter instead; callers
  (`tui.go`/`main.go`/the moved test file) pass
  `ansibleUserInterruptedExitCode` (or a literal `99` from within the
  test, once it no longer has access to that constant by name) at the
  call site.
- `FlattenRows`/`TaskVisible`/`TaskMatchesSearch`/`VisibleTasks`/
  `VisibleTasksForHost`/`BuildOutputTabs`/`BuildSourceTab` all took
  `source.go`'s `taskSourceIndex` (a `package main`-only named type) as
  a parameter. Since it's just `map[string]string` with zero
  dependencies of its own, every one of those signatures now takes
  `map[string]string` directly instead - Go's assignability rules make
  a `taskSourceIndex` value (or any named type with that same
  underlying type) pass through with no conversion needed at any call
  site, so `source.go` itself didn't have to move.

Mechanics followed Phase 2's pattern: `gorename` for every in-place
symbol/field rename (including struct fields this time - confirmed
`-from '"pkg".Type.field'` works there too, not just for methods/types),
verified by a full build after each file's batch; then the same
`go/packages`+`go/types` qualifying tool from Phase 2, generalized to
target all ~115 symbols across the 8 files at once and to skip both the
8 production files and their corresponding 9 test files (which move
into the new package essentially unchanged, needing no `uikit.` prefix
of their own). One tool bug caught before it mattered: `go/packages`
with `Tests: true` synthesizes a `_testmain.go` living under
`$GOCACHE`, and the qualifier's first pass over "every file the loader
returns" briefly wrote into that shared, content-addressed cache before
this was caught - fixed by restricting writes to paths under the repo
directory, and `go clean -cache` run once as a precaution (a build being
slower once afterward is the only possible side effect; the cache is
regenerated content, never inputs). Test files that constructed
`taskSourceIndex{...}` literals or referenced `ansibleUserInterruptedExitCode`
by name were fixed by hand before the move (literal `map[string]string{...}`/
`99` respectively), for the same reason as the two snags above - once
moved, neither name is reachable from `package uikit`.

Stale lowercase mentions of the renamed symbols in doc comments
(`taskLabel` -> `TaskLabel` etc.) were swept afterward across every file
*outside* `internal/uikit`/`internal/playbook` - about 100 occurrences
across 16 files, all comment/string text (every real code reference was
already caught by `gorename`, exhaustively, before the move - anything
left over could only be prose). The single ambiguous case, `row`, was
deliberately left alone throughout: unlike Phase 2's "outcome", which
was checked occurrence-by-occurrence and found to be 100% prose, `row`
has ~186 hits outside the moved files and is genuinely used both ways
often in the same sentence ("the row under the cursor") - capitalizing
it correctly would need real per-occurrence judgment, not a blanket
pass, and was judged not worth the effort for a case where the prose
and code meanings coincide anyway. `go build`/`go vet`/`gofmt -l`/
`go test ./...`/`go test -tags e2e ./...` all pass on all three
packages.

The `runDiffFlow` cross-file call (`tui.go` -> `diff.go`) noted as a
future import-cycle risk when this doc was first written is still
unresolved and still fine to leave that way: `diff.go` stays in
`package main` in this phase (it's app-level "diff mode" logic, not
generic UI machinery), so calling directly within the same package was
never actually a cycle - only Phase 4 (splitting `diff.go` into its own
package) would make it one, and that's the point at which the
`requestRerun`/`revisitReturn`-style callback-injection fix belongs.

Phase 4 started, incrementally, after research (grep, not assumed)
found its original 6-group scope had two real problems - see that
phase's own revised note above. Decision made: do the two confirmed-
clean clusters first, come back with a concrete proposal for the other
two once those are done and verified, rather than redesigning all ~9
eventual packages up front.

**`internal/config`** (`resolve.go`/`history.go`/`rerunargs.go`/
`rerunresolve.go`/`runlog.go`) done - confirmed zero back-references
into anything else before starting (stdlib + `github.com/BurntSushi/
toml` only), so this moved exactly as cleanly as the doc assumed. Every
cross-file symbol exported (`SettingsConfig`, `StateConfig`, `Verb`/
`VerbRun`/etc., `ResolvePlaybook`, `AppendInvocation`, `ResolveRerun`,
~45 more) - struct fields (`InvocationRecord`/`PlaybookHistory`/
`ParsedPassthroughArgs`/`RerunResolution`) were already exported before
this phase, so no field-level renaming was needed here the way Phase 1
needed for `Row`/`HostRowID`/`FilterQuery`. No import-alias collision
despite "config" being about as common a word as "playbook" was in
Phase 2 - checked directly (grepped for `config` as an actual local-
variable declaration, not just the substring inside `configPath`/
`config.toml`) and found none.

**`internal/role`** (`role.go`) done, and turned out even smaller than
expected: only `StartRoleSession` is ever called from outside the file
(`main.go`, `revisit.go`) - `WriteRoleStub`/`RoleFoundNearby`/
`RoleStubFilename` stay effectively private (still exported, per this
phase's "export everything in the moved file" policy from Phase 1, but
with zero outside callers) since `StartRoleSession` is the only thing
that calls them. Zero dependency on the config cluster despite the
doc's own earlier "role -> config" flag from initial research - that
turned out to be `main.go` calling into *both* packages side by side,
not `role.go` itself depending on `config`.

Mechanics for both: identical to Phases 1-2 - `gorename` in place
first, then move + `goimports`, verified after each cluster before
starting the next (config fully moved and re-verified before role's own
qualifying pass ran, since the qualifier needs semantic resolution and
`role.go` doesn't reference anything in `config` anyway, but doing them
strictly in sequence rather than batching both avoided finding that out
the hard way). `go build`/`go vet`/`gofmt -l`/`go test ./...`/
`go test -tags e2e ./...` all pass on all five packages now.

The remaining two pieces (host/template's cycle, and where `main.go`'s
process-lifecycle plumbing ends up) were proposed and then done as two
more increments, in the same session:

**`internal/inventory`** breaks the `host.go`/`template.go` cycle by
extracting the one thing they actually share - the `ansible-inventory
--list` JSON client - rather than merging two already-large files
(1314/661 lines) together. `AnsibleInventoryGroup` (type) and
`FlattenInventoryHosts` moved out of `template.go`; `ListInventoryHosts`
moved out of `host.go` (it also called the former, and `host.go`'s own
`hostGroupChain` used the type directly - both fixed to call
`inventory.*`). `host.go`/`template.go` then became genuinely
independent packages, each importing `inventory` instead of each other.
`TestFlattenInventoryHosts` (previously in `template_test.go`) moved
with the code it tests, into `internal/inventory`'s own test file.

**A real dependency surfaced only once `internal/host` tried to build
standalone**, not caught by the earlier research: `FetchHostPlays`
(host.go) calls `progress.go`'s `parseListTasksOutput` directly, for the
"Plays" tab. Since `progress.go` was already slated for the
process-lifecycle package, this just meant doing that part of the
runner extraction earlier than planned rather than changing the design
- `progress.go` moved into `internal/runner` first (on its own,
self-contained, no other change needed), then `host.go`/`template.go`
finished moving into `internal/host`/`internal/template`.

**`internal/runner`** (rest of it): `main.go`'s
`AnsibleUserInterruptedExitCode`/`ExitCodeOf`/`ProcHandle`/
`PendingGeneration`/`GenerationOutcome`/`SpawnGeneration`/
`StartFirstGeneration`/`StreamStderr`/`StreamItem`/`ScanEvents`, cut out
of `main.go` (which shrank to just `main()` - 380 lines, imports and all
- plus its own `import` block) into a new `process.go`, and
`generation.go` (`RunOneGeneration`/`NewRequestRerun`) moved in
wholesale alongside it and `progress.go`. `main_test.go` - which turned
out to test only the process-lifecycle pieces (`TestProcHandle`/
`TestExitCodeOf`/`TestStreamStderr`/`TestScanEvents*`), nothing of
`main()` itself - moved to `internal/runner/process_test.go` to match.
`PendingGeneration`/`GenerationOutcome`'s own fields needed exporting
too (`Cmd`/`StdoutCh`/`StderrLines`/`First`/`RunID`/`ExitCode`/`WaitErr`/
`ChildStderr`) - `main.go`'s own body reads them directly, and it's the
one file in this whole phase that couldn't just move into the package
alongside its dependencies, being the actual `package main` entrypoint.
`StreamItem`'s own fields (`Ev`/`IsEvent`, from `ev`/`isEvent`) needed
the same treatment for the same reason. `main.go`/`generation.go` both
already used a `pb` import alias for `internal/playbook` (the same
"playbook" local-variable collision from Phase 2) - carried forward
into `internal/runner`'s own two files unchanged. `source.go` stays
untouched, in `package main` - nothing about this round's moves needed
it to go anywhere.

`go build`/`go vet`/`gofmt -l`/`go test ./...`/`go test -tags e2e ./...`
all pass on all nine packages Phase 4 now leaves: `main` (thin - just
`main()`, `tui.go`'s `NewLiveTUI`, `recap.go`, `diff.go`/`diffmatch.go`/
`diffresolve.go`, `revisit.go`/`revisitresolve.go`, `source.go`) plus
`internal/config`/`role`/`playbook`/`uikit`/`inventory`/`host`/
`template`/`runner`.

Not done, deliberately out of scope for Phase 4 as it stands: `diff.go`/
`revisit.go`/`revisitresolve.go` still live in `package main` - splitting
them further would hit the `revisit.go` -> `NewLiveTUI` direct-call
problem flagged when Phase 4 started (needs the same callback-injection
treatment as `requestRerun`/`revisitReturn`), which nothing in this
session's work required solving. Phase 3 (break `NewLiveTUI` itself
apart) stays deferred per its own section above.
