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
	"slices"
	"strings"
	"testing"
)

func TestRoleFromPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "role-sourced task",
			path: "/home/user/project/roles/myrole/tasks/main.yml:1",
			want: "myrole",
		},
		{
			name: "role-sourced handler",
			path: "/home/user/project/roles/myrole/handlers/main.yml:3",
			want: "myrole",
		},
		{
			name: "role-sourced template, no trailing :line",
			path: "/home/user/project/roles/myrole/templates/foo.conf.j2",
			want: "myrole",
		},
		{
			name: "play-level task, not role-sourced",
			path: "/home/user/project/site.yml:8",
			want: "",
		},
		{
			name: "included task file outside any role",
			path: "/home/user/project/tasks/setup.yml:1",
			want: "",
		},
		{
			name: "empty path",
			path: "",
			want: "",
		},
		{
			name: "role directory in the name but not the expected layout",
			path: "/home/user/project/not-roles/myrole/tasks/main.yml:1",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roleFromPath(c.path); got != c.want {
				t.Errorf("roleFromPath(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}

func TestSkipOutputText(t *testing.T) {
	cases := []struct {
		name    string
		decoded map[string]interface{}
		want    string
	}{
		{
			name:    "reason and a string condition",
			decoded: map[string]interface{}{"skip_reason": "Conditional result was False", "false_condition": "my_var"},
			want:    "Conditional result was False: my_var",
		},
		{
			name: "false_condition not a string (e.g. when: false serializes as JSON false)",
			decoded: map[string]interface{}{
				"skip_reason":     "Conditional result was False",
				"false_condition": false,
			},
			want: "Conditional result was False",
		},
		{
			name:    "no skip_reason at all",
			decoded: map[string]interface{}{},
			want:    "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := skipOutputText(c.decoded); got != c.want {
				t.Errorf("skipOutputText(%v) = %q, want %q", c.decoded, got, c.want)
			}
		})
	}
}

func TestLoopItemLabels(t *testing.T) {
	t.Run("no results key at all - not a looped task", func(t *testing.T) {
		got := loopItemLabels(map[string]interface{}{"changed": false})
		if got != nil {
			t.Errorf("loopItemLabels() = %v, want nil", got)
		}
	})

	t.Run("string item labels, the common case", func(t *testing.T) {
		decoded := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"_ansible_item_label": "foo", "item": "foo"},
				map[string]interface{}{"_ansible_item_label": "bar", "item": "bar"},
			},
		}
		got := loopItemLabels(decoded)
		want := []string{"foo", "bar"}
		if !slices.Equal(got, want) {
			t.Errorf("loopItemLabels() = %v, want %v", got, want)
		}
	})

	t.Run("dict item label falls back to compact JSON", func(t *testing.T) {
		decoded := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{
					"_ansible_item_label": map[string]interface{}{"name": "foo", "val": 1.0},
					"item":                map[string]interface{}{"name": "foo", "val": 1.0},
				},
			},
		}
		got := loopItemLabels(decoded)
		if len(got) != 1 {
			t.Fatalf("loopItemLabels() = %v, want exactly 1 entry", got)
		}
		if got[0] != `{"name":"foo","val":1}` {
			t.Errorf("loopItemLabels()[0] = %q, want compact JSON of the dict item", got[0])
		}
	})

	t.Run("missing _ansible_item_label falls back to item", func(t *testing.T) {
		decoded := map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"item": "onlyitem"},
			},
		}
		got := loopItemLabels(decoded)
		want := []string{"onlyitem"}
		if !slices.Equal(got, want) {
			t.Errorf("loopItemLabels() = %v, want %v", got, want)
		}
	})

	t.Run("empty results is not nil but also not shown", func(t *testing.T) {
		got := loopItemLabels(map[string]interface{}{"results": []interface{}{}})
		if len(got) != 0 {
			t.Errorf("loopItemLabels() = %v, want empty", got)
		}
	})
}

func TestPrimaryOutputFieldDebugCases(t *testing.T) {
	t.Run("debug msg: plain string", func(t *testing.T) {
		decoded := map[string]interface{}{"action": "ansible.builtin.debug", "msg": "hello world"}
		_, text := primaryOutputField(decoded)
		if text != "hello world" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "hello world")
		}
	})

	t.Run("debug msg: a list of strings - not a plain string, must not be silently dropped", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.debug",
			"msg":    []interface{}{"line one", "line two"},
		}
		_, text := primaryOutputField(decoded)
		if text != "line one\nline two" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "line one\nline two")
		}
	})

	t.Run("debug msg: a dict - falls back to pretty JSON, never empty", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.debug",
			"msg":    map[string]interface{}{"port": float64(8080)},
		}
		_, text := primaryOutputField(decoded)
		if !strings.Contains(text, "8080") {
			t.Errorf("primaryOutputField() text = %q, want it to contain 8080", text)
		}
	})

	t.Run("debug var: form - no msg key at all, value under a var-named key", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action":      "ansible.builtin.debug",
			"changed":     false,
			"outer.inner": "hi",
		}
		_, text := primaryOutputField(decoded)
		if text != "hi" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "hi")
		}
	})

	t.Run("debug var: form with a non-string value - pretty JSON", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action":    "ansible.builtin.debug",
			"changed":   false,
			"some_list": []interface{}{float64(1), float64(2), float64(3)},
		}
		_, text := primaryOutputField(decoded)
		if !strings.Contains(text, "1") || !strings.Contains(text, "3") {
			t.Errorf("primaryOutputField() text = %q, want it to contain the list's own values", text)
		}
	})

	t.Run("debug with no msg and no extra key - nothing to show, not a crash", func(t *testing.T) {
		decoded := map[string]interface{}{"action": "ansible.builtin.debug", "changed": false}
		_, text := primaryOutputField(decoded)
		if text != "" {
			t.Errorf("primaryOutputField() text = %q, want empty", text)
		}
	})

	t.Run("debug with two extra keys - ambiguous, deliberately not guessed", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.debug",
			"foo":    "a",
			"bar":    "b",
		}
		_, text := primaryOutputField(decoded)
		if text != "" {
			t.Errorf("primaryOutputField() text = %q, want empty (ambiguous - two candidate keys)", text)
		}
	})

	t.Run("a looped debug task's own results key is excluded, not mistaken for the var value", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.debug",
			"msg":    "All items completed",
			"results": []interface{}{
				map[string]interface{}{"item": "a"},
			},
		}
		_, text := primaryOutputField(decoded)
		if text != "All items completed" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "All items completed")
		}
	})

	t.Run("a non-debug module with an extra field never triggers the var: heuristic", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.stat",
			"stat":   map[string]interface{}{"exists": true},
		}
		_, text := primaryOutputField(decoded)
		if text != "" {
			t.Errorf("primaryOutputField() text = %q, want empty - the var: heuristic is debug-only", text)
		}
	})
}
