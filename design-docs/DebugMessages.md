# Debug messages

Status: **implemented** (see "Implementation notes" at the end).

Not to be confused with `tangsible debug` (`DebugMode.md`), which drives
ansible-core's task debugger. This is about the output of the
`ansible.builtin.debug` *module*.

## Goal

A playbook run interactively often uses `ansible.builtin.debug` to tell the
person running it something: a version, a path, a computed value, "next step:
...". Today that output is only visible in the drill-down (and, truncated, in
the host line of an *expanded* task), so it goes unnoticed. Two changes make
it noticed:

1. **While the run is going:** a debug task that produced output is expanded
   by itself, so its host lines (which already show the start of the message,
   `uikit.PrimaryOutputField`) are visible without a keypress.
2. **When the run has ended:** the recap gets a **Messages** section that
   lists every debug message of the run, in run order, whether or not the run
   succeeded. You see them even if you did not watch.

Decided against (for now): printing the full message under each host line in
the tree. With several hosts it repeats the same text per host and multi-line
messages would take over the screen; the recap section does the same job once,
and deduplicated.

## What counts as a debug message

A host's result for a task counts when **all** of these hold:

- the result's `action` is the debug module (`uikit.ModuleShortName` is
  `debug`: `debug`, `ansible.builtin.debug` and `ansible.legacy.debug` alike);
- the outcome is OK (not skipped, failed or unreachable). This is what drops
  a task skipped by `when:`, and a `verbosity:` higher than the run's: Ansible
  reports that as skipped (`Verbosity threshold not met.`), so nothing extra is
  needed for it;
- the result is not censored (`no_log: true` replaces the whole result with a
  `censored` note; there is nothing of ours to show and it must stay hidden);
- the text is not empty. It is the one `uikit.PrimaryOutputField` already
  decides on: `msg:` as a string, a list (joined by newlines) or any other
  value (pretty JSON), or the `var:` form, via `DebugValueText`/`DebugVarValue`.
  Nothing new is decoded differently from the drill-down.

`fail` and `assert` are different modules and are not debug messages (their
text is already the failure message in the recap's `failed` category).

**Loops.** A debug task with `loop:`/`with_*` reports one result per item
(`results[]`, each with its own `msg` or `var` and `item`). Each item result
that passes the rules above is one message of that host's result; a skipped
item is skipped. The top-level `All items completed` is not a message.

## Behavior

### Auto-expanding the task (live)

When the first host result of a task that counts as a debug message arrives,
the task is expanded, once. After that it is an ordinary task: the user can
collapse and expand it as usual, and a later host's result never re-expands it.

Pitfall to keep out: `uikit.InheritedExpandState` makes a new task inherit the
expand state of the previous one (that is what makes `E` sticky while the run
is going). A task expanded by this feature must **not** pass that on, or every
task after a debug task would be expanded. So the expansion is remembered as
*automatic* (`autoExpanded`) and the inheritance reads "expanded and not
automatic". `E` and `C` hand every automatic expansion over to the user (the
mark is cleared; the task stays "decided", so a later host's result never
expands it again). One case stays automatic on purpose: collapsing such a task
by hand and expanding it again; its successor then does not inherit it, which
costs nothing.

Hosts whose result is not a message (skipped, unreachable) still show their
usual host line in the expanded task. Nothing else about the tree changes: the
host line keeps showing the start of the message as it does today.

### The Messages section (recap)

