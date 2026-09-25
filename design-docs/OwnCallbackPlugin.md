# Shipping our own Ansible callback plugin

## Status

**Approach approved, implementation deferred to after the official
launch.** The core technical feasibility questions were spiked live (see
"What was verified" below) and turned up no hard blocker; the direction
and its key trade-offs were decided on 2026-09-02 (see "Decisions"
immediately below). Nothing is built yet and nothing should be built
until after launch - this doc is the spec to pick up then.

Supersedes the "implementing a custom callback plugin is explicitly
outside this project's architecture" framing in `CLAUDE.md`'s Data
source section and in `PerHostTaskTiming.md` - not by overriding it, but
because `Purpose.md`'s Platform section actually left the door open
("...but if the benefits would outweigh the simple shell command
solution I would be open to it").

## Decisions (2026-09-02)

1. **Fork `ansible.posix.jsonl` directly** and keep the fork
   **GPL-3.0-or-later** (a fork of GPL code stays GPL - that is fine and
   expected). No clean-room process - we're not trying to escape
   jsonl's copyright, we're complying with it. This collapses "Risk A"
   (copying jsonl) entirely: a licensed derivative is allowed to copy.

2. **Tangsible itself stays Apache-2.0.** The split is made explicit in
   `README.md`: the `tangsible` binary/CLI is Apache-2.0; the bundled
   Ansible callback plugin is a GPL-3.0 derivative of
   `ansible.posix.jsonl` and carries its own license header + `LICENSE`.
   The two are separate works communicating at arm's length (the plugin
   runs inside `ansible-playbook`, a separate process, and writes to a
   pipe) - mere aggregation, not a combined program. One narrow point
   still to nail down before shipping: whether `//go:embed`-ing the GPL
   file into the Apache binary counts as aggregation (probably yes,
   given the process boundary - GPLv3 §5's aggregate clause) or whether
   the plugin should instead ride along as a sibling file in the
   release archive. See Licensing → "The `//go:embed` question".

3. **Pursue motivation 2** (keep the user's own stdout callback; capture
   and optionally show its output). This fixes the plugin as
   `CALLBACK_TYPE = 'aggregate'` + dedicated-fd transport, not a
   stdout-type drop-in.

4. **Schema: mimic jsonl.** Now that the fork is GPL and
   derivative-by-design, there is no licensing reason to diverge, and
   staying byte-compatible keeps `events.go` / `aggregate.go` / run-log
   replay changes minimal. Only additions: a `host` field on
   `v2_runner_on_start`, and the event being emitted under `linear` at
   all (drop jsonl's `if self._is_lockstep: return` in that one
   method). See "The plugin" for the full diff-against-jsonl.

5. **Timing: after the official launch.** This is a real architectural
   change sitting behind `TODOsBeforeLaunch.md`; it waits.

## Decisions (2026-09-18)

Resolving the open items from the first pass:

6. **Don't `//go:embed` the plugin.** Ship it as a sibling file in the
   release archive unconditionally, not just as a fallback - this closes
   the licensing open detail (decision 2 / Licensing) without needing a
   combination-vs-aggregation legal judgment call at all. See "Shipping
   the .py from a Go binary".
7. **Never override the user's `stdout_callback`.** If unset, Ansible's
   own default (`default`) applies - fine, no special-casing needed.
8. **No stdout persistence in Phase 1.** Captured stdout (motivation 2)
   is live-session-only for now; revisit persisting it for revisit only
   if that turns out to matter once Phase 2 (the raw-output view) is
   actually built.
9. **Skip `v2_playbook_on_stats` in Phase 1.** Cheap to add later if a
   proper recap screen (with correct rescued/ignored categories) becomes
   worth building; not needed for anything currently planned.
10. **Compute per-host duration in Go, not in the plugin.** The plugin
    stays a dumb timestamp emitter (consistent with decision 4); `tui.go`
    (or `aggregate.go`, TBD when implemented) diffs
    `v2_runner_on_start` against the completion event itself. No
    `tangsible_duration_ms` field.
11. **Loop iteration visibility (roadblock 18) is explicitly Phase 2**,
    not an unscheduled follow-on doc. The plugin-side change (override
    `v2_runner_item_on_*`) can ride along with Phase 1's other additive
    hooks if convenient, but the Go-side data-model work (new
    `(task, host, item)` axis, tree/UI design) is Phase 2 scope.
12. **Roadblock #5's mitigation is confirmed, not just proposed:** shell
    out to `ansible-config dump --only-changed` (or equivalent) to read
    the *effective* `callbacks_enabled` list before Tangsible's own env
    var is set, then union our plugin's stem into that list rather than
    replacing it.

## Why this keeps coming up

Three separate motivations, only one of which is new:

1. **Per-host task timing.** `PerHostTaskTiming.md` investigated showing
   how long a task took on each host and concluded *drop it* - because
   under the `linear` strategy the only per-host timestamp available
   from `ansible.posix.jsonl` is each host's *finish* time, and
   "finish minus task-start" conflates real execution time with
   time-spent-queued-for-a-worker-slot whenever `forks < hosts`. That
   doc explicitly named the fix it couldn't use: "`v2_runner_on_start`
   would [resolve the ambiguity], but it isn't emitted under `linear`".
   A callback plugin of our own *is* emitted `v2_runner_on_start` (see
   below) - so this motivation is what turns the idea from "nice" into
   "this unblocks a feature we'd otherwise abandon".

2. **Keep the user's own stdout callback + offer a raw-output view.**
   Today Tangsible forces `ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl`,
   so the familiar `ansible-playbook` console output (the `default` or
   `yaml` callback, whatever the user configured) never happens. If our
   structured event stream came from an *aggregate* callback instead,
   the stdout slot would be free for the user's real callback, and
   Tangsible could capture that output and offer to show it - the same
   text a plain `ansible-playbook` run (or `ansible-navigator`'s stdout
   mode) would produce.

3. **One less dependency.** Tangsible's *only* reason for requiring the
   `ansible.posix` collection is the `jsonl` callback. Confirmed:
   ansible-core bundles exactly five callback plugins (`default`,
   `junit`, `minimal`, `oneline`, `tree`) - no `json`, no `jsonl`. A
   Tangsible-bundled callback removes the collection requirement
   outright: `version.go`'s "ansible.posix: NOT INSTALLED" path, the
   `ansible-galaxy collection install` line in `README.md` and
   `man/tangsible.1`, and the version check in `version.go` all go away.
   (We trade a user-run `ansible-galaxy` step for a `.py` Tangsible
   ships and wires up itself - a strict UX improvement, not a lateral
   move.)

## What was verified (live spikes)

Ran against real `ansible-playbook` (ansible-core 2.19.4) with a
throwaway aggregate callback (`CALLBACK_TYPE = 'aggregate'`,
`CALLBACK_NEEDS_ENABLED = True`) dropped in a dir pointed at by
`ANSIBLE_CALLBACK_PLUGINS` and turned on via `ANSIBLE_CALLBACKS_ENABLED`.

- **`v2_runner_on_start` fires per host under `linear`.** jsonl's
  `if self._is_lockstep: return` at the top of its own
  `v2_runner_on_start` is *jsonl's* choice, not an Ansible limitation.
  `CallbackBase.v2_runner_on_start(self, host, task)` is invoked by the
  strategy for every host it dispatches, `linear` included. The host
  object is right there in the signature.

- **It reveals worker-slot batching.** 4 local hosts, `--forks 2`,
  `command: sleep 1`:

  ```
  runner_on_start  h1   t+0.000
  runner_on_start  h2   t+0.004
  runner_on_start  h3   t+1.193      <- after wave 1's sleep frees a slot
  runner_on_start  h4   t+1.197
  ```

  This is exactly the signal `PerHostTaskTiming.md` said would
  disambiguate "slow host" from "queued host": subtract each host's own
  `v2_runner_on_start` timestamp instead of the shared task-start, and
  the queue wait falls out of the number.

- **`no_log: true` does not leak.** The spike checked
  `'secretvalue123' in str(result._result)` in `v2_runner_on_ok` for a
  `no_log` task - `False`. ansible-core censors the result dict before
  any callback sees it (same censored dict jsonl already receives).
  Nothing new to guard.

- **Adjacent (non-collection) plugin discovery works** by short name:
  `ANSIBLE_CALLBACK_PLUGINS=<dir>` + `ANSIBLE_CALLBACKS_ENABLED=<stem>`,
  no FQCN, no `meta/runtime.yml`. And an `aggregate` callback runs
  happily alongside `ANSIBLE_STDOUT_CALLBACK=default` - both fired in
  the same run.

- **Command-family modules still carry real on-host `start`/`end`/
  `delta`** in `result._result` (already true today) - a second,
  module-measured timing source for `command`/`shell`/`script`, free of
  any controller-side queue latency.

## Addendum (2026-09-13): handler task visibility

Spiked live (`handler_spike.yml` - a `command` task with `notify:` plus
an explicit `meta: flush_handlers`, run against real
`ansible.posix.jsonl`) to check a since-corrected assumption that jsonl
doesn't report handler tasks at all.

**Correction: it does, already, unforked.**
`v2_playbook_on_handler_task_start` (`jsonl.py:137-142`) fires under
`linear`/`debug` exactly parallel to `v2_playbook_on_task_start`,
producing an identical task-start event, followed by a normal
`v2_runner_on_ok` (etc.) for the handler's result. `CLAUDE.md` already
notes `aggregate.go` handles both events in the same `case` - so
handlers already show up as their own rows in Tangsible today, no fork
needed. Two real gaps remain, though, both additive changes riding on
the same diff as the already-planned `v2_runner_on_start`-under-lockstep
change:

1. **No `is_handler` field.** Both task-start events build an identical
   shape via `_new_task(task)` - nothing in the emitted JSON says "this
   task is a handler," only which Python method happened to fire.
   `aggregate.go` can't tell them apart today, so there's no way to
   render handlers distinctly (dim, a `[handler]` tag, include/exclude
   them from the `Interesting` filter, etc.) even though the data to do
   so exists right inside `ansible-playbook`'s own task object. Fix:
   stamp `task_result['task']['is_handler'] = True` in the
   `v2_playbook_on_handler_task_start` branch only - one line, no
   `_new_task` change needed.

2. **No visibility into "notified, not yet run."**
   `CallbackBase.v2_playbook_on_notify(self, handler, host)` fires the
   instant a task's `notify:` queues a handler - well before it actually
   executes at flush time - but jsonl never overrides it, so that moment
   is invisible in the event stream today; a handler simply doesn't
   exist as far as the callback is concerned until it starts running. A
   fork could add a `v2_playbook_on_notify` override, emitting a
   lightweight event (handler name/id + host) that lets the tree show a
   greyed "queued" row between notification and flush - a genuinely new
   capability jsonl structurally can't provide, not a duplicate of
   information available another way.

Schema-wise, both are purely additive (a new field, a new event type),
consistent with decision 4 (mimic jsonl) - no changes needed to existing
event shapes, no `events.go`/replay-compatibility break: old logs simply
lack `is_handler` and the notify event, treated as `is_handler: false` /
no queued state, the same "unknown -> sensible default" degradation
already used for pre-fork logs missing `v2_runner_on_start`.

## Addendum (2026-09-17): ignore_errors is another field jsonl drops

`design-docs/Notifications.md` (terminal notifications on playbook-finish/
task-failure) wanted to skip `notify_task_failed` for an `ignore_errors:
true` failure. Confirmed the same way `recap.go`'s own doc comment already
had for the recap's rescued/ignored categories: `ansible.posix.jsonl`'s
`__getattribute__`-dispatched `_record_task_result` receives
`ignore_errors` as a callback kwarg but never writes it into the emitted
JSON at all - dropped silently, same root cause as recap.go's
"rescued/ignored categories" omission above. Decision (recorded in
`Notifications.md`): shipped without that exclusion for v1 -
`notify_task_failed` fires for every recorded failure, `ignore_errors` or
not.

**When this fork lands, add `ignore_errors` to the emitted per-host result**
alongside the existing `on_info` merge in `_record_task_result` (jsonl.py's
`__getattribute__` trick already receives it as a kwarg on the
`v2_runner_on_failed` call - it's just discarded today) - a one-line,
purely-additive change in the same spirit as `is_handler` above. That
unblocks both `Notifications.md`'s `notify_task_failed` exclusion and
`recap.go`'s own descoped rescued/ignored categories in one go.

## Addendum (2026-09-13): loop iteration visibility

Spiked live in two steps (`loop_spike.yml` - a `command` task looping
over three items):

1. Against real `ansible.posix.jsonl`: exactly one `v2_runner_on_ok`
   event fires for the whole task, containing a `results: [...]` array
   with all three items' full per-item detail (`item`, `stdout`,
   `changed`, `delta`, etc.) batched inside it - nothing arrives until
   the entire loop has finished.
2. Against a throwaway `aggregate` callback overriding
   `v2_runner_item_on_ok`/`_on_failed`/`_on_skipped` (hooks
   `CallbackBase` already defines - `ansible/plugins/callback/
   __init__.py`, fired live per-iteration from
   `executor/task_executor.py` / `strategy/__init__.py`): three
   `ITEM_OK` events land in loop order, each well before the terminal
   aggregate event.

**Confirms the premise: yes, individual iterations are invisible today
- but not because the data doesn't reach Tangsible at all.** The full
per-item detail is already sitting inside the final event's `results`
array; `aggregate.go` doesn't look at it (a looped task collapses to one
outcome via whatever top-level `changed`/`failed` the aggregate result
carries), so today it's visible only by drilling into a host's raw-JSON
fallback view, and only after the loop has already completed - never
live, never as its own row.

