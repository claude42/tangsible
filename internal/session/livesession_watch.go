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
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/debugverb"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// watchEntry is one persisted row in the watch list - just the raw typed
// expression; its value is never cached here. evaluateWatchExpr
// (debugwatch_eval.go) is cheap enough to redo every rebuild() against
// whatever snapshot is current for the selected host/task, which is the
// whole point of "re-evaluated live" (design-docs/DebugMode.md's Stage 4
// UX decisions).
type watchEntry struct {
	expr string

	// justSet is design-docs/DebugMode.md's "Stage 5" marker: true from a
	// successful set of this expr until the live pause moves on
	// (clearJustSet, called from debugApplyPauseResult/debugFinish) - it
	// only ever needs to survive exactly the window where the row's value
	// could be confused with ansible's own genuinely-computed one.
	justSet bool
}

// watchPanel is design-docs/DebugMode.md's "Stage 4" variable watch
// explorer - genuinely separable from the rest of liveSession the same
// way tabSearchPanel/rerunFieldSync already are. Debug-mode only, but
// always constructed (NewLiveTUI) so every call site stays a plain,
// unconditional method call gated on s.debugMode rather than a nil
// check - the same convention s.debugBPS/s.debugCursor already use.
type watchPanel struct {
	app   *tview.Application
	flex  *tview.Flex
	list  *tview.TextView // "Watched variables" heading + rows + (while open) a hint line
	input *tview.InputField

	// currentSnapshot resolves (snapshot, ok) for whatever host/task is
	// "current" right now - a closure over liveSession's own live state,
	// injected at construction so this type's own behavior stays
	// testable without building a whole liveSession.
	currentSnapshot func() (json.RawMessage, bool)

	entries     []watchEntry
	open        bool
	selected    int // -1 = input has focus; >=0 = index into entries
	acDismissed bool
}

func newWatchPanel(app *tview.Application, currentSnapshot func() (json.RawMessage, bool)) *watchPanel {
	w := &watchPanel{app: app, currentSnapshot: currentSnapshot, selected: -1}

	w.input = tview.NewInputField()
	uikit.StyleInputField(w.input)
	uikit.StyleAutocomplete(w.input)
	w.input.SetFieldTextColor(tcell.GetColor(uikit.KeyHighlightColor)) // green while typing - Stage 4 UX decision
	w.input.SetAutocompleteUseTags(false)                              // plain variable names, and avoids a literal '[' in a path being misread as a color tag
	w.input.SetChangedFunc(func(string) { w.acDismissed = false })

	w.list = tview.NewTextView().SetDynamicColors(true)
	w.list.SetTextColor(tcell.GetColor(uikit.StandardTextColor))

	w.flex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(w.list, 0, 0, false).
		AddItem(w.input, 0, 0, false)

	return w
}

// height reports how many rows the heading+entries block and the input
// row each need right now - 0,0 when there's nothing watched and the
// explorer is closed, which is what keeps the panel fully invisible
// (zero-height Flex items) in that state. The hint line that used to
// live here moved to whichever bottom chrome bar is already visible
// (watchSyncBottomBar) - "look like part of the chrome" (the follow-up
// round that moved it) reads oddly with a second, separate hint line of
// its own right above it. The heading itself counts even with zero
// entries as long as the explorer is open (render's own condition,
// mirrored here) - a follow-up round's report that opening a fresh
// explorer showed the input field but no "Watched variables" heading at
// all: previously the heading was gated on len(w.entries) > 0 alone, so
// it simply didn't exist yet the first time the panel opened with
// nothing watched - now it's a permanent panel title, shown the whole
// time the explorer is open.
func (w *watchPanel) height() (listH, inputH int) {
	if len(w.entries) == 0 && !w.open {
		return 0, 0
	}
	listH = 1 + len(w.entries) // heading + one row per entry
	if w.open {
		inputH = 1
	}
	return listH, inputH
}

// watchHint returns the bottom chrome bar's own text while the explorer
// is open (watchSyncBottomBar) - Esc staging (design-docs/DebugMode.md's
// "Stage 4 UX decisions") is reflected here so the visible hint always
// matches what Esc will actually do next.
func (w *watchPanel) watchHint(autocompleteOpen bool) string {
	switch {
	case w.selected != -1:
		return "↑↓: select  Delete/Backspace: remove variable  Esc: back"
	case autocompleteOpen:
		return "Enter: add  ↑↓: select  Esc: close suggestions"
	case w.input.GetText() != "":
		return "Enter: add  Esc: clear"
	default:
		return "Type variable name  ↑↓: select  Esc: close"
	}
}

