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
   whatever default Tangsible sets. Rather than gambling on whether a
   same-named plugin in a configured search path can shadow an ansible-core
   built-in (unverified), activate ours via an ordinary `--strategy
   tangsible_debug` passthrough argument, the same unconditional-append
   shape `--diff` already gets. **Accepted gap:** a play with its own
   explicit `strategy: linear` or `strategy: free` won't get debug data,
   same shape as the existing `strategy: free` gap
   (`design-docs/StrategyFree.md`) - not chased further for v1.
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

**Activation mechanics (sketch, not finalized).** A CLI flag (name TBD -
`--debug` collides in spirit with `-vvv`-style debug output, so probably
something more specific) stripped from the passthrough list before
`ansible-playbook` ever sees it, the same synthetic-flag treatment
`--start-at-play` already gets - it drives Tangsible's own env/strategy
setup, not a real `ansible-playbook` argument, so (like `--start-at-play`)
it shouldn't be recorded into `.tangsible/state.toml` history verbatim.
Because debug mode needs to stay active across every rerun in a session
(decision 5), it's tracked as session-level Go state (detected once,
alongside `HasCheckFlag`'s equivalent) and reapplied on every generation's
spawn, rather than living in the passthrough `Rest` a rerun replays. A
`general.debug_mode` config default, mirroring `run_dialog`'s
flag-beats-config-beats-verb-default precedence, is a reasonable follow-on
but wasn't part of this discussion - not decided here.

**Biggest open risk, stated plainly.** `OwnCallbackPlugin.md`'s roadblock
#7 already flags semi-private-import fragility for a callback fork; a
strategy subclass leans on this considerably harder -
`_process_pending_results`'s exact signature/timing and whether calling
`get_vars()` immediately after `super()`'s call actually observes
just-merged facts are assumptions, not confirmed facts. This needs the
same live-spike treatment `OwnCallbackPlugin.md` gave
`v2_runner_on_start`-under-`linear` before any of the above should be
treated as settled instead of proposed.

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

- Exact CLI flag name and whether a `general.` config default is wanted
  (sketch only above, not decided).
- Whether the all-variables tab (deliverable 2) reuses the Resolved tab's
  UI shape or needs its own - not designed yet.
- Whether the `_process_pending_results` hook point survives a live spike
  unchanged, or needs a different override point - the design above is the
  starting hypothesis, not a confirmed fact.
