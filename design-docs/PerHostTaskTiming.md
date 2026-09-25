# Per-host task timing

## Idea

Show, per host, how long a task took — next to the existing OK/Changed/
Skipped/Failed detail on the *expanded* host row (`hostLabel` in `tui.go`,
e.g. `web1: OK (echo hi)` growing a duration into that same parenthetical).
Motivation: a dev-focused tool benefits from being able to spot a
slow/flaky host during iteration. `taskNode.StartedAt` already exists in
`aggregate.go` and is captured "because Ansible provides it for free," but
was unused until this discussion.

Deliberately scoped to the *expanded per-host row only*, not the collapsed
task row's hostname list — that list is already under real horizontal
pressure (see `computeHostColumnLayout`'s whole shrink cascade), and a
per-task aggregate duration would cost space there for comparatively little
value. The expanded row has no such width constraint (per `tui.go`'s own
note: "no width-based truncation applies there").

## Investigation: is there a per-host *start* event?

Checked whether `v2_runner_on_start` (which fires once per host, right as
that host begins a task) is available under `linear` strategy - the only
strategy this app is designed around.

**It is not.** Confirmed two ways:

- Source of the vendored `ansible.posix.jsonl` callback
  (`/usr/lib/python3/dist-packages/ansible_collections/ansible/posix/plugins/callback/jsonl.py`):
  `v2_runner_on_start` opens with `if self._is_lockstep: return`, and
  `_is_lockstep` is set from `play.strategy in LOCKSTEP_CALLBACKS` -
  `linear` (and similar strategies) are lockstep; only non-lockstep
  strategies like `free` actually get this event. (`v2_playbook_on_task_start`
  is the mirror image: it's a no-op unless lockstep.) This is the same
  `free`-vs-`linear` event-shape split already documented in `CLAUDE.md`'s
  "`strategy: free` shows nothing at all" section, just the other event
  half of it.
- Empirically: ran `testdata/multihost.yml` and `testdata/hostnames.yml`
  through the real `ansible-playbook` with
  `ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl` under default (`linear`)
  strategy and inspected the raw jsonl. Zero `v2_runner_on_start` events in
  either run - only one `v2_playbook_on_task_start` per task (no host info)
  and one `v2_runner_on_*` outcome event per host.

So under `linear`, the only per-host timestamp available at all is each
host's own *finish* time (the outcome event's `_timestamp`). A per-host
duration would have to be approximated as:

```
duration ≈ (host's own outcome-event timestamp) − (task's own StartedAt)
```

This needs no new event wiring beyond what already exists - just recording
each host's finish timestamp alongside `Hosts`/`Raw` in `taskNode` (e.g. a
new `Finished map[string]time.Time`, set in `record` from a timestamp
threaded through `recordHost`), and computing the delta at render time in
`tui.go` (kept out of `aggregate.go`, consistent with that file's existing
"stays free of any formatting logic" convention).

## The problem: this delta doesn't mean what it looks like it means

`ansible-playbook`'s `linear` strategy queues every host for a task at
once; each of `--forks` worker slots pulls the next queued host as it frees
up. So "time since the task started" for a given host conflates two very
different things:

- genuine execution time, if that host started immediately (its slot was
  free from the start), vs.
- execution time *plus* however long it sat queued waiting for a worker
  slot, if it didn't.

Verified this live with `testdata/hostnames.yml` (three local hosts, 1s
sleep per task):

- **Default (parallel) forks**, 3 hosts: all three finished within ~15ms of
  each other, ~1.3s after task start. The delta tracks real execution time
  well here - no queueing occurred, since forks (default 5) ≥ host count (3).
- **`--forks 1`** (serialized): web1 finished at task-start+1.29s,
  database-primary at +2.48s, cache-node-alpha at +3.66s - clearly stacking
  queue-wait on top of real per-host runtime, not reflecting it.

