# Help

## Situation

Tangsible uses a large number of keyboard shortcuts. Only a fraction of these
can be visualized in one status line at the bottom of the screen (esp. if the
terminal is juet 80 characters wide). The available shortcuts also vary based
on the part of tansible where the user is navigating at th moment.

## Proposed solution

Provide a context sensitive help screen that can be activated by yet-another
keyboard shortcut (either h or ?).

## User interface

The context sensitive help screen should not be displayed full screen.
Instead it should appear as some modal dialog, maybe 70 characters wide so
that the curent screen is still partly visible.

Inside the dialog there are just two columns - Shortcut - Description.

If the contents of the dialog vertically do not fit on one screen, the ususal
motions (up/down, page up/down, home/end) shall be possible.

The dialog can be closed again with ESC.

### Nice to have:

Updates to the interface behinde the dialog (like the treeview etc.) should
be visible while while the context sensitive help dialog is open.

## Decisions (2026-10-02)

- Key: `?`, with F1 as an alias (`h` is already "change host" in the
  template view). Esc, `?`, F1 and `q` close the dialog - `q` closes
  rather than quits, like the filter dialog. Ctrl-C closes it and then
  does what it always does (interrupt the run / quit).
- One place in the source: each screen's shortcuts are a table in Go
  (`uikit.HelpScreen`; the tree's is `treeHelp` in
  `internal/session/helptables.go`), rendered by the help dialog. The
  bottom bar is a separate, hand-written string defined right next to it
  (`treeBar`) - a first version assembled the bar from per-entry labels,
  which was harder to read than the one string; keeping the two side by
  side keeps the risk of them drifting apart low. Entries that only apply
  in some contexts (re-run, revisit's Esc) have an `ID` the screen can hide
  from the help. `Keyboard-shortcuts.md` stays a hand-written long-form
  reference.
- One screen first: the tree. Drill-down, `o` view, rerun dialog (which
  should also explain its fields, not just keys), diff, revisit list,
  template and host views follow once this one is settled.
- The dialog is an overlay page (like the filter dialog), so updates
  behind it stay visible for free. Up to 70 columns wide, shrinking on a
  narrower terminal; as tall as its content, capped to the screen, with a
  scroll hint in the title when it doesn't fit.
- Bottom bar, first proposal: `?: help  f: filter  /: search  o: output
  r: re-run  q: quit` (62 columns).
- Second screen (2026-10-02): the drill-down (`outputHelp`/`outputBar`),
  same dialog. Bar: `?: help  tab: next tab  ←/→: host  n/N: task
  /: search  y: copy  esc: back` (77 columns). Closing help returns focus
  to whatever had it, so the drill-down's keys keep working afterwards.
- Every other location (2026-10-02): `o` view, re-run dialog (F1 only -
  `?` is typeable there; its help explains the fields and checkboxes), the
  run list (revisit and diff's picker, context-dependent entries), diff
  tree (with a legend for its underline/strikethrough markers) and diff
  drill-down, `hosts` list (legend for the ping dots), host detail
  (standalone vs. from the list, full-screen vs. side by side), template
  view. The dialog runtime moved into `uikit.HelpOverlay`, shared by all
  apps; each package keeps its tables and bars in its own
  `helptables.go`. Not covered: the filter and search dialogs (a menu and a
  single text field - self-explanatory) and the template view's one-field
  change-host dialog.
