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
	"os"
	"path/filepath"
	"testing"

	"code.aw.net/claude/tangsible/internal/debugcopy"
	"code.aw.net/claude/tangsible/internal/source"
)

// threeTaskTwoHostFixture lays out the same 3-task/2-host playbook
// TestSession_LiveDualChannel already uses, spliced and ready to Spawn -
// shared here so the breakpoint-policy tests don't duplicate it.
func threeTaskTwoHostFixture(t *testing.T) (copyDir string) {
	t.Helper()
	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: task one
      command: echo one

    - name: task two
      command: echo two

    - name: task three
      command: echo three
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
	t.Cleanup(cleanup)

	if err := debugcopy.SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}
	return copyDir
}

func spawnFixture(t *testing.T, copyDir string) *Session {
	t.Helper()
	inventory := filepath.Join(repoRoot(t), "testdata", "multihost-inventory.ini")
	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", inventory, "--forks", "1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })

	// Drain Events in the background - Spawn's event pipe must be read or
	// a full pipe buffer would stall the child, same reasoning
	// TestSession_LiveDualChannel's own drain goroutine documents.
	go func() {
		for range sess.Events {
		}
	}()
	return sess
}

func TestBreakpoints_RunTillBreakpoint_EmptySetRunsToCompletion(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet()

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eof {
		t.Fatalf("expected eof with no breakpoints armed, got pause %+v", pause)
	}
}

func TestBreakpoints_RunTillBreakpoint_StopsOnlyAtArmedTask(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet()
	bps.Set("TASK: task two")

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected a breakpoint hit, got eof")
	}
	if pause.Task != "TASK: task two" {
		t.Errorf("first surfaced pause = %+v, want task two", pause)
	}

	// Both hosts pause on the armed task - the breakpoint is per task
	// label, not per host, so the second host's own pause on the same
	// task must also surface, not auto-continue past.
	pause, eof, err = ContinueWithBreakpoints(sess, bps, RunTillBreakpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected a second breakpoint hit (second host), got eof")
	}
	if pause.Task != "TASK: task two" {
		t.Errorf("second surfaced pause = %+v, want task two", pause)
	}

	// task three isn't armed - both hosts' pauses on it auto-continue,
	// running the rest of the playbook to completion in one call.
	pause, eof, err = ContinueWithBreakpoints(sess, bps, RunTillBreakpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eof {
		t.Errorf("expected eof after the unarmed task three, got pause %+v", pause)
	}
}

// TestBreakpoints_OnPauseFiresForEverySilentlyContinuedPause is design-
// docs/DebugMode.md's "Resolved Variables tab, every task": a caller
// needs to see (and query) a pause even when its own task isn't armed -
// RunTillBreakpoint's own silent Continue() loop would otherwise make
// that pause's data permanently unreachable the instant it moves past
// it. bps here is empty - every one of the three tasks' pauses auto-
// continues all the way to eof in one RunWithBreakpoints call - so
// onPause firing six times (3 tasks x 2 hosts, matching
// TestBreakpoints_StepOne_SurfacesEveryPauseRegardlessOfSet's own host
// count) is only possible if it's called from inside applyPolicy's own
// loop, not just once on the single value the function ultimately
// returns.
func TestBreakpoints_OnPauseFiresForEverySilentlyContinuedPause(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet() // deliberately empty

	var seen []Pause
	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, func(p Pause) {
		seen = append(seen, p)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !eof {
		t.Fatalf("expected eof with no breakpoints armed, got pause %+v", pause)
	}
	if len(seen) != 6 {
		t.Fatalf("onPause fired %d times, want 6 (3 tasks x 2 hosts): %+v", len(seen), seen)
	}
}

func TestBreakpoints_StepOne_SurfacesEveryPauseRegardlessOfSet(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet() // deliberately empty - StepOne ignores it

	var pauses []Pause
	pause, eof, err := RunWithBreakpoints(sess, bps, StepOne, nil)
	for !eof {
		if err != nil {
			t.Fatal(err)
		}
		pauses = append(pauses, pause)
		pause, eof, err = ContinueWithBreakpoints(sess, bps, StepOne, nil)
	}
	if err != nil {
		t.Fatal(err)
	}

	if len(pauses) != 6 { // 3 tasks x 2 hosts, matching TestSession_LiveDualChannel
		t.Fatalf("expected 6 single-stepped pauses, got %d: %+v", len(pauses), pauses)
	}
}

func TestBreakpointSet_ClearDisarms(t *testing.T) {
	bps := NewBreakpointSet()
	bps.Set("TASK: x")
	if !bps.Has("TASK: x") {
		t.Fatal("expected breakpoint to be armed after Set")
	}
	bps.Clear("TASK: x")
	if bps.Has("TASK: x") {
		t.Fatal("expected breakpoint to be disarmed after Clear")
	}
	// Clearing something never armed is a no-op, not an error.
	bps.Clear("TASK: never armed")
}
