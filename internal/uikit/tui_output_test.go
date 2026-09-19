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
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
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
			if got := RoleFromPath(c.path); got != c.want {
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
			if got := SkipOutputText(c.decoded); got != c.want {
				t.Errorf("skipOutputText(%v) = %q, want %q", c.decoded, got, c.want)
			}
		})
	}
}

func TestLoopItemLabels(t *testing.T) {
	t.Run("no results key at all - not a looped task", func(t *testing.T) {
		got := LoopItemLabels(map[string]interface{}{"changed": false})
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
		got := LoopItemLabels(decoded)
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
		got := LoopItemLabels(decoded)
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
		got := LoopItemLabels(decoded)
		want := []string{"onlyitem"}
		if !slices.Equal(got, want) {
			t.Errorf("loopItemLabels() = %v, want %v", got, want)
		}
	})

	t.Run("empty results is not nil but also not shown", func(t *testing.T) {
		got := LoopItemLabels(map[string]interface{}{"results": []interface{}{}})
		if len(got) != 0 {
			t.Errorf("loopItemLabels() = %v, want empty", got)
		}
	})
}

func TestLoopItemDetails(t *testing.T) {
	t.Run("a failed item's own msg is captured alongside its label", func(t *testing.T) {
		decoded := map[string]interface{}{
			"msg": "One or more items failed",
			"results": []interface{}{
				map[string]interface{}{
					"_ansible_item_label": "fish/functions",
					"item":                "fish/functions",
					"failed":              true,
					"msg":                 "There was an issue creating /home/claude/.config/fish as requested: [Errno 13] Permission denied: b'/home/claude/.config/fish'",
				},
				map[string]interface{}{
					"_ansible_item_label": "fish/completions",
					"item":                "fish/completions",
					"changed":             true,
				},
			},
		}
		got := LoopItemDetails(decoded)
		want := []LoopItemDetail{
			{Label: "fish/functions", Msg: "There was an issue creating /home/claude/.config/fish as requested: [Errno 13] Permission denied: b'/home/claude/.config/fish'"},
			{Label: "fish/completions", Msg: ""},
		}
		if !slices.Equal(got, want) {
			t.Errorf("loopItemDetails() = %+v, want %+v", got, want)
		}
	})

	t.Run("no results key at all - not a looped task", func(t *testing.T) {
		got := LoopItemDetails(map[string]interface{}{"changed": false})
		if got != nil {
			t.Errorf("loopItemDetails() = %v, want nil", got)
		}
	})
}

func TestPrimaryOutputFieldDebugCases(t *testing.T) {
	t.Run("debug msg: plain string", func(t *testing.T) {
		decoded := map[string]interface{}{"action": "ansible.builtin.debug", "msg": "hello world"}
		_, text := PrimaryOutputField(decoded)
		if text != "hello world" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "hello world")
		}
	})

	t.Run("debug msg: a list of strings - not a plain string, must not be silently dropped", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.debug",
			"msg":    []interface{}{"line one", "line two"},
		}
		_, text := PrimaryOutputField(decoded)
		if text != "line one\nline two" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "line one\nline two")
		}
	})

	t.Run("debug msg: a dict - falls back to pretty JSON, never empty", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.debug",
			"msg":    map[string]interface{}{"port": float64(8080)},
		}
		_, text := PrimaryOutputField(decoded)
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
		_, text := PrimaryOutputField(decoded)
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
		_, text := PrimaryOutputField(decoded)
		if !strings.Contains(text, "1") || !strings.Contains(text, "3") {
			t.Errorf("primaryOutputField() text = %q, want it to contain the list's own values", text)
		}
	})

	t.Run("debug with no msg and no extra key - nothing to show, not a crash", func(t *testing.T) {
		decoded := map[string]interface{}{"action": "ansible.builtin.debug", "changed": false}
		_, text := PrimaryOutputField(decoded)
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
		_, text := PrimaryOutputField(decoded)
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
		_, text := PrimaryOutputField(decoded)
		if text != "All items completed" {
			t.Errorf("primaryOutputField() text = %q, want %q", text, "All items completed")
		}
	})

	t.Run("a non-debug module with an extra field never triggers the var: heuristic", func(t *testing.T) {
		decoded := map[string]interface{}{
			"action": "ansible.builtin.stat",
			"stat":   map[string]interface{}{"exists": true},
		}
		_, text := PrimaryOutputField(decoded)
		if text != "" {
			t.Errorf("primaryOutputField() text = %q, want empty - the var: heuristic is debug-only", text)
		}
	})
}

