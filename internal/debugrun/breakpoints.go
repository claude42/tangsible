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

import "sync"

// BreakpointSet is the client-side policy design-docs/DebugMode.md's
// "Step / Run can be the same mechanism, decided client-side" refinement
// relies on: Session's own uniform debugger:-always splice means every
// task produces a Pause, and this type decides, for each one, whether it's
// a real breakpoint to surface to the user or something to silently
// Continue past.
//
// Breakpoints are keyed by the debugger's own task label (Pause.Task, e.g.
// "TASK: some task name") - the only identity a Pause carries; Session
// never sees the source location that identifies a specific tree node.
// Two tasks sharing an exact name alias to the same breakpoint; whether
// that matters, and how to disambiguate it, is left to whatever builds the
// picker UI on top of this (M3).
//
// Safe for concurrent use: breakpoints are meant to be toggled live, from
// a different goroutine than whatever is blocked driving the session.
type BreakpointSet struct {
	mu  sync.Mutex
	set map[string]bool
}

// NewBreakpointSet returns an empty set - every pause auto-continues until
// at least one breakpoint is armed.
func NewBreakpointSet() *BreakpointSet {
	return &BreakpointSet{set: make(map[string]bool)}
}

// Set arms a breakpoint on the given task label.
func (b *BreakpointSet) Set(task string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.set[task] = true
}

// Clear disarms a breakpoint on the given task label. Clearing one that
// isn't armed is a no-op.
func (b *BreakpointSet) Clear(task string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.set, task)
}

// Has reports whether the given task label is currently armed.
func (b *BreakpointSet) Has(task string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.set[task]
}

// StepMode picks which pauses RunWithBreakpoints/ContinueWithBreakpoints
// surface to the caller.
type StepMode int

const (
	// RunTillBreakpoint auto-continues through every pause whose task
	// isn't armed in the BreakpointSet, surfacing only an armed one (or
	// eof). With an empty set this runs the whole session to completion -
	// the same outcome ansible-playbook's own --step flag gives up on
	// stepping for (see design-docs/DebugMode.md's --step discussion),
	// except here it falls out of the same mechanism as every other mode
	// instead of needing a separate, non-re-armable CLI flag.
	RunTillBreakpoint StepMode = iota
	// StepOne surfaces the very next pause unconditionally, armed or not.
	// This is "Step"/"Step Into" from design-docs/DebugMode.md's vision:
	// both fall out for free here, since Ansible already flattens
	// includes/roles/blocks into the same sequential pause stream Session
	// exposes - there's no separate "step into" primitive to build.
	StepOne
	// StepNextTask auto-continues through every further pause that shares
	// ContinueWithBreakpoints' own fromTask (i.e. every other host still
	// to come for the task the caller is currently paused at), surfacing
	// the first pause for a *different* task unconditionally, armed or
	// not - "skip the rest of this task's hosts, stop at the next task's
	// first one" (reported live: StepOne's own "every pause, armed or
	// not" made stepping through a many-host task tedious when only the
	// next task actually mattered). Meaningless as RunWithBreakpoints' own
	// mode (fromTask is always "" there - nothing is paused yet, so
	// there's no task to skip past) - falls back to behaving exactly like
	// StepOne in that case, via the same fromTask-mismatch check below
	// (pause.Task is never the empty string).
	StepNextTask
)

// RunWithBreakpoints starts sess and applies mode/bps's policy from its
// very first pause onward, returning either a surfaced Pause or eof.
// stopRequested, if non-nil, is polled once per pause (see applyPolicy's
// own doc comment) - design-docs/DebugMode.md's "q's own soft stop while
// actively driving": q, while this call is in flight, requests that the
// very next pause surface unconditionally instead of being auto-continued
// past, without ever touching the real child process. onPause, if
// non-nil, is called for *every* pause reached along the way
// - including one silently resumed past under RunTillBreakpoint, never
// otherwise visible to a caller at all - before the policy decides
// whether to surface it or continue past it; its own willSurface
// parameter is that exact decision (true for the pause this call is
// about to return), computed and passed down rather than left for onPause
// to recompute itself - design-docs/DebugMode.md's "Resolved Variables
// tab, every task" - this is the only way a caller can ever query a
// pause's own task_vars (Session.Query) for a task whose breakpoint isn't
// armed: once applyPolicy's own loop calls Continue again, that pause's
// data is gone for good, with nothing left to query it from. A caller
// that only wants that query paid for when a pause will actually be
// surfaced (design-docs/DebugMode.md's "vars at stops only" default, the
// --all-vars/-a flag opting out of it) reads willSurface to decide.
func RunWithBreakpoints(sess *Session, bps *BreakpointSet, mode StepMode, stopRequested func() bool, onPause func(Pause, bool)) (Pause, bool, error) {
	pause, eof, err := sess.Start()
	return applyPolicy(sess, bps, mode, "", pause, eof, err, stopRequested, onPause)
}

