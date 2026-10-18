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

package debugverb

import (
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
)

func TestOutcomeFor(t *testing.T) {
	cases := []struct {
		event string
		raw   string
		want  playbook.Outcome
	}{
		{"v2_runner_on_ok", `{"changed": false}`, playbook.OutcomeOK},
		{"v2_runner_on_ok", `{"changed": true}`, playbook.OutcomeChanged},
		{"v2_runner_on_skipped", `{}`, playbook.OutcomeSkipped},
		{"v2_runner_on_failed", `{}`, playbook.OutcomeFailed},
		{"v2_runner_on_unreachable", `{}`, playbook.OutcomeUnreachable},
	}
	for _, c := range cases {
		if got := outcomeFor(c.event, []byte(c.raw)); got != c.want {
			t.Errorf("outcomeFor(%q, %s) = %v, want %v", c.event, c.raw, got, c.want)
		}
	}
}

func TestTaskLabel(t *testing.T) {
	if got, want := taskLabel("deploy app"), "TASK: deploy app"; got != want {
		t.Errorf("taskLabel(%q) = %q, want %q", "deploy app", got, want)
	}
}

func TestBuildRows_InsertsOneHeaderPerPlayChange(t *testing.T) {
	skeleton := []runner.ProgressEntry{
		{Play: "play one", Task: "task a"},
		{Play: "play one", Task: "task b"},
		{Play: "play two", Task: "task c"},
	}
	d := &debugApp{}
	d.buildRows(skeleton)

	// play header, task a, task b, play header, task c
	if len(d.rows) != 5 {
		t.Fatalf("expected 5 rows, got %d: %+v", len(d.rows), d.rows)
	}
	wantHeader := []bool{true, false, false, true, false}
	wantName := []string{"play one", "task a", "task b", "play two", "task c"}
	for i, row := range d.rows {
		if row.isPlayHeader != wantHeader[i] {
			t.Errorf("rows[%d].isPlayHeader = %v, want %v", i, row.isPlayHeader, wantHeader[i])
		}
		var gotName string
		if row.isPlayHeader {
			gotName = row.play.Name
		} else {
			gotName = row.task.Name
		}
		if gotName != wantName[i] {
			t.Errorf("rows[%d] name = %q, want %q", i, gotName, wantName[i])
		}
	}

	// navRows must mark exactly the header rows non-interactive - backs
	// handleRowSkip's own uikit.NextInteractiveRow call.
	if len(d.navRows) != len(d.rows) {
		t.Fatalf("navRows length = %d, want %d", len(d.navRows), len(d.rows))
	}
	for i, interactive := range wantHeader {
		gotInteractive := d.navRows[i].Selected != nil
		if gotInteractive != !interactive {
			t.Errorf("navRows[%d].Selected != nil = %v, want %v", i, gotInteractive, !interactive)
		}
	}

	// pbState.Plays must mirror the same play/task structure, so
	// uikit.ComputeHostColumnLayout measures against the real rows.
	if len(d.pbState.Plays) != 2 {
		t.Fatalf("pbState.Plays length = %d, want 2", len(d.pbState.Plays))
	}
	if len(d.pbState.Plays[0].Tasks) != 2 || len(d.pbState.Plays[1].Tasks) != 1 {
		t.Errorf("pbState.Plays task counts = %d, %d, want 2, 1", len(d.pbState.Plays[0].Tasks), len(d.pbState.Plays[1].Tasks))
	}

	// rowForSkeleton must map each skeleton index to its own task row,
	// never to a header row.
	wantRowForSkeleton := []int{1, 2, 4}
	if len(d.rowForSkeleton) != len(wantRowForSkeleton) {
		t.Fatalf("rowForSkeleton = %v, want length %d", d.rowForSkeleton, len(wantRowForSkeleton))
	}
	for i, want := range wantRowForSkeleton {
		if d.rowForSkeleton[i] != want {
			t.Errorf("rowForSkeleton[%d] = %d, want %d", i, d.rowForSkeleton[i], want)
		}
	}
}

func TestBuildRows_TaskRowsStartWithEmptyOutcomes(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "t"}}
	d := &debugApp{}
	d.buildRows(skeleton)

	taskRow := d.rows[d.rowForSkeleton[0]]
	if taskRow.task.Hosts == nil {
		t.Fatal("task row's Hosts map is nil, not an empty map - later code indexes it directly")
	}
	if len(taskRow.task.Hosts) != 0 {
		t.Errorf("expected no outcomes yet, got %v", taskRow.task.Hosts)
	}
}

