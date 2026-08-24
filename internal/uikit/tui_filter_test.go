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

package uikit

import (
	"encoding/json"
	"slices"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
)

func TestTaskHasAnyOutcome(t *testing.T) {
	task := &playbook.TaskNode{Hosts: map[string]playbook.Outcome{
		"web1": playbook.OutcomeFailed,
		"web2": playbook.OutcomeOK,
	}}

	if !TaskHasAnyOutcome(task, playbook.OutcomeFailed) {
		t.Error("expected a match: web1 is Failed")
	}
	if !TaskHasAnyOutcome(task, playbook.OutcomeUnreachable, playbook.OutcomeFailed) {
		t.Error("expected a match against a list of outcomes when one of them matches")
	}
	if TaskHasAnyOutcome(task, playbook.OutcomeSkipped) {
		t.Error("expected no match: no host is Skipped")
	}
	if TaskHasAnyOutcome(&playbook.TaskNode{}, playbook.OutcomeOK) {
		t.Error("expected no match against a task with no hosts at all")
	}
}

func TestTaskVisible(t *testing.T) {
	all := FilterQuery{Mode: FilterAll}
	changed := FilterQuery{Mode: FilterChanged}
	failed := FilterQuery{Mode: FilterFailed}

	cases := []struct {
		name     string
		task     *playbook.TaskNode
		q        FilterQuery
		isActive bool
		want     bool
	}{
		{
			name: "one Failed host and one OK host matches filterFailed - host-level, not all-hosts",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeFailed, "web2": playbook.OutcomeOK}},
			q:    failed,
			want: true,
		},
		{
			name: "Unreachable-only task also matches filterFailed - Unreachable counts as failure",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeUnreachable}},
			q:    failed,
			want: true,
		},
		{
			name: "OK-only task does not match filterFailed",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK}},
			q:    failed,
			want: false,
		},
		{
			name: "OK-only task always matches filterAll",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK}},
			q:    all,
			want: true,
		},
		{
			name: "Changed host matches filterChanged",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeChanged}},
			q:    changed,
			want: true,
		},
		{
			name: "Failed-only task (no Changed host) also matches filterChanged",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeFailed}},
			q:    changed,
			want: true,
		},
		{
			name: "OK-only task does not match filterChanged",
			task: &playbook.TaskNode{Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK}},
			q:    changed,
			want: false,
		},
		{
			name:     "isActive forces visibility under filterFailed even with zero recorded hosts",
			task:     &playbook.TaskNode{Hosts: map[string]playbook.Outcome{}},
			q:        failed,
			isActive: true,
			want:     true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TaskVisible(c.task, c.q, nil, c.isActive)
			if got != c.want {
				t.Errorf("taskVisible() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTaskMatchesSearch(t *testing.T) {
	sourceIndex := map[string]string{"/pb.yml:3": "- name: install package\n  ansible.builtin.package:\n    name: nginx"}

	task := &playbook.TaskNode{
		Name: "install nginx",
		Path: "/pb.yml:3",
		Hosts: map[string]playbook.Outcome{
			"web1": playbook.OutcomeOK,
		},
		Raw: map[string]json.RawMessage{
			"web1": json.RawMessage(`{"stdout":"package already present"}`),
		},
	}

	cases := []struct {
		name string
		term string
		want bool
	}{
		{"empty term matches everything", "", true},
		{"matches task name, case-insensitively", "NGINX", true},
		{"matches task source text", "package", true},
		{"matches host output text", "already present", true},
		{"matches nothing", "does-not-appear-anywhere", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TaskMatchesSearch(task, c.term, sourceIndex)
			if got != c.want {
				t.Errorf("taskMatchesSearch(%q) = %v, want %v", c.term, got, c.want)
			}
		})
	}
}

func TestTaskOutputText(t *testing.T) {
	task := &playbook.TaskNode{Raw: map[string]json.RawMessage{
		"web1": json.RawMessage(`{"stdout":"hello from stdout","msg":"non-zero return code"}`),
		"web2": json.RawMessage(`{"msg":"only msg here"}`),
		"web3": json.RawMessage(`not valid json`),
	}}

	if got := TaskOutputText(task, "web1"); got != "hello from stdout" {
		t.Errorf("web1: got %q, want stdout preferred over msg", got)
	}
	if got := TaskOutputText(task, "web2"); got != "only msg here" {
		t.Errorf("web2: got %q, want msg used as fallback", got)
	}
	if got := TaskOutputText(task, "web3"); got != "" {
		t.Errorf("web3 (undecodable JSON): got %q, want empty string", got)
	}
	if got := TaskOutputText(task, "no-such-host"); got != "" {
		t.Errorf("host not recorded at all: got %q, want empty string", got)
	}
}

func TestTaskAction(t *testing.T) {
	task := &playbook.TaskNode{Raw: map[string]json.RawMessage{
		"web1": json.RawMessage(`{"action":"ansible.builtin.copy","changed":false}`),
		"web2": json.RawMessage(`{"changed":false}`),
		"web3": json.RawMessage(`not valid json`),
	}}

	if got := TaskAction(task, "web1"); got != "ansible.builtin.copy" {
		t.Errorf("web1: got %q, want the action field's own FQCN verbatim", got)
	}
	if got := TaskAction(task, "web2"); got != "" {
		t.Errorf("web2 (no action field): got %q, want empty string", got)
	}
	if got := TaskAction(task, "web3"); got != "" {
		t.Errorf("web3 (undecodable JSON): got %q, want empty string", got)
	}
	if got := TaskAction(task, "no-such-host"); got != "" {
		t.Errorf("host not recorded at all: got %q, want empty string", got)
	}
}

// buildTwoPlayState builds a small tree by hand (no Apply needed):
// play1 has task1 (web1, web2) and task2 (web1 only); play2 has task3
// (web2 only). Shared by the allTasks/tasksForHost/visibleTasks tests
// below since they all want the same shape to check ordering/filtering
// against.
func buildTwoPlayState() *playbook.PlaybookState {
	task1 := &playbook.TaskNode{Name: "task1", Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK, "web2": playbook.OutcomeFailed}}
	task2 := &playbook.TaskNode{Name: "task2", Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK}}
	task3 := &playbook.TaskNode{Name: "task3", Hosts: map[string]playbook.Outcome{"web2": playbook.OutcomeOK}}
	return &playbook.PlaybookState{Plays: []*playbook.PlayNode{
		{Name: "play1", Tasks: []*playbook.TaskNode{task1, task2}},
		{Name: "play2", Tasks: []*playbook.TaskNode{task3}},
	}}
}