func TestResolvedMatchesSource(t *testing.T) {
	cases := []struct {
		name     string
		resolved ResolvedRender
		source   string
		want     bool
	}{
		{
			name:     "pending - never matches, regardless of Text",
			resolved: ResolvedRender{Pending: true, Text: "- name: hi\n"},
			source:   "- name: hi\n",
			want:     false,
		},
		{
			name:     "error - never matches, even if Text happens to be set too",
			resolved: ResolvedRender{Err: "boom"},
			source:   "- name: hi\n",
			want:     false,
		},
		{
			name:     "byte-for-byte identical",
			resolved: ResolvedRender{Text: "- name: hi\n  debug:\n    msg: hi\n"},
			source:   "- name: hi\n  debug:\n    msg: hi\n",
			want:     true,
		},
		{
			name:     "identical modulo one trailing newline - ansible.builtin.template's own write isn't guaranteed to agree with source.go on this",
			resolved: ResolvedRender{Text: "- name: hi\n  debug:\n    msg: hi"},
			source:   "- name: hi\n  debug:\n    msg: hi\n",
			want:     true,
		},
		{
			name:     "genuinely different - a variable actually resolved",
			resolved: ResolvedRender{Text: "- name: hi\n  debug:\n    msg: hello world\n"},
			source:   "- name: hi\n  debug:\n    msg: '{{ greeting }}'\n",
			want:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolvedMatchesSource(c.resolved, c.source); got != c.want {
				t.Errorf("resolvedMatchesSource(%+v, %q) = %v, want %v", c.resolved, c.source, got, c.want)
			}
		})
	}
}

func TestResolvedTabHidden(t *testing.T) {
	t.Run("no source and no resolved text - hidden, nothing to show", func(t *testing.T) {
		if !ResolvedTabHidden(ResolvedRender{Text: ""}, "") {
			t.Error("resolvedTabHidden() = false, want true when there's neither a source nor any resolved text - e.g. the implicit Gathering Facts task")
		}
	})
	t.Run("no source to compare against but real resolved text - not hidden", func(t *testing.T) {
		if ResolvedTabHidden(ResolvedRender{Text: "some: resolved\nvalue: here\n"}, "") {
			t.Error("resolvedTabHidden() = true, want false when there's no Task definition tab to compare against but the resolve produced real content")
		}
	})
	t.Run("source present and identical - hidden", func(t *testing.T) {
		if !ResolvedTabHidden(ResolvedRender{Text: "- name: hi\n"}, "- name: hi\n") {
			t.Error("resolvedTabHidden() = false, want true for an identical resolve")
		}
	})
	t.Run("source present but different - not hidden", func(t *testing.T) {
		if ResolvedTabHidden(ResolvedRender{Text: "- name: hi there\n"}, "- name: hi\n") {
			t.Error("resolvedTabHidden() = true, want false when the resolved text actually differs")
		}
	})
	t.Run("still pending - hidden, no \"Resolving...\" placeholder", func(t *testing.T) {
		if !ResolvedTabHidden(ResolvedRender{Pending: true}, "- name: hi\n") {
			t.Error("resolvedTabHidden() = false, want true while still resolving - the tab stays entirely absent until there's something to show")
		}
	})
	t.Run("still pending - hidden even with no source to compare against", func(t *testing.T) {
		if !ResolvedTabHidden(ResolvedRender{Pending: true}, "") {
			t.Error("resolvedTabHidden() = false, want true while still resolving, regardless of source")
		}
	})
}