**Yes, our plugin could change this - mechanically it's the same shape
of change as the handler additions above:** override the three
`v2_runner_item_on_*` hooks and `_write_event` one line per iteration,
reusing the existing `_record_task_result`/`_find_result_task` machinery
(same `(host, task._uuid)` key every item shares - no new bookkeeping).
Purely additive: the existing terminal `v2_runner_on_ok` with its
`results` array keeps firing unchanged, so nothing about today's schema
or replay compatibility regresses; old logs simply lack the live item
events and fall back to exactly today's behaviour.

**Where this differs from the handler additions: the payoff needs real
Go-side design, not just a plugin tweak.** `aggregate.go`'s
`taskNode.Hosts` is `map[string]outcome` - one outcome slot per
(task, host). An item event adds a third axis, (task, host, item), that
the data model has no slot for today. Actually showing it means either a
third tree-nesting level (`tui.go` currently only goes play -> task ->
host, expanded via Enter) or a different UI idiom entirely (e.g. an item
list surfaced inside the host drill-down rather than as its own
expandable tree rows). It also leans harder on the "~10 hosts, not
hundreds" scale assumption (`CLAUDE.md`) than hosts themselves ever did
- a loop over a package list can easily have far more iterations than
there are hosts, with no natural cap.

**Decision (2026-09-18): Phase 2, not a separate unscheduled doc.** The
plugin-side hook override is small enough to land alongside Phase 1's
other additive changes if convenient, but the Go-side data-model work
(new `(task, host, item)` axis, tree/UI design) is explicitly Phase 2
scope - see "Decisions (2026-09-18)" above and "Suggested phasing"
below.

