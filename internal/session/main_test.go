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

package session

import (
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/runner"
)

// TestInterruptChildAndWait_AlreadyDoneIsANoOp covers the common case at
// this function's own real call site: processDone already true (the
// generation finished before/while app.Run() itself was failing) means
// there's no child left to signal - must return immediately with no
// attempt to touch procH (a zero-value ProcHandle here, Load() would
// return nil).
func TestInterruptChildAndWait_AlreadyDoneIsANoOp(t *testing.T) {
	var procH runner.ProcHandle
	var processDone atomic.Bool
	processDone.Store(true)

	done := make(chan struct{})
	go func() {
		interruptChildAndWait(&procH, &processDone, time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("interruptChildAndWait() didn't return promptly when processDone was already true")
	}
}

// TestInterruptChildAndWait_NoProcessIsANoOp covers the other early-return
// case: no generation has ever spawned at all (procH.Load() == nil, e.g.
// the "rerun" verb's own dialog was never confirmed) - must not panic on a
// nil *os.Process.
func TestInterruptChildAndWait_NoProcessIsANoOp(t *testing.T) {
	var procH runner.ProcHandle
	var processDone atomic.Bool

	done := make(chan struct{})
	go func() {
		interruptChildAndWait(&procH, &processDone, time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("interruptChildAndWait() didn't return promptly with no process ever stored")
	}
}

// TestInterruptChildAndWait_SignalsAndReturnsOnceTheChildExits is the real
// path this function exists for: a still-running child gets os.Interrupt,
// and interruptChildAndWait returns as soon as processDone flips true -
// simulating exactly what RunOneGeneration itself does (cmd.Wait() then
// processDone.Store(true)) - well before the grace period would otherwise
// expire.
func TestInterruptChildAndWait_SignalsAndReturnsOnceTheChildExits(t *testing.T) {
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting test child: %v", err)
	}

	var procH runner.ProcHandle
	procH.Store(cmd.Process)
	var processDone atomic.Bool

	go func() {
		_ = cmd.Wait() // plain sleep(1) terminates on SIGINT with no handling needed
		processDone.Store(true)
	}()

	start := time.Now()
	interruptChildAndWait(&procH, &processDone, 5*time.Second)
	elapsed := time.Since(start)

	if !processDone.Load() {
		t.Error("processDone was never observed true")
	}
	if elapsed > 2*time.Second {
		t.Errorf("interruptChildAndWait() took %v, want it to return promptly once the child actually exited", elapsed)
	}
}

// TestInterruptChildAndWait_BoundedEvenIfTheChildNeverExits covers the
// other real-world outcome: the child ignores the signal entirely (a
// long-running task mid-execution, say) - interruptChildAndWait must
// still return once its own grace period elapses, never hang Tangsible's
// own exit forever waiting on a child that isn't going to cooperate.
func TestInterruptChildAndWait_BoundedEvenIfTheChildNeverExits(t *testing.T) {
	cmd := exec.Command("sh", "-c", `trap '' INT; while true; do sleep 0.05; done`)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting test child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	var procH runner.ProcHandle
	procH.Store(cmd.Process)
	var processDone atomic.Bool

	grace := 10 * interruptPollInterval
	start := time.Now()
	interruptChildAndWait(&procH, &processDone, grace)
	elapsed := time.Since(start)

	if processDone.Load() {
		t.Error("processDone became true even though the child was set up to ignore the signal")
	}
	// Allow some slack over the exact grace period - the poll loop only
	// checks every interruptPollInterval, not continuously.
	if elapsed < grace {
		t.Errorf("interruptChildAndWait() returned after %v, want at least the grace period %v", elapsed, grace)
	}
	if elapsed > grace+5*interruptPollInterval {
		t.Errorf("interruptChildAndWait() took %v, want close to the grace period %v", elapsed, grace)
	}
}