Only in the frozen state, below the per-host summary lines of the recap (the
same place the recap's rows already live, `flattenRecapRows`), and only when
the run produced at least one message. It does not depend on the exit code and
is shown for a failed or interrupted run too.

When the run ends, the cursor lands on the first message (not on the end of
the recap), and Enter on a message opens the task's drill-down directly on its
Output tab.

```
Messages (3)
============

  play: Configure web servers
    Show version                         all 5 hosts
      nginx 1.24.0
    Show path                            web1, web2, +3 hosts
      /srv/www/a
      ...
    Report                               db1
      first line of a longer message
      second line
      ... (+4 more lines)
```

- The heading is underlined with `=` characters and followed by a blank row,
  like the "Summary" heading above. The section heading is a row; it starts **expanded** (it is the point of the
  feature). Enter or Left collapses it, Enter or Right expands it, like the
  recap's host rows; `E`/`C` expand/collapse it with everything else.
- One group row per group of hosts, in run order (play order, then task
  order within the play; handlers where they ran), under its play name (shown
  once per play). The first group's row carries the task name and its hosts
  ("Show version  all 5 hosts"), further groups only their hosts. The play
  name, the message lines and the "more groups" row are not interactive: the
  cursor skips them.
- **Hosts with the same message are grouped.** The group names all hosts if
  every host that ran the task has it ("all 5 hosts"), otherwise the first
  three by name and "+N hosts". Hosts whose messages differ form separate
  groups, in alphabetical order of their first host (hosts finish in any order,
  so arrival order would change the section from run to run). At most three
  groups per task are listed; the rest is "... (+N more groups)", a plain row.
- Each message is shown with its lines as they are, at most **5 lines** per
  message, then `... (+N more lines)`. A long line is cut to the width like any
  other row; the full text is in the drill-down.
- For a loop, the items of a group are listed one after another (the message
  of each item; the item value, when there is one, in front: `item1: text`),
  at most three, then `... (+N more items)`.
- Enter on a message row opens the drill-down for that task on the group's
  first host (`showOutput(task, host)`), as the recap's task rows do.
- Colors: the heading and play prefix use `heading`/`standard_text`, the host
  list `annotation`, the message text `standard_text`. No new `[colors]` keys.
- Revisit and a finished run behave the same: the section is built purely from
  `PlaybookState`, with no data of its own.
- The tree filter (`f`) does not affect the section: it lists what the run
  produced, like the rest of the recap.

## Code plan

- `internal/uikit/debugmessages.go` (pure, next to `DebugValueText`):
  `DebugMessagesOf(raw json.RawMessage) []string` returns the messages of one
  host result under the rules above (nil when none: other module, not OK,
  censored, empty); `CollectDebugMessages(state)` returns, per task in run
  order, the groups (`[]DebugGroup{Hosts, Messages}`), applying the grouping.
- `internal/session/livesession_events.go`: in `onHostRecorded`, the one-time
  expansion with `autoExpanded` (`map[*playbook.TaskNode]bool`); the
  inheritance in `onTaskAdded` ignores automatic expansions; the toggles in
  `livesession_nav.go` (and `E`/`C`) clear the mark. Event-loop only, no lock.
- `internal/session/recap.go`: the Messages rows after the host summary rows,
  with their own row ids and expand state like `recapHostExpanded`.
- `design-docs/Recap.md` gets a pointer to this document, `Codemap.md` the new
  file, `README.md` and `Changelog.md` one line each. No new keys, so no help
  table, completion or man-page change.

## Tests

- Pure (`debugmessages_test.go`): a `msg:` string, list and dict; the `var:`
  form; skipped (also by `verbosity`); failed and unreachable; `no_log`
  (censored); empty message; `ansible.legacy.debug`; a loop with some items
  skipped; another module's `msg` (a `command` task) is not a message;
  grouping: all hosts the same, all different, three hosts plus more, group
  order, line and item caps.
- The auto-expansion as a pure decision function plus a unit test of the
  inheritance (a debug task does not make its successor expanded; a manual
  collapse sticks when the next host reports; `E`/`C` clear the mark).
- Recap rows: heading, play prefix, grouping text, collapsed state, absent when
  there are no messages.
- e2e (tmux, `go test -tags e2e`): a new fixture `testdata/debugmessages.yml`
  with the cases above on a multi-host inventory (`testdata/multihost-
  inventory.ini` if its hosts are local), run to the end, checking that the
  debug task was expanded while running and that the recap section lists the
  expected groups.

## Open points

- **An opt-out.** No config key in the first version. If expanded debug tasks
  prove noisy (`debug` used as a progress marker), a
  `[general] expand_debug_tasks` or a `debug_messages = "tree" | "recap" |
  "both" | "off"` is the obvious place; add it only when it is wanted.
- **A page for the messages** (a key like `s` for statistics) with the full,
  uncut text, if the recap rows with their caps turn out too small.
- **`ansible.builtin.fail`/`assert` messages** are not collected (see above);
  they are already in the recap as the failure text.

## Implementation notes

- Real result shapes (checked against the bundled callback plugin): `action`
  is the short name `debug`; a skipped (`when:`, `verbosity:`) result has
  `skipped: true`; a `no_log` result is only `{"censored": ...}`; a loop's
  items carry `_ansible_item_label`, `skipped`, `msg` and have no `action`
  of their own, so the module is read from the top-level result.
- The Messages section's rows are `messages*RowID` (`session/debugmessages.go`);
  the group and heading rows have their own selected styling in `rebuild`
  (`messagesRowSelectedText`). The pure part is `uikit/debugmessages.go`.
- `testdata/debugmessages.yml` is the fixture of the e2e test
  (`e2e/debugmessages_test.go`: the section, the expansion, the heading's
  Enter, and a failed run).
- Not done, as specified: no config key, no full-screen page.
