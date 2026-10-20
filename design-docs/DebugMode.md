# Debug mode

## UI ideas

* Generally very similar to how run looks like. One-pane or two-pane mode
  depending on available screen real estate.
* But assumption is that the two-pane mode is the natural mode for the
  debugger
* Tree is the same, play, task, host lines - host lines are currently missing
* When the user presses enter on a host line the standard drill-down view
  opens
* When the debugger hits a break point or if there's an error or if the user
  has just performed a single step, the drill down opens for the current
  task+host
* We will have to bring back the currently disabled "Resolved variables" tab
  of the drill down view because now we can really resolve all variables
  (except for loop iterators...)
* The chrome shall get two additional colors, one for "playbook is currently
  running in debug mode" and one for "playbook has stopped at break point or
  in single step mode"
* User gets additional keyboard shortcuts for
  * toggling breakpoints (some already implemented) (Shortcut: F9)
  * single stepping (Shortcut: F10)
  * running until hitting a break point (Shortcut: F5)
  * looking at variables (see below)

## UI details variable explorer

I have put some more thoughts about the variable explorer and took some
inspiration from tools like fzf and some chat clients. I have tried to add
visualizations to my description. This is just meant for illustration the
final TUI can use different visualizations / symbols where sensible.

* When no variables are being watched, nothing is displayed

* When the user presses `v`, a text input field appears (here in this text
  visualized by ">". The bottom line changes. We are now in variable explorer
  mode. ESC brings us back to normal.

  When in two-pane mode this opens at the bottom of the right pane. In
  one-pane mode this opens at the bottom.

 > 
 Type variable name · ESC exit

* When the user starts typing, the characters will appear in new_var_color.
  In addition a popup box will appear with potential autocompletions. The
  autocompletion shows both the variable name as well as the current value.
  The autocompletion will only show variables which are not yet being
  watched. When searching for complex variables (lists, dicts), there will be
  no autocomplete (at least in v1), the user will have to input the full
  name, e.g. foo[0] or foo.bar

  The user can use cursor up/down to select on of the autocompletions and
  press Enter to add it to the list of watched variables.  The bottom chrome
  line get's updated to reflect the available keyboard shortcuts. (Note: ▶ is
  used here as an illustration, in tangsible inverting the entry would be
  more consistent)

 ┌ Available variables ─────────┐
 │ ▶ sometext = "something"     │
 │   somebool = true            │
 └──────────────────────────────┘
 > som
 Enter: add  ↑↓: select · ESC: back

* After the user pressed enter, the variable is added to the list of watch
  variables. The watched variables are rendered in the standard color.

 Watched variables
 sometext = "something"

 > 
 Type variable name  ↑↓: select  ESC: back

 * When more are added it might look like this

 Watched variables
 sometext = "something"
 foo      = 27
 bar      = true

 > 
 Type variable name  ↑↓: select  ESC: back

 * To remove a variable from the watch list, the user uses cursor up/down
   (without typing something beforehand) to move the cursor to the variable
   they want to remove and then presses Delete. Again, the bottom chrome bar
   is updated to reflect the possible shortcuts. (Note: ▶ is used here as an
   illustration, in tangsible inverting the entry would be more consistent)


 Watched variables
 sometext = "something"
▶foo      = 27
 bar      = true

 > 
 ↑↓: select  Delete: remove variable  ESC: back

 * As written above, the user can leave the variable explorer with ESC. If
   there are still any variables being watched, these variables (and their
   values) shall still be displayed when the variable explorer mode is left -
   but in a grey color (pick a suitable color from the already defined
   colors).

## Stage 4 UX decisions: scope, colors, Esc staging (2026-10-19)

Resolved while reviewing the variable explorer writeup above - the
"Architecture decision" section right below predates this writeup and
should be read as superseded wherever the two conflict (e.g. it calls
this a "modal"; it isn't one).

**Scope and "unavailable."** A watched expression is evaluated against
whichever host/task is currently selected, re-evaluating live as the
cursor moves - not fixed to the host/task that was active when it was
added. Three different reasons a value can't be shown right now (wrong
host/task context for this expression; no captured snapshot for this
task because `--all-vars` wasn't given; a syntactically valid path that
just doesn't resolve, e.g. an out-of-range index or a key that doesn't
exist) all render identically as "unavailable" - the user can't tell
these apart anyway, and doesn't need to. Rendered as the fourth color
below plus strike-through (available via tview's existing tag syntax,
`[color::S]`, backed by `tcell.AttrStrikeThrough` - no new rendering
mechanism needed; terminal support for the escape sequence varies, but
no more than other things tangsible already leans on: OSC 52 clipboard,
OSC 9/777/99 notifications, true color).

**Four colors.** Normal/resolved text: `StandardTextColor` (the existing
default body text color, no change needed). Panel deactivated but still
showing watched values (ESC leaves the explorer): `NothingReportedColor`
(already grey). A variable being typed, not yet added:
`KeyHighlightColor` (already green). Unavailable (above):
`IgnoredColor` (orange/saddlebrown) - chosen over `OutcomeSkippedColor`
for the closer connotation to existing `ignore_errors` usage ("not a
problem, just nothing to act on here"). No new `uikit.*Color`/config key
needed for any of the four.

**Accepting vs. rejecting typed text.** Raw typed text is always
accepted on Enter, even with no autocomplete match and even for a
variable not yet defined anywhere (watching ahead of a task that will
define it later is a real use case) - except a genuine syntax error
(not a plain dotted/bracket-index chain), which is rejected outright.
How exactly that accept/reject check is implemented (a client-side shape
check before ever evaluating it, vs. a small variant of
`resolve_condition.py` that returns an explicit ok/error per expression,
reusing `resolveConditionExprs`'s existing Jinja2-AST-based evaluator
rather than a new hand-rolled path walker) is deferred to Stage 4 itself,
not decided here. Enter on an empty input field is a no-op. A rejected
Enter is shown via the same transient bottom-bar status mechanism the
drill-down's `y`-copy status already uses (`livesession_tabsearch.go`) -
superseding the normal bottom bar momentarily, then clearing itself -
rather than a new UI affordance.

**Esc staging**, confirmed as fully covered by the two described cases:
(a) typing with autocomplete open: Esc closes the autocomplete, Esc
again clears the input text, Esc again closes the explorer - collapsing
a stage that doesn't apply (e.g. no autocomplete open because there were
no matches) rather than requiring a keypress that would otherwise do
nothing; (b) a watched variable selected (Delete removes it): Esc
deselects back to the input field, Esc again closes the explorer.

## Architecture decision (2026-10-18): fold into `liveSession`, not a second TUI

The "UI ideas" above (two-pane as the natural mode, the real tabbed
drill-down, live-sync between it and the tree) are all `liveSession`
machinery today (`internal/session`), not anything `debugverb`'s own
small, separate TUI has. Decided: `debug` stops being a "standalone
program that never builds the live tree" (CLAUDE.md's own current
framing) and becomes a `liveSession` with debug-specific state layered
on (breakpoints, pause, its own chrome) - not a second copy of two-pane/
drill-down maintained in parallel. `debug` stays its own verb/entry
point (`tangsible debug <playbook>`, discoverable and intentional, same
shape `role`/`revisit` already reuse the tree with their own twist) -
the change is what happens once that verb builds its `liveSession`, not
which command the user types. Follow-on documentation debt once this
lands: CLAUDE.md's "Scope" section's "`debug` ... not available from
inside the regular live tree" line, and the tview/tcell package-boundary
note ("only `internal/session`... and `internal/debugverb`...") both
need rewriting, since `debugverb` stops owning a TUI at all.

Staged, in this order: (1) keybindings/configurability/chrome, settled on
paper below, no code yet; (2) the merge itself - breakpoint toggle/step/
continue wired into `liveSession`, auto-open drill-down on
breakpoint/error/step, `debugverb`'s own TUI retired; (3) the Resolved
tab upgraded to use real per-`(task, host)` `task_vars` from the live
debugger, captured at *every* spliced pause (not just surfaced ones, so
scrolling back to an earlier task can still recall it) - performance
impact of the extra `Query()` per task not yet measured, revisit once
stage 2 is in; (4) the variable watch explorer (modal, autocomplete with
a live value preview, persistent watch panel).

## Stage 1 decisions (2026-10-18): keybindings, configurability, chrome

Settled on paper, nothing implemented yet.

**Debug-specific keys.** Everything else (navigation, Enter-to-expand/
drill-down, two-pane, `q`/Ctrl-C, filter, search, ...) is inherited as-is
from the plain live tree once `debug` builds one with extra state; only
these four actions have no existing tree equivalent to collide with:

| Action                             | Key   |
|-------------------------------------|-------|
| Run / continue to next breakpoint   | F5    |
| Toggle breakpoint on selected task  | F9    |
| Single step                         | F10   |
| Open the variable watch dialog      | `v`   |

`v` reuses the old single-shot "view task\_vars" binding's own key -
that page is retired (confirmed), replaced by the watch dialog, which
seemed worth keeping the same key for since existing muscle memory
already points at "`v` for variables."

