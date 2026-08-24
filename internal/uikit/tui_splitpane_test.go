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
)

func TestSplitTreeWidth(t *testing.T) {
	cases := []struct {
		totalWidth int
		want       int
	}{
		{110, 30},  // bare minimum total width (tree + 1-col divider + output floor): tree gets its floor
		{130, 50},  // extra so far all goes to the tree
		{160, 80},  // tree just reached its ceiling
		{200, 80},  // beyond the tree's ceiling: extra no longer grows it
		{1000, 80}, // very wide terminal: tree still capped at 80
	}
	for _, c := range cases {
		if got := SplitTreeWidth(c.totalWidth); got != c.want {
			t.Errorf("splitTreeWidth(%d) = %d, want %d", c.totalWidth, got, c.want)
		}
	}
}

// TestComputeHostColumnLayoutNilAllHosts pins the "no shared column" fallback
// computeHostColumnLayout has always had for an empty host list - the
// two-pane drill-down (design-docs/TwoPanedLayout.md) now deliberately
// invokes this same path (via rebuild's treeAllHosts) to omit hostnames from
// collapsed tree rows, so a regression here would silently break that
// feature, not just the pre-existing "no host has reported yet" case.
func TestComputeHostColumnLayoutNilAllHosts(t *testing.T) {
	state := buildTwoPlayState() // task names: task1, task2, task3
	layout := ComputeHostColumnLayout(state, nil, 200, false)
	if want := len("task1"); layout.TitleColWidth != want {
		t.Errorf("TitleColWidth = %d, want %d (widest task name, unshrunk - avail is generous)", layout.TitleColWidth, want)
	}
	if len(layout.HostDisplay) != 0 {
		t.Errorf("HostDisplay = %v, want empty - no hosts were given to align", layout.HostDisplay)
	}
}

// TestTaskLabelNilAllHosts pins taskLabel's own side of the same fallback:
// with allHosts nil, the row is just the (possibly truncated) title against
// avail directly, with no host list appended at all - regardless of what
// layout.TitleColWidth says, since there's no shared column to honor.
func TestTaskLabelNilAllHosts(t *testing.T) {
	task := &playbook.TaskNode{Name: "a-fairly-long-task-name"}
	layout := HostColumnLayout{TitleColWidth: 3, HostDisplay: []string{"web1", "web2"}} // deliberately
	// mismatched, to prove it's ignored when allHosts is nil.

	got := TaskLabel(task, nil, layout, 200, false, ' ', false, true)
	if !strings.Contains(got, "a-fairly-long-task-name") {
		t.Errorf("taskLabel() = %q, want the full task name present (plenty of avail width, layout ignored)", got)
	}
	if strings.Contains(got, "web1") || strings.Contains(got, "web2") {
		t.Errorf("taskLabel() = %q, want no hostnames at all when allHosts is nil", got)
	}
}
