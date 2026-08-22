# Show all variables

## Current situation

We already have the functionality to resolve all variables in a playbook
(with some known / accepted limitations). But I wonder if it would sometimes
help if one could see all available variables with their values in the exact
moment.

## Proposal

When going into drill down view, we are already running ansible-playbook
again (with some j2 hackery) to show the resolved versions of the variables.

Would it be possible to add a

  ansible.builtin.debug:
    var: hostvars[inventory_hostname]

and then analyze its output. 

We could put this information into an additional "Variables" tab.

## Findings from discussion

Checked a few things empirically (ansible-playbook run live, not assumed)
before going further:

* `debug: var: hostvars[inventory_hostname]` works exactly as hoped - the
  result's own JSON carries the literal key `"hostvars[inventory_hostname]"`
  holding the fully resolved dict. No parsing surprises there.
* Size is a real concern, not a hypothetical one. The same debug task run
  against a bare `localhost` host with no group_vars, no roles, nothing
  special produced a **54KB** result - bigger than the *entire rest* of
  that same run's own jsonl output combined (27KB). A real host with more
  gathered facts (network interfaces, mounts, packages, `ansible_env`,
  ...) or more variable sources (group_vars, role defaults, extra-vars)
  will be bigger still. Not necessarily a display problem (the existing
  "Details" tab already dumps large raw JSON without issue), but an open
  question: show the whole `hostvars` dict as-is, including the low-signal
  `ansible_facts` subtree, or filter that part out and focus the tab on
  the variables someone's actually likely to be checking (host_vars/
  group_vars/role defaults/extra-vars)? Leaning toward shipping it
  unfiltered first and seeing whether it's actually noisy in real use,
  matching how "Resolved"/"Docs" were both built simple-first and refined
  from there - but not decided.

## Known limitation (inherited, not new)

This would have the exact same "static context only" limitation
design-docs/Drilldown, Resolved Values.md already documents and accepted
for the "Resolved" tab: the stub run only has access to host_vars/
group_vars/extra-vars/gathered facts/role defaults - nothing computed at
runtime by earlier tasks in the *same* real run (`register`, `set_fact`,
loop-accumulated state), since that only ever existed in that one specific
`ansible-playbook` process's own memory and isn't recoverable from a
separate process afterward. A "Variables" tab built this way would show
what's *determinable without running the playbook*, not "the actual live
state of this run at this moment" - worth being clear-eyed about, since
"show all variables" sounds more comprehensive than it can actually be.
That earlier doc's own "What's next" section already anticipated wanting
something along these lines and explicitly deferred a real,
re-execution-based full-fidelity mode as a bigger, separate undertaking -
these two docs are already in conversation with each other.

## Decisions (once we proceed)

* **Fold into the existing per-task "Resolved" stub** (`resolveTaskValues`
  in resolved.go) rather than running a second, separate ansible-playbook
  invocation - one subprocess per drill-down open, not two. This does mean
  that function's current event-scanning (a simple "last matching event
  wins" loop, which currently assumes exactly one task ever runs in the
  stub) needs restructuring to track two tasks' own results separately
  rather than one overwriting the other.
* **Cache by `(host, role)`, not by host alone.** A host's variables don't
  depend on which task you're viewing *except* for role-scoped
  `defaults:`/`vars:`, which only apply while inside that role - exactly
  the same scoping `resolveTaskValues`'s own `roleVarsFiles` already
  handles for the "Resolved" tab. Caching by host alone would be simpler
  and reused across every task viewed for that host, but would silently
  drop role-scoped values - a real regression against what "Resolved"
  already shows per task. `(host, role)` is a bit coarser than per-task
  (only re-fetched when switching roles, not on every task within the same
  one) while staying complete.
* Tab placement: right after "Resolved" in the tab order - both are the
  "statically resolvable context" category.

## Status

Discussed, not yet implemented - deliberately paused for further thought
before building anything, per the "Known limitation" above.
