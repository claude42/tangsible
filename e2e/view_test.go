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

//go:build e2e

// End-to-end tests for `tangsible view` (design-docs/View.md), sharing
// e2e/rerun_test.go's tmux harness.
package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2E_View_BrowsesTasksWithoutRunningAndRunsOnR walks the verb's whole
// life: the static tree, the three-tab drill-down (with the play's Hosts),
// the keys that belong to a run doing nothing, and r turning the session
// into a real run.
func TestE2E_View_BrowsesTasksWithoutRunningAndRunsOnR(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	playbook := filepath.Join(repoRoot(t), "testdata", "view", "site.yml")

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s view %s", shellQuote(t.TempDir()), e2eCallbackEnv(t), shellQuote(bin), shellQuote(playbook)), "Enter")
	// The first frame is drawn before the terminal width is known and cuts
	// long names; wait for the final one.
	tree := s.waitFor("ansible.builtin.include_tasks", 20*time.Second)
	for _, want := range []string{"View: site.yml", "Web servers", "pre one", "in rescue", "ansible.builtin.include_tasks", "enter: task"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree lacks %q:\n%s", want, tree)
		}
	}
	if strings.Contains(tree, "Filter:") || strings.Contains(tree, "00:0") {
		t.Errorf("the top bar still looks like a run's:\n%s", tree)
	}

	// Enter on the role's task (play row, "pre one", then it).
	s.send("Down", "Down", "Enter")
	task := s.waitFor("Hosts:", 5*time.Second)
	for _, want := range []string{"Role:   web", "Task:   role task", "Action: ansible.builtin.debug", "Hosts:  web:!db1", "Task definition"} {
		if !strings.Contains(task, want) {
			t.Errorf("Task tab lacks %q:\n%s", want, task)
		}
	}

	s.send("Tab")
	def := s.waitFor("- name: role task", 5*time.Second)
	if !strings.Contains(def, "ansible.builtin.debug: msg=role") {
		t.Errorf("Task definition tab lacks the task's source:\n%s", def)
	}

	// n goes to the next task, staying on the tab (as in a run's drill-down).
	s.send("n")
	s.waitFor("- name: main one", 5*time.Second)
	s.send("BTab")
	if next := s.waitFor("Task:   main one", 5*time.Second); !strings.Contains(next, "When:   x is defined") {
		t.Errorf("next task lacks its condition:\n%s", next)
	}

	// ←/→ do what n/N do here: back to "role task" and on again.
	s.send("Left")
	s.waitFor("Task:   role task", 5*time.Second)
	s.send("Right")
	s.waitFor("Task:   main one", 5*time.Second)

	// Back to the tree: f, o and s belong to a run and do nothing.
	s.send("Escape")
	s.waitForAbsence("Hosts:  web", 5*time.Second)
	s.send("f", "o", "s")
	time.Sleep(300 * time.Millisecond)
	after := s.capture()
	if !strings.Contains(after, "View: site.yml") || strings.Contains(after, "Filter") || strings.Contains(after, "Show only") {
		t.Errorf("f/o/s changed the view:\n%s", after)
	}

	// r opens the usual dialog; confirming it runs the playbook right here.
	s.send("r")
	s.waitFor("Re-run (enter: run", 5*time.Second)
	s.send("Enter")
	done := s.waitFor("Playbook completed successfully", 30*time.Second)
	if !strings.Contains(done, "Playbook: site.yml") {
		t.Errorf("after r the session should be a plain run:\n%s", done)
	}
}

// TestE2E_View_UnreadablePlaybookExitsWithTheReason confirms a playbook
// ansible cannot parse never opens a TUI and its error is shown.
func TestE2E_View_UnreadablePlaybookExitsWithTheReason(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(bad, []byte("- hosts: all\n  tasks:\n    - name: x\n      bogus_module_xyz: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "view", bad)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("view of an unparsable playbook should fail, got:\n%s", out)
	}
	if !strings.Contains(string(out), "couldn't read") || !strings.Contains(string(out), "bogus_module_xyz") {
		t.Errorf("the error should say what's wrong, got:\n%s", out)
	}
}

// TestE2E_View_ReloadsAfterEditsAndKeepsTheOldTreeOnAnError covers R (an
// edit made elsewhere), a file that no longer loads, and e (the editor's
// return reloads, the drill-down stays on its task).
func TestE2E_View_ReloadsAfterEditsAndKeepsTheOldTreeOnAnError(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	dir := t.TempDir()
	site := filepath.Join(dir, "site.yml")
	const original = "- name: Only play\n  hosts: localhost\n  gather_facts: false\n  tasks:\n    - name: first task\n      ansible.builtin.debug: msg=1\n    - name: second task\n      ansible.builtin.debug: msg=2\n"
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(site, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(original)
	// An "editor" that renames the second task in the file it is given.
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nsed -i.bak 's/second task/second task, edited/' \"$1\" && rm -f \"$1.bak\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && VISUAL=%s EDITOR=%s %s %s view site.yml", shellQuote(dir), shellQuote(editor), shellQuote(editor), e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	s.waitFor("second task", 20*time.Second)

	// R after an edit made elsewhere.
	write(original + "    - name: third task\n      ansible.builtin.debug: msg=3\n")
	s.send("R")
	got := s.waitFor("third task", 10*time.Second)
	if !strings.Contains(got, "reloaded") {
		t.Errorf("a reload should say so:\n%s", got)
	}

	// A file that no longer loads: the old tree stays, the reason is shown.
	write("- hosts: all\n  tasks:\n    - name: broken\n      bogus_module_xyz: {}\n")
	s.send("R")
	got = s.waitFor("reload failed", 10*time.Second)
	if !strings.Contains(got, "third task") || !strings.Contains(got, "bogus_module_xyz") {
		t.Errorf("the old tree and the reason should show:\n%s", got)
	}

	// e: open the second task and let the editor rename it. The task is
	// found again only by its name, so the drill-down closes with a note, and
	// the tree shows the new name.
	write(original)
	s.send("R")
	s.waitForAbsence("third task", 10*time.Second)
	s.send("Down", "Down", "Enter") // play row -> first task -> second task
	s.waitFor("Task:   second task", 5*time.Second)
	s.send("e")
	got = s.waitFor("second task, edited", 15*time.Second)
	if !strings.Contains(got, "the task you were looking at is gone") {
		t.Errorf("returning from the editor should reload, and say the renamed task is gone:\n%s", got)
	}
}
