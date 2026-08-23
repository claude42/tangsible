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
3. **Drill-down unified diffs - done.** Enter on a host row in diff mode
   now opens a real drill-down (`showDiffOutput`/`buildDiffOutputTabs`,
   diff.go): for a matched pair, Task/Output/Task definition/Details are
   each unified-diffed (`diffTwoTexts` - reuses `colorizedUnifiedDiff`,
   pulled out of `buildDiffTab`'s own `unifiedDiffText` as a shared
   helper rather than reimplemented), a tab silently omitted whenever
   that side's content is identical (same "nothing to show" convention
   `buildOutputTabs`' own `add()` already uses); Docs is shown once,
   undiffed; Diff (ansible's own before/after) is dropped entirely, for
   both a matched and an unmatched task alike. An unmatched (old-only/
   new-only) task falls back to `singleRunTabs` - that one run's own
   normal tabs, nothing to diff against.

   One thing this needed that wasn't obvious up front:
   `buildTaskTab`/`buildOutputTab`/etc. all return tview-tag-decorated
   text meant for direct display, not plain content - diffing that as-is
   would treat a pure color change (e.g. an outcome going from green to
   red) as a text difference, which is noise the tree's own underline
   marking already covers. `stripTags` (a regex matching tview's own tag
   grammar, confirmed against its `util.go`) strips that back to plain
   text before diffing, without needing plain-text variants of every tab
   builder.

   **Resolved deliberately excluded, at least for now** - flagged rather
   than silently dropped: it isn't a genuinely per-run recorded fact in
   the first place (design-docs/Drilldown, Resolved Values.md - it always
   re-resolves against *current* vars, regardless of which run's task is
   shown), so for a matched pair whose own source didn't change it would
   just diff identically-resolved text against itself; when the source
   *did* change, Task definition's own diff already surfaces that. Can be
   added later if it proves genuinely wanted.

   **Docs fetch is synchronous here**, unlike the live tree's own
   async-fetch-plus-cache machinery (`docsCache`/`resolveCache`) - this
   view has no live session to hang that off of, and is opened rarely
   enough that a brief blocking `ansible-doc` call is an acceptable
   simplification rather than replicating that whole apparatus.

   **A pre-existing limitation surfaced live, not a new bug**: "Task
   definition" for the *old* run's own task can come back identical to
   the new run's, and get silently omitted, even when the playbook was
   genuinely edited between the two runs - because `sourceIndex` (both
   old and new) always reflects whatever the file says *right now*, never
   a historical snapshot (source.go's own long-standing, documented
   behavior - the same caveat revisit's own drill-down already has).
   Output still diffs correctly regardless, since that comes from each
   run's own recorded `Raw` JSON, not a re-read of the file.

   Confirmed live end-to-end: edited a task's own `msg:` between two
   runs, diffed them - Output tab showed the real content diff (red/green/
   teal, matching real `diff -u` output), Task/Task definition were
   correctly omitted (identical, per the caveat above for the latter),
   and the full three-level Esc/q stack (drill-down → diff tree →
   candidate list → original live session, chrome and all back to
   normal) worked exactly as designed.

## Post-Phase-3 bug fix: invisible cursor in the diff tree

Reported live: after picking a comparison run, the diff tree rendered
with no visible cursor at all (navigation still worked, expand/drill-down
too, just nothing ever looked selected).

Root cause: `runDiffTreeTUI`'s own `rebuildRows` never re-rendered the row
under the cursor with its selected styling at all - `treeList` (unlike
`tview.List`) has no built-in "current row" look of its own; a row's
selected appearance is *entirely* baked into that row's own text by
whichever code builds it (the exact mechanism the live tree's own
`rebuild()` already relies on for the same reason). `flattenDiffRows` was
missing that step outright in the first version of this.

Fixed by threading a `selectedID any` (a row's own `id` - `*playNode`,
`*taskNode`, or the new `diffHostRowID{task, host}`) through
`flattenDiffRows`/`diffHostRows`, comparing it against each row's own id
as it's built. That alone still left the very first render broken,
though, for a subtler reason: on the first ever call, `currentID` is
`nil` - nothing can render as selected yet, since nothing has been
chosen - and the default row (index 0) is only discovered *after*
that render already happened, too late to reflect it. Fixed by making
`rebuildRows` do two passes: a cheap probe pass (existing `currentID`,
possibly `nil`) purely to resolve which index it lands on, then a real
pass with `currentID` now definitely pointing at that row, which is what
actually gets displayed. Confirmed live: the cursor is now visible from
the very first frame, and stays correctly synced through navigation,
expand/collapse, and opening/closing the drill-down.

## Post-Phase-3 bug fix: task rows unindented, hosts not column-aligned

Reported live, on a real multi-play playbook with many differing tasks:
play and task rows both sat flush at column 0, and each task row's own
host list started immediately after THAT row's own title - a different
column per row - rather than one shared column. "The text content itself
looks ok, but the formatting is totally off."

Two separate gaps in the first version of this, both real: task rows
were never given `taskIndent` at all (the same prefix the live tree's own
`taskLabel` uses to set a task apart from a play's own column-0 title),
and there was no shared title-column computation - `diffTaskLine` just
put two spaces after whatever length that one row's own title happened to
be, so shorter/longer titles never lined up against each other.

Fixed by adding `taskIndent` to `diffTaskLine`'s own output, and a new
`diffTitleColWidth(alignments)` - the widest title among every row
`flattenDiffRows` would currently show, computed once per rebuild and
threaded into every `diffTaskRowText` call so every task row pads its own
title out to that same shared width (plus `titleHostGapFloor`, the same
constant the live tree's own `taskLabel` uses) before the host list
begins. Deliberately simpler than the live tree's own
`computeHostColumnLayout`: no shrink-to-fit pass for a narrow terminal or
an especially long title - just the padding that actually caused the
reported problem. Confirmed live on a synthetic multi-play, multi-length-
title playbook: task rows now sit indented under their own play, and
hostnames land at the identical column across rows regardless of title
length.

## Post-Phase-3 bug fix: crash opening an old-only task's own host

Reported live, reproducible every time: some tasks in the diff tree had
no visible underline/strikethrough at all - "look like normal tasks."
Expanding one of those and pressing Enter on its host row crashed
tangsible with a nil pointer dereference.

The "looks like a normal task" tasks were old-only (strikethrough-marked)
ones - strikethrough is a much subtler visual cue than underline in a lot
of terminals, easy to miss entirely at a glance, which is exactly why
they read as unmarked rather than as a rendering bug of their own. The
real bug was in what pressing Enter on one of their host rows actually
did: `buildDiffOutputTabs` called `taskAction(a.NewTask, host)`
*unconditionally*, before ever reaching its own `a.NewTask == nil` check
a few lines below - `a.NewTask` is nil by definition for an old-only
task, and `taskAction`'s own `t.Raw[host]` is a nil pointer dereference
on a nil `t`.

Fixed by checking which side is actually present *before* calling
`taskAction` at all, rather than calling it speculatively and only
correcting course afterward. Added
`TestBuildDiffOutputTabsOldOnlyDoesNotPanic` - confirmed it reproduces
the exact panic against the unfixed code (same nil-dereference stack
trace) and passes against the fix. The earlier
`TestBuildDiffOutputTabsUnmatchedFallsBackToSingleRun` only ever
exercised the *new*-only side of "unmatched," which is why it hadn't
already caught this - worth remembering for "unmatched" test coverage
generally in this feature: the two sides need to be tested separately,
not assumed symmetric just because the code looks symmetric.

## Post-Phase-3 bug fix: unmatched tasks drilled into looked like "no differences"

Reported live against two real saved runs: a task shown in the diff tree
(strikethrough-marked, so correctly detected as unmatched) read, once
drilled into, as if the feature had failed to find anything - every tab
just showed that one run's own ordinary content, with no diff markup
anywhere, since there was nothing to diff against. Confirmed against the
user's own two jsonl files (`grep`) that the specific task named -
`postfix : Run newaliases` - was genuinely, entirely absent from the
newer run's event stream, and that its start event was
`v2_playbook_on_handler_task_start`, not `v2_playbook_on_task_start`:
an Ansible handler, only invoked via `notify:` when a preceding task in
that run actually reports `changed`. The older run's own postfix
configuration task changed something and notified it; the newer run was
idempotent and never did - a legitimate, structural difference between
the two runs' own event streams, not a matching/detection bug at all.
`alignTasks`/`playAlignmentHasDifferences` were already doing the right
thing: an unmatched task is exactly what a task that only exists on one
side *should* produce.

The actual bug was purely in how `singleRunTabs` communicated that: it
rendered a completely normal, single-run drill-down with nothing at all
saying *why* there's nothing to diff, so a reasonable user reads "no
diff markup" as "no differences found" rather than "this task didn't run
on the other side, which is itself the difference."

Fixed by threading a `side string` ("old"/"new") through
`singleRunTabs`'s three call sites in `buildDiffOutputTabs`, and
prepending `unmatchedTaskNote(side)` - a short, explicit callout - to the
Task tab's own content whenever `side != ""`. The third call site (the
`!oldOK || !newOK` decode-failure fallback, where the task genuinely
exists on *both* sides but one side's result failed to decode)
deliberately passes `side: ""` - the task isn't actually unmatched there,
so the note would be actively wrong; a dedicated regression test
(`TestBuildDiffOutputTabsDecodeFailureFallbackOmitsUnmatchedNote`)
pins that it stays silent. `TestBuildDiffOutputTabsUnmatchedFallsBackToSingleRun`/
`TestBuildDiffOutputTabsOldOnlyDoesNotPanic` were both extended to assert
the note's own text appears. Verified live against the user's own two
provided runs (via a throwaway `.tangsible/state.toml` + `runs/`
pointing at them): drilling into `postfix : Run newaliases` now leads
with the note before the usual Name/Action/Role/Host/Status block,
rendered in yellow, matching `sectionLabel`'s own section-color
convention rather than colliding with any outcome color.

The note's own wording went through one more round after this: the
first version ("Only present in the {old/new} run - this task did not
run at all in the {other} run - possibly a handler that wasn't notified
there. That absence is itself the difference; there is nothing else to
compare against.") was cut down, per live feedback, to a single
terse line - `"Task only present in the {old/new} run."` - once it was
established (see "strikethrough not rendering" below) that the note's
real job is just to confirm what the tree row is already trying to say,
not to re-explain the whole mechanism inline.

### Strikethrough not rendering - a terminal/mosh issue, not a Tangsible bug

Live report: the strikethrough marking on an old-only/new-only task row
wasn't visible at all - "look like normal tasks." Checked directly via
`tmux capture-pane -e` against real output: `diffTaskLine` does emit the
correct ANSI SGR 9 (`\x1b[9m`) around both the title and the hostname for
exactly these rows - not a rendering bug in this codebase. Narrowed down
live, by the user testing several terminals directly: plain `ssh` renders
it correctly; the same session over `mosh` does not, in iTerm2 and in
macOS's own Terminal.app alike - `mosh`'s own terminal emulation is the
common factor, not any particular terminal emulator, and not tmux either
(tmux's own well-known SGR-9-passthrough gap, which requires an explicit
`terminal-overrides` entry, was ruled out separately - the non-tmux mosh
sessions failed identically). A `mosh` limitation is outside this project's own control, but the tree
row's own signal was worth making robust anyway - see the fix below.

### Follow-up fix: italic + a plain-text marker, alongside strikethrough

Live-tested (a raw `printf '\x1b[3mitalic...'`, in the user's own mosh
session) that italic *does* render there where strikethrough doesn't.
Rather than swap one for the other, both are now applied together to an
old-only task's whole line - `wholeLineFlag` went from `"s"` to `"si"`
(tview's tag grammar accepts multiple attribute letters combined, e.g.
`[silver::si]` - confirmed directly against `tview`'s own tag parser,
`strings.go`, which just ORs each recognized letter's own `tcell.AttrMask`
in turn). Rationale, direct from the user: strikethrough still reads as
"something's gone" wherever it renders, so it's worth keeping for
terminals that support it; italic is what survives on the ones (like
`mosh`) that don't. New-only tasks are unaffected - underline ("u") was
never reported broken, so it's untouched.

On top of that, `unmatchedMarker(a taskAlignment) string` adds a third,
completely attribute-independent signal: a literal `" (old only)"`/`"
(new only)"` suffix rendered right after the task's own title, inside the
same styled span (so it inherits `wholeLineFlag`'s own color/attributes
too, rather than looking like a separate, differently-styled annotation).
Unlike strikethrough/italic/underline, plain text needs no terminal SGR
support at all - guaranteed visible everywhere. Threaded through
`diffTaskLine`'s new `marker` parameter and `diffTaskDisplayWidth` (used
by both `diffTitleColWidth`, so the shared host column still accounts for
the marker's own width on every row that carries one, and `diffTaskLine`
itself, so a task's own padding calculation matches). Not added to the
expanded host rows (`diffHostRowText`) - those sit directly under an
already-marked collapsed task row and don't repeat the task's own name,
so there's nothing natural to hang a marker off of there; the
strikethrough+italic/underline flag alone still applies per host as
before.

Verified live again against the user's own two saved runs (same
throwaway `.tangsible/state.toml`/`runs/` setup as the earlier fix):
`postfix : Run newaliases` now renders as
`postfix : Run newaliases (old only)` with `\x1b[3;9m` (italic +
strikethrough combined) confirmed via `tmux capture-pane -e`, and the
shared host column still lines up correctly across every row in the
play, marked and unmarked alike. `TestDiffTaskRowTextUnmatchedOldOnly`/
`TestDiffTaskRowTextUnmatchedNewOnly` were extended to assert the
marker text and the combined `si` flag; `TestDiffTitleColWidth`/
`TestDiffTaskRowTextIndentedAndPaddedToSharedColumn` were updated to
size their expected/shared column width via the new
`diffTaskDisplayWidth` instead of a bare title length, since the marker
is now part of what has to line up.

