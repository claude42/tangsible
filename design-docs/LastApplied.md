# Last applied

## Situation

As a user I frequently have the question when a role was last applied to a
specific host (or if at all). AWX, Semaphore or ARA do collect all this data
and can provide the answers - but all these solutions are very heavy weight.

## Proposed solution

Remember, per project, the latest time each role was applied to each host, and
show it in a new **Roles** tab of `tangsible host`. Where else to surface it
(`hosts`, `inventory`) is decided later.

This is a latest-state table, not a log: one record per (host, role), updated
in place. No history, no rotation, no task- or play-level data. The run logs
under `.tangsible/runs/` can't answer the question - they keep only the last
20 runs per playbook - so this needs its own small store.

## Scope

- **Per project only.** Stored under `.tangsible/` and gitignored, like
  `state.toml`. It reflects what was run with tangsible from this checkout.
  Other checkouts, CI, AWX etc. are out of scope; ARA or AWX are the right
  tools for that.
- **No role-change detection in v1.** Comparing the git commit of a role's
  directory against the one recorded at apply time ("changed since it last
  ran") is a possible follow-up. Role dependencies (`meta/main.yml`) and
  inventory vars change behavior without touching the role's files, which
  has no good answer yet.

## The unit is the role

A role's name comes from the code (`task._role`), is stable across edits, and
is what the question is about. A play name is free text and renaming it
would orphan its record. It also makes the `role` verb record naturally
(its generated stub playbook has no meaningful play name).

Consequences:

- A play with several roles yields one record per role.
- Dependency roles and `include_role`/`import_role` get their own records,
  since their tasks carry their own role name.
- Tasks outside any role (`tasks:`, `pre_tasks:` of a role-less play) have no
  unit and are not recorded. If that ever matters, "play name when there is
  no role" can be added as a fallback unit without changing the table.

## What counts as applied

Decided per host, once a generation has finished:

- **Applied:** at least one of the role's tasks on that host ended ok,
  changed or failed. A failed application counts (recorded with outcome
  `failed`); `--tags`/`--skip-tags` that run only part of a role still count.
- **Not applied:** every task of the role was skipped on that host, or the
  host was unreachable and nothing ran.
- **`--check` runs record nothing**, for any host. Detected with the existing
  check-mode detection (`CheckMode.md`).
- **`--limit`** needs no special handling: only hosts that appear in the
  run's events are touched, all other records stay as they were.

## Data model

One record per (host, role):

| Field          | Meaning                                                      |
|----------------|--------------------------------------------------------------|
| `last_applied` | Start time of the generation that applied the role. Start, not end: it is well-defined for an interrupted run and closer to "when did the role run". |
| `last_changed` | Last time any task of the role reported `changed` on that host, so "applied, nothing to do" stays distinguishable. |
| `outcome`      | Outcome of the latest application: ok, changed or failed.    |

A role that never applied to a host has no record and is not shown for it.

Its own file next to `state.toml` (working name `roles.toml`), so
`state.toml`'s history semantics stay untouched. Written only by tangsible
and rewritten as a whole after each recording generation - the amounts are
tiny (~10 hosts).

## Recording

- **Written from the Go side only.** The callback plugin does not write
  anything; it only has to emit the role name with task events (a small
  schema addition, kept optional so the plugin stays compatible with
  `ansible.posix.jsonl` consumers). `PlaybookState` aggregates per
  (host, role); the session writes the table when a generation ends, on the
  event-loop goroutine like every other state mutation.
- **Which verbs record:**
  - `run`, `rerun`, `role`: yes.
  - `revisit`: only when the revisited play is run again from inside it
    (that is an ordinary rerun). Browsing never records.
  - `debug`: never.
  - `host`, `hosts`, `inventory`, `template`, `vault`, `version`, `help`: no.
- One-shot scrapes (`--list-tasks`, Resolved, File) never record.

## Roles tab (`tangsible host`)

A new tab listing the roles recorded for that host: role name, last applied,
last changed, last outcome. Roles never applied to the host are not listed.
Empty tab omitted, like the drill-down's.
Sorted by role name; a failed outcome is colored like failures elsewhere.
