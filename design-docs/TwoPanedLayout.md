# Two-paned layout

## Current situation

When the user goes into drill down view, this will open in full screen mode.
The tree is not visible anymore. The user still has the possibility to
navigate with n/p and Cursor left/right but they don't know where they're
navigating.

## Idea

If the terminal window is small, i.e. its width is below say 120 characters
or so, there's not much we can do. In this case tangsible shall show the same
behavior as now.

But in case the terminal is wide enough, we could display a two paned layout.
I.e as soon as the user opens a drill down view, it will open on the right
side of the terminal while the left side still shows the tree (albeit the
right part of the whole tree is not visible because it's being overlapped by
the drill down view.

First draft with numbers

* Tree shall be at least 30 characters wide
* Drill down view shall be at least 79 characters wide
* A one-character-wide vertical divider line separates the two panes
* hence: minimum terminal width must be 110 characters
* If more than 110 characters are available 
  * at first the tree will get more space, up to 80 characters wide
  * once the tree has grown to 80 characters and there's still space
    available, the drill down part will get the additional space

When the user hits enter, the drill down view will disappear as usual and the
tree will use the full available space again as usual.

The user will have the option to set two_pane_layout = false in the config
files to prevent this behavior (thus tangsible will behave in the same way as
it does now). Default for two_pane_layout will be true though.

## Pane-mode decision (live)

Whether a drill-down is currently shown in two-pane or full-screen mode is
re-evaluated continuously against the terminal's actual current width — on
open, and again on every later resize for as long as the drill-down stays
open, in both directions. Shrinking below 110 characters while a two-pane
session is open switches it to full-screen immediately, without having to
close and reopen it; growing back above 110 switches it back to two-pane.
While two-pane mode stays active across a resize, the tree pane's own width
is kept current too (still governed by the 30–80 character growth rule
above), independent of whichever way the terminal is being resized.

## Tree pane content while split

Hostnames are dropped from a task's collapsed row entirely while the tree is
shown in the narrow, two-pane-mode pane — not shrunk down via the existing
per-character algorithm, just omitted. The rationale: whichever host matters
right now is already shown by the live-sync behavior below (auto-expanded,
cursor on that exact host row), so the collapsed-row hostnames aren't pulling
their weight at this width and the code doesn't need to reproduce the
existing shrink-to-fit cascade at a second, narrower budget.

## Live-sync between the drill-down and the tree pane

While the drill-down view is open in two-pane mode, the tree pane keeps
following it: every navigation inside the drill-down (Left/Right to move
between hosts of the same task, `n`/`p` to move between tasks for the same
host) auto-expands the corresponding task in the tree and moves the tree's
cursor to that exact host row — the same end state that closing the
drill-down already produces today (`expanded[outputTask] = true`,
`currentID` pointing at that host row), just applied continuously rather
than once on close. If that puts the cursor outside the currently-visible
portion of the tree, the tree scrolls just enough to bring it back into view.

The tree pane itself is inert while the drill-down is open: it never takes
keyboard focus and none of the tree's own navigation keys act on it directly.
Escape closes the drill-down and returns keyboard control to the tree,
exactly as it does today.
