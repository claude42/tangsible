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
	"time"

	"code.aw.net/claude/tangsible/internal/ansibledoc"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/staticplay"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// startupView is main's hand-over of a view session's static data to
// NewLiveTUI, which already takes about forty parameters (set once, before
// NewLiveTUI runs; nil for every other verb).
var startupView *staticplay.View

// The view verb (design-docs/View.md): the tree of plays and tasks of a
// playbook nothing has run, kept in the ordinary live session so the tree,
// the two-pane layout, search and the drill-down are the real ones. While
// s.view is set the session is idle - no generation, no hosts - Enter on a
// task opens a three-tab drill-down (Task, Task definition, Docs), and 'r'
// opens the usual re-run dialog; confirming it starts a real run in place
// and clears s.view (endView), after which this is a plain run session.

// viewTaskInfo is what the drill-down of a static task shows.
func (s *liveSession) viewTaskInfo(task *playbook.TaskNode) uikit.ViewTask {
	t := s.view.Tasks[task]
	info := uikit.ViewTask{Name: task.Name, Path: task.Path, Action: t.Action, Tags: t.Tags, When: t.When}
	if play := staticplay.PlayOf(s.state, task); play != nil {
		info.Hosts = s.view.Hosts[play]
	}
	return info
}

// viewTopBarLine is the tree's top bar in a view session: no run, so no
// filter, host list, progress or clock - the playbook and what it holds.
func (s *liveSession) viewTopBarLine(width int) string {
	tasks := len(uikit.AllTasks(s.state))
	left := fmt.Sprintf(" View: %s   %d %s, %d %s", s.topBarName(), len(s.state.Plays), plural(len(s.state.Plays), "play", "plays"), tasks, plural(tasks, "task", "tasks"))
	if gap := width - len([]rune(left)); gap > 0 {
		left += strings.Repeat(" ", gap)
	}
	return left
}

