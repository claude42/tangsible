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
//
// Every helper that names a task takes an explicit id - RawEvent.Task.ID's
// own doc comment: a real event always carries task.id (confirmed
// directly against stock ansible.posix.jsonl's own _new_task, not just
// this app's bundled fork), and design-docs/StrategyFree.md's
// findOrCreateTask resolves a task's identity by that id alone, not by
// name/path or by "whichever task started most recently" - so a test
// exercising more than one task has to give each its own id, the same way
// a real run always would.

func playStartEvent(name string) RawEvent {
	return RawEvent{Event: "v2_playbook_on_play_start", Play: &PlayRef{Name: name}}
}

func taskStartEvent(id, name, path string) RawEvent {
	return RawEvent{Event: "v2_playbook_on_task_start", Task: &TaskRef{ID: id, Name: name, Path: path}}
}

func handlerTaskStartEvent(id, name, path string) RawEvent {
	return RawEvent{Event: "v2_playbook_on_handler_task_start", Task: &TaskRef{ID: id, Name: name, Path: path, IsHandler: true}}
}

// hostResultEvent builds a terminal per-host event carrying just enough of
// its own task identity (id) to resolve against an already-created
// TaskNode - name/path are left blank since findOrCreateTask's
// existing-node-first lookup never reads them once a node already exists
// under that id (see its own doc comment), which is the case for every
// call site below except the dedicated find-or-create-from-a-terminal-
// event tests further down, which build their own RawEvent literals with
// a full Task instead.
func hostResultEvent(event, taskID, host string, raw json.RawMessage) RawEvent {
	return RawEvent{Event: event, Task: &TaskRef{ID: taskID}, Hosts: map[string]json.RawMessage{host: raw}}
}

func hostResultEventAt(event, taskID, host string, raw json.RawMessage, ts string) RawEvent {
	return RawEvent{Event: event, Task: &TaskRef{ID: taskID}, Hosts: map[string]json.RawMessage{host: raw}, TimestampText: ts}
}

func runnerOnStartEvent(taskID, host, ts string) RawEvent {
	return RawEvent{Event: "v2_runner_on_start", Task: &TaskRef{ID: taskID}, Host: host, TimestampText: ts}
}

func TestApply_RecordsBasicOKOutcome(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

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
	s.Apply(taskStartEvent("t1", "task with a warning", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false,"warnings":["deprecated syntax"]}`)))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web2", json.RawMessage(`{"msg":"boom"}`)))

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
	s.Apply(taskStartEvent("t1", "task with stderr", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web1", json.RawMessage(`{"msg":"boom","stderr":"connection refused"}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web2", json.RawMessage(`{"changed":false}`)))

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
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":true}`)))

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
	s.Apply(taskStartEvent("t1", "task A", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false,"msg":"result from A"}`)))
	s.Apply(handlerTaskStartEvent("t2", "handler B", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t2", "web1", json.RawMessage(`{"changed":true,"msg":"result from B"}`)))

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
	s.Apply(taskStartEvent("t1", "regular task", "/pb.yml:3"))
	s.Apply(handlerTaskStartEvent("t2", "my handler", "/pb.yml:9"))

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
	raw := []byte(`{"_event":"v2_playbook_on_task_start","task":{"id":"t1","name":"broken ts","path":"/pb.yml:5"},"_timestamp":"not-a-timestamp"}`)
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
		s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_unreachable", "t1", "web1", json.RawMessage(`{"unreachable":true}`)))

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
		s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

		if s.HadUnreachable {
			t.Error("HadUnreachable = true, want false")
		}
	})
}

func TestApply_MultipleHostsOnSameTask(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web2", json.RawMessage(`{"failed":true}`)))

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
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))

	// Two separate events, not one event with both hosts in its Hosts map -
	// map iteration order is randomized in Go, so that would make the
	// insertion order (and thus this test) nondeterministic.
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web2", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

	if want := []string{"web1", "web2"}; !slices.Equal(s.AllHosts, want) {
		t.Fatalf("AllHosts = %v, want %v (sorted despite web2 being recorded first)", s.AllHosts, want)
	}

	// Recording a host that's already known must not duplicate it.
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":true}`)))
	if want := []string{"web1", "web2"}; !slices.Equal(s.AllHosts, want) {
		t.Errorf("AllHosts = %v, want %v (re-recording web1 should not duplicate it)", s.AllHosts, want)
	}
}

