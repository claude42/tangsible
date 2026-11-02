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

// End-to-end tests for `tangsible debug` (design-docs/DebugMode.md), sharing
// e2e_rerun_test.go's tmux harness. The headless pieces (debugcopy,
// debugrun, debugverb) are unit-tested; what only a real pty can show is
// the wiring between them and the TUI: that F9 arms a breakpoint, that F5
// really runs a real ansible-playbook up to it and surfaces the pause, and
// that the session ends cleanly afterwards.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startDebugSession writes playbookYAML (plus any extra files, name ->
// content) into a fresh workDir - not testdata/, see the first test's note -
// and starts `tangsible debug site.yml <args>` in a tmux pane there. It
// returns the pane and the workDir.
func startDebugSession(t *testing.T, playbookYAML string, extra map[string]string, args string) (*tmuxSession, string) {
	t.Helper()
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	files := map[string]string{"site.yml": playbookYAML}
	for name, content := range extra {
		files[name] = content
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s debug site.yml %s", shellQuote(workDir), e2eCallbackEnv(t), shellQuote(bin), args), "Enter")
	return s, workDir
}

// TestE2E_Debug_BreakpointPausesThenRunsToCompletion arms a breakpoint on
// the last of three tasks, runs with F5, and checks the pause (earlier
// tasks done, the run not finished, the bottom bar switched to the paused
// key set), then continues with F5 to the end.
//
// The fixture lives in the test's own workDir, not testdata/, for the same
// reason as TestE2E_CLIStartAtPlay: BuildTaskSourceIndex walks the
// playbook's whole directory.
func TestE2E_Debug_BreakpointPausesThenRunsToCompletion(t *testing.T) {
	requireE2ETools(t)
	s, _ := startDebugSession(t, `- name: debug play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: first task
      command: echo one

    - name: second task
      command: echo two

    - name: third task
      command: echo three
`, nil, "-i localhost,")

	// The whole predicted tree is on screen before anything runs
	// (debugverb.SeedState), idle: the bar offers "run", not "continue".
	got := s.waitFor("F5: run", 15*time.Second)
	for _, task := range []string{"first task", "second task", "third task"} {
		if !strings.Contains(got, task) {
			t.Fatalf("expected the pre-seeded tree to list %q before anything ran:\n%s", task, got)
		}
	}
	if strings.Contains(got, "Playbook completed successfully") {
		t.Fatalf("debug ran the playbook before F5:\n%s", got)
	}

	// Two Downs from the initial selection land on the third task; F9 arms
	// it ("●" in the row's first column). Asserting the marker's row pins
	// down a selection mismatch right here instead of as a confusing
	// failure after F5.
	s.send("Down", "Down")
	s.send("F9")
	s.waitForFieldValue("third task", "●", 5*time.Second)
	if marked := lineContaining(s.capture(), "●"); !strings.Contains(marked, "third task") {
		t.Fatalf("breakpoint armed on the wrong row %q, want \"third task\":\n%s", marked, s.capture())
	}

	// F5 runs to the breakpoint. The pause auto-opens the drill-down in
	// two-pane mode (DebugMode.md), whose own bar replaces the debug one
	// until Esc - so wait for the breakpoint task's drill-down first.
	s.send("F5")
	s.waitFor("Task:   third task", 20*time.Second)
	s.send("Escape")
	paused := s.waitFor("F5: continue", 5*time.Second)

	if strings.Contains(paused, "Playbook completed successfully") {
		t.Errorf("the run finished instead of pausing at the breakpoint:\n%s", paused)
	}
	if !strings.Contains(paused, "F12: redo") {
		t.Errorf("paused bar should offer F12: redo:\n%s", paused)
	}
	for _, task := range []string{"first task", "second task"} {
		if !strings.Contains(lineContaining(paused, task), "localhost") {
			t.Errorf("%q should already have run (a host result under it) when pausing at the third task:\n%s", task, paused)
		}
	}

	// Continuing runs it out; the session is over and says so.
	s.send("F5")
	done := s.waitFor("Playbook completed successfully", 20*time.Second)
	if !strings.Contains(done, "Completed 3 tasks") {
		t.Errorf("expected all three tasks in the recap:\n%s", done)
	}
	s.waitFor("F5/F9/F10/F11/F12 disabled", 5*time.Second)
}

// TestE2E_Debug_StepAndNextTaskOnTwoHosts pins down the F10/F11 split
// (DebugMode.md, "F10/F11 split"): F11 stops at the very next pause, even
// the other host of the same task; F10 skips the rest of the current
// task's hosts and stops at the next task's first pause. --forks 1 makes
// the host order deterministic (h1 before h2).
func TestE2E_Debug_StepAndNextTaskOnTwoHosts(t *testing.T) {
	requireE2ETools(t)
	s, _ := startDebugSession(t, `- name: step play
  hosts: all
  gather_facts: false
  tasks:
    - name: step one
      command: echo one

    - name: step two
      command: echo two

    - name: step three
      command: echo three
`, map[string]string{"hosts.ini": "h1 ansible_connection=local\nh2 ansible_connection=local\n"},
		"-i hosts.ini --forks 1")

	s.waitFor("F5: run", 15*time.Second)

	// The initial selection is the first task: arm it and run to it.
	s.send("F9")
	s.waitForFieldValue("step one", "●", 5*time.Second)
	s.send("F5")
	s.waitFor("Host:   h1", 20*time.Second)

	// F11: the very next pause is the *other host* of the same task.
	s.send("F11")
	got := s.waitFor("Host:   h2", 20*time.Second)
	if !strings.Contains(got, "Task:   step one") {
		t.Errorf("F11 should stay on step one (next host), got:\n%s", got)
	}

	// F10 from h2 (nothing left on step one anyway): step two's first pause.
	s.send("F10")
	got = s.waitFor("Task:   step two", 20*time.Second)
	if !strings.Contains(got, "Host:   h1") {
		t.Errorf("F10 should stop at step two's first host, h1:\n%s", got)
	}

	// F10 from h1 with h2 still pending on step two: h2 is skipped - it
	// runs without ever pausing - and the stop is step three's h1.
	s.send("F10")
	got = s.waitFor("Task:   step three", 20*time.Second)
	if !strings.Contains(got, "Host:   h1") {
		t.Errorf("F10 should stop at step three's first host, h1:\n%s", got)
	}
	if !strings.Contains(got, "h2: Changed (two") {
		t.Errorf("F10 should have run (not paused at) step two's other host h2:\n%s", got)
	}

	s.send("F5")
	done := s.waitFor("Playbook completed successfully", 20*time.Second)
	if !strings.Contains(done, "Completed 3 tasks on 2 reachable hosts") {
		t.Errorf("expected all three tasks on both hosts in the recap:\n%s", done)
	}
}

// TestE2E_Debug_FailedTaskPausesAndRedoRecovers covers a failing task and
// F12: the armed task fails (its marker file doesn't exist yet), the pause
// shows it Failed; once the cause is fixed, F12 re-runs that same task/host
// and the very same row flips to Changed - the recap then counts it as
// changed, not failed.
func TestE2E_Debug_FailedTaskPausesAndRedoRecovers(t *testing.T) {
	requireE2ETools(t)
	workDir := t.TempDir() // only for the marker path; the session gets its own
	marker := filepath.Join(workDir, "ready")
	s, _ := startDebugSession(t, fmt.Sprintf(`- name: flaky play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: flaky task
      shell: test -e %s && echo recovered

    - name: after task
      command: echo after
`, shellQuote(marker)), nil, "-i localhost,")

	s.waitFor("F5: run", 15*time.Second)
	s.send("F9")
	s.waitForFieldValue("flaky task", "●", 5*time.Second)
	s.send("F5")
	got := s.waitFor("Status: Failed", 20*time.Second)
	if !strings.Contains(got, "Task:   flaky task") {
		t.Errorf("expected the pause on the armed, failing task:\n%s", got)
	}
	if strings.Contains(got, "Playbook completed") || strings.Contains(got, "Playbook failed") {
		t.Errorf("a failure at an armed task should pause, not end the run:\n%s", got)
	}

	// Fix the cause, then redo that task/host. The tree's bar (not the
	// drill-down's) carries F12, so leave the drill-down first.
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s.send("Escape")
	s.waitFor("F12: redo", 5*time.Second)
	s.send("F12")
	got = s.waitFor("Status: Changed", 20*time.Second)
	if !strings.Contains(got, "Task:   flaky task") {
		t.Errorf("redo should re-pause on the same task:\n%s", got)
	}
	if !strings.Contains(got, "recovered") {
		t.Errorf("the redone task's result should show its new stdout:\n%s", got)
	}

	s.send("F5")
	done := s.waitFor("Playbook completed successfully", 20*time.Second)
	if recap := lineContaining(done, "localhost :"); !strings.Contains(recap, "failed=0") || !strings.Contains(recap, "changed=2") {
		t.Errorf("recap should count the redone task as changed, not failed: %q\n%s", recap, done)
	}
}

// TestE2E_Debug_WatchSetVariableThenRedo covers `v`, the variable watch
// explorer: watch a variable of the paused task, set a new value with
// `expr = value` (only valid at the live pause), then F12 makes that value
// reach the redone task - the scenario DebugMode.md's Stage 5 section
// verified by hand: a set_fact'd variable feeding a failed_when.
func TestE2E_Debug_WatchSetVariableThenRedo(t *testing.T) {
	requireE2ETools(t)
	s, _ := startDebugSession(t, `- name: watch play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: prepare
      set_fact:
        outcome_var: bad

    - name: judged task
      command: echo judged
      failed_when: outcome_var != 'good'
`, nil, "-i localhost,")

	s.waitFor("F5: run", 15*time.Second)
	s.send("Down") // prepare -> judged task
	s.send("F9")
	s.waitForFieldValue("judged task", "●", 5*time.Second)
	s.send("F5")
	s.waitFor("Status: Failed", 20*time.Second)
	s.send("Escape")
	s.waitFor("F12: redo", 5*time.Second)

	// Watch it: the explorer lists the variable's live value.
	s.send("v")
	s.waitFor("Type variable name", 5*time.Second)
	s.send("outcome_var")
	s.waitFor("outcome_var = \"bad\"", 5*time.Second) // the autocomplete entry
	s.send("Enter")
	s.waitFor("Watched variables", 5*time.Second)
	got := s.waitFor("outcome_var = \"bad\"", 5*time.Second)
	if strings.Contains(got, "just set") {
		t.Errorf("a merely watched variable must not be marked as set:\n%s", got)
	}

	// Set it at the live pause; the row shows the live value, marked.
	s.send("outcome_var = 'good'")
	s.send("Enter")
	s.waitFor("outcome_var = \"good\" (just set)", 10*time.Second)

	// Redo: the task now succeeds, and the mark is gone with the new pause.
	s.send("Escape")
	s.waitFor("F12: redo", 5*time.Second)
	s.send("F12")
	got = s.waitFor("Status: Changed", 20*time.Second)
	if strings.Contains(got, "(just set)") {
		t.Errorf("the 'just set' mark should clear once the pause changes:\n%s", got)
	}

	s.send("F5")
	done := s.waitFor("Playbook completed successfully", 20*time.Second)
	if recap := lineContaining(done, "localhost :"); !strings.Contains(recap, "failed=0") {
		t.Errorf("recap should show no failure after the set + redo: %q\n%s", recap, done)
	}
}

// TestE2E_Debug_WatchSetReachesTaskArguments is the counterpart of the test
// above for a variable used in a task's *arguments* (DebugMode.md, "Stage 5
// follow-up: a set didn't reach the task's arguments"): a plain redo re-runs
// the already-templated arguments, so this only passes because F12 re-
// templates the task first. The variable is a play-level vars: entry. The
// later task pins down that a set only ever affects the paused task.
func TestE2E_Debug_WatchSetReachesTaskArguments(t *testing.T) {
	requireE2ETools(t)
	s, _ := startDebugSession(t, `- name: args play
  hosts: localhost
  gather_facts: false
  connection: local
  vars:
    greeting: hello
  tasks:
    - name: echo task
      command: echo {{ greeting }}

    - name: later task
      command: echo later-{{ greeting }}
`, nil, "-i localhost,")

	s.waitFor("F5: run", 15*time.Second)
	s.send("F9") // the initial selection is the first task
	s.waitForFieldValue("echo task", "●", 5*time.Second)
	s.send("F5")
	s.waitFor("Task:   echo task", 20*time.Second)
	s.send("Escape")
	s.waitFor("F12: redo", 5*time.Second)

	s.send("v")
	s.waitFor("Type variable name", 5*time.Second)
	s.send("greeting = 'world'")
	s.send("Enter")
	s.waitFor("greeting = \"world\" (just set)", 10*time.Second)
	s.send("Escape")
	s.waitFor("F12: redo", 5*time.Second)

	// The tree row carries the task's own stdout: "Changed (<stdout>; ...)".
	s.send("F12")
	s.waitFor("Task:   echo task", 20*time.Second)
	s.send("Escape")
	got := s.waitFor("F12: redo", 5*time.Second)
	if row := lineContaining(got, "Changed ("); !strings.Contains(row, "Changed (world;") {
		t.Errorf("the redone task should have echoed the set value %q, got row %q:\n%s", "world", row, got)
	}

	// F11 steps on to the later task, which must see the original value.
	s.send("F11")
	got = s.waitFor("Task:   later task", 20*time.Second)
	if !strings.Contains(got, "later-hello") {
		t.Errorf("a set must not reach later tasks (want later-hello):\n%s", got)
	}
}

// TestE2E_Debug_UnarmedFailureEndsTheRun is the counterpart of the failing-
// task test above: with no breakpoint on it, a failing task does not pause
// (DebugMode.md: a failed task only pauses if armed) - the run just fails
// like a plain ansible-playbook run would, and the debug keys switch off.
func TestE2E_Debug_UnarmedFailureEndsTheRun(t *testing.T) {
	requireE2ETools(t)
	s, _ := startDebugSession(t, `- name: failing play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: doomed task
      command: "false"

    - name: never reached
      command: echo unreachable
`, nil, "-i localhost,")

	s.waitFor("F5: run", 15*time.Second)
	s.send("F5")
	got := s.waitFor("F5/F9/F10/F11/F12 disabled", 20*time.Second)
	if strings.Contains(got, "F12: redo") {
		t.Errorf("an unarmed failure must not leave a pause to redo:\n%s", got)
	}
	if recap := lineContaining(got, "localhost :"); !strings.Contains(recap, "failed=1") {
		t.Errorf("recap should count the failed task: %q\n%s", recap, got)
	}
}