// viewSplitHeaderLine is the two-pane header: the same left part as the
// plain bar, then, from the column the drill-down pane starts at, the play
// and task shown there.
func (s *liveSession) viewSplitHeaderLine(width, treeWidth int) string {
	left := fmt.Sprintf(" View: %s", s.topBarName())
	if pad := treeWidth + uikit.SplitDividerWidth + 1 - len([]rune(left)); pad > 0 {
		left += strings.Repeat(" ", pad)
	} else {
		left += " "
	}
	line := left + strings.TrimSpace(s.outputTopBarPlainText)
	if gap := width - len([]rune(line)); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	return line
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// viewSelectRows makes Enter (and a click) on a task row open its
// drill-down instead of expanding it (there are no host rows to show).
func (s *liveSession) viewSelectRows(rows []uikit.Row) {
	for i := range rows {
		if task, ok := rows[i].ID.(*playbook.TaskNode); ok {
			rows[i].Selected = func() { s.showViewTask(task) }
		}
	}
}

// showViewTask opens the drill-down for a statically listed task.
func (s *liveSession) showViewTask(task *playbook.TaskNode) {
	info := s.viewTaskInfo(task)
	s.outputTask, s.outputHost = task, ""
	playName := ""
	if play := staticplay.PlayOf(s.state, task); play != nil {
		playName = play.Name
	}
	s.outputTopBarPlainText = fmt.Sprintf(" %s — %s ", playName, task.Name)

	// Docs, fetched in the background like the run drill-down does; the tab
	// is absent until there is text (and for an action with no docs).
	if action := info.Action; action != "" {
		s.prefetchContextHelp(action) // the Task definition tab's click pop-up
		if _, cached := s.docsCache[action]; !cached {
			s.docsCache[action] = uikit.ResolvedRender{Pending: true}
			go func() {
				text, err := ansibledoc.FetchAnsibleDoc(action)
				result := uikit.ResolvedRender{}
				if err != nil {
					result.Err = err.Error()
				} else {
					result.Text = text
				}
				s.app.QueueUpdateDraw(func() {
					s.docsCache[action] = result
					if s.outputTask == task && s.viewingOutput && !uikit.DocsTabHidden(result) {
						s.renderViewTabs(task)
					}
				})
			}()
		}
	}
	s.renderViewTabs(task)
	s.search.hint = viewOutputBar
	s.outputBottomBar.SetText(viewOutputBar + s.viewNotice)
	s.viewingOutput = true
	s.viewingOutputFromRecap = false
	s.currentID = task
	s.following = false
	s.rebuild()
}

// renderViewTabs (re)builds the drill-down's tabs for task.
func (s *liveSession) renderViewTabs(task *playbook.TaskNode) {
	s.search.clear()
	info := s.viewTaskInfo(task)
	names, contents := uikit.BuildViewTabs(info, s.sourceIndex, s.docsCache[info.Action])
	s.setOutputTabs(names, contents)
}

// viewNavigateTask moves the open drill-down to the previous/next task of
// the tree (n/N), without wrapping.
func (s *liveSession) viewNavigateTask(delta int) {
	if s.outputTask == nil {
		return
	}
	tasks := uikit.AllTasks(s.state)
	for i, t := range tasks {
		if t == s.outputTask {
			if j := i + delta; j >= 0 && j < len(tasks) {
				s.showViewTask(tasks[j])
			}
			return
		}
	}
}

// viewTaskKey identifies a task across a reload by what the user sees: its
// play's name, its own name, and which of the same-named tasks of the plays
// of that name it is (lines move when a file is edited, node identities do
// not survive a reload at all).
type viewTaskKey struct {
	play, task string
	nth        int
}

func viewKeyOf(state *playbook.PlaybookState, task *playbook.TaskNode) (viewTaskKey, bool) {
	counts := map[viewTaskKey]int{}
	for _, p := range state.Plays {
		for _, t := range p.Tasks {
			k := viewTaskKey{play: p.Name, task: t.Name}
			if t == task {
				k.nth = counts[viewTaskKey{play: p.Name, task: t.Name}]
				return k, true
			}
			counts[k]++
		}
	}
	return viewTaskKey{}, false
}

func viewFind(state *playbook.PlaybookState, key viewTaskKey) *playbook.TaskNode {
	n := 0
	for _, p := range state.Plays {
		if p.Name != key.play {
			continue
		}
		for _, t := range p.Tasks {
			if t.Name == key.task {
				if n == key.nth {
					return t
				}
				n++
			}
		}
	}
	return nil
}

// viewReload re-reads the playbook (after the editor returned, or on R) and
// swaps the tree in place; a file that no longer loads leaves the old tree
// with a note in the bottom bar.
func (s *liveSession) viewReload() {
	if s.view == nil || s.viewReloading {
		return
	}
	s.viewReloading = true
	path := s.targetPlaybook
	go func() {
		loaded, err := staticplay.Load(path)
		if s.quitting.Load() {
			return
		}
		s.app.QueueUpdateDraw(func() {
			s.viewReloading = false
			s.viewApplyReload(loaded, err)
		})
	}()
}

func (s *liveSession) viewApplyReload(loaded staticplay.Playbook, err error) {
	if s.view == nil { // the user has started a run meanwhile
		return
	}
	if err != nil {
		msg, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
		s.viewNotify(" reload failed, showing the old version: " + msg + " ")
		return
	}
	var cursorKey, outputKey viewTaskKey
	haveCursor, haveOutput := false, false
	cursorPlay := ""
	switch id := s.currentID.(type) {
	case *playbook.TaskNode:
		cursorKey, haveCursor = viewKeyOf(s.state, id)
	case *playbook.PlayNode:
		cursorPlay = id.Name
	}
	if s.viewingOutput && s.outputTask != nil {
		outputKey, haveOutput = viewKeyOf(s.state, s.outputTask)
	}

	fresh, view := staticplay.Seed(loaded)
	s.state.Reset()
	s.state.Plays = fresh.Plays
	s.view = view
	// Tasks may have moved or appeared; entries of deleted tasks stay in the
	// index but are never looked up (as for a rerun).
	source.MergeSourceIndex(s.sourceIndex, s.targetPlaybook)
	s.expanded = map[*playbook.TaskNode]bool{}
	s.autoExpanded = map[*playbook.TaskNode]bool{}
	s.lastAppliedSelectedIndex = -1

	s.currentID = nil
	switch {
	case haveCursor:
		if t := viewFind(s.state, cursorKey); t != nil {
			s.currentID = t
		} else {
			for _, p := range s.state.Plays {
				if p.Name == cursorKey.play {
					s.currentID = p
					break
				}
			}
		}
	case cursorPlay != "":
		for _, p := range s.state.Plays {
			if p.Name == cursorPlay {
				s.currentID = p
				break
			}
		}
	}

	notice := " reloaded "
	if s.viewingOutput {
		if t := viewFind(s.state, outputKey); haveOutput && t != nil {
			s.showViewTask(t)
		} else {
			s.closeOutput()
			notice = " reloaded - the task you were looking at is gone "
		}
	} else {
		s.rebuild()
	}
	s.viewNotify(notice)
}

// viewNotify shows text after the bottom bar's hints for a few seconds.
func (s *liveSession) viewNotify(text string) {
	s.viewNotice = text
	s.viewRefreshBars()
	go func() {
		time.Sleep(4 * time.Second)
		if s.quitting.Load() {
			return
		}
		s.app.QueueUpdateDraw(func() {
			if s.viewNotice == text {
				s.viewNotice = ""
				s.viewRefreshBars()
			}
		})
	}()
}

func (s *liveSession) viewRefreshBars() {
	if s.view == nil {
		return
	}
	if s.viewingOutput {
		s.outputBottomBar.SetText(viewOutputBar + s.viewNotice)
	}
	s.bottomBar.SetText(s.currentMainBottomBarText())
}

// endView is called when the re-run dialog starts a real run: from here on
// this is an ordinary run session.
func (s *liveSession) endView() {
	s.view = nil
	s.viewNotice = ""
	s.search.hint = outputBar
	s.outputBottomBar.SetText(outputBar)
}
