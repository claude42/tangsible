# Filter

## User story

As user I want to filter tangsible's output in order to more efficiently
analyze the results of the playbook I just ran. Depending on the situation, I
want to filter for

* "All" - shows all Tasks - this is the default
* "Interesting" shows all tasks that are
  * in status changed, failed, uncreachable or
  * have stderr output or
  * have a warning
* "Changed" shows all tasks that in status "Changed"
* "Failed" shows only tasks in status "Failed"
* "Contents" show only tasks that contain the search term in either their
  title, ansible command or Output

## Acceptance criteria

* Keyboard shortcut `f` opens the Filter dialog (All/Interesting/Changed/
  Failed) and `/` opens the separate Search dialog (see Dialog section
  below - not one combined dialog). Not `F` - that's already bound to
  "resume autoscroll" and keeps that meaning unchanged.
* Different filters can not be combined (i.e. "Failed" with a specific
  content is *not* possible)
* When a filter is selected, only the tasks matching that filter shall be
  displayed
* A task's status, for filtering purposes, is host-level: a task matches
  "Changed"/"Failed"/"Interesting" if *at least one* of its hosts has the
  relevant condition (a task can have hosts in different states). When a
  task matches, all of its hosts are shown - including ones that don't
  themselves match the filter - not just the matching ones.
* While the playbook is still running, the currently in-progress task is
  always shown regardless of the active filter, even if it doesn't (yet)
  have a host result matching that filter
* If a play does not contain any tasks (after applying the filter) then the
  play line shall not be displayed
* The title bar shows the currently selected filter
* When navigating with n/p in the drill down window, tasks which don't match
  the filter shall be skipped
* If the row the cursor is on disappears because a newly-applied filter
  excludes it, the cursor moves to the nearest still-visible ancestor -
  matching the existing fallback when collapse-all removes the
  currently-selected host row

## Dialog

Two separate dialogs, not one combined one (reworked from an earlier
single-dialog design after live use showed it was too easy to hit the
wrong key).

Both are small windows displayed on top of the current treeview, and both
are modal: mouse clicks on the treeview underneath are blocked while
either is open. Esc and Ctrl-C behave the same way in both dialogs; `q`
does not, and is the one place the two dialogs genuinely differ (see
below).

### Filter dialog (a/i/c/f)

* Keyboard shortcut `f` brings it up
* Dialog headline should be something like "Select filter"
* The four filters are displayed, together with their respective keyboard
  shortcut
  * A - Show all
  * I - Show interesting tasks (changed, failed, unreachable, have
    stderr output, or have a warning)
  * C - Show only changed tasks
  * F - Show only failed tasks
* When the user presses a, i, c, or f the respective filter shall be
  activated and the window shall be closed again
* When the user presses Esc, the dialog shall be closed with no changes to
  the filter settings
* When the user presses q, the dialog shall be closed with no changes to
  the filter settings - same as Esc. The playbook shall NOT be aborted.
* When the user presses Ctrl-C, the dialog shall be closed with no changes
  to the filter settings, AND the playbook shall be aborted (interrupted if
  still running, or tangsible quit if not) - same as Ctrl-C always does
* A task is considered failed if it failed for at least one host (same
  host-level rule for "changed"/"interesting" - see Acceptance criteria
  above). Unreachable counts toward "failed" and "interesting", not
  toward "changed".

Rationale for the q/Ctrl-C split: in practice, q is frequently pressed with
only the intention of closing the dialog, out of habit - not a real intent
to quit tangsible or abort the playbook. Ctrl-C is different: pressing it,
the intention is unambiguous (abort), so it's fine for it to do both -
close the dialog and abort - at once.

### Search dialog (full-text search)

* Keyboard shortcut `/` brings it up
* Consists of only a headline and the search text box - no A/I/C/F menu
* The search box shall have keyboard focus as soon as the dialog opens -
  no extra step needed before typing
* If the dialog is opened while a search filter is already applied, the
  text box shall already contain the previous search term
* When the user presses Enter, the typed search term is applied as the
  filter and the window is closed
* Esc/q/Ctrl-C behave the same as in the Filter dialog above - EXCEPT that,
  since this box must accept arbitrary text, q is never treated as "close
  the dialog" here: it always types a literal q into the search term
  instead. Only Esc and Ctrl-C can close this dialog once it's open.

## Interesting filter - implementation notes

"Has stderr output" and "has a warning" are both host-level facts,
computed once per host when that host's result for a task is recorded
(`TaskNode.Warnings`/`TaskNode.HasStderr`, `internal/playbook/
aggregate.go`) rather than decoded from raw JSON on every redraw - the
same "compute once at ingestion, not on every render" approach
`Warnings` already used before this filter existed, for a measured
performance reason (a large frozen recap's own CPU time was mostly spent
re-decoding the same JSON on every cursor move). "Has a warning" means
Ansible's own `warnings` result field is present and non-empty (a plain
string or a list with at least one non-empty entry); "has stderr output"
means the `stderr` result field (command/shell-family modules only) is a
non-empty string. "Interesting" matches a task if *any* of its hosts has
Changed, Failed, or Unreachable status, a warning, or stderr output - the
same host-level "at least one host" rule the other filters use.

## Contents/M filter - implementation notes

Matches against three things, case-insensitively, plain substring (no
regex): the task's own title; its source as written in the playbook (the
same YAML tangsible's own output drill-down already shows under `TASK:`);
and any host's Output (the same single field - stdout, or msg as a
fallback - the drill-down view and the collapsed OK/Changed line both
already call "the output"). Same host-level "at least one host" rule as
Changed/Failed/Interesting. An empty search term behaves like "All" (matches
everything) rather than hiding everything, so clearing the box and hitting
Enter by accident doesn't look broken.

The interaction with the auto-jump-to-failed-host behavior (previously an
open question here) is resolved: implemented as "filter wins, skip the
auto-jump" - if the failed task the auto-jump would land on doesn't match
the currently active filter, the jump is skipped and the cursor is left
wherever normal autoscroll/navigation would otherwise put it. A failed
task always matches "All", "Interesting", and "Failed" by definition, so
those three never trigger it - but "Changed" now can (a task that failed
without ever reporting Changed on any host no longer matches "Changed",
now that it's changed-only rather than changed-or-failed), on top of the
search filter, which could already skip the jump before this filter
existed.
