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
	"encoding/json"
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

// TestHostColorTag covers design-docs/OwnCallbackPlugin.md's collapsed-row
// recoloring: IgnoredColor only for the specific combination of Failed +
// Ignored, ColorTag(o) for every other outcome (Ignored is only ever
// meaningfully true alongside Failed - TaskNode.Ignored's own doc comment -
// but this must not special-case some other outcome if it were somehow
// set), and GrayTag whenever the host simply hasn't reported yet.
func TestHostColorTag(t *testing.T) {
	task := newTaskNode()

	if got, want := HostColorTag(task, "web1", playbook.OutcomeOK, false), GrayTag; got != want {
		t.Errorf("not done: HostColorTag() = %q, want %q", got, want)
	}
	if got, want := HostColorTag(task, "web1", playbook.OutcomeOK, true), ColorTag(playbook.OutcomeOK); got != want {
		t.Errorf("OK: HostColorTag() = %q, want %q", got, want)
	}
	if got, want := HostColorTag(task, "web1", playbook.OutcomeFailed, true), ColorTag(playbook.OutcomeFailed); got != want {
		t.Errorf("Failed, not ignored: HostColorTag() = %q, want %q", got, want)
	}

	task.Ignored["web1"] = true
	if got, want := HostColorTag(task, "web1", playbook.OutcomeFailed, true), IgnoredColor; got != want {
		t.Errorf("Failed, ignored: HostColorTag() = %q, want %q (not the plain Failed color)", got, want)
	}
	// Ignored set but the outcome isn't Failed - shouldn't happen for a
	// real event (Ignored is only ever populated for a Failed result), but
	// hostColorTag must not treat it as special regardless.
	if got, want := HostColorTag(task, "web1", playbook.OutcomeSkipped, true), ColorTag(playbook.OutcomeSkipped); got != want {
		t.Errorf("Skipped with Ignored set: HostColorTag() = %q, want %q (Ignored only matters for Failed)", got, want)
	}
}

// TestTaskLabel_RecolorsIgnoredFailure confirms the collapsed row actually
// renders the recolored segment, in both the unselected and selected
// paths (two independent code paths in TaskLabel) - a genuinely failed
// host stays the plain Failed color, an ignored one gets IgnoredColor.
func TestTaskLabel_RecolorsIgnoredFailure(t *testing.T) {
	task := newTaskNode()
	task.Name = "diverging task"
	task.Hosts["host1"] = playbook.OutcomeFailed
	task.Ignored["host1"] = true
	task.Hosts["host2"] = playbook.OutcomeFailed

	layout := ComputeHostColumnLayout(
		&playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{task}}}},
		[]string{"host1", "host2"}, 200, false,
	)

	for _, selected := range []bool{false, true} {
		got := TaskLabel(task, []string{"host1", "host2"}, layout, 200, false, ' ', selected, true)
		if !strings.Contains(got, IgnoredColor) {
			t.Errorf("selected=%v: TaskLabel = %q, want it to contain IgnoredColor (%q) for host1", selected, got, IgnoredColor)
		}
		if !strings.Contains(got, ColorTag(playbook.OutcomeFailed)) {
			t.Errorf("selected=%v: TaskLabel = %q, want it to still contain the plain Failed color for host2", selected, got)
		}
	}
}

// TestHostLabel_RecolorsIgnoredFailure is HostLabel's own counterpart to
// TestTaskLabel_RecolorsIgnoredFailure above - the expanded row must agree
// with the collapsed one about an ignored failure's color, in both the
// unselected and selected paths, rather than showing the plain alarming
// Failed red the collapsed row no longer does.
func TestHostLabel_RecolorsIgnoredFailure(t *testing.T) {
	ignored := newTaskNode()
	ignored.Hosts["host1"] = playbook.OutcomeFailed
	ignored.Ignored["host1"] = true
	ignored.Raw["host1"] = json.RawMessage(`{"msg":"boom"}`)

	genuine := newTaskNode()
	genuine.Hosts["host2"] = playbook.OutcomeFailed
	genuine.Raw["host2"] = json.RawMessage(`{"msg":"boom"}`)

	// Tag-boundary-aware substring checks, not a bare color-name search:
	// ColorTag(OutcomeFailed) is "red", which - unlike almost any other
	// color name here - is also a plain substring of the word "ignored"
	// itself (ignoRED), so a naive Contains(got, "red") is a false
	// positive on the very row it's meant to rule out.
	failedTag := func(selected bool) string {
		if selected {
			return ":" + ColorTag(playbook.OutcomeFailed) + ":"
		}
		return "[" + ColorTag(playbook.OutcomeFailed) + "]"
	}
	ignoredTag := func(selected bool) string {
		if selected {
			return ":" + IgnoredColor + ":"
		}
		return "[" + IgnoredColor + "]"
	}

	for _, selected := range []bool{false, true} {
		got := HostLabel(ignored, "host1", DurationLayout{}, selected)
		if !strings.Contains(got, ignoredTag(selected)) {
			t.Errorf("selected=%v: HostLabel(ignored) = %q, want it to contain %q", selected, got, ignoredTag(selected))
		}
		if strings.Contains(got, failedTag(selected)) {
			t.Errorf("selected=%v: HostLabel(ignored) = %q, want it to NOT contain %q", selected, got, failedTag(selected))
		}

		got = HostLabel(genuine, "host2", DurationLayout{}, selected)
		if !strings.Contains(got, failedTag(selected)) {
			t.Errorf("selected=%v: HostLabel(genuine) = %q, want it to contain %q", selected, got, failedTag(selected))
		}
		if strings.Contains(got, ignoredTag(selected)) {
			t.Errorf("selected=%v: HostLabel(genuine) = %q, want it to NOT contain %q", selected, got, ignoredTag(selected))
		}
	}
}
