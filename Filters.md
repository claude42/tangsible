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

* Keyboard shortcut `/` opens a dialog (see below) where the desired filter
  can be selected. (Not `F` - that's already bound to "resume autoscroll"
  and keeps that meaning unchanged.)
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

* Keyboard shortcut / brings up the filter selection dialog
* It shall be a small window displayed on top of the current treeview
* The dialog is modal: mouse clicks on the treeview underneath are blocked
  while it's open. `q`/Ctrl-C still quit the whole app immediately even
  while the dialog is open - consistent with how quitting already isn't
  blocked by the output drill-down view either.
* Dialog headline should be something like "Select filter"
* And then the four possible filters are displayed - together with their
  respective keyboard shortcut
  * A - Show all
  * C - Show all changes and failed tasks
  * F - Show only failed tasks
  * M - Match search string
* Next to (or below) the "Match search string" shall be a text box
* When the user presses A, C, F the respective filter shall be activated and
  the window shall be closed again
* When M is pressed, curshor shall be in the text box and the user can enter
  the search text. When the user presses Enter, the search term will be
  applied and the window will be closed
* If the dialog is opened while the M filter is already applied the text box
  shall already contain the previous search time (but still has to be
  activated by pressing M first
* When user presses ESC, the dialog shall be closed with no changes to the
  filter settings
* A task is considered failed if it failed for at least one host (same
  host-level rule for "changed" - see Acceptance criteria above)

## Open question (deferred to the "Contents"/M step)

Once the search filter (M) exists, a task with a failed host could
theoretically not match it, which would conflict with the existing
auto-jump-to-failed-host behavior on a genuine failure. Leaning towards
"filter wins, skip the auto-jump" when we get there - simpler to implement
(just gate the existing jump on the target still matching the active
filter) and doesn't make the filter's own promise ambiguous. Doesn't apply
to the A/C/F filters in this first step, since a failed task always matches
both "Changed" and "Failed" by definition.
