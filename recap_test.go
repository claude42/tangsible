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

package main

import (
	"encoding/json"
	"testing"
)

func TestRecapForHost(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEvent("my play"))

	s.Apply(taskStartEvent("task one", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	s.Apply(taskStartEvent("task two", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":true}`)))

	s.Apply(taskStartEvent("task three", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom"}`)))

	s.Apply(taskStartEvent("task four", "/pb.yml:12"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom again"}`)))

	s.Apply(taskStartEvent("task five", "/pb.yml:15"))
	s.Apply(hostResultEvent("v2_runner_on_skipped", "web1", json.RawMessage(`{"skip_reason":"Conditional result was False"}`)))

	// A second host, never mentioned for "web1"'s own tasks above - must
	// not pollute web1's own tally.
	s.Apply(hostResultEvent("v2_runner_on_ok", "web2", json.RawMessage(`{"changed":false}`)))

	got := recapForHost(s, "web1")

	if got.OK != 1 || got.Changed != 1 || got.Unreachable != 0 || got.Failed != 2 || got.Skipped != 1 {
		t.Fatalf("counts = %+v, want OK:1 Changed:1 Unreachable:0 Failed:2 Skipped:1", got)
	}

	// Unreachable is 0 for web1 and must not appear in Categories at all -
	// design-docs/Recap.md's own "don't render a category with zero
	// tasks" rule.
	wantLabels := []string{"ok", "changed", "failed", "skipped"}
	if len(got.Categories) != len(wantLabels) {
		t.Fatalf("got %d categories, want %d: %+v", len(got.Categories), len(wantLabels), got.Categories)
	}
	for i, label := range wantLabels {
		if got.Categories[i].Label != label {
			t.Errorf("Categories[%d].Label = %q, want %q", i, got.Categories[i].Label, label)
		}
	}

	// The two failed tasks must both be present, in run order, so the
	// recap's own expanded "failed (2)" list can show each with its own
	// message.
	failed := got.Categories[2]
	if failed.Label != "failed" || len(failed.Tasks) != 2 {
		t.Fatalf("failed category = %+v, want 2 tasks", failed)
	}
	if failed.Tasks[0].Name != "task three" || failed.Tasks[1].Name != "task four" {
		t.Errorf("failed task order = [%q, %q], want [task three, task four]", failed.Tasks[0].Name, failed.Tasks[1].Name)
	}
}

func TestRecapForHost_HostNeverReported(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task one", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	got := recapForHost(s, "never-seen")
	if got.OK != 0 || got.Changed != 0 || got.Unreachable != 0 || got.Failed != 0 || got.Skipped != 0 {
		t.Errorf("counts = %+v, want all zero", got)
	}
	if len(got.Categories) != 0 {
		t.Errorf("Categories = %+v, want empty", got.Categories)
	}
}
