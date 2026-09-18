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

package playbook

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// A couple of tiny constructors, just to avoid repeating the same struct
// literal shape in every test below - not a framework, just less noise.

func playStartEvent(name string) RawEvent {
	return RawEvent{Event: "v2_playbook_on_play_start", Play: &PlayRef{Name: name}}
}

func taskStartEvent(name, path string) RawEvent {
	return RawEvent{Event: "v2_playbook_on_task_start", Task: &TaskRef{Name: name, Path: path}}
}

func handlerTaskStartEvent(name, path string) RawEvent {
	return RawEvent{Event: "v2_playbook_on_handler_task_start", Task: &TaskRef{Name: name, Path: path, IsHandler: true}}
}

func hostResultEvent(event, host string, raw json.RawMessage) RawEvent {
	return RawEvent{Event: event, Hosts: map[string]json.RawMessage{host: raw}}
}

func hostResultEventAt(event, host string, raw json.RawMessage, ts string) RawEvent {
	return RawEvent{Event: event, Hosts: map[string]json.RawMessage{host: raw}, TimestampText: ts}
}

func runnerOnStartEvent(host, ts string) RawEvent {
	return RawEvent{Event: "v2_runner_on_start", Host: host, TimestampText: ts}
}

func TestApply_RecordsBasicOKOutcome(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	if len(s.Plays) != 1 {
		t.Fatalf("got %d plays, want 1", len(s.Plays))
	}
	if len(s.Plays[0].Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(s.Plays[0].Tasks))
	}
	task := s.Plays[0].Tasks[0]
	if got := task.Hosts["web1"]; got != OutcomeOK {
		t.Errorf("host outcome = %v, want OutcomeOK", got)
	}
}

// TestApply_RecordsWarnings checks that record (via Apply) populates
// TaskNode.Warnings from each host's own raw result, for every outcome
// kind - not just OK, since a warning is orthogonal to outcome (see
// TaskNode.Warnings' own doc comment). This is what lets callers rendering
// many rows (uikit's TaskLabel/HostLabel, recap.go's recapForHost) read a
// plain map lookup instead of re-decoding raw JSON on every redraw.
func TestApply_RecordsWarnings(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task with a warning", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false,"warnings":["deprecated syntax"]}`)))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web2", json.RawMessage(`{"msg":"boom"}`)))

	task := s.Plays[0].Tasks[0]
	if !task.Warnings["web1"] {
		t.Error(`task.Warnings["web1"] = false, want true`)
	}
	if task.Warnings["web2"] {
		t.Error(`task.Warnings["web2"] = true, want false (no warnings field at all)`)
	}
}

// TestApply_RecordsHasStderr mirrors TestApply_RecordsWarnings above, for
// TaskNode.HasStderr - the same "computed once per host, in record" shape,
// just a different field/JSON key.
func TestApply_RecordsHasStderr(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task with stderr", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom","stderr":"connection refused"}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web2", json.RawMessage(`{"changed":false}`)))

	task := s.Plays[0].Tasks[0]
	if !task.HasStderr["web1"] {
		t.Error(`task.HasStderr["web1"] = false, want true`)
	}
	if task.HasStderr["web2"] {
		t.Error(`task.HasStderr["web2"] = true, want false (no stderr field at all)`)
	}
}

func TestApply_DistinguishesChangedFromOK(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":true}`)))

	task := s.Plays[0].Tasks[0]
	if got := task.Hosts["web1"]; got != OutcomeChanged {
		t.Errorf("host outcome = %v, want OutcomeChanged", got)
	}
}

// Regression test for a real bug: v2_playbook_on_handler_task_start used to
// not be recognized as a task-start event at all, so a handler's own
// results silently landed on whatever task had genuinely started last
// instead of getting their own TaskNode. Both events must produce their
// own task, each with only its own host data.
func TestApply_HandlerTaskDoesNotCorruptPriorTask(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task A", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false,"msg":"result from A"}`)))
	s.Apply(RawEvent{Event: "v2_playbook_on_handler_task_start", Task: &TaskRef{Name: "handler B", Path: "/pb.yml:9"}})
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":true,"msg":"result from B"}`)))

	if len(s.Plays[0].Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (task A and handler B)", len(s.Plays[0].Tasks))
	}
	taskA, handlerB := s.Plays[0].Tasks[0], s.Plays[0].Tasks[1]

	if taskA.Name != "task A" || handlerB.Name != "handler B" {
		t.Fatalf("tasks in wrong order/named wrong: got %q, %q", taskA.Name, handlerB.Name)
	}
	if got := taskA.Hosts["web1"]; got != OutcomeOK {
		t.Errorf("task A's own outcome was overwritten by the handler's event: got %v, want OutcomeOK", got)
	}
	if got := string(taskA.Raw["web1"]); got != `{"changed":false,"msg":"result from A"}` {
		t.Errorf("task A's raw payload was overwritten by the handler's event: got %s", got)
	}
	if got := handlerB.Hosts["web1"]; got != OutcomeChanged {
		t.Errorf("handler B's own outcome missing/wrong: got %v, want OutcomeChanged", got)
	}
	if got := string(handlerB.Raw["web1"]); got != `{"changed":true,"msg":"result from B"}` {
		t.Errorf("handler B's raw payload missing/wrong: got %s", got)
	}
}

