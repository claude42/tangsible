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
	"fmt"
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
// so the bundled callback plugin (callback/tangsible_events.py) can be
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

// TestSession_Redo confirms Session.Redo (ansible-core's own debugger
// "redo" command, design-docs/DebugMode.md's "redo" follow-up) re-queues
// and re-pauses on the exact same task/host, not a different one - and
// uses a task whose own outcome depends on an external side effect (a
// marker file its first execution itself creates) so a flipped result on
// the second pause can only mean the task genuinely re-executed, not that
// the debugger just re-showed a cached pause.
func TestSession_Redo(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	marker := filepath.Join(proj, "marker")
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := fmt.Sprintf(`- hosts: all
  gather_facts: false
  tasks:
    - name: flaky task
      shell: "test -f '%s' && echo exists || (touch '%s'; echo missing)"
      register: result
      failed_when: "'missing' in result.stdout"
`, marker, marker)
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

	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", "localhost,", "-c", "local"})
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
		t.Fatal("session ended before the first debug pause")
	}
	if pause.Task != "TASK: flaky task" {
		t.Fatalf("first pause = %+v, want TASK: flaky task", pause)
	}

	raw, err := sess.Query("result.is_failed()")
	if err != nil {
		t.Fatal(err)
	}
	var firstFailed bool
	if err := json.Unmarshal(raw, &firstFailed); err != nil {
		t.Fatal(err)
	}
	if !firstFailed {
		t.Fatalf("expected the first run to fail (marker file didn't exist yet), got failed=%v", firstFailed)
	}

	redone, eof, err := sess.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected a second pause on the redone task, got eof")
	}
	if redone.Task != pause.Task || redone.Host != pause.Host {
		t.Errorf("redo pause = %+v, want the exact same task/host as %+v", redone, pause)
	}

	raw, err = sess.Query("result.is_failed()")
	if err != nil {
		t.Fatal(err)
	}
	var secondFailed bool
	if err := json.Unmarshal(raw, &secondFailed); err != nil {
		t.Fatal(err)
	}
	if secondFailed {
		t.Error("expected the redone run to succeed (marker file now exists), got failed=true")
	}

	if _, eof, err := sess.Continue(); err != nil {
		t.Fatal(err)
	} else if !eof {
		t.Error("expected eof after continuing past the redone task's own pause")
	}
}

// TestSession_Redo_MultiHostInterleaving documents a real wrinkle
// discovered empirically while implementing this: Redo's own next pause
// is NOT guaranteed to be the redone task/host - only the raw "next
// prompt that happens to arrive" (exactly Continue's own contract).
// With forks=1 and two hosts, by the time the first host's own pause
// fires, ansible-core's _queue_task has typically already finished
// running every host in this same lockstep batch (the queueing for-loop
// itself blocks on worker-slot reuse until each host's worker exits), so
// both hosts' results are already sitting in the same pending-results
// list debug_closure iterates one at a time - do_redo's own re-queue
// doesn't retroactively reorder that list; it only requeues host1 and
// recurses once (plugins/strategy/__init__.py's debug_closure,
// `_processed_results.extend(debug_closure(func)(self, iterator,
// one_pass))`), which - confirmed live - finds nothing ready yet (the
// redone host1 needs real wall-clock time to re-execute), so the OUTER
// loop falls through to host2's own already-pending, never-redone pause
// next. The redone host1 only resurfaces once that batch fully drains
// and the main strategy loop calls _process_pending_results again - i.e.
// after an ordinary Continue() past host2.
//
// This isn't a bug to work around: every pause along the way (including
// host2's genuinely unrelated one) is handled by the exact same generic
// pause-handling code any other pause is (internal/session's
// debugApplyPauseResult never assumes a pause is the one it asked for),
// so nothing breaks - it's simply not instant when other hosts' results
// for the same task are already pending, the same "one host at a time,
// in whatever order the batch drains" characteristic StepNextTask's own
// test already relies on for an *ordinary* Continue. Documented here so
// a future change to debugRedo's own UI-facing comments/behavior isn't
// made on the wrong assumption.
func TestSession_Redo_MultiHostInterleaving(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	markerDir := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := fmt.Sprintf(`- hosts: all
  gather_facts: false
  tasks:
    - name: flaky task
      shell: "test -f '%s/{{ inventory_hostname }}' && echo exists || (touch '%s/{{ inventory_hostname }}'; echo missing)"
      register: result
      failed_when: "'missing' in result.stdout"
`, markerDir, markerDir)
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

	inventory := filepath.Join(repoRoot(t), "testdata", "multihost-inventory.ini")
	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", inventory, "--forks", "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() {
		for range sess.Events {
		}
	}()

	isFailed := func() bool {
		t.Helper()
		raw, err := sess.Query("result.is_failed()")
		if err != nil {
			t.Fatal(err)
		}
		var failed bool
		if err := json.Unmarshal(raw, &failed); err != nil {
			t.Fatal(err)
		}
		return failed
	}

	pause, eof, err := sess.Start()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("session ended before the first debug pause")
	}
	if pause.Host != "host1" {
		t.Fatalf("first pause = %+v, want host1 (forks=1)", pause)
	}
	if !isFailed() {
		t.Fatal("expected host1's first run to fail (its own marker didn't exist yet)")
	}

	// Redo host1 - but host2's own pause (already pending from the same
	// batch, never redone) surfaces first, exactly like an ordinary
	// Continue would have - not a re-pause of host1.
	next, eof, err := sess.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected host2's own pending pause, got eof")
	}
	if next.Host != "host2" {
		t.Fatalf("pause right after redoing host1 = %+v, want host2 (its own pending, never-redone pause) - if this starts failing, the batching/interleaving behavior this test documents may have changed", next)
	}
	if !isFailed() {
		t.Fatal("expected host2's own first run to fail too (its own marker didn't exist yet either) - host2 was never redone")
	}

	// Only now, once host2's own pending pause has been drained via an
	// ordinary Continue, does host1's redone result finally surface.
	redone, eof, err := sess.Continue()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected the redone host1 pause, got eof")
	}
	if redone.Host != "host1" {
		t.Fatalf("pause after draining host2 = %+v, want host1 (the redo finally catching up)", redone)
	}
	if isFailed() {
		t.Error("expected the redone host1 run to succeed this time (its marker now exists)")
	}

	if _, eof, err := sess.Continue(); err != nil {
		t.Fatal(err)
	} else if !eof {
		t.Error("expected eof - nothing left pending after the redone host1 pause")
	}
}