// ContinueWithBreakpoints resumes sess past its current pause and applies
// the same policy, auto-continuing through any further non-breakpoint
// pauses under RunTillBreakpoint. fromTask is the task label the caller
// is currently paused at (Pause.Task, e.g. "TASK: some task name") -
// meaningful only under StepNextTask, where it's what every further pause
// is compared against; every other mode ignores it. stopRequested/
// onPause: see RunWithBreakpoints.
func ContinueWithBreakpoints(sess *Session, bps *BreakpointSet, mode StepMode, fromTask string, stopRequested func() bool, onPause func(Pause, bool)) (Pause, bool, error) {
	pause, eof, err := sess.Continue()
	return applyPolicy(sess, bps, mode, fromTask, pause, eof, err, stopRequested, onPause)
}

// RedoUntilTarget resumes sess via its own Redo (not Continue), then -
// unlike a plain Continue/Step call - silently drives straight past any
// further pause for the *same* task but a *different* host (targetHost,
// targetTask identify the pause just redone, read by the caller before
// issuing the redo) via ordinary Continue calls, until the redone pause
// itself resurfaces or a pause for a genuinely different task arrives.
//
// This exists because Session.Redo's own next pause is NOT reliably a
// re-pause of the redone host at all: with another host's result for
// the same task already pending in the same batch, that one surfaces
// first (TestSession_Redo_MultiHostInterleaving, session_test.go) -
// reported live, twice, as confusing, unacceptable behavior: pressing
// redo on host A and being shown host B's own unrelated, never-redone
// pause instead, with no way to tell the two apart from the UI alone.
// Earlier rounds tried surfacing that interleaved pause plainly (an
// ordinary pause, nothing hidden) and separately disabling a second F12
// press until the redone one caught up - technically correct, but it
// didn't fix the actual complaint: the tree still visibly, confusingly
// jumped to a host the caller never asked to look at. This version
// doesn't show the interleaved pause to the caller at all: it only
// means the caller never gets a chance to interactively stop at that
// other host's own breakpoint pause *this one time* (an ordinary,
// future Continue/Step call would still stop there as usual, armed
// breakpoint or not - this only ever affects the one pause being driven
// past while chasing a specific redo). onPause still fires for every
// pause encountered (so task_vars can still be cached for it under
// --all-vars, same as any other not-shown-to-the-caller pause) -
// willSurface is true only for the one ultimately returned (a genuine
// re-pause of targetTask/targetHost, or a different task entirely if the
// target was somehow never seen again - e.g. consumed some other way -
// false for every interleaved one silently driven past.
func RedoUntilTarget(sess *Session, targetTask, targetHost string, onPause func(Pause, bool)) (Pause, bool, error) {
	pause, eof, err := sess.Redo()
	for {
		if err != nil || eof {
			return pause, eof, err
		}
		if pause.Task == targetTask && pause.Host == targetHost || pause.Task != targetTask {
			if onPause != nil {
				onPause(pause, true)
			}
			return pause, eof, err
		}
		if onPause != nil {
			onPause(pause, false)
		}
		pause, eof, err = sess.Continue()
	}
}

// applyPolicy is the loop both entry points share: under StepOne the very
// first pause is always returned as-is; under RunTillBreakpoint, pauses
// not armed in bps are silently resumed past until an armed one, eof, or
// an error is reached; under StepNextTask, pauses whose task still
// matches fromTask are resumed past the same way, until one for a
// different task arrives. stopRequested, if non-nil, is called once per
// loop iteration, unconditionally, before willSurface is computed -
// design-docs/DebugMode.md's "q's own soft stop while actively driving":
// the real implementation (liveSession.debugConsumeStopRequest) is itself
// a check-and-clear, so a true result here is consumed the instant it's
// observed, regardless of whether bps/mode would also have independently
// surfaced this exact same pause - there's no attempt to distinguish "the
// stop caused this" from "something else also would have," which is both
// simpler and still matches the decided contract ("the very next pause
// surfaces, once honored it's consumed"). This also covers a stop
// requested before this call's very first pause has even arrived, since
// the first loop iteration already checks it. onPause fires for every
// genuine pause (err == nil, !eof) reached along the way, surfaced or
// not, before the surface/continue decision - called synchronously, on
// this same goroutine, since the whole point is to let a caller query the
// REPL (Session.Query) while this exact pause is still the one it's
// sitting at. willSurface is computed once and reused for both onPause's
// own parameter and the return decision below, so the two can never
// disagree.
func applyPolicy(sess *Session, bps *BreakpointSet, mode StepMode, fromTask string, pause Pause, eof bool, err error, stopRequested func() bool, onPause func(Pause, bool)) (Pause, bool, error) {
	for {
		willSurface := (stopRequested != nil && stopRequested()) || mode == StepOne || bps.Has(pause.Task) ||
			(mode == StepNextTask && pause.Task != fromTask)
		if err == nil && !eof && onPause != nil {
			onPause(pause, willSurface)
		}
		if err != nil || eof || willSurface {
			return pause, eof, err
		}
		pause, eof, err = sess.Continue()
	}
}
