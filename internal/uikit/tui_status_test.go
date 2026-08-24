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
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
)

// statusRowText and genuineFailure must never disagree about what counts
// as an actual failure (genuineFailure's own doc comment says as much), so
// this table checks both together against the same (code, hadUnreachable)
// input rather than as two separate tests.
func TestStatusRowTextAndGenuineFailure(t *testing.T) {
	cases := []struct {
		name           string
		code           int
		hadUnreachable bool
		wantFailure    bool
		wantContains   string
	}{
		{"success", 0, false, false, "completed successfully"},
		{"benign: exit 4 with a real unreachable host observed", 4, true, false, "unreachable"},
		{
			// ansible-core overloads exit code 4 for both HOST_UNREACHABLE
			// and PARSER_ERROR. Without a real v2_runner_on_unreachable
			// event to back it up (hadUnreachable=false here), exit 4 must
			// be treated as a genuine failure - this is the case most
			// likely to get silently broken if the exit-code handling is
			// ever "simplified."
			"exit 4 with no unreachable host observed is a genuine failure (parser error)",
			4, false, true, "failed",
		},
		{"user-interrupted (exit 99) is not a failure", 99, false, false, "stopped"},
		{"any other nonzero code is a genuine failure", 2, false, true, "failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := GenuineFailure(c.code, c.hadUnreachable, 99); got != c.wantFailure {
				t.Errorf("genuineFailure(%d, %v) = %v, want %v", c.code, c.hadUnreachable, got, c.wantFailure)
			}
			if got := StatusRowText(c.code, c.hadUnreachable, 99); !strings.Contains(got, c.wantContains) {
				t.Errorf("statusRowText(%d, %v) = %q, want it to contain %q", c.code, c.hadUnreachable, got, c.wantContains)
			}
		})
	}

	// The generic-failure message should also include the actual exit code
	// number, not just the word "failed".
	if got := StatusRowText(2, false, 99); !strings.Contains(got, "2") {
		t.Errorf("statusRowText(2, false) = %q, want it to mention the exit code", got)
	}
}

func TestLastFailedTaskAndHost(t *testing.T) {
	t.Run("no failure anywhere returns nil", func(t *testing.T) {
		state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{
			{Name: "play1", Tasks: []*playbook.TaskNode{
				{Name: "task1", HostOrder: []string{"web1"}, Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK}},
			}},
		}}
		task, host := LastFailedTaskAndHost(state)
		if task != nil || host != "" {
			t.Errorf("got (%v, %q), want (nil, \"\")", task, host)
		}
	})

	t.Run("returns the most recent failure, not the first one", func(t *testing.T) {
		taskA := &playbook.TaskNode{Name: "task A", HostOrder: []string{"web1"}, Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeFailed}}
		taskB := &playbook.TaskNode{Name: "task B", HostOrder: []string{"web2"}, Hosts: map[string]playbook.Outcome{"web2": playbook.OutcomeOK}}
		taskC := &playbook.TaskNode{Name: "task C", HostOrder: []string{"web3"}, Hosts: map[string]playbook.Outcome{"web3": playbook.OutcomeFailed}}
		state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{
			{Name: "play1", Tasks: []*playbook.TaskNode{taskA}},
			{Name: "play2", Tasks: []*playbook.TaskNode{taskB, taskC}},
		}}

		task, host := LastFailedTaskAndHost(state)
		if task != taskC || host != "web3" {
			t.Errorf("got (%v, %q), want (task C, \"web3\") - the most recent failure, not task A's earlier one", task, host)
		}
	})

	t.Run("within the winning task, returns the first Failed/Unreachable host in HostOrder", func(t *testing.T) {
		task := &playbook.TaskNode{
			Name:      "task1",
			HostOrder: []string{"web1", "web2"},
			Hosts:     map[string]playbook.Outcome{"web1": playbook.OutcomeOK, "web2": playbook.OutcomeFailed},
		}
		state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Name: "play1", Tasks: []*playbook.TaskNode{task}}}}

		gotTask, gotHost := LastFailedTaskAndHost(state)
		if gotTask != task || gotHost != "web2" {
			t.Errorf("got (%v, %q), want (task1, \"web2\") - web1 is OK, so it must be skipped", gotTask, gotHost)
		}
	})

	t.Run("Unreachable counts as a failure here too", func(t *testing.T) {
		task := &playbook.TaskNode{Name: "task1", HostOrder: []string{"web1"}, Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeUnreachable}}
		state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Name: "play1", Tasks: []*playbook.TaskNode{task}}}}

		gotTask, gotHost := LastFailedTaskAndHost(state)
		if gotTask != task || gotHost != "web1" {
			t.Errorf("got (%v, %q), want (task1, \"web1\")", gotTask, gotHost)
		}
	})
}
