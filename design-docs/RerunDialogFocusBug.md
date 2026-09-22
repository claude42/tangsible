# Re-run dialog silently stopped accepting keyboard input

## Status

Fixed.

## Situation

A real user report: after a full playbook run where one task failed for
one host, pressing `r` opened the re-run dialog as normal, but it was
almost unusable - mouse worked (clicking a field to focus it, toggling a
checkbox), but typing and Tab did nothing at all. Escape still closed the
dialog. Not reproducible on demand by description alone; reproduced here
by suspecting the one thing the user's session had that none of this
project's own recent testing had exercised - opening the output
drill-down for the failed host (the natural next thing to do after seeing
a failure) before pressing `r`.

## Root cause (confirmed live, via a debug build)

Two separate tview mechanisms that look like they should be equivalent
aren't:

- **Mouse dispatch** (`Pages.MouseHandler`, tview's own `pages.go`) walks
  registered pages by `Visible`, topmost first, stopping at whichever
  consumes the event.
- **Keyboard dispatch** (`Pages.InputHandler`) walks registered pages in
  *registration order* and dispatches to the *first* one whose own
  `Item.HasFocus()` reports true - regardless of `Visible`.

`internal/session/livesession_output.go`'s `closeOutput()` (leaving the
drill-down, e.g. via Escape) never blurred `s.outputTabs`' own internal
state. Confirmed with a debug build tracing `HasFocus()` on every
keypress: `s.outputTabs.Primitive().HasFocus()` stayed `true` long after
the drill-down closed, through an entirely unrelated later dialog open.
Since "output"/"split" are registered in `s.pages` *before* any dialog
page, `Pages.InputHandler` kept finding the stuck "output" page first and
dispatching every real keystroke there - into a page nothing was even
showing - instead of into the now-frontmost "rerun" page's own focused
field. `handleRerunDialogKey`'s own dispatcher (`internal/session/
livesession_input.go`) correctly identified `rerunDialogOpen == true` and
correctly returned every non-Enter/Escape key untouched the whole time -
the bug was entirely downstream, in tview's own post-capture forwarding,
which the capture callback's return value doesn't control on its own.

**Narrowing it further** (also confirmed live, with more granular
instrumentation): plain `Application.SetFocus(s.list)` - the same call
`closeDialogs()` already correctly relies on for the filter/search/rerun
dialogs - does *not* fix this. `s.outputTabs`'s own internal `tview.Pages`
(switching between tab content) had its *own* embedded `Box.hasFocus`
stuck `true`, independent of `Application`'s single tracked focus pointer
and independent of which specific tabs were currently registered -
removing every tab (`RemovePage`, the same cleanup `SetTabs` already does
on every call) didn't clear it either. Only an explicit `.Blur()` call on
that `tview.Pages` (and the `Flex` wrapping it) does.

## Fix

`internal/uikit/tabs.go`'s new `TabbedPane.Clear()`: removes every
registered tab (mirroring `SetTabs`'s own cleanup) and explicitly blurs
both `p.pages` and `p.root`. `closeOutput()` calls it before switching
back to "main". Nothing about `SetTabs`/tab-switching *while the
drill-down stays open* needed to change - this is specific to leaving the
pane's content behind entirely.

## Verification

Live, via tmux with a temporary debug build (`TANGSIBLE_DEBUG_FOCUS`
env-gated instrumentation, removed before committing) tracing `HasFocus()`
on `s.list`/`s.rerunForm`/`s.outputTabs.Primitive()` on every keypress -
reproduced the exact failure (stuck `true` surviving into the re-run
dialog, real keystrokes never reaching it) and confirmed the fix (`false`
immediately after `closeOutput()`, typing/Tab both work normally
afterward) against the precise repro: a genuine per-host task failure →
click the failed host to open its drill-down → Escape → `r` → type into a
field, Tab to another, type again. Also confirmed a clean run (no drill-
down ever opened) was never affected either way. `go test ./...` and
`go test -tags e2e ./...` both pass. Unit test: `internal/uikit/
tabs_test.go`'s `TestTabbedPaneClearResetsStateAndBlursStuckFocus` -
focuses the pane via a self-forwarding delegate (mirroring `Application.
SetFocus`'s own real recursive behavior, since a plain no-op delegate
doesn't actually reach any leaf primitive), confirmed to fail without the
`.Blur()` calls (mere tab removal isn't enough) and pass with them.