// TestApply_RecordsIsHandler covers design-docs/OwnCallbackPlugin.md's
// is_handler field: only a "v2_playbook_on_handler_task_start" event
// (only ever emitted by this app's own bundled plugin fork with
// IsHandler set - RawEvent.Task.IsHandler's own doc comment) produces a
// TaskNode with IsHandler true; a regular task-start event never does,
// even one that happens to be named like a handler.
func TestApply_RecordsIsHandler(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("regular task", "/pb.yml:3"))
	s.Apply(handlerTaskStartEvent("my handler", "/pb.yml:9"))

	if len(s.Plays[0].Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(s.Plays[0].Tasks))
	}
	regular, handler := s.Plays[0].Tasks[0], s.Plays[0].Tasks[1]
	if regular.IsHandler {
		t.Error("regular task: IsHandler = true, want false")
	}
	if !handler.IsHandler {
		t.Error("handler task: IsHandler = false, want true")
	}
}

// Regression test for a real bug: a time.Time-typed field would fail to
// unmarshal on a malformed _timestamp, which made the whole event fail
// json.Unmarshal and get silently dropped - task and hosts included. Since
// TimestampText is a plain string, the same malformed value must still
// unmarshal successfully and Apply must still process the rest of the
// event normally; only Timestamp() itself is allowed to fail, quietly.
func TestApply_MalformedTimestampStillProcessesEvent(t *testing.T) {
	raw := []byte(`{"_event":"v2_playbook_on_task_start","task":{"name":"broken ts","path":"/pb.yml:5"},"_timestamp":"not-a-timestamp"}`)
	var ev RawEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("Unmarshal returned an error for a malformed _timestamp field: %v", err)
	}

	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(ev)

	if len(s.Plays[0].Tasks) != 1 || s.Plays[0].Tasks[0].Name != "broken ts" {
		t.Fatalf("task was not created despite the malformed timestamp")
	}
	if got := ev.Timestamp(); !got.IsZero() {
		t.Errorf("Timestamp() = %v, want the zero value for a malformed _timestamp", got)
	}
}

