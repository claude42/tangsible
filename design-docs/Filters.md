# Filter

## User story

As user I to filter tangsible's output in order to more efficiently analyze
the results of the playbook I just ran. Depending on the situation, I want to
filter for

* "All" - shows all Tasks - this is the default
* "Changed" shows all tasks that in status "Changed" or "Failed"
* "Failed" shows only tasks in status "Failed"
* "Contents" show only tasks that contain the search term in either their
  title, ansible command or Output

## Acceptance criteria

* Keyboard shortcut `f` opens the Filter dialog (All/Changed/Failed) and
  `/` opens the separate Search dialog (see Dialog section below - not one
  combined dialog). Not `F` - that's already bound to "resume autoscroll"
  and keeps that meaning unchanged.
* Different filters can not be combined (i.e. "Failed" with a specific
  content is *not* possible)
* When a filter is selected, only the tasks matching that filter shall be
  displayed
* A task's status, for filtering purposes, is host-level: a task matches
  "Changed"/"Failed" if *at least one* of its hosts has that outcome (a task
  can have hosts in different states). When a task matches, all of its
  hosts are shown - including ones that don't themselves match the filter -
  not just the matching ones.
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

### Filter dialog (a/c/f)

* Keyboard shortcut `f` brings it up
* Dialog headline should be something like "Select filter"
* The three filters are displayed, together with their respective keyboard
  shortcut
  * A - Show all
  * C - Show all changes and failed tasks
  * F - Show only failed tasks
* When the user presses a, c, or f the respective filter shall be activated
  and the window shall be closed again
* When the user presses Esc, the dialog shall be closed with no changes to
  the filter settings
* When the user presses q, the dialog shall be closed with no changes to
  the filter settings - same as Esc. The playbook shall NOT be aborted.
* When the user presses Ctrl-C, the dialog shall be closed with no changes
  to the filter settings, AND the playbook shall be aborted (interrupted if
  still running, or tangsible quit if not) - same as Ctrl-C always does
* A task is considered failed if it failed for at least one host (same
  host-level rule for "changed" - see Acceptance criteria above)

Rationale for the q/Ctrl-C split: in practice, q is frequently pressed with
only the intention of closing the dialog, out of habit - not a real intent
to quit tangsible or abort the playbook. Ctrl-C is different: pressing it,
the intention is unambiguous (abort), so it's fine for it to do both -
close the dialog and abort - at once.

### Search dialog (full-text search)

* Keyboard shortcut `/` brings it up
* Consists of only a headline and the search text box - no A/C/F menu
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

## Contents/M filter - implementation notes

Matches against three things, case-insensitively, plain substring (no
regex): the task's own title; its source as written in the playbook (the
same YAML tangsible's own output drill-down already shows under `TASK:`);
and any host's Output (the same single field - stdout, or msg as a
fallback - the drill-down view and the collapsed OK/Changed line both
already call "the output"). Same host-level "at least one host" rule as
Changed/Failed. An empty search term behaves like "All" (matches
everything) rather than hiding everything, so clearing the box and hitting
Enter by accident doesn't look broken.

The interaction with the auto-jump-to-failed-host behavior (previously an
open question here) is resolved: implemented as "filter wins, skip the
auto-jump" - if the failed task the auto-jump would land on doesn't match
the currently active filter, the jump is skipped and the cursor is left
wherever normal autoscroll/navigation would otherwise put it. This can
only actually happen with the search filter active - a failed task always
matches "Changed" and "Failed" by definition, so A/C/F never trigger it.
