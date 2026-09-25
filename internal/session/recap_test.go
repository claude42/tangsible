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
	"encoding/json"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// A couple of tiny constructors, just to avoid repeating the same struct
// literal shape in every test below - not a framework, just less noise.
// Duplicated from internal/playbook's own test helpers of the same name
// (unexported test helpers aren't visible across a package boundary). Each
// names an explicit task id, same reasoning as the internal/playbook
// copy's own doc comment: a real event always carries one
// (RawEvent.Task.ID), and design-docs/StrategyFree.md's findOrCreateTask
// resolves task identity by id alone.

func playStartEvent(name string) playbook.RawEvent {
	return playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: name}}
}

func taskStartEvent(id, name, path string) playbook.RawEvent {
	return playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{ID: id, Name: name, Path: path}}
}

func hostResultEvent(event, taskID, host string, raw json.RawMessage) playbook.RawEvent {
	return playbook.RawEvent{Event: event, Task: &playbook.TaskRef{ID: taskID}, Hosts: map[string]json.RawMessage{host: raw}}
}

func hostResultEventAt(event, taskID, host string, raw json.RawMessage, ts string) playbook.RawEvent {
	return playbook.RawEvent{Event: event, Task: &playbook.TaskRef{ID: taskID}, Hosts: map[string]json.RawMessage{host: raw}, TimestampText: ts}
}

func runnerOnStartEvent(taskID, host, ts string) playbook.RawEvent {
	return playbook.RawEvent{Event: "v2_runner_on_start", Task: &playbook.TaskRef{ID: taskID}, Host: host, TimestampText: ts}
}

