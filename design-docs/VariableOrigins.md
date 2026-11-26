# Variable origins (where was this value defined?)

Status: **implemented for `debug` and for `host`/`hosts`/`inventory`;
not available in `run`/`rerun`/`role`** (updated 2026-11-26). The findings
below are from a live probe against ansible-core 2.19.11 (2026-10-27); "As
built" says what exists and where, the sections after it are the original
research, kept for the reasoning.

## As built

| Where | What it shows | How the origin is fetched |
|---|---|---|
| `debug`: `v` watch panel | `(label)` after the value of the expression's root variable; `(just set)` / `(set in debugger)` after an `expr = value` | `tangsible_snapshot`'s `origins`, one batched query per pause (`debugOriginCache`) |
| `debug`: drill-down Variables tab | every non-fact, non-builtin top-level variable of the paused task as sorted YAML, `# label` comment each (registered results `# registered`) | the same snapshot and origins, no extra query (`livesession_variables.go`) |
| `host`/`hosts`/`inventory`: Variables tab | inventory-level variables only, `# file:line` or `# -e` | a one-task stub play running tangsible's own action plugin (`callback/action/tangsible_vars.py`) |

Shared pieces: the callback plugin's `tangsible_snapshot` /
`tangsible_variables` (the Python side, `Origin` read by tag name), and
`internal/origin` (labels: project-relative path and line, `-e`; a path in
the debug shadow copy is mapped back to the real tree and the lines inserted
for `debugger: always` are subtracted). Details: `Data Tagging.md` ("Design",
"Variables tab on the host verb"), `HostVerb.md` ("Variables tab"),
`DebugMode.md` ("Variables tab in the drill-down").

What the snapshot reports per top-level variable: `{path, line}`, `{desc}`
(`-e`), or a coarse `kind` - `fact`, `builtin` (ansible's own, a fixed list;
checked before the tag) or `registered`. Top-level only, the winning
definition only; nested leaves (the research below found they carry their
own origins) are not shown.

**Not available in `run`, `rerun`, `role`.** Origins exist only inside the
ansible process, and nothing in the normal event stream carries them. `debug`
reads them at the debugger prompt, `host` runs a stub play with an action
plugin. For a normal run the callback plugin could emit them with each task
(it has `task_vars`), but that adds a variable snapshot to every task event
(size, secret masking, ansible-core 2.19+ only) - not built. A cheaper
alternative would be an on-demand probe like the Resolved/File tabs use,
limited to what a probe run can see (static context, no `register`/`set_fact`
state). Not decided.

## Idea

Show, next to a variable's value, the file and line it was defined in
(`group_vars/web.yml:1`, `pb.yml:5`, `<CLI option '-e'>`). Natural homes:

* `debug`'s `v` watch explorer (`livesession_watch.go`,
  `debugwatch_eval.go`): one extra line or suffix per watched expression.
* Possibly the Resolved tab, which today shows only values.
* A **Variables tab** in the debug drill-down: the full list of
  `task_vars` with values *and* origins (see "Variables tab" below).

Answers the question Ansible's own precedence rules make hard: "which of my
twenty places for this variable actually won?"

## Background: Data Tagging (ansible-core 2.19+)

2.19 attaches metadata ("tags") to values as they flow through Ansible.
Classes live in `ansible._internal._datatag._tags` (private API):

* `Origin(path, line_num, col_num, description)` - where the value was
  defined. Used by ansible-core for error messages.
* `TrustedAsTemplate` - string may be run through Jinja (replaces `!unsafe`
  as the trust model; module results/lookups/facts are untrusted).
* `VaultedValue`, `SourceWasEncrypted` - vault tracking.

Tagged values are subclasses of the plain types (`_AnsibleTaggedStr`,
`_AnsibleTaggedDict`, `_AnsibleTaggedList`, ...), so they behave like normal
`str`/`dict`/`list` everywhere else. Read tags with
`ansible.module_utils._internal._datatag.AnsibleTagHelper.tags(value)`, which
returns a `frozenset` of tag instances.

## Finding: Origin survives into the debugger's `task_vars`

Probe: a throwaway playbook with `debugger: always` on a task, a vars
source of every kind, and `AnsibleTagHelper.tags(task_vars[k])` evaluated at
the debugger prompt (stdin piped).

| Source of the variable | Origin |
|---|---|
| play `vars:` | playbook file, line, col |
| `vars_files:` | vars file, line, col |
| `group_vars/` | file, line, col |
| `host_vars/` | file, line, col |
| inventory host var (`inv_var=x` in `.ini`) | inventory file, line, **no col** |
| `set_fact` | playbook line of the `set_fact` value |
| task-level `vars:` | playbook line |
| `-e key=value` | no path; `description="<CLI option '-e'>"` |

* **Containers and their members carry their own origins**: `play_dict` is
  tagged at the mapping's line, `play_dict['k']` at the key's own line/col;
  `gv_list[0]` at its own line. A dotted/indexed watch path therefore gets
  the origin of the exact leaf, not just the top-level variable.
* **Magic/computed variables have none** (`inventory_hostname`,
  `ansible_check_mode`): the tag set has no `Origin`; show nothing.