// TestRecordHost_NoopBeforeAnyTaskStarted covers the one case
// findOrCreateTask can do nothing with at all: an event with no Task
// field whatsoever (not even an id) - a malformed event in practice
// (every real terminal event carries one, RawEvent.Task.ID's own doc
// comment), but Apply must still degrade to a safe no-op rather than
// panic on a nil Task, the same defensive posture this file already takes
// for a missing ev.Play (see the play-start case).
func TestRecordHost_NoopBeforeAnyTaskStarted(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Hosts: map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)}})

	if len(s.Plays) != 0 {
		t.Errorf("got %d plays, want 0 (no task field on the event at all)", len(s.Plays))
	}
}

// TestApply_TerminalEventsFindOrCreateTaskWithoutTaskStart is step 1's own
// regression test (design-docs/StrategyFree.md): under strategy: free,
// v2_playbook_on_task_start never fires at all - only terminal events,
// each carrying its own full task identity. Two tasks interleaved across
// two hosts, with no task-start event for either, must still land each
// host on the correct task - not misattributed to whichever task a single
// shared pointer would have last pointed at (the old, linear-only
// currentTask design this replaces).
func TestApply_TerminalEventsFindOrCreateTaskWithoutTaskStart(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("free play"))

	task1 := &TaskRef{ID: "t1", Name: "task one", Path: "/pb.yml:3"}
	task2 := &TaskRef{ID: "t2", Name: "task two", Path: "/pb.yml:6"}

	// web1 finishes task one while web2 is still on it; web2 then moves on
	// to task two while web1 has already finished task one - genuine
	// interleaving, not sequential per-task batches.
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: task1, Hosts: map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false,"msg":"one/web1"}`)}})
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: task2, Hosts: map[string]json.RawMessage{"web2": json.RawMessage(`{"changed":false,"msg":"two/web2"}`)}})
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: task1, Hosts: map[string]json.RawMessage{"web2": json.RawMessage(`{"changed":false,"msg":"one/web2"}`)}})

	if len(s.Plays) != 1 {
		t.Fatalf("got %d plays, want 1", len(s.Plays))
	}
	if len(s.Plays[0].Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (task one, task two) - no task-start event ever fired for either", len(s.Plays[0].Tasks))
	}
	one, two := s.Plays[0].Tasks[0], s.Plays[0].Tasks[1]
	if one.Name != "task one" || two.Name != "task two" {
		t.Fatalf("tasks in wrong order/named wrong: got %q, %q", one.Name, two.Name)
	}
	if got := string(one.Raw["web1"]); got != `{"changed":false,"msg":"one/web1"}` {
		t.Errorf("task one/web1 raw = %s, want the one/web1 payload", got)
	}
	if got := string(one.Raw["web2"]); got != `{"changed":false,"msg":"one/web2"}` {
		t.Errorf("task one/web2 raw = %s, want the one/web2 payload (misattributed to task two?)", got)
	}
	if got := string(two.Raw["web2"]); got != `{"changed":false,"msg":"two/web2"}` {
		t.Errorf("task two/web2 raw = %s, want the two/web2 payload", got)
	}
	if _, ok := two.Raw["web1"]; ok {
		t.Error("task two recorded a web1 result it was never given")
	}
}

// TestApply_TaskIdempotent_TaskStartThenTerminalSameID covers the linear
// case: a task-start event followed by a terminal event for the same
// task.id must resolve to the very same TaskNode, not create a second one
// - findOrCreateTask's whole point.
func TestApply_TaskIdempotent_TaskStartThenTerminalSameID(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

	if len(s.Plays[0].Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 (task-start and the terminal event share one id)", len(s.Plays[0].Tasks))
	}
}

// TestApply_TaskIdempotent_TwoHostsNoTaskStart covers the pure free-
// strategy case: two different hosts' terminal events sharing the same
// task.id, with no task-start event at all, must still resolve to one
// TaskNode, not two.
func TestApply_TaskIdempotent_TwoHostsNoTaskStart(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("free play"))
	task := &TaskRef{ID: "t1", Name: "my task", Path: "/pb.yml:3"}
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: task, Hosts: map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)}})
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: task, Hosts: map[string]json.RawMessage{"web2": json.RawMessage(`{"changed":false}`)}})

	if len(s.Plays[0].Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 (both hosts share one task.id)", len(s.Plays[0].Tasks))
	}
	if want := []string{"web1", "web2"}; !slices.Equal(s.Plays[0].Tasks[0].HostOrder, want) {
		t.Errorf("HostOrder = %v, want %v", s.Plays[0].Tasks[0].HostOrder, want)
	}
}

// TestFindOrCreateTask_LockstepStartEventDoesNotBlankNameOrPath covers the
// real wrinkle design-docs/StrategyFree.md calls out: under lockstep, a
// v2_runner_on_start event's own Task is minimal ({"id": ...} only, no
// name/path - tangsible_jsonl.py's own lockstep branch). Once a task
// already exists (created by the real task-start event moments earlier),
// that minimal event must resolve to the existing node unchanged, not
// overwrite its already-populated Name/Path with the empty strings this
// event's own Task carries.
func TestFindOrCreateTask_LockstepStartEventDoesNotBlankNameOrPath(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))

	if len(s.Plays[0].Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(s.Plays[0].Tasks))
	}
	task := s.Plays[0].Tasks[0]
	if task.Name != "my task" || task.Path != "/pb.yml:3" {
		t.Errorf("task.Name/Path = %q/%q, want \"my task\"/\"/pb.yml:3\" (blanked by the minimal v2_runner_on_start payload?)", task.Name, task.Path)
	}
}

func TestReset_ClearsRunDataButNotHooks(t *testing.T) {
	s := &PlaybookState{}
	var hookFired bool
	s.OnTaskAdded = func(*PlayNode, *TaskNode) { hookFired = true }

	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "task A", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))
	s.Apply(hostResultEvent("v2_runner_on_unreachable", "t1", "web1", json.RawMessage(`{}`)))
	s.Apply(taskStartEvent("t2", "task B", "/pb.yml:9"))
	s.Apply(runnerOnStartEvent("t2", "web1", "2026-09-18T08:00:01.000000Z"))

	if len(s.IncompleteTasks()) != 1 {
		t.Fatalf("got %d incomplete tasks before Reset, want 1 (task B still in flight)", len(s.IncompleteTasks()))
	}

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
	if got := s.IncompleteTasks(); len(got) != 0 {
		t.Errorf("IncompleteTasks() = %v after Reset, want empty", got)
	}

	// A fresh Apply sequence after Reset must behave exactly like it did on
	// a brand new &PlaybookState{} - including still firing the
	// previously-wired hook, which Reset must not have cleared, and
	// resolving a repeated task id ("t1") as a brand new task rather than
	// somehow still finding the pre-Reset one (tasksByID must have been
	// cleared, not just Plays).
	hookFired = false
	s.Apply(playStartEvent("second play"))
	s.Apply(taskStartEvent("t1", "task B", "/pb.yml:9"))
	if !hookFired {
		t.Error("OnTaskAdded hook did not fire after Reset - Reset must not clear it")
	}
	if len(s.Plays) != 1 || s.Plays[0].Name != "second play" {
		t.Errorf("Plays after post-Reset Apply = %v, want a single \"second play\"", s.Plays)
	}
	if len(s.Plays[0].Tasks) != 1 || s.Plays[0].Tasks[0].Name != "task B" {
		t.Errorf("post-Reset task = %v, want a fresh \"task B\" (reused id \"t1\" from before Reset must not resolve to the old node)", s.Plays[0].Tasks)
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
	s.Apply(taskStartEvent("t1", "task 1", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web2", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web3", json.RawMessage(`{"changed":false}`)))

	s.Apply(playStartEvent("play two"))
	s.Apply(taskStartEvent("t2", "task 2", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t2", "web1", json.RawMessage(`{"msg":"boom"}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t2", "web3", json.RawMessage(`{"changed":false}`)))

	s.Apply(playStartEvent("play three"))
	s.Apply(taskStartEvent("t3", "task 3", "/pb.yml:15"))
	s.Apply(hostResultEvent("v2_runner_on_unreachable", "t3", "web2", json.RawMessage(`{}`)))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t3", "web3", json.RawMessage(`{"changed":false}`)))

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
	s.Apply(taskStartEvent("t1", "task 1", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))

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
	s.Apply(taskStartEvent("t1", "task with ignore_errors", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web1", json.RawMessage(`{"msg":"boom","ignore_errors":true}`)))

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

	s.Apply(taskStartEvent("t1", "ignored failure", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web1", json.RawMessage(`{"msg":"boom","ignore_errors":true}`)))

	s.Apply(taskStartEvent("t2", "ordinary failure", "/pb.yml:6"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t2", "web1", json.RawMessage(`{"msg":"boom"}`)))

	s.Apply(taskStartEvent("t3", "ok task", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "t3", "web1", json.RawMessage(`{"changed":false}`)))

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
	s.Apply(taskStartEvent("t1", "task 1", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t1", "web1", json.RawMessage(`{"msg":"boom"}`)))
	s.Apply(taskStartEvent("t2", "task 2", "/pb.yml:9"))
	s.Apply(hostResultEvent("v2_runner_on_failed", "t2", "web1", json.RawMessage(`{"msg":"boom again"}`)))

	if got, want := s.FailedHosts(), []string{"web1"}; !slices.Equal(got, want) {
		t.Errorf("FailedHosts() = %v, want %v (deduped)", got, want)
	}
}

// TestApply_RecordsPerHostStartTime covers design-docs/OwnCallbackPlugin.md
// step 4: a "v2_runner_on_start" event (only ever emitted by this app's own
// bundled plugin fork - RawEvent.Host's own doc comment) records that
// host's dispatch timestamp on its own task, resolved by task.id exactly
// like every other event now (findOrCreateTask) - design-docs/
// StrategyFree.md.
func TestApply_RecordsPerHostStartTime(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))
	s.Apply(runnerOnStartEvent("t1", "web2", "2026-09-18T08:00:01.270000Z"))

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

// TestApply_RunnerOnStartNoopWithNoTaskField mirrors
// TestRecordHost_NoopBeforeAnyTaskStarted - a "v2_runner_on_start" event
// with no Task field at all (a malformed/pre-findOrCreateTask-era shape;
// a real event always carries at least a task.id) must not panic or
// fabricate a task.
func TestApply_RunnerOnStartNoopWithNoTaskField(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(RawEvent{Event: "v2_runner_on_start", Host: "web1", TimestampText: "2026-09-18T08:00:00.000000Z"})

	if len(s.Plays) != 0 {
		t.Errorf("got %d plays, want 0 (no task field on the event at all)", len(s.Plays))
	}
}

// TestApply_RunnerOnStartIgnoredWithoutHost covers a malformed/pre-fork
// "v2_runner_on_start" event with no "host" field at all - must not record
// a spurious Started[""] entry.
func TestApply_RunnerOnStartIgnoredWithoutHost(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "", "2026-09-18T08:00:00.000000Z"))

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
			s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
			s.Apply(hostResultEventAt(event, "t1", "web1", json.RawMessage(`{}`), "2026-09-18T08:00:02.500000Z"))

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
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:01.000000Z"))
	s.Apply(hostResultEventAt("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`), "2026-09-18T08:00:01.340000Z"))

	task := s.Plays[0].Tasks[0]
	got := task.Finished["web1"].Sub(task.Started["web1"])
	if want := 340 * time.Millisecond; got != want {
		t.Errorf("Finished - Started = %v, want %v", got, want)
	}
}

// TestIncompleteTasks_EmptyBeforeAnyRunAndAfterReset covers the two "no
// data at all" ends of IncompleteTasks' own lifecycle.
func TestIncompleteTasks_EmptyBeforeAnyRunAndAfterReset(t *testing.T) {
	s := &PlaybookState{}
	if got := s.IncompleteTasks(); len(got) != 0 {
		t.Fatalf("IncompleteTasks() = %v before any event, want empty", got)
	}

	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))
	if got := s.IncompleteTasks(); len(got) != 1 {
		t.Fatalf("IncompleteTasks() = %v mid-run, want exactly 1 (web1 dispatched, not yet reported)", got)
	}

	s.Reset()
	if got := s.IncompleteTasks(); len(got) != 0 {
		t.Errorf("IncompleteTasks() = %v after Reset, want empty", got)
	}
}

// TestIncompleteTasks_ClearsOnTerminalOutcome covers the basic linear
// shape: a task becomes incomplete the moment its only host is dispatched
// (v2_runner_on_start), and complete again the moment that host reports a
// terminal outcome - matching CurrentTask()'s own old "active until the
// next task starts" semantics for the one-task-at-a-time case, without
// needing a next task to start at all to notice completion (a real
// improvement CurrentTask() never had: the old pointer stayed "active" on
// a finished task forever, until something else started).
func TestIncompleteTasks_ClearsOnTerminalOutcome(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "task A", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))

	tasks := s.IncompleteTasks()
	if len(tasks) != 1 || tasks[0].Name != "task A" {
		t.Fatalf("IncompleteTasks() = %v, want exactly [task A]", tasks)
	}

	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))
	if got := s.IncompleteTasks(); len(got) != 0 {
		t.Errorf("IncompleteTasks() = %v once web1 has reported, want empty", got)
	}
}

