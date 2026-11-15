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
// run has ended. A page of its own with a tab per statistic: Slowest hosts, Slowest plays, Play timeline, Slowest tasks, Task timeline. Its content is rebuilt from the run's state on every rebuild(), so a
// terminal resize rescales the bars.

// statsTabNames lists the screen's tabs, in order.
var statsTabNames = []string{"Slowest hosts", "Slowest plays", "Play timeline", "Slowest tasks", taskTimelineTab}

// taskTimelineTab is the tab with the play picker.
const taskTimelineTab = "Task timeline"

// statsAvailable: the run has ended and at least one host has a known
// duration - the same condition the Slowest hosts tab needs something to show.
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
		s.statsPlaysView.SetText("No timing was recorded for this run.")
		s.statsTimelineView.SetText("No timing was recorded for this run.")
		s.statsTasksView.SetText("No timing was recorded for this run.")
		s.statsAllTasksView.SetText("No timing was recorded for this run.")
		return
	}
	_, _, width, _ := s.pages.GetInnerRect()
	s.statsHostsView.SetText(renderHostsTab(stats, width))
	s.statsPlaysView.SetText(renderPlaysTab(stats, width))
	s.statsTimelineView.SetText(renderTimelineTab(stats, width))
	s.renderTasks(stats, width)
	s.statsAllTasksView.SetText(renderAllTasksTab(stats, width))
}

// statsPlayPicked: the user chose a play in the picker.
func (s *liveSession) statsPlayPicked(string, int) {
	if !s.statsSyncing {
		s.renderStats()
	}
}

// renderTasks refills the play picker (keeping the chosen play) and draws the
// chosen play's timeline into the Tasks tab.
func (s *liveSession) renderTasks(stats runStats, width int) {
	if len(stats.PlayTasks) == 0 {
		s.statsTasksView.SetText("No timing was recorded for the tasks.")
		return
	}
	names := make([]string, len(stats.PlayTasks))
	widest := 0
	for i, p := range stats.PlayTasks {
		names[i] = p.Name
		if n := len([]rune(p.Name)); n > widest {
			widest = n
		}
	}
	chosen, _ := s.statsPlayPicker.GetCurrentOption()
	if chosen < 0 || chosen >= len(names) {
		chosen = 0
	}
	s.statsSyncing = true
	// SetOptions also replaces the selected callback, so it is passed here.
	s.statsPlayPicker.SetOptions(names, s.statsPlayPicked)
	s.statsPlayPicker.SetFieldWidth(widest + 2)
	s.statsPlayPicker.SetCurrentOption(chosen)
	s.statsSyncing = false
	s.statsTasksView.SetText(renderTasksTab(stats.PlayTasks[chosen], width))
	s.statsTasksView.ScrollToBeginning()
}

// handleStatsViewKey is handleKey's dispatch while the statistics screen is
// frontmost - checked before the global 'q' handling, which it replaces
// (q backs out here, like the other views).
func (s *liveSession) handleStatsViewKey(event *tcell.EventKey) *tcell.EventKey {
	if s.statsPlayPicker.IsOpen() {
		// The open list owns every key (Esc closes just it). Called directly:
		// see tabSearchPanel.handleComposingKey.
		s.statsPlayPicker.InputHandler()(event, func(p tview.Primitive) { s.app.SetFocus(p) })
		if !s.statsPlayPicker.IsOpen() {
			s.app.SetFocus(s.statsTabs.Primitive())
		}
		return nil
	}
	switch {
	case (event.Key() == tcell.KeyLeft || event.Key() == tcell.KeyRight) && s.statsTabs.ActiveName() == taskTimelineTab:
		// Previous / next play, without opening the picker; stops at the ends.
		i, _ := s.statsPlayPicker.GetCurrentOption()
		if event.Key() == tcell.KeyLeft {
			i--
		} else {
			i++
		}
		if i >= 0 && i < s.statsPlayPicker.GetOptionCount() {
			s.statsPlayPicker.SetCurrentOption(i)
		}
		return nil
	case event.Key() == tcell.KeyRune && event.Rune() == 'p' && s.statsTabs.ActiveName() == taskTimelineTab:
		s.statsPlayPicker.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
			func(p tview.Primitive) { s.app.SetFocus(p) })
		return nil
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
	if s.statsPlayPicker.IsOpen() || (s.statsTabs.ActiveName() == taskTimelineTab && uikit.InRect(x, y, s.statsPlayPicker)) {
		s.statsPlayPicker.MouseHandler()(action, event, func(p tview.Primitive) { s.app.SetFocus(p) })
		if !s.statsPlayPicker.IsOpen() {
			s.app.SetFocus(s.statsTabs.Primitive())
		}
		return nil, action
	}
	if action == tview.MouseLeftClick {
		s.statsTabs.HandleClick(x, y)
		return nil, action
	}
	if (action == tview.MouseScrollUp || action == tview.MouseScrollDown) && uikit.InRect(x, y, s.statsTabs.Primitive()) {
		return event, action
	}
	return nil, action
}
