# Keyboard shortcuts

## In the main tree view

* Cursor up / down - move cursor up / down (stop autoscrolling if necessary)
* j / k            - move cursor up / down (stop autoscrolling if necessary)
* Page up / down   - move cursor one page up / down
* Ctrl-F / Ctrl-B  - move cursor one page up / down
* Home / End       - move cursor to top / end of the list
* Ctrl-A / Ctrl-E  - move cursor to top / end of the list
* G                - move cursor to end of list (but don't resume autoscrolling!)
* n / p            - move to previous / next task, expand the task if
                     necessary, if cursor was on a specific host on the
                     current task, position cursor on same host in the new task
* Cursor right     - expand tree telement
* Cursor left      - collapse tree element
* Enter / Space    - toggle tree element
* E / C            - expand / collaps all tree elements
* q / Ctrl-C       - quit
* F                - move cursor to end of list and resume autoscrolling

* Mouse wheel      - move cursor up / down (stop autoscrolling if necessary)
* Trackpad gestures- move cursor up / down (stop autoscrolling if necessary)
* Single click on tree element - move cursor to this element
* Double click on tree element - expand element or show command output if
                                 applicable

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
treeview should be updated accordingly. I.e. when I close the command output
view, the cursor in the tree view should be on the current task / current
host that had been displayed in the command output view. Make sure that the
treeview is expanded so that the element with the cursor is visible.

* Mouse wheel      - move cursor up / down
* Trackpad gestures- move cursor up / down
