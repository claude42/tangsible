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
// (zero-height Flex items) in that state.
func (w *watchPanel) height() (listH, inputH int) {
	if len(w.entries) == 0 && !w.open {
		return 0, 0
	}
	if len(w.entries) > 0 {
		listH = 1 + len(w.entries) // heading + one row per entry
	}
	if w.open {
		listH += 2 // blank separator + hint line
		inputH = 1
	}
	return listH, inputH
}

// watchHint returns the bottom hint line's own text for the panel's
// current sub-state - Esc staging (design-docs/DebugMode.md's "Stage 4
// UX decisions") is reflected here so the visible hint always matches
// what Esc will actually do next.
func (w *watchPanel) watchHint(autocompleteOpen bool) string {
	switch {
	case w.selected != -1:
		return "↑↓: select  Delete: remove variable  Esc: back"
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
// live snapshot lookup.
func (w *watchPanel) render(evaluate func(string) watchResult, autocompleteOpen bool) {
	var b strings.Builder
	if len(w.entries) > 0 {
		fmt.Fprintf(&b, "[%s::b]Watched variables[-:-:-]\n", uikit.StandardTextColor)
		for i, e := range w.entries {
			cursor := " "
			if i == w.selected {
				cursor = "▶"
			}
			line := fmt.Sprintf("%s %s", cursor, tview.Escape(e.expr))
			if !w.open {
				// Deactivated but still showing watched values - the
				// whole block renders grey, collapsing the resolved/
				// unavailable distinction (Stage 4 UX decision).
				fmt.Fprintf(&b, "[%s::-]%s = %s[-::-]\n", uikit.NothingReportedColor, line, w.renderValuePlain(evaluate(e.expr)))
				continue
			}
			switch res := evaluate(e.expr); res.kind {
			case watchResolved:
				fmt.Fprintf(&b, "[%s::-]%s = %s[-::-]\n", uikit.StandardTextColor, line, tview.Escape(formatWatchScalar(res.value)))
			default: // watchUnavailable (watchRejected never reaches a stored entry)
				// Lowercase 's' - tview's tag convention is lowercase
				// *sets* an attribute, uppercase *clears* it (confirmed
				// against strings.go's own parseTag); an uppercase 'S'
				// here would silently be a no-op, never striking through
				// anything.
				fmt.Fprintf(&b, "[%s::s]%s = unavailable[-::-]\n", uikit.IgnoredColor, line)
			}
		}
	}
	if w.open {
		if len(w.entries) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s::-]%s[-:-:-]\n", uikit.StandardTextColor, w.watchHint(autocompleteOpen))
	}
	w.list.SetText(b.String())

	listH, inputH := w.height()
	w.flex.ResizeItem(w.list, listH, 0)
	w.flex.ResizeItem(w.input, inputH, 0)
}

// renderValuePlain is render's own helper for the deactivated (grey)
// state, where resolved/unavailable/rejected all look identical - only
// a resolved value has real text to show; anything else renders as the
// same "unavailable" placeholder the active state uses for its own
// unavailable rows.
func (w *watchPanel) renderValuePlain(res watchResult) string {
	if res.kind == watchResolved {
		return tview.Escape(formatWatchScalar(res.value))
	}
	return "unavailable"
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

// --- liveSession-level glue: candidate source, snapshot lookup, keys ---

// watchCurrentSnapshot resolves "whatever host/task is currently
// selected" to its captured task_vars snapshot, reusing
// showOutputWithOrigin's own confirmed lookup pattern
// (livesession_output.go) rather than a new one. A play/bare-task/recap
// row selection (no single host to resolve against) is always
// "unavailable" - the simplest, most consistent v1 behavior.
func (s *liveSession) watchCurrentSnapshot() (json.RawMessage, bool) {
	var task *playbook.TaskNode
	var host string
	if s.viewingOutput {
		task, host = s.outputTask, s.outputHost
	} else if hostRow, ok := s.currentID.(uikit.HostRowID); ok {
		task, host = hostRow.Task, hostRow.Host
	} else {
		return nil, false
	}
	if task == nil {
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

// watchShowRejected shows a transient "rejected" status, reusing the
// drill-down's own 'y'-copy-status mechanism (livesession_tabsearch.go):
// a direct SetText with no timer, superseded by whatever next
// legitimately changes that same bar (every other watch-panel key ends
// in s.rebuild(), which never touches these bars itself).
func (s *liveSession) watchShowRejected(expr string) {
	msg := fmt.Sprintf(" Not a valid expression: %s ", tview.Escape(expr))
	if s.viewingOutput {
		s.outputBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
		s.outputBottomBar.SetText(msg)
	} else {
		s.bottomBar.SetTextStyle(s.debugChromeStyle())
		s.bottomBar.SetText(msg)
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
		s.rebuild()
		return nil, true
	}
	return s.handleWatchOpenKey(event), true
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
		}
		s.rebuild()
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
		if !w.commitExpr(text) {
			s.watchShowRejected(text)
			return nil
		}
		s.rebuild()
		return nil

	case tcell.KeyUp, tcell.KeyDown:
		if w.input.GetText() == "" {
			delta := -1
			if event.Key() == tcell.KeyDown {
				delta = 1
			}
			w.selected = watchClamp(w.selected+delta, -1, len(w.entries)-1)
			s.rebuild()
			return nil
		}
		if s.watchAutocompleteOpenNow() {
			return s.watchForwardToInput(event) // let tview navigate its own drop-down
		}
		return nil // non-empty input, zero matches: ignored per spec

	case tcell.KeyDelete:
		if w.selected == -1 {
			return nil // no meaning while typing
		}
		w.entries = append(w.entries[:w.selected], w.entries[w.selected+1:]...)
		if w.selected >= len(w.entries) {
			w.selected = len(w.entries) - 1
		}
		s.rebuild()
		return nil

	default:
		if w.selected != -1 {
			return nil // only Up/Down/Delete/Esc mean anything with a row selected
		}
		return s.watchForwardToInput(event)
	}
}

func (s *liveSession) watchForwardToInput(event *tcell.EventKey) *tcell.EventKey {
	if handler := s.watch.input.InputHandler(); handler != nil {
		handler(event, func(p tview.Primitive) { s.app.SetFocus(p) })
	}
	s.rebuild()
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
