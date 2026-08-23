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

### Phase 2 - extract the core data model into its own package

`aggregate.go`/`events.go`/`recap.go` (`playbookState`/`taskNode`/
`playNode`/`rawEvent`) into `internal/playbook` or similar. Already a
clean, naturally isolated boundary per the check above - nothing else
needs to change conceptually, just exporting the fields other files
touch directly (a few of these already have an accessor for exactly
this reason - `playbookState.CurrentTask()` exists specifically so
`tui.go` doesn't reach into the private `currentTask` field directly,
per its own doc comment - so some of this groundwork already exists).

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

### Phase 5 - thin root `main.go` (trivial, any time once Phase 4 exists)

`main.go` (`package main`) at the repo root, importing whatever
top-level package Phase 4 leaves as the entry point. No `cmd/`
subdirectory - single binary, so it doesn't earn its keep here.

## Status

Not started. Phase 0 is the proposed next step whenever this gets picked
back up - lowest risk, most immediate relief for the `tui.go` size
concern that prompted this doc.
