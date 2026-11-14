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

// The statistics screen (design-docs/Stats.md): 's' from the tree, once the
// run has ended. A page of its own with a tab per statistic; for now only
// Hosts. Its content is rebuilt from the run's state on every rebuild(), so a
// terminal resize rescales the bars.

// statsTabNames lists the screen's tabs, in order.
var statsTabNames = []string{"Hosts"}

// statsAvailable: the run has ended and at least one host has a known
// duration - the same condition the Hosts tab needs something to show.
func (s *liveSession) statsAvailable() bool {
	if !s.processDone.Load() {
		return false
	}
	_, ok := computeRunStats(s.state)
	return ok
}

// openStatsView switches to the statistics screen.
func (s *liveSession) openStatsView() {
	if !s.statsAvailable() {
		return
	}
	s.viewingStats = true
	s.statsTopBar.SetTextStyle(s.outputBottomBarNormalStyle())
	s.statsBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
	s.statsBottomBar.SetText(statsBar)
	s.statsTabs.SetHeaderStyle(s.outputBottomBarNormalStyle(), s.chromeColorName())
	s.switchPage("stats")
	s.rebuild() // renders the content at the current width
	s.app.SetFocus(s.statsTabs.Primitive())
}

// closeStatsView returns to the tree.
func (s *liveSession) closeStatsView() {
	s.viewingStats = false
	s.switchPage("main")
	s.app.SetFocus(s.list)
	s.rebuild()
}

// renderStats fills the tabs from the current state, at the page's width.
func (s *liveSession) renderStats() {
	s.statsTopBar.SetText(" Statistics")
	stats, ok := computeRunStats(s.state)
	if !ok {
		s.statsHostsView.SetText("No timing was recorded for this run.")
		return
	}
	_, _, width, _ := s.pages.GetInnerRect()
	s.statsHostsView.SetText(renderHostsTab(stats, width))
}

// handleStatsViewKey is handleKey's dispatch while the statistics screen is
// frontmost - checked before the global 'q' handling, which it replaces
// (q backs out here, like the other views).
func (s *liveSession) handleStatsViewKey(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyEscape,
		event.Key() == tcell.KeyRune && (event.Rune() == 's' || event.Rune() == 'q'):
		s.closeStatsView()
		return nil
	case event.Key() == tcell.KeyTab:
		s.statsTabs.Next()
		return nil
	case event.Key() == tcell.KeyBacktab:
		s.statsTabs.Prev()
		return nil
	case uikit.IsHelpKey(event):
		s.help.Open(statsHelp, nil)
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'y':
		s.statsBottomBar.SetTextStyle(s.outputBottomBarNormalStyle())
		s.statsBottomBar.SetText(uikit.CopyActiveTabStatus(s.statsTabs))
		return nil
	}
	if translated, ok := s.translateKeyAlias(event); ok {
		event = translated
	}
	switch event.Key() {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		// Called directly: see tabSearchPanel.handleComposingKey for why
		// forwarding to a nested primitive doesn't reliably work here.
		if tv, ok := s.statsTabs.ActiveTextView(); ok {
			if handler := tv.InputHandler(); handler != nil {
				handler(event, func(p tview.Primitive) { s.app.SetFocus(p) })
			}
		}
	}
	return nil
}

// handleStatsViewMouse: a click on a tab header switches tabs, the wheel
// scrolls the content, everything else is swallowed so a click on a bar can't
// steal focus.
func (s *liveSession) handleStatsViewMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	x, y := event.Position()
	if action == tview.MouseLeftClick {
		s.statsTabs.HandleClick(x, y)
		return nil, action
	}
	if (action == tview.MouseScrollUp || action == tview.MouseScrollDown) && uikit.InRect(x, y, s.statsTabs.Primitive()) {
		return event, action
	}
	return nil, action
}
