# Strategy free

## Status

**Implemented**, on the `strategy-free` branch (built on top of
`own-callback-plugin`, since step 2 depends on the plugin's own
`v2_runner_on_start.host` field). All four steps below are done and
verified, live, against both `testdata/free-strategy.yml` (interleaved
tasks across two hosts, one deliberately slower) and an existing `linear`
fixture (`testdata/multihost.yml`, regression check) - see each step's own
section for what was actually built, which may have refined the original
sketch in small ways (documented inline, not silently). The rest of this
doc is kept as the design record, not rewritten into a pure "how it works"
reference - the "why", including the parts of the original plan that
turned out to need adjusting once the real plugin code was read, is worth
keeping.

## Situation

Tangsible's data model has always assumed Ansible's `linear` strategy: one
task runs across every host before the next task begins, so a single
`currentTask` pointer (set by `v2_playbook_on_task_start`) is enough to
attribute every host result correctly. Under `strategy: free`, hosts
progress independently - Ansible's own `ansible.posix.jsonl` callback never
emits `v2_playbook_on_task_start`/`v2_playbook_on_handler_task_start` for a
non-lockstep strategy at all (confirmed directly against the vendored
callback source, `jsonl.py`'s
`LOCKSTEP_CALLBACKS = frozenset(('linear', 'debug'))`). `currentTask` stays
`nil` for the whole run, so `recordHost` no-ops for every result, and the
tree stays completely empty while the playbook runs and finishes correctly
underneath - a known, long-documented gap (`CLAUDE.md`'s "Current scope
constraints", `README.md`).

This has been discussed before but never written up. This doc is the
write-up: what's actually observable under `free` (verified live against
real `ansible-playbook` output, not assumed), the design decisions already
made, and the smallest fix that makes the tree populate correctly and stay
useful - explicitly *not* a bespoke per-host-progress redesign (see
Decisions below).

## The callback plugin fork (now on `main`, confirmed)

