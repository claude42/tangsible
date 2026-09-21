// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package session

import (
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// handleMouse is the single func wired via s.app.SetMouseCapture
// (NewLiveTUI) - same increment-6 split as handleKey (livesession_input.go),
// same zero-behavior-change caveat: this doesn't reduce the shared-state
// coupling these branches already had, it only turns one ~300-line
// anonymous closure into named, individually-scoped methods in the exact
// order the original code checked them.
func (s *liveSession) handleMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event == nil {
		// tview's own fireMouseActions (application.go) fires several
		// actions per physical mouse event (move, then down/up/click)
		// against this same callback, threading the event/action pair
		// from one call's return value into the next call's arguments
		// within that batch - so once any earlier call in the same batch
		// returns a nil event (e.g. a MouseMove that happened to land on
		// a swallowed bar below), every later call in that same batch is
		// invoked with event == nil too. Hit live as a real crash
		// (event.Position() on a nil event) before this guard existed -
		// every branch below assumes a non-nil event, so bail out
		// immediately rather than touch it.
		return nil, action
	}
	if result, resultAction, handled := s.handleTabSearchComposingMouse(event, action); handled {
		return result, resultAction
	}
	if result, resultAction, handled := s.handleFilterDialogMouse(event, action); handled {
		return result, resultAction
	}
	if result, resultAction, handled := s.handleSearchDialogMouse(event, action); handled {
		return result, resultAction
	}
	if result, resultAction, handled := s.handleRerunDialogMouse(event, action); handled {
		return result, resultAction
	}
	if s.viewingOutput {
		return s.handleOutputViewMouse(event, action)
	}
	return s.handleTreeMouse(event, action)
}

// handleTabSearchComposingMouse: same reasoning as handleFilterDialogMouse/
// handleRerunDialogMouse below, and for the same underlying bug those two
// already guard against: fireMouseActions forwards every mouse event -
// including a bare MouseMove with no button down, which tmux/terminals
// under SGR mouse tracking can send continuously - to whatever primitive
// sits under the cursor, and that primitive's own MouseHandler can call
// the setFocus callback on nothing more than a hover. Left unguarded, a
// stray MouseMove landing on s.outputTabs' own content (the tab body, not
// the footer) silently steals focus back from s.search.input moments
// after s.search.open() sets it - caught live: typed characters and
// Enter/Esc stopped reaching the field at all, with no visible error,
// because keyboard input was still correctly being forwarded, just to the
// wrong primitive. A click inside s.search.input's own rect is let
// through (native click-to-position-cursor); everything else swallowed.
func (s *liveSession) handleTabSearchComposingMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	if !s.search.composing {
		return nil, action, false
	}
	if x, y := event.Position(); uikit.InRect(x, y, s.search.input) {
		return event, action, true
	}
	return nil, action, true
}

