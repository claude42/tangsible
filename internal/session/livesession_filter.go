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
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// openFilterDialog/openSearchDialog/closeDialogs/applyFilter back the
// 'f'/'/' shortcuts and the two dialogs themselves (Filters.md). Both
// dialogs are fully modal (see SetInputCapture/SetMouseCapture) so these
// are the only places s.filterDialogOpen/s.searchDialogOpen/
// s.currentFilter ever change.
func (s *liveSession) openFilterDialog() {
	s.filterDialogOpen = true
	s.filterDialog.SetText(uikit.FilterDialogText(s.currentFilter))
	s.pages.ShowPage("filter")
}

// openSearchDialog pre-fills the box with the previous term whenever one
// is already active (Filters.md's explicit "reopening the dialog while
// the search filter is already active should show it right away") and,
// unlike the old combined dialog, moves keyboard focus into it
// immediately - there's no menu to browse first in a search-only dialog,
// so there's nothing to wait for before typing.
func (s *liveSession) openSearchDialog() {
	s.searchDialogOpen = true
	if s.currentFilter.Mode == uikit.FilterSearch {
		s.searchInput.SetText(s.currentFilter.Search)
	} else {
		s.searchInput.SetText("")
	}
	s.pages.ShowPage("search")
	s.app.SetFocus(s.searchInput)
}

// closeDialogs closes whichever of the three dialogs is currently open
// (harmless no-op on the other two) with no filter/search/rerun change -
// shared by all three dialogs' own Esc/q/Ctrl-C handling, by applyFilter,
// and by submitRerun, so there's exactly one place that resets this state
// and refocuses the main tree.
func (s *liveSession) closeDialogs() {
	s.filterDialogOpen = false
	s.searchDialogOpen = false
	s.rerunDialogOpen = false
	s.pages.HidePage("filter")
	s.pages.HidePage("search")
	s.pages.HidePage("rerun")
	s.app.SetFocus(s.list) // undo openSearchDialog's/openRerunDialog's
	// SetFocus above, if either ran - harmless no-op if neither did
	// (s.list already has focus in that case).
}

// applyFilter switches to newFilter (a no-op switch still closes
// whichever dialog is open, matching "when the user presses a/c/f the
// respective filter shall be activated and the window shall be closed
// again" - the search dialog's own Enter-to-apply, wired up on
// s.searchInput's SetDoneFunc, funnels through here too).
//
// If the cursor is currently pinned to a specific row (s.following ==
// false - if it's true, s.rebuild() already re-resolves the selection to
// the newest *visible* row every time, so there's nothing to fix up),
// and that row's task won't survive the new filter, this moves
// s.currentID to the nearest still-visible task first (see
// NearestVisibleTask) - Filters.md's "cursor moves to the nearest
// still-visible ancestor" requirement. A task is always the right
// granularity to land on here: per Filters.md, a filter can only ever
// hide a whole task (and, transitively, a whole play with none left) at
// once, never an individual host row on its own - unlike collapsing a
// task, which removes host rows one task at a time while the task's own
// row stays put, a filter switch never leaves a "row still there, just
// fall back to it" case to fall back to.
func (s *liveSession) applyFilter(newFilter uikit.FilterQuery) {
	if newFilter != s.currentFilter && !s.following {
		activeTask := s.activeTaskNow()
		var anchor *playbook.TaskNode
		switch id := s.currentID.(type) {
		case *playbook.TaskNode:
			anchor = id
		case uikit.HostRowID:
			anchor = id.Task
		case *playbook.PlayNode:
			stillVisible := false
			for _, t := range id.Tasks {
				if uikit.TaskVisible(t, newFilter, s.sourceIndex, t == activeTask) {
					stillVisible = true
					break
				}
			}
			if !stillVisible && len(id.Tasks) > 0 {
				anchor = id.Tasks[0]
			}
		}
		if anchor != nil && !uikit.TaskVisible(anchor, newFilter, s.sourceIndex, anchor == activeTask) {
			if nt := uikit.NearestVisibleTask(uikit.AllTasks(s.state), anchor, uikit.VisibleTasks(s.state, newFilter, s.sourceIndex, activeTask)); nt != nil {
				s.currentID = nt
			}
		}
	}
	s.currentFilter = newFilter
	s.closeDialogs()
	s.rebuild()
}