// render rebuilds the watched-list TextView's own text and resizes both
// Flex items to match. evaluate is injected (rather than a direct
// evaluateWatchExpr/currentSnapshot call) purely so render stays a plain
// function of its inputs - called from rebuild() with a closure over the
// live snapshot lookup. chromeColorName paints the "Watched variables"
// headline with the session's own current chrome color (orange/maroon
// while debug-running/paused, otherwise whatever a plain `run` would
// use) as its background - a tag-name string (the same one
// chromeColorName/debugChromeBg's own callers already use for the top/
// split bars' progress-fill tags), not a tcell.Color, since this goes
// straight into an inline `[fg:bg]` tag, not a Box's own
// SetBackgroundColor. An earlier round painted the whole panel (including
// the entries and the input field) this way; tried live and reverted -
// only the headline keeps it now, the watched rows and the input field
// render with their own normal colors again, same as before that round.
// width is the panel's own current column count (the caller already
// knows it - whichever of totalWidth/the split output pane's own share
// currently applies) - the headline is padded out to it before the tag
// wraps it, so the chrome color fills the whole line instead of just the
// literal text run (tags only color the runes actually present, the
// same reason ComposeTopBarLine/s.outputTopBar's own plain text get
// padded before their fill tags, elsewhere in this file's siblings).
// snapshotAvailable is watchCurrentSnapshot's own second return value -
// false means the current host/task has no captured task_vars at all
// (the run hasn't paused there, and --all-vars wasn't passed, so nothing
// was ever captured to evaluate against), as opposed to a single watched
// expression simply not resolving against a snapshot that does exist.
// The heading says so explicitly rather than leaving every single row to
// separately render "<undefined>" with no explanation why.
func (w *watchPanel) render(evaluate func(string) watchResult, autocompleteOpen bool, chromeColorName string, width int, snapshotAvailable bool) {
	var b strings.Builder
	if w.open || len(w.entries) > 0 {
		// Leading space on both the heading and every entry row below -
		// requested live: the panel's own text sat flush against the
		// left edge, one column tighter than everything else in this
		// app's chrome (the top/split bars' own text, the tree's own
		// row labels) ever does.
		heading := " Watched variables"
		if !snapshotAvailable {
			heading += " (unavailable)"
		}
		if gap := width - len([]rune(heading)); gap > 0 {
			heading += strings.Repeat(" ", gap)
		}
		// "white" fg, not uikit.StandardTextColor - the same fixed
		// foreground every other chrome-colored bar in this codebase uses
		// against these particular background colors (buildChromeStyle's
		// own callers), chosen because they're saturated/dark enough that
		// a theme's own (possibly light) standard_text could easily stop
		// contrasting against them.
		fmt.Fprintf(&b, "[white:%s:b]%s[-:-:-]\n", chromeColorName, heading)
		for i, e := range w.entries {
			cursor := " "
			if i == w.selected {
				cursor = "▶"
			}
			line := fmt.Sprintf(" %s %s", cursor, tview.Escape(e.expr))
			// Deactivated (explorer closed, still showing watched
			// values) renders the whole block grey, collapsing the
			// resolved/unavailable distinction (an existing Stage 4 UX
			// decision, unaffected by the follow-up that dropped
			// unavailable's own separate color/strike-through below).
			color := uikit.StandardTextColor
			if !w.open {
				color = uikit.NothingReportedColor
			}
			valueText := formatWatchValue(evaluate(e.expr))
			if e.justSet {
				// Stage 5: a plain-text suffix, same convention as the
				// headline's own "(unavailable)" - no new color.
				valueText += " (just set)"
			}
			fmt.Fprintf(&b, "[%s::-]%s = %s[-::-]\n", color, line, valueText)
		}
	}
	w.list.SetText(b.String())

	listH, inputH := w.height()
	w.flex.ResizeItem(w.list, listH, 0)
	w.flex.ResizeItem(w.input, inputH, 0)
}

