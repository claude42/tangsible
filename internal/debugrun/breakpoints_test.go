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

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil, nil)
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

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil, nil)
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
	pause, eof, err = ContinueWithBreakpoints(sess, bps, RunTillBreakpoint, "", nil, nil)
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
	pause, eof, err = ContinueWithBreakpoints(sess, bps, RunTillBreakpoint, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eof {
		t.Errorf("expected eof after the unarmed task three, got pause %+v", pause)
	}
}

// TestBreakpoints_StopRequested_SurfacesNextPauseEvenWithNothingArmed is
// design-docs/DebugMode.md's "q's own soft stop while actively driving":
// a one-shot stopRequested closure, with bps deliberately empty, should
// surface the very next pause unconditionally (like a single step)
// instead of RunTillBreakpoint's own silent auto-continue - and, once
// consumed, never fire again.
func TestBreakpoints_StopRequested_SurfacesNextPauseEvenWithNothingArmed(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet()

	stopped := false
	stopRequested := func() bool {
		if !stopped {
			stopped = true
			return true
		}
		return false
	}

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, stopRequested, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof || pause.Task != "TASK: task one" {
		t.Fatalf("expected the soft stop to surface the very first pause, got pause=%+v eof=%v", pause, eof)
	}

	// The one-shot request is already consumed - with bps still empty,
	// this must now run straight to eof.
	_, eof, err = ContinueWithBreakpoints(sess, bps, RunTillBreakpoint, "", stopRequested, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eof {
		t.Fatal("expected eof - the one-shot stop request was already consumed")
	}
}

// TestBreakpoints_StopRequested_ConsumedEvenWhenBreakpointAlsoFires pins
// applyPolicy's own documented "no attempt to distinguish the reason"
// rule: stopRequested and an armed breakpoint both apply to the very same
// pause here - it's still called (and consumed) exactly once, not skipped
// just because bps.Has alone would already have surfaced that pause.
func TestBreakpoints_StopRequested_ConsumedEvenWhenBreakpointAlsoFires(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet()
	bps.Set("TASK: task one")

	stopped := false
	stopCalls := 0
	stopRequested := func() bool {
		stopCalls++
		if !stopped {
			stopped = true
			return true
		}
		return false
	}

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, stopRequested, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof || pause.Task != "TASK: task one" {
		t.Fatalf("expected the armed first pause, got pause=%+v eof=%v", pause, eof)
	}
	if stopCalls != 1 {
		t.Fatalf("stopRequested called %d times, want exactly 1", stopCalls)
	}
	if !stopped {
		t.Fatal("expected stopRequested to be consumed even though the armed breakpoint alone would also have surfaced this pause")
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
	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil, func(p Pause, willSurface bool) {
		seen = append(seen, p)
		if willSurface {
			t.Errorf("onPause got willSurface=true for %+v, want false - bps is empty and mode is RunTillBreakpoint", p)
		}
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

// TestBreakpoints_OnPause_WillSurfaceTrueForArmedBreakpoint confirms
// willSurface agrees with the actual surface/continue decision in the
// one case TestBreakpoints_OnPauseFiresForEverySilentlyContinuedPause
// doesn't cover: a pause whose task *is* armed - the pause onPause is
// called for last, and the one RunWithBreakpoints itself then returns.
func TestBreakpoints_OnPause_WillSurfaceTrueForArmedBreakpoint(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet()
	bps.Set("TASK: task two")

	var willSurfaceSeen []bool
	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil, func(p Pause, willSurface bool) {
		willSurfaceSeen = append(willSurfaceSeen, willSurface)
	})
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatalf("expected a surfaced pause at the armed breakpoint, got eof")
	}
	if pause.Task != "TASK: task two" {
		t.Fatalf("surfaced pause = %+v, want TASK: task two", pause)
	}
	if len(willSurfaceSeen) == 0 || !willSurfaceSeen[len(willSurfaceSeen)-1] {
		t.Fatalf("willSurface for the final (returned) pause = %v, want true: %v", willSurfaceSeen[len(willSurfaceSeen)-1], willSurfaceSeen)
	}
	for _, v := range willSurfaceSeen[:len(willSurfaceSeen)-1] {
		if v {
			t.Errorf("willSurface = true for a pause before the armed one, want false: %v", willSurfaceSeen)
		}
	}
}

// TestBreakpoints_StepNextTask_SkipsRemainingHostsOfCurrentTask is F10's
// own regression test (reported live: F10/StepOne stepped through every
// remaining host of the *current* task one at a time, tedious when only
// the next task actually mattered) - confirms StepNextTask auto-
// continues past every further pause for fromTask (the task the caller
// is already paused at), surfacing unconditionally the instant a
// different task's pause arrives, regardless of the (deliberately empty)
// breakpoint set.
func TestBreakpoints_StepNextTask_SkipsRemainingHostsOfCurrentTask(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet() // deliberately empty - StepNextTask ignores it

	// RunWithBreakpoints has no "current task" to skip past yet - falls
	// back to StepOne's own behavior, surfacing the very first pause.
	pause, eof, err := RunWithBreakpoints(sess, bps, StepNextTask, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected the first pause, got eof")
	}
	if pause.Task != "TASK: task one" {
		t.Fatalf("first pause = %+v, want task one", pause)
	}

	// Paused at task one/host1 - StepNextTask should skip task one's
	// second host entirely and land directly on task two's first host.
	pause, eof, err = ContinueWithBreakpoints(sess, bps, StepNextTask, pause.Task, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected task two's own pause, got eof")
	}
	if pause.Task != "TASK: task two" {
		t.Fatalf("pause after StepNextTask = %+v, want task two (task one's second host skipped)", pause)
	}

	// And again, from task two to task three.
	pause, eof, err = ContinueWithBreakpoints(sess, bps, StepNextTask, pause.Task, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected task three's own pause, got eof")
	}
	if pause.Task != "TASK: task three" {
		t.Fatalf("pause after second StepNextTask = %+v, want task three", pause)
	}

	// task three is the last task - StepNextTask past it runs to eof,
	// same as StepOne/RunTillBreakpoint would with nothing left armed.
	pause, eof, err = ContinueWithBreakpoints(sess, bps, StepNextTask, pause.Task, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eof {
		t.Errorf("expected eof after task three's last host, got pause %+v", pause)
	}
}

func TestBreakpoints_StepOne_SurfacesEveryPauseRegardlessOfSet(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet() // deliberately empty - StepOne ignores it

	var pauses []Pause
	pause, eof, err := RunWithBreakpoints(sess, bps, StepOne, nil, nil)
	for !eof {
		if err != nil {
			t.Fatal(err)
		}
		pauses = append(pauses, pause)
		pause, eof, err = ContinueWithBreakpoints(sess, bps, StepOne, "", nil, nil)
	}
	if err != nil {
		t.Fatal(err)
	}

	if len(pauses) != 6 { // 3 tasks x 2 hosts, matching TestSession_LiveDualChannel
		t.Fatalf("expected 6 single-stepped pauses, got %d: %+v", len(pauses), pauses)
	}
}

// TestRedoUntilTarget_DrainsInterleavedPauseSilently is the real
// regression test for the "redo jumped to an unrelated host" complaint:
// confirms RedoUntilTarget's whole reason to exist - when another host's
// own (never redone) pause for the same task is already pending (forks=1
// with two hosts, same as TestSession_Redo_MultiHostInterleaving -
// reliably reproduces the interleaving), it's silently driven past
// (onPause called for it with willSurface=false - no reason for a caller
// to query/cache it the way a shown pause would be) rather than returned
// to the caller, and the function only returns once the exact (task,
// host) pair just redone resurfaces.
func TestRedoUntilTarget_DrainsInterleavedPauseSilently(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	sess := spawnFixture(t, threeTaskTwoHostFixture(t))
	bps := NewBreakpointSet()
	bps.Set("TASK: task one")

	pause, eof, err := RunWithBreakpoints(sess, bps, RunTillBreakpoint, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected the first pause, got eof")
	}
	if pause.Task != "TASK: task one" || pause.Host != "host1" {
		t.Fatalf("first pause = %+v, want task one/host1 (forks=1)", pause)
	}

	var seen []Pause
	var willSurfaceSeen []bool
	redone, eof, err := RedoUntilTarget(sess, pause.Task, pause.Host, func(p Pause, willSurface bool) {
		seen = append(seen, p)
		willSurfaceSeen = append(willSurfaceSeen, willSurface)
	})
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected the redone pause, got eof")
	}
	if redone.Task != pause.Task || redone.Host != pause.Host {
		t.Fatalf("RedoUntilTarget returned %+v, want the exact target %+v - the interleaved host2 pause should have been drained silently, not returned", redone, pause)
	}

	if len(seen) != 2 {
		t.Fatalf("onPause fired %d times, want exactly 2 (the drained host2 pause, then the redone host1 one): %+v", len(seen), seen)
	}
	if seen[0].Host != "host2" || willSurfaceSeen[0] {
		t.Errorf("first onPause call = %+v (willSurface=%v), want host2 with willSurface=false (drained silently)", seen[0], willSurfaceSeen[0])
	}
	if seen[1].Host != "host1" || !willSurfaceSeen[1] {
		t.Errorf("second onPause call = %+v (willSurface=%v), want host1 with willSurface=true (the actual redo result)", seen[1], willSurfaceSeen[1])
	}
}

// TestRedoUntilTarget_NoInterleaving_ReturnsImmediately confirms the
// common case (nothing else pending) needs no draining at all - onPause
// fires exactly once, for the redone pause itself.
func TestRedoUntilTarget_NoInterleaving_ReturnsImmediately(t *testing.T) {
	requireAnsiblePlaybook(t)
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot(t), "callback"))

	// A single-host fixture - never anything else to interleave with.
	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(`- hosts: all
  gather_facts: false
  tasks:
    - name: only task
      command: echo hi
`), 0o644); err != nil {
		t.Fatal(err)
	}
	locs := source.ListTaskLocations(playbookPath)
	copyDir, cleanup, err := debugcopy.BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := debugcopy.SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	inventory := filepath.Join(repoRoot(t), "testdata", "multihost-inventory.ini")
	sess, err := Spawn(filepath.Join(copyDir, "playbook.yml"), []string{"-i", inventory, "--limit", "host1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() })
	go func() {
		for range sess.Events {
		}
	}()

	pause, eof, err := sess.Start()
	if err != nil || eof {
		t.Fatalf("Start: pause=%+v eof=%v err=%v", pause, eof, err)
	}

	var seen []Pause
	redone, eof, err := RedoUntilTarget(sess, pause.Task, pause.Host, func(p Pause, willSurface bool) {
		seen = append(seen, p)
		if !willSurface {
			t.Errorf("onPause got willSurface=false for %+v, want true - nothing should need draining here", p)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if eof {
		t.Fatal("expected the redone pause, got eof")
	}
	if redone.Task != pause.Task || redone.Host != pause.Host {
		t.Errorf("redo pause = %+v, want the exact same task/host as %+v", redone, pause)
	}
	if len(seen) != 1 {
		t.Fatalf("onPause fired %d times, want exactly 1: %+v", len(seen), seen)
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
