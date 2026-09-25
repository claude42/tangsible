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
	"sync/atomic"
	"testing"

	"code.aw.net/claude/tangsible/internal/runner"
)

// TestOnPlayStarted is the one livesession_events.go hook that touches
// only a plain field (s.progH) rather than calling s.rebuild() or
// s.app.QueueUpdateDraw - onPlayAdded/onTaskAdded/onHostRecorded/applyLive
// all need a real widget/*tview.Application to exercise meaningfully, but
// this one is a direct AdvanceToPlay call, verifiable the same way
// TestProgressPosition already verifies progressPosition.
func TestOnPlayStarted(t *testing.T) {
	tracker := runner.NewProgressTracker([]runner.ProgressEntry{
		{Play: "play one", Task: "task a"},
		{Play: "play one", Task: "task b"},
		{Play: "play two", Task: "task c"},
	})

	var progH atomic.Pointer[runner.ProgressTracker]
	progH.Store(tracker)
	s := &liveSession{progH: &progH}

	// Resyncing straight to "play two" - an entirely-skipped "play one"
	// never produces a task-start event of its own to advance through -
	// must credit everything before it as done.
	s.onPlayStarted("play two")

	position, total := s.progressPosition()
	if position != 2 || total != 3 {
		t.Errorf("progressPosition() after onPlayStarted(\"play two\") = (%d, %d), want (2, 3)", position, total)
	}
}