// formatWatchValue is render's own per-entry value formatter - a
// resolved value renders as itself; a rejected, unavailable, or
// out-of-context expression all render identically as the literal
// placeholder below (a follow-up round's "drop unavailable's own
// special rendering - no strike-through, no different color" request:
// the three cases were already visually indistinguishable in meaning,
// so now they're indistinguishable in rendering too, not just color).
func formatWatchValue(res watchResult) string {
	if res.kind == watchResolved {
		return tview.Escape(formatWatchScalar(res.value))
	}
	return "<undefined>"
}

// commitExpr adds expr (already trimmed and confirmed non-empty by the
// caller - handleWatchOpenKey) to the watch list if it parses, clearing
// the input field on success. false means a genuine syntax error - the
// caller shows the rejected-expression status message.
func (w *watchPanel) commitExpr(expr string) bool {
	if _, ok := parseWatchExpr(expr); !ok {
		return false
	}
	w.entries = append(w.entries, watchEntry{expr: expr})
	w.input.SetText("")
	w.acDismissed = false
	return true
}

// markJustSet flags the entry matching expr (a prior successful set of an
// already-watched variable), or appends a new one with the flag already
// set - a set of an expr not yet watched still needs a row to show its
// newly-confirmed value in, design-docs/DebugMode.md's "Stage 5".
func (w *watchPanel) markJustSet(expr string) {
	for i := range w.entries {
		if w.entries[i].expr == expr {
			w.entries[i].justSet = true
			return
		}
	}
	w.entries = append(w.entries, watchEntry{expr: expr, justSet: true})
}

// clearJustSet drops every entry's marker - called wherever the live
// pause changes (debugApplyPauseResult/debugFinish), per the Stage 5
// decision: once that happens, the next captured snapshot is genuinely-
// computed again either way, so there's nothing left to distinguish.
func (w *watchPanel) clearJustSet() {
	for i := range w.entries {
		w.entries[i].justSet = false
	}
}

// --- liveSession-level glue: candidate source, snapshot lookup, keys ---

// watchCurrentTaskHost resolves "whatever host/task is currently
// selected", reusing showOutputWithOrigin's own confirmed lookup pattern
// (livesession_output.go) rather than a new one. A play/bare-task/recap
// row selection (no single host to resolve against) is always
// "unavailable". Shared by watchCurrentSnapshot (the read side) and
// design-docs/DebugMode.md's "Stage 5" set-gate (watchAttemptSet), which
// additionally requires this to be the exact live pause (debugIsPaused),
// not just any host/task that happens to have a captured snapshot.
func (s *liveSession) watchCurrentTaskHost() (task *playbook.TaskNode, host string, ok bool) {
	if s.viewingOutput {
		task, host = s.outputTask, s.outputHost
	} else if hostRow, hrOk := s.currentID.(uikit.HostRowID); hrOk {
		task, host = hostRow.Task, hostRow.Host
	} else {
		return nil, "", false
	}
	if task == nil {
		return nil, "", false
	}
	return task, host, true
}

// watchCurrentSnapshot resolves watchCurrentTaskHost's own target to its
// captured task_vars snapshot - the simplest, most consistent v1
// behavior for anything watchCurrentTaskHost itself already calls
// "unavailable."
func (s *liveSession) watchCurrentSnapshot() (json.RawMessage, bool) {
	task, host, ok := s.watchCurrentTaskHost()
	if !ok {
		return nil, false
	}
	snap, ok := s.debugTaskVarsCache[debugVarsKey{debugverb.TaskLabel(task.Name, task.IsHandler), host}]
	return snap, ok
}

