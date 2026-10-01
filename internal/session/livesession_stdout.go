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
	"fmt"
	"strings"

	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The raw-output view ('o', design-docs/OwnCallbackPlugin.md's Phase 2):
// the current generation's console output - the user's own stdout callback
// merged with stderr (runner.OutputLog) - full screen, following new lines
// as they arrive while the run is live, the same way the tree follows new
// rows: any navigation stops following, only 'F' resumes it.
//
// Lines are appended to stdoutView incrementally (syncStdoutView, from every
// rebuild() while the view is open - the heartbeat drives that while the run
// is live), never re-set wholesale, so a long verbose run doesn't re-parse
// everything every 200ms.
//
// While an in-tab search is active, new lines are held back rather than
// appended: TextSearch re-renders the view from a snapshot of its text and
// restores that snapshot on Stop(), so appending underneath it would either
// be lost or shift every match. The top bar counts what's held; clearing the
// search appends it all.

const stdoutHintBarText = " F: follow  /: search  y: copy  esc/o/q: back  ↑/↓/j/k: scroll  space/b: page  CTRL-A/E: top/bottom "

// stdoutLineText renders one raw OutputLog line as TextView content: plain
// text tview-escaped (a literal "[" would otherwise be read as a tag), ANSI
// color codes translated to tview tags - or dropped, if color isn't usable
// (s.useColor). The trailing reset keeps one line's color from leaking into
// the next if a line ends mid-color.
func stdoutLineText(line string, color bool) string {
	segments, isEscape := runner.SplitANSI(line)
	var b strings.Builder
	for i, seg := range segments {
		if isEscape[i] {
			if color {
				b.WriteString(seg)
			}
			continue
		}
		b.WriteString(tview.Escape(seg))
	}
	if !color {
		return b.String()
	}
	return tview.TranslateANSI(b.String()) + "[-:-:-]"
}

// stdoutAvailable reports whether there's any output to show at all -
// nothing has spawned yet ("rerun"'s startup dialog), or a revisited run
// saved before .stdout files existed.
func (s *liveSession) stdoutAvailable() bool {
	return s.outLog != nil && s.outLog.Load() != nil
}

// openStdoutView switches to the raw-output view, following from the end.
func (s *liveSession) openStdoutView() {
	if !s.stdoutAvailable() {
		return
	}
	s.viewingStdout = true
	s.stdoutFollowing = true
	s.stdoutBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
	s.stdoutBottomBar.SetText(stdoutHintBarText)
	s.syncStdoutView()
	s.stdoutView.ScrollToEnd()
	s.switchPage("stdout")
	s.app.SetFocus(s.stdoutView)
	s.rebuild() // top bar
}

// closeStdoutView returns to the tree. The view's own content is kept, so
// reopening it doesn't re-render everything from scratch.
func (s *liveSession) closeStdoutView() {
	s.stdoutSearch.clear()
	s.viewingStdout = false
	s.switchPage("main")
	s.app.SetFocus(s.list)
	s.rebuild()
}

// syncStdoutView appends whatever the current generation's log has
// collected since the last call. A rerun swaps in a new log (runner.
// NewRequestRerun) - detected by identity, and the view starts over, since
// it only ever shows the current generation.
func (s *liveSession) syncStdoutView() {
	log := s.outLog.Load()
	if log != s.stdoutShownLog {
		s.stdoutSearch.clear()
		s.stdoutView.Clear()
		s.stdoutShownLog = log
		s.stdoutShown = 0
	}
	if log == nil || s.stdoutSearch.active != nil {
		return
	}
	lines := log.LinesFrom(s.stdoutShown)
	if len(lines) == 0 {
		return
	}
	var b strings.Builder
	for i, line := range lines {
		// Newline *before* every line but the very first, not after each:
		// a trailing newline would leave a permanently blank last row.
		if s.stdoutShown > 0 || i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(stdoutLineText(line, s.useColor))
	}
	s.stdoutShown += len(lines)

	// TextView follows appended text on its own whenever its internal
	// trackEnd flag is set - which End/G/scrolling to the bottom also set,
	// natively. Only 'F' means "follow" here (same as the tree), so when
	// not following, pin the viewport where it is: ScrollTo also clears
	// trackEnd.
	row, col := s.stdoutView.GetScrollOffset()
	fmt.Fprint(s.stdoutView, b.String())
	if s.stdoutFollowing {
		s.stdoutView.ScrollToEnd()
	} else {
		s.stdoutView.ScrollTo(row, col)
	}
}

// stdoutTopBarPlainText is the view's own top bar content, before the
// progress fill is applied (rebuild()).
func (s *liveSession) stdoutTopBarPlainText() string {
	text := " ansible-playbook output"
	log := s.outLog.Load()
	switch {
	case log == nil:
	case s.stdoutSearch.active != nil:
		if held := log.Len() - s.stdoutShown; held > 0 {
			text += fmt.Sprintf("   +%d new lines (shown once the search is cleared)", held)
		}
	case s.stdoutFollowing && !s.processDone.Load():
		text += "   following"
	case !s.stdoutFollowing && !s.processDone.Load():
		text += "   paused - F: follow"
	}
	return text
}

// handleStdoutViewKey is handleKey's dispatch while the raw-output view is
// frontmost - checked before the global 'q' handling and the key-alias
// translation, both of which it handles itself.
func (s *liveSession) handleStdoutViewKey(event *tcell.EventKey) *tcell.EventKey {
	// rebuild() runs syncStdoutView and refreshes the top bar - clearing
	// a search (Esc, or Enter on an empty query) should show held-back
	// lines and drop the "+N new lines" note right away, not on the next
	// heartbeat tick, which never comes once the run is frozen.
	defer func() {
		if s.viewingStdout {
			s.rebuild()
		}
	}()
	if s.stdoutSearch.composing {
		s.stdoutSearch.handleComposingKey(event)
		return nil
	}
	switch {
	case event.Key() == tcell.KeyEscape && s.stdoutSearch.active != nil:
		s.stdoutSearch.clear()
		return nil
	case event.Key() == tcell.KeyEscape,
		event.Key() == tcell.KeyRune && (event.Rune() == 'o' || event.Rune() == 'q'):
		s.closeStdoutView()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'F':
		s.stdoutSearch.clear() // following and a held-back search don't mix
		s.stdoutFollowing = true
		s.syncStdoutView()
		s.stdoutView.ScrollToEnd()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == '/':
		s.stdoutFollowing = false
		s.stdoutSearch.open()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'n':
		s.stdoutSearch.next()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'N':
		s.stdoutSearch.prev()
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'y':
		s.stdoutBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
		s.stdoutBottomBar.SetText(copyTextViewStatus(s.stdoutView))
		return nil
	}
	if translated, ok := s.translateKeyAlias(event); ok {
		event = translated
	}
	switch event.Key() {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn,
		tcell.KeyHome, tcell.KeyEnd, tcell.KeyLeft, tcell.KeyRight:
		s.stdoutFollowing = false
		// Called directly rather than returned for tview's own focus-
		// driven dispatch - same reasoning as tabSearchPanel.
		// handleComposingKey: that path has proven unreliable for
		// primitives nested inside this app's Pages.
		if handler := s.stdoutView.InputHandler(); handler != nil {
			handler(event, func(p tview.Primitive) { s.app.SetFocus(p) })
		}
	}
	return nil
}

// handleStdoutViewMouse: wheel scrolling over the text pans it (and, like
// any navigation, stops following); everything outside the text and the
// search prompt is swallowed, so a click on a bar can't steal focus.
func (s *liveSession) handleStdoutViewMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	x, y := event.Position()
	if s.stdoutSearch.composing {
		if uikit.InRect(x, y, s.stdoutSearch.input) {
			return event, action
		}
		return nil, action
	}
	if !uikit.InRect(x, y, s.stdoutView) {
		return nil, action
	}
	if (action == tview.MouseScrollUp || action == tview.MouseScrollDown) && s.stdoutFollowing {
		s.stdoutFollowing = false
		s.rebuild()
	}
	return event, action
}

// copyTextViewStatus copies tv's plain text to the clipboard and formats the
// result for the bottom bar - uikit.CopyActiveTabStatus's single-view
// counterpart.
func copyTextViewStatus(tv *tview.TextView) string {
	text := tv.GetText(true)
	if text == "" {
		return " copy failed: nothing to copy "
	}
	if err := uikit.CopyToClipboard(text); err != nil {
		return fmt.Sprintf(" copy failed: %s ", err)
	}
	return fmt.Sprintf(" copied output to clipboard (%d bytes) ", len(text))
}