func TestApply_TracksHadUnreachable(t *testing.T) {
	t.Run("unreachable host sets the flag", func(t *testing.T) {
		s := &PlaybookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("my task", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_unreachable", "web1", json.RawMessage(`{"unreachable":true}`)))

		if !s.HadUnreachable {
			t.Error("HadUnreachable = false, want true")
		}
		if got := s.Plays[0].Tasks[0].Hosts["web1"]; got != OutcomeUnreachable {
			t.Errorf("host outcome = %v, want OutcomeUnreachable", got)
		}
	})

	t.Run("no unreachable host leaves it false", func(t *testing.T) {
		s := &PlaybookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("my task", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

		if s.HadUnreachable {
			t.Error("HadUnreachable = true, want false")
		}
	})
}

func TestApply_MultipleHostsOnSameTask(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web2", json.RawMessage(`{"failed":true}`)))

	task := s.Plays[0].Tasks[0]
	if want := []string{"web1", "web2"}; !slices.Equal(task.HostOrder, want) {
		t.Errorf("HostOrder = %v, want %v", task.HostOrder, want)
	}
	ok, changed, skipped, failed, unreachable := task.Counts()
	if ok != 1 || changed != 0 || skipped != 0 || failed != 1 || unreachable != 0 {
		t.Errorf("counts() = (ok=%d changed=%d skipped=%d failed=%d unreachable=%d), want (1,0,0,1,0)",
			ok, changed, skipped, failed, unreachable)
	}
}

func TestNoteHost_KeepsAllHostsSorted(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))

	// Two separate events, not one event with both hosts in its Hosts map -
	// map iteration order is randomized in Go, so that would make the
	// insertion order (and thus this test) nondeterministic.
	s.Apply(hostResultEvent("v2_runner_on_ok", "web2", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	if want := []string{"web1", "web2"}; !slices.Equal(s.AllHosts, want) {
		t.Fatalf("AllHosts = %v, want %v (sorted despite web2 being recorded first)", s.AllHosts, want)
	}

	// Recording a host that's already known must not duplicate it.
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":true}`)))
	if want := []string{"web1", "web2"}; !slices.Equal(s.AllHosts, want) {
		t.Errorf("AllHosts = %v, want %v (re-recording web1 should not duplicate it)", s.AllHosts, want)
	}
}

func TestRecordHost_NoopBeforeAnyTaskStarted(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	if len(s.Plays) != 0 {
		t.Errorf("got %d plays, want 0 (no task has started yet)", len(s.Plays))
	}
}

func TestCurrentTask_StaysActiveUntilNextTaskStarts(t *testing.T) {
	s := &PlaybookState{}
	if got := s.CurrentTask(); got != nil {
		t.Fatalf("CurrentTask() = %v, want nil before the first task starts", got)
	}

	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task A", "/pb.yml:3"))
	if got := s.CurrentTask(); got == nil || got.Name != "task A" {
		t.Fatalf("CurrentTask() = %v, want task A", got)
	}

	// Once web1 has reported, task A should still be "current" - there's
	// no separate "this task is now done" signal.
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))
	if got := s.CurrentTask(); got == nil || got.Name != "task A" {
		t.Fatalf("CurrentTask() = %v, want task A to still be current after its host reported", got)
	}

	s.Apply(taskStartEvent("task B", "/pb.yml:9"))
	if got := s.CurrentTask(); got == nil || got.Name != "task B" {
		t.Fatalf("CurrentTask() = %v, want task B once it starts", got)
	}
}

func TestReset_ClearsRunDataButNotHooks(t *testing.T) {
	s := &PlaybookState{}
	var hookFired bool
	s.OnTaskAdded = func(*PlayNode, *TaskNode) { hookFired = true }

	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task A", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_unreachable", "web1", json.RawMessage(`{}`)))

	s.Reset()

	if len(s.Plays) != 0 {
		t.Errorf("Plays = %v, want empty after Reset", s.Plays)
	}
	if len(s.AllHosts) != 0 {
		t.Errorf("AllHosts = %v, want empty after Reset", s.AllHosts)
	}
	if s.HadUnreachable {
		t.Error("HadUnreachable = true after Reset, want false")
	}
	if got := s.CurrentTask(); got != nil {
		t.Errorf("CurrentTask() = %v after Reset, want nil", got)
	}

	// A fresh Apply sequence after Reset must behave exactly like it did on
	// a brand new &PlaybookState{} - including still firing the
	// previously-wired hook, which Reset must not have cleared.
	hookFired = false
	s.Apply(playStartEvent("second play"))
	s.Apply(taskStartEvent("task B", "/pb.yml:9"))
	if !hookFired {
		t.Error("OnTaskAdded hook did not fire after Reset - Reset must not clear it")
	}
	if len(s.Plays) != 1 || s.Plays[0].Name != "second play" {
		t.Errorf("Plays after post-Reset Apply = %v, want a single \"second play\"", s.Plays)
	}
}

