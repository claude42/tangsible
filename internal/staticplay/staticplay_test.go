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

package staticplay

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestShebangInterpreter(t *testing.T) {
	for _, c := range []struct{ head, want string }{
		{"#!/usr/bin/python3\nimport x", "/usr/bin/python3"},
		{"#!/usr/bin/python3 -s\n", "/usr/bin/python3"},
		{"#!/usr/bin/env python3\n", "python3"},
		{"#!/usr/bin/env -S python3 -s\n", "fb"},
		{"#!/opt/venv/bin/python\n", "/opt/venv/bin/python"},
		{"not a script\n", "fb"},
		{"", "fb"},
	} {
		if got := shebangInterpreter(c.head, "fb"); got != c.want {
			t.Errorf("shebangInterpreter(%q) = %q, want %q", c.head, got, c.want)
		}
	}
}

func TestParse(t *testing.T) {
	pb, err := Parse([]byte(`{"plays":[{"name":"p","hosts":"web:!db1","tasks":[{"name":"t","action":"ansible.builtin.debug","path":"/x/site.yml","line":4,"role":"","tags":[],"when":["a"]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pb.Plays) != 1 || pb.Plays[0].Hosts != "web:!db1" || len(pb.Plays[0].Tasks) != 1 {
		t.Fatalf("unexpected result: %+v", pb)
	}
	if got := pb.Plays[0].Tasks[0].Location(); got != "/x/site.yml:4" {
		t.Errorf("Location() = %q", got)
	}
	if _, err := Parse([]byte("nope")); err == nil {
		t.Error("garbage must be an error")
	}
}

// TestLoadFixture runs the real loader (needs ansible) against a fixture
// with a role, a block with rescue, an include and an unnamed task.
func TestLoadFixture(t *testing.T) {
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not installed")
	}
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "view", "site.yml"))
	if err != nil {
		t.Fatal(err)
	}
	pb, err := Load(abs)
	if err != nil {
		t.Fatal(err)
	}
	if len(pb.Plays) != 2 {
		t.Fatalf("plays = %d, want 2: %+v", len(pb.Plays), pb)
	}
	web := pb.Plays[0]
	if web.Name != "Web servers" || web.Hosts != "web:!db1" {
		t.Errorf("first play = %q / %q", web.Name, web.Hosts)
	}
	var names []string
	for _, tk := range web.Tasks {
		names = append(names, tk.Name)
		if tk.Path == "" || tk.Line == 0 {
			t.Errorf("task %q has no source location", tk.Name)
		}
	}
	want := []string{"pre one", "web : role task", "main one", "in block", "in rescue", "ansible.builtin.include_tasks", "ansible.builtin.copy"}
	if len(names) != len(want) {
		t.Fatalf("tasks = %q, want %q", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("task %d = %q, want %q", i, names[i], want[i])
		}
	}
	if web.Tasks[1].Role != "web" {
		t.Errorf("role = %q", web.Tasks[1].Role)
	}
	if got := pb.Plays[1].Hosts; got != "all" {
		t.Errorf("second play hosts = %q", got)
	}
}

func TestLoadParseError(t *testing.T) {
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not installed")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Error("a missing playbook must be an error")
	}
}