`design-docs/OwnCallbackPlugin.md`'s Phase 1 fork (`callback/
tangsible_jsonl.py`) is merged and live for every real run - confirmed by
reading the actual code, not the design doc's own intentions:

- `internal/runner/process.go`'s `SpawnGeneration` already passes an event
  pipe as the child's fd 3 and sets `TANGSIBLE_EVENT_FD=3` unconditionally -
  every `run`/`rerun`/`role` generation goes through the bundled plugin, not
  stock `ansible.posix.jsonl`.
- `internal/playbook/events.go`'s `RawEvent` already has a `Host string`
  field and `TaskRef` already has `IsHandler bool` - but **not yet an `ID`
  field** (still just `Name`/`Path`/`IsHandler`). Step 1 below still needs
  to add it.
- `internal/playbook/aggregate.go` already has a
  `case "v2_runner_on_start":`, but it was built for Phase 1's own
  motivation (per-host task *timing* under `linear`, `design-docs/
  PerHostTaskTiming.md`), not for `free` support, and its own doc comment
  says so explicitly: *"Only ever arrives from this app's own bundled
  callback plugin fork... a strategy other than linear/debug that our fork
  doesn't cover either, simply never produces this case."* That line is
  slightly stale (see below) but the code itself is unambiguous:
  ```go
  case "v2_runner_on_start":
      if s.currentTask != nil && ev.Host != "" {
          s.currentTask.Started[ev.Host] = ev.Timestamp()
      }
  ```
  This still trusts `s.currentTask`, which stays `nil` all run under `free`
  (task-start events never fire there) - so today, under `free`, this case
  is a guaranteed no-op, same as every other case. **`free` is still
  completely unimplemented on this branch** - the plugin work was scoped
  to linear-only timing (`OwnCallbackPlugin.md`'s own roadblock #10: *"
  `strategy: free` still unsupported by the aggregate model - unchanged;
  but our plugin means a future `free` effort is no longer also a
  dependency change"*).

**What the plugin actually emits under `free`** (read directly from
`callback/tangsible_jsonl.py`, not inferred): `v2_runner_on_start`'s
non-lockstep branch builds a *full* `task_result` (same shape as a
task-start event - `task.id`/`name`/`path`/`duration.start`) and adds a
top-level `"host": host.get_name()` to it - this is new, not something
stock `jsonl.py` did (stock jsonl's `_new_task` always left `hosts: {}` and
never recorded which host the event was for; the fork's whole point, per
`OwnCallbackPlugin.md` decision 4, was adding that `host` field). Under
lockstep, the same event fires too (also new vs. stock jsonl, which
suppressed it under `linear`/`debug` outright), but with a *minimal*
payload: `{"task": {"id": ...}, "host": ..., "_event": ...,
"_timestamp": ...}` - no `name`/`path`, since the task itself was already
created by the task-start event moments earlier.

**Net effect for this plan**: `v2_runner_on_start` is now a real "host X
just started task Y" signal, under *both* strategies, carrying full task
identity under `free` and enough identity (`task.id`) under lockstep to
correlate back to the node the task-start event already created. This is
exactly the enabler the plan's earlier speculative section below asked
about - it's confirmed true now, not hypothetical - and it upgrades step 2
from an approximation into exact tracking. See "The fix" for the concrete
design; the speculative section immediately below is kept for its reasoning
trail but its open question is now answered.

## What's actually true under `free` (verified live against stock `ansible.posix.jsonl`)

This section describes the stock-jsonl baseline as originally verified -
still the accurate picture for a *pre-fork* run log (an old `.jsonl` saved
before the plugin fork landed) and for the shape step 1 below is built to
handle regardless of plugin. See "The callback plugin fork" above for what
our own bundled plugin changes on top of this.

- `v2_playbook_on_task_start`/`_handler_task_start`: **never fire.**
- `v2_runner_on_start` (stock jsonl): fires once per (host, task), but its
  own `hosts` field is **always empty** -
  `{"_event": "v2_runner_on_start", "hosts": {}, "task": {...}}`, and there
  is no top-level `host` field either. There is no event that means "host X
  has just started task Y" against stock jsonl. Our own plugin fixes exactly
  this (see above) - this bullet is what motivated that fix in the first
  place.
- **The terminal per-host events already carry everything needed**:
  `v2_runner_on_ok`/`_skipped`/`_failed`/`_unreachable` each carry a full
  `task` object - `{"id": "<uuid>", "name": "...", "path": "<file>:<line>",
  "duration": {...}}` - **identical in shape whether the play is `linear`
  or `free`**, confirmed by capturing both directly. `task.id` is stable
  across every host running that task. This means task identification
  doesn't need to special-case `free` at all: a terminal event can
  find-or-create its own `TaskNode` from its own payload, regardless of
  whether a task-start event for it ever fired.
- Tasks genuinely interleave (one host on task 3 while another is still on
  task 1) - not a corner case, the normal shape of `free`.
- Handlers behave identically to regular tasks in the event stream (also
  just `_on_start`/`_on_ok`, no handler-specific event) - no special
  handling needed beyond what the unified fix already does, since
  handler-vs-task classification already comes from the static source
  index (`internal/source`), not the event stream.
- `strategy:` is a **per-play** YAML key - a single playbook can mix
  `linear` and `free` plays. Nothing should assume "this whole run is one
  strategy."
- `v2_playbook_on_play_start` is unaffected by strategy and fires normally
  either way.
- **Ansible itself, not tangsible, refuses to run some modules at all
  under `free`** - a real user report, confirmed independent of tangsible
  entirely by reading `ansible/plugins/strategy/free.py` directly and
  reproducing with a plain `ansible-playbook` invocation, no tangsible
  involved: any module whose action plugin sets `BYPASS_HOST_LOOP = True`
  (on a stock install, just `ansible.builtin.pause` and
  `ansible.builtin.add_host` - checked directly, not assumed) raises a
  hard `AnsibleError` and aborts the whole run the moment `free`'s own
  strategy plugin reaches it, for any host, anywhere in the playbook -
  handlers included. These modules don't run per-host at all (that's the
  whole point of `pause`'s single blocking prompt/timer); `free`'s
  execution model has no way to run them safely, so it refuses outright
  rather than doing something surprising. **Nothing tangsible can or
  should work around** - this is ansible-core's own safety check, not a
  data/rendering gap. The fix is playbook-side: swap `pause: seconds: N`
  for `wait_for: timeout: N` (a plain per-host delay, `free`-safe), or
  keep that specific play/handler on `strategy: linear` (`strategy:` is a
  per-play key, so mixing is fine).

## Decisions already made

1. **Top bar / "active task"**: show the earliest task (in first-seen
   order) that's currently incomplete. Originally decided as an
   approximation (recorded-host-count vs. `AllHosts`) before the plugin fork
   was available to build against; now implementable exactly, via step 2's
   `inFlight` tracking sourced from the plugin's own `v2_runner_on_start`
   signal - see "The callback plugin fork" above. (Extended below:
   filter-visibility's existing "never hide an in-progress task" protection
   should apply to *every* currently-incomplete task, not just the earliest
   one - same flicker-prevention rationale the current single-task version
   already has, just generalized to however many tasks are simultaneously
   incomplete. Flagging this extension clearly since it wasn't asked
   verbatim, but it's a direct consequence of the same principle.)
2. **Tree population**: grow task-by-task as revealed by the event stream,
   not pre-populated from an upfront probe. Matches how linear already
   works; accepts that task order in the tree can depend on which host
   reaches a task first.
3. **Scope**: a correctness fix for the existing `Play → Task → Host` tree
   (reuse current UI/filtering/drill-down as-is), not a purpose-built
   per-host view.

## If the custom plugin's event shape changes (historical reasoning; now resolved)

Kept for the reasoning trail that led to the confirmed section above - the
question this section originally posed ("would plugin control make a
better fix possible?") is now answered (yes, for step 2; no change to step
1), not still open. Two genuinely separate questions - don't conflate them:

**Does plugin control make the core fix (below, step 1) smaller?** No.
`Apply` currently trusts one shared `currentTask` pointer, which is wrong
the moment more than one task can be in flight - that's a Go-side
attribution bug, not a data-poverty problem. Every terminal event already
carries its own `task.id`/`path` (confirmed identical under `linear` and
`free`), so resolving each event's task from its own payload instead of the
shared pointer is already the simplest available fix, and it's needed
regardless of what the plugin does or doesn't emit. **Step 1 should happen
exactly as written below no matter what happens with the plugin fork.**

**Does plugin control make a *better* fix possible for step 2 (the
active-task/spinner approximation)?** Yes, potentially. The reason "earliest
incomplete task" is an approximation at all is that `v2_runner_on_start` -
the one event that fires before a host finishes a task - carries no host
identity (`"hosts": {}`, confirmed empirically). That's the *stock*
plugin's own choice not to serialize the `host` argument its Python
callback method already receives (`v2_runner_on_start(self, host, task)` -
the host object is right there in scope, just not written out). If the
in-house fork populates that field (a small, contained, plugin-side change
- no protocol redesign, just serialize one already-available argument),
tangsible gains a real "host X has started task Y" signal it cannot have
today against the stock plugin. That would upgrade step 2 from an
approximation (compare recorded-host-count against `AllHosts`) into exact
per-host "currently running" tracking - a better design, genuinely more
work than the plan below (new Go-side state: real in-flight per-host-task
pairs, not just a derived incomplete-count), not less.

**Progress tracking (step 3) is the one place a richer signal could make
things *simpler*, not just better.** The current windowed-lookahead
matching algorithm in `progress.go` exists specifically to infer progress
from a stream of *anonymous* completions (it doesn't know which host is on
which task, only that *a* task named X just finished). If the plugin
provided real per-host start/finish pairs, progress could become a plain
counter - completed `(host, task)` pairs over a predicted total - instead
of the cursor/lookahead-window machinery that exists today. Worth keeping
in mind as a possible follow-on simplification, independent of whether it's
pursued alongside `free` support specifically or later on its own merits
(the windowed-matching algorithm's complexity isn't `free`-specific to
begin with - it exists because of duplicate task names under `linear` too).

**Confirmed (2026-09-22), no longer open:** yes, the in-house fork already
touches `v2_runner_on_start`/host attribution - it adds the `host` field
under both lockstep and non-lockstep, exactly the change this section
speculated might be worth making. See "The callback plugin fork" above for
what was actually read off the merged branch.

## The fix

Step 1 is plugin-independent by design (it would be the right fix even
against stock jsonl); step 2 now builds directly on the confirmed plugin
behavior above rather than the old approximation - both are described
against the actual merged code, not speculatively.

### 1. Core data model (`internal/playbook/aggregate.go`, `events.go`) - do this first, independently valuable/testable on its own

**`TaskRef`** (`events.go:58-65`) gains an `ID string \`json:"id"\`` field -
the JSON already carries it (`json.Unmarshal` silently drops unrecognized
fields today, confirmed by capturing a raw terminal event directly), so
this is a one-line addition, no decode restructuring.

