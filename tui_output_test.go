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

func TestResolvedMatchesSource(t *testing.T) {
	cases := []struct {
		name     string
		resolved resolvedRender
		source   string
		want     bool
	}{
		{
			name:     "pending - never matches, regardless of Text",
			resolved: resolvedRender{Pending: true, Text: "- name: hi\n"},
			source:   "- name: hi\n",
			want:     false,
		},
		{
			name:     "error - never matches, even if Text happens to be set too",
			resolved: resolvedRender{Err: "boom"},
			source:   "- name: hi\n",
			want:     false,
		},
		{
			name:     "byte-for-byte identical",
			resolved: resolvedRender{Text: "- name: hi\n  debug:\n    msg: hi\n"},
			source:   "- name: hi\n  debug:\n    msg: hi\n",
			want:     true,
		},
		{
			name:     "identical modulo one trailing newline - ansible.builtin.template's own write isn't guaranteed to agree with source.go on this",
			resolved: resolvedRender{Text: "- name: hi\n  debug:\n    msg: hi"},
			source:   "- name: hi\n  debug:\n    msg: hi\n",
			want:     true,
		},
		{
			name:     "genuinely different - a variable actually resolved",
			resolved: resolvedRender{Text: "- name: hi\n  debug:\n    msg: hello world\n"},
			source:   "- name: hi\n  debug:\n    msg: '{{ greeting }}'\n",
			want:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolvedMatchesSource(c.resolved, c.source); got != c.want {
				t.Errorf("resolvedMatchesSource(%+v, %q) = %v, want %v", c.resolved, c.source, got, c.want)
			}
		})
	}
}

func TestResolvedTabHidden(t *testing.T) {
	t.Run("no source to compare against - never hidden, even if Text happens to be empty too", func(t *testing.T) {
		if resolvedTabHidden(resolvedRender{Text: ""}, "") {
			t.Error("resolvedTabHidden() = true, want false when there's no Task definition tab to compare against")
		}
	})
	t.Run("source present and identical - hidden", func(t *testing.T) {
		if !resolvedTabHidden(resolvedRender{Text: "- name: hi\n"}, "- name: hi\n") {
			t.Error("resolvedTabHidden() = false, want true for an identical resolve")
		}
	})
	t.Run("source present but different - not hidden", func(t *testing.T) {
		if resolvedTabHidden(resolvedRender{Text: "- name: hi there\n"}, "- name: hi\n") {
			t.Error("resolvedTabHidden() = true, want false when the resolved text actually differs")
		}
	})
	t.Run("still pending - hidden, no \"Resolving...\" placeholder", func(t *testing.T) {
		if !resolvedTabHidden(resolvedRender{Pending: true}, "- name: hi\n") {
			t.Error("resolvedTabHidden() = false, want true while still resolving - the tab stays entirely absent until there's something to show")
		}
	})
	t.Run("still pending - hidden even with no source to compare against", func(t *testing.T) {
		if !resolvedTabHidden(resolvedRender{Pending: true}, "") {
			t.Error("resolvedTabHidden() = false, want true while still resolving, regardless of source")
		}
	})
}