// watchCandidates lists the currently-selected host/task's own top-level
// scalar variables, excluding any already being watched, formatted as
// "name = value" - tview's native autocomplete drop-down has no
// secondary-text/second-column support (confirmed against tview's own
// source: ShowSecondaryText(false) is hardcoded), so the value is baked
// into the one string. Complex (non-scalar) values are never offered,
// matching "no autocomplete for complex variables" from the UX spec.
func (s *liveSession) watchCandidates() []string {
	snapshot, ok := s.watch.currentSnapshot()
	if !ok || len(snapshot) == 0 {
		return nil
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(snapshot, &root) != nil {
		return nil
	}
	watched := make(map[string]bool, len(s.watch.entries))
	for _, e := range s.watch.entries {
		watched[e.expr] = true
	}
	names := make([]string, 0, len(root))
	for name, raw := range root {
		if watched[name] {
			continue
		}
		var v any
		if json.Unmarshal(raw, &v) != nil || !isWatchScalar(v) {
			continue
		}
		names = append(names, name+" = "+formatWatchScalar(v))
	}
	sort.Strings(names)
	return names
}

// watchMatchCandidates is autocomplete.go's own "thin wrapper around
// uikit.FilteredMatch" convention (matchTags/matchTaskName) - matching
// must happen on the leading "name" portion only, since FilteredMatch's
// own substring fallback would otherwise match the token against a
// candidate's *value* half too.
func watchMatchCandidates(candidates []string, token string) []string {
	names := make([]string, len(candidates))
	byName := make(map[string]string, len(candidates))
	for i, c := range candidates {
		name, _, _ := strings.Cut(c, " = ")
		names[i] = name
		byName[name] = c
	}
	matched := uikit.FilteredMatch(names, token, nil)
	out := make([]string, len(matched))
	for i, n := range matched {
		out[i] = byName[n]
	}
	return out
}

func (s *liveSession) watchMatchCandidatesFunc(token string) []string {
	if token == "" {
		return nil // no drop-down until typing starts - same rule every other field follows
	}
	return watchMatchCandidates(s.watchCandidates(), token)
}

func (s *liveSession) watchAutocompleteOpenNow() bool {
	if s.watch.acDismissed {
		return false
	}
	return len(s.watchMatchCandidatesFunc(s.watch.input.GetText())) > 0
}

// watchAutocompleted is SetAutocompletedFunc's own apply callback -
// unlike rerunFieldSync's wireAutocomplete (which only ever fills the
// field), picking a watch candidate also immediately adds it to the
// watch list in the same keystroke (the UX spec: "press Enter to add it
// to the list of watched variables"), since tview's own InputField, once
// a real pick is made (Enter/Tab/click - never Navigate), never gives a
// second chance to react to it anywhere else.
func (s *liveSession) watchAutocompleted(text string, index, source int) bool {
	if source == tview.AutocompletedNavigate {
		return false // never preview into the field - same reasoning rerunFieldSync's own wireAutocomplete documents
	}
	name, _, _ := strings.Cut(text, " = ")
	s.watch.commitExpr(name) // a bare name picked from real snapshot keys is always syntactically valid
	s.rebuild()
	return true
}

// watchShowStatus shows a transient status message, reusing the
// drill-down's own 'y'-copy-status mechanism (livesession_tabsearch.go):
// a direct SetText with no timer, superseded by whatever next
// legitimately changes that same bar - deliberately not followed by
// watchSyncBottomBar (every other watch-key branch is), so the message
// isn't immediately clobbered by it. msg should already be fully
// tview-escaped by the caller (every caller embeds external text: a typed
// expression, a Python exception message, ...).
func (s *liveSession) watchShowStatus(msg string) {
	text := " " + msg + " "
	if s.viewingOutput {
		s.outputBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
		s.outputBottomBar.SetText(text)
	} else {
		s.bottomBar.SetTextStyle(s.debugChromeStyle())
		s.bottomBar.SetText(text)
	}
}

// watchShowRejected is watchShowStatus's own wrapper for a plain syntax
// error - the original, Stage 4 message.
func (s *liveSession) watchShowRejected(expr string) {
	s.watchShowStatus(fmt.Sprintf("Not a valid expression: %s", tview.Escape(expr)))
}

// watchAttemptSet is handleWatchOpenKey's own Enter-time handler for the
// watch panel's "expr = value" form (design-docs/DebugMode.md's "Stage
// 5"). Every contextual check happens synchronously here, before the REPL
// is ever touched: expr's own grammar, a non-empty value, and - critically
// - that the current selection *is* the exact live pause (debugIsPaused),
// since task_vars is only live/mutable for that one pause. A rejection
// here leaves the typed text in place, same convention a plain syntax
// error already follows; the field is only cleared once debugSetWatch
// confirms it actually dispatched the request - not unconditionally here,
// since debugSetWatch's own debugDriving/debugSess guard can still
// legitimately say no even after every check here passed (e.g. an
// unrelated F5/F10/F11/F12 is already mid-flight) - clearing the field
// regardless would silently discard what the user typed on exactly that
// race.
func (s *liveSession) watchAttemptSet(expr, value string) {
	ast, ok := parseWatchExpr(expr)
	if !ok {
		s.watchShowRejected(expr)
		return
	}
	if value == "" {
		s.watchShowStatus(fmt.Sprintf("Nothing to set %s to", tview.Escape(expr)))
		return
	}
	task, host, ok := s.watchCurrentTaskHost()
	if !ok || !s.debugIsPaused(task, host) {
		s.watchShowStatus("Not paused here - can't set a variable")
		return
	}
	if !s.debugSetWatch(expr, watchSetTarget(ast), value) {
		return // debugSetWatch already showed its own status
	}
	s.watch.input.SetText("")
}

// watchSyncBottomBar keeps whichever bottom chrome bar is currently
// visible (s.bottomBar for the tree page, s.outputBottomBar for the
// drill-down/split page - same s.viewingOutput switch watchShowRejected
// just above already uses) in sync with the explorer's own state: its
// own key hints while open (a follow-up round's "render the explorer's
// own shortcuts in the bottom chrome line instead of a separate hint row"
// request), restored to its normal text the instant it closes.
// Deliberately not done from rebuild() itself the way s.watch.render()
// is - these two bars aren't otherwise touched on every rebuild() pass (a
// 'y' copy-status or search-composing message on s.outputBottomBar is
// meant to survive exactly that kind of unrelated redraw, the existing
// "set once, superseded only by the next legitimate change" idiom) -
// called explicitly instead, at each watch-key transition that actually
// changes what the hint should say; safe to do unconditionally there
// since the explorer is fully modal while open, so neither of those other
// transient messages can be triggered in the meantime.
func (s *liveSession) watchSyncBottomBar() {
	hint := " " + s.watch.watchHint(s.watchAutocompleteOpenNow()) + " "
	if s.viewingOutput {
		s.outputBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
		if s.watch.open {
			s.outputBottomBar.SetText(hint)
		} else {
			s.outputBottomBar.SetText(outputBar)
		}
		return
	}
	s.bottomBar.SetTextStyle(s.debugChromeStyle())
	if s.watch.open {
		s.bottomBar.SetText(hint)
	} else {
		s.bottomBar.SetText(s.currentMainBottomBarText())
	}
}

// handleWatchKey is handleKey's own guard for the variable watch
// explorer - debug-mode only, and (unlike handleDebugKey) gated
// additionally on !s.viewingStdout: the raw console view has no host/
// task selection to resolve against, and isn't one of the three pages
// this panel is ever inserted into. While closed, this only ever claims
// a bare 'v' to open it; while open, it owns every key the same modal way
// the rerun/filter/search dialogs already do.
func (s *liveSession) handleWatchKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.debugMode || s.viewingStdout {
		return nil, false
	}
	if !s.watch.open {
		if event.Key() != tcell.KeyRune || event.Rune() != 'v' {
			return nil, false
		}
		s.watch.open = true
		s.watch.selected = -1
		s.watch.acDismissed = false
		// Required, not cosmetic: tview's own InputField.Draw only
		// renders the autocomplete drop-down while HasFocus() is true
		// (confirmed against tview's own source) - confirmed safe at
		// this nesting depth the same way tabSearchPanel's own doc
		// comment already confirms HasFocus() propagates correctly even
		// though normal key *dispatch* doesn't (which is exactly why
		// keys are still forwarded manually below, not relied on from
		// tview's own focus-driven routing).
		s.app.SetFocus(s.watch.input)
		// Blur the drill-down's own tabs explicitly - once they've ever
		// held real focus (e.g. the user pressed Tab at some point),
		// uikit.TabbedPane's own p.pages keeps reporting HasFocus() true
		// forever after (TabbedPane.Blur's own doc comment), regardless
		// of SetFocus above. Left alone, that stuck flag makes
		// tview.Flex.Draw treat *two* siblings (the tabs and watch.flex)
		// as "the focused one," both deferred - and since Go's defer
		// stack unwinds LIFO, the tabs (the earlier sibling, so the
		// first one registered) then draws *last*, painting over the
		// autocomplete drop-down everywhere the two overlap. Confirmed
		// live as the reported "popup's upper rows look clipped" bug:
		// only the drop-down's one row that shares no space with the
		// tabs (right above the input field) survived.
		s.outputTabs.Blur()
		s.rebuild()
		s.watchSyncBottomBar()
		return nil, true
	}
	if s.watchForwardPassthrough(event) {
		return nil, true
	}
	return s.handleWatchOpenKey(event), true
}

