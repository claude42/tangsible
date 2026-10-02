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

// The '?'/F1 help dialog (design-docs/Help.md) - an overlay page on top of
// whatever's showing, like the filter dialog, so the tree keeps updating
// behind it. Rebuilt on every open (AddPage replaces the previous "help"
// page and puts it on top): its size follows the terminal's current size,
// and its content follows the session's current context (treeHelpHidden).

func (s *liveSession) openHelp() {
	_, _, width, height := s.pages.GetInnerRect()
	view, modal := uikit.NewHelpDialog(treeHelp, width, height, s.treeHelpHidden)
	s.helpView = view
	s.helpOpen = true
	s.pages.AddPage("help", modal, true, true)
	s.app.SetFocus(view)
}

func (s *liveSession) closeHelp() {
	if !s.helpOpen {
		return
	}
	s.helpOpen = false
	s.pages.RemovePage("help")
	s.app.SetFocus(s.list)
}

// handleHelpKey: while help is open it owns the keyboard (Ctrl-C excepted,
// handled before this - see handleCtrlC). Esc, ?, F1 and q close it - q
// closes rather than quits, the filter dialog's convention for a plain
// read-only overlay; navigation keys (and their j/k/Space/... aliases)
// scroll it; everything else is swallowed.
func (s *liveSession) handleHelpKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.helpOpen {
		return nil, false
	}
	switch {
	case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyF1,
		event.Key() == tcell.KeyRune && (event.Rune() == '?' || event.Rune() == 'q'):
		s.closeHelp()
		return nil, true
	}
	if translated, ok := s.translateKeyAlias(event); ok {
		event = translated
	}
	switch event.Key() {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		// Called directly, not returned for tview's focus-driven dispatch
		// - see handleStdoutViewKey's identical call for why.
		if handler := s.helpView.InputHandler(); handler != nil {
			handler(event, func(p tview.Primitive) { s.app.SetFocus(p) })
		}
	}
	return nil, true
}

// handleHelpMouse: the wheel scrolls the dialog; everything else - inside
// it or outside - is swallowed, so nothing behind it reacts while it's
// open and a click can't move focus away from it.
func (s *liveSession) handleHelpMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	if !s.helpOpen {
		return nil, action, false
	}
	if x, y := event.Position(); uikit.InRect(x, y, s.helpView) &&
		(action == tview.MouseScrollUp || action == tview.MouseScrollDown) {
		return event, action, true
	}
	return nil, action, true
}