// handleFilterDialogMouse: s.filterDialog (the plain TextView rendering
// the A/C/F menu) has no click handling of its own for that text - there's
// no real widget underneath to unlock there, unlike the two dialogs
// below, so those three rows still need their own hit-test. s.filterFlex
// (see NewLiveTUI) wraps s.filterDialog together with a real Cancel
// button below it, though - a click on that button needs no hit-test of
// its own: letting it through reaches Pages' native dispatch and Button's
// own MouseHandler, exactly like handleSearchDialogMouse/
// handleRerunDialogMouse's own buttons/fields below.
//
// Only a click landing outside s.filterFlex's own box (not just
// s.filterDialog's - that would incorrectly swallow clicks on the Cancel
// button sitting below it) is unconditionally swallowed here (same
// reasoning as handleSearchDialogMouse/handleRerunDialogMouse below -
// Pages tries every visible page, topmost first, so an unswallowed click
// outside the dialog would otherwise fall through to the page
// underneath). Everything else - Down/Up/Move inside the box, and a click
// on the Cancel button - is deliberately let through unchanged rather
// than swallowed unconditionally the way an earlier version of this code
// did: tview's own fireMouseActions (application.go) synthesizes
// MouseLeftClick right after MouseLeftUp within the same physical click,
// threading the *same* event value through both calls - unconditionally
// returning a nil event from the MouseLeftUp call (as this used to) meant
// the click action was invoked with an already-nil event and could never
// fire at all, silently eating every click. Confirmed live: with the old
// unconditional swallow, clicking a menu row did nothing whatsoever, not
// even the wrong row.
func (s *liveSession) handleFilterDialogMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	if !s.filterDialogOpen {
		return nil, action, false
	}
	x, y := event.Position()
	if !uikit.InRect(x, y, s.filterFlex) {
		return nil, action, true
	}
	if action == tview.MouseLeftClick && uikit.InRect(x, y, s.filterDialog) {
		// FilterDialogText's own fixed layout: row 0 headline, row 1
		// blank, rows 2/3/4/5 = All/Interesting/Changed/Failed.
		// s.filterDialog itself has no border of its own (that lives on
		// s.filterFlex instead), so GetRect()'s own y is already the
		// first content row - unlike the dialogs below, which are
		// bordered themselves.
		_, ry, _, _ := s.filterDialog.GetRect()
		switch y - ry {
		case 2:
			s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterAll})
		case 3:
			s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterInteresting})
		case 4:
			s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterChanged})
		case 5:
			s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterFailed})
		}
		return nil, action, true
	}
	// Anything else inside s.filterFlex but outside s.filterDialog's own
	// A/I/C/F rows - the real Cancel button, or one of s.filterFlex's own
	// bare tview.NewBox() margin/padding cells (NewLiveTUI's own
	// s.filterFlex construction). A click-type action is dispatched to
	// s.filterFlex's own MouseHandler directly and unconditionally
	// swallowed, rather than just letting it fall through to Pages'
	// native dispatch as the code above used to (comment above still
	// describes why Up must never be swallowed too) - see
	// handleRerunDialogMouse's own doc comment below for the confirmed-
	// against-tview's-source root cause: Box.MouseHandler only ever
	// consumes MouseLeftDown, never MouseLeftClick, so a click on one of
	// those bare margin Box cells went unconsumed and leaked straight
	// through to the tree page underneath - reproduced live the same way
	// handleRerunDialogMouse's own bug was.
	switch action {
	case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
		tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
		tview.MouseRightClick, tview.MouseRightDoubleClick:
		s.filterFlex.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
		return nil, action, true
	default:
		return event, action, true
	}
}

// handleSearchDialogMouse: same fix, same reasoning, as
// handleFilterDialogMouse above and handleRerunDialogMouse below:
// s.searchDialogFlex has its own bare tview.NewBox() margin/padding cells
// (around its top margin and its Cancel/Search buttons - NewLiveTUI's own
// s.searchDialogFlex construction), which Box.MouseHandler never consumes
// for a click - only for MouseLeftDown. A click landing there used to
// leak straight through to the tree page underneath (reproduced live) -
// and, as an added symptom, still silently steals focus off
// s.searchInput onto the margin Box itself (Box's own MouseHandler
// fallback consumes MouseLeftDown by refocusing itself - manually
// dispatching to s.searchDialogFlex below reaches that same fallback,
// since it's the exact same dispatch tview's own Pages would have done).
// That's harmless now: handleSearchDialogKey (livesession_input.go)
// handles Escape centrally, so closing no longer depends on
// s.searchInput itself still having focus.
func (s *liveSession) handleSearchDialogMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	if !s.searchDialogOpen {
		return nil, action, false
	}
	x, y := event.Position()
	if !uikit.InRect(x, y, s.searchDialogFlex) {
		return nil, action, true
	}
	switch action {
	case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
		tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
		tview.MouseRightClick, tview.MouseRightDoubleClick:
		s.searchDialogFlex.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
		return nil, action, true
	default:
		return event, action, true
	}
}

