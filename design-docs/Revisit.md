# Revisit

## Situation

After quitting tangsible it often happens that one would like to look at the
results of the last run (or maybe even an earlier run) again. Of course the
user can look into ansible.log but that's just painful. I would be great if
they could simply open tangsible again to revisit an old run and have all the
tree and drill down functionality.

## Behavior

Introduce a new verb "revisit".

`tangsible revisit [<playbook>] [<other ansible-playbook-options>]`

tangsible will then show a list of previous tangsible runs in reverse
chronological order, cursor is on the most recent entry. If only `tangsible
revisit` was used, all previous runs will be listed. If a playbook or e.g. -l
or -t has been specified only previous runs which match these criterie will
be shown.

When the user selects one entry with enter, the normal tree will be displayed
be displayed and the user can use all the usual functionality (expand
entries, drill down view, filter, re-run, ...).

That top and bottom lines (and also the vertical divider when drilling down
in two-paned mode) shall be rendered in a different color than the current
blue to visualize that this is data from an old run.

When the user presses esc from the tree view they will get back to the list
of previous runs.

## Implementation (proposed)

### What gets saved, and where

`main.go`'s `appendInvocation` call sites (the top-level `run`/`role` calls,
plus `requestRerun`) are already the one funnel every generation of every
verb goes through - `run`, `rerun`, and `role` alike - so that's where
per-generation persistence hooks in too, with no verb-specific code needed.

Each generation gets two new files under `.tangsible/runs/`, named from a
nanosecond-precision timestamp so collisions are a non-issue at this app's
own scale:

* `<id>.jsonl` - the exact raw stdout lines `ansible-playbook` emitted,
  written as each one is read, not re-serialized from the decoded
  `rawEvent`. This needs one small change to `scanEvents`/`streamItem`: keep
  the original line bytes alongside the decoded event, so the saved file is
  byte-identical to what a live run would have produced. Re-marshaling the
  decoded struct instead was the other option, but `rawEvent` only models
  the fields this app currently cares about - re-serializing would silently
  drop anything else in the line, todays or future. Byte-identical also
  means replay can reuse the exact same line-based scanner, just pointed at
  a file instead of a pipe.
* `<id>.stderr` - `childStderr`, which `runGeneration` already collects in
  full by the time it reaches `cmd.Wait()`. Nothing new to capture, just
  something new to do with data that already exists in memory. (Not
  surfaced anywhere in the UI yet, per your note - out of scope here.)

`state.toml`'s `playbookHistory.Invocations` changes shape from `[]string`
to `[]invocationRecord`, a breaking change to that one field (same spirit as
Dottangsible-directory.md's own breaking change - single user, no migration
code):

```go
type invocationRecord struct {
    Args     string `toml:"args"`      // unchanged - argsToHistoryString's
                                        // existing output
    Time     string `toml:"time"`      // RFC3339, this generation's start
    ExitCode int    `toml:"exit_code"`
    RunID    string `toml:"run_id,omitempty"` // <id> above; empty if this
                                        // generation's own save failed
                                        // (non-fatal, see below) - such an
                                        // entry still pre-fills a rerun
                                        // dialog but can't be revisited
}
```