func TestBuildOutputTabsResolvedVisibility(t *testing.T) {
	const path = "/project/site.yml:3"
	const source = "- name: hi\n  ansible.builtin.debug:\n    msg: hi\n"

	task := &playbook.TaskNode{
		Name:  "hi",
		Path:  path,
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
	}
	sourceIndex := map[string]string{path: source}

	hasTab := func(names []string, name string) bool {
		return slices.Contains(names, name)
	}

	t.Run("identical to Task definition - Resolved tab omitted", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", sourceIndex, ResolvedRender{Text: source}, ResolvedRender{}, ResolvedRender{})
		if hasTab(names, "Resolved") {
			t.Errorf("names = %v, want no Resolved tab for an identical resolve", names)
		}
		if !hasTab(names, "Task definition") {
			t.Errorf("names = %v, want a Task definition tab regardless", names)
		}
	})

	t.Run("genuinely different from Task definition - Resolved tab shown", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", sourceIndex, ResolvedRender{Text: "- name: hi\n  ansible.builtin.debug:\n    msg: hello world\n"}, ResolvedRender{}, ResolvedRender{})
		if !hasTab(names, "Resolved") {
			t.Errorf("names = %v, want a Resolved tab when the resolved text differs", names)
		}
	})

	t.Run("still pending - Resolved tab omitted, no placeholder", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", sourceIndex, ResolvedRender{Pending: true}, ResolvedRender{}, ResolvedRender{})
		if hasTab(names, "Resolved") {
			t.Errorf("names = %v, want no Resolved tab while still pending", names)
		}
	})

	t.Run("resolve errored - Resolved tab shown", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", sourceIndex, ResolvedRender{Err: "ansible-playbook exploded"}, ResolvedRender{}, ResolvedRender{})
		if !hasTab(names, "Resolved") {
			t.Errorf("names = %v, want a Resolved tab on a genuine resolve error", names)
		}
	})

	t.Run("no source found and nothing resolved - Resolved tab omitted too", func(t *testing.T) {
		// The implicit "Gathering Facts" task's own shape: task.Path points
		// at the play's own "hosts:" line, which source.go never indexes as
		// a task, so both the source lookup and the resolve (fed that same
		// empty source) come back with nothing.
		noSourceTask := &playbook.TaskNode{
			Name:  "Gathering Facts",
			Path:  "/project/unknown.yml:1",
			Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
			Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
		}
		names, _ := BuildOutputTabs(noSourceTask, "web1", map[string]string{}, ResolvedRender{Text: ""}, ResolvedRender{})
		if hasTab(names, "Resolved") {
			t.Errorf("names = %v, want no Resolved tab when neither a source nor any resolved text exists", names)
		}
		if hasTab(names, "Task definition") {
			t.Errorf("names = %v, want no Task definition tab on a sourceIndex miss", names)
		}
	})

	t.Run("no source found but real resolved content - Resolved tab shown", func(t *testing.T) {
		noSourceTask := &playbook.TaskNode{
			Name:  "hi",
			Path:  "/project/unknown.yml:1",
			Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
			Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
		}
		names, _ := BuildOutputTabs(noSourceTask, "web1", map[string]string{}, ResolvedRender{Text: "- name: hi\n  debug:\n    msg: hello\n"}, ResolvedRender{}, ResolvedRender{})
		if !hasTab(names, "Resolved") {
			t.Errorf("names = %v, want a Resolved tab when there's no Task definition tab to compare against but the resolve produced real content", names)
		}
		if hasTab(names, "Task definition") {
			t.Errorf("names = %v, want no Task definition tab on a sourceIndex miss", names)
		}
	})
}