## Background: the two callback types that matter

- **stdout** (`CALLBACK_TYPE = 'stdout'`): exactly one active per run,
  selected by `stdout_callback` / `ANSIBLE_STDOUT_CALLBACK`. Owns the
  console: task banners, per-host lines, `PLAY RECAP`. `ansible.posix.
  jsonl` is one of these; so is `default`.

- **aggregate / notification** (`CALLBACK_TYPE = 'aggregate'`): any
  number active, each explicitly enabled via `callbacks_enabled` /
  `ANSIBLE_CALLBACKS_ENABLED`. Runs *in addition to* the stdout
  callback. Same event hooks, same arguments - it is not a lesser API.
  `profile_tasks`, `junit`, `mail` are these.

The whole design hinges on Tangsible's event source moving from a
**stdout** callback we impose to an **aggregate** callback we ship,
leaving the stdout slot to the user.

## Proposed design

### The plugin: a modified fork of `jsonl.py`

Start from `ansible.posix.jsonl`'s source verbatim (GPL-3.0 header
kept, our copyright line added), then make the minimal set of changes:

- **`CALLBACK_TYPE = 'stdout'` → `'aggregate'`**, add
  `CALLBACK_NEEDS_ENABLED = True`. This is what lets it run *alongside*
  the user's real stdout callback (decision 3). The `v2_*` hooks all
  still fire for an aggregate callback (verified in the spikes).
