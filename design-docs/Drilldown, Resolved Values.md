# Drilldown: resolved values

## Problem

The drill-down view's "Task definition" section already shows a task exactly
as written in the source file - but that's the raw text, `{{ some_var }}`
and all. Being able to see the same task with its variables actually filled
in would make it much easier to spot potential errors (a typo'd variable
name, an unexpected default, a value that isn't what you thought it was)
without having to go dig through group_vars/host_vars/role defaults by hand.
Won't be possible in every situation - loop variables being the obvious
one - but for a large fraction of real tasks it will be, and that's worth
having even without full coverage.

## Idea

ansible-playbook itself doesn't expose a "give me this task fully resolved"
function - but a real running task can resolve its own values just fine,
using ansible's own templating engine, on ansible's own terms. So instead of
reimplementing variable resolution ourselves (which would mean either
duplicating ansible's ~14-level variable precedence chain, or reaching into
undocumented internals no supported plugin API exposes - confirmed live: a
callback plugin doesn't get handed the VariableManager either, so that
avenue doesn't unlock this either), the task's own source is fed back into
ansible as a `.j2` template and rendered for real, via
`ansible.builtin.template`, `delegate_to: localhost` (so nothing is ever
written to a real target host - same as `tangsible template`'s own
approach).

Every `{{ expr }}` in the task's own source is rewritten to
`{{ expr | default("{{ expr }}") }}` before rendering, so a variable that
isn't resolvable (a loop variable like `item`, anything genuinely
undefined) falls back to showing its own original, literal `{{ expr }}`
text rather than erroring the whole render out or silently going blank -
confirmed live that Jinja doesn't recursively re-template a filter's own
string output, so this is safe, ordinary behavior, not a fragile trick.

## Scope for this first version

Chosen over two more ambitious alternatives (see the design conversation
this doc follows from) because it's safe, cheap, and reuses machinery that
already exists:

* **Static context only.** The stub run only has access to what
  `tangsible role`/`tangsible template` already have access to - host_vars,
  group_vars, extra-vars, gathered facts, and (for a role-owned task) the
  role's own `defaults/main.yml`/`vars/main.yml` via `vars_files`. It does
  *not* have access to anything computed at runtime by earlier tasks in the
  same real run - `register`ed values, `set_fact`, accumulated loop
  state - since that only ever exists in that one specific
  `ansible-playbook` process's own memory and isn't recoverable afterward
  from a separate process. This is a real, known gap, not an oversight -
  closing it would mean re-running the whole playbook for real (a much
  bigger, riskier undertaking - more ideas along those lines later, once
  this first version has proven itself useful).
* **Never mutates the real run.** This is a separate, explicit,
  after-the-fact ansible-playbook invocation against a generated stub -
  exactly the same "we only ever observe, never change what actually
  executes" rule `role.go`/`template.go` already follow. The task's own
  real result is never touched.

## Mechanics

Reuses most of `template.go`'s existing plumbing directly:

* The stub playbook targets `hosts: <the exact host being drilled into>`
  (known already - a drill-down is always for one specific host, so there's
  no need for `template.go`'s own `hosts: all` + `--limit` trick here) and
  sets `ignore_unreachable: true`.
* Role-owned tasks get the same `vars_files:` treatment `roleVarsFiles`
  already provides for `tangsible template` - detected via the same
  `rolePathPattern`/`roleFromPath` the drill-down's own `Role:` line
  already uses.
* One injected task: `ansible.builtin.template`, `delegate_to: localhost`,
  `src:` a temp `.j2` file holding the task's own source text (from
  `sourceIndex[task.Path]` - the same text "Task definition" already
  shows) with every `{{ expr }}` wrapped as described above, `dest:` a
  scratch output file, read back once the run completes.
* The `{{ }}`-wrapping transform is a regex-based heuristic, not a real
  Jinja lexer - same "documented, not chased to 100%" tolerance this
  project already applies elsewhere (`colorizeYAML`'s key-line matcher,
  `taskLabel`'s truncation). Known blind spot: a string literal that
  itself contains a literal `}}` would confuse the boundary-finding: not
  worth a full tokenizer over.
* A `{% %}` block (a Jinja statement, as opposed to a `{{ }}` expression)
  is left untouched - if one references an undefined variable, the whole
  render fails; the failure's own error text is shown in the new section
  rather than hidden or crashing anything else.

## Triggering, caching, and display

* Kicked off automatically the moment a drill-down opens (or navigates to
  a different host/task via Left/Right/n/p) - not gated behind a keypress.
  Runs on its own goroutine (the same `QueueUpdateDraw`-on-completion
  pattern already used elsewhere in this app), so opening the view is
  never blocked on it.
* Cached per `(task, host)` for the lifetime of the current generation -
  revisiting the same row a second time is free. The cache is cleared
  wherever `state.Reset()` already runs (a rerun), so a stale render from
  a previous generation's own vars can never linger.
* Shown as a new tab, placed directly after "Task definition" (raw and
  resolved side by side, for easy comparison) - see design-docs/Tabbed
  UI.md for the tab-based drill-down view this section predates.
* **No "Resolving..." placeholder** - the tab is entirely absent from the
  tab bar until the background resolve has actually finished with
  something worth showing (`resolvedTabHidden`, `tui.go`). An earlier
  revision of this feature showed the tab immediately with placeholder
  text, on the reasoning that "something is happening" is itself useful
  feedback - reverted after real use showed the opposite: a user who
  tabbed onto it while it still read "Resolving..." would sometimes watch
  it disappear out from under them a moment later (whenever resolving
  finished identical to source, see below), which reads as the UI
  glitching rather than as intended behavior. A tab that simply never
  appears for a task with nothing to show reads as "this task doesn't
  have one," which is the truth.
* **Omitted entirely** once resolving finishes and comes back byte-for-byte
  identical to "Task definition"'s own raw source - a real, common
  outcome, not an edge case: it happens whenever a task has no `{{ }}`
  expressions at all, and equally whenever every expression it does have
  falls back to its own literal text via the `default()` wrapper above
  (e.g. a play-level `vars:` entry, which this mechanism's stub genuinely
  can't see - see "Known gaps" below). In both cases the tab would show
  nothing "Task definition" doesn't already, so it's left out rather than
  shown as a pointless duplicate. Still shown (once resolved, not before)
  on a genuine resolve error - that's real information of its own, not a
  duplicate of anything else on screen.

## Known gaps (accepted, not chased further here)

* No visibility into `register`/`set_fact`/loop-accumulated state from
  earlier in the same real run - see "Scope" above.
* Loop variables (`item`, etc.) always fall back to their own literal
  `{{ item }}` text, per the design's own `default()` fallback - expected,
  not a bug.
* A handful of module parameters don't survive into a resolvable form at
  all regardless of approach (e.g. `ansible.builtin.copy`'s own `content:`
  gets swapped for an internal temp-file reference before ansible ever
  reports it back) - out of scope for this mechanism since it's not
  actually about *this* task's own source being re-rendered, it's about a
  different, unrelated field ansible itself doesn't echo back.

## What's next

This is intentionally scoped to the mechanics only - there are further
ideas for what could build on top of this (a more powerful, opt-in,
full-fidelity mode that actually re-runs the playbook for real for full
`register`/`set_fact` visibility was discussed and deliberately deferred,
not ruled out) once this first version is in place and has proven itself
useful in practice.
