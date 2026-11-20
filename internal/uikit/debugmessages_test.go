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
	"reflect"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
)

// The payloads are the shapes a real ansible-playbook run reported (checked
// against the bundled callback plugin's output).
func TestDebugMessagesOf(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  string
		want []string
	}{
		{"msg string", `{"action":"debug","changed":false,"msg":"hello"}`, []string{"hello"}},
		{"fully qualified action", `{"action":"ansible.builtin.debug","msg":"hello"}`, []string{"hello"}},
		{"legacy action", `{"action":"ansible.legacy.debug","msg":"hello"}`, []string{"hello"}},
		{"msg list", `{"action":"debug","msg":["a","b"]}`, []string{"a\nb"}},
		{"msg dict", `{"action":"debug","msg":{"k":1}}`, []string{"{\n  \"k\": 1\n}"}},
		{"var form", `{"action":"debug","_ansible_no_log":false,"_ansible_verbose_always":true,"changed":false,"foo":[1,2]}`, []string{"[\n  1,\n  2\n]"}},
		{"skipped by when", `{"action":"debug","skipped":true,"skip_reason":"Conditional result was False","false_condition":false}`, nil},
		{"skipped by verbosity", `{"action":"debug","skipped":true,"skipped_reason":"Verbosity threshold not met."}`, nil},
		{"failed", `{"action":"debug","failed":true,"msg":"boom"}`, nil},
		{"no_log", `{"action":"debug","_ansible_no_log":true,"censored":"the output has been hidden due to the fact that 'no_log: true' was specified for this result"}`, nil},
		{"empty msg", `{"action":"debug","msg":""}`, nil},
		{"another module's msg", `{"action":"command","msg":"non-zero return code"}`, nil},
		{"no raw", ``, nil},
		{"not json", `nope`, nil},
		{"loop, one item skipped", `{"action":"debug","msg":"All items completed","results":[` +
			`{"_ansible_item_label":"x","ansible_loop_var":"item","item":"x","msg":"item x","failed":false},` +
			`{"_ansible_item_label":"y","item":"y","skipped":true,"skip_reason":"Conditional result was False"},` +
			`{"_ansible_item_label":"z","item":"z","msg":"item z"}]}`, []string{"x: item x", "z: item z"}},
		{"loop, all skipped", `{"action":"debug","msg":"All items skipped","results":[{"item":"y","skipped":true}]}`, nil},
		{"loop without items", `{"action":"debug","msg":"All items completed","results":[]}`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := DebugMessagesOf(json.RawMessage(c.raw)); !reflect.DeepEqual(got, c.want) {
				t.Errorf("DebugMessagesOf = %q, want %q", got, c.want)
			}
		})
	}
}

func debugTask(hosts map[string]string) *playbook.TaskNode {
	task := &playbook.TaskNode{Name: "Show", Hosts: map[string]playbook.Outcome{}, Raw: map[string]json.RawMessage{}}
	for _, h := range []string{"a", "b", "c", "d", "e"} {
		raw, ok := hosts[h]
		if !ok {
			continue
		}
		task.HostOrder = append(task.HostOrder, h)
		task.Hosts[h] = playbook.OutcomeOK
		if raw == "skipped" {
			task.Hosts[h] = playbook.OutcomeSkipped
			raw = `{"action":"debug","skipped":true}`
		}
		task.Raw[h] = json.RawMessage(raw)
	}
	return task
}

func msg(s string) string { return `{"action":"debug","msg":"` + s + `"}` }