**`PlaybookState`** (`aggregate.go`) gains an unexported
`tasksByID map[string]*TaskNode`, cleared in `Reset()` alongside the
existing `currentPlay`/`currentTask` clears.

New unexported helper, replacing the inline allocation currently in the
`v2_playbook_on_task_start`/`_handler_task_start` case (`aggregate.go:239-261`):
```go
func (s *PlaybookState) findOrCreateTask(ref *TaskRef) *TaskNode {
    if s.currentPlay == nil {
        s.currentPlay = &PlayNode{Name: s.pendingPlayName}
        s.Plays = append(s.Plays, s.currentPlay)
        if s.OnPlayAdded != nil {
            s.OnPlayAdded(s.currentPlay)
        }
    }
    if t, ok := s.tasksByID[ref.ID]; ok {
        return t
    }
    t := &TaskNode{Name: ref.Name, Path: ref.Path, Hosts: map[string]Outcome{}, Raw: map[string]json.RawMessage{}, Warnings: map[string]bool{}, HasStderr: map[string]bool{}}
    s.currentPlay.Tasks = append(s.currentPlay.Tasks, t)
    s.tasksByID[ref.ID] = t
    if s.OnTaskAdded != nil {
        s.OnTaskAdded(s.currentPlay, t)
    }
    return t
}
```
(`StartedAt` needs `ev.Timestamp()`, which isn't available inside this
helper as sketched - pass `ev RawEvent` in instead of just `ref *TaskRef`,
or pass the timestamp separately; settle this mechanically during
implementation, not a design question.)

