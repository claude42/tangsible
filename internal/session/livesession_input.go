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
	"os"
	"os/exec"

	"code.aw.net/claude/tangsible/internal/diff"
	"code.aw.net/claude/tangsible/internal/template"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
)

// handleKey is the single func wired via s.app.SetInputCapture
// (NewLiveTUI) - increment 6 of design-docs/Restructuring.md's postponed
// "Phase 3" (see the refactor plan this session worked from). It's a pure
// dispatcher: a fixed, ordered sequence of guard checks, each pulled out
// of what used to be one ~458-line anonymous closure into its own named
// method below, in the exact same order the original code checked them -
// this split changes zero behavior, only readability/localization (most
// of these methods still reach into the same broad shared-state pool
// every other liveSession method does, so this doesn't reduce coupling
// the way the tabSearchPanel/rerunFieldSync extractions did - see
// Keyboard-shortcuts.md and CLAUDE.md's own "Keyboard shortcuts" section
// for the three-tier mental model this dispatcher implements: (1) Quit,
// checked before anything else so it can never be swallowed by dialog- or
// page-specific logic; (2) universal key aliases, applied regardless of
// frontmost page; (3) page-specific bindings, gated on s.viewingOutput.
// Dialog-open guards are checked before tier 2, so a literal typed space/
// letter still lands normally in a text field when a dialog owns focus.
func (s *liveSession) handleKey(event *tcell.EventKey) *tcell.EventKey {
	if result, handled := s.handleCtrlC(event); handled {
		return result
	}
	if result, handled := s.handleSearchDialogKey(event); handled {
		return result
	}
	if result, handled := s.handleTabSearchComposingKey(event); handled {
		return result
	}
	if result, handled := s.handleRerunDialogKey(event); handled {
		return result
	}
	if result, handled := s.handleFilterDialogKey(event); handled {
		return result
	}
	if result, handled := s.handleOutputViewQuit(event); handled {
		return result
	}
	if result, handled := s.handleGlobalQuit(event); handled {
		return result
	}
	if result, handled := s.handleRevisitEscape(event); handled {
		return result
	}
	if result, handled := s.handleTreeRowSkip(event); handled {
		return result
	}
	if translated, ok := s.translateKeyAlias(event); ok {
		return translated
	}
	if s.viewingOutput {
		return s.handleOutputViewKey(event)
	}
	return s.handleTreeKey(event)
}

// handleCtrlC's meaning never changes based on what's open - per
// Purpose.md's "behaves like running ansible-playbook directly" guarantee,
// it always aborts/quits, unconditionally. If either dialog happens to be
// open, it also closes that dialog first (with no filter/search change) -
// the one exception to both dialogs' own "q closes without aborting" rule
// (handleFilterDialogKey/handleSearchDialogKey below), since Ctrl-C is
// unambiguous about wanting to abort too. Checked first, before anything
// else in handleKey, so it can never be swallowed or reinterpreted by
// dialog- or page-specific logic below it (most importantly, by
// handleSearchDialogKey's own "let everything through" pass-through).
func (s *liveSession) handleCtrlC(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if event.Key() != tcell.KeyCtrlC {
		return nil, false
	}
	s.closeDialogs()          // harmless no-op if neither dialog is open
	s.search.closeComposing() // ditto if the tab-search prompt isn't open
	if s.processDone.Load() {
		s.quitting.Store(true) // before Stop() - see main.go's race note
		s.app.Stop()
	} else {
		_ = s.procH.Load().Signal(os.Interrupt) // best-effort; child may race-exit
	}
	return nil, true
}

// handleSearchDialogKey: while the search dialog's box has focus, every
// other key must reach InputField's own editing logic completely
// untouched - including 'q' itself (a real search term can contain the
// letter q, e.g. "request") and letters that double as shortcuts
// elsewhere ('a'/'c'/'f'). There's deliberately no "q closes this one too"
// here, unlike the filter dialog below: this dialog is nothing but a text
// box, so unlike a menu where q is never a valid choice, typing q here is
// always meaningful input, never a mistake to rescue the user from.
func (s *liveSession) handleSearchDialogKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.searchDialogOpen {
		return nil, false
	}
	if event.Key() == tcell.KeyEscape {
		// Centralized, unlike every other key here (see below) - fixes a
		// real bug: a click on s.searchDialogFlex's own bare margin Box
		// (SetMouseCapture's own s.searchDialogOpen branch) natively
		// steals keyboard focus onto that inert Box (Box's own
		// MouseHandler fallback: "a mouse-down anywhere in its rect
		// refocuses it"), which left Escape with nowhere to go -
		// s.searchInput.SetDoneFunc's own Escape-closes-the-dialog
		// handling only ever fires while s.searchInput itself still has
		// focus. Handled centrally here instead, the same way
		// s.rerunDialogOpen/s.filterDialogOpen already handle Escape (and,
		// for rerun, Enter) regardless of focus, so it always closes this
		// dialog no matter what currently has it.
		s.closeDialogs()
		return nil, true
	}
	return event, true
}