**Configurable.** New ground - nothing in tangsible rebinds a key from
`config.toml` today. A `[keys]` section (not `[debug_keys]`: scoped to
these four debug actions for now, but named with room for other views to
grow configurable keys later too), snake\_case action names matching the
table above (`run`, `toggle_breakpoint`, `step`, `watch`), values spelled
the way `tcell` itself names them (`"F5"`, a single character otherwise).
v1 scope is exactly these four actions - not navigation, not `q`/Ctrl-C,
not anything the plain tree already owns. An unrecognized value falls
back to the key's own default and warns after exit, the same convention
`[colors]` already uses for a bad value (`PrintColorWarnings`,
`CLAUDE.md`'s "Never print while the TUI runs" invariant). A value that
collides with an existing, non-debug tree binding (e.g. remapping
`watch` to `f`, already Filter) is honored anyway, with the same kind of
warning rather than refused outright - the user's own informed choice,
consistent with how a bad `[colors]` value is handled, not a hard
validation error.

**Chrome.** Four states, each one of `uikit.ChromeLiveColor`'s own
default navy or the two new vars this adds (`ChromeDebugRunningColor`/
`ChromeDebugPausedColor` → `chrome_debug_running`/`chrome_debug_paused`,
following the existing `uikit.*Color` → snake\_case convention,
`design-docs/ColorConfig.md`):

* Idle, before the first Start/Continue/Step call - default chrome
  (navy), same as a plain run.
* Running - a Start/Continue/Step call actually in flight -
  `chrome_debug_running`.
* Paused at a breakpoint, or stopped on a task error - both
  `chrome_debug_paused`; an error is treated exactly like a pause, both
  for chrome and for the drill-down auto-open behavior above.
* Finished, the whole run completed - back to default chrome, same as
  idle.

## Stage 2: implemented (2026-10-18)

The merge itself. `debug` is now a real `liveSession`
(`internal/session`'s own switch in `main.go`), not a second TUI -
`internal/debugverb` dropped its `tview`/`tcell`/`uikit` dependency
entirely and became a headless package, the same shape `internal/
debugcopy`/`internal/debugrun` already were. `tui.go`/`tui_test.go`/
`helptables.go` (the old standalone TUI) are gone.

**The one real risk, found and resolved during this pass:** the
predicted, pre-run tree (every task visible before anything has run, so
breakpoints can be armed up front) can't be built the way a plain run's
tree is. `PlaybookState.Apply`'s own task/play identity resolution
(`findOrCreateTask`, `aggregate.go`) assumes incremental arrival - it
matches purely by the live event's own `task.id`, and
`v2_playbook_on_play_start` always starts a fresh `PlayNode`, never
reusing one by name. Pre-populating `state.Plays` from a skeleton and
then letting real events flow through `Apply` as normal would not reuse
those nodes at all - it would create a second, parallel set once the run
actually started, leaving the pre-built ones frozen and orphaned.
Confirmed by reading `Apply`/`findOrCreateTask` directly, not assumed.
Resolved by never calling `Apply` for a debug session at all:
`debugverb.SeedState` builds `state.Plays` directly from the skeleton
(mirroring the old standalone TUI's own `buildRows`, which already
proved this works), and `debugverb.Cursor.Apply` resolves each live
event against those exact nodes via the same `runner.ProgressTracker`
forward-matching the progress bar already uses, writing straight into
`Hosts`/`Raw`/`Ignored` - never through `Apply`. Because `rebuild()`,
`uikit.FlattenRows`, the drill-down, two-pane layout, etc. only ever
*read* `state.Plays`/`*TaskNode` fields, all of that machinery works
unchanged on top of a tree built this way.

**Other things built:** `uikit.FlattenRows` grew an `armed func(*TaskNode)
bool` parameter (nil for every non-debug caller) so the breakpoint marker
renders through the real shared row renderer, via a new shared
`uikit.BreakpointMarkerText()`, instead of debug-specific rendering code.
`playbook.PlaybookState.NoteHost` was exported (debug's own event
resolution needs it, and can't call `Apply`'s private equivalent).
`debugrun.Session.Process()` was added so the real child's `*os.Process`
reaches the session's own `procH` the same way a plain run's does -
`q`/Ctrl-C needed zero debug-specific code as a result. `NewLiveTUI`
gained two new parameters: `pendingSpawned bool` (decoupled from
`startWithRerunDialog` - debug's own "idle, no dialog, nothing spawned"
startup is a combination no earlier Verb needed) and `debugCfg
debugverb.DebugConfig` (the armed-breakpoint set + cursor + shadow-copy
path). `rebuild()`'s own `frozen` local now also requires
`s.everStarted` - without it, debug's idle state (`processDone` forced
true, same trick `rerun`'s own pre-dialog startup already used) rendered
a "Playbook completed successfully" recap before anything had run.

**A real bug found and fixed live, not just on paper:** `processDone`
starts forced `true` for debug's own idle state, and nothing cleared it
once a real run actually started - so `q`/Ctrl-C's own
`handleGlobalQuit`/`handleCtrlC` (which read `processDone` directly, not
`s.everStarted`) still saw it as `true` the entire time a real child was
alive and running, and quit outright on the very first `q` instead of
interrupting it. Confirmed by checking the actual OS process (not just
the tmux pane) before and after each press - fixed by clearing
`processDone` the moment driving begins (`debugStartOrContinue`).

**Verified live via tmux:** the full predicted tree renders idle, with
nothing spawned; F9 arms a breakpoint (marker renders in the right
column); F5 runs to it, auto-opening the drill-down in two-pane mode for
the exact paused task+host; F5 again continues to completion, recap
renders correctly; `q`/Ctrl-C's two-stage interrupt-then-quit behavior is
correct in all four states (idle, driving, paused, finished), confirmed
by inspecting the real child process's survival, not just the pane; both
new chrome colors render distinctly from navy and from each other.

**Deferred, not done in this pass:**
- The `[keys]` config section ("Stage 1 decisions" above) - F5/F9/F10
  are hardcoded for now, not yet read from `config.toml`.
- "Stopped on a task error" (the "UI ideas" section's own "if there's an
  error" auto-open trigger) isn't actually implemented as its own
  mechanism: `debugrun.BreakpointSet`'s policy (`applyPolicy`) has no
  concept of a task's own outcome at all - a `Pause` carries only a host
  and a task label, never success/failure, which only the jsonl event
  stream (a different pipe, consumed on a different goroutine) knows.
  Making a task failure auto-surface regardless of arming would need
  either a new synchronous query against the debugger's own `result`
  scope at every single pause (a real cost, and a new round-trip per
  task), or re-implementing `RunWithBreakpoints`/`ContinueWithBreakpoints`'s
  loop inside `internal/session` instead of delegating to `debugrun` -
  neither attempted here. Today, a failed task only pauses if its own
  breakpoint happens to be armed, same as any other task.

## Stage 2 follow-up: three bugs found in real use (2026-10-18)

All found from actually using the merged session, not spotted on paper:

1. **Cursor/viewport opened on the last task, not the first.**
   `rebuild()`'s own `s.following` branch pins to "the newest row" as
   `len(currentRows) - 1` - correct for a plain run, whose `state.Plays`
   grows incrementally (the last row really is the newest one, for that
   row's whole lifetime), completely wrong for a debug session's own
   fully pre-built tree, where "the last row" is just whatever the
   playbook's own last task happens to be. Fixed with a debug-specific
   branch: pin to `debugCursor.CurrentTask`'s own row once something has
   started, the first interactive row before that.
2. **Scrolling up to a play's first task never revealed its own header.**
   Pre-existing in the plain tree too, not new here - just far more
   noticeable in debug mode, where the *whole* tree (every play) is
   visible from the very first frame. `TreeList.ensureVisible` now
   reveals one additional non-interactive row (a header, or any other
   decorative divider - inferred the same way `AddItem`'s own "nil
   Selected marks a row non-interactive" convention already does, not a
   header-specific concept) immediately above wherever it scrolls up to,
   if there is one - a direct jump (Home/Ctrl-A) is unaffected, and
   ordinary task-to-task moves within the same play don't trigger it.
3. **Enter never expanded a task into its own host rows.** `FlattenRows`
   (`uikit/tui_rows.go`) iterates a task's `HostOrder`, never `Hosts`
   directly - `debugverb.Cursor.Apply` populated `Hosts`/`Raw`/`Ignored`
   on every terminal event but never touched `HostOrder` at all, so it
   stayed permanently empty for every debug task, regardless of whether
   a breakpoint was armed. Fixed by mirroring `aggregate.go`'s own
   `TaskNode.record` exactly: append to `HostOrder` the first time a
   host is ever seen for that task, before `Hosts` itself is written.

## Stage 2 follow-up round 2: three more, from real use (2026-10-18)

1. **"Task definition" tab always missing.** `TaskNode.Path` was never
   set at all for a debug session - `SeedState` has nothing to set it
   from (the `--list-tasks` skeleton carries only play/task names, no
   file:line), and `Cursor.Apply` never touched it either. Both the
   drill-down's "Task definition" tab and its "Resolved" tab are keyed
   by `sourceIndex[task.Path]`/`resolveTaskValues(task.Path, ...)` - with
   `Path` always `""`, both came back with nothing to show, always.
   Fixing this is more than copying `ev.Task.Path` over, though: that
   path points into the *shadow copy*, post-splice - `sourceIndex` is
   built from the real tree, with real file paths as keys, and
   `SpliceDebuggerAlways`'s own `debugger: always` lines are real,
   inserted lines, not a zero-width marker, so every task's own line
   number shifts downward in the copy by however many *other* tasks
   earlier in the same file already got their own line spliced in above
   it (confirmed by reading `spliceFile`, not assumed - a single-task
   file, or the first task in a multi-task one, comes out unshifted,
   which is exactly what made this easy to miss testing with a single-
   task-per-file fixture). Fixed with a new `debugcopy.BuildPathMapping`,
   computed from the exact same `locations` `SpliceDebuggerAlways` itself
   spliced from, translating a copy-side "path:line" straight to the
   original's - verified against a *real* spliced file's own actual line
   numbers in a new test, not just hand-derived arithmetic. Confirmed
   live on a 5-task single-file playbook: "Task definition" now shows
   the exact right task's source, at the exact right original line.
2. **The drill-down sometimes opened with only an Output tab reading
   "no output recorded for `<host>`"**, every other tab missing -
   until navigating to a different host row and back, which somehow
   fixed it. Root cause: the debugger's own REPL pause notification
   (`debugApplyPauseResult`) and the jsonl event reporting that exact
   host's result (`debugDrainEvents`) arrive on two entirely independent
   pipes and goroutines, with no ordering guarantee between them - the
   auto-open was racing the very data it needed to render anything.
   Fixed by not opening immediately unless that host's result is already
   on `debugCursor.CurrentTask`; otherwise remembering the target
   (`debugPendingOpenTask`/`Host`) and letting `debugDrainEvents`' own
   event-apply path open it the moment the event actually arrives,
   whichever of the two that turns out to be.
3. **No way to see which host line the debugger is actually paused
   on**, once a task has more than one host and its own row is expanded -
   requested as a `›` marker, `chrome_debug_paused`'s own color. Added as
   `HostLabel`'s own `marker` parameter (mirroring `TaskLabel`'s existing
   one for the breakpoint `●`) - same "one slot, several mutually
   exclusive meanings" shape as the existing warning-glyph prefix, since
   a host can genuinely be both the paused one and carry a warning at
   once, and there's only one column to show either in.

## Stage 2 follow-up round 3: two more, from real use (2026-10-18)

1. **F9 at idle, before ever navigating, silently armed nothing at all.**
   `debugToggleBreakpoint` resolved its own target from `s.currentID` -
   but that field is only ever set by a genuine `SetCurrentItem` call
   made from *outside* `rebuild()` (`TreeList`'s own `SetChangedFunc`
   bails out whenever `s.rebuilding` is true, guarding against a
   recursive rebuild). Debug mode's own following-pinned selection
   (`rebuild()`'s own debug-aware `selectedIndex` branch, the "cursor
   defaults to the first task" fix from the previous round) is applied
   *from inside* `rebuild()` itself, so `s.currentID` never actually gets
   synced to it - confirmed live by pressing F9 then F5 immediately on
   open and watching the whole playbook run with no pause at all. Fixed
   with a new `debugCursorTask()`, mirroring `rebuild()`'s own
   resolution instead of trusting `currentID` whenever still following.
2. **The drill-down kept the plain navy chrome regardless of the
   session's own running/paused state.** `s.outputBottomBar`/
   `s.outputTabs`/`s.splitDivider` - the drill-down's three remaining
   pieces of chrome, on top of the top/split-header bars' own progress-
   fill (already dynamic for free via `chromeColorName`'s own color
   tags) - were only ever styled once, at construction, from a static
   snapshot (`s.chromeStyle`/`s.chromeBg`) that's never debug-aware.
   Fixed by extending `debugRefreshChrome` to update all three, on every
   debug state transition, the same way it already did for the tree's
   own bottom bar.

## Stage 2 follow-up round 4: `include_tasks`/`include_role` (2026-10-18)

**Reported live:** with `include_tasks`/`include_role` in the playbook, it
looked like the run kept going through the including task over and over,
rather than progressing - not a crash, just visibly wrong.

**Root cause:** confirmed live, not guessed - `ansible-playbook
--list-tasks` never expands `include_tasks`/`include_role` at all, even
for a plain literal filename with no templating, loop, or conditional (a
harder limit than "dynamic targets can't be predicted" - *no* include's
contents are ever enumerable this way, regardless of how static they
look). So the skeleton `Cursor` resolves events against has exactly one
entry for the include point itself, never one for anything inside it.
`runner.ProgressTracker.Advance`'s own doc comment already says what it
does on a miss: "the tracker's cursor/matched are left completely
untouched" - meaning every included task's own task-start event kept
resolving `Cursor.CurrentTask` back to whatever was last *actually*
matched (the include point's own row), silently overwriting its outcome
on every included task, instead of erroring or doing nothing.

This is the exact same mechanism a handler's task-start already hit
(`ProgressTracker.Advance`'s own doc comment calls a handler's miss
"fully expected, not evidence of drift" - handlers aren't in the skeleton
either) - already a latent instance of the same bug, just never reported
because a handler and the task that happened to precede it rarely has a
host result worth noticing get silently dropped.

**Considered and rejected:** predicting included tasks up front, the
"natural-looking" fix, so breakpoints could be armed on them before the
run starts too - rejected because of the `--list-tasks` limitation above:
there is no static source of truth for an include's contents to predict
from, for *any* `include_tasks`/`include_role`, so this would need our
own recursive YAML parsing, and would still have a hard ceiling (a
templated path, a data-driven loop count) no static analysis can lift.
Consistent with "v1 scope" (Phase 1, above): tasks inside an include were
already explicitly out of scope for prediction, for the same reason.

**Fix:** when `Cursor.Apply`'s task-start case gets a miss (detected by
comparing `Tracker.Position()` before/after `Advance` - unchanged means
no match was found), it now calls a new `insertDynamicTask`, which builds
a fresh `*TaskNode` and splices it into the current play's own `Tasks`
right after wherever `CurrentTask` currently sits - not appended at the
end of the play, which would read as having run after tasks that, in
reality, come later. `CurrentTask` moves to point at it, exactly as a
genuine skeleton match already would, so every later event for it (its
own outcome, and - if the included batch itself contains several tasks -
each subsequent one anchoring off the previous) lands correctly. This is
the live-growth behavior a plain run's own `PlaybookState.Apply` already
has for free (`findOrCreateTask` doesn't care whether a task was
predicted); `Cursor` just needed its own version, since it deliberately
never calls `Apply` at all (see `SeedState`'s own doc comment).

A newly-inserted task can't have a breakpoint armed on it *before* it
first appears (it doesn't exist yet) - F9 still works on it reactively,
once it's visible, like any other task row. Progress-bar percentage
doesn't move while processing an included batch (none of it counts
toward the skeleton's own total) and catches up once the real skeleton
resumes matching afterward - an accepted characteristic of the
progress indicator's own prediction, not a new gap.

**Known, accepted gap carried forward, not solved here:** under
`strategy: free`, the same conceptual included task can fire its own
task-start once per host (`aggregate.go`'s own "Aggregation" notes in
main `CLAUDE.md` - dynamic includes under `strategy: free` get per-host
task ids). `findOrCreateTask` resolves that back to one shared row by
matching on path once id can't be trusted; this fix doesn't replicate
that - each (host, iteration) combination gets its own row instead of
one shared one. No data loss or misattribution, just a less-collapsed
tree than a plain run's equivalent. `strategy: free` was already a
documented gap for debug mode's progress prediction specifically; this
extends it slightly rather than introducing a new one.

Verified live: an `include_tasks` with two sub-tasks, both a plain
invocation and one wrapped in a `loop:` (two full passes through the
include) - every included task gets its own correctly-ordered row with
its own correct outcome, the include point's own row stays untouched,
and the run correctly resyncs to the real skeleton's next task
afterward. New unit tests: `TestCursor_ApplyInsertsUnmatchedTasksLiveInPlace`,
`TestCursor_ApplyInsertsHandlerTasksLive` (`internal/debugverb/
cursor_test.go`).

## Stage 2 follow-up round 5: no `--diff` at all (2026-10-18)

**Reported live:** a `lineinfile` task that should have changed a file
showed no Diff tab at all.

**Root cause:** `debugrun.Spawn`'s own `cmdArgs` never appended `--diff`,
unlike `runner.SpawnGeneration`'s own identical-looking code (`internal/
runner/process.go`), which always does, specifically so `BuildDiffTab`
has something to show whenever a module supports diff mode - not just
when the user happens to pass `--diff` themselves. A debug session was
spawned via a completely separate path (`debugrun.Spawn`, never
`SpawnGeneration`), so it never inherited that behavior - the Diff tab
could never have shown anything in debug mode, for any task, regardless
of whether a real change happened. Fixed by adding the identical
`--diff` append to `debugrun.Spawn`'s own `cmdArgs`.

Verified live: a `lineinfile` task that actually changes a file's
content now shows a real unified diff on the Diff tab, same as a plain
`run` already would.

## Mouse: toggle a breakpoint by clicking a task row's first column (2026-10-18)

**Requested live:** toggle a breakpoint with the mouse, by clicking in a
task row's own first column - the same column `BreakpointMarkerText`'s
`●` already occupies, whether or not it's currently showing.

A click anywhere on a task row already activated that row's own
`Selected` callback (expand/collapse) via `TreeList.MouseHandler`
(`uikit/treelist.go`) - `uikit.FlattenRows`'s per-row `Selected` closure,
unaware of debug mode at all. Column-sensitivity (first column vs.
anywhere else on the same row) had to be decided *before* that normal
activation, in `liveSession`'s own mouse dispatch
(`livesession_mouse.go`), not inside `TreeList`/`FlattenRows` - neither
has (or should gain) any concept of "armed breakpoint."

New `debugTaskAtFirstColumn(x, y)` (`livesession_debug.go`): true only
when `x` is exactly `s.list`'s own leftmost screen column (from
`GetInnerRect()`) and the row resolved from `y` (via `s.list.GetOffset()`
and `s.currentRows`, the exact slice `s.list`'s own rows are built from)
is a `*playbook.TaskNode` - false for every other column, a play header
or host row at that position, or a non-debug session. `debugToggleBreakpoint`
(F9's own body) split into that plus a new `debugToggleBreakpointFor(task)`,
so the click path can toggle an arbitrary clicked task, not just the
cursor's current one.

Wired into both tree-click paths - full-screen (`handleTreeMouse`) and
two-pane (`handleOutputViewMouse`'s split-mode branch,
`TwoPanedLayout.md`) - checked first, before either path's own
unconditional fall-through/`MouseHandler()` call that would otherwise
also fire the row's normal expand/collapse. On a match: toggle, then
`s.list.SetCurrentItem(index)` to move the cursor there too (matching a
plain click's own "activate, then select" ordering) - deliberately not
`TreeList.MouseHandler()`'s own `activate(index)`, which is exactly the
expand/collapse this is replacing for that one column.

Verified live, with real SGR mouse clicks sent via `tmux` (press+release
at an exact column/row) rather than only reasoning about the code: arm/
disarm by clicking column one, a click anywhere else on the same row
still expands/collapses normally and never disturbs the marker, both in
full-screen and two-pane mode, and a mouse-armed breakpoint genuinely
pauses the run exactly like one armed with F9.

## Stage 3: Resolved Variables tab, debug mode only (2026-10-18)

**Requested live:** bring back the drill-down's "Resolved" tab - but
only for `debug`, using the real variable data a debug session now has
that a plain run never did; explicitly not reviving `resolvedTabEnabled`
(`internal/session/resolved.go`) for plain `run`/`rerun`/`role`, which
stays `false`. Loop variables stay unresolvable by nature (understood,
not chased); variables inside `when:` conditions are explicitly
postponed (see "Scope, deliberately not solved here" below - it turns
out there's nothing *to* solve, this mechanism never touches them at
all).

**Where the data comes from.** `internal/debugrun.Session.Query` already
existed, fully implemented and tested, but uncalled from anywhere in
`internal/session` - a leftover from the 2026-10-14 dual-channel spike's
own `print(json.dumps(expr, default=str))` trick. `Query("task_vars")`
asks the debugger's own REPL, at its *current* pause, for the exact
pre-task variable snapshot that task's own `{{ }}` expressions would
have resolved against - confirmed (independently, twice, across two
different spikes - "Pivot spike"/"dual-channel spike" above) to be the
*pre-task* snapshot specifically: at a later task's own pause, querying
`task_vars` still reads an earlier task's `register`/`set_fact` value
from *before* that later task ran, exactly the semantics "what did this
task see" needs.

**Capture timing is the one real constraint.** The REPL has no notion of
history - once `Continue`/`Step` moves past a pause, `task_vars` for it
is gone for good. New `debugCaptureTaskVars` (`livesession_debug.go`)
queries and caches it the moment a pause surfaces, synchronously, inside
`debugApplyPauseResult` itself - called right then and there is the
*only* chance this data will ever be collectable at all, not a choice
made for convenience. Safe to do synchronously (not a background
goroutine): the REPL is guaranteed idle at that exact point (nothing
else drives it until the user's next F5/F9/F10), and it's one local-pipe
round-trip to a child already sitting at its prompt.

**Scope, deliberately not solved here: a task whose breakpoint was never
armed.** `debugrun`'s own `BreakpointSet`-driven `applyPolicy` silently
`Continue()`s straight past every pause that isn't armed - that loop
lives entirely inside `internal/debugrun`, so `debugCaptureTaskVars` is
never even reached for those pauses; `internal/session` never sees them
at all. A task actually paused at, stepped to, or erroring on gets a
Resolved tab; one that ran straight through doesn't - the tab stays
exactly as absent for it as it already was, same convention the tab
always had. Capturing every pause regardless would mean threading a
capture hook into `debugrun`'s own policy loop itself, not something
reachable from `internal/session` alone - left as a known gap, not a
bug, and not attempted this round.

**Rendering reuses the existing mechanism, seeded differently - not
reimplemented.** `resolveTaskValues`'s "Consuming the data" plan (2026-
10-14, above) holds: real Jinja evaluation of the task's own source
still has to happen somewhere, and "let Ansible do the actual work" via
the same `ansible.builtin.template` stub stays the right call - just fed
from the captured snapshot instead of letting the stub re-gather facts/
host_vars/group_vars fresh. New `resolveTaskValuesFromSnapshot` writes
the snapshot to a temp JSON file and calls `resolveTaskValues` with a
new `extraVarsFile` parameter, appended as `--extra-vars @file` (ansible's
own highest-precedence layer) - last on the command line, so it wins
over anything `rest` itself might also carry. Strictly more accurate
than a fresh-gathered plain-run resolve ever could be: the snapshot
already reflects every `register`/`set_fact`/earlier-task update visible
to the real run at the exact moment it paused.

`showOutputWithOrigin`'s existing async-fetch-on-open machinery
(`livesession_output.go`) needed no new shape, only a second trigger
condition alongside `resolvedTabEnabled`: `s.debugMode` plus a cached
snapshot for this `(task, host)` in the new `s.debugTaskVarsCache`. Same
`s.resolveCache`/`ResolvedRender` the plain-run path already uses for
the final rendered result - `BuildOutputTabs`/`ResolvedTabHidden`/
`BuildResolvedTab` (`uikit/tui_drilldown.go`) needed no changes
whatsoever, confirming the original design's own instinct that this tab
never needed to know where its data came from.

**Two real, pre-existing bugs in `resolveTaskValues` itself, found while
verifying live** (this mechanism had never actually run in production
before - `resolvedTabEnabled` has been `false` since before this stage
existed):
1. The stub never set `gather_facts: false` - harmless for a reachable
   host, but a playbook relying on `connection: local` (this project's
   own test fixtures among them) has no way to satisfy a real connection
   attempt during fact-gathering. Fixed, but *only* when a snapshot is
   supplied (`extraVarsFile != ""`) - a plain-run resolve, with no
   snapshot to fall back on, may still genuinely need a fresh gather to
   resolve a facts-dependent expression at all, so that path keeps
   gathering by default.
2. The delegated task set `delegate_to: localhost` but never
   `connection: local` - harmless for a real remote host, but for a host
   literally named "localhost" via an *explicit* inventory entry (`-i
   localhost,`, again this project's own fixtures), only ansible's own
   *implicit*, auto-added localhost gets the automatic local-connection
   treatment; an explicit one still tries real SSH. Fixed unconditionally
   (both resolve paths) - this is never wrong to have on a
   `delegate_to: localhost` task.
   (`internal/template.WriteTemplateStub` has this same latent gap, for
   the same reason - noted, not fixed, since `tangsible template` has
   never hit it in practice targeting real remote hosts.)

Verified live: a two-task fixture where the second task's own `{{ }}`
expression depends on a `set_fact` the first task made - something a
fresh-gathered plain-run resolve could never have seen at all - renders
the real substituted value correctly. A looped task's own `{{ item }}`
falls back to its own literal text exactly as `wrapJinjaDefaults` always
intended, and - since that renders identical to "Task definition" -
`ResolvedTabHidden` correctly omits the tab rather than showing a
not-actually-resolved value. A task whose breakpoint was never armed
shows no Resolved tab at all, confirming the "only a surfaced pause"
scope above.

## Stage 3 follow-up: every task, not just a surfaced pause; a second, unrelated bug (2026-10-18)

**Reported live, a misunderstanding of Stage 3's own scope:** the
Resolved tab should be available in *any* task's drill-down, not just
one actually paused at/stepped to/erroring on - Stage 3's own "accepted
v1 scope" above undersold what was actually wanted.

**Why "only a surfaced pause" wasn't actually a hard limit.** The real
constraint was narrower than first assumed: `debugrun`'s own
`BreakpointSet`-driven `applyPolicy` (`breakpoints.go`) *does* reach
every single pause in the whole run, including one whose task isn't
armed - it just silently calls `Continue()` past it in a tight loop
*internal to that package*, discarding each one before `internal/
session` ever sees it. The data was reachable the whole time; nothing
outside `debugrun` had ever been given a chance to look.

**Fix: a hook inside the loop itself.** `RunWithBreakpoints`/
`ContinueWithBreakpoints`/`applyPolicy` gained a new `onPause
func(Pause)` parameter, called for *every* pause reached - surfaced or
silently continued past - before the surface/continue decision. New
test `TestBreakpoints_OnPauseFiresForEverySilentlyContinuedPause` pins
this: with an empty `BreakpointSet`, a whole three-task/two-host run
still fires `onPause` six times in one `RunWithBreakpoints` call, not
once.

**The capture mechanism moved, and had to change its own cache key.**
`debugOnPause` (`livesession_debug.go`) replaces the old
`debugCaptureTaskVars`, called from `onPause` on `debugStartOrContinue`'s
own background goroutine (not the event-loop one - only the actual
cache write is `QueueUpdateDraw`-wrapped) for every pause instead of
just the one `debugApplyPauseResult` used to see. This meant
`debugTaskVarsCache` could no longer be keyed by `resolveKey{*playbook.
TaskNode, string}`: `onPause` fires in lockstep with the REPL's own
pause sequence, racing independently against the jsonl event stream
that `debugCursor.CurrentTask` is driven by (the exact race
`debugPendingOpenTask`/`Host` already exists to cope with, for one
pause at a time - reliably doing the equivalent for every pause in an
unbroken, rapid, auto-continued sequence would need a comparably
complex buffering mechanism of its own, not attempted here). New
`debugVarsKey{label, host string}` keys by the debugger's own pause
identity instead - `Pause.Task`/`Pause.Host` - sidestepping the race
entirely at the cost of accepting the same label-based ambiguity
breakpoints themselves already do (`debugverb.TaskLabel`'s own doc
comment: two tasks sharing an exact name alias to the same one) - a
pre-existing, accepted limitation elsewhere in this same feature area,
not a new one introduced here.

Verified live: a two-task fixture run straight through with F5 and
*nothing* armed at all - no pause ever surfaced - still shows a correct
Resolved tab, with the real substituted value, when opening the
drill-down for either task afterward (a task with nothing to resolve,
e.g. one with no `{{ }}` expression at all, correctly still shows no
tab - `ResolvedTabHidden`, unchanged).

**Second, unrelated bug, reported in the same message: the per-task
busy-animation (spinner) never appeared anywhere in debug mode.** Root
cause: `PlaybookState.IncompleteTasks` (what `TaskLabel`'s "active" case
reads to decide which row gets a spinner instead of its usual prefix)
is driven entirely by `TaskNode.InFlight`, set by `Apply`'s own handling
of `v2_runner_on_start` and cleared by `recordHost` - both exclusively
inside `internal/playbook`, and both never gone through at all for a
debug session (`SeedState`'s own doc comment: a debug session's events
never go through `Apply`). `debugverb.Cursor.Apply` simply never
touched it, and couldn't have anyway - the field was unexported
(`inFlight`), on the stated premise that "nothing outside this package
needs per-host in-flight detail," which stopped being true the moment
debug mode needed to mirror its effect from outside the package. Fixed
by exporting it (`InFlight`) and adding a `v2_runner_on_start` case to
`Cursor.Apply` (sets `Started`/`InFlight` against `CurrentTask`,
mirroring `Apply`'s own case - task-start always precedes any host's
own dispatch, so `CurrentTask` is already correct by the time this
fires) plus clearing `InFlight` alongside the existing `Hosts`/`Raw`/
`Ignored` writes in the terminal-outcome case. New test
`TestCursor_ApplyTracksInFlight`. Verified live with a deliberately slow
task (`command: sleep 8`): the spinner now animates in column one while
it's running, and disappears once it completes, exactly matching a
plain run's own tree.

## Stage 3 follow-up round 2: `readUntilPrompt` was O(n^2) (2026-10-18)

**Reported live, right after the previous round shipped:** a single task
on a single host now took over ten seconds; the whole point of Stage 3
was that the data was already being collected, not newly fetched slowly.

**Root cause, confirmed with an isolated probe before touching any
code** (`cmd/perfprobe`, a throwaway timing harness built inside the
module tree since `internal/...` packages aren't importable from
outside it, deleted once done): `debugrun.Session.readUntilPrompt`
re-stringified the *entire* accumulated REPL buffer (`buf.String()`, a
full copy) and re-ran `promptRe` against it, once per byte received. A
one-time O(n) prompt check is fine for any response; doing it n times
over a buffer that grows to n is O(n^2) - and this was always true, just
never triggered: every caller before the previous round (`Start`/
`Continue`'s own short prompt lines, the existing test's small targeted
query) returned responses far too small to multiply into anything
noticeable. `Query("task_vars")` against a host with facts gathered
returns 100KB+, which is exactly what the previous round made Stage 3
call on *every* pause - the probe measured ~20s for one such 112KB
response.

**Fix:** match only the last 4096 bytes of the buffer (comfortably
larger than any real `"[host] TASK: name (debug)> "` prompt line, nowhere
near the size of a real data response), and match bytes directly
(`FindSubmatch` against `buf.Bytes()`'s tail) instead of copying via
`buf.String()` first. Confirmed with the same probe (~0.74s for the same
112KB response afterward), then live in a full tmux debug session: a
three-task, one-host fixture with facts gathering enabled dropped from
~41-66s to ~5s wall-clock for a complete run.

## Stage 3 follow-up round 3: resolving when:/failed_when:/changed_when:/until: (2026-10-19)

**Requested live:** the Resolved tab leaves conditions untouched - ansible
evaluates `when:`, `failed_when:`, `changed_when:`, and `until:` as bare
Jinja expressions, never `{{ }}`-wrapped, so `wrapJinjaDefaults`'s own
`{{ }}`-regex (resolved.go) never finds anything to substitute in them.
The simplest fix - wrapping the *whole* condition in `{{ }}` and letting
it render - was considered and rejected: it would collapse `something !=
0` down to a flat `True`/`False`, throwing away exactly the "which
operand turned out to be what" detail that makes a condition worth
reading in the first place. What's wanted instead: resolve only the
*variable references* within the expression, leaving every operator,
filter, test, and literal exactly as written - `something != 0` becomes
`1 != 0`, not `True`.

**Why this needed real Jinja parsing, not another regex.** A regex-based
identifier substitution (tried first, in exploration, never committed)
immediately misfires on three ordinary shapes: a filter name (`| default`
would rewrite `default` itself if it happened to collide with a vars
key), an `is defined`/`is not` test name, and dotted attribute access
(`foo.bar.baz` would try to resolve `bar`/`baz` as if they were their own
free-standing variables). The fix needed the *real* Jinja grammar - which
means running Jinja's own parser, not reimplementing it. There's no
Go-native Jinja parser in this codebase (nor vendored), but `jinja2` -
the same library `ansible-playbook`'s own templating depends on - is
already present on any machine capable of running this project at all.
This is this project's first use of `python3` as its own subprocess
(previously only `ansible-playbook`/`ansible-inventory`/`ansible-config`/
`ansible-doc` were ever shelled out to) - confirmed no existing seam for
"run a Python snippet" existed before this.

**The resolver (`resolve_condition.py`, go:embed'd into
`resolve_condition.go`, run as `python3 -c <script>`):** parses an
expression with `jinja2.Environment().parse()`, then recursively
replaces every *maximal pure-data-access subtree* - a bare variable
reference, or an attribute/item-access chain rooted at one (`x`, `x.y.z`,
`x['y']`) - with its fully resolved value, provided the whole chain
resolves against the captured `task_vars` snapshot (same snapshot the
module-arg resolver already uses) and the result is plain JSON-safe data
(not, say, a bound method reached via `x.split`). Everything else -
`Compare`, `And`/`Or`/`Not`, `Filter`, `Test`, `Call`, arithmetic,
`Concat`, list/dict/tuple literals, the ternary `CondExpr` - is walked
recursively (so a variable nested inside any of them still gets its own
chance) but never collapsed itself; only genuine data-access subtrees
ever become a `Const`. The simplified tree is then rendered back to text
by a small hand-written unparser covering exactly this grammar subset -
not a general Jinja-to-source tool, since conditions never need the rest
of Jinja's grammar (control blocks, macros, `set`). Anything this can't
handle - a syntax error, an ansible-specific Jinja extension plain
`jinja2` doesn't know, a reference nothing in the snapshot satisfies - is
echoed back completely unchanged, the exact same silent-fallback contract
`wrapJinjaDefaults` already has for an undefined `{{ }}` variable.

**Splicing the result back in (`resolve_condition.go`):**
`extractTaskConditions` parses a task's raw source text (the same
`taskSource` blob `resolveTaskValues` already renders) with `yaml.v3`,
finds every `when`/`failed_when`/`changed_when`/`until` key at the
task's own top level - a plain scalar or a block list (ansible ANDs list
items; a flow list or a folded/literal block scalar is left alone, not
chased further) - and records both its exact original text span (for
splicing) and its expression(s) (for resolving), using the node's own
`Line`/`Column` the same way `internal/source`'s own line-slicing
already does. `resolveConditionsInText` then resolves every expression
in one batched Python call and replaces each key's original span with
its resolved rendering (preserving the original's own indentation and
scalar/list shape) inside the text `resolveTaskValues` already produced
- safe because Jinja preserves literal text outside `{{ }}`/`{% %}`
verbatim, so a condition key's own span is guaranteed byte-identical
between the raw source and the rendered output (the one exception - a
condition whose own text happens to contain a literal `{{ }}` - just
silently fails to find its span and is left alone, an even narrower
version of a gap `wrapJinjaDefaults` already accepts).

**Scope, at the time this was written:** `failed_when:`/`changed_when:`/
`until:` typically reference the task's own `register:` target, which
doesn't exist in `task_vars` (a pre-task snapshot - see the 2026-09-27
pivot spike above) - these fell back to unresolved text in practice, same
as a loop variable or anything else genuinely undefined. Verified live
with a three-task fixture (`something: 1`, no facts gathered): a scalar
`when: something != 0` resolved to `when: 1 != 0`; a list-form `when:`
mixing a resolvable `something == 1` with an unresolvable
`ansible_os_family is defined or true` (facts not gathered) resolved the
first line and left the second exactly as written, both inside one
correctly-reconstructed list block; a task with no condition at all kept
showing no Resolved tab when nothing else about it needed resolving
either - no regression from this on tasks the feature doesn't apply to.
**This gap is closed** - see "Stage 3 follow-up round 4" below.

## Stage 3 follow-up round 4: resolving a task's own register: target too (2026-10-19)

**Requested live:** a real task (`ansible.builtin.command` with
`register: result`, `delegate_to: localhost`, `failed_when: result.rc not
in [0, 1, 2]`, `changed_when: result.rc not in [1]`) showed `result.rc`
completely unresolved in both conditions - round 3's own documented
"not chased further" gap, immediately hit in practice.

**Why this turned out to be fixable after all.** Round 3's gap analysis
was right that `task_vars` (the snapshot `debugOnPause`,
`livesession_debug.go`, already captures via `Session.Query("task_vars")`
on every pause) never reflects the paused task's own `register:` output -
confirmed directly, live: `Query("task_vars")` for a `register: result`
task's own pause doesn't have a `result` key at all. But `debugger:
always` fires *after* the task completes (`debug_closure` wraps
`StrategyBase._process_pending_results` - the 2026-09-27 pivot spike,
above), and the debugger's own scope at that exact same pause separately
exposes the just-completed task and its result as their own top-level
names (`Session.Query`'s own doc comment: "task/host/task_vars/
play_context/result"). `task.register` gives the variable name ansible
would store the result under (`None` if the task has no `register:` at
all); `result._return_data` gives that value - both confirmed live,
queryable at the exact same pause `task_vars` itself comes from, via the
exact same `Session.Query` round-trip round 3's own module-arg resolve
already uses, no new mechanism needed:

```
task.register = "result"
result._return_data = {"changed": true, "stdout": "", "stderr": "",
  "rc": 0, "cmd": ["/bin/true"], ...}
```

**The fix (`mergeRegisteredResult`, `livesession_debug.go`):** right
after `debugOnPause`'s existing `Query("task_vars")` call, query
`task.register`; if it's a non-empty string, query `result._return_data`
and merge `{<register name>: <that value>}` into the snapshot's own
top-level object before caching it - so `failed_when:`/`changed_when:`
conditions referencing that task's own `register:` target now resolve
through the exact same `resolveConditionsInText` path round 3 already
built, with no changes needed there at all. Two extra `Query` round-trips
per pause (cheap: `task.register` is a short string; `result.
_return_data` only runs at all when a task actually registers something),
same cost class as the existing `task_vars` query. Falls back to the
snapshot completely unchanged on any error (no `register:` on this task,
a query failure, unparseable JSON) - the same silent-fallback contract
every other piece of this mechanism already has.

**Verified live** (`TestMergeRegisteredResult_Live`, and a full tmux
debug run reproducing the reported task's exact shape): the Resolved tab
now shows

```
when: 'charon' != 'localhost'
register: result
failed_when: 0 not in [0, 1, 2]
changed_when: 0 not in [1]
```

- `result.rc` resolved to `0` in both conditions, alongside `when:`'s own
unrelated variables resolving exactly as round 3 already handled.

**Still not chased further:** a loop's own per-item `register:` (which
ansible accumulates into a `results` list across iterations, not a
single `result._return_data`), and `async:`/`poll:`'s own registered
status-check result, aren't covered by this - `task.register`/`result.
_return_data` describe one single, synchronous task completion, the only
shape this round addressed.

## Stage 3 follow-up round 5: spinner froze before a pause actually surfaced (2026-10-19)

Reported live: a task's busy spinner animated only for ansible's own
real execution time, then froze - even though debug mode then spends
real additional time (the `task_vars` query, then the `task.register`/
`result._return_data` queries round 3 above added, all debugger-REPL
round-trips) before the pause is actually surfaced (tree re-rendered as
paused, drill-down auto-opened). The gap is exactly `Query(task_vars)`'s
own cost (`readUntilPrompt`'s O(n) fix, Stage 3 follow-up round 2 above
- still proportional to how much the debugger's REPL echoes back, which
grows with the task's own variable count) plus two more round-trips -
not imagined, and not free.

Root cause: `activeTasks()` (`livesession_rebuild.go`), the set
`FlattenRows`/`TaskLabel` use to decide which task row gets the spinner
prefix, is built purely from `playbook.PlaybookState.IncompleteTasks()`
- membership in a task's own `InFlight` map. In debug mode,
`debugverb.Cursor.Apply` clears `InFlight` the instant ansible's real
jsonl result event arrives (a completely separate pipe from the
debugger REPL) - before `debugOnPause`'s own query work even starts, let
alone finishes.

Fix: `activeTasks()` additionally marks `s.debugCursor.CurrentTask`
active for as long as `s.debugDriving` stays true. `debugDriving` was
already exactly the right signal with no changes needed to its own
lifecycle - set before the blocking `RunWithBreakpoints`/
`ContinueWithBreakpoints` call, cleared only in `debugApplyPauseResult`,
which runs after that call (and so after `debugOnPause`'s queries)
returns. `debugCursor.CurrentTask` itself is stable across this whole
window - ansible can't dispatch a next task-start event until the
debugger resumes, so nothing else could change which task this points
to in the meantime.

**Verified live**: a `gather_facts: true` fixture (inflating `task_vars`
enough to make the query gap visible, ~2-3s) showed the spinner
continuing to animate on `step one` well after every row's host column
already showed `localhost` (i.e. after the real result events had all
arrived), stopping only once the run actually finished.

## Stage 3 follow-up round 6: `--vars-at-stops-only` (2026-10-19)

Reported live: debug mode is noticeably slower overall than a plain run,
beyond just the per-pause gap round 5 above made visible - as expected
given the extra machinery, but the user asked whether capturing
variables for *every* task (not just the ones actually stopped at) was
a significant, avoidable part of it.

Confirmed yes: `debugOnPause` (`livesession_debug.go`) runs its
`task_vars` query - and, since round 4, `task.register`/
`result._return_data` too - for every single pause `applyPolicy`
(`internal/debugrun/breakpoints.go`) reaches, including one silently
resumed past with no breakpoint armed on it at all. This is deliberate,
not an oversight (round "Resolved tab for every task" - a much older
entry than the numbering above, see git history for `9afb7ff`): it's the
only way the Resolved tab can work for a task you navigate back to
later, one that was never itself a breakpoint stop. But it means every
task in the playbook pays `Query(task_vars)`'s own real cost (round 2's
own profiling: ~0.74s for one gather_facts-sized response, scaling with
the task's own variable count) regardless of whether the session ever
actually stops there - for a playbook with few or no breakpoints armed,
that's most or all of its tasks.

`applyPolicy` already computes, once per pause, exactly the
surface-or-continue decision this needed: `mode == StepOne ||
bps.Has(pause.Task)`. Threaded through as a new `willSurface` parameter
on `onPause` (`RunWithBreakpoints`/`ContinueWithBreakpoints`'s own
signature, now `func(Pause, bool)`) rather than letting each caller
recompute it - the one call site (`debugOnPause`) can't recompute it
itself anyway, since it has no access to `bps`/`mode` from inside the
closure `debugStartOrContinue` builds.

New flag `--vars-at-stops-only` (`internal/debugverb`'s own synthetic
flag, `extractVarsAtStopsOnlyFlag`, stripped before `rest` reaches
anything else - same shape as `config.ExtractRerunFlags`): when set,
`debugOnPause` returns immediately for a pause `willSurface` says won't
be shown, skipping its query work entirely. Default is unset (today's
"every task" behavior, unchanged) - this trades the Resolved tab's own
"works for any task" guarantee for speed, which should stay an explicit
opt-in rather than a surprise.

No change needed to how a missing snapshot is handled: `showOutput`
(`livesession_output.go`) already treats "no `task_vars` snapshot for
this (task, host)" as "nothing to resolve, tab stays absent" - exactly
what a task skipped under `--vars-at-stops-only` looks like, no
different from a task that genuinely never paused at all (e.g. one
ansible-core's debugger never gets a chance to splice `debugger: always`
onto).

**Verified**: `TestBreakpoints_OnPause_WillSurfaceTrueForArmedBreakpoint`/
`TestBreakpoints_OnPauseFiresForEverySilentlyContinuedPause`
(`internal/debugrun/breakpoints_test.go`) confirm `willSurface` agrees
with the actual surface/continue decision in both directions;
`TestExtractVarsAtStopsOnlyFlag` (`internal/debugverb`) covers the flag's
own extraction.

## Stage 3 follow-up round 7: a handler task hung the whole session (2026-10-19)

Reported live: reaching a handler task froze the session completely - F5/
F9/F10 all became no-ops, only `q` still worked (the child process itself
was still alive and reachable, just never progressing).

Root cause: `internal/debugrun`'s `promptRe` (`\[(\S+)\] (TASK: .+?)
\(debug\)> $`) only ever matched `ansible.playbook.task.Task.__repr__`'s
own `"TASK: name"` format. `ansible/playbook/handler.py`'s `Handler` (a
`Task` subclass) overrides `__repr__` to `"HANDLER: name"` instead -
confirmed live by driving a real `ansible-playbook` child directly over
plain pipes (no Go involved) and watching the raw bytes: a handler's own
prompt line reads `[localhost] HANDLER: say hello (debug)> `, which
`promptRe` never matched at all. The prompt had already arrived in full
on the pipe; `readUntilPrompt`'s byte-by-byte loop just kept calling
`ReadByte` waiting for more, which never came, while the child sat
blocked on its own stdin waiting for the next REPL command that was never
going to be sent - a genuine two-sided deadlock, not a slow response.

Fixed by widening the regex to `(?:TASK|HANDLER): ` - confirmed this is
the only other `__repr__` ansible-core's own `Task`/`Handler` classes
produce (`Task.__repr__` itself also covers a `meta:` task as `"TASK:
meta (...)"`, already matched). A second, related bug found alongside it:
`debugverb.TaskLabel` (the breakpoint-set/Resolved-tab-cache key a task
row builds) always produced `"TASK: " + name`, with no way to ask for the
`"HANDLER: "` prefix - so even once the hang above is fixed, a breakpoint
armed on a handler row could never match its own live pause at all
(always silently auto-continuing past it), and the handler's own Resolved
tab would never find its captured snapshot. Fixed by giving `TaskLabel`
an `isHandler bool` parameter, threaded from `*playbook.TaskNode.
IsHandler` at every call site.

**Verified**: `TestSession_HandlerPauseDoesNotHang`
(`internal/debugrun/session_test.go`) - a real session against a
notify-triggered handler, driven through to `eof` inside a 15s
`withTimeout` guard (confirmed this test actually catches the regression,
not just passes vacuously: reverting the regex fix made it fail at the
15s mark with a clear message, not the 10-minute default `go test`
timeout a plain hang would otherwise cost every future test run).
`TestTaskLabel` covers the new `isHandler` parameter. Live-verified in
tmux: a handler-notifying playbook runs end to end with no breakpoints
armed (previously hung indefinitely); stepping (the step key - F11 as of
round 8 below, F10 at the time this was verified) onto the handler's own
pause shows it correctly as the current task, with the same `›` paused
marker and host coloring as any other task, and F9 arms/disarms a
breakpoint on it without issue.

## Stage 3 follow-up round 8: F10/F11 split - step vs. skip to next task (2026-10-19)

Requested live: F10 (`debugrun.StepOne`) surfaces *every* pause
unconditionally, including every other host still left on the current
task - tedious on a many-host run when only the next task actually
mattered, not that host's own individual result. Split into two keys:
F10 now means "skip the rest of this task's hosts, stop at the next
task's first pause" (new `debugrun.StepNextTask` mode); F11 takes over
StepOne's own old behavior unchanged.

`StepNextTask` needed one new piece of state `applyPolicy`
(`internal/debugrun/breakpoints.go`) didn't have before: `fromTask`, the
task label the caller is already paused at - every further pause gets
silently resumed past as long as its own task label still matches
`fromTask`, surfacing unconditionally the instant a different task's
pause arrives (or `bps.Has` would have surfaced it anyway, or eof).
Threaded through `ContinueWithBreakpoints`'s own new parameter;
`RunWithBreakpoints` (the very first `Start()` call, nothing paused at
yet) always passes `""`, which falls back to behaving exactly like
`StepOne` there - there's no "current task" to skip past before anything
has run, so stopping at the very first pause is the only sensible
reading of "next task" in that case.

On `internal/session`'s own side: a new `liveSession.debugLastPauseTask`
field holds the most recent real pause's own task label (set in
`debugApplyPauseResult` alongside `debugPausedHost`), read in
`debugStartOrContinue` right before spawning the background goroutine
and passed down as `fromTask` - deliberately the debugger's own REPL-
stream label, not `debugCursor.CurrentTask` (the jsonl-stream-derived
`*playbook.TaskNode` `debugOnPause`'s own doc comment already explains
can't be reliably correlated to every pause in an auto-continued
sequence).

**Verified**: `TestBreakpoints_StepNextTask_SkipsRemainingHostsOfCurrentTask`
(`internal/debugrun/breakpoints_test.go`) - a 3-task/2-host fixture,
confirming each `StepNextTask` call lands on the next task's first host
regardless of how many hosts remained on the current one, and that the
very first call (via `RunWithBreakpoints`, nothing paused yet) falls back
to `StepOne`'s own "stop at the very first pause" behavior. Live-verified
in tmux with a 3-host, 3-task fixture (`--forks 1` for deterministic
ordering): F11 stepped hostA → hostB on `step one` as before; F10 from
hostB skipped hostC entirely and landed directly on `step two`'s own
hostA, then again straight to `step three`'s hostA - the bottom bar hint
now reads "F10: next task  F11: step" (idle/paused) accordingly.

**Follow-up:** the `?`/F1 help overlay never actually documented F5/F9/
F10 at all - `helptables.go`'s `treeHelp`/`outputHelp` had no entries for
them, a gap predating this round entirely (CLAUDE.md's "Help + bottom
bars" invariant: "changing a binding means updating both there" - this
had never been done even once). Closed alongside the F10/F11 split:
both `treeHelp` and `outputHelp` get a new "Debug mode" section (F5/F9/
F10/F11, one line each), hidden together outside a debug session via a
new `helpIDDebug` entry ID - the hide function itself renamed from
`treeHelpHidden` to `helpEntryHidden` (it was never actually tree-
specific; `outputHelp`'s own `Open` call previously passed `nil`, i.e.
no entries could be conditionally hidden there at all). Verified live:
the section renders in both the tree's and the drill-down's own help
overlay during a debug session, and is completely absent for a plain
`run`.

## Stage 3 follow-up round 9: `--vars-at-stops-only` flipped to `--all-vars`/`-a` (2026-10-19)

Requested: round 6's flag had the default backwards for typical use -
most sessions want the faster "vars at stops only" behavior, not the
slower "every task" one, so capturing every task's variables should be
the opt-in, not the default. Also asked to shorten the flag's own name
while at it.

Renamed (not just re-spelled) `--vars-at-stops-only` to `--all-vars`,
also accepting `-a` (neither spelling collides with an `ansible-playbook`
flag - confirmed against its own `--help` output's short-flag list, same
check round 1's synthetic flags already passed). Flipped the sense
throughout rather than just the name: `SessionPrep.VarsAtStopsOnly` →
`AllVars`, `DebugConfig.VarsAtStopsOnly` → `AllVars`,
`liveSession.debugVarsAtStopsOnly` → `debugAllVars`,
`extractVarsAtStopsOnlyFlag` → `extractAllVarsFlag` (now recognizing
either spelling). `debugOnPause`'s own early-exit
(`livesession_debug.go`) inverted from `if s.debugVarsAtStopsOnly &&
!willSurface` to `if !s.debugAllVars && !willSurface` - the zero value
(flag absent) is now "vars at stops only," matching every other boolean
synthetic flag in this codebase where the zero value is the common case.

No change needed to `applyPolicy`'s own `willSurface` computation
(`internal/debugrun/breakpoints.go`) or to how a missing snapshot is
handled (`showOutput`'s existing "no snapshot, tab stays absent" path) -
only which side of `willSurface` decides to skip the query changed.

**Verified**: `TestExtractAllVarsFlag` (`internal/debugverb`, renamed
from `TestExtractVarsAtStopsOnlyFlag`) covers both spellings, repeated
and combined; `go build ./... && go vet ./... && go test ./...` all
pass. `completions/tangsible.bash`/`tangsible.fish`, `README.md`,
`man/tangsible-debug.1.scd` (regenerated via `make man`), and
`design-docs/Codemap.md` updated to match.

## Stage 4: implemented - the variable watch explorer (2026-10-19)

The UX spec above ("UI details variable explorer"/"Stage 4 UX
decisions") is now built, following the implementation plan worked out
beforehand (three parallel research passes - layout/transient-status
patterns, autocomplete/debug-state, expression evaluation - then a
design pass, then independent verification against the real source
before writing code). Full design detail lives in those two sections
above and isn't repeated here; this entry covers what actually landed
and what was learned building it.

**New**: `internal/session/debugwatch_eval.go` (+ `_test.go`) -
`parseWatchExpr`/`evaluateWatchExpr`, a small hand-written grammar
(`IDENT ('.' IDENT | '[' INDEX ']' | '[' STRING ']')*`, no filters/
operators/calls) and resolver against a flat JSON snapshot, returning a
clean reject/unavailable/resolved tri-state - deliberately not
`resolve_condition.go`'s existing jinja2-backed mechanism, whose "always
echo back unchanged on any failure" contract can't make that
distinction (confirmed by direct tracing during the design pass: a
genuinely undefined root and a plain parse error both produce output
`== input`). `internal/session/livesession_watch.go` - `watchPanel`
(mirrors `rerunFieldSync`/`tabSearchPanel`'s "genuinely separable
sub-struct" shape), its two tview primitives (a `TextView` for the
watched list/hint, an `InputField` for typing), `watchCurrentSnapshot`
(reuses `showOutputWithOrigin`'s own `debugTaskVarsCache` lookup
pattern for whatever host/task is currently selected - a bare task/play/
recap row selection, with no single host to resolve against, is always
"unavailable"), `watchCandidates`/`watchMatchCandidates` (autocomplete,
formatted `"name = value"` since tview's native drop-down has no second
column - confirmed against tview's own source), and
`handleWatchKey`/`handleWatchOpenKey` (`v` opens it, then it's modal
while open, same as the rerun/filter/search dialogs). `outputFlex`/
`outputBody` (`tui.go`) were promoted from local vars to `liveSession`
fields so `rebuild()` could `ResizeItem` the new panel into all three
of `s.flex`/`s.outputFlex`/`s.outputBody` (never `s.treeBody` - two-
pane's tree/left side never shows this panel, per the spec's own
"bottom of the right pane" wording).

**Two real bugs found and fixed during live tmux verification, neither
caught by `go vet`/unit tests**:

- tview's inline tag convention is *lowercase letter sets an attribute,
  uppercase clears it* (confirmed directly in `rivo/tview`'s own
  `strings.go`: a lowercase flag ORs the bit in, anything else - including
  the same letter uppercased - ANDs it back out). The first version of
  `watchPanel.render` used `[orange::S]` for the "unavailable" strike-
  through, which is a silent no-op (clears a bit that was never set) -
  caught only by actually looking for the `ESC[9m` escape code in a
  `capture-pane -e` single-line snapshot, exactly per `design-docs/
  TESTING.md`'s own warning that attribute changes don't always show up
  the way you'd expect across lines. Fixed to lowercase `s`, matching
  every other attribute tag already in this codebase (`::b]` for bold).
- Esc's "close just the autocomplete drop-down" case initially only set
  `watchPanel.acDismissed` and swallowed the event - which updates this
  package's *own* bookkeeping (used for `Up`/`Down`/`Enter` routing) but
  never touches tview's `InputField`'s own internal `autocompleteList`
  field, which is what `Draw()` actually checks to decide whether to
  keep rendering the drop-down. Confirmed live: the drop-down stayed
  visible after the "first" Esc. Fixed by forwarding that one case's
  real event to `InputField.InputHandler()` (which does clear its own
  internal field on a real `KeyEscape`, confirmed in `inputfield.go`)
  instead of swallowing it - the same thing `rerunFieldSync`'s own
  Escape handling already relies on tview to do natively, just reached
  by direct `InputHandler()` forwarding here instead of a passed-through
  `SetInputCapture` event, since this field sits too deep in nested
  Flex/Pages for normal focus-driven dispatch to reach at all (the exact
  `tabSearchPanel.handleComposingKey` gotcha, CLAUDE.md's own tview/
  tcell section).

**Verified live in tmux**, one-pane tree, one-pane drill-down, and
two-pane split, all three: panel fully invisible (zero height) with
nothing watched and the explorer closed, in a plain `run` session as
much as an untouched debug one; `v` opens/closes correctly from every
context; typing shows the autocomplete drop-down (name = value,
excluding already-watched names and non-scalar values) without
previewing a navigated-to suggestion into the field; picking a
suggestion (`Enter`/click-equivalent) both fills the field and commits
it in one step; a manually-typed valid path resolves or renders
unavailable (struck through, orange) correctly against the selected
host/task's own captured snapshot; a genuine syntax error is rejected
with a transient status message (reusing the drill-down's own
`y`-copy-status mechanism) and leaves the typed text in place; `Up`/
`Down` with a non-empty, non-matching field is ignored; `Up`/`Down`
with an empty field selects/deselects a watched row, `Delete` removes
the selected one; closing the explorer leaves watched values visible
and grey, still re-evaluating live as the tree selection moves; the
full Esc-staging matrix (both branches) behaves exactly as specified.
`go build ./... && go vet ./... && go test ./...` and `gofmt -l
internal/` all clean (aside from the one pre-existing, unrelated
`recap_rowtext_test.go` gofmt flag). `CLAUDE.md`, `design-docs/
Codemap.md`, `design-docs/Keyboard-shortcuts.md`, `README.md`, and the
man page updated to match.

**Deferred, not attempted here** (per the implementation plan): mouse
support for the panel, and a static `"v: variables"` bottom-bar
reminder for when the panel is fully hidden.

## Stage 4 follow-up: a stale `--vars-at-stops-only` gave a misleading error (2026-10-19)

Reported live: typing the pre-round-9 flag name (muscle memory) produced
`tangsible: couldn't predict this playbook's own task list (--list-tasks
probe failed) - nothing to debug` - true, but useless: since
`extractAllVarsFlag` no longer recognizes that spelling, it stayed in
`rest` and reached `ansible-playbook`'s own `--list-tasks --list-hosts`
probe as an unrecognized argument, which failed for a real, specific
reason `BuildProgressSkeleton` had never captured in the first place
(no `cmd.Stderr` at all - any failure, for any reason, just returned
`nil`).

A general fix, not a specific one - `runner.BuildProgressSkeleton` now
has a sibling, `BuildProgressSkeletonVerbose`, which also captures and
returns the probe's own stderr (`""` on success) - the plain form still
discards it, unchanged for its other two callers (`session/main.go`,
`runner/generation.go`), where a failed probe is silently tolerated (no
progress bar) and always has been. `PrepareSession` uses the verbose
form and prints the real reason - but trimmed, not raw: confirmed live
that ansible-playbook's own argparse error for *any* unrecognized flag
prints a short usage block and one `ansible-playbook: error: ...` line
immediately followed by its own full `--help` text, 127+ lines no
matter how small the actual problem is. `summarizeProbeStderr` cuts
right after that error line when present; a failure shape with no such
line (a YAML syntax error, say) falls back to a 20-line cap instead,
since there's no single reliable landmark to cut at there.

Considered also rejecting `--vars-at-stops-only` explicitly by name
(the same "usage error" shape `--step`'s own rejection uses), with a
direct pointer to the new name. Dropped: nobody but the one person who
hit this has ever seen the pre-round-9 flag name, so a generic
unrecognized-argument message (now, thanks to the fix above, showing
`ansible-playbook`'s real `unrecognized arguments: --vars-at-stops-only`
text) is exactly as useful without a special case to maintain.

**Verified**: `TestSummarizeProbeStderr` (`internal/debugverb`) covers
the cut-at-error-line case, a short no-error-line passthrough, and the
line-cap fallback; `go build ./... && go vet ./... && go test ./...`
all pass. Live: `tangsible debug <playbook> --vars-at-stops-only` (and
an arbitrary unrecognized flag like `--some-made-up-flag`) now shows the
real usage+error text instead of the generic "probe failed" message
alone.

## Stage 4 follow-up round 2: drop unavailable's own styling, chrome-colored panel, bottom-bar hints, nav/delete fixes (2026-10-20)

Five small changes requested after trying the explorer live, all in
`internal/session/livesession_watch.go` unless noted:

- **Unavailable rendering simplified.** The "Four colors" decision above
  gave unavailable its own color (`IgnoredColor`) plus strike-through.
  Dropped: a rejected, out-of-context, and merely-doesn't-resolve-yet
  expression all now render identically as `<undefined>`, in whatever
  color the row would use anyway (`StandardTextColor` while open,
  `NothingReportedColor` once the explorer closes) - one fewer color to
  explain, and a plain, ASCII-safe placeholder instead of leaning on
  strike-through support that varies by terminal. `formatWatchValue`
  replaces the old `renderValuePlain`, now used for both the open and
  closed states (they'd already converged on "just show the value or a
  placeholder" - only the surrounding color differed).
- **Chrome-colored panel**, tried live per request: `watchPanel.render`
  now takes a `chromeBg tcell.Color` (the caller passes
  `s.debugChromeBg()`, already existing - orange/maroon while debug-
  running/paused, otherwise whatever a plain `run` uses) and paints it
  onto both the list `TextView` and the input field (`SetBackgroundColor`
  on both, plus `SetFieldBackgroundColor` for the field's own typed-text
  rectangle) - read fresh every `render()` call, so it tracks a live
  running/paused flip the same way the rest of the chrome does. The
  autocomplete drop-down's own colors (`uikit.StyleAutocomplete`) were
  left alone - a separate, temporary overlay, not "the panel" itself.
- **Bottom-bar hints, not a separate line.** The hint line used to be the
  panel's own trailing `TextView` row (the "Stage 4: implemented" entry
  above). Moved to whichever bottom chrome bar is already on screen -
  `s.bottomBar` for the tree page, `s.outputBottomBar` for the drill-
  down/split page, the same `s.viewingOutput` switch `watchShowRejected`
  already used - replacing its normal text while the explorer is open and
  restoring it the instant the explorer closes (new
  `watchSyncBottomBar`, called at every watch-key transition that
  changes what the hint should say, deliberately *not* called from
  `rebuild()` itself: unlike `s.watch.render()`, these two bars aren't
  otherwise touched on every `rebuild()` pass, which is what lets the
  drill-down's own `y`-copy-status message survive an unrelated
  heartbeat-driven redraw - doing this from `rebuild()` instead would
  have clobbered that message, and the explorer's own rejected-expression
  status right after it's shown). Safe to treat as unconditional since
  the explorer is fully modal while open - nothing else can be triggering
  a competing message in the meantime. `height()`'s old "+2 for a blank
  separator and the hint line" is gone with it.
- **Two small, real bugs**, both found by exercising the panel live, not
  reported in advance:
  - `Up` with nothing selected and an empty input field did nothing at
    all, forever - the existing clamp (`watchClamp(selected+delta, -1,
    len-1)`) pins `-1 + -1` straight back to `-1`. `Down` from the same
    state correctly landed on the list's head (`0`) only because `-1 + 1`
    happens to already be a valid, in-range index. Fixed with one special
    case: `Up` from "nothing selected" jumps straight to the last entry
    instead of going through the clamp - which, with no entries at all,
    still correctly stays a no-op (`len(w.entries)-1` is `-1` exactly
    when there's nothing to select, so no extra guard was needed).
    Elsewhere the existing "no wraparound" clamp is unchanged.
  - `Delete` was the only key that removed a selected watched row;
    `Backspace` did nothing. Added `tcell.KeyBackspace`/
    `tcell.KeyBackspace2` alongside `tcell.KeyDelete` in that one branch -
    but only when a row is actually selected; with nothing selected,
    `Backspace`/`Backspace2` still forward to the input field as before
    (ordinary text editing), while a bare `Delete` keeps being a no-op
    there, unchanged.

**Verified live in tmux**: `<undefined>` with no strike-through escape
code (confirmed via `capture-pane -e -p`, which needs the `-p` flag to
actually emit color codes with a `-S`/`-E` range - a tmux quirk hit
again this round, not new to this codebase); the panel's own background
showing a non-default color (confirmed in one-pane tree, one-pane
drill-down, and two-pane split - split mode's left/tree pane keeps its
own unrelated `SplitBottomBarText` untouched, only the right pane's
`outputBottomBar` carries the hint); the bottom bar swapping to the
watch hint on open and reverting to the session's normal hint (including
the debug F5/F9/F10/F11 one) on close; `Up` from no selection jumping to
the last entry, `Down` from no selection still landing on the first;
both `Delete` and `Backspace` removing a selected row. `go build ./... &&
go vet ./... && go test ./...` and `gofmt -l internal/` clean (aside
from the one pre-existing, unrelated `recap_rowtext_test.go` flag).
`README.md`, `design-docs/Keyboard-shortcuts.md`, and the man page
updated to match.

## Stage 4 follow-up round 3: chrome color on the headline only (2026-10-20)

Round 2's "paint the whole panel with the chrome background" was tried
live and only half-liked: reverted for the entries and the input field
(back to their normal colors - `uikit.StandardTextColor`/
`NothingReportedColor` text on the default background, and
`uikit.StyleInputField`'s own control colors, exactly as before round
2), kept for just the `"Watched variables"` headline.

`watchPanel.render` no longer calls `SetBackgroundColor`/
`SetFieldBackgroundColor` on `w.list`/`w.input` at all - instead the
headline line itself carries an inline `[white:<name>:b]...[-:-:-]` tag,
`<name>` being a tag-compatible color *name* string, not a `tcell.Color`:
the call site (`rebuild()`) now passes `s.chromeColorName()` (already
existed, used elsewhere for the top/split bars' own progress-fill tags)
instead of `s.debugChromeBg()`. Foreground is a literal `"white"`, not
`uikit.StandardTextColor` - matching `buildChromeStyle`'s own existing
convention for every other chrome-colored bar in this codebase
(`BarStyle`/`DebugRunningBarStyle`/...): these background colors are
fixed and reliably dark/saturated, but a theme's own `standard_text`
isn't guaranteed to contrast against them (a light theme's dark
`standard_text` on a navy background, say), so a fixed `"white"` is the
one safe choice, the same reasoning that's kept those other bars off
`standard_text` all along.

**Verified live in tmux**, paused mid-debug (maroon chrome) in two-pane
split: the headline shows bold white text with a maroon background
(confirmed via a clean single-line `capture-pane -e -p -S n -E n`, not
the whole-screen form - `-e`'s own cross-row attribute carryover makes a
multi-line capture easy to misread, as `design-docs/TESTING.md` already
warns); the watched entry row right below it renders on the normal
(black/default) background, not maroon; the input field's own background
is the normal control background, not maroon either; closing the
explorer still leaves the headline chrome-colored while the (now grey)
entries keep their own normal background. `go build ./... && go vet
./... && go test ./...` clean; `gofmt -l internal/` shows only the one
pre-existing, unrelated `recap_rowtext_test.go` flag.

## Stage 4 follow-up round 4: always show the heading, fill the whole line (2026-10-20)

Two fixes, plus a likely-not-a-bug clarified while investigating a third
report:

- **Heading now shown the whole time the explorer is open**, not just
  once at least one variable is watched. Reported live as "I press `v`
  and see the text field but not 'Watched variables'" - correct: before
  this round, `watchPanel.render`/`height` both gated the heading on
  `len(w.entries) > 0`, so a freshly opened explorer with nothing
  watched yet genuinely had no heading to show. Both now key off `w.open
  || len(w.entries) > 0` instead, so the heading acts as a permanent
  panel title for as long as the explorer is open, heading or no
  watches.
- **Chrome color now fills the whole headline row**, not just the
  literal `"Watched variables"` text - round 3's own tag only colors the
  runes actually present in the string, the same reason
  `ComposeTopBarLine`/`s.outputTopBar`'s own plain text get padded to the
  full terminal width before their own fill tags are applied
  (`livesession_rebuild.go`). `render` now takes the panel's own current
  width too (`rebuild()`'s new `watchWidth` - `totalWidth` itself in the
  tree-only/output-single-pane cases, or the split output pane's own
  narrower share, the same arithmetic `s.splitBody`'s own `AddItem` calls
  already use) and pads the heading out to it before wrapping the tag
  around it. Confirmed correct via a throwaway unit test rather than
  `tmux capture-pane` this round - capture-pane trims trailing whitespace
  off every captured line regardless of its own color/attributes, so a
  padded-but-still-blank tail is invisible to that tool no matter how
  it's invoked; `watchPanel.render`'s own output string
  (`w.list.GetText(false)`) was checked directly instead, confirming the
  padding spaces land *inside* the `[white:<name>:b]...[-:-:-]` tag span
  as intended.
- **"No autocomplete popup appears anymore" - investigated, likely not a
  regression.** Reproduced extensively live (both one-at-a-time and
  batched `tmux send-keys`, paused mid-task with `--all-vars`, idle
  before any run, and a plain `F5` straight through with no breakpoint
  armed and no `--all-vars`) and the autocomplete drop-down worked
  correctly in every case where a captured snapshot actually existed for
  the current host/task context - confirmed via `watchCandidates`'s own
  source, unchanged across every round so far. It showed nothing only
  when there was genuinely no snapshot to suggest from yet: before any
  run has started, or after a plain run finished with no breakpoint ever
  armed and no `--all-vars` - `testdata/outcomes.yml`'s own one failing
  task is `ignore_errors`-wrapped, so it doesn't auto-surface a pause
  either (`uikit.GenuineFailure`'s own predicate), meaning a bare `F5`
  with nothing else set up captures nothing at all, anywhere, by design.
  This matches "by default, variables are only captured at a pause
  actually shown to you" (the man page's own `--all-vars` paragraph) -
  not confirmed as an actual bug; flagged back to the user rather than
  guess-fixed further.

**Verified**: `go build ./... && go vet ./... && go test ./...` and
`gofmt -l internal/` clean (aside from the one pre-existing, unrelated
`recap_rowtext_test.go` flag); live in tmux, `v` on a totally fresh
session immediately shows the heading above the (empty) input field.

## Stage 4 follow-up round 5: the "clipped" autocomplete drop-down - root cause found

The round 4 autocomplete investigation above concluded "likely not a
regression" based on testing with few enough candidates that the
drop-down never needed more room than the panel itself had. Revisited
after a better repro: open the explorer in the drill-down/split page
(not the plain tree page) and type a prefix matching *several*
candidates (`--all-vars`, paused, prefix `a` matches all 7 `ansible_*`
vars) - only the drop-down's *last* row (right above the input field)
ever rendered; every row above it stayed blank, and navigating up into
them made the cursor vanish while the entry remained selectable (Enter
still added it) - exactly the user's own description, down to "the
upper end is somehow clipped."

Root-caused by temporarily vendoring a locally-patched copy of
`rivo/tview` (via a throwaway `go.mod` replace directive, reverted
before finishing - never part of any commit) and instrumenting
`InputField.Draw`/`List.Draw`/`Flex.Draw` directly: the drop-down's own
`screen.SetContent` calls were confirmed correct immediately after
drawing (read back via `screen.GetContent`), so the content was right
the instant it was written - something else overwrote it afterward,
every time, only when `s.outputTabs` (the drill-down's `TabbedPane`) sat
above the watch panel in the same `Flex`. The `Flex.Draw` trace named
it directly: `*tview.Pages` (the tabs' own internal page switcher)
logged as `DEFERRED (hasFocus)` right alongside the input field - two
siblings both reporting `HasFocus() == true` at once, when only the
watch input should.

This is `uikit.TabbedPane`'s own already-documented "stuck focus" bug
(`Clear`'s doc comment, `design-docs/ErrorOutput.md`): once `p.pages`
has ever genuinely held focus (e.g. the user pressed Tab once), it
reports `HasFocus() == true` forever after, regardless of where
`Application.SetFocus` actually points - previously only ever fixed at
the one call site that mattered before this feature existed
(`closeOutput`'s own `s.outputTabs.Clear()`). Opening the watch explorer
is a *second* way focus moves away from the tabs that nothing cleared
it for. With two "focused" siblings in the same `Flex`, `Flex.Draw`'s
"draw whichever child reports focus *last*" rule (so the real focused
item always paints over its siblings) applies to *both* - and since Go
unwinds `defer` LIFO, the earlier-registered one (the tabs, always
listed before the watch panel in every parent `Flex`) ends up drawn
dead last, painting over the drop-down wherever the two overlap. The
drop-down's own geometry (`InputField.Draw`'s "drop up when there's no
room below," confirmed necessary here since the panel always sits
directly above the one-row bottom bar) made that overlap large -
everywhere the tabs' own rect extended, which is most of it; only the
row the tabs' rect doesn't reach (immediately above the input field)
survived.

Fix: new `uikit.TabbedPane.Blur()` - the same two calls `Clear` already
makes (`p.pages.Blur()`, `p.root.Blur()`) factored out as their own
method, for a caller that wants to clear the stuck flag *without*
tearing down the tabs' own content (`Clear`'s contract). Called from
`handleWatchKey` right alongside the existing `s.app.SetFocus(s.watch.input)`
when the explorer opens.

**Verified**: 6/6 and 4/4 clean runs (one-pane drill-down and two-pane
split respectively) with a 7-candidate prefix that reproduced the bug
on every single attempt before the fix (previously confirmed flaky-but-
frequent on both the pre- and post-round-4 code, not something any
earlier round introduced); `Tab` still switches the drill-down's own
active tab correctly after opening and closing the watch explorer once
(confirming `Blur()` doesn't regress ordinary tab switching). `go build
./... && go vet ./... && go test ./...` and `gofmt -l internal/` clean
(aside from the one pre-existing, unrelated `recap_rowtext_test.go`
flag).

## Status

**Design decided 2026-09-23, nothing implemented yet.** This doc replaces
the original file-mutation proposal (kept below as "Original proposal
(superseded)" for the reasoning trail) with a strategy-plugin design worked
out across discussion on 2026-09-22/23. The core technical premise (that a
custom strategy plugin, not a callback, is the only layer with access to
per-host variable state) is sound, but the exact hook point
(`_process_pending_results`) is **not yet spiked live** - see "Biggest open
risk" below. Depends on `design-docs/OwnCallbackPlugin.md`'s fd-transport
plugin, which is merged and live (`callback/tangsible_jsonl.py`).

**2026-09-25 spike addendum: hook point confirmed, but a new foundation-
level risk surfaced. Paused here, not proceeding for now** - see "Live
spike findings (2026-09-25)" below, right after "Proposed design." The
short version: the `_process_pending_results` hook works exactly as
hypothesized, but this ansible-core version (2.19.11) actively deprecates
third-party strategy plugins entirely, with no replacement mechanism
announced. That's a bigger deal than any of the implementation details
below and is why work stopped here rather than continuing into the actual
plugin build.

**2026-09-25 addendum:** "Phase 2" below sketches an interactive
breakpoint/single-step debugger on the same strategy-plugin foundation -
conceptual only, confirmed feasible against ansible-core's own source, not
designed in detail. Everything above this addendum is Phase 1 and unchanged
by it.

**2026-09-27 pivot spike: a different foundation found, sidesteps the
deprecation risk entirely.** Rather than accepting the third-party
strategy-plugin deprecation risk the 2026-09-25 spike paused on, a live
spike explored using ansible-core's own built-in task debugger
(`debugger:` keyword / `ENABLE_TASK_DEBUGGER`) as the foundation instead of
a forked strategy plugin - see "Pivot spike (2026-09-27)" below, placed
right after "Live spike findings (2026-09-25)". Confirmed live: no
deprecation warning at all (it's first-party, maintained infrastructure,
not the thing being sunset), and it works under `strategy: free` too,
which the original design's own known gap couldn't claim. The interface
would be a plain-text `cmd.Cmd` REPL over the child's stdin/stdout rather
than a structured JSON event, and a real limitation surfaced (`task_vars`
in the debug scope is a pre-task snapshot, not refreshed with the halted
task's own `set_fact`/`register` output) - see that section for the full
findings. Not yet decided whether this replaces or complements Phase 1/2
above; details deferred to further discussion.

**2026-09-27 workflow discussion, on the pivot-spike foundation above:**
worked out a concrete two-entry-point breakpoint workflow (picker before a
fresh run; a "Debug" option in the rerun dialog) - see "Workflow design
(2026-09-27)" below, right after the pivot spike. Settled: never mutate
the user's own tracked file, only an ephemeral copy, mirroring
`WriteRoleStub`/`TrimPlaybookToPlay`'s existing pattern rather than the
backup-then-revert shape first proposed; v1 scope is breakpoints on tasks
defined directly in the top-level playbook only, the same cut `Start with
a play` already accepted for roles/includes. New wrinkle surfaced: driving
the debugger's REPL means Tangsible has to own the child's real stdout,
which today is deliberately left free for the user's own configured
stdout callback - needs a resolution before implementation, sketched
below.

**2026-09-27 addendum: the `$ANSIBLE_ROLES_PATH`-shadow-copy idea (for
reaching into a role's own task file without touching it) was spiked live
and dropped.** `<playbook_dir>/roles/<name>` is checked before
`ANSIBLE_ROLES_PATH` unconditionally, hardcoded in ansible-core's own role
resolution - confirmed both by reading `ansible/playbook/role/
definition.py` and by a live run, so a project's own local role always
wins regardless of what's prepended to the search path. The technique only
helps for an out-of-tree role with no local copy of the same name, not the
common in-project case - see the v2 idea write-up under "Workflow design
(2026-09-27)" below for the full findings. Tasks inside roles/includes
stay a dropped, documented gap for the foreseeable future, not just v1.

**2026-09-27 addendum, later the same day: a different idea supersedes the
one just dropped, for local roles specifically.** Rather than shadowing
one role via a search-path trick, duplicate the whole directory tree the
playbook depends on (playbook + roles + everything reachable from it) and
mutate freely inside that copy, never touching the original tree at all.
Spiked live and confirmed working end-to-end, including running a real
`ansible-playbook` invocation against the copy - see "Superseding idea
(2026-09-27): full directory-tree hardlink shadow copy" under "Workflow
design (2026-09-27)" below. This reopens local roles as in-scope: the
"top-level playbook tasks only" restriction now only needs to apply to
roles genuinely outside the playbook's own directory tree, which is the
narrower case the just-dropped `$ANSIBLE_ROLES_PATH` idea still covers.

**2026-10-14: full interactive-debugger UX vision laid out, and the
dual-channel architecture it depends on spiked live.** The user's actual
vision goes well beyond the raw-terminal-handoff MVP proposed as a first
cut: a dedicated `debug` entry point showing the whole task tree up front,
breakpoints set directly on it, the usual debugger controls (Step, Step
Into, Step Out, Run), the tree highlighting the current pause position
live, and the ability to query (and ideally modify) variables - all on the
*same* tree the live run already uses, no separate raw-terminal fallback.
See "Full interactive-debugger vision and dual-channel spike (2026-10-14)"
below for the full discussion and the two things spiked: concurrent use of
the real jsonl event-fd pipe and the debugger's REPL at the same time (both
channels coexist correctly, and a debug pause genuinely blocks the whole
child process, so the event stream and the debugger naturally stay in
lockstep for free), and a JSON-dump query trick that avoids parsing the
debugger's native Python-repr output entirely. Decided out of scope for
v1: Step Out (no clean notion of "where does this include end" under
dynamic includes) and variable *modification* persisting beyond an
immediate same-task redo (structurally unconfirmed, and not a stated v1
requirement - viewing variables is). Decided in scope, accepted tradeoff:
splicing `debugger: always` onto *every* task (not just chosen
breakpoints), trading a per-task round-trip cost for getting Step/Run
semantics and live breakpoint toggling for free, with no re-editing of the
copy needed mid-run.

**2026-10-14, later the same day: M0 (the shadow-copy primitive) is
implemented.** Headless, no verb/TUI wiring - `internal/debugcopy`
(`shadowcopy.go`'s `BuildShadowCopy`, `splice.go`'s
`SpliceDebuggerAlways`), plus `internal/source`'s new `TaskLocation`/
`ListTaskLocations` (reusing the existing YAML walk rather than
duplicating it; `BuildTaskSourceIndex`'s own signature/behavior
unchanged). Implements exactly the mechanics confirmed live across the
spikes above: hardlink-based copy rooted under `.tangsible/debug/<RunID>/`,
excluding `.tangsible`/`.git`; splice-by-unlink-then-write; bottom-to-top
multi-location insertion per file so one insertion's line shift never
invalidates another not-yet-processed location above it; block/rescue
wrappers and flow-style tasks excluded from splice targets. 11 new unit
tests (hardlink verification, exclusion, cleanup safety, indent
correctness, multi-line folded-scalar safety, multi-task-per-file
ordering, a round-trip re-index of the spliced copy to confirm it's still
valid YAML) - all passing, `go vet`/`go test ./...` clean across the whole
repo. Not yet built: any verb/flag entry point, the breakpoint picker UI,
or the dual-channel driving logic itself (spiked standalone in Python, not
yet ported to Go) - M0 is the primitive two later milestones build on, not
a usable feature by itself.

**2026-10-14, later still: M1 (the live process driver) is implemented,
and ports the dual-channel architecture from Python to Go successfully -
with one assumption corrected along the way.** `internal/debugrun`'s
`Session`/`Spawn` runs a real `ansible-playbook` invocation against an
`internal/debugcopy` shadow copy, wiring the bundled callback plugin onto
one pipe (`Events`, reusing `runner.ScanEvents` unmodified) while driving
ansible-core's own built-in task debugger synchronously over a second,
independent pipe pair (`Start`/`Continue`/`Query`).

**Correction to the 2026-10-14 vision spike above: the REPL doesn't need a
pty after all.** Every earlier spike (Python, both the solo debugger spike
and the dual-channel one) drove the debugger over a pty, mostly out of
habit. `Session` uses plain `os.Pipe()`s instead - matching this codebase's
own `runner.SpawnGeneration`, needing no pty library, and avoiding the
pty's own local-echo noise that the Python spikes had to filter back out
of every response. Confirmed live via a real integration test
(`session_test.go`'s `TestSession_LiveDualChannel`, skipped if
`ansible-playbook` isn't on `PATH`, same pattern `internal/vaultcrypto`'s
own real-binary tests already use): a 3-task, 2-host fixture, spliced via
`debugcopy.SpliceDebuggerAlways`, driven through all 6 pauses (3 tasks x 2
hosts, `--forks 1`), with a `Query` at one pause reconfirming the
pre-task-snapshot limitation yet again - now through the Go driver itself,
not just the Python spikes. `go vet`/`go test ./...` clean across the
whole repo.

Still not built: any verb/flag entry point, the breakpoint picker UI, the
client-side policy that decides which pauses are real breakpoints versus
silently auto-continued, or the "paused" TUI chrome - `Session` only
exposes the raw primitive (every task pauses, `Continue` advances exactly
one), matching the uniform-splice decision; the policy of auto-continuing
past non-breakpoints is deliberately left to whatever calls this package.

**2026-10-15: M2 (the breakpoint policy) is implemented, still headless.**
`internal/debugrun/breakpoints.go` adds exactly the piece flagged as
missing above: `BreakpointSet` (a mutex-guarded set of armed task labels,
`Pause.Task` verbatim - safe for live toggling from another goroutine
while a session is blocked mid-run) and a `StepMode` (`RunTillBreakpoint`
vs `StepOne`) consumed by two functions, `RunWithBreakpoints`/
`ContinueWithBreakpoints`, that wrap `Session.Start`/`Continue` with the
"Step / Run can be the same mechanism, decided client-side" policy from
the 2026-10-14 vision section above: under `RunTillBreakpoint`, any pause
whose task isn't armed is silently resumed past (`Session.Continue`) until
an armed one or eof; under `StepOne` the very next pause is always
surfaced, armed or not. An empty `BreakpointSet` under `RunTillBreakpoint`
runs the whole session to completion in one call - incidentally the same
outcome as answering `c` to ansible-playbook's own `--step` prompt (see
that discussion below), but falling out of the same mechanism as every
other mode rather than needing a separate flag that, unlike this, can't be
re-armed later in the run.

Confirmed live (`breakpoints_test.go`, same 3-task/2-host fixture and
skip-if-`ansible-playbook`-missing pattern as `session_test.go`): an empty
set runs to completion; arming `"TASK: task two"` surfaces exactly that
task's pause for *each* host in turn (breakpoints are per task label, not
per host - both hosts' pauses on the armed task surface, confirming the
doc's own "per-host clarity while paused" open item is still real, just
not solved here) and auto-continues through the unarmed task either side
of it; `StepOne` surfaces all 6 pauses regardless of an empty set,
matching driving `Session` with no policy layer at all. `go vet`/
`go test ./...` clean across the whole repo.

Still not built: any verb/flag entry point, the breakpoint picker UI, or
the "paused" TUI chrome itself - M2 is the last headless primitive on the
path to M3 (the actual TUI integration: entry point, pre-run static tree,
live highlighting, variable view), which remains undesigned beyond the
vision and open items already captured above.

**2026-10-15: side discussion - could `ansible-playbook --step` do any of
this instead of the uniform `debugger: always` splice?** Raised, spiked by
reading source (not re-spiked live - no behavior claim here needed a
live run to settle). `_take_step` (`ansible/plugins/strategy/__init__.py`,
~lines 876-897) is a separate, pre-dispatch `display.prompt()` asking
"(N)o/(y)es/(c)ontinue", confirmed wired into both `linear.py` and
`free.py` (so it's not a strategy-specific limitation) - but answering
`c` sets `self._step = False` **permanently**, checked at every call site
with no code path that ever sets it back to `True`. That means `--step`
can only express "step through literally everything from here" or "give
up stepping for the rest of the run entirely" - never "run free past
these tasks but still stop at a later one," which is the actual
breakpoint use case `BreakpointSet` exists for. It also carries no
variable-inspection capability of its own (a bare `y`/`n`/`c` prompt, no
`cmd.Cmd` eval scope) - that still only comes from the `debugger:`
keyword. Conclusion: not a viable alternative to the uniform splice for
re-armable breakpoints, though its one-way "run to completion" shape is
worth noting as something `BreakpointSet` already gives us for free (an
empty set under `RunTillBreakpoint`), with no separate flag needed.

**2026-10-15: M3 (the "debug" verb and its own TUI) is implemented - a
first slice, confirmed live via tmux, not the full end-state vision.**
Before writing any TUI code, read `internal/session/tui.go`'s `NewLiveTUI`
in full (941 lines, 30+ constructor parameters, cross-cutting rerun/
revisit/two-pane/filter/search/recap machinery none of which applies to a
debug session) and checked with the user whether M3 should graft onto
that struct or build something separate - answer: **a separate, smaller
construct**, matching every other standalone verb's own "one package per
verb" pattern (`internal/role`/`internal/template`/`internal/host`), not
`internal/session`'s own highest-invariant file. `internal/debugverb` is
the result: `debugverb.go` (`RunDebugVerb` - arg parsing, shadow copy +
splice, `--step` rejection - see below) and `tui.go` (`debugApp` - the
tree, key handling, the background-goroutine-drives-Session/
QueueUpdateDraw-reports-back pattern `internal/host`'s own tab fetches
already use).

**What's reused, per the user's own standing instruction ("certainly use
the same mechanisms as with the live run, otherwise implementations will
drift apart"):** the same jsonl event stream/decoding
(`runner.ScanEvents`, `playbook.RawEvent`, `playbook.DecodeHostResult` -
read `aggregate.go`'s own `Apply` first to confirm the exact event-name →
`Outcome` mapping and copy it rather than guess), the same outcome/color
vocabulary (`playbook.Outcome`, `uikit.ColorTag`), and - the one genuinely
reusable piece that wasn't obvious up front - the live run's own progress-
indicator machinery (`runner.BuildProgressSkeleton`/`ParseListTasksOutput`/
`ProgressEntry`/`ProgressTracker`, `progress.go`). That machinery already
solves two problems this verb also has: predicting a playbook's full task
list ahead of time from a throwaway `--list-tasks --list-hosts` probe (the
pre-run tree), and resolving a live event back to a specific position
despite task names repeating across a real playbook (`ProgressTracker`'s
own bounded forward-matching). Repurposed here for a different end (a
position *index*, not a percentage) but the identical underlying problem,
inheriting its own documented approximations too (a handler, or a
dynamically-resolved `include_tasks:`, won't appear in the predicted tree
even though it genuinely runs).

**One confirmed subtlety, caught by reading `aggregate.go`'s
`findOrCreateTask` first rather than assuming:** a task-start event's own
`ev.Play` field is not reliable for "which play is this" - only a
`v2_playbook_on_play_start` event's `ev.Play.Name` is (the real app tracks
its own `currentPlay`/`pendingPlayName` across task-start events for
exactly this reason). `debugApp.applyEvent` mirrors that: `currentPlayName`
is updated only on play-start, carried forward across task-start events,
and that's what's fed to `ProgressTracker.Advance`, not the task-start
event's own (unreliable) `Play` field.

**Pause-to-row correlation falls out for free from the confirmed dual-
channel lockstep ordering (the 2026-10-14 spike above):** `Session.Pause`
carries only `{Host, Task}`, no play name and no source location - but
since a task's own jsonl event is confirmed to always arrive at-or-before
that same task's own debug pause, `applyEvent`'s own `currentRowIdx` (set
by the preceding task-start event) is already correct by the time the
matching `Pause` arrives. No second tracker or event-to-pause
synchronization was needed.

**`--step` is rejected outright, not silently forwarded** (`debugverb.go`):
confirmed by re-reading the 2026-10-15 `--step` discussion above that
letting it through would mean `Session`'s own `readUntilPrompt` blocks
forever on a prompt shape (`--step`'s own "(N)o/(y)es/(c)ontinue") it
doesn't recognize at all - a hang, not a clean error. Caught during
completions/man-page housekeeping, not while writing `internal/debugrun`
itself - worth noting as a reminder that a "reasonable default" (forward
every ansible-playbook flag through) needs re-checking against each new
caller's own actual constraints, not just carried over by convention.

**Two real bugs, both caught live in tmux, neither from reading the code
alone:**
1. `uikit.TreeList` has no built-in per-row highlight at all (confirmed
   against `tui.go`'s own doc comment, then confirmed live: the very
   first screen showed no cursor anywhere) - `NewLiveTUI`'s own
   `rebuild()` re-renders the selected row with its own style variant
   before every `AddItem` call instead, "the entire highlighting
   mechanism"; `debugApp.rowText`/`rebuildList` now do the same, and
   `d.list.SetChangedFunc` was missing entirely at first (without it,
   moving the cursor updated `TreeList`'s own `currentItem` but nothing
   ever re-rendered to reflect it).
2. `'b'` (the first choice for "toggle breakpoint," debugger muscle
   memory) silently collided with `uikit.TranslateNavAlias`'s own shared
   Page-Up alias (`j`/`k`, Space/`b`, Ctrl-F/B, Ctrl-A/E, `<`/`>`/`G` -
   reserved app-wide) - confirmed live (the key moved the cursor to the
   top instead of arming anything). Renamed to `t`.
3. Ctrl-C/q while genuinely *paused* (no `Start`/`Continue`/`Query` call
   in flight) killed the child via SIGINT, but nothing was blocked
   reading either of `Session`'s own pipes at that moment to notice -
   the bar kept showing a stale "paused" prompt, and a bare q/Ctrl-C
   alone could never actually quit until some other key happened to
   issue a doomed `Continue`/`Query` first and discover the eof.
   Confirmed live (`pgrep` found no `ansible-playbook` process left, yet
   the TUI still showed "paused"). Fixed by also treating the event pipe
   closing (`drainEvents`'s own `for range sess.Events` loop ending) as
   an independent "the child is gone" signal - it closes once the
   child's own fd 3 closes with the process, regardless of whether the
   REPL side is being read.

**Confirmed live, full flow, real `ansible-playbook`, tmux:** pre-run tree
shown immediately; armed a breakpoint on one task, pressed Enter - ran
straight through the earlier, unarmed task (auto-continuing silently,
confirmed by its outcome dots already showing before the first pause ever
appeared) and paused at the armed task for the first host; continued -
paused again at the *same* armed task for the second host (breakpoints
are per task label, not per host - the doc's own "per-host clarity while
paused" open item is still real, unresolved, just directly visible now
rather than theoretical); pressed `v` - real `task_vars` rendered as
indented JSON; closed it, continued to completion - a `when:`-skipped and
an `ignore_errors`-failed outcome both rendered with their own correct
colors on the last task; stepped through from a fresh run one pause at a
time regardless of breakpoints; confirmed the shadow copy directory is
fully removed after a clean exit.

Added 4 unit tests for the non-tview-dependent logic
(`outcomeFor`/`taskLabel`/`buildRows`), per CLAUDE.md's "pure functions
first" testing philosophy - everything else here needs tmux, consistent
with the rest of `internal/session`. `go vet`/`go test ./...` clean across
the whole repo. New verb wired into `config.ParseVerb`/`session/main.go`
(an early standalone-verb branch, alongside template/host/vault - never
touches the run/rerun/role switch at all) plus the full housekeeping pass
CLAUDE.md's own convention calls for: `completions/` (bash/fish, including
excluding `--step` specifically for `debug` while every other
ansible-playbook flag is still offered), `man/tangsible-debug.1.scd` (new)
plus `man/tangsible.1.scd`'s own VERBS/SEE ALSO lists, `README.md`
(a new section, a shortcuts table, and a correction to the old blanket
"neither will the ansible debugger" limitations line - still true for
`run`/`rerun`/`role`, no longer true for Tangsible as a whole),
`design-docs/Keyboard-shortcuts.md`, `design-docs/Codemap.md`, and
`CLAUDE.md` itself (verb count, Packages, Features, Scope).

Still not built, and explicitly out of scope for this slice: Step Out
(unchanged - no real call stack to step out of), variable modification,
a searchable/collapsible variable view (today: one flat `task_vars` dump -
"variable volume" from the 2026-10-14 open items is still real, just not
addressed), and per-host clarity while paused beyond what's directly
visible in the tree (see above).

**2026-10-15, later still: the user tried M3 live and flagged real
rendering drift from the main tree** - confirmed, root-caused, and fixed.
Reported: per-host status shown as bare dots rather than hostnames; the
task-line color looked different from the run tree's; no busy/running
indicator; the cursor could rest on a play-header row (impossible in the
main tree); and a selected row's gray background wrongly covered the
prefix column too. All five traced to one root cause: `debugverb/tui.go`
re-derived its own smaller version of row rendering instead of calling
`internal/uikit`'s existing, verb-agnostic rendering helpers
(`TaskLabel`/`PlayRowText`/`HostColorTag`/`ComputeHostColumnLayout`/
`NextInteractiveRow`) - the exact functions `internal/session`'s own
`rebuild()` calls, put in `uikit` specifically so a standalone verb
wouldn't have to reinvent this. Raised with the user as a real
architectural choice (not something to silently redo unilaterally, the
same "discuss before building" norm M3's own construct decision got):
reuse just the rendering helpers (keeping `debugverb`'s own simpler
event-tracking as is), or go further and back the tree with a real
`playbook.PlaybookState` fed by its own `Apply`. User chose the narrower
option - lower risk to a feature just confirmed working well live.

**What changed:** `debugRow` now wraps real `*playbook.TaskNode`/
`*playbook.PlayNode` values (populated directly from the skeleton in
`buildRows`, not via `Apply` - the skeleton's own shape is static, only a
`TaskNode`'s own `Hosts` map mutates later) plus a `*playbook.
PlaybookState` (`d.pbState`) purely so `ComputeHostColumnLayout` can
measure the shared host column the same way the main tree does, and a
run-wide, sorted `d.allHosts` (grown via `recordHost`, mirroring
`PlaybookState.AllHosts`' own "only ever grows" contract). `rowText` now
calls `uikit.TaskLabel`/`PlayRowText` directly rather than composing its
own strings - which, as a direct consequence of reusing real code, also
fixed the color and selected-row-prefix issues for free, and surfaced the
selected row's per-host halfBlock color transition (confirmed live) that
the old bare-dot rendering never had at all. Debug mode's own two
concepts with no TaskLabel equivalent - the breakpoint marker and the
"(paused: host)" annotation - are composed as their own segments outside
whatever `TaskLabel` returns, so neither inherits the selected-row
background either (the marker's own exact placement - inside `TaskLabel`'s
own prefix slot, not a second slot of its own next to it - was revisited
twice more after this round; see the "third round of incidental findings"
entry below). The busy indicator needed
one new piece with no prior equivalent in this view: a permanent
heartbeat ticker (`startHeartbeat`, mirroring `internal/session`'s own
but simplified - no elapsed clock to freeze, so it never stops, which is
harmless since redrawing a frozen finished screen every tick costs
nothing visible) driving `uikit.SpinnerAt` the same way `rebuild()` does,
guarded by a `quitting` atomic bool before every `QueueUpdateDraw` call -
same CLAUDE.md invariant `internal/session`'s own heartbeat already
documents (nothing drains the update queue once `app.Run()` returns).
Cursor-skips-headers reuses `uikit.NextInteractiveRow` directly against a
parallel `d.navRows` slice (`Selected` nil on a header row), intercepted
in `handleKey` before `uikit.TranslateNavAlias` - the exact same
ordering `internal/session`'s own `handleTreeRowSkip` uses and for the
same reason (`j`/`k` must get this before being translated to plain
Up/Down). The same fix had to extend to the very first frame too - no
keypress has happened yet "before" the state the normal header-skip
guards, so the first-populate branch (`rebuildList`'s own `selIdx < 0`
case) was defaulting to literal row 0, which **is** a play header
whenever a playbook's first play has any tasks at all - confirmed live
(the very first screen opened with the cursor on the play header) and
fixed the same way, via `NextInteractiveRow` from `-1`.

Confirmed live via tmux (`outcomes.yml`, single host, all four rollup
buckets): hostnames now render instead of dots, correctly colored; the
busy spinner visibly animates on the currently-executing task before its
own debug pause; the cursor cannot land on, or be moved onto, the play
header; a selected row's prefix (breakpoint marker, or the busy spinner)
stays outside the gray background, only the title-through-hosts span
does. Also confirmed against a genuine two-host run
(`multihost-inventory.ini`): per-task-label breakpoint semantics (a
breakpoint on a task still surfaces that task's pause for *each* host in
turn, "per-host clarity while paused" still open per above) and the
selected row's real halfBlock per-host color transition, both exactly as
the main tree's own `TaskLabel` already renders them, for free.

**Incidental finding from the round above, fixed in a follow-up pass:** a
relative path argument (e.g. `-i testdata/multihost-inventory.ini`)
passed through to `ansible-playbook` resolved against the *shadow copy's*
cwd, not the original invocation's - for a playbook whose own directory is
what gets copied (the common case), a relative inventory path with a
leading path segment matching that directory's own name silently resolved
to a nonexistent nested path, Ansible fell back to its own default
inventory, `hosts: all` matched nothing, and the run "finished" instantly
with no tasks ever run and no debugger pauses ever firing - looked exactly
like a broken breakpoint/pause mechanism from the TUI's point of view, but
wasn't one. Confirmed reproducible against the pre-existing, already-
committed M3 code too (not introduced by the rendering-parity change
above) by rebuilding the prior commit and reproducing it there first.

Root cause: `debugrun.Spawn` set `cmd.Dir` to the shadow copy so it could
pass the playbook as a path relative to that copy - but that meant every
*other* relative passthrough path resolved against the wrong directory
too, unlike a plain `run` (`runner.SpawnGeneration` never sets `cmd.Dir` at
all, so it inherits the real invocation cwd). Fixed by having
`debugverb.RunDebugVerb` pass `Spawn` the copy-playbook's own *absolute*
path (`copyPlaybookPath`, already computed there for the `--list-tasks`
probe) instead of a copy-relative one, and dropping `cmd.Dir` entirely -
ansible-core itself always resolves role/include paths against the
playbook's own directory, never cwd, so an absolute playbook path needs no
cwd help, while every other passthrough arg now resolves exactly like a
plain run. Guarded by a new live (real `ansible-playbook`) test,
`TestSession_RelativePassthroughPathResolvesAgainstCallersCwd`, which
`os.Chdir`s into a directory holding nothing but a relative inventory file
never placed under the shadow copy, so only correct cwd-resolution can
make it pass. Confirmed live via tmux too, with the exact repro above
(`-i testdata/multihost-inventory.ini`, relative, from the repo root):
both hosts now show real outcomes instead of the run finishing instantly
with none.

Added `TestBuildRows_InsertsOneHeaderPerPlayChange`'s own navRows/pbState
assertions and a new `TestRecordHost_SortedDedup`, alongside updating the
existing tests for the new `debugRow` shape. `go vet`/`go test ./...`
clean across the whole repo.

**Second incidental finding, also fixed:** a debug session killed before
its own cleanup ran (crash, `kill -9`, a tmux session torn down instead of
quitting via `q`) leaves a stale `.tangsible/debug/<runID>/` shadow copy
behind under the playbook's own directory - itself a full copy of the
playbook's directory tree with `debugger: always` already spliced onto
every task. `internal/source.ListTaskLocations`'s own directory walk
didn't exclude `.tangsible` (only `.git`), unlike `debugcopy.
BuildShadowCopy`'s own walk, which does - so a later `debug` invocation's
locations included paths under that *old* copy, which the *new* shadow
copy never receives (`.tangsible` is excluded when building it), and
`SpliceDebuggerAlways` then failed outright with a plain "no such file or
directory", with nothing in that error pointing back at the real, stale
cause. Reproduced live via tmux: started a debug session, killed the tmux
session instead of quitting, confirmed the leftover `.tangsible/debug/
<timestamp>/` directory existed, then confirmed a second `debug`
invocation failed with exactly that error. Fixed by adding the same
`.tangsible` exclusion to both of `internal/source`'s own directory walks
(`ListTaskLocations` and `BuildTaskSourceIndex` - the latter has the
same latent gap for a plain `run`'s own File/Resolved-tab source index,
even though it's not what surfaced the bug here). Guarded by a new test,
`TestListTaskLocations_SkipsLeftoverTangsibleDir` (confirmed to fail
without the fix, by temporarily reverting it and re-running), and
reconfirmed live afterward: the same kill-tmux-then-restart repro now
starts cleanly on the second invocation.

**Third round of incidental findings, also fixed:** after the rendering-
parity pass above, a live side-by-side comparison against `run` turned up
five more real divergences, all fixed in the same pass:

1. **Busy-indicator column, and (found on a second look, after the first
   fix) task indent width.** The breakpoint-armed marker (`●`) was
   concatenated *in front of* whatever `uikit.TaskLabel` returned, pushing
   its own prefix slot (the spinner/warning/indent) two columns further
   right than a plain `run`/`rerun`/`role` tree puts it - and, since play
   header rows (`uikit.PlayRowText`) never got a matching shift, two
   columns further right than this very same debug tree's own header rows
   too. First fix: gave `TaskLabel` its own `marker` parameter (`""` for
   every other caller), inserted *inside* the function, right after its
   own prefix and before the title - which did put the busy indicator
   back in column 0, but left a task row's own indent under its play
   header at 4 columns (prefix slot *plus* a second, marker-only slot
   next to it), not `TaskIndent`'s own 2 - reported again live even after
   the column-0 fix, this time as "indentation is still off." Real fix:
   marker now *replaces* the prefix slot instead of adding a second one
   next to it (same priority order a plain tree already uses for
   spinner-vs-warning: active beats marker beats warning beats blank),
   so a task row's total indent is back to exactly 2 columns, matching a
   plain tree, with the busy indicator still in column 0 within that same
   slot. Confirmed live via `cat -A` on a captured pane line, not just by
   eye: an armed, idle row renders `● ok task` and an unarmed one `
   changed task` - both exactly 2 columns before the title starts.
2. **q/Ctrl-C could force-quit tangsible while the child was still alive
   and unsignaled.** `startOrContinue`'s very first `Spawn` call only
   published the resulting `*debugrun.Session` into `d.sess` *after* the
   blocking `RunWithBreakpoints` call returned - which, for a run with no
   armed breakpoints, is the entire run's duration. `handleKey`'s own
   q/Ctrl-C branch treats `d.sess == nil` as "nothing to abort yet, quit
   outright" - so pressing either during that whole window force-quit the
   real tangsible process while the shadow-copy `ansible-playbook` child
   (and its own descendants) kept running, completely unsignaled. Fixed
   by publishing `d.sess` via `QueueUpdateDraw` immediately once `Spawn`
   succeeds, before the blocking call. Confirmed live via tmux with a
   deliberately slow task (`command: sleep 5`, no breakpoints armed): `q`
   mid-run now interrupts the child (bar flips to "finished", no
   `ansible-playbook`/`sleep` process survives) without quitting
   tangsible itself, and a second `q` is what actually quits - matching
   the main tree's own two-stage `q`/Ctrl-C exactly.
3. **No progress indicator at all** (and, once added, an elapsed clock
   that didn't belong there). The top bar was a static
   `" Debug: <playbook> "` string; `d.tracker` (already driving the
   active-row spinner) was never surfaced as a percentage or fill. Fixed
   by giving the header its own `headerLine`, composed from the same
   primitives the main tree's own top bar uses (`uikit.ProgressFillLine`,
   `SpinnerAt`, `TruncateHostsList`) - deliberately *not* a call to
   `uikit.ComposeTopBarLine` itself, which bakes in both a `Filter:` field
   this view has no equivalent of and an elapsed mm:ss clock. The first
   version of this fix added that clock back in anyway (`uikit.
   MinutesSeconds`, ticking from `d.startedAt`) - reported live:
   unlike the main tree, whose own top bar only ever exists once a
   generation is already running, this TUI exists the whole time the
   user is still setting breakpoints before pressing Enter/s even once,
   and sits paused for however long between pauses - a clock ticking the
   entire time (including before any task has run) read as "how long has
   this debug session been stuck," not "how long has the playbook been
   running." Removed; the spinner alone still conveys "this view is
   alive," without claiming to measure anything.
4. **No auto-scroll/"following."** Nothing ever moved the cursor as a run
   advanced past the first screenful of tasks - unlike the main tree's own
   "following" (`CLAUDE.md`), which pins the cursor to the newest row
   until the user navigates away. Fixed by adding the same mechanism:
   `d.following` (default on), cleared by any genuine navigation via a
   `d.rebuilding` guard around `rebuildList`'s own trailing
   `SetCurrentItem` call (exactly mirroring `internal/session`'s own
   `s.rebuilding`/`SetChangedFunc` pair), resumed by `F` (same key, same
   "only F resumes it" rule).
5. **Enter didn't expand a task row to show its own hosts.** The debug
   tree had no per-host rows at all - every host's outcome only ever
   showed as `TaskLabel`'s own inline, collapsed segment. Enter couldn't
   be reused for this the way the main tree's own bare Enter is (Enter
   here already means "run/continue"), so expand/collapse is bound to
   `Tab` instead. Rows are flattened fresh on every `rebuildList` call
   (`flatten`, mirroring `uikit.FlattenRows`) rather than kept as a second,
   cached shape; a host sub-row renders via `uikit.HostLabel` - the exact
   function the main tree's own expanded task rows use - which needed
   `applyEvent` to start populating `TaskNode.Raw`/`Ignored` per host (only
   `Hosts` was populated before), the same two fields `HostLabel`/
   `HostColorTag` already read for the main tree. `playbook.
   hasIgnoreErrors` was exported (`HasIgnoreErrors`) for this, rather than
   duplicated - debug mode's skeleton is static and can't go through
   `PlaybookState.Apply` the way a plain run does.

Confirmed live via tmux for all five: the busy-indicator column now lines
up with a plain `run`'s; a deliberately slow, breakpoint-free task
confirmed q/Ctrl-C's two-stage behavior and left no orphaned process; the
top bar shows a real percentage and fill that reaches 100% on completion;
navigating away and back with `F` re-pins the cursor to the paused/active
row; and `Tab` on a finished task shows its own per-host row, including
the orange "ignored" color for a `ignore_errors: true` failure. `go vet`/
`go test ./...` clean across the whole repo.

## Motivating gap

The existing Resolved tab (`internal/session/resolved.go`) re-renders a
task's fields with variables substituted by spawning a throwaway
`ansible.builtin.template` stub against the task's own source text. It can
only see facts, `host_vars`, and `group_vars` - never a variable created
during the play itself (`set_fact`, `register`, a role's `vars:`) - because
nothing about that stub actually executes the play up to that point. This
is `DebugMode.md`'s original "Current situation" gap, unchanged.

## Original proposal (superseded)

The original idea: insert an `ansible.builtin.debug: var:
hostvars[inventory_hostname]` task after every task in the playbook, which
sees exactly the runtime state the Resolved tab can't. Rejected as a build
approach (not as a goal - the goal is unchanged) because doing this for
real requires rewriting every task list across the whole role/include tree
(blocks/rescue boundaries, loops, tags, `when:` propagation onto the
injected task), copying directories that may not even be safely writable
(galaxy-installed roles, `$ANSIBLE_ROLES_PATH`), and duplicates a lot of
data via a Jinja-templated stub for something Ansible's own execution
engine already computes internally for free. See conversation history for
the fuller critique; not reproduced here since it's superseded, not
informative for implementation.

## Decisions (2026-09-23)

1. **Use a custom strategy plugin, not playbook mutation.** Only
   strategy-layer code sees `VariableManager`/`task_vars` - callback hooks
   only ever receive a `TaskResult` (the module's output), never the
   variable pool that produced it. This is the load-bearing architectural
   fact behind the whole design: enhancing the existing callback plugin
   cannot do this, regardless of how it's extended.
2. **Ship it as a distinctly-named plugin (`tangsible_debug`), not by
   shadowing `linear`/`free`'s own names.** Unlike aggregate callbacks
   (which stack), only one strategy plugin is active per play, and
   `strategy:` is a per-play YAML key that can hardcode a value overriding
   whatever default Tangsible sets. Activate ours via `ANSIBLE_STRATEGY=
   tangsible_debug` (**correction, 2026-09-25 live spike:** there is no
   `--strategy` CLI flag in this ansible-core version to append the way
   `--diff` gets appended - see "Live spike findings" below; env var only,
   which sidesteps the "gambling on shadowing a built-in name" concern this
   bullet originally raised, since there's no CLI argument to collide with
   in the first place). **Accepted gap:** a play with its own explicit
   `strategy: linear` or `strategy: free` won't get debug data, same shape
   as the existing `strategy: free` gap (`design-docs/StrategyFree.md`) -
   not chased further for v1.
3. **Opt-in only**, gated behind a flag - not because the variable capture
   itself is expensive (Ansible already computes `task_vars` for every task
   regardless), but because serializing and shipping a potentially large
   per-host dict over the event pipe for every task is real added cost this
   project's ~10-host/interactive-dev-tool posture shouldn't pay by
   default.
4. **Delivery order: Resolved tab first, a dedicated all-variables tab
   second.** The captured snapshot becomes the `--extra-vars` seed for the
   Resolved tab's existing template-stub render (closing the actual
   documented gap above) before any new tab is built. The all-variables tab
   is real future scope, not dropped, just sequenced after.
5. **Scope is the whole playbook, not one on-demand task.** Once debug mode
   is on for a session, it stays on for every generation in that session
   (including reruns) - the same "fixed for the session's whole lifetime"
   treatment `--check` already gets (`internal/config/dialogflag.go`-style
   detection, not literally the same code path).
6. **No persistence for v1.** Captured snapshots live only in the running
   `PlaybookState`, never written to `.tangsible/runs/`. This is a
   deliberate scope cut, not an oversight - see "Shortcomings" for why it
   also does most of the work of bounding the security exposure below.
7. **`no_log` and vault redaction are documented shortcomings, not
   implemented in v1.** See "Shortcomings" for the concrete leak mechanics
   and the rationale for deferring rather than building the mitigations
   that were designed during discussion.

## Why a strategy plugin, not the callback plugin

Ansible's callback API (`v2_runner_on_ok`, etc.) receives a `TaskResult` -
the module's output - never `VariableManager`/`task_vars`. That boundary is
structural, not a missing feature to request; it's the reason motivation 1
of `OwnCallbackPlugin.md` (per-host timing) only ever needed timestamps,
never variable state. Only strategy-layer code (or an action plugin) sees
the variable pool that actually produced a task's rendered arguments.

Two consequences that shape everything below:

- This needs a second plugin, not a bigger version of the existing one.
- Unlike aggregate callbacks, which run *alongside* whatever else is
  enabled, only one strategy plugin executes a given play - the debug
  capture has to be baked directly into the code that actually runs the
  play, not layered on top of it.

## Proposed design

**The plugin: a fork of `ansible.plugins.strategy.linear.StrategyModule`.**
Subclass it and override only `_process_pending_results` - not `run()` -
calling `super()._process_pending_results(...)` first (this is what merges
a just-finished result's facts/`set_fact`/`register` output into the
variable manager) and then, for each result that just completed, call
`self._variable_manager.get_vars(play=iterator._play, host=host,
task=task)` and serialize it. This is not new computation - it's the same
call the real strategy already makes for every task dispatch to build
`task_vars` in the first place; the added cost is capturing and shipping
what Ansible was going to compute anyway, not computing something new.

**Transport: reuse `OwnCallbackPlugin.md`'s fd pipe.** The strategy plugin
writes to the same `TANGSIBLE_EVENT_FD` the bundled callback plugin already
writes to, as one more additive JSON event type (e.g.
`tangsible_vars_snapshot`, carrying `host`/`task.id`/the captured vars) -
no new transport, no second pipe. Both a callback object and a strategy
object exist in the same `ansible-playbook` process, each writing complete
newline-terminated JSON lines with a flush per write, which is safe for a
shared fd at this line-oriented granularity.

**Payload shape: diffs, not full snapshots.** The plugin already needs
per-host state to know what "just changed" means, so keep the last sent
snapshot per host in the plugin's own memory and ship only new/changed
top-level keys each time. Bounds the common case (most tasks touch a
handful of variables) without needing any Go-side change to make it
useful; a full-dict fallback stays available for whatever consumes it
first if diffing turns out not to be worth the complexity.

**Shipping.** Same mechanism `OwnCallbackPlugin.md` already settled: a
second sibling `.py` file in the release arch archive (GPL-3.0, forked from
ansible-core's own `linear.py`, same Apache-binary/GPL-plugin
arm's-length-aggregation licensing story - no new licensing question, just
a second file under the same reasoning), installed to the same
`<prefix>/share/tangsible` path. Activation sets `ANSIBLE_STRATEGY_PLUGINS`
(union with any existing value, never clobber - same discipline
`OwnCallbackPlugin.md`'s roadblock #5 already established for
`ANSIBLE_CALLBACKS_ENABLED`) and appends `--strategy tangsible_debug` to
the invocation.

**Activation mechanics (sketch, not finalized; updated 2026-09-25 per the
live spike above).** CLI flag name settled as `--tangsible-debug` -
stripped from the passthrough list before `ansible-playbook` ever sees
it, the same synthetic-flag treatment `--start-at-play` already gets - it
drives Tangsible's own env setup, not a real `ansible-playbook` argument,
so (like `--start-at-play`) it shouldn't be recorded into
`.tangsible/state.toml` history verbatim. Because debug mode needs to stay
active across every rerun in a session (decision 5), it's tracked as
session-level Go state (detected once, alongside `HasCheckFlag`'s
equivalent) and reapplied on every generation's spawn, rather than living
in the passthrough `Rest` a rerun replays.

Per the spike: there is no `--strategy`/`--strategy-plugins` CLI argument
to append in this ansible-core version at all (see "Live spike findings"
above) - activation is purely `ANSIBLE_STRATEGY=tangsible_debug` +
`ANSIBLE_STRATEGY_PLUGINS=<dir>` (union with any existing value, same
discipline `ANSIBLE_CALLBACK_PLUGINS` already gets), set the same way
`CallbackPluginEnv()` already builds its own env slice. No passthrough-arg
plumbing needed for this piece at all.

A `general.debug_mode` config default, mirroring `run_dialog`'s
flag-beats-config-beats-verb-default precedence, is a reasonable follow-on
but wasn't part of this discussion - not decided here. (Deliberately left
out of the first implementation pass for the same reason - not decided
yet, not because it's hard.)

**Biggest open risk, stated plainly - RESOLVED 2026-09-25, confirmed live.**
`OwnCallbackPlugin.md`'s roadblock #7 already flags semi-private-import
fragility for a callback fork; a strategy subclass leans on this
considerably harder - `_process_pending_results`'s exact signature/timing
and whether calling `get_vars()` immediately after `super()`'s call
actually observes just-merged facts were assumptions, not confirmed facts.
Spiked live the same way `OwnCallbackPlugin.md` spiked
`v2_runner_on_start`-under-`linear` before treating it as settled - see
"Live spike findings" immediately below. The hook point itself works
exactly as hypothesized; a different, more serious risk turned up instead.

## Live spike findings (2026-09-25)

**Status: paused here.** The spike confirmed the plugin's core mechanism
works, but surfaced a foundation-level risk serious enough that work
stopped before starting the real plugin build. Not a "some details need
adjusting" outcome - a "worth deciding whether to proceed at all" one.

**What was spiked.** A throwaway strategy plugin (not committed -
scratchpad-only), forked from the *installed* `ansible.plugins.strategy.
linear.StrategyModule` (ansible-core 2.19.11), overriding only
`_process_pending_results`: call `super()._process_pending_results(...)`
first, then for each `_RawTaskResult` in its return value, call
`self._variable_manager.get_vars(play=iterator._play, host=result.host,
task=result.task)`. Run against a throwaway two-host fixture
(`testdata/multihost-inventory.ini`) with a `set_fact` → `register` →
a task reading both → a second overwriting `set_fact` → a final task
reading the update, five tasks per host.

**Confirmed: the hook point works exactly as designed.**
- `set_fact` and `register` values both show up in `get_vars()`'s result
  for the very same task that produced them, correctly scoped per host (no
  cross-host bleed between `host1`/`host2` running concurrently).
- A later `set_fact` overwriting an earlier one is reflected correctly on
  the next task's snapshot.
- All 10 host×task combinations (2 hosts × 5 tasks) produced exactly one
  snapshot each - reading results off `super()`'s own return value (rather
  than, say, assuming exactly one result per call) means concurrent
  multi-host completions are not a dropped-result risk, which was itself
  an open question worth confirming, not just assumed.

**New finding #1: `--strategy`/`--strategy-plugins` don't exist as CLI
flags in this ansible-core version at all.** Grepping the actual CLI
arg parser (`ansible/cli/*.py`) turns up no trace of either. Strategy
selection is env-var/config only: `ANSIBLE_STRATEGY=<name>` (config key
`DEFAULT_STRATEGY`, `[defaults] strategy`) and `ANSIBLE_STRATEGY_PLUGINS`
for the search path (config key `DEFAULT_STRATEGY_PLUGIN_PATH`,
`[defaults] strategy_plugins`, colon-separated `pathspec` - unions the
same way `ANSIBLE_CALLBACK_PLUGINS` already does). Confirmed working via
the spike run. This actually **simplifies** decision 2/"Activation
mechanics" below - activation never touches the passthrough arg list at
all, so the "gambling on shadowing a same-named built-in" concern that
motivated the distinct `tangsible_debug` name doesn't even arise the way
originally framed (there's no `--strategy <name>` argument to collide
with); the distinct name is still right, just for the simpler reason that
`ANSIBLE_STRATEGY` only ever holds one value at a time regardless.

**New finding #2 - the real bummer: third-party strategy plugins are
being deprecated by ansible-core itself, with no stated replacement.**
The spike run emitted this unprompted:
```
[DEPRECATION WARNING]: Use of strategy plugins not included in
ansible.builtin are deprecated and do not carry any backwards
compatibility guarantees. No alternative for third party strategy
plugins is currently planned. This feature will be removed from
ansible-core in a future release.
```
This is a foundation-level risk for the whole Phase 1 design (and more so
for Phase 2, which leans on the same extension point harder still) - not
an implementation detail to patch around. No removal timeline is stated,
and the feature works today, but "we're deprecating this, no replacement
planned" from the upstream project this whole app is built on top of is
a different category of risk than anything else discussed in this doc so
far. This is why work paused here rather than continuing into the actual
`tangsible_debug.py` plugin build - worth a deliberate decision on whether
to accept that risk (this is opt-in debug tooling, not something a user's
production run depends on, and "future release" could be a long way off)
before sinking more implementation time in, rather than discovering it
after the fact.

**New finding #3 (minor, fixable, not a blocker).** Captured `task_vars`
can contain non-JSON-serializable Python objects nested inside ordinary-
looking data - confirmed live: a `register`'d command result's own
`warnings` list contained real `WarningSummary`/`Event` objects, not
plain strings. A naive per-key `json.dumps` on the snapshot breaks on
this. The real plugin needs `json.dumps(vars, default=str)` (or an
equivalent recursive sanitizer) at the whole-snapshot level rather than
try/excepting key by key - noted for whenever the actual build resumes,
not itself a reason to pause.

## Pivot spike (2026-09-27): built-in task debugger instead of a custom strategy plugin

**Status: spiked live, not yet decided whether to pursue over Phase 1/2
above.** Motivated directly by the 2026-09-25 pause: rather than accepting
the "no replacement planned" deprecation risk for a forked third-party
strategy plugin, this explores whether ansible-core's own *built-in* task
debugger - the interactive `pdb`-like session real `ansible-playbook`
already ships - can be driven programmatically by Tangsible (read its
stdout, write commands to its stdin) as an alternative foundation for both
Phase 1 (variable inspection) and Phase 2 (breakpoints/single-step) above.
User's framing: rather than mutating the tracked playbook by inserting new
tasks (the "Original proposal (superseded)" approach, still rejected for
the reasons given there), duplicate the playbook and set the existing
`debugger: always` keyword on a task already present in the copy - a much
smaller mutation than inserting sibling tasks, since it needs no
block/rescue/loop-propagation handling at all.

**What was spiked.** Read `ansible/plugins/strategy/__init__.py` and
`ansible/executor/task_result.py` directly (ansible-core 2.19.11, the same
installed copy the 2026-09-25 spike used), then drove a real
`ansible-playbook` child process over a pty from a throwaway Python driver
script (not committed - scratchpad-only), sending debugger commands and
capturing raw stdout. Two throwaway fixtures, both two-host
(`testdata/multihost-inventory.ini`): one plain `linear` play (`set_fact`
-> a `command`+`register` task with `debugger: always` -> a `debug:` task
reading both), one identical play with `strategy: free` added.

**Confirmed: this is built-in infrastructure, not a strategy plugin at
all.** `debugger:` is an ordinary `FieldAttribute` on
`ansible/playbook/base.py:761`, valid on task/block/play
(`always`/`never`/`on_failed`/`on_unreachable`/`on_skipped`,
`_post_validate_debugger` at `base.py:200`), checked by
`_TaskResult.needs_debugger()` (`task_result.py:143`). The actual pause
point, `debug_closure` (`strategy/__init__.py:137`), wraps
`StrategyBase._process_pending_results` itself - the base class every
strategy (`linear`, `free`, any future one) inherits from - not a
strategy-specific override. `ansible/plugins/strategy/debug.py`'s
`strategy: debug` (the old `--step`-adjacent plugin) turns out to be a
now-largely-vestigial thin subclass of `linear` that just sets
`self.debugger_active = True`; the actual mechanism this design would lean
on is the base-class hook, triggered by the `debugger:` keyword directly,
with no need to select any particular strategy at all.

**Confirmed: no deprecation warning, under either strategy.** Both spike
runs (`linear` and `strategy: free`) completed with zero occurrence of
`DEPRECATION`/`deprecat` anywhere in captured output - a real, live check,
not an inference from reading source. This directly removes the
2026-09-25 pause's blocking concern: there is no "no replacement planned"
warning here because this isn't the deprecated surface.

**Confirmed: works under `strategy: free`, unlike the original design.**
Both hosts independently reached their own `debugger: always` stop under
`free` and blocked correctly - `debug_closure` processes one completed
result at a time regardless of which strategy queued it, so the "no
single clean pause point under `free`" gap the original Phase 2 sketch
(and `StrategyFree.md`) accepted doesn't apply to this mechanism.

**The interface: a plain-text `cmd.Cmd` REPL, not JSON.** Confirms the
premise from the start of this conversation. Prompt format (`Debugger`
class, `strategy/__init__.py:1120`): `[<host>] TASK: <task-str> (debug)> `.
Commands actually spiked: `p <python-expr>` (`do_pprint`/`do_p`, output via
`pprint.pformat` - Python repr syntax, e.g. `dict_keys([...])`,
`'hello-host1'`, not JSON), `c`/`continue`, with `r`/`redo` and `u`/
`update_task` read from source but not exercised live this round. A parser
would need to handle Python-repr text, not `json.loads` - and would hit the
same non-JSON-serializable-object problem the 2026-09-25 spike already
flagged for the strategy-plugin approach: `p result._return_data` on a
task with warnings printed a nested `WarningSummary(event=Event(...))`
object inline in the `pformat` output, not a plain string.

**New finding: `task_vars` in the debug scope is a pre-task snapshot, not
refreshed with the halted task's own effects.** `debug_closure` reuses
`_queued_task_cache`'s snapshot - captured when the task was *queued*,
before it ran - rather than calling `get_vars()` fresh after
`super()._process_pending_results()` already merged the result (which is
what the original 2026-09-23 custom-strategy design specifically did, and
confirmed live in the 2026-09-25 spike, to see `set_fact`/`register` from
the very same task). Confirmed live: at the `register something` task's
own debug stop, `p task_vars['my_var']` (set by the *previous* task)
returned `'hello-host1'` correctly, but `p reg_result` (registered by
*this* task) raised `NameError` and the key was absent from
`task_vars.keys()` entirely. The task's own output is still reachable via
`p result._return_data` (confirmed: full module return dict, including
`stdout`/`changed`/`warnings`) - but that duplicates what the jsonl
callback already provides via `TaskNode.Raw`, not new data. Net: this
mechanism's genuinely new value over the existing jsonl pipe is the
*control* plane (pause/continue/redo), not the variable-snapshot data
plane, which is actually a step down from Phase 1's original design for
same-task variable inspection (mitigated only by breakpointing one task
later than the one whose effect you want to see).

**`redo` is a real, novel capability worth noting**, read from source but
not yet exercised live: `do_redo` (`strategy/__init__.py:1164`) rolls back
host state and re-queues the same task, letting a paused session
re-execute the task it just watched fail (optionally after `update_task`
re-templates it with edited `task_vars`) - a genuinely new "fix and retry"
capability neither Phase 1 nor Phase 2's sketch above has an equivalent
for today.

**Net assessment, not yet a decision.** This trades the 2026-09-25 spike's
foundation-level deprecation risk for a real but smaller data-plane
limitation (pre-task `task_vars`) and a REPL-scraping integration instead
of a structured JSON channel. Whether this replaces the Phase 1/2 design
above outright, or Phase 1/2's custom strategy plugin still ends up
worthwhile for its stronger post-merge variable capture despite the
deprecation risk, isn't decided - deferred to further discussion per
conversation history.

## Workflow design (2026-09-27): breakpoint picker + ephemeral mutated copy

**Status: discussed and largely agreed, not yet built.** Builds directly on
"Pivot spike (2026-09-27)" above - assumes the built-in `debugger:` keyword
foundation, not the custom strategy plugin. Two entry-point workflows,
proposed by the user and refined across discussion:

1. **Fresh start.** List every task (backed by static source, not a fresh
   `ansible-playbook --list-tasks` scrape - see below); the user navigates
   and toggles breakpoints; Tangsible writes a mutated copy with
   `debugger: always` inserted at the chosen task(s); the copy is executed;
   the copy is cleaned up once the generation finishes (regardless of
   outcome).
2. **From an already-running session.** Ordinary `run`, then the rerun
   dialog (`internal/session/livesession_rerundialog.go`'s `rerunFieldSync`)
   gets a "Debug" option alongside its existing three checkboxes; picking
   it opens the same breakpoint picker from workflow 1, then proceeds
   identically. This is always a fresh generation, never an attach to the
   one currently running - the breakpoint has to be set before the target
   task executes, so there's no other way to do it.

### Never mutate the tracked file - not even backup-then-revert

The user's original sketch was: back up the real playbook, mutate it in
place, run, then revert from the backup once finished. Changed instead to:
never touch the original at all - write a full mutated copy (same
directory as the original, so relative `roles:`/`include_tasks:` still
resolve) and point `ansible-playbook` at the copy, exactly the pattern
`WriteRoleStub`/`TrimPlaybookToPlay` already use, with the same
`cleanup func()` shape `StartRoleSession` already returns.

**Why this matters, not just tidiness.** A backup-then-revert scheme has a
real failure window: if Tangsible is killed, panics, or the revert step
itself has a bug, the user's actual tracked playbook is left on disk with
`debugger: always` spliced into it - a real, silent corruption of
something likely under version control. An ephemeral copy failing to get
cleaned up is cosmetically annoying (an oddly-named leftover file, the same
accepted risk `WriteRoleStub`'s own doc comment already lives with) but
never touches the file the user actually cares about.

### Backing the task picker off the existing source index, not `--list-tasks`

`--list-tasks --list-hosts` (already used by `internal/runner/progress.go`
and the `hosts` verb's Plays tab) gives task *names* only - ambiguous the
same way `--start-at-task` was already rejected for (`Start with a play`
above), and unusable as a splice target since it carries no line
information. `internal/source.BuildTaskSourceIndex` already walks the
playbook's directory tree with a line-tracked `yaml.Node` parse and indexes
every task by exact `(path, start line)` - the same data that already backs
the "Task definition" tab and `TrimPlaybookToPlay`. The breakpoint picker
should enumerate from that index directly rather than re-deriving a
separate, coarser task list: it's simultaneously the right identifier for
"which physical YAML location does this breakpoint target" and already
excludes anything the splice couldn't act on anyway (a dynamically-included
task has no fixed line to splice into, same gap `BuildTaskSourceIndex`
already documents as "a miss just means no source section, never an
error"). Presentation: a flat `TreeList`-based picker, same shape as
revisit's own list (`RunRevisitListTUI`), Space/Enter toggling a breakpoint
marker per row.

### Inserting the keyword: splice raw text, don't re-encode the YAML tree

`internal/vaultfile`'s `Reassemble` already hit this exact class of bug
once: its first version re-encoded the whole `yaml.Node` tree on save,
which kept ciphertext values correct but silently reformatted every
untouched block's indentation (`yaml.v3`'s encoder clamps to `[2,9]`,
real `ansible-vault` output uses 10), making an unrelated `git diff` show
every secret as changed. Fixed there by splicing raw source-text spans
instead of re-encoding (`keyContentSpans`). The same fix applies here:
given a task's start line/indent from the source index, splice a literal
`debugger: always\n` line into the raw file bytes at that point rather
than decoding/mutating/re-encoding the tree - everything else in the copy
stays byte-identical to the original.

### v1 scope: top-level playbook tasks only

Breakpointing a task defined inside a role or included file means mutating
a file that may not even live under the project directory
(`$ANSIBLE_ROLES_PATH`, a galaxy-installed collection) - riskier to touch
and, if copied wholesale, heavier than copying one top-level file. Scoped
out for v1, the same cut `Start with a play` already made for
`import_playbook` ("only a playbook's own top-level plays are addressable
... a known, documented gap, not a partial implementation pretending to be
complete").

**v2 idea, spiked live 2026-09-27 - doesn't work for the common case,
dropped.** The original pitch: reach into a role's own task file without
ever touching the real one, by copying just that role into a scratch
directory and prepending it to `ANSIBLE_ROLES_PATH` (union with any
existing value, same discipline `CallbackPluginEnv`/
`ANSIBLE_STRATEGY_PLUGINS` already use), on the assumption that Ansible
resolves a role by first match on that search path so the shadow copy
would be found first and the genuine role directory on disk would never be
written to at all.

Checked against `ansible/playbook/role/definition.py:166-182` (ansible-core
2.19.11) before trusting that assumption: role resolution is a hardcoded,
sequential first-match search, and `<playbook_dir>/roles/<name>` is always
checked *first*, unconditionally - `ANSIBLE_ROLES_PATH` entries are never
even consulted for a role name that already resolves there. There is no
environment variable that reorders this; it's baked into the search-path
list construction itself.

Confirmed live with a throwaway two-role setup (a real `roles/myrole` next
to the playbook, plus a shadow copy elsewhere with `debugger: always`
spliced into its one task, each copy's task echoing a different string so
the two are distinguishable from stdout alone regardless of whether the
debugger fires):

- **With the local `roles/myrole` present** and the shadow prepended to
  `ANSIBLE_ROLES_PATH`, the *local* role ran (`"stdout": "from LOCAL
  role"`) and no debug prompt appeared at all - the shadow copy was
  completely ignored, exactly as the source predicts.
- **With the local `roles/myrole` temporarily out of the way**, the shadow
  copy was found via `ANSIBLE_ROLES_PATH` and its `debugger: always`
  genuinely fired (`[localhost] TASK: myrole : local role task (debug)>`
  appeared in the output).

Net: the technique only works when no role of the same name already lives
in `<playbook_dir>/roles` - an out-of-tree, galaxy-installed or
`roles_path`-only role, not a project's own hand-rolled role sitting next
to its playbook, which is the case someone debugging their own playbook
interactively would most want. Making it work for a local role would
require either moving the ephemeral top-level playbook copy out of the
project directory (breaks `BuildTaskSourceIndex`'s directory-tree walk for
the whole session, not just this feature) or temporarily renaming the real
`roles/<name>` directory out of the way (reintroduces exactly the
backup-then-revert risk already rejected for file content, now on a whole
directory - arguably worse, since a role directory missing from its usual
path mid-session is a more confusing failure than one mutated file).

**Decided: dropped, not narrowed.** Tasks inside roles/includes stay a
documented v1 (and likely permanent) gap, the same shape `import_playbook`
already is for `Start with a play` - not chased further.

**Superseded the same day for local roles specifically - see below.** The
full directory-tree hardlink shadow copy (next section) reopens local
roles as in-scope; only genuinely out-of-tree roles keep this gap.

### Superseding idea (2026-09-27): full directory-tree hardlink shadow copy

**Status: spiked live, confirmed working end-to-end.** User's follow-up
idea, proposed right after the `$ANSIBLE_ROLES_PATH` attempt above was
dropped: instead of trying to shadow one role via a search-path trick,
duplicate the *whole* directory tree the playbook depends on - playbook,
roles, everything - and mutate freely inside that copy. This sidesteps
the problem the search-path trick couldn't get around (a local role always
winning resolution over anything in `ANSIBLE_ROLES_PATH`) simply by not
needing role resolution to prefer anything: the copy *is* a complete,
self-contained project directory, run in place of the original.

**Tree boundary: reuse `BuildTaskSourceIndex`'s existing walk, don't invent
a new one.** "The whole directory tree" doesn't need fresh scoping -
`internal/source.BuildTaskSourceIndex` already walks exactly this boundary
(everything under the playbook's own directory) to build the source
index. Copying that same walked set means the copy's extent is something
already battle-tested, not a new judgment call, and it naturally inherits
the same already-accepted gaps (out-of-tree roles, `import_playbook`)
rather than requiring new ones.

**Copy mechanism: hardlinks, not a byte-for-byte copy, so cost is
independent of repo size.** A real playbook repo can carry vendored
collections or galaxy-installed roles that are large, and blindly `cp -r`
would mean paying for that every time debug mode starts. Instead: walk the
tree, and for every file except the one(s) actually being mutated,
`os.Link` instead of copying bytes - cost becomes independent of repo
size entirely. Two consequences:

- Hardlinks require the same filesystem, so the scratch copy needs to live
  under the project's own tree (`.tangsible/debug/<RunID>/`, mirroring
  `.tangsible/runs/<RunID>.jsonl`'s own naming), not a system temp
  directory.
- The walk must explicitly prune `.tangsible/` and `.git/` - neither is
  something `BuildTaskSourceIndex`'s existing walk needs to skip today
  (it only ever reads `.yml`/`.yaml`), but a full directory copy would
  otherwise try to walk the app's own state directory into itself.

**Spiked live** (throwaway Python driver, not committed - scratchpad
only): built a small project - `playbook.yml` (`roles: [myrole]`), a local
`roles/myrole/tasks/main.yml` (a `set_fact` task, then the task to be
breakpointed), a `group_vars/all.yml`, plus dummy `.tangsible/` and `.git/`
directories that must never be walked into. Confirmed:

- The walk correctly excludes `.tangsible/`/`.git/` (pruned from
  `os.walk`'s `dirnames` in place, before descending).
- Every copied file is a genuine hardlink to the original: same
  `(st_dev, st_ino)`, `nlink` bumped to 2.
- **Splice-by-unlink-then-write is the safe order, confirmed live, not
  just reasoned about.** A hardlink shares the same inode as the
  original - writing to it in place would mutate the real file's content,
  exactly the corruption this design exists to avoid. Removing the copy's
  hardlink entry for the one target file *before* writing the spliced
  content is what keeps the original untouched: confirmed afterward that
  the original file's content and inode were unchanged, that it no longer
  shared an inode with the (now independently-written) copy, and that
  every *other* hardlinked file in the copy was unaffected by the splice.
- **Ran a real `ansible-playbook` against the copy** (cwd set to the copy
  directory): the local role resolved from the copy's own `roles/`
  subdirectory with no search-path tricks needed, the spliced
  `debugger: always` fired exactly at the target task
  (`[localhost] TASK: myrole : local role task (debug)>`), `task_vars`
  correctly showed the role's own earlier `set_fact`, and the run
  completed normally on `c`.
- **Confirmed the real project tree was untouched afterward** - the real
  role task file's content was unchanged and it still shared an inode with
  the (unspliced) copy.
- **Confirmed the same-filesystem requirement live, not just in theory.**
  `/tmp` on this box is `tmpfs`; the project directory is on a different
  filesystem (`zfs`, confirmed via `df`). Attempting `os.Link` from the
  project into `/tmp` failed immediately with `OSError: [Errno 18]
  Invalid cross-device link` - concretely validating why the scratch copy
  has to live under the project's own tree, not a system temp directory.
- Cleanup is a single `rm -rf` on the copy tree: only removes directory
  entries, leaves every hardlinked original's data untouched (`nlink`
  drops back to 1).

**One accepted, narrow residual risk, not tested live.** This is only as
safe as the assumption that nothing the playbook does writes back into its
own source tree via a hardlinked path. Ansible's own file-writing modules
(`copy`/`template`/`lineinfile`) write to a temp file and atomically
rename over the destination, which is safe against a hardlink (rename
swaps the directory entry, never touches the shared inode's content) - but
a task doing a direct in-place write to a path inside the copied tree
wouldn't be. Documented as a caveat, not chased further; a playbook
mutating its own git-tracked source as a side effect of a normal run would
be unusual.

**Net: supersedes the dropped `$ANSIBLE_ROLES_PATH` idea for the case that
actually matters.** Local roles are no longer out of scope - "v1 scope:
top-level playbook tasks only" above no longer needs to apply to roles
found inside the playbook's own directory tree, only to roles that live
entirely outside it (galaxy-installed, `roles_path`-only), which this copy
naturally excludes the same way `BuildTaskSourceIndex` already does. The
`$ANSIBLE_ROLES_PATH` shadow-path trick isn't wasted: it remains the
mechanism for that narrower, genuinely out-of-tree case, which a
directory-tree copy can't reach either.

### New wrinkle: driving the REPL means owning the child's real stdout

`TANGSIBLE_EVENT_FD` (`OwnCallbackPlugin.md`) exists specifically so
Tangsible's own event stream doesn't collide with whatever stdout callback
the user already has configured - the child's real stdout is deliberately
left alone. The built-in debugger doesn't use that channel at all: its
`cmd.Cmd` REPL talks over the process's actual inherited stdin/stdout. That
means debug mode needs Tangsible to fully own and parse that stream
instead of leaving it free - and a user's own configured stdout callback
writing ordinary per-task chatter to the same stream could be
indistinguishable from, or interleaved with, the debugger's own prompt
text, making reliable parsing fragile. Proposed direction (not yet built
or spiked): while debug mode is active, force a quiet/minimal stdout
callback (e.g. `ANSIBLE_STDOUT_CALLBACK=minimal`, overriding whatever the
user configured) so the stream is unambiguous - an acceptable, disclosed
trade for this already-opt-in, interactive-only mode, rather than trying
to robustly tell the two apart.

### Open questions carried forward

- Whether the fresh-start entry point is a distinct verb (`tangsible
  debug`) or a flag/dialog on `run`/`rerun`/`role` - the latter seems to
  fit better given how much of the live-TUI machinery is shared afterward,
  but not decided.
- How the TUI represents "currently paused at (host, task)" and offers
  step/continue/redo once multiple breakpoints and multiple hosts can be
  paused independently - not designed yet, carries over from Phase 2's own
  "a 'paused' TUI state ... net new chrome" open item below.
- Exact mechanism for forcing a quiet stdout callback during debug mode -
  sketched above, not spiked live.

## Full interactive-debugger vision and dual-channel spike (2026-10-14)

**Status: vision agreed, two foundational mechanics spiked live and
confirmed; several items explicitly deferred past v1.** Follows directly
from "Workflow design (2026-09-27)" above, but goes further: that section
assumed breakpoints get spliced only onto chosen tasks, and left the live
tree/debugger integration (its own "M3", harder tier) undesigned. This
section replaces that assumption and tackles the integration question
head-on, because it turns out to be required for v1, not an optional
later enhancement.

### The actual vision

Laid out directly, not inferred: start with a `debug` entry point;
Tangsible shows the whole task tree up front (every task that will be
involved, before any execution); the user navigates that tree and sets
breakpoints on it directly; the user then has the usual debugger controls
- Step, Step Into, Step Out, Run (till breakpoint); when execution stops
(single step or breakpoint hit), Tangsible highlights the current position
*on that same tree*; the user can query (and ideally modify) variables,
either by browsing a list of everything known or by typing a search
string; and the existing drill-down still works as it already does. No
separate raw-terminal fallback screen - one continuous tree throughout.

This is explicitly **not** the M1 raw-terminal-handoff MVP proposed
earlier: that approach would satisfy none of "the tree highlights the
current position" or "query variables from the TUI," since it hands the
whole terminal to the child process with no Tangsible-side visibility into
where execution actually is. Building this vision means the stdout-
ownership and REPL-integration problems flagged as "M3, harder, optional"
in the earlier roadmap are actually required for a v1 that matches what
was asked for - acknowledged and accepted.

### Two refinements that make the controls and the data plane easier than feared

**Step / Run can be the same mechanism, decided client-side, by splicing
`debugger: always` onto every task rather than onto chosen breakpoints
only.** This supersedes "Workflow design (2026-09-27)"'s assumption of
splicing only at chosen tasks. Instead: splice the keyword onto every task
in the shadow copy uniformly, and let "breakpoint" become a purely
in-memory set Tangsible checks on each pause - if the paused task isn't in
that set (and the user isn't single-stepping), auto-send `c` immediately
without surfacing anything to the user (= Run); otherwise, actually
present the pause (= Step / breakpoint hit). Consequences, both upsides:
Step and Step Into fall out for free (the next paused task just *is* the
next thing, whether a sibling task or the first task of an include -
Ansible flattens includes into the same sequential stream regardless), and
breakpoints become toggleable live mid-run with no re-splicing needed for
any statically-compiled task. The accepted cost: every task now pays a
synchronous debugger round-trip even while "running free" between
breakpoints, not just the chosen ones - judged acceptable at this
project's own ~10-host/interactive-dev-tool scale, not benchmarked.

**The debugger's REPL doesn't need a Python-repr parser if the query
itself emits JSON.** Rather than parsing `pprint.pformat`'s output (the
earlier flagged fragility), have the query compute and `print()` its own
JSON directly as a side effect: `print(json.dumps(expr, default=str)) or
0` writes clean JSON to the real stdout stream with only a throwaway
trailing line (the `pformat`'d result of the `or 0`) to discard. `default=
str` also folds in the "non-JSON-serializable objects nested in results"
mitigation the very first spike (`New finding #3` under "Live spike
findings (2026-09-25)") already flagged as needed. Confirmed live (see
below) - with one correction: `json` isn't pre-imported into the strategy
module's globals the debugger evaluates against, so the query needs
`__import__('json').dumps(...)`, not a bare `json.dumps(...)`.

### Hard or open items, discussed and triaged

1. **Step Out - deferred past v1, agreed.** Ansible flattens
   includes/roles/blocks into a linear task sequence at the strategy level;
   there's no real call stack. "Where does this include/block end" is
   knowable statically for `import_tasks`/`roles:`/blocks, but not
   reliably for a dynamically-resolved `include_tasks:` (especially one
   inside a loop, which - per "Aggregation" in the main `CLAUDE.md` -
   gets a fresh Task object per iteration per host). Treated as
   best-effort-at-best, not pursued for v1.
2. **Variable modification persisting beyond an immediate same-task redo -
   structurally unconfirmed, deferred past v1 regardless.** What's
   confirmed (`strategy/__init__.py:171-189`): `task_vars` in the
   debugger's scope is the same dict object `_queue_task` reuses on
   `redo`, so a mutation plus `redo` does affect the re-executed task.
   Whether a mutation also reaches *later, not-yet-run* tasks depends on
   whether anything in the debugger's exec scope reaches back into
   `VariableManager`'s own persistent store, which hasn't been checked -
   `self` (the strategy instance) isn't exposed to the eval/exec scope,
   only `task`/`host`/`task_vars`/`play_context`/`result`. Not spiked
   further this round since it isn't a stated v1 requirement - viewing
   variables is the v1 bar, not modifying them.
3. **Driving the debugger's REPL while keeping the live tree populated
   from the jsonl event stream, concurrently - the central engineering
   risk, now spiked (see below).**
4. **Variable volume** - even a trivial two-task fixture has ~25 keys in
   `task_vars`; a real playbook with gathered facts will have hundreds,
   several deeply nested (`ansible_facts`). Needs a real searchable/
   collapsible view (reusing this project's existing search/filter
   machinery), not a flat dump. Acknowledged, not designed further this
   round.
5. **Per-host clarity while paused** - hosts pause one at a time (even
   under `strategy: free`), but potentially back-to-back for the same
   breakpoint. Acknowledged as real UI work; may not need to be fully
   solved for v1.

### Live spike: dual-channel architecture + JSON-query trick

**What was spiked** (throwaway Python driver, not committed - scratchpad
only, ansible-core 2.19.11): a 4-task, 2-host fixture
(`testdata/multihost-inventory.ini`) with `debugger: always` spliced onto
every task (simulating the uniform-splice approach above), run with the
*real* bundled callback plugin (`callback/tangsible_jsonl.py`) wired up
exactly as `SpawnGeneration` does it - `ANSIBLE_CALLBACK_PLUGINS`/
`ANSIBLE_CALLBACKS_ENABLED` set, a pipe's write end passed to the child and
named via `TANGSIBLE_EVENT_FD` - while *also* driving the debugger's own
REPL over a pty standing in for the child's inherited stdin/stdout (plus
`ANSIBLE_STDOUT_CALLBACK=minimal` to keep that stream's other noise down).
Two independent reader loops: one draining the event-fd pipe and decoding
each jsonl line, one driving the pty (detect the `[host] TASK: name
(debug)>` prompt, send `c` or a query, read the response).

**Confirmed: the two channels coexist correctly, and a pause genuinely
blocks event delivery too - not just display.** Timestamped every jsonl
event and every debug-prompt sighting across the whole run. Events for a
task/host arrive at or just before that same task/host's own debug pause
(matches the code: `send_callback` happens inside the original
`_process_pending_results`, which runs before `debug_closure`'s own
post-loop pause). More importantly: **no event for the next task appeared
until every host's pause on the current task had been resumed** - the
whole child process is truly frozen during a pause, confirmed with the
real plugin and fd wiring active, not just inferred from reading source
as the 2026-09-27 spikes did. Practical consequence: the live tree and the
debugger are naturally kept in lockstep for free - there's no risk of the
tree racing ahead of what the user is currently paused on, and no new
synchronization mechanism is needed beyond "read both channels and the
ordering falls out."

**Confirmed: the JSON-query trick works, modulo the `__import__` fix
above.** `print(__import__('json').dumps(task_vars.get('step_var'),
default=str)) or 0` produced clean, directly parseable JSON as one line of
the debugger's response (plus the pty's own local-echo of the sent command
and the throwaway `0` line, both trivial to discard/ignore).

**Incidentally reconfirmed the pre-task-snapshot limitation via this new,
independent method.** At `task three`'s own pause - *after* that task had
already reassigned the variable being queried - the JSON query still
returned the value from *before* task three ran (set by an earlier task).
Same finding as "Pivot spike (2026-09-27)"'s `task_vars` limitation,
reconfirmed through the JSON-query path rather than raw `p`/`pformat`.

**Net: the two things most likely to force a redesign both check out.**
Live-tree-while-debugging and structured-data-without-repr-parsing are no
longer open questions - the dual-channel architecture and the JSON-query
trick are both ready to build on. What remains undesigned: the actual
"paused" TUI chrome, the pre-run static tree (seeded from
`BuildTaskSourceIndex`, not yet built), and the breakpoint-set data
structure and its live toggling - none of which were blocked on this
spike, but none of which are started either.

## Consuming the data

**Resolved tab (`internal/session/resolved.go`), first deliverable.**
`resolveTaskValues` keeps its existing template-stub render (real Jinja
evaluation of the task's own source expressions still has to happen
somewhere, and "let Ansible do the actual work" stays the right call here)
but seeds it via `--extra-vars @snapshot.json` from the captured
in-memory snapshot instead of only gathering facts/`host_vars`/`group_vars`
fresh. This directly closes the documented gap above. When debug mode is
on, this also means the tab no longer needs its own fact-gathering pass -
the data is already sitting in `PlaybookState` - though the stub-render
call itself still runs to do the actual templating.

**All-variables tab, second deliverable (design not started).** A tab
showing (almost) all variables known for a given `(task, host)`, built
from the same captured snapshots, once the Resolved tab's consumption path
above is real. Deliberately not designed further here - sequencing was the
only thing decided (see decision 4).

## Shortcomings (accepted, documented, not solved in v1)

Both of these were discussed and deliberately deferred rather than solved,
with a rationale, not just noted as future work:

- **`no_log` doesn't stop the leak past the task itself.** A task's
  `no_log: true` only suppresses *that task's own result* - if it's a
  `set_fact`/`register`, the value it writes is now an ordinary fact for
  the rest of the play, and every later task's captured snapshot would
  still carry it. (A real mitigation was designed during discussion: a
  static scan - same shape as `internal/source`'s directory walk - for
  tasks with `no_log: true` (checked at the task's own level *and*
  inherited from an enclosing `block:`/play) that also have a literal
  `register:` or `set_fact:` key, collecting those variable names into a
  permanent redaction set applied to every snapshot for the rest of the
  run. Not built for v1.)
- **Vaulted variables aren't excluded.** A decrypted `!vault`-tagged value
  sits in `task_vars` like any other variable once resolved, and would
  appear in a captured snapshot the same way. (Also designed, not built: an
  inline `!vault |`-tagged scalar in `host_vars`/`group_vars`/`vars` files
  is statically visible without decrypting anything - `internal/vaultfile`
  already parses exactly this for the `vault` verb - so a second static
  scan collecting those variable names could feed the same redaction set
  above. A **wholly vault-encrypted file** (`vars.yml` +
  fully-encrypted `vault.yml`, a common split) is opaque without the
  password and isn't covered by this; decrypting proactively to read key
  names is possible given `internal/vaultcrypto` already exists, but is
  meaningfully heavier and wasn't pursued.)
- **Both mitigations, even if built later, only redact by declared source,
  not by data flow.** A later task doing `set_fact: derived: "{{
  my_secret | some_filter }}"` with no `no_log` of its own produces a new
  variable that's still secret-derived but wouldn't be in either
  redaction set. Real taint-tracking is out of scope regardless of whether
  the simpler redaction above ever gets built.

**Why deferred rather than built:** this is an explicitly opt-in debug
mode - a user turning it on has already accepted that intermediate
variable state gets surfaced more broadly than normal operation, in the
same spirit real `ansible-playbook -vvv`/`--check` diagnostics already
expose things day-to-day output doesn't. Decision 6 (no persistence) is
what actually bounds the risk that matters most: nothing captured here is
ever written to disk, so a `no_log`'d or vaulted secret exposed this way
never ends up sitting in a run log or anywhere a `git diff`/backup/shared
machine could later surface it - the exposure window is the live on-screen
session only. Revisit if/when persistence (decision 6) is reconsidered,
since that would meaningfully change the calculus.

## Open questions

- **New, top of the list as of 2026-09-25, reframed 2026-09-27: whether to
  accept the third-party-strategy-plugin deprecation risk at all, now that
  a built-in-debugger alternative avoiding it has been spiked** (see "Live
  spike findings (2026-09-25)" and "Pivot spike (2026-09-27)" above) before
  resuming implementation. This is what the whole effort is paused on right
  now - everything else below is secondary until this is decided.
- Whether a `general.debug_mode` config default is wanted alongside the
  now-settled `--tangsible-debug` CLI flag (sketch only, not decided).
- Whether the all-variables tab (deliverable 2) reuses the Resolved tab's
  UI shape or needs its own - not designed yet.
- ~~Whether the `_process_pending_results` hook point survives a live
  spike unchanged~~ - resolved 2026-09-25: yes, unchanged, confirmed live
  (see "Live spike findings").

## Phase 2: interactive debugger (breakpoints / single-step)

**Status: discussed 2026-09-25, conceptual only.** Feasibility was checked
by reading ansible-core's actual strategy source (2.19.11, as installed),
not just reasoned about abstractly. Sequenced after Phase 1 above - shares
its foundation, isn't a prerequisite for it.

### Motivating idea

Pause playbook execution at a chosen point - a breakpoint on a specific
task/play, or single-step through every task - and resume only on a signal
from Tangsible's own TUI. Not a stdin prompt: raw-mode `tcell` already owns
the terminal, the same "interactive input requested after the TUI already
owns the terminal" gap `CLAUDE.md`'s Current scope constraints section
already documents as unsupported.

### Precedent already in ansible-core

Confirmed live in `/usr/lib/python3/dist-packages/ansible/plugins/strategy/`
on this box, not assumed:

- `StrategyBase._take_step` (`linear.py`, the real `--step` CLI flag)
  already blocks synchronously *before* queuing every task, via
  `display.prompt()`, until the user answers y/n/c - single-step mode,
  already built, just wired to stdin.
- The `ENABLE_TASK_DEBUGGER` `cmd.Cmd`-based debugger (`debug_closure`,
  wrapping `StrategyBase._process_pending_results`) blocks *after* a
  result, with `task`/`host`/`task_vars`/`result` in scope, and can
  `continue`/`redo` (rolls back host state, re-queues the same task)/`exit`
  - breakpoint-on-result, already built.

Both prove "a strategy plugin blocks its own execution thread until told to
proceed" is a normal, already-shipped pattern at this layer - the real gap
is *what it blocks on* (stdin, incompatible with the TUI owning the
terminal), not *whether it can block*.

### What would need to be new

1. **A second, bidirectional control channel.** The existing
   `TANGSIBLE_EVENT_FD` (`OwnCallbackPlugin.md`) is one-way,
   child-to-parent only. A debugger needs the reverse direction too:
   Tangsible writes a command (`step`, `continue`, `set-breakpoint
   <task-id>`) that the strategy plugin is blocked reading. Likely a
   second inherited fd via the same `cmd.ExtraFiles` mechanism, rather
   than overloading the event fd for both directions.
2. **A pause point in the strategy plugin**, one of two candidates (both
   already used by ansible-core itself, for the same purpose):
   - Pre-dispatch, right where `task_vars` is computed and before
     `_queue_task` (mirrors `_take_step`'s hook point) - lets a paused
     breakpoint show/edit variable state *before* the task runs.
   - Post-result, inside `_process_pending_results` (mirrors the
     `cmd.Cmd` debugger's hook point) - lets a paused breakpoint inspect
     the outcome before deciding whether to continue.
   Single-step wants the first; a named/conditional breakpoint could use
   either, depending on whether it means "before this task" or "after
   this task."
3. **Breakpoint identification.** Same non-uniqueness problem `Start with
   a play`'s doc already solved for `--start-at-task`: task names repeat
   across a playbook. Breakpoints would need to key on `task.id`/path the
   way `findOrCreateTask` (Aggregation, main `CLAUDE.md`) already does,
   not on task name.
4. **A "paused" TUI state.** The live tree needs a visible paused/waiting
   indicator plus UI affordances for step/continue/toggle-breakpoint - net
   new chrome, not reuse of an existing dialog.

### Known gap

No single clean pause point exists under `strategy: free` (hosts progress
independently - no shared "next task" moment). Same accepted-gap shape as
`StrategyFree.md`'s progress-fill skeleton, and as this doc's own decision
2 (a play with an explicit `strategy:` override skips debug mode
entirely). Not chased further here.

### Relationship to Phase 1

Complementary, not competing: the pause point is exactly where `task_vars`
is freshly computed or freshly merged (see "Why a strategy plugin" above),
so a paused breakpoint can reuse Phase 1's captured-snapshot machinery to
show live variable state for free, once Phase 1 ships.

### Not decided

- Whether this ships as the same `tangsible_debug` plugin (extended) or a
  second, distinct strategy plugin.
- Exact breakpoint UI/config shape - set live from the TUI, a config-file
  list of task paths, or both.
- Whether single-step and named breakpoints share one mechanism or need
  separate code paths.

# Resume

claude --resume ffbd62f0-5860-4b91-a329-4872afd4657e