**A real wrinkle, now that the actual payload shapes are known:** under
lockstep, `v2_runner_on_start`'s `task` object is minimal - `{"id": ...}`
only, no `name`/`path` (confirmed against `tangsible_jsonl.py`'s own
lockstep branch). If `findOrCreateTask` is ever called for that event (step
2 below needs to, to update in-flight state), the existing-node lookup
(`if t, ok := s.tasksByID[ref.ID]; ok { return t }`) must run first and
return early *without* touching `Name`/`Path` - which the sketch above
already does. This is safe in practice because a lockstep task's
`v2_playbook_on_task_start` always creates the node before that task's own
per-host `v2_runner_on_start` events arrive (dispatch always follows
task-start), so the minimal-shape branch of `findOrCreateTask` should never
actually be reached for a lockstep event - but verify this ordering live
before relying on it, the same "confirmed, not assumed" discipline the rest
of this doc follows.

- **Task-start case** (`aggregate.go:239-261`): becomes
  `s.currentTask = s.findOrCreateTask(ev)` (keeps `currentTask` for now -
  see step 2 for its removal). Behaves identically to today under linear,
  since `ref.ID` is always new the first time task-start fires for a task.
- **Terminal event cases** (`aggregate.go:263-286`, currently four
  near-identical loops calling `s.recordHost(host, outcome, raw)`): resolve
  the task **once per event** (not per host - a single terminal event's
  `Hosts` map can list several hosts finishing the same task at once) via
  `task := s.findOrCreateTask(ev)`, then pass that resolved node into the
  recording call instead of `recordHost` trusting `s.currentTask`
  internally. `recordHost`'s signature changes from `(host, o, raw)` to
  take the resolved `*TaskNode` explicitly (or `recordHost` itself takes
  `ev` and resolves once - either shape is fine, just don't resolve
  per-host inside the existing `for host, raw := range ev.Hosts` loops).