// handleTabSearchComposingKey: same reasoning as handleSearchDialogKey
// above, for the in-tab search prompt (design-docs/Search.md) instead of
// the tree's own row-filter search - a query might legitimately contain
// any letter, shortcut or not, so every key but Ctrl-C (handled above)
// must reach s.search.input's own native editing untouched. Unlike
// handleSearchDialogKey's own plain "return event" pass-through, this
// routes through tabSearchPanel.handleComposingKey - see its own doc
// comment (livesession_tabsearch.go) for why a plain pass-through doesn't
// reliably reach s.search.input here.
func (s *liveSession) handleTabSearchComposingKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.search.composing {
		return nil, false
	}
	s.search.handleComposingKey(event)
	return nil, true
}

// handleRerunDialogKey: the re-run dialog (Rerun.md) is a real form, not a
// plain text box like the search dialog above - but the same reasoning
// applies to letting most keys through untouched (any field might
// legitimately contain 'q' or any other shortcut letter, and Tab/Backtab
// need to reach Form's own native focus-cycling). The two exceptions are
// handled centrally here rather than via each item's own SetDoneFunc:
// Escape, because Form's own default Escape behavior (reset focus to the
// first item, unless a cancel func is set) isn't what's wanted - Esc
// should close the dialog outright, per Rerun.md. Enter, because
// tview.Form treats Enter identically to Tab on a FormItem - just
// advances focus, confirmed against form.go's own Focus() - never submits
// on its own, so submission has to be driven from here regardless of
// which field currently has focus, exactly matching "Re-run shall be
// initiated by pressing return" for the whole dialog, not just one field.
// The one exception to that "regardless of focus" rule: once Tab has
// moved focus onto the Re-run/Cancel buttons themselves (added alongside
// s.rerunForm's own construction), Enter should trigger whichever button
// is actually focused rather than always forcing a submit - checked via
// GetFocusedItemIndex (form.go's own accessor for "does a button
// currently have focus"), letting the event through untouched so Button's
// native InputHandler (button.go: KeyEnter calls its own selected func)
// fires the correct one of the two.
//
// A further exception to both cases (design-docs/Autocomplete.md): if the
// focused field currently has an open autocomplete drop-down
// (autocompleteOpenNow), Enter/Escape are let through untouched instead,
// so InputField's own native handling can pick the current suggestion or
// dismiss just the drop-down - only once that's no longer true does
// either key fall back to submitting/closing the whole dialog.
func (s *liveSession) handleRerunDialogKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.rerunDialogOpen {
		return nil, false
	}
	switch event.Key() {
	case tcell.KeyEnter:
		if _, button := s.rerunForm.GetFocusedItemIndex(); button >= 0 {
			return event, true
		}
		if s.rerunFields.autocompleteOpenNow() {
			return event, true
		}
		s.submitRerun()
	case tcell.KeyEscape:
		if s.rerunFields.autocompleteOpenNow() {
			s.rerunFields.acDismissed = true
			return event, true
		}
		s.closeDialogs()
	default:
		return event, true
	}
	return nil, true
}

