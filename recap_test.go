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
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
)

// A couple of tiny constructors, just to avoid repeating the same struct
// literal shape in every test below - not a framework, just less noise.
// Duplicated from internal/playbook's own test helpers of the same name
// (unexported test helpers aren't visible across a package boundary).

func playStartEvent(name string) playbook.RawEvent {
	return playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: name}}
}

func taskStartEvent(name, path string) playbook.RawEvent {
	return playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: name, Path: path}}
}

func hostResultEvent(event, host string, raw json.RawMessage) playbook.RawEvent {
	return playbook.RawEvent{Event: event, Hosts: map[string]json.RawMessage{host: raw}}
}

func TestRecapForHost(t *testing.T) {
	s := &playbook.PlaybookState{}
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
	wantLabels := []string{"ok", "skipped", "changed", "failed"}
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
	failed := got.Categories[3]
	if failed.Label != "failed" || len(failed.Tasks) != 2 {
		t.Fatalf("failed category = %+v, want 2 tasks", failed)
	}
	if failed.Tasks[0].Name != "task three" || failed.Tasks[1].Name != "task four" {
		t.Errorf("failed task order = [%q, %q], want [task three, task four]", failed.Tasks[0].Name, failed.Tasks[1].Name)
	}
}

func TestRecapForHost_HostNeverReported(t *testing.T) {
	s := &playbook.PlaybookState{}
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

// TestRecapForHost_WarningsAreCrossCutting confirms a warning-bearing
// task still lands in its own outcome bucket as usual, and *additionally*
// shows up under "warnings" too - not instead of it. Deliberately using
// the exact same task in both places, since that's the whole point of
// warnings being orthogonal to outcome.
func TestRecapForHost_WarningsAreCrossCutting(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playStartEvent("my play"))

	s.Apply(taskStartEvent("task with a warning", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false,"warnings":["deprecated syntax"]}`)))

	s.Apply(taskStartEvent("task without a warning", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	got := recapForHost(s, "web1")
	if got.OK != 2 || got.Warnings != 1 {
		t.Fatalf("counts = %+v, want OK:2 Warnings:1", got)
	}

	wantLabels := []string{"ok", "warnings"}
	if len(got.Categories) != len(wantLabels) {
		t.Fatalf("got %d categories, want %d: %+v", len(got.Categories), len(wantLabels), got.Categories)
	}
	for i, label := range wantLabels {
		if got.Categories[i].Label != label {
			t.Errorf("Categories[%d].Label = %q, want %q", i, got.Categories[i].Label, label)
		}
	}

	ok := got.Categories[0]
	if len(ok.Tasks) != 2 {
		t.Errorf("ok category has %d tasks, want 2 (both, warning or not)", len(ok.Tasks))
	}
	warnings := got.Categories[1]
	if len(warnings.Tasks) != 1 || warnings.Tasks[0].Name != "task with a warning" {
		t.Errorf("warnings category = %+v, want exactly [task with a warning]", warnings)
	}
}

func TestHasWarnings(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"no warnings field at all", `{"changed":false}`, false},
		{"empty warnings list", `{"warnings":[]}`, false},
		{"one warning", `{"warnings":["be careful"]}`, true},
		{"multiple warnings", `{"warnings":["one","two"]}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasWarnings(json.RawMessage(c.raw)); got != c.want {
				t.Errorf("hasWarnings(%s) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestTaskHasWarnings(t *testing.T) {
	task := &playbook.TaskNode{
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK, "web2": playbook.OutcomeOK},
		Raw: map[string]json.RawMessage{
			"web1": json.RawMessage(`{"changed":false}`),
			"web2": json.RawMessage(`{"changed":false,"warnings":["hi"]}`),
		},
	}
	if !taskHasWarnings(task) {
		t.Error("taskHasWarnings() = false, want true (web2 has one)")
	}

	noWarnings := &playbook.TaskNode{
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
	}
	if taskHasWarnings(noWarnings) {
		t.Error("taskHasWarnings() = true, want false")
	}
}

func TestRecapNarrativeSummary(t *testing.T) {
	buildState := func(hosts map[string]string) *playbook.PlaybookState {
		// hosts maps hostname -> "ok"/"failed"/"unreachable" for one single
		// task shared by all of them - enough to exercise the counting
		// logic without needing a realistic multi-task run.
		s := &playbook.PlaybookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("task one", "/pb.yml:3"))
		for host, outcome := range hosts {
			switch outcome {
			case "ok":
				s.Apply(hostResultEvent("v2_runner_on_ok", host, json.RawMessage(`{"changed":false}`)))
			case "failed":
				s.Apply(hostResultEvent("v2_runner_on_failed", host, json.RawMessage(`{"msg":"boom"}`)))
			case "unreachable":
				s.Apply(hostResultEvent("v2_runner_on_unreachable", host, json.RawMessage(`{"msg":"no route"}`)))
			}
		}
		return s
	}

	cases := []struct {
		name    string
		hosts   map[string]string
		elapsed time.Duration
		want    string
	}{
		{
			name:    "clean run, singular task and host",
			hosts:   map[string]string{"web1": "ok"},
			elapsed: 5 * time.Second,
			want:    "Completed 1 task on 1 reachable host in 00:05 minutes.",
		},
		{
			name:    "clean run, plural tasks and hosts",
			hosts:   map[string]string{"web1": "ok", "web2": "ok"},
			elapsed: 3*time.Minute + 12*time.Second,
			want:    "Completed 1 task on 2 reachable hosts in 03:12 minutes.",
		},
		{
			name:    "one host failed, singular clause",
			hosts:   map[string]string{"web1": "failed"},
			elapsed: 0,
			want:    "Completed 1 task on 1 reachable host in 00:00 minutes. 1 host failed before the end of the playbook.",
		},
		{
			name:    "two hosts failed, plural clause",
			hosts:   map[string]string{"web1": "failed", "web2": "failed"},
			elapsed: 0,
			want:    "Completed 1 task on 2 reachable hosts in 00:00 minutes. 2 hosts failed before the end of the playbook.",
		},
		{
			name:    "one host unreachable, singular was",
			hosts:   map[string]string{"web1": "unreachable"},
			elapsed: 0,
			want:    "Completed 1 task on 0 reachable hosts in 00:00 minutes. 1 host was not reachable.",
		},
		{
			name:    "two hosts unreachable, plural were",
			hosts:   map[string]string{"web1": "unreachable", "web2": "unreachable"},
			elapsed: 0,
			want:    "Completed 1 task on 0 reachable hosts in 00:00 minutes. 2 hosts were not reachable.",
		},
		{
			name:    "both failed and unreachable, each its own sentence",
			hosts:   map[string]string{"web1": "failed", "web2": "unreachable"},
			elapsed: 0,
			want:    "Completed 1 task on 1 reachable host in 00:00 minutes. 1 host failed before the end of the playbook. 1 host was not reachable.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := recapNarrativeSummary(buildState(c.hosts), c.elapsed)
			if got != c.want {
				t.Errorf("recapNarrativeSummary() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRecapTaskDetail_WarningsJoinsWithSemicolons(t *testing.T) {
	task := &playbook.TaskNode{
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"warnings":["one","two"]}`)},
	}
	got := recapTaskDetail(task, "web1", "warnings")
	want := " (one; two)"
	if got != want {
		t.Errorf("recapTaskDetail() = %q, want %q", got, want)
	}
}
