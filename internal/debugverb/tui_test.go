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