// handleFilterDialogKey: the filter dialog is a plain modal menu
// (Filters.md), no text entry at all: every key except Esc/q and the
// three filter shortcuts is swallowed outright - checked before
// handleGlobalQuit below (so q closes the dialog here instead of quitting
// - per explicit request, pressing q was too often just a reflex to close
// something, not a real intent to quit) and before the vim-alias
// translation block and the s.viewingOutput branch further down, so it
// takes priority over all of them regardless of which page is otherwise
// frontmost.
func (s *liveSession) handleFilterDialogKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.filterDialogOpen {
		return nil, false
	}
	switch {
	case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyRune && event.Rune() == 'q':
		s.closeDialogs()
	case event.Key() == tcell.KeyRune && event.Rune() == 'a':
		s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterAll})
	case event.Key() == tcell.KeyRune && event.Rune() == 'i':
		s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterInteresting})
	case event.Key() == tcell.KeyRune && event.Rune() == 'c':
		s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterChanged})
	case event.Key() == tcell.KeyRune && event.Rune() == 'f':
		s.applyFilter(uikit.FilterQuery{Mode: uikit.FilterFailed})
	}
	return nil, true
}

// handleOutputViewQuit backs the output view's own 'q' meaning - same
// convention as the filter dialog's own q (Filters.md): closes/backs out
// rather than quitting. Calls closeOutput directly rather than
// synthesizing an Escape the way this used to (relying on outputView's
// own native SetDoneFunc to catch it) - there's no single persistent
// TextView left to forward a synthesized event to now that each tab's
// content is its own TextView, recreated fresh on every renderOutputTabs
// call.
func (s *liveSession) handleOutputViewQuit(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.viewingOutput || event.Key() != tcell.KeyRune || event.Rune() != 'q' {
		return nil, false
	}
	s.closeOutput()
	return nil, true
}

// handleGlobalQuit is 'q' everywhere handleOutputViewQuit above doesn't
// already claim it.
func (s *liveSession) handleGlobalQuit(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if event.Key() != tcell.KeyRune || event.Rune() != 'q' {
		return nil, false
	}
	if s.processDone.Load() {
		s.quitting.Store(true) // before Stop() - see main.go's race note
		s.app.Stop()
	} else {
		_ = s.procH.Load().Signal(os.Interrupt) // best-effort; child may race-exit
	}
	return nil, true
}

// handleRevisitEscape: Esc at the bare tree level (no dialog open - both
// already returned above - and not viewing a drill-down, which has its
// own Esc meaning in handleOutputViewKey below) has never meant anything
// here before revisitReturn existed. design-docs/Revisit.md: back out to
// the run list. s.quitting is deliberately NOT set here, unlike
// handleGlobalQuit above - this doesn't stop s.app.Run() itself, it stops
// THIS session's Application (see revisit.go), so the process as a whole
// keeps going.
func (s *liveSession) handleRevisitEscape(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if s.viewingOutput || !s.revisitActive || event.Key() != tcell.KeyEscape {
		return nil, false
	}
	s.revisitReturn()
	return nil, true
}

// handleTreeRowSkip: main tree only (not the output view, a real
// tview.TextView with its own unrelated meaning for these same keys):
// Up/Down/j/k skip straight over the trailing status/recap section's own
// purely decorative rows (NextInteractiveRow) instead of falling through
// to TreeList's native one-row-at-a-time handling, which has no idea any
// of these rows are meant to be invisible to the cursor. Checked here,
// before the vim-alias translation in translateKeyAlias, so 'j'/'k' get
// exactly the same treatment as the real arrow keys rather than being
// translated first and forwarded straight to TreeList, bypassing this
// entirely.
func (s *liveSession) handleTreeRowSkip(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if s.viewingOutput {
		return nil, false
	}
	var delta int
	switch {
	case event.Key() == tcell.KeyUp, event.Key() == tcell.KeyRune && event.Rune() == 'k':
		delta = -1
	case event.Key() == tcell.KeyDown, event.Key() == tcell.KeyRune && event.Rune() == 'j':
		delta = 1
	}
	if delta == 0 {
		return nil, false
	}
	// A genuine SetCurrentItem call (not one made while s.rebuilding)
	// already makes s.list's own SetChangedFunc update s.currentID and
	// clear s.following itself - no need to do either of those here too.
	if next := uikit.NextInteractiveRow(s.currentRows, s.list.GetCurrentItem(), delta); next != -1 {
		s.list.SetCurrentItem(next)
	}
	return nil, true
}