This covers what you asked for (`Args` reused as-is, `Time` added) plus the
minimum extra needed for revisit to function at all - `ExitCode` (so the
list view and the status line don't need to re-derive anything) and `RunID`
(so an entry can find its own files). Nothing beyond that.

Saving is best-effort, same tolerance this app already has elsewhere for
non-critical I/O (e.g. `requestRerun`'s own history writes are silently
dropped on failure): a write failure here doesn't fail the run, it just
means that one entry isn't revisitable later, same as if it predates this
feature entirely.

### Retention

Reuses the existing `maxHistoryPerPlaybook` (20) cap outright, rather than
adding a second number: when an old `Invocations` entry is evicted for a
target, its `<id>.jsonl`/`<id>.stderr` files (if `RunID` is set) are deleted
right alongside it, in the same `appendInvocation` call. No orphaned files,
no new config knob.

`.gitignore` gains `/.tangsible/runs/` next to the existing
`/.tangsible/state.toml` entry - same reasoning (hostnames, tags, real
command output).

### The `revisit` verb and its list view

Structured the same way `template`/`host`/`hosts` already are (per
HostVerb.md's own findings): fully split off in `main.go` before any of the
run/rerun/role machinery runs, with a `resolveRevisit`-style pure resolver
(mirroring `resolveRerun`) that reads `state.toml` and filters its flattened
`invocationRecord`s by playbook/role (if a positional arg was given) and by
`-l`/`--tags` (if given - parsed with the existing `parsePassthroughArgs`
and compared against each candidate entry's own parsed `Args`), newest
first.

The list itself is a standalone `tview.Application` built the same way
`runHostsListTUI` already is - `newTreeList()` (flat, no expand/collapse
needed here) inside a `Pages`/`Flex` with its own header/footer bars,
one row per matching entry:

```
<timestamp> - tangsible run -l zen
<timestamp> - tangisble role  postfix
```

Selecting an entry with Enter needs a *second*, separate `tview.Application`
- the actual `NewLiveTUI` tree, unmodified, just started from a new
"replay" path rather than a live process:

* `replayEvents(path string) <-chan streamItem` - `scanEvents`'s own
  line-scanning logic, pointed at the saved file instead of a pipe, closing
  immediately at EOF rather than blocking on a live process.
* `NewLiveTUI` gets a `startFrozen bool`-ish path (same shape
  `startWithRerunDialog`/`everStarted` already use for "rerun, nothing has
  run yet"): `processDone` pre-true, `exitCode`/`HadUnreachable` seeded from
  the manifest entry and the replayed events (not re-derived from a live
  process, since there is none), chrome switched to the new purple
  (Colors.md).
* Everything downstream - tree, filtering, two-pane drill-down, Diff/Docs/
  Resolved tabs - is entirely unchanged, since all of it already reads from
  `state`/`task.Raw`/`sourceIndex`, none of which care whether the data
  arrived live or replayed.

Tearing down the list `Application` and handing off to the detail
`Application` (and back again on Esc) is two sequential `app.Run()` calls,
same pattern `host.go`'s list-then-detail flow already uses one level down
- just one level higher here, at the whole-program level, since the detail
side needs to be the real `NewLiveTUI` rather than a second tab set. Esc
from the tree closes that `Application` and re-shows a freshly rebuilt list
(freshly rebuilt so a re-run started and finished during that detail
session - see below - shows up in it). q/Ctrl-C from the list quit the
whole program, matching normal mode.

### Re-run from within a revisited session

`requestRerun` already exists and needs no changes to work from a replay-
started session - it resets `state`, spawns a real generation, and from
that point on `processDone`/`exitCode`/etc. are exactly what a live
`run`/`rerun` session already produces. Two things follow from that,
proposed rather than asked:

* The moment a real rerun is confirmed, the session stops being "old data"
  in every sense - chrome reverts to navy, and Esc stops meaning "back to
  the list" (there's a live/finished run on screen now, same as `run`/
  `rerun` - closing it should behave like closing any other session, not
  quietly discard it back to a list).
* That new generation gets recorded (and, per the above, saved) exactly
  like any other rerun - it becomes a new, independent `invocationRecord`
  under the same target, revisitable on its own later.

## Open questions

A few things I don't think I should just decide unilaterally:

1. Does the above match what you had in mind for `invocationRecord`, or did
   you want `Args`/`Time` to stay exactly `[]string` + something else? (You
   asked me to flag if I see a problem with your sketch - I don't, this is
   just the same idea with the two fields revisit itself can't do without.)
2. Two sequential `tview.Application`s (list, then a real `NewLiveTUI`, torn
   down and rebuilt on every list<->detail transition) vs. folding the list
   into `NewLiveTUI` as a third page/mode alongside "main"/"output"/"split":
   I'm proposing the former (matches `host.go`'s existing precedent, keeps
   `tui.go` - already ~4800 lines - from taking on yet another mode). Any
   objection?
3. A `tangsible role` session's saved `task.Path` points at the role's
   generated stub playbook, which is deleted (`defer os.Remove`) once that
   session ends - so revisiting a role-originated entry would have no
   `TASK:` source to show (same graceful "just don't show that section"
   behavior source lookup already has for any other miss, not an error).
   Fine to accept as a known gap, or worth persisting the stub's content
   too so source lookup still works?
4. ~~If the saved `<id>.jsonl`/`.stderr` files are missing at revisit
   time...~~ **Resolved differently**: instead of erroring at selection
   time, the list-building step itself stats each candidate entry's
   `<id>.jsonl` (the one file actually required to open the tree -
   `.stderr` is supplementary and not yet surfaced anywhere, so its
   absence alone doesn't disqualify an entry) and silently skips any entry
   whose file is gone, pruning it from `state.toml` at the same time so it
   isn't checked again next time. Self-healing if files are cleaned up
   externally; no dead-end "error, then what" state in the UI at all.
   Settled: only `RunID` is cleared; `Args`/`Time` stay, so `rerun`'s own
   tags/hosts pre-fill (`lastInvocation`, Rerun.md) for that target is
   unaffected.

## Proposed phasing

1. **Persistence plumbing - done.** `scanEvents` now tees every raw stdout
   line to a per-generation log file (`runlog.go`'s `createRunLog`), byte-
   identical to what ansible-playbook emitted. `invocationRecord` replaces
   the old plain-string `Invocations` (`Args`/`Time`/`ExitCode`/`RunID`);
   `appendInvocation` still stamps `Args`/`Time` before a generation is even
   spawned (same "an invocation is an invocation" guarantee as before),
   `finalizeInvocation` fills in `ExitCode`/`RunID` once the generation
   actually finishes - including the pre-flight fast-fail path, which
   bypasses `runGeneration` entirely. Collected stderr is saved to
   `<id>.stderr` (`writeRunStderr`). Retention reuses `maxHistoryPerPlaybook`
   outright: `appendCapped` now reports what it evicts, and
   `appendInvocation` deletes that entry's saved files right alongside it.
   `.gitignore` covers the new `.tangsible/runs/` directory. Zero
   user-visible change yet - `revisit` doesn't exist as a verb. Covered by
   new tests in `history_test.go`, `runlog_test.go`, `main_test.go`.
2. **The `revisit` verb: list + replay, no rerun yet - done.**
   `resolveRevisitEntries`/`revisitEntry` (`revisitresolve.go`) flatten and
   filter `state.toml`'s history into what the list shows;
   `pruneMissingRunLogs` (`history.go`) does the dangling-`RunID` cleanup
   (question 4) right before that on every invocation. `revisit.go` is the
   verb itself: `runRevisitListTUI` (a small standalone `Application`,
   structured like `host.go`'s own list, no detail page of its own) for the
   browsing list, `openRevisitEntry` for replaying a selected entry's saved
   `.jsonl` (via the existing `scanEvents`, pointed at a file instead of a
   pipe - no separate `replayEvents` needed) into a fresh `playbookState`
   and handing it to an ordinary `NewLiveTUI` call, already frozen
   (`processDone` pre-true, `exitCode`/`HadUnreachable` already exactly
   what the original run produced). `runRevisitVerb` loops between the two,
   re-pruning/re-resolving on every return to the list. Confirmed live
   under tmux (real `ansible-playbook`, not just unit tests) - purple
   chrome (top bar, bottom bar, two-pane divider), Esc back to the list,
   the drill-down view (source/output/diff tabs) all working correctly
   against replayed data.

   One real bug caught only by that live check, not by any unit test:
   `chromeStyle`'s `SetTextStyle` calls don't actually reach the top bar's
   own visible text at all - `topBarText`/`composeSplitHeaderLine`/
   `outputTopBar`'s own progress-fill sweep (`progressFillLine`/
   `progressFillLineAt`) bakes its "unfilled" background into inline
   `[white:navy:b]` tags, bypassing `SetTextStyle` entirely (a single
   `tcell.Style` can't vary per-column the way a sweeping fill needs to).
   Fixed by threading a `bgColorName string` parameter through all three
   functions, fed from a new `chromeColorName()` closure (same
   read-fresh-every-call pattern as `currentMainBottomBarText`) - so it's
   still `revisitActive`-current on every redraw, not a value snapshotted
   once at construction.

   Two judgment calls made along the way, worth flagging even though
   nothing forced a stop for them:
   - ~~The top bar's elapsed-timer starts fresh...~~ **Resolved**: rather
     than show a misleading ~00:00, the clock is dropped entirely for a
     revisit session. `composeTopBarLine`/`composeSplitHeaderLine` gained a
     `showElapsed bool` (fed by a `showElapsed()` closure - `!revisitActive`,
     read fresh on every redraw, same pattern as `chromeColorName`/
     `currentMainBottomBarText`): `false` omits the spinner/mm:ss entirely
     (a bare `Task x/y` prefix would still show alongside it if there were
     one, though revisit never actually builds a progress skeleton, so in
     practice the whole right-hand side of the bar is just blank). Reverts
     - clock reappears - the moment a real rerun starts, same as every
     other chrome piece.
   - `q`/Ctrl-C from a revisited entry's own detail view (not the list)
     currently drops back to the list, the same as Esc - it was never
     given a case of its own, so it just inherits `NewLiveTUI`'s existing
     "close this session" meaning, which here means "this Application
     specifically," not "the whole revisit session." Only `q` *at the
     list itself* exits the program outright. Left as-is for now per your
     own note - you'll try it live and decide whether it should instead
     skip the list and exit outright.
3. **Re-run from within revisit - done.** `requestRerun`/`runGeneration`'s
   actual mechanism was pulled out of `main()`'s own body into
   `generation.go` (`runOneGeneration`/`newRequestRerun`) - the same "one
   shared funnel" pattern `spawnGeneration`/`appendInvocation`/
   `finalizeInvocation` already established for starting/recording a
   generation, extended here to running one, so `openRevisitEntry` reuses
   it rather than forking its own copy. main.go itself is otherwise
   unchanged behaviorally - confirmed live (`run` then `r` twice in a row,
   two distinct saved generations, correct exit codes/files) before
   touching revisit.go at all.

   `openRevisitEntry` now builds a real `requestRerun`, and - for a role-
   originated entry - eagerly regenerates a stub via `startRoleSession`
   (unconditionally, even if 'r' never gets pressed - cheap, and it's what
   `sourceIndex` gets built from too, so a *rerun's* own fresh tasks get a
   working "Task definition" tab even though the historical replay itself
   still can't, per the accepted gap above). Confirmed live for both a
   plain playbook and a role entry: chrome/clock correctly revert to
   normal, Esc stops returning to the list, the new generation gets its
   own saved run-log and shows up in the list on the next loop - and, for
   the role case, the rerun's own tasks do get real Task-definition source
   now.

   One more live-only bug, same shape as Phase 2's top-bar one: reverting
   `revisitActive` in `submitRerun` reset every chrome bar's *style*
   correctly, but `bottomBar`'s "Esc: back to list" hint stayed in its
   *text* - unlike `topBar`/`splitHeader`, `bottomBar`'s text is a plain
   string baked in once (construction/`closeOutput`/split-mode toggle),
   never otherwise refreshed. Fixed by also calling
   `bottomBar.SetText(currentMainBottomBarText())` in that same revert
   block.

4. **Scroll position on open - fixed a Phase-2-introduced regression.**
   Reported: opening a revisit entry showed only the last line or two of
   the tree, then Summary/recap, then a lot of empty terminal below the
   recap - on a playbook long enough to overflow one screen, this got much
   worse (a handful of lines near the *middle* of the tree, nowhere near
   the top or the recap). Root cause: Phase 2's own "render immediately
   instead of waiting ~200ms for the heartbeat's first tick" optimization
   (an explicit `rebuild()` call before `app.Run()`) ran while `list` had
   no real rect yet - `ensureVisible`/the existing "reveal trailing status
   rows" scroll-to-bottom logic (both inside `rebuild()`, unchanged by any
   of this) computed against a bogus size, landing `itemOffset` somewhere
   wrong. The *next* `rebuild()` (the heartbeat's one tick, by which point
   `list` has a real rect) saw an unchanged `selectedIndex` and took the
   `restoreCurrentItem` path - which deliberately never touches
   `itemOffset` - so nothing ever corrected the bogus position on its own;
   only a genuine terminal resize (forcing the resize-watcher's own
   `rebuild()` down a different path) ever did.

   Fixed by simply removing that early `rebuild()` call - a revisit
   session now waits for the heartbeat's first tick like every other verb
   already does (a brief blank flash, same startup experience `run`/
   `rerun`/`role` already have), rather than trying to skip it. With that
   removed, the *existing* "reveal trailing status rows" mechanism
   (already in `rebuild()`, built for exactly this "keep the tail visible"
   requirement, not something new written for revisit) does the right
   thing on its own: recap fully visible at the bottom when content
   overflows, or shown from the top with empty space below when it
   doesn't - confirmed live both ways.

(Surfacing captured stderr in the UI itself is explicitly separate, per
your note above - not part of this feature's phasing at all.)

## Answers to existing questions

Scope of "a run.": one generation, i.e. one ansible-playbook invocation

What gets save: One file per run plus a manifest entry sounds sensible. Could
re-use / extend the current [[History]] sections in state.tml. I'd say
Invocations string can be re-used, timestamp should be added. Wouldn't add
more for the time being, let's see what we will need. Please let me know if
you see a problem with what I just wrote.

Retention: cap by count, seems the simplest for me

Stderr: good question, had that on my Ideas list any way how / where to
display this but let's handle that question separately. But yes, please
capture it somewhere so we can use it later.

The list view: we won't need a tree view for this, simple list (like used for
the "tangsible hosts" should be good. Each row should show the timestamp and
how tansible was called, e.g.

<timestamp> - tangsible run -l zen
<timestamp> - tangisble role  postfix

Navigation. Correct, esc from drill down -> treeview. esc from treeview ->
back to the list. esc/q on the list -> quit. Yeah, for consistency's sake,
make q/ctrl-c from the list view quit the whole thing as in normal mode.

For the moment, list should not be reachable from a run/rerun session, only
from revisit (I might change my opinion later on but I'll first have to think
about this).

Re-run ifrom revisit: yes, exactly as you describe. Let me know if this
creates any difficulties.

Color: propose something :-) (and update the Colors.md document accordingly)

CLAUDE.md: you can remove that comment now.

# Next ideas

## Manage runs

After a while many previous runs might accumulate. We will 

Previous runs are difficult to discern. Potentially the only difference is
the timestamp.
