# Keyboard shortcuts

## In the main tree view

* Cursor up / down - move cursor up / down (stop autoscrolling if necessary)
* j / k            - move cursor up / down (stop autoscrolling if necessary)
* Page up / down   - move cursor one page up / down
* Ctrl-F / Ctrl-B  - move cursor one page up / down
* Home / End       - move cursor to top / end of the list (does NOT resume
                     autoscrolling - see F below)
* Ctrl-A / Ctrl-E  - same as Home / End
* G                - move cursor to end of list (same as End - does not
                     resume autoscrolling)
* n / p            - move to previous / next task, expand the task if
                     necessary, if cursor was on a specific host on the
                     current task, position cursor on same host in the new
                     task (falls back to the task's own row if that host
                     hasn't reported a result for it yet). From a play row,
                     next/prev targets that play's own first task, or the
                     previous play's last task
* Cursor right     - expand tree element (no-op if already expanded, or on
                     a host row / play row)
* Cursor left      - collapse tree element; on a host row, collapses its
                     parent task instead and moves the cursor there; no-op
                     on a play row
* Enter / Space    - toggle tree element; on a host row (nothing to
                     toggle), opens its command output instead
* E / C            - expand / collapse all tree elements. If collapsing
                     removes the row the cursor was on (a host row whose
                     task just collapsed), the cursor moves to that task's
                     own row
* q / Ctrl-C       - quit
* F                - move cursor to end of list and resume autoscrolling -
                     the only shortcut that resumes it
* /                - open the filter dialog (see below). Not available while
                     the command output view is shown.

* Mouse wheel      - pan contents up / down (stop autoscrolling if
                     necessary).
* Trackpad gestures- same as mouse wheel
* Single click on tree element - select AND activate it (same as
                     Enter/Space) - expands/collapses a task, or opens a
                     host's command output
* Double click on tree element - not implemented separately from single
                     click - since a single click already activates on
                     first press (including opening a host's output),
                     there's nothing left for a double click to add

## Filter dialog

Opened with `/` from the main tree (see Filters.md for the full feature
spec). Fully modal: no other key does anything while it's open, and mouse
clicks are blocked entirely - except that q/Ctrl-C still quit the whole app
immediately, same as everywhere else.

* a - show all tasks (the default)
* c - show only tasks with a changed or failed host
* f - show only tasks with a failed (or unreachable) host
* m - move the cursor into the search text box below (see below)
* Esc - close the dialog, no change to the active filter

(The dialog itself displays these as A/C/F/M - shown upper case to stand
out next to each option's description, but typed as lower case.)

The search box is always visible in the dialog, but starts unfocused -
pressing `m` is what moves the cursor into it. Once there, every other key
(including q/j/k/etc.) types normally into the box instead of doing
whatever it would do elsewhere - only Ctrl-C is still the global "quit/
interrupt" it always is. Reopening the dialog while a search filter is
already active shows the previous term in the box right away, though it
still isn't focused until `m` is pressed again.

* Enter (while typing) - apply the typed term as the search filter and
                          close the dialog. Matches a task if the term
                          appears (case-insensitively) in its title, its
                          own source, or any host's output - same
                          host-level "at least one host" rule as the
                          changed/failed filters. An empty term behaves
                          like "show all" rather than hiding everything.
* Esc / Tab (while typing) - cancel back out, same as Esc from the menu:
                          no change to the active filter

Pressing A, C, or F immediately applies that filter and closes the dialog.
The title bar always shows the currently active filter. If the row the
cursor was on is no longer shown under the new filter, the cursor moves to
the nearest task that still is; n/p (main tree and command output view
alike) and the play-row next/prev targets in the section above all skip
over tasks the active filter is currently hiding.

## When command output is shown

* Cursor up / down - move cursor up / down
* j / k            - move cursor up / down
* Page up / down   - move cursor one page up / down
* Ctrl-F / Ctrl-B  - move cursor one page up / down
* Home / End       - move cursor to top / end of the list
* Ctrl-A / Ctrl-E  - move cursor to top / end of the list
* G                - move cursor to end of list
* ESC              - close view, go back to tree view
* Cursor left / right - show command output of previous / next host (same task)
* n / p            - show command output of previous / next task (same host)

When the user has navigated inside the command output view (with Cursor left
/ right or n / p) and then exits the view, the cursor position in the
treeview is updated accordingly: the cursor lands on the task / host that
had been displayed in the command output view, and the treeview is expanded
so that row is visible.

* Mouse wheel      - scroll up / down
* Trackpad gestures- scroll up / down