// translateKeyAlias: vim/emacs navigation aliases, translated to the
// native key tview itself already understands and handled identically by
// both List (main tree) and TextView (output view) - confirmed against
// tview's own source rather than reimplementing this logic here. j/k
// specifically only ever reach this translation for the output view now -
// the main tree's own j/k are already handled, skip-aware, by
// handleTreeRowSkip above. Returning a *different* event than the one
// passed in makes tview forward it to whichever primitive is currently
// focused, as if the user had typed that key - see tview's application.go.
// This is also what makes plain 'G'/Ctrl-E/'>' ride the exact same path
// plain End already does: ordinary navigation, deliberately with no
// special "resume autoscroll" side effect (that's F's job alone, in
// handleTreeKey below). '<'/'>' are plain mnemonic aliases for Home/End
// (think "jump to the start/end", same shape as many pagers/viewers) -
// not otherwise meaningful input in either the main tree or the output
// view, so safe to claim unconditionally the same way 'G' already is.
// Space/'b' are the same idea for Page down/up (a common pager convention,
// e.g. `less`/`man`) - claiming Space here does mean it's no longer also
// an alias for Enter in the main tree (TreeList's own native InputHandler
// used to activate the current row on Space, same as Enter); Enter alone
// still does that, so nothing is actually lost, just no longer duplicated
// onto this key.
func (s *liveSession) translateKeyAlias(event *tcell.EventKey) (*tcell.EventKey, bool) {
	switch {
	case event.Key() == tcell.KeyRune && event.Rune() == 'j':
		return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyRune && event.Rune() == 'k':
		return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyCtrlF:
		return tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyCtrlB:
		return tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyRune && event.Rune() == ' ':
		return tcell.NewEventKey(tcell.KeyPgDn, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyRune && event.Rune() == 'b':
		return tcell.NewEventKey(tcell.KeyPgUp, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyCtrlA:
		return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyCtrlE:
		return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyRune && event.Rune() == 'G':
		return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyRune && event.Rune() == '<':
		return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone), true
	case event.Key() == tcell.KeyRune && event.Rune() == '>':
		return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone), true
	}
	return nil, false
}

// handleOutputViewKey is everything handleKey falls through to once
// s.viewingOutput is true and none of the guards above claimed the key -
// the drill-down's own full key set. Always returns a non-nil event or
// nil explicitly; the trailing "return event" is the same "nothing here
// matched, pass it through untouched" fallback the original single
// closure had.
func (s *liveSession) handleOutputViewKey(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEscape && s.search.active != nil:
		// Layered per design-docs/Search.md: Esc clears an active search
		// first, rather than immediately closing the whole drill-down out
		// from under it - a second Esc (s.search.active is nil by then)
		// falls through to the case below and closes normally.
		s.search.clear()
		return nil
	case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyEnter:
		// Used to be tview.TextView's own native "done key" handling
		// (SetDoneFunc, which also fired on Tab/Backtab - harmless there
		// since those had no other meaning yet). Now explicit, since Tab/
		// Backtab below mean "switch tab" instead (design-docs/Tabbed
		// UI.md), and there's no single persistent TextView left to hang
		// a native SetDoneFunc off of - each tab's content is recreated
		// fresh on every renderOutputTabs call.
		s.closeOutput()
		return nil
	case event.Key() == tcell.KeyTab:
		s.outputTabs.Next()
		return nil
	case event.Key() == tcell.KeyBacktab:
		s.outputTabs.Prev()
		return nil
	case event.Key() == tcell.KeyLeft:
		s.navigateOutputHost(-1)
		return nil
	case event.Key() == tcell.KeyRight:
		s.navigateOutputHost(1)
		return nil
	// n/N, not n/p: Search.md's own next/prev-match convention (n
	// forward, N backward, no separate 'p') was made the one convention
	// this whole app uses for "step through a sequence," not just
	// search's own - task-hop/host-hop here and in host.go dropped their
	// own plain 'p' case to match, rather than leaving two different
	// next/prev idioms live side by side. Context-sensitive here
	// specifically: while a search found at least one match, n/N step
	// through matches instead of tasks - an active-but-empty search
	// (HasMatches false) falls through to the normal task-hop meaning
	// instead of doing nothing, so a stale "no matches" search can't
	// silently strand these keys.
	case event.Key() == tcell.KeyRune && event.Rune() == 'N':
		if s.search.hasMatches() {
			s.search.prev()
			return nil
		}
		s.navigateOutputTask(-1)
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'n':
		if s.search.hasMatches() {
			s.search.next()
			return nil
		}
		s.navigateOutputTask(1)
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == '/':
		s.search.open()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'y':
		s.outputBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
		s.outputBottomBar.SetText(uikit.CopyActiveTabStatus(s.outputTabs))
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'e':
		// Opens the file the currently displayed task's own source came
		// from, per source.go's TaskSourceIndex/task.Path - same
		// s.app.Suspend + $VISUAL/$EDITOR/vi mechanism the template
		// Verb's own 'e' binding already uses (template.go's
		// PreferredEditor). Deliberately does NOT refresh anything
		// afterward, unlike the template Verb - there's no live render to
		// redo here, and this view's own content (the task's already-
		// recorded result) can't change by editing the source after the
		// fact.
		if s.outputTask != nil {
			if file := uikit.TaskSourceFile(s.outputTask.Path); file != "" {
				s.app.Suspend(func() {
					cmd := exec.Command(template.PreferredEditor(), file)
					cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
					_ = cmd.Run()
				})
			}
		}
		return nil
	}
	return event
}

