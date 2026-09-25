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

// Tests for the handful of *liveSession methods in livesession_rebuild.go
// that only ever read plain fields (bool/string/tcell.Style/atomic
// pointers) - chromeColorName, showElapsed, outputBottomBarNormalStyle,
// currentMainBottomBarText, activeTasks, progressPosition. None of
// these need NewLiveTUI's real widget construction: a bare &liveSession{}
// literal with just the fields each one reads is enough, the same
// "just call it and compare" testability Phase 1's &playbook.PlaybookState{}
// already established - these were just never split out and tested that
// way before.
package session

import (
	"strings"
	"sync/atomic"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
)

func TestChromeColorName(t *testing.T) {
	cases := []struct {
		name                string
		revisitActive       bool
		liveChromeColorName string
		want                string
	}{
		{"live session shows its own live color", false, "navy", "navy"},
		{"live session in check mode shows olive", false, "olive", "olive"},
		{"revisit always wins, regardless of the live color underneath", true, "olive", "purple"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &liveSession{revisitActive: c.revisitActive, liveChromeColorName: c.liveChromeColorName}
			if got := s.chromeColorName(); got != c.want {
				t.Errorf("chromeColorName() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestShowElapsed(t *testing.T) {
	if s := (&liveSession{revisitActive: false}); !s.showElapsed() {
		t.Error("showElapsed() = false for a live session, want true")
	}
	if s := (&liveSession{revisitActive: true}); s.showElapsed() {
		t.Error("showElapsed() = true for a revisit session, want false (its elapsed time is unknown, not zero)")
	}
}

func TestOutputBottomBarNormalStyle(t *testing.T) {
	live := tcell.StyleDefault.Background(tcell.ColorNavy)
	revisit := tcell.StyleDefault.Background(tcell.ColorPurple)

	s := &liveSession{revisitActive: false, liveChromeStyle: live, chromeStyle: revisit}
	if got := s.outputBottomBarNormalStyle(); got != live {
		t.Errorf("outputBottomBarNormalStyle() for a live session = %v, want liveChromeStyle %v", got, live)
	}

	s.revisitActive = true
	if got := s.outputBottomBarNormalStyle(); got != revisit {
		t.Errorf("outputBottomBarNormalStyle() for a revisit session = %v, want chromeStyle %v", got, revisit)
	}
}

func TestCurrentMainBottomBarText(t *testing.T) {
	noop := func(startAtPlay, tags, skipTags, hosts string) {}

	t.Run("plain live session matches the shared hint text verbatim", func(t *testing.T) {
		s := &liveSession{requestRerun: noop}
		if got := s.currentMainBottomBarText(); got != uikit.MainBottomBarText {
			t.Errorf("currentMainBottomBarText() = %q, want %q", got, uikit.MainBottomBarText)
		}
	})

	t.Run("nil requestRerun (a Phase 2 revisit session) drops the re-run hint", func(t *testing.T) {
		s := &liveSession{requestRerun: nil}
		got := s.currentMainBottomBarText()
		if strings.Contains(got, "r: re-run") {
			t.Errorf("currentMainBottomBarText() = %q, must not advertise r: re-run when requestRerun is nil", got)
		}
		// Nothing else about the hint text should be disturbed.
		if !strings.Contains(got, "d: diff") || !strings.Contains(got, "q: quit") {
			t.Errorf("currentMainBottomBarText() = %q, want the rest of the hint text intact", got)
		}
	})

	t.Run("check mode appends its own note", func(t *testing.T) {
		s := &liveSession{requestRerun: noop, checkMode: true}
		got := s.currentMainBottomBarText()
		if !strings.Contains(got, "[CHECK MODE - dry run]") {
			t.Errorf("currentMainBottomBarText() = %q, want a CHECK MODE note", got)
		}
	})

	t.Run("revisit appends its own escape hint", func(t *testing.T) {
		s := &liveSession{requestRerun: noop, revisitActive: true}
		got := s.currentMainBottomBarText()
		if !strings.Contains(got, "Esc: back to the list") {
			t.Errorf("currentMainBottomBarText() = %q, want a revisit escape hint", got)
		}
	})

	t.Run("check mode and revisit combine, and revisit doesn't need requestRerun", func(t *testing.T) {
		s := &liveSession{requestRerun: nil, checkMode: true, revisitActive: true}
		got := s.currentMainBottomBarText()
		if strings.Contains(got, "r: re-run") {
			t.Errorf("currentMainBottomBarText() = %q, must not advertise r: re-run", got)
		}
		if !strings.Contains(got, "[CHECK MODE - dry run]") || !strings.Contains(got, "Esc: back to the list") {
			t.Errorf("currentMainBottomBarText() = %q, want both the check-mode note and the revisit hint", got)
		}
	})
}

func TestActiveTasks(t *testing.T) {
	t.Run("nil before any task has started", func(t *testing.T) {
		state := &playbook.PlaybookState{}
		var processDone atomic.Bool
		s := &liveSession{state: state, processDone: &processDone}
		if got := s.activeTasks(); len(got) != 0 {
			t.Errorf("activeTasks() = %v, want empty", got)
		}
	})

	t.Run("contains a task with a host dispatched but not yet reported, while still running", func(t *testing.T) {
		state := &playbook.PlaybookState{}
		state.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
		state.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{ID: "t1", Name: "task one"}})
		state.Apply(playbook.RawEvent{Event: "v2_runner_on_start", Task: &playbook.TaskRef{ID: "t1"}, Host: "web1", TimestampText: "2026-09-18T08:00:00.000000Z"})
		var processDone atomic.Bool
		s := &liveSession{state: state, processDone: &processDone}
		got := s.activeTasks()
		if len(got) != 1 {
			t.Fatalf("activeTasks() = %v, want exactly the one in-flight task", got)
		}
		for task := range got {
			if task.Name != "task one" {
				t.Errorf("activeTasks() task = %q, want %q", task.Name, "task one")
			}
		}
	})

	t.Run("empty once the run is frozen, even though the last-started task is still incomplete", func(t *testing.T) {
		state := &playbook.PlaybookState{}
		state.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
		state.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{ID: "t1", Name: "task one"}})
		state.Apply(playbook.RawEvent{Event: "v2_runner_on_start", Task: &playbook.TaskRef{ID: "t1"}, Host: "web1", TimestampText: "2026-09-18T08:00:00.000000Z"})
		var processDone atomic.Bool
		processDone.Store(true)
		s := &liveSession{state: state, processDone: &processDone}
		if got := s.activeTasks(); len(got) != 0 {
			t.Errorf("activeTasks() = %v, want empty once processDone", got)
		}
		if len(state.IncompleteTasks()) == 0 {
			t.Fatal("test setup: state.IncompleteTasks() unexpectedly empty - activeTasks' own frozen check wouldn't be exercised")
		}
	})
}

func TestProgressPosition(t *testing.T) {
	t.Run("zero value progH (nothing stored yet) reports 0/0", func(t *testing.T) {
		var progH atomic.Pointer[runner.ProgressTracker]
		s := &liveSession{progH: &progH}
		position, total := s.progressPosition()
		if position != 0 || total != 0 {
			t.Errorf("progressPosition() = (%d, %d), want (0, 0)", position, total)
		}
	})

	t.Run("reflects a real tracker's current match", func(t *testing.T) {
		tracker := runner.NewProgressTracker([]runner.ProgressEntry{
			{Play: "p", Task: "task one"},
			{Play: "p", Task: "task two"},
			{Play: "p", Task: "task three"},
		})
		tracker.Advance("p", "task one", false)
		tracker.Advance("p", "task two", false)

		var progH atomic.Pointer[runner.ProgressTracker]
		progH.Store(tracker)
		s := &liveSession{progH: &progH}

		position, total := s.progressPosition()
		if position != 2 || total != 3 {
			t.Errorf("progressPosition() = (%d, %d), want (2, 3)", position, total)
		}
	})
}