Worse: there's no way to tell these two cases apart from the data alone.
Under `linear`, which hosts land in "wave 1" (started immediately) vs.
"wave 2+" (queued) depends on *how long wave 1 takes to finish* - but
knowing which wave a host is in is exactly what you'd need to correctly
interpret its own elapsed time. It's circular: you can't separate "this
host was slow" from "this host was queued" using completion timestamps
alone, for any task where `forks < hosts-for-that-task`. And there's no
event that resolves the ambiguity directly - `v2_runner_on_start` would,
but it isn't emitted under `linear` (see above), and implementing a custom
callback plugin to get it is explicitly outside this project's architecture
(`CLAUDE.md`'s Data source section - Tangsible deliberately never talks to
Ansible's Python API or ships its own callback plugin).

## Options considered

1. **Drop it.** The ambiguity undermines the actual use case (spotting a
   genuinely slow/flaky host) - a number that's sometimes real elapsed time
   and sometimes just "this host was Nth in the queue" is worse than no
   number, since it actively risks misattributing slowness to the wrong
   host.
2. **Only show it when it's provably unambiguous** - i.e. gate display on
   `forks >= hosts-for-this-task`, so no queueing could possibly have
   happened for that task. Always trustworthy when shown, but silently
   absent whenever a task has more hosts than `--forks` (default forks is
   5) - which is common exactly in the runs where a straggler would be most
   worth spotting. Would also need `--forks`/`-f` parsed out of the
   invocation's passthrough args (not currently done anywhere; the closest
   precedent is `parsePassthroughArgs`' existing `--tags`/`--limit`
   extraction) to know the effective fork count, plus the current task's
   own host count from `taskNode.Hosts`.
3. A silently-sometimes-appearing feature (option 2) was judged to be its
   own kind of confusing on top of the narrow window it'd actually cover,
   which is why the discussion leaned toward option 1 without landing
   firmly on it.

## Status

**Implemented (2026-09-18)**, via `design-docs/OwnCallbackPlugin.md`'s
bundled callback plugin fork rather than the gated-display option (2)
below. The fork makes `v2_runner_on_start` obtainable under `linear` too
(see that doc's "What was verified" section) - the queueing ambiguity this
whole doc is about no longer applies, since a host's *own* dispatch
timestamp is now available directly, instead of only the shared task-start
timestamp every host would otherwise have to share. Verified live against
the exact scenario "Verified this live" above (4 hosts, `--forks 1`
equivalent): all hosts now correctly show their own real execution time
regardless of which wave they landed in.

The shipped shape differs from the sketch originally below in a few ways,
worth knowing if this doc is consulted again:

- Duration is computed from `Started`/`Finished` (both populated from real
  events), not `Finished` alone measured against the task's shared
  `StartedAt` - see `aggregate.go`'s `TaskNode.Started`/`.Finished` and
  `uikit.HostDuration`.
- `FormatDuration` always renders seconds with one decimal place (e.g.
  `"5.3s"`), not the three-tier ms/seconds/minutes+seconds format
  originally sketched - live use found that switching units made
  different hosts' durations land in different unit systems depending on
  which side of a threshold they fell on, defeating the actual point of
  showing this at all (comparing hosts at a glance).
- Duration renders as a column-aligned prefix (`uikit.DurationLayout`/
  `HostAndDurationPrefix`, e.g. `"somehost  ( 5.3s): OK ..."`), not merged
  into the trailing parenthetical - a later request specifically to make
  per-host durations easy to compare at a glance, which a trailing
  variable-length position couldn't give. Unreachable does get a detail
  from this now, as sketched.
- The recap section (`recap.go`) also gained a per-host *total* duration
  (summed across every task), same aligned-prefix shape - not part of the
  original sketch below, added afterward on request.

Original discussion and implementation sketch, kept for the reasoning
trail (the queueing-ambiguity investigation above is still accurate
background on *why* `v2_runner_on_start` mattered):
