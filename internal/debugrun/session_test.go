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

package debugrun

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/debugcopy"
	"code.aw.net/claude/tangsible/internal/source"
)

// requireAnsiblePlaybook skips the test if ansible-playbook isn't on
// PATH - same pattern internal/vaultcrypto's own real-binary interop
// tests already use, not just a one-off manual check.
func requireAnsiblePlaybook(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not found in PATH, skipping live debug-session test")
	}
}

// repoRoot finds the repository root from this test file's own location,
// so the bundled callback plugin (callback/tangsible_jsonl.go) can be
// found the same dev-only way CLAUDE.md documents (TANGSIBLE_CALLBACK_DIR).
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestSession_LiveDualChannel(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: task one
      set_fact:
        step_var: "one-{{ inventory_hostname }}"

    - name: task two
      command: "echo step2-{{ inventory_hostname }}"
      register: step2_result

    - name: task three
      set_fact:
        step_var: "three-{{ inventory_hostname }}"
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 3 {
		t.Fatalf("expected 3 task locations, got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := debugcopy.BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := debugcopy.SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	inventory := filepath.Join(repoRoot(t), "testdata", "multihost-inventory.ini")
	sess, err := Spawn(copyDir, "playbook.yml", []string{"-i", inventory, "--forks", "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	type eventRec struct {
		at   time.Time
		name string
		task string
	}
	var observedEvents []eventRec
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for item := range sess.Events {
			if !item.IsEvent {
				continue
			}
			taskName := ""
			if item.Ev.Task != nil {
				taskName = item.Ev.Task.Name
			}
			observedEvents = append(observedEvents, eventRec{time.Now(), item.Ev.Event, taskName})
		}
	}()

	pause, eof, err := sess.Start()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("session ended before the first debug pause")
	}
	if pause.Host != "host1" {
		t.Errorf("first pause host = %q, want host1 (forks=1, linear)", pause.Host)
	}

	var pauses []Pause
	pauses = append(pauses, pause)

	for i := 0; i < 20 && !eof; i++ {
		if pause.Host == "host1" && strings.Contains(pause.Task, "task three") {
			raw, qerr := sess.Query("task_vars.get('step_var')")
			if qerr != nil {
				t.Fatalf("query failed: %v", qerr)
			}
			var val string
			if jerr := json.Unmarshal(raw, &val); jerr != nil {
				t.Fatalf("query result %q is not a JSON string: %v", raw, jerr)
			}
			// task_vars is the pre-task snapshot (design-docs/DebugMode.md's
			// own confirmed limitation) - at task three's own pause, the
			// variable should still read task one's value, not task
			// three's own not-yet-merged update.
			if val != "one-host1" {
				t.Errorf("task_vars['step_var'] at task three's pause = %q, want %q (pre-task snapshot)", val, "one-host1")
			}
		}

		pause, eof, err = sess.Continue()
		if err != nil {
			t.Fatal(err)
		}
		if !eof {
			pauses = append(pauses, pause)
		}
	}

	<-eventsDone

	if len(pauses) != 6 { // 3 tasks x 2 hosts
		t.Fatalf("expected 6 pauses (3 tasks x 2 hosts), got %d: %+v", len(pauses), pauses)
	}

	sawTaskStart := map[string]bool{}
	for _, e := range observedEvents {
		if e.name == "v2_playbook_on_task_start" {
			sawTaskStart[e.task] = true
		}
	}
	for _, want := range []string{"task one", "task two", "task three"} {
		if !sawTaskStart[want] {
			t.Errorf("never observed a v2_playbook_on_task_start event for %q (events: %+v)", want, observedEvents)
		}
	}
}