// TestFailedUnreachableHostsAndEarliestFailingPlay covers design-docs/
// Rerun.md's "Extend rerun dialog": a multi-play run where web1 fails in
// the second play, web2 goes unreachable in the third, and web3 never has
// any trouble - FailedHosts/UnreachableHosts/EarliestFailingPlay must
// agree on exactly who/where, in run order, not aggregate/task order.
func TestFailedUnreachableHostsAndEarliestFailingPlay(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("play one"))
	s.Apply(taskStartEvent("task 1", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web2", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web3", json.RawMessage(`{"changed":false}`)))

	s.Apply(playStartEvent("play two"))
	s.Apply(taskStartEvent("task 2", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom"}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web3", json.RawMessage(`{"changed":false}`)))

	s.Apply(playStartEvent("play three"))
	s.Apply(taskStartEvent("task 3", "/pb.yml:15"))
	s.Apply(hostResultEvent("v2_runner_on_unreachable", "web2", json.RawMessage(`{}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web3", json.RawMessage(`{"changed":false}`)))

	if got, want := s.FailedHosts(), []string{"web1"}; !slices.Equal(got, want) {
		t.Errorf("FailedHosts() = %v, want %v", got, want)
	}
	if got, want := s.UnreachableHosts(), []string{"web2"}; !slices.Equal(got, want) {
		t.Errorf("UnreachableHosts() = %v, want %v", got, want)
	}
	if got, want := s.EarliestFailingPlay(), "play two"; got != want {
		t.Errorf("EarliestFailingPlay() = %q, want %q", got, want)
	}
}

func TestFailedHostsEmptyWhenNoFailures(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("play one"))
	s.Apply(taskStartEvent("task 1", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	if got := s.FailedHosts(); len(got) != 0 {
		t.Errorf("FailedHosts() = %v, want empty", got)
	}
	if got := s.UnreachableHosts(); len(got) != 0 {
		t.Errorf("UnreachableHosts() = %v, want empty", got)
	}
	if got := s.EarliestFailingPlay(); got != "" {
		t.Errorf("EarliestFailingPlay() = %q, want \"\"", got)
	}
}

// TestFailedHosts_IgnoreErrorsStillCountsAsFailed locks in design-docs/
// Rerun.md's explicit "stick with current behavior for ignore_errors:
// true" decision: OutcomeFailed doesn't distinguish an ignored failure
// from a genuine one anywhere else in this package (TaskNode.Counts', own
// doc-comment history), and FailedHosts/EarliestFailingPlay deliberately
// don't special-case it either - a host whose only failure was
// ignore_errors: true is still "failed" for both.
func TestFailedHosts_IgnoreErrorsStillCountsAsFailed(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("play one"))
	s.Apply(taskStartEvent("task with ignore_errors", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom","ignore_errors":true}`)))

	if got, want := s.FailedHosts(), []string{"web1"}; !slices.Equal(got, want) {
		t.Errorf("FailedHosts() = %v, want %v (ignore_errors still counts as failed)", got, want)
	}
	if got, want := s.EarliestFailingPlay(), "play one"; got != want {
		t.Errorf("EarliestFailingPlay() = %q, want %q", got, want)
	}
}