This is the whole fix for "does data go in the right place." It requires no
knowledge of which strategy is in play - it's strictly more correct than
trusting a single shared pointer, for both `linear` and `free`, since it
now sources task identity from the event that's actually reporting the
result rather than from whatever happened to be "current" when it arrives.

**A real bug found after shipping the above, via a live user report:
`task.id` alone isn't actually stable across hosts for a *dynamically
included* task (`include_tasks`/`include_role`) under `strategy: free`.**
Confirmed live by capturing the raw plugin output directly: two hosts
hitting the exact same `included.yml:1` task ended up with two different
`task.id` values - Ansible only merges hosts into one shared Task object
for an identical include under `linear`'s own lockstep batching; `free`'s
per-host independence means that merging never happens, so each host gets
its own freshly-parsed Task object (and so its own UUID) for anything
reached via a dynamic include. `findOrCreateTask`'s pure id-based lookup
showed this as one row per host instead of one shared row - exactly the
kind of misattribution step 1 was supposed to eliminate, just from a
different cause than the one originally identified.

The task's own source `path` (`<file>:<line>`) *is* still identical across
hosts in this case - two distinct tasks in a real playbook never share a
path - so path is what still identifies "the same task" once `id` can't
be trusted alone. Path-only matching isn't safe by itself, though (also
confirmed live): a `loop:` around an `include_tasks` produces one real
task-start-shaped event *per iteration*, every one sharing the exact same
included file's path - those genuinely are separate tasks and must stay
separate rows, not collapse into one. What actually distinguishes "the
same dispatch, split across hosts" from "a later, distinct loop iteration
at the same path" is host membership: a given host can only ever belong
to *one* occurrence of a path at a time. `PlayNode.tasksByPath` (new,
unexported, scoped per play - not run-wide, since two unrelated plays
including the same file would otherwise collide on one shared path key)
keeps every `TaskNode` ever created for a given path, in creation order;
`findOrCreateTask`'s fallback (tried only when the id-only lookup misses)
walks them and claims the first one this host hasn't already touched
(`TaskNode.hasHost`, checking both `Hosts` and `Started`) - if every
existing occurrence already has this host, it's a new iteration, so a
fresh node is created instead. Verified live for both: a plain
per-host-diverging include now correctly merges into one row; a `loop:`
around one still correctly produces one row per iteration, each merged
correctly across hosts. The fallback never engages when `path` is empty
(a synthetic/internal task with no real source location) - there's no
safe signal to match on then, so each diverging id simply stays its own
node, same "don't guess" posture as everywhere else in this file.

**New tests** (`internal/playbook/aggregate_test.go`, matching the file's
existing per-scenario `Apply`-sequence style, not table-driven): a
regression test that never sends a task-start event and instead sends
terminal events for two tasks interleaved across hosts (mirroring the live
capture above) - assert each host lands in the correct `TaskNode`, not
misattributed to whichever task a shared pointer last named. Also confirm
idempotency: the same `task.id` arriving via task-start *and* later via a
terminal event (linear case) must not create two nodes; the same `task.id`
arriving via two different hosts' terminal events with no task-start at all
(pure free case) must also not create two nodes.