// handleRerunDialogMouse: real, reported bug this whole method exists to
// fix: clicking one of the blank separator rows between s.rerunForm's
// fields used to toggle a tree row on the page behind the dialog. Root
// cause, confirmed against tview's own source (application.go/pages.go/
// form.go): Form.MouseHandler's own catch-all ("a mouse-down anywhere
// else refocuses the last element") only ever consumes the MouseLeftDown
// action - it has no equivalent for MouseLeftUp/MouseLeftClick, so a
// click landing on a blank row (nothing there to consume Up/Click) went
// unconsumed by the form, and Pages.MouseHandler (tries every visible
// page, topmost first, falling through to the next on non-consumption)
// let it leak straight through to the "main" page underneath.
func (s *liveSession) handleRerunDialogMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	if !s.rerunDialogOpen {
		return nil, action, false
	}
	x, y := event.Position()
	// An open autocomplete drop-down (design-docs/Autocomplete.md) renders
	// at an absolute screen position directly below its own field
	// (InputField.Draw), independent of s.rerunForm's own fixed-height
	// box - it can render partly or entirely below s.rerunForm's own
	// rect. InputField exposes no accessor for the drop-down's own rect,
	// so this is a deliberately generous fixed band below s.rerunForm
	// sized to the maximum drop-down height, not a precise hit-test.
	rx, ry, rw, rh := s.rerunForm.GetRect()
	inBand := x >= rx && x < rx+rw && y >= ry+rh && y < ry+rh+autocompleteMaxEntries+1
	if !uikit.InRect(x, y, s.rerunForm) && !inBand {
		return nil, action, true // outside the dialog entirely - fully modal
	}
	switch action {
	case tview.MouseLeftClick, tview.MouseLeftDoubleClick,
		tview.MouseMiddleClick, tview.MouseMiddleDoubleClick,
		tview.MouseRightClick, tview.MouseRightDoubleClick:
		// The actual fix: dispatch straight to s.rerunForm's own
		// MouseHandler ourselves (still reaches a real field/button/
		// autocomplete-entry click exactly as normal Pages dispatch
		// would - neither Box.WrapMouseHandler nor InputField.MouseHandler,
		// tview's box.go/inputfield.go, gate on the primitive's own rect
		// before checking its own open autocomplete list, so Form's
		// per-item loop reaches it correctly even inside the band above),
		// then unconditionally swallow (return nil) so a click Form
		// itself doesn't consume - a blank row - can never fall through
		// to Pages' own dispatch and leak to the page underneath.
		s.rerunForm.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
		return nil, action, true
	default:
		// MouseMove/MouseLeftDown/MouseLeftUp: let through unchanged via
		// normal Pages dispatch. MouseLeftDown is safe to let through as-
		// is - Form's own catch-all above already consumes it anywhere in
		// rect, so it was never the source of the leak. MouseLeftUp must
		// also be let through unchanged, even though nothing here needs
		// its own effect: tview's fireMouseActions (application.go) only
		// synthesizes the MouseLeftClick action afterward if the
		// MouseLeftUp call's own mouseCapture result came back non-nil
		// (it reassigns its own shared `event` variable to whatever this
		// callback returns) - swallowing Up here, as an earlier version
		// of this fix did, silently suppressed every Click on
		// s.rerunForm, buttons included.
		return event, action, true
	}
}

