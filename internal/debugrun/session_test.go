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
	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", inventory, "--forks", "1"})
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

// TestSession_RelativePassthroughPathResolvesAgainstCallersCwd guards
// against Spawn regressing to setting cmd.Dir to the shadow copy (what it
// did before design-docs/DebugMode.md's "relative inventory path vs.
// shadow-copy cwd" fix): a relative -i must resolve against wherever the
// caller's own process cwd is, exactly like runner.SpawnGeneration's own
// plain run, never against the shadow copy's directory. It deliberately
// chdirs so an inventory file that exists only relative to that directory
// - never placed under the shadow copy itself - is the one thing that can
// make this pass.
func TestSession_RelativePassthroughPathResolvesAgainstCallersCwd(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: only task
      command: echo hi
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 1 {
		t.Fatalf("expected 1 task location, got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := debugcopy.BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := debugcopy.SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	// A separate directory, never part of the shadow copy, holding the
	// inventory under a plain relative name - resolving it at all proves
	// resolution happened against this directory, not copyDir.
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "inventory.ini"), []byte("[all]\nhost1 ansible_connection=local\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)

	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", "inventory.ini", "--forks", "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() {
		for range sess.Events {
		}
	}()

	pause, eof, err := sess.Start()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("session ended before any debug pause - the relative inventory path didn't resolve, so no host matched hosts: all")
	}
	if pause.Host != "host1" {
		t.Errorf("pause host = %q, want host1", pause.Host)
	}
}

// withTimeout runs fn in its own goroutine and fails the test fast (rather
// than waiting out go test's own default 10-minute timeout) if it doesn't
// return within d - specifically for a case like
// TestSession_HandlerPauseDoesNotHang below, where the exact failure mode
// under test *is* readUntilPrompt blocking forever: a plain t.Fatal from
// inside fn would never itself run if fn never returns.
func withTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out after %s - likely readUntilPrompt stuck waiting for a prompt that already arrived but didn't match promptRe", d)
	}
}

// TestSession_HandlerPauseDoesNotHang is a regression test for a real,
// reported hang: ansible/playbook/handler.go's Handler (a Task subclass)
// overrides __repr__ to "HANDLER: name" rather than Task's own "TASK:
// name" - promptRe only ever matched the latter, so a handler's own
// debugger prompt (confirmed live: "[localhost] HANDLER: say hello
// (debug)> ") never matched at all. The prompt had already fully arrived
// on the pipe; readUntilPrompt just kept calling ReadByte waiting for
// more, which never came, while the child sat blocked on its own stdin
// waiting for the next REPL command - a genuine deadlock, not a slow
// response, and still "stuck" for as long as the session lives (q/Ctrl-C
// works because that's a signal to the child, not a response to this
// blocked read).
func TestSession_HandlerPauseDoesNotHang(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: localhost
  connection: local
  gather_facts: false
  tasks:
    - name: step one
      command: echo one
      notify: say hello

    - name: step two
      command: echo two
  handlers:
    - name: say hello
      command: echo hello from handler
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 3 {
		t.Fatalf("expected 3 task locations (2 tasks + 1 handler), got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := debugcopy.BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := debugcopy.SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", "localhost,"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() {
		for range sess.Events {
		}
	}()

	var pauses []Pause
	withTimeout(t, 15*time.Second, func() {
		pause, eof, err := sess.Start()
		if err != nil {
			t.Fatal(err)
		}
		for !eof {
			pauses = append(pauses, pause)
			pause, eof, err = sess.Continue()
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	// Notified handlers run after every regular task in the play, not
	// right after the task that notified them - step one, step two, then
	// the handler, in that order.
	if len(pauses) != 3 {
		t.Fatalf("expected 3 pauses (step one, step two, handler), got %d: %+v", len(pauses), pauses)
	}
	var sawHandler bool
	for _, p := range pauses {
		if strings.HasPrefix(p.Task, "HANDLER: ") {
			sawHandler = true
		}
	}
	if !sawHandler {
		t.Errorf("no pause had a HANDLER: prefix: %+v", pauses)
	}
}