func TestRecapForHost(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playStartEvent("my play"))

	s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

	s.Apply(taskStartEvent("t2", "task two", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t2", "web1", json.RawMessage(`{"changed":true}`)))

	s.Apply(taskStartEvent("t3", "task three", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t3", "web1", json.RawMessage(`{"msg":"boom"}`)))

	s.Apply(taskStartEvent("t4", "task four", "/pb.yml:12"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t4", "web1", json.RawMessage(`{"msg":"boom again"}`)))

	s.Apply(taskStartEvent("t5", "task five", "/pb.yml:15"))
	s.Apply(hostResultEvent("v2_runner_on_skipped", "t5", "web1", json.RawMessage(`{"skip_reason":"Conditional result was False"}`)))

	// A second host, never mentioned for "web1"'s own tasks above - must
	// not pollute web1's own tally.
	s.Apply(hostResultEvent("v2_runner_on_ok", "t5", "web2", json.RawMessage(`{"changed":false}`)))

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
	s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

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

	s.Apply(taskStartEvent("t1", "task with a warning", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false,"warnings":["deprecated syntax"]}`)))

	s.Apply(taskStartEvent("t2", "task without a warning", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t2", "web1", json.RawMessage(`{"changed":false}`)))

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

// TestRecapForHost_IgnoredIsCrossCutting mirrors
// TestRecapForHost_WarningsAreCrossCutting above: an ignore_errors: true
// failure lands in *both* "failed" (Ignored is additive, not a
// replacement for OutcomeFailed - see TaskNode.Ignored's own doc comment)
// and the new "ignored" category, while an ordinary failure only ever
// lands in "failed".
func TestRecapForHost_IgnoredIsCrossCutting(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playStartEvent("my play"))

	s.Apply(taskStartEvent("t1", "ignored failure", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web1", json.RawMessage(`{"msg":"boom","ignore_errors":true}`)))

	s.Apply(taskStartEvent("t2", "ordinary failure", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t2", "web1", json.RawMessage(`{"msg":"boom"}`)))

	got := recapForHost(s, "web1")
	if got.Failed != 2 || got.Ignored != 1 {
		t.Fatalf("counts = %+v, want Failed:2 Ignored:1", got)
	}

	wantLabels := []string{"failed", "ignored"}
	if len(got.Categories) != len(wantLabels) {
		t.Fatalf("got %d categories, want %d: %+v", len(got.Categories), len(wantLabels), got.Categories)
	}
	for i, label := range wantLabels {
		if got.Categories[i].Label != label {
			t.Errorf("Categories[%d].Label = %q, want %q", i, got.Categories[i].Label, label)
		}
	}

	failed := got.Categories[0]
	if len(failed.Tasks) != 2 {
		t.Errorf("failed category has %d tasks, want 2 (both, ignored or not)", len(failed.Tasks))
	}
	ignored := got.Categories[1]
	if len(ignored.Tasks) != 1 || ignored.Tasks[0].Name != "ignored failure" {
		t.Errorf("ignored category = %+v, want exactly [ignored failure]", ignored)
	}
}

// TestRecapForHost_NoTimestampsLeavesDurationUnknown covers the common
// (pre-fork run log, or any event stream never carrying v2_runner_on_start)
// case: TestRecapForHost above never sets a single timestamp, so its
// result must have HasDuration false, not a spurious zero total.
func TestRecapForHost_NoTimestampsLeavesDurationUnknown(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

	got := recapForHost(s, "web1")
	if got.HasDuration {
		t.Errorf("HasDuration = true, want false (no timestamps anywhere)")
	}
}

// TestRecapForHost_TotalDurationSumsAcrossTasks covers design-docs/
// OwnCallbackPlugin.md's per-host total-time summary: TotalDuration is the
// sum of every task's own HostDuration for this host, across every
// outcome kind (a skipped task's near-instant duration and a failed
// task's duration both count, same as OK/Changed).
func TestRecapForHost_TotalDurationSumsAcrossTasks(t *testing.T) {
	s := &playbook.PlaybookState{}
	s.Apply(playStartEvent("my play"))

	s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))
	s.Apply(hostResultEventAt("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`), "2026-09-18T08:00:01.000000Z"))

	s.Apply(taskStartEvent("t2", "task two", "/pb.yml:6"))
	s.Apply(runnerOnStartEvent("t2", "web1", "2026-09-18T08:00:01.100000Z"))
	s.Apply(hostResultEventAt("v2_runner_on_skipped", "t2", "web1", json.RawMessage(`{"skip_reason":"x"}`), "2026-09-18T08:00:01.200000Z"))

	got := recapForHost(s, "web1")
	if !got.HasDuration {
		t.Fatal("HasDuration = false, want true")
	}
	if want := 1100 * time.Millisecond; got.TotalDuration != want {
		t.Errorf("TotalDuration = %v, want %v (1.0s + 0.1s)", got.TotalDuration, want)
	}
}

// TestRecapComputeColumnWidths_Duration covers ShowDuration/TotalSeconds:
// false/zero when no host anywhere has a known duration, and sized to the
// widest total actually seen once at least one host does.
func TestRecapComputeColumnWidths_Duration(t *testing.T) {
	t.Run("no duration anywhere", func(t *testing.T) {
		s := &playbook.PlaybookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

		got := recapComputeColumnWidths(s)
		if got.ShowDuration {
			t.Errorf("ShowDuration = true, want false")
		}
	})

	t.Run("widest total sets TotalSeconds", func(t *testing.T) {
		s := &playbook.PlaybookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
		s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))
		s.Apply(hostResultEventAt("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`), "2026-09-18T08:00:10.200000Z"))
		s.Apply(runnerOnStartEvent("t1", "web2", "2026-09-18T08:00:00.000000Z"))
		s.Apply(hostResultEventAt("v2_runner_on_ok", "t1", "web2", json.RawMessage(`{"changed":false}`), "2026-09-18T08:00:00.900000Z"))

		got := recapComputeColumnWidths(s)
		if !got.ShowDuration {
			t.Fatal("ShowDuration = false, want true")
		}
		if want := len("10") + 2; got.TotalSeconds != want {
			t.Errorf("TotalSeconds = %d, want %d (widest total is 10.2s)", got.TotalSeconds, want)
		}
	})
}

// TestRecapHostRowText_NoDurationRendersExactlyAsBefore is the regression
// guard for the "" fallback: a run/replay with no duration data anywhere
// must produce byte-identical output to before DurationLayout existed.
func TestRecapHostRowText_NoDurationRendersExactlyAsBefore(t *testing.T) {
	s := recapHostSummary{OK: 2, Skipped: 1}
	w := recapColumnWidths{Host: 4, OK: 1, Skipped: 1}
	got := recapHostRowText("web1", s, w, false)
	want := "[white::b]web1[-::-] : [green]ok=2[-]  [teal]skipped=1[-]  [gray]changed=0[-]  [gray]unreachable=0[-]  [gray]failed=0[-]  [gray]warnings=0[-]  [gray]ignored=0[-]"
	if got != want {
		t.Errorf("recapHostRowText = %q, want %q", got, want)
	}
}

// TestRecapHostRowText_Duration reproduces the requested visualization
// (design-docs/OwnCallbackPlugin.md) directly against recapHostRowText's
// real output - the aligned "(X.Ys): " prefix, same shape as the live
// tree's own HostAndDurationPrefix.
func TestRecapHostRowText_Duration(t *testing.T) {
	w := recapColumnWidths{Host: len("otherhost"), OK: 1, ShowDuration: true, TotalSeconds: len("10") + 2}

	cases := []struct {
		host string
		s    recapHostSummary
		want string
	}{
		{"somehost", recapHostSummary{OK: 1, HasDuration: true, TotalDuration: 5300 * time.Millisecond},
			"[white::b]somehost  ( 5.3s)[-::-]: [green]ok=1[-]  [gray]skipped=0[-]  [gray]changed=0[-]  [gray]unreachable=0[-]  [gray]failed=0[-]  [gray]warnings=0[-]  [gray]ignored=0[-]"},
		{"otherhost", recapHostSummary{OK: 1, HasDuration: true, TotalDuration: 10200 * time.Millisecond},
			"[white::b]otherhost (10.2s)[-::-]: [green]ok=1[-]  [gray]skipped=0[-]  [gray]changed=0[-]  [gray]unreachable=0[-]  [gray]failed=0[-]  [gray]warnings=0[-]  [gray]ignored=0[-]"},
	}
	for _, c := range cases {
		if got := recapHostRowText(c.host, c.s, w, false); got != c.want {
			t.Errorf("recapHostRowText(%q) = %q, want %q", c.host, got, c.want)
		}
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
			if got := uikit.HasWarnings(json.RawMessage(c.raw)); got != c.want {
				t.Errorf("HasWarnings(%s) = %v, want %v", c.raw, got, c.want)
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
		// Warnings mirrors Raw here the same way record (aggregate.go)
		// populates it from real events - TaskHasWarnings reads this
		// cached field now, not Raw directly (see its own doc comment).
		Warnings: map[string]bool{"web1": false, "web2": true},
	}
	if !uikit.TaskHasWarnings(task) {
		t.Error("TaskHasWarnings() = false, want true (web2 has one)")
	}

	noWarnings := &playbook.TaskNode{
		Hosts:    map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:      map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
		Warnings: map[string]bool{"web1": false},
	}
	if uikit.TaskHasWarnings(noWarnings) {
		t.Error("TaskHasWarnings() = true, want false")
	}
}

func TestRecapNarrativeSummary(t *testing.T) {
	buildState := func(hosts map[string]string) *playbook.PlaybookState {
		// hosts maps hostname -> "ok"/"failed"/"unreachable" for one single
		// task shared by all of them - enough to exercise the counting
		// logic without needing a realistic multi-task run.
		s := &playbook.PlaybookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("t1", "task one", "/pb.yml:3"))
		for host, outcome := range hosts {
			switch outcome {
			case "ok":
				s.Apply(hostResultEvent("v2_runner_on_ok", "t1", host, json.RawMessage(`{"changed":false}`)))
			case "failed":
				s.Apply(hostResultEvent("v2_runner_on_failed", "t1", host, json.RawMessage(`{"msg":"boom"}`)))
			case "unreachable":
				s.Apply(hostResultEvent("v2_runner_on_unreachable", "t1", host, json.RawMessage(`{"msg":"no route"}`)))
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