- **Make `v2_runner_on_start` emit under lockstep too** - this is the
  whole point, the per-host event under `linear`. Not a one-line
  deletion: jsonl's non-lockstep path also does `_new_task` + appends to
  `self.results[-1]['tasks']` + registers in `_task_map`, all of which
  `v2_playbook_on_task_start` *already did* under lockstep. So the
  lockstep branch must emit a *minimal* event (host name + task ref +
  `_timestamp`) and return **without** touching `results` / `_task_map`.
  Keep `v2_playbook_on_task_start`'s own lockstep behaviour untouched.
- **Add the host name to the `v2_runner_on_start` payload** - jsonl
  builds `_new_task(task)` there with an empty `hosts: {}` and never
  records which host it's for. Add `"host": host.get_name()` (top-level,
  smallest possible deviation from the existing schema).
- **Redirect output from stdout to a dedicated fd.** `_write_event`
  currently ends in `self._display.display(...)`; instead write the
  line to the fd named by `TANGSIBLE_EVENT_FD` (see Transport). Fall
  back to `self._display.display` when that env var is absent, so the
  plugin still works standalone for testing.
- Leave everything else - `AnsibleJSONEncoder`, the `_new_play` /
  `_new_task` shapes, `task.duration`, the `__getattribute__` dispatch
  trick, `_find_result_task` - **exactly as jsonl has it.** We're a
  GPL derivative; there's no reason to reimplement what already works,
  and byte-compatibility is decision 4.
- Optional cleanup: jsonl's `current_time()` uses the deprecated
  `datetime.utcnow()`. Swap to `datetime.now(timezone.utc)` - but a naive
  swap is *not* output-identical (verified 2026-09-18): tz-aware
  `.isoformat()` appends `+00:00`, so `'%sZ' % ...isoformat()` produces
  `...+00:00Z`, a double suffix that breaks the wire format. Must
  `.replace(tzinfo=None)` before `.isoformat()` to actually match the old
  output byte-for-byte.

Net diff against `jsonl.py`: a few lines. That is the point of
forking rather than rebuilding.

Two more small additions, from the 2026-09-13 addendum below: stamp
`is_handler` on the task object built in
`v2_playbook_on_handler_task_start`, and add a `v2_playbook_on_notify`
override emitting a lightweight queued-handler event. Neither changes
any existing event's shape.

**Schema: mimic jsonl (decision 4).** Same `_event` names, same
`{"hosts": {"<name>": {...}}}` shape, same `task.duration.{start,end}`,
same `_timestamp`. Consequences:

- `events.go`'s `RawEvent` needs only a new `Host string
  \`json:"host"\`` field; `aggregate.go`'s `Apply` switch needs only a
  new `case "v2_runner_on_start":`.
- Saved run logs (`<runID>.jsonl`, `runlog.go`) stay
  replay-compatible - `revisit.go` / `diff.go` replay through the same
  `ScanEvents` path. Pre-fork logs simply lack `v2_runner_on_start`
  lines; the new per-host-timing code treats a missing start as
  "unknown → fall back to the task-start approximation", exactly the
  degraded mode `PerHostTaskTiming.md` already describes.

