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

// End-to-end tests of design-docs/DebugMessages.md: the debug module's
// output expands its task while the run is going and is listed in the recap's
// Messages section. Needs tmux and ansible-playbook (see e2e_rerun_test.go).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_DebugMessages_ExpandedAndListedInRecap(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	root := repoRoot(t)
	playbook := filepath.Join(root, "testdata", "debugmessages.yml")
	inventory := filepath.Join(root, "testdata", "multihost-inventory.ini")

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run %s -i %s", shellQuote(t.TempDir()), e2eCallbackEnv(t), shellQuote(bin), shellQuote(playbook), shellQuote(inventory)), "Enter")
	got := s.waitFor("Messages (5)", 20*time.Second)

	// The recap is below the tree, so check the section first, then scroll
	// up to the tree: while the run was going the debug tasks expanded by
	// themselves (their host lines show the start of the message); the task
	// after the first one did not inherit that.
	// the recap's section: grouped hosts, items, the line cap, the var form
	for _, want := range []string{
		"Show version  all 2 hosts", "nginx 1.24.0",
		"Show who  host1", "I am host1", "I am host2",
		"x: item x", "z: item z",
		"l5", "... (+2 more lines)",
		"Show var  all 2 hosts",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Messages section lacks %q:\n%s", want, got)
		}
	}
	section := got[strings.Index(got, "Messages (5)"):]
	for _, bad := range []string{"must not show", "secret", "Skipped by when", "Needs verbosity", "Hidden", "Empty", "y: item y", "l6"} {
		if strings.Contains(section, bad) {
			t.Errorf("Messages section must not contain %q:\n%s", bad, section)
		}
	}

	s.send("Home")
	tree := s.waitFor("OK (nginx 1.24.0)", 3*time.Second)
	for _, want := range []string{"host1 (", "OK (nginx 1.24.0)", "OK (I am host2)"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree lacks %q:\n%s", want, tree)
		}
	}
	lines := strings.Split(tree, "\n")
	for i, l := range lines {
		if strings.Contains(l, "After a debug task") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    host") {
			t.Errorf("the task after a debug task must not be expanded:\n%s", tree)
		}
	}

	// Enter on the heading collapses the section, Enter again opens it
	s.send("End")
	for i := 0; i < 7; i++ {
		s.send("Up")
	}
	s.send("Enter")
	s.waitForAbsence("nginx 1.24.0\n", 3*time.Second)
	s.send("Enter")
	s.waitFor("Show version  all 2 hosts", 3*time.Second)
}

func TestE2E_DebugMessages_ShownAfterAFailedRun(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	dir := t.TempDir()
	playbook := filepath.Join(dir, "failing.yml")
	const body = `- hosts: localhost
  gather_facts: false
  tasks:
    - name: Tell
      debug:
        msg: "before the failure"
    - name: Break
      fail:
        msg: boom
`
	if err := os.WriteFile(playbook, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s run %s -i localhost,", shellQuote(dir), e2eCallbackEnv(t), shellQuote(bin), shellQuote(playbook)), "Enter")
	got := s.waitFor("Messages (1)", 20*time.Second)
	if !strings.Contains(got, "Tell  localhost") || !strings.Contains(got, "before the failure") {
		t.Errorf("the message of a failed run is missing from the recap:\n%s", got)
	}
}
