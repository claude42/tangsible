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
		if row.name != wantName[i] {
			t.Errorf("rows[%d].name = %q, want %q", i, row.name, wantName[i])
		}
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
	if taskRow.outcomes == nil {
		t.Fatal("task row's outcomes map is nil, not an empty map - later code indexes it directly")
	}
	if len(taskRow.outcomes) != 0 {
		t.Errorf("expected no outcomes yet, got %v", taskRow.outcomes)
	}
}