// TestExtractErrorLine is a plain, no-ansible-playbook-needed unit test
// for extractErrorLine's own per-line scan.
func TestExtractErrorLine(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantLine string
		wantOK   bool
	}{
		{"no error", "some noise\n0\n", "", false},
		{"one error line", "noise before\n***TypeError:'bad'\nmore noise\n", "***TypeError:'bad'", true},
		{"first of several wins", "***NameError:'x'\n***ValueError:'y'\n", "***NameError:'x'", true},
		{"empty text", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, ok := extractErrorLine(c.text)
			if ok != c.wantOK || line != c.wantLine {
				t.Errorf("extractErrorLine(%q) = (%q, %v), want (%q, %v)", c.text, line, ok, c.wantLine, c.wantOK)
			}
		})
	}
}

// TestSession_Set confirms Session.Set (design-docs/DebugMode.md's "Stage
// 5") genuinely mutates task_vars for the task actually being reached
// next, not just for the debugger's own read-back of the same pause - the
// marker-file-dependent task itself (same shape TestSession_Redo uses) is
// deliberately made to depend on the set variable's own value via
// failed_when, so a flipped outcome can only mean the real exec reached
// the live child process's own task_vars, not some local bookkeeping.
func TestSession_Set(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: flaky task
      set_fact:
        outcome_var: "bad"

    - name: depends on outcome_var
      command: "echo {{ outcome_var }}"
      register: result
      failed_when: "outcome_var != 'good'"
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 2 {
		t.Fatalf("expected 2 task locations, got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := debugcopy.BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := debugcopy.SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", "localhost,", "-c", "local"})
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
		t.Fatal("session ended before the first debug pause")
	}
	if pause.Task != "TASK: flaky task" {
		t.Fatalf("first pause = %+v, want TASK: flaky task", pause)
	}

	pause, eof, err = sess.Continue()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("session ended before the second debug pause")
	}
	if pause.Task != "TASK: depends on outcome_var" {
		t.Fatalf("second pause = %+v, want TASK: depends on outcome_var", pause)
	}

	if err := sess.Set("task_vars['outcome_var']", "'good'"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	pause, eof, err = sess.Redo()
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected a re-pause of the redone task, got eof")
	}
	if pause.Task != "TASK: depends on outcome_var" {
		t.Fatalf("redo pause = %+v, want the same task", pause)
	}

	raw, err := sess.Query("result.is_failed()")
	if err != nil {
		t.Fatal(err)
	}
	var failed bool
	if err := json.Unmarshal(raw, &failed); err != nil {
		t.Fatal(err)
	}
	if failed {
		t.Error("expected the redone task to succeed with outcome_var set to 'good', but it failed")
	}

	if _, eof, err := sess.Continue(); err != nil {
		t.Fatal(err)
	} else if !eof {
		t.Error("expected eof after continuing past the redone task's own pause")
	}
}

// TestSession_Set_Failure confirms a failed assignment (an undefined
// name on the right-hand side, so the exec itself raises a NameError)
// reports a non-nil error carrying the exception text, and - just as
// important - that the REPL session is still fully usable afterward:
// default()'s own except handler swallows the re-raise it triggers, so
// nothing about the pipe or the debugger's own prompt state is left
// corrupted by one failed statement.
func TestSession_Set_Failure(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: localhost
  connection: local
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

	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", "localhost,"})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() {
		for range sess.Events {
		}
	}()

	if _, eof, err := sess.Start(); err != nil {
		t.Fatal(err)
	} else if eof {
		t.Fatal("session ended before the first debug pause")
	}

	err = sess.Set("task_vars['outcome_var']", "this_name_does_not_exist_anywhere")
	if err == nil {
		t.Fatal("expected Set to fail assigning an undefined right-hand side, got nil error")
	}
	if !strings.Contains(err.Error(), "NameError") {
		t.Errorf("Set error = %q, want it to mention NameError", err.Error())
	}

	// The session must still be usable: a plain Query and a Continue to
	// completion both need to keep working after one failed exec.
	if _, qerr := sess.Query("1 + 1"); qerr != nil {
		t.Errorf("Query after a failed Set = %v, want it to still work", qerr)
	}
	if _, eof, cerr := sess.Continue(); cerr != nil {
		t.Fatal(cerr)
	} else if !eof {
		t.Error("expected eof after continuing past the only task")
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
