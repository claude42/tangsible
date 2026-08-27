# Progress indicator: drop the numbers, keep the bar

## Situation

The top bar's "Task x/y" indicator (`internal/runner/progress.go`) predicts
a task sequence ahead of time from a throwaway `ansible-playbook
--list-tasks --list-hosts` invocation, then matches real task-start events
against it live. That prediction is structurally incomplete - documented
in `progress.go`'s own header comment before this decision, not a surprise
found here - because `--list-tasks` can't see:

* handlers (never listed at all, even one that will genuinely fire);
* a looped `include_tasks:`/`import_tasks:` (listed as one opaque line,
  regardless of how many real iterations actually run).

For a small, thin-task-per-role playbook this rarely matters much. For a
real, role-heavy playbook it can be dramatic - confirmed live: a two-task
loop over a small file list alone produced "Task 1/1" for what turned out
to be 7 real executed tasks, and a real-world report showed "Task 52/52"
against a genuinely-completed "115 tasks" in the end-of-run recap.

The recap's own count (`internal/session/recap.go`'s "Completed N tasks on
M reachable hosts...") has no such blind spot - it's a plain tally of every
real task-start event that actually fired, nothing predicted. So the two
numbers can legitimately disagree, sometimes by a lot, and there's no
practical way to make the *upfront* prediction exact: a loop's own
iteration count, and whether a task will actually notify a handler, aren't
knowable without genuinely simulating the run.

## Decision

Rather than keep chasing the upfront number's accuracy (impossible in
general, only ever reducible to "fewer known blind spots"), drop the
number entirely. The top bar (and split mode's own header) keep the
proportional fill effect - background color sweeping left to right as the
run progresses, still driven by the exact same `progressPos`/
`progressTotal` from `ProgressTracker` - but no longer print "Task x/y" as
literal text next to it. The fill communicates "roughly how far along
this is" without asserting a number that can't be trusted to be accurate.
The one number this app ever asserts as truth - the recap's own
"Completed N tasks" - is unaffected: still computed the same way, still
shown once the run is frozen, still exact.

Two different numbers on screen for "how many tasks" invites exactly the
confusion a live report surfaced; showing only the one that's actually
correct, plus a wordless progress cue for the one that can't be made
correct, resolves that without pretending the underlying prediction
problem is solved.

## Implementation

`ComposeTopBarLine`/`ComposeSplitHeaderLine` (`internal/uikit/
tui_layout.go`) drop their own `"Task %d/%d  "` formatting entirely - no
longer need `progressPos`/`progressTotal` as parameters at all, since
nothing in either function reads them anymore. The fill itself is
untouched: `TopBarText`/the split header's own `ProgressFillLine` call
still receive `progressPos`/`progressTotal` directly (from
`ProgressTracker.Position()`, wired through `rebuild()`'s own
`progressPosition` closure exactly as before) and still compute the
proportional sweep exactly as they always have - the fill was already a
separate wrapping step around the composed line's own plain text, not
derived from that text, so removing the text doesn't touch the fill at
all.

`showElapsed == false` (a revisit session, design-docs/Revisit.md) used to
still show a bare "Task x/y" with no clock alongside it; now shows nothing
at all in that slot, which is simpler and consistent - a revisit session
never had a real skeleton to predict from in the first place (replay never
builds one), so there was never anything genuine to show there anyway.

`ProgressTracker`/`BuildProgressSkeleton`/`ParseListTasksOutput`
(`internal/runner/progress.go`) are otherwise unchanged - the fill still
needs `progressPos`/`progressTotal`, so the underlying prediction
machinery stays exactly as it was, just no longer surfaced as a literal
number a user could compare against the recap's own.
