package main

import (
	"encoding/json"
	"slices"
	"testing"
)

// A couple of tiny constructors, just to avoid repeating the same struct
// literal shape in every test below - not a framework, just less noise.

func playStartEvent(name string) rawEvent {
	return rawEvent{Event: "v2_playbook_on_play_start", Play: &playRef{Name: name}}
}

func playStartEventWithPath(name, path string) rawEvent {
	return rawEvent{Event: "v2_playbook_on_play_start", Play: &playRef{Name: name, Path: path}}
}

func taskStartEvent(name, path string) rawEvent {
	return rawEvent{Event: "v2_playbook_on_task_start", Task: &taskRef{Name: name, Path: path}}
}

func hostResultEvent(event, host string, raw json.RawMessage) rawEvent {
	return rawEvent{Event: event, Hosts: map[string]json.RawMessage{host: raw}}
}

func TestApply_RecordsBasicOKOutcome(t *testing.T) {
	s := &playbookState{}
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
	if got := task.Hosts["web1"]; got != outcomeOK {
		t.Errorf("host outcome = %v, want outcomeOK", got)
	}
}

func TestApply_DistinguishesChangedFromOK(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":true}`)))

	task := s.Plays[0].Tasks[0]
	if got := task.Hosts["web1"]; got != outcomeChanged {
		t.Errorf("host outcome = %v, want outcomeChanged", got)
	}
}

// Regression test for a real bug: v2_playbook_on_handler_task_start used to
// not be recognized as a task-start event at all, so a handler's own
// results silently landed on whatever task had genuinely started last
// instead of getting their own taskNode. Both events must produce their
// own task, each with only its own host data.
func TestApply_HandlerTaskDoesNotCorruptPriorTask(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("task A", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false,"msg":"result from A"}`)))
	s.Apply(rawEvent{Event: "v2_playbook_on_handler_task_start", Task: &taskRef{Name: "handler B", Path: "/pb.yml:9"}})
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":true,"msg":"result from B"}`)))

	if len(s.Plays[0].Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 (task A and handler B)", len(s.Plays[0].Tasks))
	}
	taskA, handlerB := s.Plays[0].Tasks[0], s.Plays[0].Tasks[1]

	if taskA.Name != "task A" || handlerB.Name != "handler B" {
		t.Fatalf("tasks in wrong order/named wrong: got %q, %q", taskA.Name, handlerB.Name)
	}
	if got := taskA.Hosts["web1"]; got != outcomeOK {
		t.Errorf("task A's own outcome was overwritten by the handler's event: got %v, want outcomeOK", got)
	}
	if got := string(taskA.Raw["web1"]); got != `{"changed":false,"msg":"result from A"}` {
		t.Errorf("task A's raw payload was overwritten by the handler's event: got %s", got)
	}
	if got := handlerB.Hosts["web1"]; got != outcomeChanged {
		t.Errorf("handler B's own outcome missing/wrong: got %v, want outcomeChanged", got)
	}
	if got := string(handlerB.Raw["web1"]); got != `{"changed":true,"msg":"result from B"}` {
		t.Errorf("handler B's raw payload missing/wrong: got %s", got)
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
	var ev rawEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("Unmarshal returned an error for a malformed _timestamp field: %v", err)
	}

	s := &playbookState{}
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
		s := &playbookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("my task", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_unreachable", "web1", json.RawMessage(`{"unreachable":true}`)))

		if !s.HadUnreachable {
			t.Error("HadUnreachable = false, want true")
		}
		if got := s.Plays[0].Tasks[0].Hosts["web1"]; got != outcomeUnreachable {
			t.Errorf("host outcome = %v, want outcomeUnreachable", got)
		}
	})

	t.Run("no unreachable host leaves it false", func(t *testing.T) {
		s := &playbookState{}
		s.Apply(playStartEvent("my play"))
		s.Apply(taskStartEvent("my task", "/pb.yml:3"))
		s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

		if s.HadUnreachable {
			t.Error("HadUnreachable = true, want false")
		}
	})
}

func TestApply_MultipleHostsOnSameTask(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEvent("my play"))
	s.Apply(taskStartEvent("my task", "/pb.yml:3"))
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))
	s.Apply(hostResultEvent("v2_runner_on_failed", "web2", json.RawMessage(`{"failed":true}`)))

	task := s.Plays[0].Tasks[0]
	if want := []string{"web1", "web2"}; !slices.Equal(task.HostOrder, want) {
		t.Errorf("HostOrder = %v, want %v", task.HostOrder, want)
	}
	ok, changed, skipped, failed, unreachable := task.counts()
	if ok != 1 || changed != 0 || skipped != 0 || failed != 1 || unreachable != 0 {
		t.Errorf("counts() = (ok=%d changed=%d skipped=%d failed=%d unreachable=%d), want (1,0,0,1,0)",
			ok, changed, skipped, failed, unreachable)
	}
}

func TestNoteHost_KeepsAllHostsSorted(t *testing.T) {
	s := &playbookState{}
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
	s := &playbookState{}
	s.Apply(hostResultEvent("v2_runner_on_ok", "web1", json.RawMessage(`{"changed":false}`)))

	if len(s.Plays) != 0 {
		t.Errorf("got %d plays, want 0 (no task has started yet)", len(s.Plays))
	}
}

func TestCurrentTask_StaysActiveUntilNextTaskStarts(t *testing.T) {
	s := &playbookState{}
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
	s := &playbookState{}
	var hookFired bool
	s.OnTaskAdded = func(*playNode, *taskNode) { hookFired = true }

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
	// a brand new &playbookState{} - including still firing the
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

func TestApply_SetsPlayPathAndTaskPlayBackPointer(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEventWithPath("my play", "/pb.yml:1"))
	s.Apply(taskStartEvent("task A", "/pb.yml:3"))
	s.Apply(taskStartEvent("task B", "/pb.yml:5"))

	if len(s.Plays) != 1 {
		t.Fatalf("got %d plays, want 1", len(s.Plays))
	}
	play := s.Plays[0]
	if play.Path != "/pb.yml:1" {
		t.Errorf("play.Path = %q, want %q", play.Path, "/pb.yml:1")
	}
	if len(play.Tasks) != 2 {
		t.Fatalf("got %d tasks, want 2", len(play.Tasks))
	}
	for _, task := range play.Tasks {
		if task.Play != play {
			t.Errorf("task %q's Play = %v, want the same *playNode as s.Plays[0] (%v)", task.Name, task.Play, play)
		}
	}
}

// Regression guard: a second play must get its own Path, not the first
// play's leftover pendingPlayPath - each v2_playbook_on_play_start must
// overwrite it, the same way pendingPlayName already does.
func TestApply_SecondPlayGetsItsOwnPath(t *testing.T) {
	s := &playbookState{}
	s.Apply(playStartEventWithPath("play one", "/pb.yml:1"))
	s.Apply(taskStartEvent("task A", "/pb.yml:3"))
	s.Apply(playStartEventWithPath("play two", "/pb.yml:9"))
	s.Apply(taskStartEvent("task B", "/pb.yml:11"))

	if len(s.Plays) != 2 {
		t.Fatalf("got %d plays, want 2", len(s.Plays))
	}
	if s.Plays[1].Path != "/pb.yml:9" {
		t.Errorf("second play's Path = %q, want %q", s.Plays[1].Path, "/pb.yml:9")
	}
	if s.Plays[1].Tasks[0].Play != s.Plays[1] {
		t.Error("task B's Play back-pointer doesn't point at the second play")
	}
}