// handleTreeKey is everything handleKey falls through to once
// s.viewingOutput is false and none of the guards above claimed the key -
// the main tree's own full key set.
func (s *liveSession) handleTreeKey(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyRune && event.Rune() == 'F':
		// The only way to resume autoscroll (see translateKeyAlias above:
		// End/Ctrl-E/G are deliberately plain navigation now).
		// s.jumpingToEnd guards against SetCurrentItem's own resulting
		// "changed" event immediately flipping s.following back off - same
		// two-step dance End/G used to need for this exact reason.
		s.following = true
		s.jumpingToEnd = true
		if s.list.GetItemCount() > 0 {
			s.list.SetCurrentItem(s.list.GetItemCount() - 1)
		}
		s.jumpingToEnd = false
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'E':
		s.expandAll()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'C':
		s.collapseAll()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'r':
		// Rerun.md: only once a run has actually finished - a no-op while
		// ansible-playbook is still going, same "processDone gates it"
		// convention as the failure auto-jump in rebuild(). s.requestRerun
		// == nil is a second, independent reason to no-op here (design-
		// docs/Revisit.md's Phase 2: a replay session with rerun-from-
		// revisit not yet wired up passes nil rather than a real closure -
		// submitRerun would otherwise nil-panic calling it) -
		// currentMainBottomBarText already drops the "r: re-run" hint
		// whenever this is the case, so there's nothing advertised for
		// this to silently fail to do.
		if !s.processDone.Load() || s.requestRerun == nil {
			return nil
		}
		s.openRerunDialog()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'd':
		// design-docs/Diff.md: only once a run has actually finished, same
		// processDone gate 'r' already has - and only from the bare tree
		// (this whole switch is already un-reachable while s.viewingOutput,
		// a dialog is open, or a filter is active, so nothing further is
		// needed for "d can only be pressed from the tree view"). No 'd'
		// binding inside diff mode itself - RunDiffFlow's own Application
		// has no such case, by design.
		//
		// s.app.Suspend hands the real terminal to RunDiffFlow's own
		// nested Applications (the candidate-run list, then the diff
		// tree) for as long as the user keeps navigating them - the same
		// primitive already used for the output view's own 'e' (open
		// $EDITOR) - and automatically resumes THIS Application, exactly
		// where it left off, the moment RunDiffFlow returns. No custom
		// state save/restore needed for that "Esc/q eventually returns to
		// the standard tree view" requirement - it falls out of Suspend's
		// own contract for free.
		if !s.processDone.Load() {
			return nil
		}
		s.app.Suspend(func() {
			diff.RunDiffFlow(s.state, s.targetPlaybook, s.targetRole, s.initialTags, s.initialHosts, s.sourceIndex)
		})
		return nil
	case event.Key() == tcell.KeyRight:
		s.handleRight()
		return nil
	case event.Key() == tcell.KeyLeft:
		s.handleLeft()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'n':
		s.navigateMainTask(1)
		return nil
	// n/N, not n/p - see the same binding's own comment in
	// handleOutputViewKey above.
	case event.Key() == tcell.KeyRune && event.Rune() == 'N':
		s.navigateMainTask(-1)
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'f':
		// Main-tree-only, deliberately, same as '/' below: opening either
		// dialog while the output drill-down view is frontmost isn't
		// supported (the s.viewingOutput branch above already returned by
		// this point).
		s.openFilterDialog()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == '/':
		s.openSearchDialog()
		return nil
	}
	return event
}
