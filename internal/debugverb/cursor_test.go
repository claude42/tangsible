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
	"encoding/json"
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
		if got := OutcomeFor(c.event, []byte(c.raw)); got != c.want {
			t.Errorf("OutcomeFor(%q, %s) = %v, want %v", c.event, c.raw, got, c.want)
		}
	}
}

func TestTaskLabel(t *testing.T) {
	if got, want := TaskLabel("deploy app", false), "TASK: deploy app"; got != want {
		t.Errorf("TaskLabel(%q, false) = %q, want %q", "deploy app", got, want)
	}
	if got, want := TaskLabel("restart service", true), "HANDLER: restart service"; got != want {
		t.Errorf("TaskLabel(%q, true) = %q, want %q", "restart service", got, want)
	}
}

func TestSeedState_InsertsOnePlayPerPlayChange(t *testing.T) {
	skeleton := []runner.ProgressEntry{
		{Play: "play one", Task: "task a"},
		{Play: "play one", Task: "task b"},
		{Play: "play two", Task: "task c"},
	}
	state, nodes, nodePlay := SeedState(skeleton)

	if len(state.Plays) != 2 {
		t.Fatalf("state.Plays length = %d, want 2", len(state.Plays))
	}
	if len(state.Plays[0].Tasks) != 2 || len(state.Plays[1].Tasks) != 1 {
		t.Fatalf("task counts = %d, %d, want 2, 1", len(state.Plays[0].Tasks), len(state.Plays[1].Tasks))
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes length = %d, want 3", len(nodes))
	}
	wantNames := []string{"task a", "task b", "task c"}
	for i, want := range wantNames {
		if nodes[i].Name != want {
			t.Errorf("nodes[%d].Name = %q, want %q", i, nodes[i].Name, want)
		}
	}
	// nodes must be the exact same objects buildRows put into Plays -
	// Cursor.Apply writes into nodes[i] directly and expects the tree's
	// own rendering (reading state.Plays) to see it.
	if nodes[0] != state.Plays[0].Tasks[0] {
		t.Error("nodes[0] is not the same *TaskNode as state.Plays[0].Tasks[0]")
	}
	if nodes[2] != state.Plays[1].Tasks[0] {
		t.Error("nodes[2] is not the same *TaskNode as state.Plays[1].Tasks[0]")
	}
	if len(nodePlay) != 3 {
		t.Fatalf("nodePlay length = %d, want 3", len(nodePlay))
	}
	if nodePlay[0] != state.Plays[0] || nodePlay[1] != state.Plays[0] {
		t.Error("nodePlay[0]/[1] must both be state.Plays[0] (task a and task b are both play one's)")
	}
	if nodePlay[2] != state.Plays[1] {
		t.Error("nodePlay[2] must be state.Plays[1] (task c is play two's)")
	}
}

func TestSeedState_TaskNodesStartWithEmptyOutcomes(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "t"}}
	_, nodes, _ := SeedState(skeleton)

	if nodes[0].Hosts == nil {
		t.Fatal("Hosts is nil, not an empty map - Cursor.Apply indexes it directly")
	}
	if nodes[0].Raw == nil || nodes[0].Ignored == nil {
		t.Fatal("Raw/Ignored is nil - Cursor.Apply writes into them directly from outside the playbook package")
	}
}

func TestCursor_ApplyResolvesEventsAgainstSkeleton(t *testing.T) {
	skeleton := []runner.ProgressEntry{
		{Play: "p", Task: "task a"},
		{Play: "p", Task: "task b"},
	}
	_, nodes, nodePlay := SeedState(skeleton)
	c := NewCursor(nodes, nodePlay, runner.NewProgressTracker(skeleton), nil)

	if host, changed := c.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}}); host != "" || changed {
		t.Errorf("play-start: host=%q changed=%v, want \"\", false", host, changed)
	}
	if c.CurrentTask != nil {
		t.Fatal("CurrentTask set before any task-start event")
	}

	host, changed := c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "task a"}})
	if host != "" || !changed {
		t.Errorf("task-start task a: host=%q changed=%v, want \"\", true", host, changed)
	}
	if c.CurrentTask != nodes[0] {
		t.Fatal("CurrentTask doesn't point at nodes[0] after task a's own start event")
	}

	host, changed = c.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Hosts: map[string]json.RawMessage{"host1": []byte(`{"changed": true}`)}})
	if changed {
		t.Error("an outcome event must never itself report taskChanged")
	}
	if host != "host1" {
		t.Errorf("recordedHost = %q, want host1", host)
	}
	if nodes[0].Hosts["host1"] != playbook.OutcomeChanged {
		t.Errorf("nodes[0].Hosts[host1] = %v, want OutcomeChanged", nodes[0].Hosts["host1"])
	}
	if string(nodes[0].Raw["host1"]) != `{"changed": true}` {
		t.Errorf("nodes[0].Raw[host1] = %s, want the raw payload verbatim", nodes[0].Raw["host1"])
	}

	// Moving to task b must clear nothing on task a and must flip
	// CurrentTask/changed exactly once.
	host, changed = c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "task b"}})
	if host != "" || !changed {
		t.Errorf("task-start task b: host=%q changed=%v, want \"\", true", host, changed)
	}
	if c.CurrentTask != nodes[1] {
		t.Fatal("CurrentTask doesn't point at nodes[1] after task b's own start event")
	}
	if nodes[0].Hosts["host1"] != playbook.OutcomeChanged {
		t.Error("task a's own recorded outcome was disturbed by moving to task b")
	}
}