// watchForwardPassthrough is the explorer's one deliberate carve-out from
// being otherwise fully modal while open: page-content navigation (Page
// Up/Down, Home/End - never the plain arrow keys, which the explorer's
// own Up/Down/Left/Right already claim for its own list/field navigation)
// and Tab/Backtab (switching the drill-down's own tabs) must keep
// reaching whatever's behind the panel, requested live. The trouble is
// that s.app.SetFocus(s.watch.input) (handleWatchKey, above) means
// tview's own focus-based dispatch would otherwise deliver an
// unclaimed key straight to the watch input instead of to the tree/tab
// content that would normally have focus - so these are forwarded
// explicitly to the right widget's own InputHandler, bypassing the input
// field entirely, rather than merely left unclaimed here. Reports true
// (always "handled," even when there's nothing to forward to, e.g. Tab
// with no drill-down open) so the caller never falls through to
// handleWatchOpenKey's own default case, which would otherwise type
// these keys into the field.
func (s *liveSession) watchForwardPassthrough(event *tcell.EventKey) bool {
	switch event.Key() {
	case tcell.KeyTab:
		if s.viewingOutput {
			s.outputTabs.Next()
			s.rebuild()
		}
	case tcell.KeyBacktab:
		if s.viewingOutput {
			s.outputTabs.Prev()
			s.rebuild()
		}
	case tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		var target tview.Primitive
		if s.viewingOutput {
			if tv, ok := s.outputTabs.ActiveTextView(); ok {
				target = tv
			}
		} else {
			target = s.list
		}
		if target != nil {
			if handler := target.InputHandler(); handler != nil {
				// s.list's own InputHandler (the tree case) triggers its
				// changed-callback synchronously, which calls s.rebuild()
				// itself - same as any other tree navigation key. That
				// rebuild() (like the two explicit ones above) would
				// otherwise wipe the watch explorer's own bottom-bar hint
				// back to its normal text (rebuild's own
				// s.bottomBar.SetText(...) for the plain-tree/non-split
				// case), since it has no idea the explorer is still open -
				// watchSyncBottomBar below, after the switch, re-asserts
				// it regardless of which branch actually ran.
				handler(event, func(p tview.Primitive) { s.app.SetFocus(p) })
			}
		}
	default:
		return false
	}
	s.watchSyncBottomBar()
	return true
}

