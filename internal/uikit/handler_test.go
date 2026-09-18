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

package uikit

import (
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"github.com/rivo/tview"
)

func TestTaskDisplayName(t *testing.T) {
	regular := &playbook.TaskNode{Name: "install packages"}
	if got, want := TaskDisplayName(regular), "install packages"; got != want {
		t.Errorf("TaskDisplayName(regular) = %q, want %q", got, want)
	}

	handler := &playbook.TaskNode{Name: "restart nginx", IsHandler: true}
	if got, want := TaskDisplayName(handler), "[Handler] restart nginx"; got != want {
		t.Errorf("TaskDisplayName(handler) = %q, want %q", got, want)
	}
}

// TestComputeHostColumnLayout_AccountsForHandlerPrefix confirms the shared
// title column is sized against TaskDisplayName, not the raw task.Name -
// otherwise a handler's own "[Handler] " tag would get silently truncated
// away by a column sized too narrow to hold it.
func TestComputeHostColumnLayout_AccountsForHandlerPrefix(t *testing.T) {
	handler := newTaskNode()
	handler.Name = "h"
	handler.IsHandler = true
	state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{handler}}}}

	layout := ComputeHostColumnLayout(state, []string{"web1"}, 200, false)
	if want := len("[Handler] h"); layout.TitleColWidth < want {
		t.Errorf("TitleColWidth = %d, want at least %d (room for \"[Handler] h\")", layout.TitleColWidth, want)
	}
}

func TestTaskLabel_ShowsHandlerTag(t *testing.T) {
	handler := newTaskNode()
	handler.Name = "restart nginx"
	handler.IsHandler = true
	layout := ComputeHostColumnLayout(
		&playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{handler}}}},
		[]string{"web1"}, 200, false,
	)

	got := TaskLabel(handler, []string{"web1"}, layout, 200, false, ' ', false, true)
	// tview.Escape neutralizes the literal "[Handler]" text so its own tag
	// parser doesn't misread it as a real tag (turning it into the raw
	// tag-encoded form "[Handler[]") - Unescape reverses that back to what
	// actually renders on screen, which is what this test cares about.
	if display := tview.Unescape(got); !strings.Contains(display, "[Handler] restart nginx") {
		t.Errorf("TaskLabel (unescaped) = %q, want it to contain \"[Handler] restart nginx\"", display)
	}
}

func TestTaskLabel_NoHandlerTagForRegularTask(t *testing.T) {
	regular := newTaskNode()
	regular.Name = "install packages"
	layout := ComputeHostColumnLayout(
		&playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{regular}}}},
		[]string{"web1"}, 200, false,
	)

	got := TaskLabel(regular, []string{"web1"}, layout, 200, false, ' ', false, true)
	if strings.Contains(got, "[Handler]") {
		t.Errorf("TaskLabel = %q, want no \"[Handler]\" tag for a regular task", got)
	}
	if !strings.Contains(got, "install packages") {
		t.Errorf("TaskLabel = %q, want it to still contain the plain task name", got)
	}
}
