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
	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// onPlayAdded is wired to state.OnPlayAdded (playbook.PlaybookState's own
// growth hook) in NewLiveTUI's construction.
func (s *liveSession) onPlayAdded(*playbook.PlayNode) { s.rebuild() }

// onPlayStarted is wired to state.OnPlayStarted. Fires for every real
// play, including one whose hosts: pattern matches nothing in this run -
// see aggregate.go's OnPlayStarted and runner.ProgressTracker.AdvanceToPlay
// for why this resync exists at all (an entirely-skipped play's tasks
// never fire a single event of their own for onTaskAdded's own Advance
// call, below, to ever match against).
func (s *liveSession) onPlayStarted(name string) { s.progH.Load().AdvanceToPlay(name) }

// onTaskAdded is wired to state.OnTaskAdded. See InheritedExpandState's
// own doc comment for the decision itself - this is what makes 'E'
// (expandAll) "sticky" for a still-running playbook: every task added
// afterward inherits true from whichever task was added most recently,
// not just the ones already on screen when 'E' was pressed.
func (s *liveSession) onTaskAdded(play *playbook.PlayNode, task *playbook.TaskNode) {
	s.expanded[task] = uikit.InheritedExpandState(uikit.AllTasks(s.state), s.expanded, s.startExpanded)
	// A miss here (a handler - see progress.go's own doc comment - or any
	// task the skeleton couldn't predict) is a silent no-op by design:
	// runner.ProgressTracker.Advance leaves its own state untouched rather
	// than treating "not found" as a regression. task.IsHandler
	// additionally keeps a handler's own miss from inflating missStreak at
	// all (Advance's own doc comment) - it's an expected miss, not
	// evidence of skeleton drift.
	s.progH.Load().Advance(play.Name, task.Name, task.IsHandler)
	s.rebuild()
}

// onHostRecorded is wired to state.OnHostRecorded - fires once per host per
// task, right after PlaybookState.recordHost has already set task.Hosts[host]
// and task.Ignored[host] (task.record, called just before this hook), so
// both are safe to read here.
//
// notify_task_failed (design-docs/Notifications.md) lives here rather than
// in applyLive's own per-event switch specifically to get task.Ignored for
// free: the bundled callback plugin (unlike stock ansible.posix.jsonl) does
// emit ignore_errors, and task.Ignored[host] already carries it by the time
// this fires - an ignore_errors: true failure is deliberately not notified,
// matching real ansible-playbook's own recap treatment of it as distinct
// from a genuine failure, even though this app's own Outcome/tree-coloring
// elsewhere still deliberately folds the two together (see aggregate.go's
// own doc comment on Ignored).
func (s *liveSession) onHostRecorded(task *playbook.TaskNode, host string) {
	if task.Hosts[host] == playbook.OutcomeFailed && !task.Ignored[host] && s.notifyTaskFailedKind != config.NotificationOff {
		if s.taskFailedNotifyCount < s.notifyTaskFailedMax {
			s.taskFailedNotifyCount++
			_ = uikit.SendNotification(s.notifyTaskFailedKind, uikit.NotificationTitle, uikit.TaskFailedBody(task.Name, host))
		} else {
			s.suppressedTaskFailures++
		}
	}
	s.rebuild()
}

// applyLive feeds one playbook.RawEvent in on the app's own update queue -
// NewLiveTUI's second return value, called by main.go's scanEvents loop
// for every live jsonl line.
func (s *liveSession) applyLive(ev playbook.RawEvent) {
	s.app.QueueUpdateDraw(func() {
		s.state.Apply(ev)
	})
}