func TestCollectDebugMessagesGrouping(t *testing.T) {
	same := debugTask(map[string]string{"a": msg("v1"), "b": msg("v1"), "c": msg("v1")})
	mixed := debugTask(map[string]string{"a": msg("x"), "b": msg("y"), "c": msg("x"), "d": "skipped"})
	command := &playbook.TaskNode{Name: "cmd", Hosts: map[string]playbook.Outcome{"a": playbook.OutcomeChanged}, HostOrder: []string{"a"},
		Raw: map[string]json.RawMessage{"a": json.RawMessage(`{"action":"command","msg":"x"}`)}}
	state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{
		{Name: "one", Tasks: []*playbook.TaskNode{same, command}},
		{Name: "two", Tasks: []*playbook.TaskNode{mixed}},
	}}

	got := CollectDebugMessages(state)
	if len(got) != 2 || got[0].Play != "one" || got[0].Task != same || got[1].Play != "two" || got[1].Task != mixed {
		t.Fatalf("tasks = %+v", got)
	}
	if g := got[0].Groups; len(g) != 1 || !reflect.DeepEqual(g[0].Hosts, []string{"a", "b", "c"}) || got[0].HostCount != 3 {
		t.Errorf("same message on every host: %+v (hosts %d)", g, got[0].HostCount)
	}
	// a and c agree, b differs, d (skipped) has no message but still ran the task
	g := got[1].Groups
	if len(g) != 2 || !reflect.DeepEqual(g[0].Hosts, []string{"a", "c"}) || !reflect.DeepEqual(g[1].Hosts, []string{"b"}) || got[1].HostCount != 4 {
		t.Errorf("mixed: %+v (hosts %d)", g, got[1].HostCount)
	}
}

func TestCollectDebugMessagesIsIndependentOfArrivalOrder(t *testing.T) {
	task := debugTask(map[string]string{"a": msg("x"), "b": msg("y")})
	task.HostOrder = []string{"b", "a"} // b finished first
	got := CollectDebugMessages(&playbook.PlaybookState{Plays: []*playbook.PlayNode{{Name: "p", Tasks: []*playbook.TaskNode{task}}}})
	if g := got[0].Groups; len(g) != 2 || g[0].Hosts[0] != "a" || g[1].Hosts[0] != "b" {
		t.Errorf("groups = %+v", g)
	}
	if !reflect.DeepEqual(task.HostOrder, []string{"b", "a"}) {
		t.Error("the task's own HostOrder must not be reordered")
	}
}

func TestCollectDebugMessagesNothingToShow(t *testing.T) {
	state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Name: "p", Tasks: []*playbook.TaskNode{
		debugTask(map[string]string{"a": "skipped"}),
		debugTask(map[string]string{"a": msg("")}),
	}}}}
	if got := CollectDebugMessages(state); got != nil {
		t.Errorf("got %+v", got)
	}
}

func TestDebugHostsLabel(t *testing.T) {
	g := func(hosts ...string) DebugGroup { return DebugGroup{Hosts: hosts} }
	for _, c := range []struct {
		group DebugGroup
		count int
		want  string
	}{
		{g("web1"), 1, "web1"},
		{g("a", "b", "c", "d", "e"), 5, "all 5 hosts"},
		{g("a", "b"), 2, "all 2 hosts"},
		{g("a", "b"), 5, "a, b"},
		{g("a", "b", "c"), 5, "a, b, c"},
		{g("a", "b", "c", "d", "e"), 8, "a, b, c, +2 hosts"},
	} {
		if got := DebugHostsLabel(c.group, c.count); got != c.want {
			t.Errorf("DebugHostsLabel(%v, %d) = %q, want %q", c.group.Hosts, c.count, got, c.want)
		}
	}
}

func TestDebugMessageLines(t *testing.T) {
	long := strings.Join([]string{"1", "2", "3", "4", "5", "6", "7"}, "\n")
	for _, c := range []struct {
		name string
		in   []string
		want []string
	}{
		{"one line", []string{"hello"}, []string{"hello"}},
		{"trailing newline dropped", []string{"a\nb\n"}, []string{"a", "b"}},
		{"five lines fit", []string{"1\n2\n3\n4\n5"}, []string{"1", "2", "3", "4", "5"}},
		{"more than five lines", []string{long}, []string{"1", "2", "3", "4", "5", "... (+2 more lines)"}},
		{"more than three items", []string{"a", "b", "c", "d", "e"}, []string{"a", "b", "c", "... (+2 more items)"}},
	} {
		if got := DebugMessageLines(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDebugGroupsShown(t *testing.T) {
	for n, want := range map[int][2]int{1: {1, 0}, 3: {3, 0}, 4: {3, 1}, 9: {3, 6}} {
		if shown, more := DebugGroupsShown(n); shown != want[0] || more != want[1] {
			t.Errorf("DebugGroupsShown(%d) = %d, %d, want %v", n, shown, more, want)
		}
	}
}
