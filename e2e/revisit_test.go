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

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Smoke tests for revisit (design-docs/Revisit.md): a run is made for real
// with `tangsible run`, then browsed, opened and exported with `tangsible
// revisit`, and the exported text file is opened again. The saved run logs
// under .tangsible/runs are the only thing the two commands share.

const e2eRevisitPlaybook = `- hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: first revisited task
      debug:
        msg: hello from the original run
    - name: second revisited task
      debug:
        msg: bye
`

// startSavedRun runs the playbook once in a fresh dir, waits until it is
// saved, quits, and returns the session (back at the shell prompt in that
// dir), the dir and the binary.
func startSavedRun(t *testing.T) (*tmuxSession, string, string) {
	t.Helper()
	s, dir, bin := startRunSession(t, map[string]string{"site.yml": e2eRevisitPlaybook}, "run site.yml -i localhost, --no-dialog")
	s.waitFor("Playbook completed successfully", 30*time.Second)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if logs, _ := filepath.Glob(filepath.Join(dir, ".tangsible", "runs", "*.jsonl")); len(logs) > 0 {
			break
		} else if time.Now().After(deadline) {
			t.Fatal("the run was never saved under .tangsible/runs")
		}
		time.Sleep(150 * time.Millisecond)
	}
	s.quitCleanly()
	return s, dir, bin
}

func (s *tmuxSession) startRevisit(bin, args string) {
	s.send(fmt.Sprintf("%s %s revisit %s", e2eCallbackEnv(s.t), shellQuote(bin), args), "Enter")
}

func TestE2E_Revisit_ListOpensTheSavedRunAsATree(t *testing.T) {
	requireE2ETools(t)
	s, _, bin := startSavedRun(t)
	s.startRevisit(bin, "")
	got := s.waitFor("previous run(s)", 15*time.Second)
	if !strings.Contains(got, "tangsible run site.yml") {
		t.Errorf("the run is not listed with its command line:\n%s", got)
	}
	s.send("Enter")
	got = s.waitFor("Playbook completed successfully", 15*time.Second)
	for _, task := range []string{"first revisited task", "second revisited task"} {
		if !strings.Contains(got, task) {
			t.Errorf("the replayed tree lacks %q:\n%s", task, got)
		}
	}
	// q goes back to the list, a second q leaves.
	s.send("q")
	s.waitFor("previous run(s)", 5*time.Second)
	s.send("q")
	s.waitForAbsence("previous run(s)", 5*time.Second)
}

func TestE2E_Revisit_ExportedFileOpensAgain(t *testing.T) {
	requireE2ETools(t)
	s, dir, bin := startSavedRun(t)
	s.startRevisit(bin, "")
	s.waitFor("previous run(s)", 15*time.Second)

	s.send("E")
	s.waitFor("Export this run", 5*time.Second)
	s.send("C-u") // the field starts with a suggested name
	s.send("exported.txt", "Enter")
	s.waitFor("exported to exported.txt", 5*time.Second)
	s.send("q")
	s.waitForAbsence("previous run(s)", 5*time.Second)

	data, err := os.ReadFile(filepath.Join(dir, "exported.txt"))
	if err != nil {
		t.Fatalf("no export file: %v", err)
	}
	if !strings.Contains(string(data), "first revisited task") || !strings.Contains(string(data), "hello from the original run") {
		t.Errorf("the export lacks the tasks or their results:\n%s", data)
	}

	// The saved runs are no longer needed: the text file alone is enough.
	if err := os.RemoveAll(filepath.Join(dir, ".tangsible")); err != nil {
		t.Fatal(err)
	}
	s.startRevisit(bin, "exported.txt")
	got := s.waitFor("Playbook completed successfully", 15*time.Second)
	if !strings.Contains(got, "second revisited task") {
		t.Errorf("the re-imported run lacks its tasks:\n%s", got)
	}
	s.send("q")
}

func TestE2E_Revisit_NoSavedRunsIsSaidPlainly(t *testing.T) {
	requireE2ETools(t)
	s, _, _ := startRunSession(t, map[string]string{}, "revisit")
	s.waitFor("no matching previous runs to revisit", 15*time.Second)
}