* Registered results, facts and anything from lookups carry no `Origin`
  (confirmed later, see `Data Tagging.md`'s spike findings); the snapshot
  labels them by kind instead (`registered`, `fact`).
* Only 2.19.11 tested. Older ansible-core has no tags at all.

## How to fetch it (no new transport)

`debugrun.Session.Query` (`internal/debugrun/session.go`) already evaluates an
arbitrary expression in the debugger scope and wraps it as
`p print(json.dumps(<expr>, default=str)) or 0`. Today's value queries lose
the tags, because a tagged `str` JSON-dumps as a plain string. A query of the
same shape returns origins fine (verified):

```
{k: [[t.path, t.line_num, t.col_num, t.description]
     for t in __import__('ansible.module_utils._internal._datatag',
                         fromlist=['x']).AnsibleTagHelper.tags(task_vars[k])
     if type(t).__name__ == 'Origin']
 for k in (...)}
```

Result (abridged): `{"play_var": [["pb.yml", 5, 15, null]], "gv_var":
[["group_vars/web.yml", 1, 9, null]], "xv_var": [[null, null, null,
"<CLI option '-e'>"]], "inventory_hostname": []}`.

Match tags by `type(t).__name__ == 'Origin'` rather than importing the class,
so the one-liner needs only the `AnsibleTagHelper` import.

## Variables tab (full list: values + origins)

Built (see "As built"); the original idea: while paused, an extra drill-down tab listing every variable in the
paused task's `task_vars`, with value and origin. Cost was measured, not
guessed (ansible-core 2.19.11, localhost, `gather_facts: true`, inside the
debugger):

* `task_vars` is ~154 top-level keys (112 of them under `ansible_facts`),
  ~112 KB as JSON.
* Origins for all top-level keys: ~0.2 ms (one `AnsibleTagHelper.tags` call
  per key), ~5.7 KB of output.
* Origins via a recursive walk (~9,100 nodes, depth limit 6): ~55 ms,
  ~7.4 KB of output.
* Only 14 top-level keys had an `Origin` - the ones from the playbook, vars
  files, group/host_vars, inventory and `-e`. Facts and magic variables
  have none (facts and results are untrusted values, not file-defined).

**The values are already in hand.** `debugCaptureSnapshot`
(`internal/session/livesession_debug.go`) already runs
`Query("task_vars")` - the whole dict - on every pause (since the
`--all-vars` flag was removed) and caches it in `debugTaskVarsCache`,
keyed by (task, host), merged with the registered result. The ~112 KB
transfer is therefore already paid; the tab only needs one more small
origin query next to it. The expensive part of this feature would be moving
big payloads over the debugger pipe (`session.go` records a past ~20 s
stall on a >100 KB response, fixed by bounding the prompt scan to the last
`promptCheckTail` bytes) - an origins-only response of a few KB avoids that
entirely. Don't send values a second time.

**It also answers the limitation in `Showallvariables.md`.** That doc
paused because a separate stub run can only show *static* context (no
`register`/`set_fact` state from the same run). The debugger's `task_vars`
is the live state at the pause, so in debug mode this tab shows exactly
that.

Open design points:

* Rendering ~150-200 rows with large values (`ansible_facts`,
  `ansible_env`): needs per-row collapse/truncation; in-tab search and copy
  already exist for tabs.
* Ordering/grouping by origin: variables from the user's own files and
  `-e` first, then facts, then magic variables. Leaning toward hiding facts
  behind a toggle, off by default.
* Origins come from one batched query for all top-level names (dict
  comprehension, as in the example above); nested origins only if the tab
  expands a node.
* Untested: behavior in a real paused session through tangsible's own pipe
  (only the stdin-piped debugger prompt was probed), and a host with very
  many facts (many interfaces/mounts) - the origins-only output stays small
  either way, the value rendering is what scales.

## Implementation sketch (original; see "As built" for what was done)

1. In `debugwatch_eval.go`, build the origin query for a watched expression's
   resolved path (same bare-name / dotted / `[idx]` grammar the value
   evaluation already uses), and issue it through `Session.Query` alongside
   the value query - same "only valid for the live pause" constraint as
   the value (`debugIsPaused`), since `task_vars` exists only there.
2. Display as a dimmed suffix or second line (`group_vars/web.yml:1`);
   `Origin.path` is absolute, so show it relative to the playbook/cwd where
   possible (note the demo GIFs leak absolute paths otherwise).
3. Cache per (task, host) pause like the values.

## Risks / open questions

* **Private API.** `_internal` modules can move or change in any ansible-core
  release. Treat any failure (ImportError, AttributeError, a query that
  doesn't produce a JSON line) as "no origin known" and render nothing; never
  surface it as an error, and never let it fail the value query - run it as
  a separate query. A unit test on the query's JSON parsing is feasible;
  the live behavior needs a tmux/e2e probe against real ansible-core.
* **Older ansible-core**: no tags, import fails; same fallback.
* **Cost**: one more debugger round trip per watched expression per pause.
  Target scale makes this fine; batch all watched names into one dict query
  as in the example above.
* **`-e` and inventory** have partial origins (no path / no column);
  display code must tolerate `null` for each field.
* Do origins survive into the other places tangsible reads variables
  (Resolved tab's extra `ansible-playbook` run, callback plugin results)?
  Still unchecked; this document only covers the debugger's `task_vars` and
  the stub play's.