func TestDocsTabHidden(t *testing.T) {
	t.Run("zero value - no action was ever looked up - hidden", func(t *testing.T) {
		if !DocsTabHidden(ResolvedRender{}) {
			t.Error("docsTabHidden() = false, want true for the zero value (no action to look up)")
		}
	})
	t.Run("still pending - hidden, no placeholder", func(t *testing.T) {
		if !DocsTabHidden(ResolvedRender{Pending: true}) {
			t.Error("docsTabHidden() = false, want true while still fetching")
		}
	})
	t.Run("fetched successfully - shown", func(t *testing.T) {
		if DocsTabHidden(ResolvedRender{Text: "- name: copy\n  description: ...\n"}) {
			t.Error("docsTabHidden() = true, want false once ansible-doc's own output is in hand")
		}
	})
	t.Run("fetch errored - shown, not hidden behind the error", func(t *testing.T) {
		if DocsTabHidden(ResolvedRender{Err: "[ERROR]: module some_module not found"}) {
			t.Error("docsTabHidden() = true, want false on a genuine fetch error - that's real information")
		}
	})
}

func TestBuildOutputTabsDocsVisibility(t *testing.T) {
	task := &playbook.TaskNode{
		Name:  "hi",
		Path:  "/project/unknown.yml:1",
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"changed":false}`)},
	}
	hasTab := func(names []string, name string) bool {
		return slices.Contains(names, name)
	}

	t.Run("zero value docs - Docs tab omitted", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", map[string]string{}, ResolvedRender{}, ResolvedRender{}, ResolvedRender{})
		if hasTab(names, "Docs") {
			t.Errorf("names = %v, want no Docs tab when nothing was ever looked up", names)
		}
	})

	t.Run("docs fetched - Docs tab shown with ansible-doc's own text", func(t *testing.T) {
		names, contents := BuildOutputTabs(task, "web1", map[string]string{}, ResolvedRender{}, ResolvedRender{Text: "- copy:\n"}, ResolvedRender{})
		idx := slices.Index(names, "Docs")
		if idx == -1 {
			t.Fatalf("names = %v, want a Docs tab once ansible-doc's output is in hand", names)
		}
		if !strings.Contains(contents[idx], "- copy:") {
			t.Errorf("Docs tab content = %q, want it to contain ansible-doc's own output", contents[idx])
		}
	})

	t.Run("docs errored - Docs tab shown with the error", func(t *testing.T) {
		names, contents := BuildOutputTabs(task, "web1", map[string]string{}, ResolvedRender{}, ResolvedRender{Err: "module not found"}, ResolvedRender{})
		idx := slices.Index(names, "Docs")
		if idx == -1 {
			t.Fatalf("names = %v, want a Docs tab on a genuine fetch error", names)
		}
		if !strings.Contains(contents[idx], "module not found") {
			t.Errorf("Docs tab content = %q, want it to contain the error", contents[idx])
		}
	})
}

func TestRemoteFilePath(t *testing.T) {
	lineinfileRaw := json.RawMessage(`{"action":"ansible.builtin.lineinfile","path":"/etc/hosts","changed":true}`)
	moduleArgsOnlyRaw := json.RawMessage(`{"action":"ansible.builtin.lineinfile","invocation":{"module_args":{"path":"/etc/hosts"}}}`)
	unsupportedRaw := json.RawMessage(`{"action":"ansible.builtin.copy","dest":"/etc/motd"}`)
	noPathRaw := json.RawMessage(`{"action":"ansible.builtin.lineinfile"}`)

	cases := []struct {
		name          string
		task          *playbook.TaskNode
		host          string
		wantPath      string
		wantSupported bool
	}{
		{
			name:          "lineinfile with a top-level path",
			task:          &playbook.TaskNode{Raw: map[string]json.RawMessage{"web1": lineinfileRaw}},
			host:          "web1",
			wantPath:      "/etc/hosts",
			wantSupported: true,
		},
		{
			name:          "lineinfile with path only under invocation.module_args",
			task:          &playbook.TaskNode{Raw: map[string]json.RawMessage{"web1": moduleArgsOnlyRaw}},
			host:          "web1",
			wantPath:      "/etc/hosts",
			wantSupported: true,
		},
		{
			name:          "an unsupported module",
			task:          &playbook.TaskNode{Raw: map[string]json.RawMessage{"web1": unsupportedRaw}},
			host:          "web1",
			wantPath:      "",
			wantSupported: false,
		},
		{
			name:          "supported module but no path field found",
			task:          &playbook.TaskNode{Raw: map[string]json.RawMessage{"web1": noPathRaw}},
			host:          "web1",
			wantPath:      "",
			wantSupported: false,
		},
		{
			name:          "no result recorded yet for this host",
			task:          &playbook.TaskNode{Raw: map[string]json.RawMessage{}},
			host:          "web1",
			wantPath:      "",
			wantSupported: false,
		},
		{
			name:          "raw bytes that don't decode as JSON",
			task:          &playbook.TaskNode{Raw: map[string]json.RawMessage{"web1": json.RawMessage("not json")}},
			host:          "web1",
			wantPath:      "",
			wantSupported: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, supported := RemoteFilePath(c.task, c.host)
			if path != c.wantPath || supported != c.wantSupported {
				t.Errorf("RemoteFilePath(...) = (%q, %v), want (%q, %v)", path, supported, c.wantPath, c.wantSupported)
			}
		})
	}
}

func TestFileTabHidden(t *testing.T) {
	tests := []struct {
		name string
		file ResolvedRender
		want bool
	}{
		{"zero value - hidden", ResolvedRender{}, true},
		{"still pending - hidden", ResolvedRender{Pending: true}, true},
		{"fetched successfully - shown", ResolvedRender{Text: "127.0.0.1 localhost\n"}, false},
		{"fetch errored - hidden, unlike Docs/Resolved", ResolvedRender{Err: "permission denied"}, true},
		{"binary content (empty Text, no Err) - hidden", ResolvedRender{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FileTabHidden(tt.file); got != tt.want {
				t.Errorf("FileTabHidden(%+v) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}

func TestBuildOutputTabsFileVisibility(t *testing.T) {
	task := &playbook.TaskNode{
		Name:  "ensure hosts entry",
		Path:  "/project/unknown.yml:1",
		Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeOK},
		Raw:   map[string]json.RawMessage{"web1": json.RawMessage(`{"action":"ansible.builtin.lineinfile","path":"/etc/hosts","changed":false}`)},
	}
	hasTab := func(names []string, name string) bool {
		return slices.Contains(names, name)
	}

	t.Run("zero value file - File tab omitted", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", map[string]string{}, ResolvedRender{}, ResolvedRender{}, ResolvedRender{})
		if hasTab(names, "File") {
			t.Errorf("names = %v, want no File tab before a fetch was ever requested", names)
		}
	})

	t.Run("file fetched - File tab shown with its content", func(t *testing.T) {
		names, contents := BuildOutputTabs(task, "web1", map[string]string{}, ResolvedRender{}, ResolvedRender{}, ResolvedRender{Text: "127.0.0.1 localhost\n"})
		idx := slices.Index(names, "File")
		if idx == -1 {
			t.Fatalf("names = %v, want a File tab once fetched content is in hand", names)
		}
		if !strings.Contains(contents[idx], "127.0.0.1 localhost") {
			t.Errorf("File tab content = %q, want it to contain the fetched file's content", contents[idx])
		}
	})

	t.Run("file fetch errored - File tab stays hidden, no error text shown", func(t *testing.T) {
		names, _ := BuildOutputTabs(task, "web1", map[string]string{}, ResolvedRender{}, ResolvedRender{}, ResolvedRender{Err: "permission denied"})
		if hasTab(names, "File") {
			t.Errorf("names = %v, want no File tab on a fetch error - design-docs/ShowFileContents.md says don't display it", names)
		}
	})
}
