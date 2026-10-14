# Debug mode

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