// handleOutputViewMouse is everything handleMouse falls through to once
// s.viewingOutput is true and none of the dialog guards above claimed the
// event - the drill-down's own full mouse handling. Always returns.
func (s *liveSession) handleOutputViewMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	// While a two-pane drill-down (design-docs/TwoPanedLayout.md) is open,
	// the tree pane stays visible but must stay fully inert - a click
	// landing on it would otherwise reach s.list's own MouseHandler
	// (toggling expand/collapse, opening a different host's output) with
	// no keyboard-side equivalent guarding it, unlike the full-screen case
	// where the tree isn't drawn at all so no click can ever land there.
	// Checked first, before any of the output-specific hit-tests below.
	if s.splitMode {
		if x, y := event.Position(); uikit.InRect(x, y, s.treeBody) {
			// A wheel scroll over the tree pane itself (not its
			// s.bottomBar row - matching the full-screen case below,
			// which swallows a scroll over s.bottomBar the same way) is
			// the one deliberate exception to "fully inert while split"
			// (design-docs/TwoPanedLayout.md's own "no focus-switching,
			// Esc to close" call): unlike a click, it doesn't select or
			// change anything, only pans the view, so it's let through to
			// reach s.list's own MouseHandler via tview's normal
			// position-based dispatch - already correctly unbounded
			// (TreeList's own wheel handling), no new panning logic
			// needed here. s.following=false has to be set explicitly on
			// this path, same reasoning as handleTreeMouse's own shared
			// fallthrough below: TreeList's wheel handling never fires
			// SetChangedFunc (it never touches currentItem), so nothing
			// else disengages autoscroll here.
			if (action == tview.MouseScrollUp || action == tview.MouseScrollDown) && uikit.InRect(x, y, s.list) {
				s.following = false
				return event, action
			}
			return nil, action
		}
		// s.splitHeader is a plain, non-interactive TextView, same focus-
		// steal reasoning as s.outputTopBar/s.outputBottomBar just below -
		// it replaces s.topBar/s.outputTopBar entirely for the duration
		// of a split session (s.splitFlex's own construction), so it
		// needs the identical guard they'd otherwise each carry on their
		// own.
		if x, y := event.Position(); uikit.InRect(x, y, s.splitHeader) {
			return nil, action
		}
	}
	// s.outputTopBar/s.outputBottomBar are plain, non-interactive
	// TextViews - swallow a click there before it can reach TextView's
	// own default MouseLeftDown handling, which would otherwise silently
	// move keyboard focus onto a one-line status bar (confirmed live:
	// Escape/Enter/arrow-key navigation then stop reaching the output
	// view at all, since TextView's own InputHandler intercepts Escape/
	// Enter for itself and there's nothing else to visibly scroll).
	if x, y := event.Position(); uikit.InRect(x, y, s.outputTopBar) || uikit.InRect(x, y, s.outputBottomBar) {
		return nil, action
	}
	// A left click on the tab bar itself switches tabs (design-docs/
	// Tabbed UI.md) - checked here, at the Application level, rather than
	// via s.outputTabs' own MouseHandler, matching this app's existing
	// convention of doing mouse/key overrides centrally rather than
	// inside a widget (see NewLiveTUI's own doc comment). Anything else
	// (a click elsewhere, wheel scrolling) passes through unchanged -
	// TextView's own wheel handling has no "keep the selected line
	// visible" clamp to fight the way the main tree's s.list once did, so
	// the active tab's own content already pans freely without any help.
	if action == tview.MouseLeftClick {
		if x, y := event.Position(); s.outputTabs.HandleClick(x, y) {
			return nil, action
		}
	}
	return event, action
}

// handleTreeMouse is everything handleMouse falls through to once
// s.viewingOutput is false and none of the dialog guards above claimed
// the event - the main tree's own mouse handling.
func (s *liveSession) handleTreeMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	// s.topBar/s.bottomBar - same focus-steal guard as s.outputTopBar/
	// s.outputBottomBar above, for the main page.
	if x, y := event.Position(); uikit.InRect(x, y, s.topBar) || uikit.InRect(x, y, s.bottomBar) {
		return nil, action
	}
	switch action {
	case tview.MouseScrollUp, tview.MouseScrollDown:
		// TreeList's default handling (left to run below) never fires
		// SetChangedFunc, since it never touches currentItem - so
		// disengaging autoscroll on a genuine pan has to happen here
		// explicitly instead of falling out of that callback the way
		// keyboard navigation gets it for free.
		s.following = false
	}
	return event, action
}