Plus, for the path-based fallback specifically:
`TestApply_FreeStrategy_IncludedTaskMergesAcrossHostsDespiteDifferentIDs`
(the actual reported bug, reproduced directly), `TestApply_FreeStrategy_
LoopedIncludeKeepsIterationsSeparate` (the companion "must not
over-merge" case - two hosts progressing through two loop iterations at
different paces, confirming ordering alone can't confuse the matching),
`TestApply_PathBasedFallbackNeverAppliesWithNoPath` (the empty-path
guard), and `TestApply_PathFallbackScopedPerPlay` (two different plays
including the same file must not merge across the play boundary).

### 2. "Active task(s)" - replaces `currentTask`/`CurrentTask()`

**Upgraded from the original approximation now that the plugin's
`v2_runner_on_start.host` field is confirmed real** (see "The callback
plugin fork" above): rather than approximating "incomplete" from a
recorded-host-count vs. `AllHosts` comparison, track exactly which
`(task, host)` pairs are currently dispatched-but-not-yet-reported, sourced
directly from real start/finish events - true under both strategies, not
just an approximation that happens to be exact for `linear`.

**`TaskNode`** gains an unexported `inFlight map[string]bool` (parallel to
`Started`/`Finished`, same per-host-map shape already established there).

- **New `case "v2_runner_on_start":`** (replacing the existing one that
  only updates `Started` against `s.currentTask` - fold both into one,
  since both need the same resolved node): `task := s.findOrCreateTask(ev)`,
  then if `ev.Host != ""`: `task.Started[ev.Host] = ev.Timestamp()` (today's
  existing behavior, now correctly attributed via `task.id` instead of
  `s.currentTask`) *and* `task.inFlight[ev.Host] = true`.
- **Terminal event cases** (`recordHost`): after recording the outcome,
  `delete(task.inFlight, host)`.
- New method on `PlaybookState`:
  ```go
  // IncompleteTasks returns every task, across all plays, in first-seen
  // order, that currently has at least one host recorded as in-flight
  // (dispatched via v2_runner_on_start but not yet reported a terminal
  // outcome) - exact under both strategies when the run's events came
  // from our own bundled plugin (design-docs/OwnCallbackPlugin.md), which
  // is every live run today. A pre-fork replayed run log (revisit/diff)
  // never has a v2_runner_on_start event at all, so inFlight never gets
  // populated for one - harmless, since IncompleteTasks is only ever
  // consulted for a still-running generation (processDone gates every
  // call site), and a replayed log is frozen (processDone pre-true, see
  // Revisit above) before this is ever asked.
  func (s *PlaybookState) IncompleteTasks() []*TaskNode
  ```
  Implementation: walk `s.Plays`/`Tasks` in order, collect any task whose
  `inFlight` is non-empty. A plain scan, not a maintained index - same
  "simple beats clever at ~10-host scale" reasoning `hostsWithOutcome`
  already uses elsewhere in this file.
- Remove `currentTask` field, `CurrentTask()` method, and their use in
  `recordHost`/task-start (superseded by step 1's
  `findOrCreateTask`/`tasksByID` for task identity, and by `inFlight` above
  for "is this task still active"). Retire/rewrite
  `TestCurrentTask_StaysActiveUntilNextTaskStarts` (`aggregate_test.go`) -
  it directly asserts the semantics being replaced.
- `Reset()` needs no new clearing code beyond what already clears
  `tasksByID` (step 1) - `inFlight` lives on each `TaskNode`, which is
  already thrown away wholesale by `s.Plays = nil`.

**Built as `internal/session/livesession_rebuild.go`'s `activeTasks()`**
(renamed from `activeTaskNow()`, replacing its `return
s.state.CurrentTask()` body with `uikit.TaskSet(s.state.IncompleteTasks())`
- `TaskSet` already existed, reused rather than duplicated) - simpler than
originally sketched here: there turned out to be no separate "top bar
names the active task" text anywhere in the real code to feed a "headline"
task into (the top bar's own composition is just spinner + elapsed +
progress fill, per `CLAUDE.md`'s own TUI section) - every real call site
(`FlattenRows`'s per-row spinner, the selected-row re-render, filter
visibility) only ever needed set membership, not a single distinguished
task. So `activeTasks()` returns exactly one thing: the membership set.

**Every current single-pointer call site needs its own signature to move
from `activeTask *playbook.TaskNode` + `t == activeTask` to
`activeTasks map[*playbook.TaskNode]bool` + `activeTasks[t]`** (full
inventory already confirmed, not guessed):
- `internal/uikit/tui_filter.go`: `VisibleTasks`, `VisibleTasksForHost`
  (both currently take one `activeTask`, do `t == activeTask` internally,
  feed `TaskVisible`'s `isActive bool`).
- `internal/uikit/tui_rows.go`: `FlattenRows` (`t == activeTask` for
  visibility *and* for `TaskLabel`'s spinner-prefix rendering - with the
  new set, every incomplete task's row can show the "still filling in"
  indicator, not just one, which is more honest for `free` and harmless
  under `linear` where the set only ever has one member).
- `internal/session/livesession_filter.go`'s `applyFilter` (two direct
  `TaskVisible`/`==` call sites).
- `internal/session/livesession_rebuild.go`'s `rebuild()` (two more:
  `FlattenRows`'s own call, and the selected-row re-render's
  `id == activeTask`).
- `internal/session/livesession_nav.go`'s `navigateMainTask` and
  `internal/session/livesession_output.go`'s `navigateOutputTask` -
  mechanical pass-throughs, no logic of their own beyond forwarding
  whatever `activeTaskNow()`-equivalent now returns.
- **Provably unaffected, no change needed**: `TaskVisible`'s own internals
  (still just `bool isActive`), the frozen-run auto-jump's hardcoded
  `isActive: false` (`rebuild.go:345,354` - a finished run has no
  incomplete task by definition, true under either strategy), and
  `onTaskAdded`/`InheritedExpandState` (`livesession_events.go`,
  `tui_filter.go`) - both are agnostic to host attribution and only depend
  on "one `TaskNode` + one `OnTaskAdded` firing per distinct task, in
  first-seen order," which step 1's `findOrCreateTask` already guarantees.

### 3. Progress tracker (`internal/runner/progress.go`) - disable for `free` plays, don't try to adapt

Confirmed directly (reading `ParseListTasksOutput`'s matching algorithm,
and the vendored `jsonl.py`): today, `Advance` is simply never called
during a `free` play (nothing wires `v2_runner_on_start` to it), so nothing
is corrupted today - the fill just freezes at that play's start and jumps
at the next play boundary, the same graceful degradation already accepted
for a long dynamic-include gap. The risk is only in a *naive* future fix
that feeds `free`'s per-host events into `Advance` 1:1 - that would violate
the tracker's "never overcount" contract whenever a task name repeats (a
real, already-documented case). Don't do that.

Instead: statically detect a play's `strategy: free` and exclude its tasks
from the skeleton entirely, the same way a zero-host play already is.
Concretely:
- Extend `internal/source/source.go`'s `parseTopLevelPlays`/`topLevelPlay`
  (`source.go:148-181`, already walks each top-level play's YAML mapping
  via the existing `mappingValue(item, "name")` helper) to also capture
  `mappingValue(item, "strategy")`'s scalar value, and expose a small set
  of free-strategy play names (mirroring `ListTopLevelPlayNames`'s own
  existing shape).
- In `internal/runner/progress.go`'s
  `ParseListTasksOutput`/`BuildProgressSkeleton`, filter out any
  `ProgressEntry` whose `Play` name is in that set - same mechanism already
  used for `skipCurrentPlay` (zero-host plays), just a second reason to
  skip.
- No change needed to `ProgressTracker.Advance`/`AdvanceToPlay` themselves
  - `AdvanceToPlay` already unconditionally fires on every real play-start
  event and already handles "nothing in the skeleton for this play"
  gracefully.
- **Accepted, documented gap** (matching this project's existing
  "heuristic, not chased further" convention elsewhere): a
  globally-configured `strategy = free` (`ansible.cfg`'s `[defaults]`,
  `ANSIBLE_STRATEGY` env var, no per-play `strategy:` key in the YAML)
  won't be caught by this static scan - that play's progress fill would
  behave exactly like today's unfixed state (freeze, imprecise), not a
  regression, just not solved by this plan.

### 4. Test fixtures, docs, verification

- New `testdata/free-strategy.yml` + inventory (2-3 hosts,
  `ansible_connection=local`, `strategy: free`, 2-3 tasks with deliberately
  different per-host timing so interleaving is naturally observable - e.g.
  varying `sleep` durations, matching `testdata/hostnames.yml`'s own
  existing "observable timing" precedent).
- New unit tests: `internal/playbook/aggregate_test.go` (step 1, above),
  `internal/runner/progress_test.go` (free-strategy play excluded from the
  skeleton), `internal/source/source_test.go` (the new strategy-scan
  helper).
- Manual/tmux verification, both `linear` (regression - confirm nothing
  broke) and `free`: tree populates correctly with interleaved tasks,
  filtering (Failed/Changed/Interesting/search) doesn't flicker on an
  in-progress task, `n`/`N` navigation, the top-bar name tracks the
  earliest genuinely in-flight task (step 2's exact tracking, not an
  approximation) and the spinner/progress fill behaves sensibly
  (frozen-but-not-wrong under `free`, accurate under `linear`), multiple
  simultaneously-incomplete tasks all show their own spinner under `free`
  specifically (the case `linear` structurally can't exercise, since it
  never has more than one in-flight task), rerun and diff mode still work
  against a `free`-strategy run (should be unaffected - both operate at the
  play/event-replay level, not per-task lockstep), two-paned drill-down
  still live-syncs correctly, and a *revisited* pre-fork run log (no
  `v2_runner_on_start` at all) still opens and renders correctly frozen
  (confirms the `IncompleteTasks`-empty fallback path is harmless, per its
  own doc comment in step 2).
- Update `CLAUDE.md`'s "Current scope constraints" (remove/rewrite the
  `strategy: free` bullet) and `README.md`'s "Current limitations" section.
  Add a `Changelog.md` `[Unreleased]` entry.

