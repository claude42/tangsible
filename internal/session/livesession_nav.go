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

// expandAll/collapseAll back the main tree's E/C shortcuts.
// collapseAll's cursor-fallback: if the cursor was on a host row, that
// row is about to disappear - snap s.currentID to its enclosing task
// (still visible, now collapsed) rather than letting s.rebuild() fall
// back to index 0.
func (s *liveSession) expandAll() {
	for _, t := range uikit.AllTasks(s.state) {
		s.expanded[t] = true
	}
	// Extends to the recap section too (design-docs/Recap.md), for
	// consistency with E's own "expand everything" meaning everywhere
	// else in this app.
	for _, host := range s.state.AllHosts {
		s.recapHostExpanded[host] = true
		for _, cat := range recapForHost(s.state, host).Categories {
			s.recapCategoryExpanded[recapCategoryRowID{host: host, label: cat.Label}] = true
		}
	}
	s.rebuild()
}

func (s *liveSession) collapseAll() {
	switch id := s.currentID.(type) {
	case uikit.HostRowID:
		s.currentID = id.Task
	case recapTaskRowID:
		s.currentID = recapHostRowID(id.host)
	case recapCategoryRowID:
		s.currentID = recapHostRowID(id.host)
	}
	s.expanded = map[*playbook.TaskNode]bool{}
	s.recapHostExpanded = map[string]bool{}
	s.recapCategoryExpanded = map[recapCategoryRowID]bool{}
	s.rebuild()
}

// handleRight/handleLeft back the main tree's cursor-Right/cursor-Left
// expand/collapse shortcuts - they act on whichever row is currently
// under the cursor (s.currentRows[s.list.GetCurrentItem()]), not on
// s.currentID, since the cursor's actual on-screen position is what the
// user means by "this element".
func (s *liveSession) handleRight() {
	idx := s.list.GetCurrentItem()
	if idx < 0 || idx >= len(s.currentRows) {
		return
	}
	switch id := s.currentRows[idx].ID.(type) {
	case *playbook.TaskNode:
		if !s.expanded[id] {
			s.expanded[id] = true
			s.rebuild()
			s.revealExpandedTask(id)
		}
	case recapHostRowID:
		if !s.recapHostExpanded[string(id)] {
			s.recapHostExpanded[string(id)] = true
			s.rebuild()
		}
	case recapCategoryRowID:
		if !s.recapCategoryExpanded[id] {
			s.recapCategoryExpanded[id] = true
			s.rebuild()
		}
	}
	// Already-expanded task/host/category, a recap task line, a host
	// row, or a play row: no-op - see Keyboard-shortcuts.md's "Right
	// on an already-expanded element" decision.
}

func (s *liveSession) handleLeft() {
	idx := s.list.GetCurrentItem()
	if idx < 0 || idx >= len(s.currentRows) {
		return
	}
	switch id := s.currentRows[idx].ID.(type) {
	case *playbook.TaskNode:
		if s.expanded[id] {
			s.expanded[id] = false
			s.rebuild()
		}
	case uikit.HostRowID:
		// Collapsing the parent task removes this row - move the
		// cursor up to the task row that's left behind, per
		// Keyboard-shortcuts.md.
		s.expanded[id.Task] = false
		s.currentID = id.Task
		s.following = false
		s.rebuild()
	case recapHostRowID:
		if s.recapHostExpanded[string(id)] {
			s.recapHostExpanded[string(id)] = false
			s.rebuild()
		}
	case recapCategoryRowID:
		if s.recapCategoryExpanded[id] {
			s.recapCategoryExpanded[id] = false
			s.rebuild()
		}
	case recapTaskRowID:
		// Collapsing the parent category removes this row - move the
		// cursor up to the category row left behind, same reasoning
		// as HostRowID above.
		categoryID := recapCategoryRowID{host: id.host, label: id.label}
		s.recapCategoryExpanded[categoryID] = false
		s.currentID = categoryID
		s.following = false
		s.rebuild()
	}
	// Play row: no-op, plays aren't collapsible.
}

// navigateMainTask moves the cursor to the previous/next task (delta
// -1/+1) in run order, among currently-visible tasks only (see
// VisibleTasks - never targets a task the active filter is hiding,
// since FlattenRows wouldn't have given it a row to land on), expanding
// it if necessary. If the cursor was on a specific host of the current
// task, the same host is preserved on the destination task when that
// task has already recorded a result for it; otherwise the cursor lands
// on the destination task's own row. From a play row, "next" is that
// play's own first visible task; "prev" is whichever visible task comes
// immediately before that in the visible sequence - which, since
// VisibleTasks skips hidden tasks (and, transitively, plays with none
// visible) entirely, naturally lands on the previous *visible* play's
// last visible task without needing to search play-by-play - per
// Keyboard-shortcuts.md.
func (s *liveSession) navigateMainTask(delta int) {
	idx := s.list.GetCurrentItem()
	if idx < 0 || idx >= len(s.currentRows) {
		return
	}

	vis := uikit.VisibleTasks(s.state, s.currentFilter, s.sourceIndex, s.activeTaskNow())

	var target *playbook.TaskNode
	var host string
	haveHost := false

	switch id := s.currentRows[idx].ID.(type) {
	case *playbook.PlayNode:
		first := uikit.FirstVisibleTask(id, uikit.TaskSet(vis))
		if first == nil {
			return
		}
		pos := -1
		for i, t := range vis {
			if t == first {
				pos = i
				break
			}
		}
		if delta > 0 {
			target = first
		} else if pos > 0 {
			target = vis[pos-1]
		}
	case *playbook.TaskNode:
		for i, t := range vis {
			if t == id {
				if newIdx := i + delta; newIdx >= 0 && newIdx < len(vis) {
					target = vis[newIdx]
				}
				break
			}
		}
	case uikit.HostRowID:
		for i, t := range vis {
			if t == id.Task {
				if newIdx := i + delta; newIdx >= 0 && newIdx < len(vis) {
					target = vis[newIdx]
					host = id.Host
					haveHost = true
				}
				break
			}
		}
	}

	if target == nil {
		return
	}
	s.expanded[target] = true
	if _, ok := target.Hosts[host]; haveHost && ok {
		s.currentID = uikit.HostRowID{Task: target, Host: host}
	} else {
		s.currentID = target
	}
	s.following = false
	s.rebuild()
}