func TestAllTasks(t *testing.T) {
	state := buildTwoPlayState()
	got := AllTasks(state)
	var names []string
	for _, task := range got {
		names = append(names, task.Name)
	}
	if want := []string{"task1", "task2", "task3"}; !slices.Equal(names, want) {
		t.Errorf("allTasks() names = %v, want %v (play order, then task order)", names, want)
	}
}

func TestTasksForHost(t *testing.T) {
	state := buildTwoPlayState()
	got := TasksForHost(state, "web2")
	var names []string
	for _, task := range got {
		names = append(names, task.Name)
	}
	// task2 has no web2 entry at all - must be skipped.
	if want := []string{"task1", "task3"}; !slices.Equal(names, want) {
		t.Errorf("tasksForHost(web2) names = %v, want %v", names, want)
	}
}

func TestVisibleTasks(t *testing.T) {
	state := buildTwoPlayState() // task1 has a Failed host, task2/task3 don't
	got := VisibleTasks(state, FilterQuery{Mode: FilterFailed}, nil, nil)
	var names []string
	for _, task := range got {
		names = append(names, task.Name)
	}
	if want := []string{"task1"}; !slices.Equal(names, want) {
		t.Errorf("visibleTasks(filterFailed) names = %v, want %v", names, want)
	}
}

func TestVisibleTasksForHost(t *testing.T) {
	state := buildTwoPlayState()
	// web2 reports on task1 (Failed) and task3 (OK) - only task1 should
	// survive a Failed filter.
	got := VisibleTasksForHost(state, "web2", FilterQuery{Mode: FilterFailed}, nil, nil)
	var names []string
	for _, task := range got {
		names = append(names, task.Name)
	}
	if want := []string{"task1"}; !slices.Equal(names, want) {
		t.Errorf("visibleTasksForHost(web2, filterFailed) names = %v, want %v", names, want)
	}
}

func TestTaskSet(t *testing.T) {
	task1 := &playbook.TaskNode{Name: "task1"}
	task2 := &playbook.TaskNode{Name: "task2"}
	set := TaskSet([]*playbook.TaskNode{task1})

	if !set[task1] {
		t.Error("expected task1 to be a member")
	}
	if set[task2] {
		t.Error("expected task2 (never added) to not be a member")
	}
}
