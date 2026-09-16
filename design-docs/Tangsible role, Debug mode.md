# tangsible role: debug mode

## Problem

`design-docs/Drilldown, Resolved Values.md` resolves a task's variables by
feeding its source back into a separate, static stub run
(`delegate_to: localhost`, no mutation of the real run). That approach is
explicitly scoped to "static context only" - it has no visibility into
anything computed at runtime by earlier tasks in the *same* real run
(`register`, `set_fact`, loop-accumulated state), since that only ever
existed in that one specific `ansible-playbook` process's own memory. That
doc's own "What's next" section already anticipated wanting a real,
re-execution-based full-fidelity mode as a bigger, separate undertaking, and
deliberately deferred it. This doc is that undertaking, scoped down to
something buildable: one role at a time, via the existing `tangsible role`
verb.

## Idea

Add a `--debug` flag to `tangsible role <role_name>`. Instead of running the
role unmodified, tangsible inserts

```yaml
- name: All variables
  ansible.builtin.debug:
    var: vars
```

after every task in the role, then runs the *modified* role for real - not a
side stub, the actual execution the user asked for. Because this is a real
run, each injected debug task sees genuine post-task runtime state:
`register`ed values, `set_fact`, anything accumulated so far - the exact gap
the static stub approach can't close.

## Relationship to existing work

- Directly fulfills the deferred "full-fidelity mode" from `Drilldown,
  Resolved Values.md`'s "What's next", scoped to a role instead of an
  arbitrary playbook.
- Reuses `tangsible role`'s own stub-playbook and role-discovery machinery
  (`Tangsible role.md`) rather than inventing new plumbing for "run a role
  standalone."
- Shares `Showallvariables.md`'s open question about payload size and
  whether to show variables unfiltered or trimmed - see below.
- Complementary to, not a substitute for, the already-approved-but-deferred
  own-callback-plugin effort - that's a different avenue (structured access
  to Ansible's own internals) tracked separately.

## Fundamental difference from the static-stub approach

Every existing variable-resolution mechanism in this codebase (`Drilldown,
Resolved Values.md`, `Showallvariables.md`, `tangsible template`) is built
on "never mutates the real run" - a separate, after-the-fact invocation that
only ever observes. `--debug` breaks that rule on purpose: it *is* the real
run, actually executing, actually installing packages / restarting services
/ writing files - just decorated with variable dumps along the way. This
needs to be communicated clearly wherever the flag is documented/surfaced,
so it's never mistaken for a safe, side-effect-free introspection tool the
way the other three are.

## Design decisions (once we proceed)

* **Work on a copied role directory, never the tracked source file.**
  Editing the real `tasks/main.yml` in place - even temporarily, even with
  cleanup-on-exit - risks a crash/SIGKILL/power-loss leaving a mangled
  version of a tracked file behind. `tangsible role` already does
  best-effort role-directory discovery for its stub playbook; `--debug`
  copies the discovered role directory to a scratch location and points the
  stub's role search path there instead, so the instrumented copy is the
  only thing ever touched. Cleaned up the same way the existing role stub
  is - once at process exit, not per-generation.
* **Textual splice, not a YAML round-trip.** `source.go`'s
  `buildTaskSourceIndex` already computes each task's own line boundaries
  (respecting `block:`/`rescue:`/`always:` nesting) for the drill-down's
  "Task definition" section. Reuse that boundary computation to insert the
  debug task's raw YAML text at each computed offset, rather than parsing
  the whole file into a `yaml.v3` tree and re-emitting it - re-emission
  risks mangling comments/formatting in a file that (per the point above)
  is disposable but whose *structure* still needs to stay valid.
* **Injected tasks get `tags: [always]`.** Without it, a `--tags`/
  `--skip-tags` invocation would silently drop the debug tasks along with
  whatever they didn't select, defeating the point of the flag.

## Open questions (not decided yet)

* **`tasks/main.yml` only, or recursive into includes?** Roles commonly
  fan out via `include_tasks`/`import_tasks`, and have a separate
  `handlers/main.yml`. Instrumenting only the top-level file is the smaller
  first version but leaves anything included elsewhere invisible. Worth
  shipping v1 scoped to `tasks/main.yml` only and seeing how much that gap
  actually matters in practice, matching how `Showallvariables.md` shipped
  its own "unfiltered first, see if it's noisy" approach - but not decided.
* **Extra tree rows vs. merged tabs.** The straightforward implementation
  makes each injected debug task its own visible task node - for a
  15-task role that roughly doubles the row count with repeated "All
  variables" rows interleaved among the real ones. A cleaner presentation
  would fold each debug task's result into a new tab on the *preceding*
  real task instead of a separate row, but that means correlating a
  synthetic task's result back onto a different tree node - real
  additional engineering, not a byproduct of the insertion trick itself.
  This is an architecture choice, not a polish pass, so needs deciding
  before implementation starts, not after.
* **`var: vars` vs `var: hostvars[inventory_hostname]`.** The proposal
  above uses the smaller-seeming `var: vars`, but modern Ansible injects
  gathered facts as top-level vars by default (`inject_facts_as_vars`), so
  `vars` may pull in most of the same `ansible_facts` bulk
  `Showallvariables.md` measured at 54KB per dump against a bare localhost.
  Whether to filter the dump down to the "interesting" subset (host_vars/
  group_vars/role defaults/extra-vars/runtime-registered values, excluding
  low-signal `ansible_facts`) or ship unfiltered first is the same open
  question `Showallvariables.md` already left unresolved for its own,
  smaller-scale version of this.
* **`when:` on the debug task.** A debug task inserted unconditionally
  after a task whose own `when:` was false (so the real task didn't run)
  will still fire and report state as if nothing happened. Possibly fine,
  possibly worth copying the preceding task's own `when:` onto the debug
  task - not decided.

## Known gaps (accepted, not chased further here)

* **Real side effects, not a dry run** - see "Fundamental difference"
  above. By design, not a limitation to fix.
* **Loop variables stay opaque.** A debug task placed after a looped task
  only sees post-loop state, never a per-iteration snapshot - the same
  accepted limitation the static-stub approach already has.
* **Payload volume scales with task count × host count.** Unlike the
  per-task "Resolved"/variables tabs (one on-demand subprocess per
  drill-down open), `--debug` bakes a debug dump after every task,
  unconditionally, for every host, all in one live-streamed run. At the
  project's ~10-host target scale this is likely fine; worth being aware
  it's a meaningfully larger jsonl stream than anything else in the app
  today.
* **`block:`/`rescue:`/`always:` interaction.** A debug task placed after
  the last task of a `block:` won't run if that block fails into
  `rescue:` - inherent to how Ansible executes those constructs, not
  something insertion placement can work around.

## Status

Discussed, not yet implemented - written up per the brainstorm, no
implementation started. The two points flagged as load-bearing before any
code gets written: working on a copied role directory (never the tracked
file), and deciding extra-rows vs. merged-tabs, since that changes the
shape of the implementation rather than just its polish.
