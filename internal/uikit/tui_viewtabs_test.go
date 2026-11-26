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
)

func TestBuildViewTaskTab(t *testing.T) {
	got := BuildViewTaskTab(ViewTask{
		Name: "web : install", Path: "/p/roles/web/tasks/main.yml:3", Action: "ansible.builtin.package",
		Hosts: "web:!db1", Tags: []string{"pkg", "web"}, When: []string{"a", "b"},
	})
	for _, want := range []string{"Role:", "web", "Task:", "install", "Action:", "ansible.builtin.package", "Hosts:", "web:!db1", "When:", "a and b", "Tags:", "pkg, web"} {
		if !strings.Contains(got, want) {
			t.Errorf("Task tab lacks %q:\n%s", want, got)
		}
	}
	minimal := BuildViewTaskTab(ViewTask{Name: "plain", Hosts: "all"})
	if strings.Contains(minimal, "When:") || strings.Contains(minimal, "Tags:") || strings.Contains(minimal, "Action:") {
		t.Errorf("empty fields must be left out:\n%s", minimal)
	}
}

func TestBuildViewTabs(t *testing.T) {
	task := ViewTask{Name: "t", Path: "/p/site.yml:2", Action: "a", Hosts: "all"}
	index := map[string]string{"/p/site.yml:2": "- name: t\n  a:"}

	names, _ := BuildViewTabs(task, index, ResolvedRender{})
	if strings.Join(names, ",") != "Task,Task definition" {
		t.Errorf("without docs the tabs are %v", names)
	}
	names, _ = BuildViewTabs(task, index, ResolvedRender{Text: "docs"})
	if strings.Join(names, ",") != "Task,Task definition,Docs" {
		t.Errorf("with docs the tabs are %v", names)
	}
	names, _ = BuildViewTabs(task, nil, ResolvedRender{Pending: true})
	if strings.Join(names, ",") != "Task" {
		t.Errorf("no source and pending docs leave only Task, got %v", names)
	}
}
