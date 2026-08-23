# Diff

This is based in part on the existing revisit functionality

## Situation

When debugging playbook one frequently runs the same playbook multiple times.
In this case the important things are what's different. This potentially
means a lot of scrolling and one has to remember the outcome of the previous
run. With tangsible revisit one could put two windows side by side but it's
still painful.

## Idea

Provide some kind of functionality comparable to that of the well-known diff
utility which in turn will only visualize things that are different between
two runs.

## User interaction

* User runs a playbook normally with tangsible run - or alternatively
  revisits a previous run with tangsible revisit.
* Use then presses a hotkey, e.g. d
  * d can only be pressed from the tree view
  * d hotkey is only available when the current session has finished -
    similar to the r hotkey
  * d can be pressed both after a real playbook run or when revisiting a run
* A list similar to the one that opens when calling tangsible revisit opens.
  * it will only show previous runs that match the current one
  * i.e. it does not make sense to diff two different playbooks, two
    different tags or a different set of hosts
* The user can now select another old run which should be used to perform the
  diff to the current one
* Tangsible computes the difference and opens a treeview (with yet another
  coloring so it's clear we are in diff mode). This tree looks similar to the
  standard tree view in general but will only show plays / tasks that with
  differences between the two runs.
* When the user drills down, the drill down view will also highlight the
  differences

## What counts as "different"

* Different outcome (e.g. OK->Failed)
* Different output (stdout, stderr, warning)
* I wouldn't count a difference in hosts between runs as difference because
  then we would have show every task as different. Would defeat the purpose.

## Matching tasks from two different runs if the two lists don't exactly match

To be honest, I haven't really thought about this. LCS sounds good. Let's see
how this turns out

For tasks that are only in one run I would propose
* Only present in new run: underline the whole task line
* Only present in old run: strike through the whole task line

## Visualization

### Tree view

* The whole tree shall be colored as normal, visualizing different outcomes
  etc. In the tree view differences are only visualized by underlining.
* Only show plays that contain tasks with differences between the two runs
  * But don't render a play line differently
* Only show tasks with differences between the two runs
* Render the task line based on the new current version but underline those
  hosts that are different in the version it's compared to
  * i.e. when a host changes from "Failed" to "OK" - underline "OK"
* Host line: show the version from the new run, underline the whole line if
  there was a change
* When the user presses esc / q in the tree view, the diff treeview is closed
  and the user is back at the run list. Hitting esc / q again brings them
  back to the standard tree view.

### Drill down view

* Leave the "Docs" tab as is
* Drop the "Diff" - too confusing
* Output in all other tabs a unified diff of the outputs of both runs for that tab.
* Drop any coloring instead use red / green to color the differences (similar
  to how other diff utils do it)

## Answers to open questions

**Play/task matching**: your call - match by task *name* (within a play),
not by `task.Path`. Path looks more precise but is actually worse here:
editing the playbook between debug runs (the whole point of this feature)
shifts line numbers for everything below the edit, even for tasks that
didn't change themselves - path-matching would misread that as "this task
no longer exists" and cascade into spurious mismatches. Worse, a role-
originated diff would *never* match anything by path at all - every role
session gets its own freshly generated stub at a new temp path
(`startRoleSession`), so paths would differ even with zero real changes.
Name-based matching risks the opposite failure (two same-named tasks in one
play misaligning), but `SequenceMatcher`'s own alignment leans on
surrounding position, not just raw equality, so it tends to do the sensible
thing even with some repeats - same "documented heuristic, not chased
further" tradeoff this codebase already makes elsewhere (`taskLabel`'s
truncation, `primaryOutputField`'s stdout-vs-msg choice).

Plays get the *same* treatment, not special-cased - which makes "a whole
play got added/removed" fall out for free: if a play only exists in one
run, none of its tasks have a counterpart, so each one already gets the
existing "only in new/old run" treatment on its own - which is exactly what
makes the play itself qualify for "contains tasks with differences" and
show up, rendered normally. No separate "play was added" code path needed.
In the 95% no-playbook-change case this is a trivial 1:1 match, so it costs
nothing in the common case.

**Candidate-run list filtering**: exact match on tags/hosts (not overlap -
a run with `--tags foo` is not offered against a current `--tags foo,bar`
session), and only playbook+tags+hosts are checked - other passthrough args
(`-e`, `-i`, `--forks`, `-vvv`, ...) are ignored, exactly as originally
listed.

## Implementation (proposed)

### Matching engine (`diffmatch.go`, pure, no I/O)

`github.com/pmezard/go-difflib/difflib` - already a dependency, already
used for `buildDiffTab`'s own module-level before/after diffs - is reused
here in a new context: `difflib.NewMatcher(oldNames, newNames
[]string).GetOpCodes()` aligns two ordered name sequences (task names
within a play, or play names across the whole playbook) with no custom LCS
implementation needed. Each `OpCode` becomes:

* `'e'` (equal) - `oldNames[I1:I2]`/`newNames[J1:J2]` pair up 1:1, each pair
  a *matched* task/play, diffable.
* `'d'` (delete) - `oldNames[I1:I2]` are old-only (strikethrough).
* `'i'` (insert) - `newNames[J1:J2]` are new-only (underline).
* `'r'` (replace) - deliberately *not* forced into a pairing despite same
  position: `oldNames[I1:I2]` become old-only and `newNames[J1:J2]` become
  new-only, independently. Correctness (never claiming two differently-
  named tasks "are the same task, just changed") matters more here than
  cleverness.

```go
type taskAlignment struct {
    OldTask, NewTask *taskNode // exactly one nil for old-only/new-only
}
func alignTasks(oldPlay, newPlay *playNode) []taskAlignment
// oldPlay/newPlay may themselves be nil (whole play only on one side) -
// every task on the present side becomes old-only/new-only, uniformly.

type playAlignment struct {
    OldPlay, NewPlay *playNode
    Tasks            []taskAlignment
}
func alignPlays(oldState, newState *playbookState) []playAlignment
```

`taskDiffers(a taskAlignment) bool` - false for an unmatched pair (always
"different" by construction, never called); for a matched pair, true if
*any* host present in **both** `OldTask.Hosts`/`NewTask.Hosts` has a
different outcome, or different output (`primaryOutputField` text, stderr,
warnings - the same fields `formatHostOutput` already surfaces as distinct
sections, decoded via the same `Raw[host]` JSON both tasks already carry).
A host present on only one side is skipped entirely for this check (per
your own "wouldn't count host differences as a difference") - and, since
display always follows the *new* task's own `HostOrder` (per "render based
on the new version"), such a host simply doesn't appear in diff mode at
all when it only existed on the old side; nothing further needed for that
to fall out correctly.

### Candidate-run filtering (`diffresolve.go`)

`csvSetEqual(a, b string) bool` - a sibling of `revisitresolve.go`'s own
`csvOverlap`, same split/trim shape, but requiring the two comma-separated
sets to be *equal*, not merely overlapping.

```go
func resolveDiffCandidates(currentPlaybook, currentRole, currentTags, currentHosts, excludeRunID string, cfg stateConfig) []revisitEntry
```

Reuses `resolveRevisitEntries`'s own underlying flattening, filtered
further to `h.Playbook == currentPlaybook && h.Role == currentRole &&
csvSetEqual(inv.Tags, currentTags) && csvSetEqual(inv.Hosts,
currentHosts)`, and excluding `excludeRunID` (the session currently on
screen, so it never offers itself). Sorted newest-first, identical to
`resolveRevisitEntries`.

### The `d` key and its own list (`diff.go`)

Only reachable from the plain tree (not from within diff mode itself - no
`d` binding there, confirmed), only once `processDone` (same gate `r`
already has). Pressing it: load the comparison run's own saved `.jsonl`
into a second `playbookState` (`openRevisitEntry`'s own replay code,
factored out so both call sites share it), run `alignPlays` against
whichever `state` is currently on screen (live or itself a revisit
replay - `state` is already the uniform representation either way, so
`d` works identically from both starting points, per your own answer),
and show the result via a new, dedicated view - **not** another
`NewLiveTUI` call: the data model here is fundamentally pairs of
tasks/hosts, not one `playbookState`, and forcing that through
`NewLiveTUI`'s single-state-oriented plumbing (`sourceIndex`,
`requestRerun`, live-generation bookkeeping) would fight it at every turn
rather than reuse it. `runDiffTUI` is a new, standalone `tview.Application`
- same "own Application, own construction" shape `revisit.go`'s own list
already uses - built from `flattenDiffRows` (a new function mirroring
`flattenRows`'s conventions - same `row` type, same expand/collapse
mechanics via `treeList` - but walking `[]playAlignment` instead of one
`state`, only emitting a play/task row when it (or something under it)
differs, and setting an `underline`/`strikethrough` flag per row instead
of `flattenRows`'s own filter-driven visibility).

Row rendering reuses `taskLabel`/`hostLabel` for the *colors* (normal
outcome coloring throughout, per your own "colored as normal... only
differences are underlined") with a new `diffMark` parameter threaded in
for the underline/strikethrough tag itself (`::u`/`::s` - tview supports
both; worth a quick throwaway confirmation before leaning on it, not a
design risk). Chrome (top/bottom bar, two-pane divider) gets a fourth
color alongside navy/purple - proposing `fuchsia` (another fixed base-16
ANSI slot, distinct from every color already in use) - to answer the
"yet another coloring" line from the original draft: that's the chrome,
not the tree content, which stays normally-colored per the newer
Visualization section.

Esc/q: closes diff mode back to the run-list picker (matching revisit's
own list, reused here too - filtered via `resolveDiffCandidates` rather
than `resolveRevisitEntries`); Esc/q again from that list returns to the
standard tree, exactly as described. No `d` binding inside diff mode
itself.

### Drill-down (`diff.go`, reusing `buildDiffTab`'s own diff machinery)

For a matched pair, each tab except Docs renders `difflib.UnifiedDiff`
between the old/new text for that tab (Task/Output/Task definition/
Resolved/Details - the same content each tab already computes for a single
run, computed twice and diffed instead of shown once) - green `+`/red `-`
lines, no other coloring, matching `buildDiffTab`'s own existing line-
prefix convention exactly (reused, not reinvented). Docs is shown once,
undiffed (module docs don't vary per run). The existing per-run `Diff` tab
(ansible's own before/after) is omitted entirely from this tab set. For an
unmatched (old-only/new-only) task, there's nothing to diff against - each
tab just shows that one run's own single content, same as the normal
(non-diff) drill-down would.

## Proposed phasing

1. **Matching engine + candidate filtering.** `diffmatch.go`/
   `diffresolve.go` - pure, fully unit-testable (alignment correctness,
   `taskDiffers`, `csvSetEqual`, candidate filtering) with zero UI. Same
   shape as Revisit's own Phase 1.
2. **Diff tree view - done.** `diff.go`. The `d` key (tui.go, right
   alongside `r`, same `processDone` gate) runs the whole flow inside
   `app.Suspend` - the same primitive the output view's own 'e' (open
   $EDITOR) already uses - rather than any custom state save/restore:
   Suspend hands the real terminal to `runDiffFlow`'s own nested
   Applications (the candidate-run list - `runRevisitListTUI`, reused
   as-is, just fed `resolveDiffCandidates` instead of
   `resolveRevisitEntries` - then `runDiffTreeTUI`) and automatically
   resumes the original live tree, exactly where it left off, the moment
   `runDiffFlow` returns - which is what makes "Esc/q eventually returns
   to the standard tree view" fall out for free rather than needing
   dedicated plumbing.

   One real plumbing gap this surfaced: `NewLiveTUI` never actually knew
   its own session's target identity (playbook path or role name) -
   `playbookName` is display-only (`filepath.Base(playbook)` for a
   playbook session), not what `state.toml` itself keys history on. Fixed
   by adding `targetPlaybook, targetRole string` parameters (mirroring
   `appendInvocation`'s own convention), threaded from both main.go and
   revisit.go. Separately, rather than track "this session's own current
   RunID" through the live-generation machinery (which changes across
   reruns and would need its own cross-goroutine-synchronized tracker),
   `lastRunID` just looks it up fresh from `state.toml` each time `d` is
   pressed - correct because, by the time `d` is even pressable
   (`processDone`), the current generation's own invocation record has
   already been finalized and is necessarily the last one recorded for
   that target.

   Row rendering (`flattenDiffRows`/`diffTaskRowText`/`diffHostRowText`)
   deliberately doesn't reuse `taskLabel`/`hostLabel`'s own shared-column-
   width shrink algorithm - diff mode shows far fewer, more focused rows
   than the live tree ever does, so that sophistication isn't needed; a
   simpler, standalone renderer was faster to get right and easier to
   verify. Confirmed live end-to-end (two real runs of a tweaked
   playbook): only the actually-different play/task showed up, the
   differing host was individually underlined on the collapsed row and
   fully on its own expanded row, chrome was fuchsia, and quitting back
   out through the list correctly resumed the original session exactly
   where it was left, chrome back to normal, fully interactive.

   Enter on a host row is a no-op for now (no drill-down yet) - decided
   live rather than guessed at, per the plan above.
3. **Drill-down unified diffs.** Per-tab red/green diffing via
   `buildDiffTab`'s own machinery, the Docs/Diff-tab exceptions.