func TestRecordHost_SortedDedup(t *testing.T) {
	d := &debugApp{}
	d.recordHost("web2")
	d.recordHost("web1")
	d.recordHost("web2") // duplicate, must not append again

	want := []string{"web1", "web2"}
	if len(d.allHosts) != len(want) {
		t.Fatalf("allHosts = %v, want %v", d.allHosts, want)
	}
	for i, h := range want {
		if d.allHosts[i] != h {
			t.Errorf("allHosts[%d] = %q, want %q", i, d.allHosts[i], h)
		}
	}
}

func TestFlatten_ExpandedTaskShowsOnlyReportedHosts(t *testing.T) {
	skeleton := []runner.ProgressEntry{
		{Play: "p", Task: "task a"},
		{Play: "p", Task: "task b"},
	}
	d := &debugApp{expanded: map[*playbook.TaskNode]bool{}}
	d.buildRows(skeleton)
	d.allHosts = []string{"host1", "host2"}

	taskA := d.rows[d.rowForSkeleton[0]].task
	taskA.Hosts["host1"] = playbook.OutcomeOK // host2 hasn't reported yet
	d.expanded[taskA] = true

	flat, nav := d.flatten()
	// header, task a, task a's host1 sub-row, task b.
	if len(flat) != 4 {
		t.Fatalf("flatten() rows = %+v, want length 4", flat)
	}
	if flat[2].rowIdx != d.rowForSkeleton[0] || flat[2].host != "host1" {
		t.Errorf("flat[2] = %+v, want {rowIdx: %d, host: host1}", flat[2], d.rowForSkeleton[0])
	}
	if len(nav) != len(flat) {
		t.Fatalf("nav length = %d, want %d (parallel to flat)", len(nav), len(flat))
	}
	if nav[0].Selected != nil {
		t.Error("header row must stay non-interactive, like d.navRows' own header entries")
	}
	if nav[2].Selected == nil {
		t.Error("a host sub-row must be interactive (a cursor target)")
	}
}

func TestFlatten_CollapsedTaskHasNoHostRows(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "task a"}}
	d := &debugApp{expanded: map[*playbook.TaskNode]bool{}}
	d.buildRows(skeleton)
	d.allHosts = []string{"host1"}
	d.rows[d.rowForSkeleton[0]].task.Hosts["host1"] = playbook.OutcomeOK
	// d.expanded left empty - task a is collapsed.

	flat, _ := d.flatten()
	if len(flat) != 2 {
		t.Fatalf("flatten() rows = %+v, want length 2 (header, task a - no host row)", flat)
	}
}

func TestCurrentTaskRowIdx_HeaderAndHostRowsExcluded(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "task a"}}
	d := &debugApp{expanded: map[*playbook.TaskNode]bool{}}
	d.buildRows(skeleton)
	d.list = uikit.NewTreeList()
	d.lastFlat = []flatEntry{{rowIdx: 0}, {rowIdx: 1}, {rowIdx: 1, host: "host1"}}
	d.list.AddItem("header", nil)
	d.list.AddItem("task", nil)
	d.list.AddItem("host", nil)

	d.list.SetCurrentItem(0)
	if got := d.currentTaskRowIdx(); got != -1 {
		t.Errorf("header row: currentTaskRowIdx() = %d, want -1", got)
	}
	d.list.SetCurrentItem(1)
	if got := d.currentTaskRowIdx(); got != 1 {
		t.Errorf("task row: currentTaskRowIdx() = %d, want 1", got)
	}
	d.list.SetCurrentItem(2)
	if got := d.currentTaskRowIdx(); got != -1 {
		t.Errorf("host sub-row: currentTaskRowIdx() = %d, want -1", got)
	}
}

func TestFlatIndexForRow(t *testing.T) {
	flat := []flatEntry{{rowIdx: 0}, {rowIdx: 1}, {rowIdx: 1, host: "host1"}, {rowIdx: 2}}
	if got := flatIndexForRow(flat, 1); got != 1 {
		t.Errorf("flatIndexForRow(flat, 1) = %d, want 1 (the task row itself, not its host sub-row)", got)
	}
	if got := flatIndexForRow(flat, 2); got != 3 {
		t.Errorf("flatIndexForRow(flat, 2) = %d, want 3", got)
	}
	if got := flatIndexForRow(flat, 99); got != -1 {
		t.Errorf("flatIndexForRow(flat, 99) = %d, want -1 for a row not present", got)
	}
}