## Verification (every increment, this project's established discipline)

`go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` after each
of the four steps above, plus a manual tmux smoke run at the end of steps
1-2 together (data model + active-task UI) and again after step 3 (progress
tracker) - both against the new `free`-strategy fixture and against an
existing `linear` fixture (e.g. `testdata/multihost.yml`) to confirm no
regression. `go test -tags e2e ./...` at the end, since nothing here should
affect the rerun-dialog/mouse-click surface those tests cover, but it's
cheap insurance given how much of `internal/uikit`'s filter/row-rendering
signatures are touched in step 2.

## Sizing

Four increments, each independently valuable and a safe stopping point,
similar in spirit to the `NewLiveTUI` refactor's own phasing: (1) core
attribution fix - the single most important piece, makes the tree correct
instead of blank; (2) active-task/filter-visibility generalization - UI
polish on top of (1); (3) progress tracker - cosmetic, lowest risk, most
isolated; (4) fixtures/docs/verification. (1) alone is a complete, real
improvement even if (2)-(4) slip to a later session.

## Critical files

- `internal/playbook/aggregate.go`, `events.go` - the core fix (step 1) and
  its test file. Both already carry real, merged plugin-support code
  (`RawEvent.Host`, `TaskNode.Started`/`Finished`, an existing
  `case "v2_runner_on_start":`) that this plan builds on and partly
  replaces (see "The callback plugin fork" above) rather than working
  against a clean slate - read the current file before touching it, don't
  assume it still matches this doc's original pre-fork sketch verbatim.
- `internal/session/livesession_rebuild.go` (`activeTaskNow`, `rebuild`),
  `livesession_filter.go` (`applyFilter`), `livesession_nav.go`,
  `livesession_output.go` - active-task signature changes (step 2)
- `internal/uikit/tui_filter.go` (`VisibleTasks`, `VisibleTasksForHost`,
  `TaskVisible`), `tui_rows.go` (`FlattenRows`) - same (step 2)
- `internal/runner/progress.go`, `internal/source/source.go` -
  progress-skip detection (step 3)
- `CLAUDE.md`, `README.md`, `Changelog.md` - docs (step 4)
