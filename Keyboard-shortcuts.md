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
* f                - open the filter dialog (All/Changed/Failed - see
                     below). Not available while the command output view
                     is shown.
* /                - open the search dialog (see below). Not available
                     while the command output view is shown.

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

Opened with `f` from the main tree (see Filters.md for the full feature
spec). A plain modal menu - no text entry at all. Mouse clicks are blocked
entirely while it's open.

* a - show all tasks (the default)
* c - show only tasks with a changed or failed host
* f - show only tasks with a failed (or unreachable) host
* Esc / q - close the dialog, no change to the active filter
* Ctrl-C - close the dialog *and* quit/interrupt the playbook, same as
           Ctrl-C always does outside the dialog

(The dialog itself displays a/c/f as A/C/F - shown upper case to stand out
next to each option's description, but typed as lower case.)

Pressing a, c, or f immediately applies that filter and closes the dialog.
`q` deliberately does *not* quit here, unlike everywhere else in the app -
only Esc and q both just close the dialog, since in a plain menu like this
one, pressing q is almost always a reflex to close it, not a real intent to
quit. Ctrl-C is the one exception: since its intent (abort) is always
unambiguous, it closes the dialog *and* still quits/interrupts, exactly as
it would if the dialog weren't open.

## Search dialog

Opened with `/` from the main tree (see Filters.md). Consists of nothing
but a headline and a search text box, which gets keyboard focus immediately
- there's nothing else in this dialog to browse first, so typing works
right away with no separate activation step. Reopening the dialog while a
search filter is already active pre-fills the box with the previous term.
Mouse clicks are blocked entirely while it's open.

* (type freely) - every key except Ctrl-C is typed into the box normally,
                   including letters that are shortcuts elsewhere - most
                   importantly `q`, since a real search term can contain
                   the letter q (e.g. "request"). Unlike the filter
                   dialog's plain menu, q is never treated as "close" here.
* Enter - apply the typed term as the search filter and close the dialog.
          Matches a task if the term appears (case-insensitively) in its
          title, its own source, or any host's output - same host-level
          "at least one host" rule as the changed/failed filters. An empty
          term behaves like "show all" rather than hiding everything.
* Esc / Tab - cancel back out: no change to the active filter
* Ctrl-C - cancel back out *and* quit/interrupt the playbook, same as
           Ctrl-C always does outside the dialog

The title bar always shows the currently active filter, from either
dialog. If the row the cursor was on is no longer shown under a newly
applied filter, the cursor moves to the nearest task that still is; n/p
(main tree and command output view alike) and the play-row next/prev
targets in the main tree view section above all skip over tasks the active
filter is currently hiding.

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