func TestCursor_ApplyPopulatesHostOrder(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "t"}}
	_, nodes, nodePlay := SeedState(skeleton)
	c := NewCursor(nodes, nodePlay, runner.NewProgressTracker(skeleton), nil)
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "t"}})

	// FlattenRows' own Enter-to-expand loop (uikit/tui_rows.go) iterates
	// HostOrder, never Hosts directly - a task whose HostOrder stays
	// empty shows no host rows at all, armed breakpoint or not, even
	// though Hosts/Raw are both already correctly populated (confirmed
	// live as a real bug: Enter did nothing on a finished task).
	c.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Hosts: map[string]json.RawMessage{"host2": []byte(`{}`)}})
	c.Apply(playbook.RawEvent{Event: "v2_runner_on_changed", Hosts: map[string]json.RawMessage{"host1": []byte(`{"changed": true}`)}})
	// A later event for a host already recorded for this task must not
	// append a duplicate entry.
	c.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Hosts: map[string]json.RawMessage{"host2": []byte(`{}`)}})

	want := []string{"host2", "host1"}
	if len(nodes[0].HostOrder) != len(want) {
		t.Fatalf("HostOrder = %v, want %v", nodes[0].HostOrder, want)
	}
	for i, h := range want {
		if nodes[0].HostOrder[i] != h {
			t.Errorf("HostOrder[%d] = %q, want %q", i, nodes[0].HostOrder[i], h)
		}
	}
}

// TestCursor_ApplyTracksInFlight pins the busy-animation prefix's own
// data source (PlaybookState.IncompleteTasks, which TaskLabel's "active"
// case ultimately reads) actually gets populated for a debug session -
// confirmed live as a real, reported bug without this: debug mode's own
// busy column stayed permanently empty for the whole run, since nothing
// in this package ever touched TaskNode.InFlight at all (aggregate.go's
// own v2_runner_on_start/recordHost handling, which Cursor never goes
// through - SeedState's own doc comment).
func TestCursor_ApplyTracksInFlight(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "t"}}
	_, nodes, nodePlay := SeedState(skeleton)
	c := NewCursor(nodes, nodePlay, runner.NewProgressTracker(skeleton), nil)
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "t"}})

	c.Apply(playbook.RawEvent{Event: "v2_runner_on_start", Host: "host1", TimestampText: "2026-10-18T12:00:00.000000Z"})
	if !nodes[0].InFlight["host1"] {
		t.Fatal("InFlight[host1] not set after v2_runner_on_start")
	}
	if nodes[0].Started["host1"].IsZero() {
		t.Error("Started[host1] not set after v2_runner_on_start")
	}

	c.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Hosts: map[string]json.RawMessage{"host1": []byte(`{}`)}})
	if nodes[0].InFlight["host1"] {
		t.Error("InFlight[host1] must be cleared once the host's own terminal outcome arrives")
	}
}

