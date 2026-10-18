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
)

// RunWithBreakpoints starts sess and applies mode/bps's policy from its
// very first pause onward, returning either a surfaced Pause or eof.
// onPause, if non-nil, is called for *every* pause reached along the way
// - including one silently resumed past under RunTillBreakpoint, never
// otherwise visible to a caller at all - before the policy decides
// whether to surface it or continue past it. design-docs/DebugMode.md's
// "Resolved Variables tab, every task" - this is the only way a caller
// can ever query a pause's own task_vars (Session.Query) for a task
// whose breakpoint isn't armed: once applyPolicy's own loop calls
// Continue again, that pause's data is gone for good, with nothing left
// to query it from.
func RunWithBreakpoints(sess *Session, bps *BreakpointSet, mode StepMode, onPause func(Pause)) (Pause, bool, error) {
	pause, eof, err := sess.Start()
	return applyPolicy(sess, bps, mode, pause, eof, err, onPause)
}

// ContinueWithBreakpoints resumes sess past its current pause and applies
// the same policy, auto-continuing through any further non-breakpoint
// pauses under RunTillBreakpoint. onPause: see RunWithBreakpoints.
func ContinueWithBreakpoints(sess *Session, bps *BreakpointSet, mode StepMode, onPause func(Pause)) (Pause, bool, error) {
	pause, eof, err := sess.Continue()
	return applyPolicy(sess, bps, mode, pause, eof, err, onPause)
}

// applyPolicy is the loop both entry points share: under StepOne the very
// first pause is always returned as-is; under RunTillBreakpoint, pauses
// not armed in bps are silently resumed past until an armed one, eof, or
// an error is reached. onPause fires for every genuine pause (err == nil,
// !eof) reached along the way, surfaced or not, before the surface/
// continue decision - called synchronously, on this same goroutine,
// since the whole point is to let a caller query the REPL (Session.Query)
// while this exact pause is still the one it's sitting at.
func applyPolicy(sess *Session, bps *BreakpointSet, mode StepMode, pause Pause, eof bool, err error, onPause func(Pause)) (Pause, bool, error) {
	for {
		if err == nil && !eof && onPause != nil {
			onPause(pause)
		}
		if err != nil || eof || mode == StepOne || bps.Has(pause.Task) {
			return pause, eof, err
		}
		pause, eof, err = sess.Continue()
	}
}