func TestBuildOutputTabsResolvedVisibility(t *testing.T) {
	const path = "/project/site.yml:3"
	const source = "- name: hi\n  ansible.builtin.debug:\n    msg: hi\n"

	task := &taskNode{
		Name:  "hi",
		Path:  path,
		Hosts: map[string]outcome{"web1": outcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
	}
	sourceIndex := taskSourceIndex{path: source}

	hasTab := func(names []string, name string) bool {
		return slices.Contains(names, name)
	}

	t.Run("identical to Task definition - Resolved tab omitted", func(t *testing.T) {
		names, _ := buildOutputTabs(task, "web1", sourceIndex, resolvedRender{Text: source}, resolvedRender{})
		if hasTab(names, "Resolved") {
			t.Errorf("names = %v, want no Resolved tab for an identical resolve", names)
		}
		if !hasTab(names, "Task definition") {
			t.Errorf("names = %v, want a Task definition tab regardless", names)
		}
	})

	t.Run("genuinely different from Task definition - Resolved tab shown", func(t *testing.T) {
		names, _ := buildOutputTabs(task, "web1", sourceIndex, resolvedRender{Text: "- name: hi\n  ansible.builtin.debug:\n    msg: hello world\n"}, resolvedRender{})
		if !hasTab(names, "Resolved") {
			t.Errorf("names = %v, want a Resolved tab when the resolved text differs", names)
		}
	})

	t.Run("still pending - Resolved tab omitted, no placeholder", func(t *testing.T) {
		names, _ := buildOutputTabs(task, "web1", sourceIndex, resolvedRender{Pending: true}, resolvedRender{})
		if hasTab(names, "Resolved") {
			t.Errorf("names = %v, want no Resolved tab while still pending", names)
		}
	})

	t.Run("resolve errored - Resolved tab shown", func(t *testing.T) {
		names, _ := buildOutputTabs(task, "web1", sourceIndex, resolvedRender{Err: "ansible-playbook exploded"}, resolvedRender{})
		if !hasTab(names, "Resolved") {
			t.Errorf("names = %v, want a Resolved tab on a genuine resolve error", names)
		}
	})

	t.Run("no source found at all - Resolved tab still shown, nothing to call it identical to", func(t *testing.T) {
		noSourceTask := &taskNode{
			Name:  "hi",
			Path:  "/project/unknown.yml:1",
			Hosts: map[string]outcome{"web1": outcomeOK},
			Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
		}
		names, _ := buildOutputTabs(noSourceTask, "web1", taskSourceIndex{}, resolvedRender{Text: ""}, resolvedRender{})
		if !hasTab(names, "Resolved") {
			t.Errorf("names = %v, want a Resolved tab when there's no Task definition tab to compare against", names)
		}
		if hasTab(names, "Task definition") {
			t.Errorf("names = %v, want no Task definition tab on a sourceIndex miss", names)
		}
	})
}

func TestDocsTabHidden(t *testing.T) {
	t.Run("zero value - no action was ever looked up - hidden", func(t *testing.T) {
		if !docsTabHidden(resolvedRender{}) {
			t.Error("docsTabHidden() = false, want true for the zero value (no action to look up)")
		}
	})
	t.Run("still pending - hidden, no placeholder", func(t *testing.T) {
		if !docsTabHidden(resolvedRender{Pending: true}) {
			t.Error("docsTabHidden() = false, want true while still fetching")
		}
	})
	t.Run("fetched successfully - shown", func(t *testing.T) {
		if docsTabHidden(resolvedRender{Text: "- name: copy\n  description: ...\n"}) {
			t.Error("docsTabHidden() = true, want false once ansible-doc's own output is in hand")
		}
	})
	t.Run("fetch errored - shown, not hidden behind the error", func(t *testing.T) {
		if docsTabHidden(resolvedRender{Err: "[ERROR]: module some_module not found"}) {
			t.Error("docsTabHidden() = true, want false on a genuine fetch error - that's real information")
		}
	})
}

func TestBuildOutputTabsDocsVisibility(t *testing.T) {
	task := &taskNode{
		Name:  "hi",
		Path:  "/project/unknown.yml:1",
		Hosts: map[string]outcome{"web1": outcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
	}
	hasTab := func(names []string, name string) bool {
		return slices.Contains(names, name)
	}

	t.Run("zero value docs - Docs tab omitted", func(t *testing.T) {
		names, _ := buildOutputTabs(task, "web1", taskSourceIndex{}, resolvedRender{}, resolvedRender{})
		if hasTab(names, "Docs") {
			t.Errorf("names = %v, want no Docs tab when nothing was ever looked up", names)
		}
	})

	t.Run("docs fetched - Docs tab shown with ansible-doc's own text", func(t *testing.T) {
		names, contents := buildOutputTabs(task, "web1", taskSourceIndex{}, resolvedRender{}, resolvedRender{Text: "- copy:\n"})
		idx := slices.Index(names, "Docs")
		if idx == -1 {
			t.Fatalf("names = %v, want a Docs tab once ansible-doc's output is in hand", names)
		}
		if !strings.Contains(contents[idx], "- copy:") {
			t.Errorf("Docs tab content = %q, want it to contain ansible-doc's own output", contents[idx])
		}
	})

	t.Run("docs errored - Docs tab shown with the error", func(t *testing.T) {
		names, contents := buildOutputTabs(task, "web1", taskSourceIndex{}, resolvedRender{}, resolvedRender{Err: "module not found"})
		idx := slices.Index(names, "Docs")
		if idx == -1 {
			t.Fatalf("names = %v, want a Docs tab on a genuine fetch error", names)
		}
		if !strings.Contains(contents[idx], "module not found") {
			t.Errorf("Docs tab content = %q, want it to contain the error", contents[idx])
		}
	})
}