// TestIncompleteTasks_MultipleSimultaneousTasksUnderFree is the actual
// payoff this whole method exists for (design-docs/StrategyFree.md): under
// strategy: free, more than one task can be genuinely in flight at once -
// no task-start events at all, just v2_runner_on_start/terminal events
// naming their own task.id directly. Both tasks must show as incomplete
// while both have an undispatched-or-unreported host, and each must clear
// independently as its own hosts finish - not clear together, and not
// leak into each other.
func TestIncompleteTasks_MultipleSimultaneousTasksUnderFree(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("free play"))
	taskOne := &TaskRef{ID: "t1", Name: "task one", Path: "/pb.yml:3"}
	taskTwo := &TaskRef{ID: "t2", Name: "task two", Path: "/pb.yml:6"}

	// web1 reaches task one while web2 is already past it, onto task two -
	// both genuinely in flight at the same instant.
	s.Apply(RawEvent{Event: "v2_runner_on_start", Task: taskOne, Host: "web1", TimestampText: "2026-09-18T08:00:00.000000Z"})
	s.Apply(RawEvent{Event: "v2_runner_on_start", Task: taskTwo, Host: "web2", TimestampText: "2026-09-18T08:00:00.500000Z"})

	tasks := s.IncompleteTasks()
	if len(tasks) != 2 {
		t.Fatalf("IncompleteTasks() = %v, want exactly 2 (task one and task two, both in flight)", tasks)
	}
	if tasks[0].Name != "task one" || tasks[1].Name != "task two" {
		t.Errorf("IncompleteTasks() order = [%q, %q], want first-seen order [\"task one\", \"task two\"]", tasks[0].Name, tasks[1].Name)
	}

	// web1 finishes task one - task one clears, task two (web2's own, still
	// unreported) must not be affected by it.
	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: taskOne, Hosts: map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)}})
	tasks = s.IncompleteTasks()
	if len(tasks) != 1 || tasks[0].Name != "task two" {
		t.Fatalf("IncompleteTasks() = %v after task one finished, want exactly [task two]", tasks)
	}

	s.Apply(RawEvent{Event: "v2_runner_on_ok", Task: taskTwo, Hosts: map[string]json.RawMessage{"web2": json.RawMessage(`{"changed":false}`)}})
	if got := s.IncompleteTasks(); len(got) != 0 {
		t.Errorf("IncompleteTasks() = %v once both tasks' only hosts have reported, want empty", got)
	}
}

// TestIncompleteTasks_TaskWithMultipleHostsStaysIncompleteUntilAllReport
// covers a task with more than one dispatched host: it must stay
// incomplete as long as *any* of them hasn't reported yet, not clear the
// moment the first one does.
func TestIncompleteTasks_TaskWithMultipleHostsStaysIncompleteUntilAllReport(t *testing.T) {
	s := &PlaybookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("t1", "my task", "/pb.yml:3"))
	s.Apply(runnerOnStartEvent("t1", "web1", "2026-09-18T08:00:00.000000Z"))
	s.Apply(runnerOnStartEvent("t1", "web2", "2026-09-18T08:00:00.100000Z"))

	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web1", json.RawMessage(`{"changed":false}`)))
	if got := s.IncompleteTasks(); len(got) != 1 {
		t.Fatalf("IncompleteTasks() = %v after only web1 reported, want still 1 (web2 still in flight)", got)
	}

	s.Apply(hostResultEvent("v2_runner_on_ok", "t1", "web2", json.RawMessage(`{"changed":false}`)))
	if got := s.IncompleteTasks(); len(got) != 0 {
		t.Errorf("IncompleteTasks() = %v once both hosts reported, want empty", got)
	}
}
