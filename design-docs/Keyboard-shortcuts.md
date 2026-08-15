# Keyboard shortcuts

Covers both `tangsible run`/`rerun`/`role` (the live tree UI, `tui.go`) and
`tangsible template` (the standalone template-debugging UI, `template.go`)
- two separate programs with their own key/mouse handling, noted separately
below. A short "Possible inconsistencies" section at the end collects
things worth a second look while reviewing this list, not necessarily bugs.

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
                     own row. While the playbook is still running, this is
                     "sticky" - a task added afterward starts in whichever
                     state (expanded/collapsed) the task added right
                     before it currently has, so pressing E mid-run also
                     makes every later task start expanded, not just the
                     ones already visible at the time
* q / Ctrl-C       - quit (or, while the playbook is still running,
                     interrupt it - Ctrl-C always does this unconditionally;
                     `q` does the same but only once the run has finished
                     does it actually close the app - see "q vs Ctrl-C" in
                     Possible inconsistencies below)
* F                - move cursor to end of list and resume autoscrolling -
                     the only shortcut that resumes it
* f                - open the filter dialog (All/Changed/Failed - see
                     below). Not available while the command output view
                     is shown.
* /                - open the search dialog (see below). Not available
                     while the command output view is shown.
* r                - open the re-run dialog (see below - Rerun.md). Only
                     once the playbook has finished (successfully, failed,
                     or interrupted) - a no-op while it's still running.
                     Not available while the command output view is shown.

E/C/r/f// are all only reachable from the main tree - none of them have any
effect while the command output view is frontmost (see that section below
for what *is* available there).

**Mouse**
* Wheel / trackpad - pan contents up / down (stop autoscrolling if
                     necessary)
* Click on a tree element - select AND activate it (same as Enter/Space) -
                     expands/collapses a task, or opens a host's command
                     output. No separate double-click behavior - a single
                     click already does everything Enter/Space does.
* Click on the top/bottom info bars - no effect (deliberately swallowed;
                     these are plain status text, not interactive - a
                     click here used to silently steal keyboard focus and
                     make Escape/arrow keys appear to stop working until
                     you clicked the tree again, fixed this session)

## Output/command drill-down view