// TestApply_RecordsIgnored covers design-docs/OwnCallbackPlugin.md's
// ignore_errors field, additive alongside the "still counts as failed"
// simplification TestFailedHosts_IgnoreErrorsStillCountsAsFailed above
// locks in: Ignored[host] is true only for a Failed result whose task set
// ignore_errors: true, false for an ordinary failure and for every other
// outcome kind.
func TestApply_RecordsIgnored(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))

	s.Apply(taskStartEvent("ignored failure", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom","ignore_errors":true}`)))

	s.Apply(taskStartEvent("ordinary failure", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom"}`)))

	s.Apply(taskStartEvent("ok task", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	ignoredTask, ordinaryTask, okTask := s.Plays[0].Tasks[0], s.Plays[0].Tasks[1], s.Plays[0].Tasks[2]
	if !ignoredTask.Ignored["web1"] {
		t.Error(`ignored failure: Ignored["web1"] = false, want true`)
	}
	if ordinaryTask.Ignored["web1"] {
		t.Error(`ordinary failure: Ignored["web1"] = true, want false`)
	}
	if okTask.Ignored["web1"] {
		t.Error(`ok task: Ignored["web1"] = true, want false`)
	}
	// Both still record as OutcomeFailed - Ignored is purely additive, per
	// TestFailedHosts_IgnoreErrorsStillCountsAsFailed above.
	if ignoredTask.Hosts["web1"] != OutcomeFailed || ordinaryTask.Hosts["web1"] != OutcomeFailed {
		t.Error("both failures must still record as OutcomeFailed regardless of Ignored")
	}
}

// TestHostsWithOutcome_DedupesAcrossTasks covers a host recorded with the
// same outcome on more than one task within the run - FailedHosts must
// list it once, not once per task.
func TestHostsWithOutcome_DedupesAcrossTasks(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("play one"))
	s.Apply(taskStartEvent("task 1", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom"}`)))
	s.Apply(taskStartEvent("task 2", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web1", json.RawMessage(`{"msg":"boom again"}`)))

	if got, want := s.FailedHosts(), []string{"web1"}; !slices.Equal(got, want) {
		t.Errorf("FailedHosts() = %v, want %v (deduped)", got, want)
	}
}

// TestApply_RecordsPerHostStartTime covers design-docs/OwnCallbackPlugin.md
// step 4: a "v2_runner_on_start" event (only ever emitted by this app's own
// bundled plugin fork - RawEvent.Host's own doc comment) records that
// host's dispatch timestamp on the current task, keyed by host, without
// needing any task-ID correlation - the same "currentTask is already
// right" assumption every other case in Apply already relies on.
func TestApply_RecordsPerHostStartTime(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("web1", "2026-09-18T08:00:00.000000Z"))
	s.Apply(runnerOnStartEvent("web2", "2026-09-18T08:00:01.270000Z"))

	task := s.Plays[0].Tasks[0]
	want1, _ := time.Parse(time.RFC3339, "2026-09-18T08:00:00.000000Z")
	want2, _ := time.Parse(time.RFC3339, "2026-09-18T08:00:01.270000Z")
	if got := task.Started["web1"]; !got.Equal(want1) {
		t.Errorf(`task.Started["web1"] = %v, want %v`, got, want1)
	}
	if got := task.Started["web2"]; !got.Equal(want2) {
		t.Errorf(`task.Started["web2"] = %v, want %v`, got, want2)
	}
}

// TestApply_RunnerOnStartNoopBeforeAnyTaskStarted mirrors
// TestRecordHost_NoopBeforeAnyTaskStarted - a "v2_runner_on_start" arriving
// with no task ever having started (shouldn't happen for a real run, but
// nothing here should assume it can't) must not panic or fabricate a task.
func TestApply_RunnerOnStartNoopBeforeAnyTaskStarted(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(runnerOnStartEvent("web1", "2026-09-18T08:00:00.000000Z"))

	if len(s.Plays) != 0 {
		t.Errorf("got %d plays, want 0 (no task has started yet)", len(s.Plays))
	}
}

// TestApply_RunnerOnStartIgnoredWithoutHost covers a malformed/pre-fork
// "v2_runner_on_start" event with no "host" field at all - must not record
// a spurious Started[""] entry.
func TestApply_RunnerOnStartIgnoredWithoutHost(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("", "2026-09-18T08:00:00.000000Z"))

	task := s.Plays[0].Tasks[0]
	if len(task.Started) != 0 {
		t.Errorf("task.Started = %v, want empty (event carried no host)", task.Started)
	}
}

// TestApply_RecordsPerHostFinishTimeForEveryOutcomeKind covers all four
// v2_runner_on_* outcome events threading ev.Timestamp() into
// TaskNode.Finished, not just v2_runner_on_ok - a plain "it compiles"
// check wouldn't catch e.g. one case accidentally passing a zero
// time.Time instead of ev.Timestamp().
func TestApply_RecordsPerHostFinishTimeForEveryOutcomeKind(t *testing.T) {
	for _, event := range []string{
		"v2_runner_on_ok", "v2_runner_on_skipped", "v2_runner_on_failed", "v2_runner_on_unreachable",
	} {
		t.Run(event, func(t *testing.T) {
			s := &PlaybookState{}
			s.Apply(playStartEvent("my play"))
			s.Apply(taskStartEvent("my task", "/pb.yml:3"))
			s.Apply(hostResultEventAt(event, "web1", json.RawMessage(`{}`), "2026-09-18T08:00:02.500000Z"))

			want, _ := time.Parse(time.RFC3339, "2026-09-18T08:00:02.500000Z")
			task := s.Plays[0].Tasks[0]
			if got := task.Finished["web1"]; !got.Equal(want) {
				t.Errorf("task.Finished[\"web1\"] = %v, want %v", got, want)
			}
		})
	}
}

// TestApply_StartedAndFinishedGiveARealDuration is the actual payoff this
// whole step exists for (design-docs/PerHostTaskTiming.md): once both
// Started and Finished are populated for a host, the difference between
// them is real per-host execution time, disambiguated from queue-wait -
// unlike the finish-only approximation that doc rejected.
func TestApply_StartedAndFinishedGiveARealDuration(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("web1", "2026-09-18T08:00:01.000000Z"))
	s.Apply(hostResultEventAt("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`), "2026-09-18T08:00:01.340000Z"))

	task := s.Plays[0].Tasks[0]
	got := task.Finished["web1"].Sub(task.Started["web1"])
	if want := 340 * time.Millisecond; got != want {
		t.Errorf("Finished - Started = %v, want %v", got, want)
	}
}
