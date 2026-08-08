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