// handleWatchOpenKey is the explorer's own key state machine while open -
// see design-docs/DebugMode.md's "Stage 4 UX decisions" for the exact
// Esc-staging/Up-Down-target rules this implements. Every branch ends in
// s.rebuild() (to re-render the panel from the new state) except where
// a key is forwarded to tview's own InputField.InputHandler directly -
// the same "route around tview instead of fighting it" call
// tabSearchPanel.handleComposingKey already makes for a field nested
// this deep (root Pages -> main/output/split -> s.flex/outputFlex/
// outputBody -> watch.flex -> watch.input).
func (s *liveSession) handleWatchOpenKey(event *tcell.EventKey) *tcell.EventKey {
	w := s.watch
	switch event.Key() {
	case tcell.KeyEscape:
		switch {
		case w.selected != -1:
			w.selected = -1
			s.rebuild()
			s.watchSyncBottomBar()
			return nil
		case s.watchAutocompleteOpenNow():
			// Forwarded, not swallowed: tview's own InputField.InputHandler
			// closes its *internal* autocompleteList on a real KeyEscape
			// (confirmed against tview's own source) - that internal field,
			// not our own acDismissed bookkeeping, is what Draw() actually
			// checks to decide whether to keep rendering the drop-down.
			// acDismissed is set first anyway, for our own
			// watchAutocompleteOpenNow() check (Up/Down/Enter routing),
			// which would otherwise still see the same, unchanged text as
			// a fresh match on the very next call.
			w.acDismissed = true
			return s.watchForwardToInput(event)
		case w.input.GetText() != "":
			w.input.SetText("")
			w.acDismissed = false
		default:
			w.open = false
			w.selected = -1
			// Restore keyboard focus to s.list - the one app-wide default
			// every other transient focus-stealer (the filter/search/
			// rerun dialogs, the output tab search bar) already restores
			// it to on close (livesession_filter.go's closeDialogs,
			// livesession_output.go's closeOutput, both
			// s.app.SetFocus(s.list)). Opening the explorer
			// (handleWatchKey, above) moves focus onto s.watch.input and
			// never gets it back on its own - reported live: closing the
			// explorer without ever having opened/closed the drill-down
			// in between (that path's own closeOutput happens to paper
			// over this) left focus stranded on the now-invisible input,
			// so Enter on a host row - like every other key this app
			// doesn't itself intercept, only reaching whatever currently
			// HasFocus() - silently went nowhere instead of reaching
			// s.list's own row-activation handler.
			s.app.SetFocus(s.list)
		}
		s.rebuild()
		s.watchSyncBottomBar()
		return nil

	case tcell.KeyEnter:
		if w.selected != -1 {
			return nil // no meaning while a watched row is selected
		}
		if s.watchAutocompleteOpenNow() {
			return s.watchForwardToInput(event) // tview's own pick-and-close; watchAutocompleted does the "add"
		}
		text := strings.TrimSpace(w.input.GetText())
		if text == "" {
			return nil // Enter on an empty field is a no-op
		}
		if exprPart, valuePart, isSet := splitWatchSetAssignment(text); isSet {
			s.watchAttemptSet(exprPart, valuePart)
			return nil
		}
		if !w.commitExpr(text) {
			s.watchShowRejected(text)
			return nil
		}
		s.rebuild()
		s.watchSyncBottomBar()
		return nil

	case tcell.KeyUp, tcell.KeyDown:
		if w.input.GetText() == "" {
			if w.selected == -1 && event.Key() == tcell.KeyUp {
				// Reported live: the clamp below pinned this at -1
				// forever (lo=-1, delta=-1 from an already-at-lo
				// selected), so Up never did anything from "nothing
				// selected" - only Down (which lands on 0, the head of
				// the list) worked. Jumping straight to the last entry
				// here is the one exception to the "no wraparound"
				// clamp below: len(w.entries)-1 is -1 when there's
				// nothing to select, so this stays a no-op in that case
				// too, with no extra guard needed.
				w.selected = len(w.entries) - 1
			} else {
				delta := -1
				if event.Key() == tcell.KeyDown {
					delta = 1
				}
				w.selected = watchClamp(w.selected+delta, -1, len(w.entries)-1)
			}
			s.rebuild()
			s.watchSyncBottomBar()
			return nil
		}
		if s.watchAutocompleteOpenNow() {
			return s.watchForwardToInput(event) // let tview navigate its own drop-down
		}
		return nil // non-empty input, zero matches: ignored per spec

	case tcell.KeyDelete, tcell.KeyBackspace, tcell.KeyBackspace2:
		if w.selected == -1 {
			if event.Key() == tcell.KeyDelete {
				return nil // no meaning while typing
			}
			return s.watchForwardToInput(event) // Backspace/Backspace2 edit the field as usual
		}
		// A follow-up round asked for Backspace to remove the selected
		// entry too, not just Delete - a plain, unlabeled "Delete" key
		// doesn't exist on every keyboard (notably Mac laptops, where the
		// labeled "delete" key sends Backspace), and there's nothing else
		// for Backspace to do once a row - not the input field - is what's
		// selected.
		w.entries = append(w.entries[:w.selected], w.entries[w.selected+1:]...)
		if w.selected >= len(w.entries) {
			w.selected = len(w.entries) - 1
		}
		s.rebuild()
		s.watchSyncBottomBar()
		return nil

	default:
		if w.selected != -1 {
			return nil // only Up/Down/Delete/Backspace/Esc mean anything with a row selected
		}
		return s.watchForwardToInput(event)
	}
}

func (s *liveSession) watchForwardToInput(event *tcell.EventKey) *tcell.EventKey {
	if handler := s.watch.input.InputHandler(); handler != nil {
		handler(event, func(p tview.Primitive) { s.app.SetFocus(p) })
	}
	s.rebuild()
	s.watchSyncBottomBar()
	return nil
}

func watchClamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