Opened by Enter/Space/click on a host row. Six tabs - Task, Output, Task
definition, Resolved, Play definition, Details - shown only when that tab
actually has content for the current task (Task/Resolved/Details are
always present; Output/Task definition/Play definition are shown only when
there's something to put in them).

* Cursor up / down - move cursor up / down (scrolls the active tab's own
                     content)
* j / k            - move cursor up / down
* Page up / down   - move cursor one page up / down
* Ctrl-F / Ctrl-B  - move cursor one page up / down
* Home / End       - move cursor to top / end
* Ctrl-A / Ctrl-E  - move cursor to top / end
* G                - move cursor to end
* Tab / Shift-Tab  - switch to the next / previous tab (wraps at both
                     ends - unlike every other list in this app, a small,
                     closed set of tabs is exactly the case where
                     wraparound is the natural gesture). Scroll position
                     resets to the top on every switch.
* Escape / Enter   - close the view, go back to the tree (cursor lands on
                     whichever (task, host) was last shown here)
* q                - same as Escape/Enter here (closes the view) - does
                     NOT quit the app, unlike bare `q` everywhere else
* Cursor left / right - show the same tab's content for the previous /
                     next host on this task (no wraparound)
* n / p            - show the same tab's content for the previous / next
                     task, same host (no wraparound; skips tasks hidden by
                     the active filter)
* e                - open the file containing the currently displayed
                     task's own source in `$VISUAL`/`$EDITOR` (a foreground
                     subprocess, suspending the TUI - same mechanism as the
                     `template` verb's own `e` binding below). Unlike the
                     `template` verb, this does NOT re-run or refresh
                     anything once the editor exits - the view still shows
                     the already-recorded result for this task, unaffected
                     by any edit made to its source afterward. A no-op if
                     the task's source location couldn't be determined.

When you close the view (Escape/Enter/q), the tree's own cursor updates to
match whatever (task, host) was last shown - including if you navigated to
a different one via Cursor left/right or n/p since opening it - and the
tree auto-expands so that row is visible.

**Mouse**
* Wheel / trackpad - scroll the active tab's content
* Click on a tab   - switch to it (same as Tab/Shift-Tab)
* Click on the top/bottom info bars - no effect (same fix as the main
                     tree's own top/bottom bars, above)

## Filter dialog

Opened with `f` from the main tree (see Filters.md for the full feature
spec). A plain menu (A/C/F) plus a Cancel button - no text entry.

* a / c / f  - show all tasks / show only tasks with a changed-or-failed
              host / show only tasks with a failed-or-unreachable host.
              Applies immediately and closes the dialog.
* Esc / q    - close the dialog, no change to the active filter
* Ctrl-C     - close the dialog *and* quit/interrupt the playbook, same as
              Ctrl-C always does outside the dialog

(The dialog itself displays a/c/f as A/C/F to stand out next to each
option's description, but typed as lower case.)

`q` deliberately does *not* quit here, unlike everywhere else in the app -
only Esc and q both just close the dialog, since in a plain menu like this
one, pressing q is almost always a reflex to close it, not a real intent to
quit. Ctrl-C is the one exception: since its intent (abort) is always
unambiguous, it closes the dialog *and* still quits/interrupts, exactly as
it would if the dialog weren't open.

**Mouse** - a click inside the dialog's own box works; a click on the A/C/F
menu lines applies that filter (same as typing the letter); a click on the
Cancel button (bottom-right) closes the dialog with no change. A click
outside the box does nothing (correctly doesn't leak through to the tree
underneath). None of the three menu rows or the Cancel button are
Tab-reachable - see "Buttons aren't keyboard-reachable" below.

## Search dialog

Opened with `/` from the main tree (see Filters.md). A headline, a search
text box (gets keyboard focus immediately), and Search/Cancel buttons.
Reopening the dialog while a search filter is already active pre-fills the
box with the previous term.

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

**Mouse** - a click inside the box positions the text cursor (native
InputField behavior); the Search button applies the typed term (same as
Enter); the Cancel button closes without applying (same as Esc). A click
outside the box does nothing. Neither button is Tab-reachable - see below.

## Re-run dialog

Opened with `r` from the main tree, only once the playbook has finished
(see Rerun.md). A real form with four fields - Start with task, Limit tags
to, Skip tags, Limit hosts to - plus Cancel/Re-run buttons, right-aligned
in that order (Cancel left, Re-run right).

* Tab / Backtab    - move to the next / previous field, then to the
                     Cancel/Re-run buttons (wraps back to the first field
                     after Re-run)
* (type freely)    - every key except Ctrl-C, Enter, and Esc is typed into
                     whichever field has focus normally, including `q` and
                     every other letter that's a shortcut elsewhere - same
                     reasoning as the search dialog's own text box
* Enter            - while a text field has focus: start the re-run and
                     close the dialog, regardless of which *field*
                     currently has focus (tview's own default Enter
                     behavior inside a form field just moves to the next
                     field, not what's wanted here). While the Cancel or
                     Re-run *button* has focus (reached via Tab): triggers
                     that button instead - Cancel cancels, Re-run submits
* Esc              - cancel back out: no re-run started, nothing on screen
                     changes (regardless of what has focus)
* Ctrl-C           - cancel back out *and* quit/interrupt the playbook,
                     same as Ctrl-C always does outside the dialog

Task is empty by default and never pre-filled from the cursor's position in
the tree - leaving it empty re-runs the whole playbook; typing a task name
passes it as `--start-at-task`, so the re-run skips straight to it. Tags,
Skip tags, and Hosts pre-fill from whatever `--tags`/`--skip-tags`/`--limit`
this run itself was started with (once, the first time each field is opened
while still empty) - editing them changes what the re-run uses. All four
fields keep whatever was last typed into them across repeated `r` presses
within the same session, the same way the search dialog's box remembers the
last search term.

Starting `tangsible` with the `rerun` verb instead of `run` (see the
project README for the full command-line syntax) opens this same dialog
immediately, before anything has run, pre-filled from `.tangsible`'s
recorded history instead of the current session.

**Mouse** - a click inside any field positions the text cursor (native
Form/InputField behavior) and gives it focus; a click on Cancel or Re-run
does the same as Tab-ing to it and pressing Enter. A click outside the
dialog's own box does nothing. This is the one dialog whose buttons *are*
Tab-reachable - see "Buttons aren't keyboard-reachable" below for why the
other three dialogs' buttons aren't.

## The `template` verb (standalone program, `template.go`)

A completely separate program from `run`/`rerun`/`role` (own `main()`
entry point, own key/mouse handling) for interactively debugging a Jinja2
template against one host. Two tabs - Rendered, Source.

* Cursor keys, Page up/down, Home/End - native `tview.TextView` scrolling
                     of whichever tab is active (also `g`/`G`/`j`/`k`/`h`/`l`
                     and Ctrl-F/Ctrl-B, all of TextView's own built-in vim
                     bindings - unlike the main tree/drill-down view above,
                     these are never explicitly wired here; they work
                     because TextView natively supports them and nothing
                     intercepts them first)
* Ctrl-A / Ctrl-E  - same as Home / End - unlike the vim bindings above,
                     `tview.TextView` does NOT handle these two natively,
                     so (like the main tree) they're explicitly translated
                     to Home/End before dispatch
* Tab / Shift-Tab  - switch between the Rendered and Source tabs (wraps at
                     both ends, same as the drill-down view's own tabs)
* e                - open the template file in `$VISUAL`/`$EDITOR` (a
                     foreground subprocess, suspending the TUI exactly like
                     `git commit` would) - re-renders unconditionally once
                     the editor exits, whether or not anything was saved
* h                - open the "change host" dialog (see below)
* q / Ctrl-C       - quit the program outright - no distinction between the
                     two here, and no interrupt-vs-quit split either, since
                     there's no long-running child process to interrupt
                     (each render is one synchronous `ansible-playbook`
                     invocation). Esc deliberately does NOT quit here (it
                     used to, identically to q/Ctrl-C, but that made it too
                     easy to close the whole program by reflex while just
                     browsing the tabs) - it's simply inert instead

**Mouse**
* Wheel / trackpad - scroll the active tab's content
* Click on a tab   - switch to it (same as Tab/Shift-Tab)
* Click on the top/bottom info bars - no effect (same fix as the main
                     app's own bars, above)

### Change host dialog (template page)

Opened with `h`. A single Host field plus Cancel/Apply buttons,
right-aligned (Cancel left, Apply right).

* (type freely) - every key except Escape is typed into the field normally
* Enter         - apply the typed host (if non-empty and different from
                  the current one) and close the dialog, re-rendering
                  against the new host
* Esc           - close the dialog with no change

**Mouse** - a click inside the field positions the text cursor (native
InputField behavior); the Apply button does the same as Enter; the Cancel
button closes with no change. A click outside the box does nothing.
Neither button is Tab-reachable - see below.

## Possible inconsistencies

Collected while compiling this list - flagged for review, not necessarily
things to change:

* **Buttons aren't keyboard-reachable in three of the four dialogs.** The
  re-run dialog's Cancel/Re-run buttons are real `tview.Form` buttons, so
  Tab reaches them and Enter triggers whichever has focus. The filter,
  search, and template-host dialogs' buttons are plain `tview.Button`s
  sitting in a bare `Flex`, added purely as a mouse affordance - Tab never
  reaches them (deliberate: those three dialogs already repurpose
  Enter/Esc/Tab away from their native meaning via `SetDoneFunc`, and
  wrapping them in a real `Form` would make `Form.Focus()` silently
  overwrite that `SetDoneFunc` and double-fire on the same keypress - see
  the code's own comments on this). Net effect: a keyboard-only user has
  full parity everywhere via Esc/Enter/the menu letters, but *can't*
  Tab-and-Enter onto a Cancel/Apply/Search button the way they can in the
  re-run dialog specifically.
* **`q`'s meaning varies by context**, more than most other keys:
  quits/interrupts in the main tree; closes-without-quitting in the filter
  dialog and the output drill-down view; is ordinary typed text in the
  search and re-run dialogs' text fields; and quits outright (no
  interrupt distinction) in the `template` program. Each is individually
  justified in its own section above, but it's a lot of different
  behavior for one key across the app.
* **Escape's meaning varies by context** in a similar way, though less than
  it used to: does nothing at the main tree's own top level; closes a
  dialog or the drill-down view with no side effect; and, in the
  `template` verb, is likewise just inert at the top level - it used to
  quit the whole program there, uniquely, which was flagged here as an
  inconsistency and has since been removed (only `q`/Ctrl-C quit the
  `template` verb now, matching how nothing else in the app treats bare
  Escape as "quit").
* **Cursor left/right mean different things depending on what's focused**:
  expand/collapse in the main tree, but previous/next *host* in the output
  drill-down view. Not a bug (there's nothing to expand/collapse once
  you're looking at one host's result), but worth knowing the same keys
  aren't doing conceptually the same thing.
* **E/C (expand/collapse all) and F (resume autoscroll) have no mouse
  equivalent at all** - unlike per-row expand/collapse (click-to-toggle
  already works) or tab-switching (click already works), there's no
  clickable affordance anywhere for these three. Noted as a real gap in
  an earlier mouse-support pass this session; intentionally left out of
  scope so far.
* **The template page's Tab/Backtab and the drill-down view's Tab/Backtab
  do the same thing (switch tabs)** - listed here as a *positive*
  consistency worth preserving, not a problem, in case future changes to
  either drift apart.

## My notes, pls ignore for now

### Treeview bottom bar

* n/p
* E/C
* F
* f
* /
* r

### Drill down

* n/p
* <-/->
* Tab



## Misc

* Search dialog buttons