The rejected alternative was a bespoke flat schema (host explicit on
every event, outcome pre-classified). Better in the abstract, but it
would rewrite `RawEvent` decoding + the `Apply` switch, force
re-recording every test fixture, and break replay of old run logs
without a translation shim - and with the fork being an accepted GPL
derivative, its one real remaining advantage (licensing distance from
jsonl's schema) no longer applies.

### Note: this transport is host-process-only

`ExecutionEnvironments.md` (a separate, independent effort) found that
`ansible-runner`'s own container-invocation code never uses fd-passthrough
(no `--preserve-fds`, no docker equivalent) - there is no portable way to
hand a container process an arbitrary inherited fd the way `cmd.ExtraFiles`
hands one to a plain child process. So the dedicated-fd transport below
works for direct `ansible-playbook` execution but **cannot extend to a
future containerized/execution-environment mode** - that mode would need
to fall back to plain stdout-jsonl (today's mechanism) regardless of what
ships here. Not a blocker on either doc, but worth keeping in mind before
treating the fd transport as the only one Tangsible will ever need.

### Transport: dedicated inherited fd

The plugin writes its JSONL to a **dedicated file descriptor**, not
stdout:

- Go side opens an `os.Pipe()`, passes the write end via
  `cmd.ExtraFiles` (→ fd 3 in the child), sets
  `TANGSIBLE_EVENT_FD=3` in the child env, closes its own copy of the
  write end after `cmd.Start()` so EOF arrives when the child exits.
- Plugin does `os.fdopen(int(os.environ["TANGSIBLE_EVENT_FD"]), "w")`,
  writes one JSON line per event, flushes per event (liveness).
- If the fd env var is absent (plugin run outside Tangsible), fall back
  to stdout so the plugin is still independently usable / testable.

Why a dedicated fd and not stdout-interleaved-with-tags:

- The child's stdout becomes *purely* the user's human callback →
  capturing it for the raw-output view (motivation 2) is trivial and
  unambiguous.
- No line-classification heuristic that breaks if the user sets
  `ANSIBLE_STDOUT_CALLBACK=json`.
- `ScanEvents`' "tee byte-identical to the run log" promise stays clean:
  the fd stream is the run log, nothing else mixed in.

Cost: a second pipe + reader goroutine, and `ScanEvents` now reads the
fd instead of `cmd.StdoutPipe()`. The pre-flight gate (peek first item,
`v2_playbook_on_play_start` is the reliable first event) moves from the
stdout reader to the fd reader - and gets slightly *more* robust:
`--list-tasks` / `--syntax-check` / `--list-hosts`, which currently open
a blank ticking TUI (`CLAUDE.md`'s own noted gap), produce zero fd
events → gate cleanly declines to build the TUI → captured stdout is
printed through.

### Shipping the .py from a Go binary

**Decision (2026-09-18): sibling file, not `//go:embed`.** Chosen
unconditionally rather than as a fallback, specifically to avoid ever
having to resolve the aggregation-vs-combination question at all - see
"Decisions (2026-09-18)" and Licensing below.

Mechanism: the release archive carries `tangsible` +
`tangsible-jsonl.py` side by side. The installer drops the `.py` at a
known path, and the binary points `ANSIBLE_CALLBACK_PLUGINS` there
directly (`:`-join with any existing value, don't clobber) and adds our
stem to `ANSIBLE_CALLBACKS_ENABLED` (union, don't clobber - see
roadblock 5). No temp-dir write, no per-run file rewrite. Cost: `go
install`-from-source users need a one-line manual fetch of the `.py`,
which the `version` / startup check should detect and explain.

**Implementation note (2026-09-18): the "known path" is `<prefix>/share/
tangsible`, derived structurally from the running executable's own path**
(its `bin/` dir's own parent + `/share/tangsible`), not from
`$XDG_DATA_HOME` directly and not a `callback/` directory sibling to the
binary itself (an earlier, briefly-shipped choice - see below for why it
didn't hold up). Data belongs under `.../share`, not mixed into `.../bin`
alongside the executable - the whole reason this needed a second look.
The structural derivation is what makes one rule correctly cover both the
default install (`~/.local/bin/tangsible` → `~/.local/share/tangsible`,
exactly `$XDG_DATA_HOME`'s own default) and a `--prefix` one
(`/opt/x/bin/tangsible` → `/opt/x/share/tangsible`, exactly what
`install.sh`'s own `--prefix`-aware `DATA_DIR` computes) without
`install.sh` and the Go binary ever needing to agree on anything at
install time - confirmed live, both directions, actually installing and
running the resulting binary with zero env overrides. `$XDG_DATA_HOME/
tangsible` remains a second, lower-priority candidate, for the one case
where the two genuinely diverge: a default (no-`--prefix`) install with
`$XDG_DATA_HOME` set to something other than `~/.local/share` -
`install.sh`'s own `DATA_DIR` already follows that override in the
no-`--prefix` case, so this candidate is what actually finds it then.

**Not `~/.ansible/plugins/callback`** (ansible's own default callback
plugin search path) either - confirmed live it would work with zero
`ANSIBLE_CALLBACK_PLUGINS` needed at all, but two reasons against it: the
plugin is purpose-built for tangsible, not a general-purpose callback
worth placing somewhere other tooling/the user's own plugins might also
live; and it wouldn't actually remove the need for `ANSIBLE_CALLBACK_PLUGINS`'s
own union logic anyway - confirmed `ANSIBLE_CALLBACK_PLUGINS` replaces
rather than adds to the default search path (same non-additive behavior
roadblock 5 already found for `ANSIBLE_CALLBACKS_ENABLED`), so a user
with their own `callback_plugins` customization in `ansible.cfg` would
still silently lose our plugin there too, unless tangsible actively
re-unions its own location in - the exact same mechanism either way.

(Rejected: `//go:embed`-ing the file and writing it to a per-version
temp dir on startup. Mechanically straightforward - the file is small
(~5 KB), a temp-dir write is cheap, and a locked-down `$TMPDIR` could
still fall back to documenting a manual drop into
`~/.ansible/plugins/callback/`. Dropped anyway because it would have
made shipping the plugin contingent on resolving whether embedded GPL
bytes inside an Apache binary count as GPLv3 §5 aggregation or
combination - a question worth avoiding entirely rather than answering,
per decision 6.)

### Go-side touch points (checked, not assumed)

- `internal/runner/process.go:~136` - `SpawnGeneration`: swap the env
  vars, add the event-fd pipe, point `ScanEvents` at the fd, capture
  stdout to a new `<runID>.stdout` file.
- `internal/session/resolved.go:~179`,
  `internal/host/host.go:~604`,
  `internal/template/template.go:~215` - three more sites that set
  `ANSIBLE_STDOUT_CALLBACK=ansible.posix.jsonl` for their own one-shot
  `ansible-playbook` scrapes. Each needs the same env change. These
  don't need the fd dance (they already just capture `cmd.Output()` and
  scan it) - simplest is a shared helper that returns the env slice +
  (optionally) sets up the fd, so all four sites stay in sync.
- `internal/playbook/events.go` - add `Host string \`json:"host"\`` to
  `RawEvent`; nothing else (schema is mimicked).
- `internal/playbook/aggregate.go:~214` - add a
  `case "v2_runner_on_start":` that records `ev.Timestamp()` per
  `(currentTask, ev.Host)` into a new `taskNode` map (the
  `PerHostTaskTiming.md` sketch's `Started map[string]time.Time`, now
  actually meaningful).
- `internal/session/version.go:~93-142` - drop the `ansible.posix`
  collection check; report the bundled plugin's own version instead.
- `internal/config/runlog.go` + `revisit.go` + `diff.go` - add the
  `.stdout` sidecar alongside the existing `.jsonl` (or decide the raw
  output isn't worth persisting for revisit - see open questions).
- `README.md` (§Requirements ~L111, and a new §License note per
  decision 2), `man/tangsible.1:~78-98` - drop the collection install
  step; state the Apache-2.0 binary / GPL-3.0 plugin split.

### The raw-output view (motivation 2)

Deliberately deferred to its own phase. Once stdout is captured to a
buffer/file, surfacing it is a UI question independent of everything
above: a drill-down tab? a full-screen toggle like the old pre-split
output view? revisit-only? Not designed here.

## Licensing

**Not legal advice.** This records the chosen position and the one
detail still to confirm.

Tangsible is **Apache-2.0** throughout (`LICENSE`, every `.go` header,
`README.md` §License). `ansible.posix.jsonl` and ansible-core are
**GPL-3.0-or-later**.

### The chosen position (decisions 1-2)

The plugin is a **direct fork of `jsonl.py`, distributed as GPL-3.0**.
We are not trying to escape jsonl's copyright - a licensed GPL
derivative may copy jsonl freely, so the "did we copy jsonl's
expression" question (previously "Risk A", and the whole clean-room
discussion) simply **does not arise**.

The remaining question - "is a callback plugin a derivative work of
GPL ansible-core, given it subclasses `CallbackBase` in-process"
(previously "Risk B") - **also does not bite us**, because the plugin
is GPL anyway. It's allowed to be a derivative of ansible-core. What we
need is only that this GPL obligation stays contained to the plugin and
does not reach the `tangsible` binary.

Containment argument: the plugin and the binary are **separate works at
arm's length**. The `tangsible` binary never imports, links, or runs
the plugin in its own process - it ships the `.py` as a sibling file on
disk and spawns `ansible-playbook`, a separate GPL program, which loads
the plugin. The
only channel between them is a pipe carrying JSON lines. This is the
textbook shape of *mere aggregation* (GPLv3 §5's aggregate clause:
"separate and independent works, which are not by their nature
extensions of the covered work"). The binary stays Apache-2.0.

`README.md` states the split plainly: **the `tangsible` CLI is
Apache-2.0; the bundled `tangsible-jsonl` Ansible callback plugin is a
GPL-3.0 derivative of `ansible.posix.jsonl`** and ships with its own
GPL header and `LICENSE` file.

Prior art that the plugin-as-non-viral-component reading is mainstream:
`DataDog/ansible-datadog-callback` is **MIT** (verified 2026-09-02, ©
Datadog Inc.) and is an `aggregate` callback subclassing `CallbackBase`
- the same shape. Not dispositive, but a sophisticated actor taking
that position publicly.

### The `//go:embed` question - resolved by avoiding it (2026-09-18)

`//go:embed`-ing the GPL `.py` into the Apache-2.0 `tangsible` binary
would put GPL bytes *inside* the shipped binary. Two readings existed:

- **Aggregation (likely):** the embedded file is an inert data resource,
  extracted to disk and run only by a separate process. A binary with
  embedded assets is "a volume of a storage medium" carrying two
  separate works - §5 aggregation. The binary's own license is
  unaffected.
- **Combination (conservative):** the binary "contains" GPL code, so
  the binary-as-distributed must be offered under GPL-compatible terms.
  Apache-2.0 → GPLv3 is one-way compatible, so the *combination* would
  be GPLv3 even though the Go source stays Apache-2.0 - which defeats
  the "the binary is Apache" goal.

**Decided 2026-09-18: don't embed, full stop.** Rather than pick a side
in the aggregation-vs-combination reading, ship the `.py` beside the
binary unconditionally (see "Shipping the .py" for the mechanics) - the
binary then never contains GPL bytes and the legal question simply
doesn't need answering. Cost: a manual fetch for `go install`-from-source
users, which the `version` / startup check should detect and explain.

## Roadblocks and open questions

| # | Concern | Finding / mitigation |
|---|---------|----------------------|
| 1 | `v2_runner_on_start` under `linear` | ✅ fires per host (spiked) |
| 2 | Does it actually disambiguate queue wait | ✅ wave-2 starts fire when wave-1 frees slots (spiked) |
| 3 | `no_log` / secrets in the event stream | ✅ censored upstream before callbacks (spiked); parity with jsonl |
| 4 | ansible-core has no json/jsonl callback | ✅ confirmed; our plugin removes the only `ansible.posix` need |
| 5 | `ANSIBLE_CALLBACKS_ENABLED` *replaces* `ansible.cfg`'s list, doesn't merge | user's `profile_tasks` etc. would silently stop. ✅ Mitigation confirmed 2026-09-18: read the effective value via `ansible-config dump --only-changed` (or equivalent) before setting our own env var, then union our stem into that list rather than replacing it. Still real plumbing (subprocess call, precedence correctness) - not a one-liner. |
| 6 | env var name history | `ANSIBLE_CALLBACKS_ENABLED` is 2.11+; older is `ANSIBLE_CALLBACK_WHITELIST`. Set a documented min ansible-core version (already effectively past this). |
| 7 | semi-private imports (`AnsibleJSONEncoder`, `CallbackBase`) | kept as jsonl has them - we're a GPL fork mimicking jsonl, so no reason to avoid them; same version-fragility jsonl itself carries |
| 8 | `ANSIBLE_CALLBACK_PLUGINS` / `_ENABLED` already set by user | prepend / union, never clobber |
| 9 | plugin without `DOCUMENTATION` | jsonl already has one; keep it |
| 10 | `strategy: free` still unsupported by the aggregate model | ✅ resolved on the `strategy-free` branch (`design-docs/StrategyFree.md`) - built directly on this plugin's `v2_runner_on_start.host` field, exactly the dependency-change-avoided outcome this row anticipated |
| 11 | run-log replay compat (revisit / diff) | schema is mimicked → old `.jsonl` logs replay unchanged; new `v2_runner_on_start` lines are simply absent in old logs and handled as "unknown" |
| 12 | Windows | `cmd.ExtraFiles` is POSIX; Tangsible needs a real TTY anyway - document POSIX-only, no regression |
| 13 | become / vault prompts | unaffected - they hit the tty before the TUI, no callback involved |
| 14 | double timing sources (plugin `v2_runner_on_start` vs module `delta`) | plugin start-time is controller-observed (dispatch); module `delta` is on-host and only for command-family. Decide precedence in `tui.go`; not a blocker |
| 15 | GPL of the plugin reaching the binary | see Licensing - decided: plugin is GPL, binary stays Apache via arm's-length aggregation. ✅ 2026-09-18: `//go:embed` question resolved by not embedding at all - ships as a sibling file in the release archive, unconditionally |
| 16 | Handlers not visually distinguishable from regular tasks | ✅ spiked (2026-09-13 addendum) - jsonl already *reports* handler tasks (`v2_playbook_on_handler_task_start` under lockstep), but the emitted JSON has no `is_handler` field to tell them apart; one-line fix in the fork |
| 17 | "Notified but not yet run" handlers invisible | jsonl never overrides `v2_playbook_on_notify`; a fork adding it would be new capability, not just parity (2026-09-13 addendum) |
| 18 | Loop iterations invisible until the whole loop finishes | ✅ spiked (2026-09-13 addendum) - jsonl never overrides `v2_runner_item_on_*`; plugin-side fix is small (same pattern as 16/17) but rendering it needs a real `aggregate.go`/`tui.go` data-model change (new task/host/item axis), plus it strains the ~10-host scale assumption harder than hosts do. ✅ 2026-09-18: explicitly assigned to Phase 2, not left unscheduled |
| 19 | `ignore_errors` not visible, blocking `Notifications.md`'s `notify_task_failed` exclusion | ✅ confirmed (2026-09-17 addendum) - jsonl receives it as a kwarg but never emits it; one-line additive fix, same shape as `is_handler` (16) |

Open questions: **none remaining** - all resolved 2026-09-18 (see
"Decisions (2026-09-18)" above):

- Respect the user's `stdout_callback` unconditionally, or default it to
  something when unset? → **Never override.** If unset, ansible's own
  default (`default`) applies, which is fine.
- Persist captured stdout for revisit, or keep it live-session-only? →
  **Live-session-only for Phase 1**; revisit only if it matters once
  Phase 2 actually builds the raw-output view.
- Emit `v2_playbook_on_stats`? → **Skip for Phase 1**; cheap to add
  later if a recap screen with accurate rescued/ignored counts is ever
  wanted.
- Pre-computed `tangsible_duration_ms` on outcome events, or difference
  timestamps in `tui.go`? → **Diff timestamps in Go.** Keeps the plugin
  a dumb timestamp emitter and `aggregate.go` formatting-free.

## Suggested phasing

**All of this waits until after the official launch (decision 5).**

- **Phase 0** - spikes (done; recorded here). Direction decided
  2026-09-02; all remaining open questions resolved 2026-09-18. Nothing
  owed before Phase 1 starts.
- **Phase 1** - fork `jsonl.py` (GPL) with the modifications in "The
  plugin" (excluding `v2_runner_item_on_*` and `v2_playbook_on_stats` -
  see below); ship it as a sibling file + fd transport; migrate all four
  env-var sites (including the roadblock-5 `ansible-config dump`
  union logic); drop the `ansible.posix` dependency (motivation 3); wire
  `v2_runner_on_start` into `aggregate.go` and build per-host timing
  (motivation 1) - now unambiguous, so `PerHostTaskTiming.md`'s
  "option 1: drop it" is off the table and its implementation sketch
  (`hostDuration`, `formatDuration`, `hostLabel` parenthetical) applies
  directly, computing the diff in Go rather than the plugin. Add the
  `README.md` license-split note.
- **Phase 2** - capture + surface the user's stdout-callback output
  (motivation 2, UI-only, design separately); loop iteration visibility
  (roadblock 18, both the plugin-side `v2_runner_item_on_*` hooks and
  the Go-side data-model/UI work); revisit stdout persistence and
  `v2_playbook_on_stats` only if they turn out to matter once this phase
  is actually being built.

## Rough effort

- Fork + modify `jsonl.py`: ~half a day incl. pytest-style tests for
  the `v2_runner_on_start`-under-linear behaviour.
- fd transport + four env sites + gate move: ~1-1.5 days (the riskiest
  Go-side change - the pre-flight gate and rerun reset both touch it).
- `events.go` / `aggregate.go` additions + per-host timing UI: ~1 day
  (sketch already exists).
- Docs (incl. license split), `version.go`, e2e fixture re-record:
  ~half a day.
- Phase 2 raw-output view: ~1-2 days, not estimated in detail.

Phase 1 total ≈ 3-4 days.

## Recommendation (historical - superseded by Decisions above)

Kept for the reasoning trail. Before the 2026-09-02 decisions this
section read: do Phase 1 as specified (aggregate + fd) rather than a
stdout-drop-in shortcut if motivation 2 is wanted; and the licensing
question is the real gate. Both are now settled - motivation 2 is in
(decision 3), and the licensing position is chosen (decisions 1-2, GPL
fork + Apache binary via arm's-length aggregation, with the `//go:embed`
detail resolved by shipping a sibling file instead - decision 6).

## Follow-ups when Phase 1 lands

Not now - only as part of the Phase 1 change, post-launch:

- `CLAUDE.md` Data source section: "Never uses Ansible's Python API or a
  custom callback plugin" - update to describe the bundled GPL fork.
- `Purpose.md` Platform section: already hedged; note the decision.
- `PerHostTaskTiming.md`: currently treats `v2_runner_on_start` as
  unobtainable under `linear` and leans toward dropping per-host timing;
  the fork makes it obtainable, so per-host timing is back on.
- `README.md` / `man/tangsible.1`: drop the `ansible.posix` install
  step; add the Apache-2.0-binary / GPL-3.0-plugin split.
- `.gitignore` / release tooling: the forked `.py` is a tracked source
  file with its own GPL `LICENSE`.