// TestCursor_ApplyInsertsUnmatchedTasksLiveInPlace: a task-start event for
// something --list-tasks never predicted - a task spliced in by a dynamic
// include_tasks/include_role (design-docs/DebugMode.md's "Dynamic
// includes") - must grow the tree with a brand-new row, inserted right
// after the task that was current before it, rather than silently
// re-attributing every such event to that stale task (confirmed live as a
// real, reported bug before this existed: every included task's own
// result landing on the include point's own row, overwriting the previous
// one, looking like the run was stuck looping through it).
func TestCursor_ApplyInsertsUnmatchedTasksLiveInPlace(t *testing.T) {
	skeleton := []runner.ProgressEntry{
		{Play: "p", Task: "before include"},
		{Play: "p", Task: "the include point"},
		{Play: "p", Task: "after include"},
	}
	state, nodes, nodePlay := SeedState(skeleton)
	c := NewCursor(nodes, nodePlay, runner.NewProgressTracker(skeleton), nil)

	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "before include"}})
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "the include point"}})

	// Two tasks inside the included file - neither is in the skeleton at
	// all, so both must be newly created rows, not aliases of "the
	// include point" itself.
	host, changed := c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "included task one"}})
	if host != "" || !changed {
		t.Fatalf("included task one: host=%q changed=%v, want \"\", true", host, changed)
	}
	includedOne := c.CurrentTask
	if includedOne == nodes[1] {
		t.Fatal("included task one must not alias \"the include point\"'s own node")
	}
	if includedOne.Name != "included task one" {
		t.Errorf("includedOne.Name = %q, want %q", includedOne.Name, "included task one")
	}

	host, changed = c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "included task two"}})
	if host != "" || !changed {
		t.Fatalf("included task two: host=%q changed=%v, want \"\", true", host, changed)
	}
	includedTwo := c.CurrentTask
	if includedTwo == includedOne {
		t.Fatal("included task two must be its own, distinct node from included task one")
	}

	// Record an outcome for included task two - must land on its own
	// node, not corrupt "the include point"'s or included task one's.
	c.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Hosts: map[string]json.RawMessage{"host1": []byte(`{"changed": false}`)}})
	if _, recorded := includedTwo.Hosts["host1"]; !recorded {
		t.Error("included task two's own outcome was not recorded on its own node")
	}
	if len(nodes[1].Hosts) != 0 {
		t.Error("\"the include point\"'s own node must stay untouched by its included tasks' outcomes")
	}

	// Resuming the real skeleton afterward must still correctly resolve
	// to "after include", not get stuck on the last included task.
	host, changed = c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "after include"}})
	if host != "" || !changed {
		t.Fatalf("after include: host=%q changed=%v, want \"\", true", host, changed)
	}
	if c.CurrentTask != nodes[2] {
		t.Fatal("CurrentTask must resync to nodes[2] (\"after include\") once the skeleton resumes matching")
	}

	// Final tree order, within play "p": before include, the include
	// point, included task one, included task two, after include - the
	// two dynamically-inserted rows read in their real chronological
	// place, not appended after everything else.
	play := state.Plays[0]
	wantOrder := []string{"before include", "the include point", "included task one", "included task two", "after include"}
	if len(play.Tasks) != len(wantOrder) {
		t.Fatalf("play.Tasks length = %d, want %d", len(play.Tasks), len(wantOrder))
	}
	for i, want := range wantOrder {
		if play.Tasks[i].Name != want {
			t.Errorf("play.Tasks[%d].Name = %q, want %q", i, play.Tasks[i].Name, want)
		}
	}
}

// TestCursor_ApplyInsertsHandlerTasksLive pins that a handler's task-start
// (never listed by --list-tasks either, same miss mechanism as a dynamic
// include) gets the same live-insert treatment, not just include_tasks
// content specifically.
func TestCursor_ApplyInsertsHandlerTasksLive(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "notify something"}}
	_, nodes, nodePlay := SeedState(skeleton)
	c := NewCursor(nodes, nodePlay, runner.NewProgressTracker(skeleton), nil)

	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "notify something"}})

	_, changed := c.Apply(playbook.RawEvent{Event: "v2_playbook_on_handler_task_start", Task: &playbook.TaskRef{Name: "restart service", IsHandler: true}})
	if !changed {
		t.Fatal("handler task-start must change CurrentTask to its own new node")
	}
	if !c.CurrentTask.IsHandler {
		t.Error("the dynamically-inserted handler node must itself have IsHandler set")
	}
	if c.CurrentTask == nodes[0] {
		t.Fatal("the handler must not alias the preceding task's own node")
	}
}

func TestCursor_ApplyRecordsIgnoredFailures(t *testing.T) {
	skeleton := []runner.ProgressEntry{{Play: "p", Task: "t"}}
	_, nodes, nodePlay := SeedState(skeleton)
	c := NewCursor(nodes, nodePlay, runner.NewProgressTracker(skeleton), nil)
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "p"}})
	c.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{Name: "t"}})

	c.Apply(playbook.RawEvent{Event: "v2_runner_on_failed", Hosts: map[string]json.RawMessage{"host1": []byte(`{"ignore_errors": true}`)}})
	if !nodes[0].Ignored["host1"] {
		t.Error("Ignored[host1] not set for a v2_runner_on_failed result carrying ignore_errors: true")
	}
}
