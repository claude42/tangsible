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

// The matching engine behind design-docs/Diff.md: aligning two independent
// PlaybookState trees (an "old" run and a "new" run) so a diff tree/drill-
// down can be built from the result. Pure - no I/O, no UI - so it's usable
// identically whichever of the two sides happens to be live/replayed data.
package main

import (
	"encoding/json"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/pmezard/go-difflib/difflib"
)

// taskAlignment pairs up a task from an "old" run with its counterpart in
// a "new" run. Exactly one of OldTask/NewTask is nil for a task that only
// exists on one side (added/removed since the old run, or moved into/out
// of a play that itself only exists on one side - see playAlignment);
// both set for a matched pair, further comparable via taskDiffers.
type taskAlignment struct {
	OldTask, NewTask *playbook.TaskNode
}

// alignTasks aligns oldPlay's and newPlay's own task sequences by name,
// via difflib.SequenceMatcher (already a dependency - the same library
// BuildDiffTab already uses for line-level diffs, reused here for name-
// level sequence alignment instead of a custom LCS implementation).
//
// Matching by name rather than by task.Path deliberately: editing the
// playbook between debug runs (the whole reason this feature exists)
// shifts line numbers for everything below the edit, even for tasks that
// didn't change themselves - path-matching would misread that as "this
// task no longer exists" and cascade into spurious mismatches for
// everything after the edit point. Worse, a role-originated session's own
// task.Path points at a freshly generated stub every single time
// (startRoleSession) - path-matching would never match anything at all
// for a role diff, even with zero real changes. Name-based matching risks
// the opposite failure - two same-named tasks in one play misaligning -
// but SequenceMatcher's own alignment leans on surrounding position, not
// just raw equality, so it tends to do the sensible thing even then: same
// "documented heuristic, not chased further" tradeoff this codebase
// already makes elsewhere (TaskLabel's own truncation, PrimaryOutputField's
// stdout-vs-msg choice).
//
// oldPlay/newPlay may themselves be nil - the play that owns them only
// exists on one side (see alignPlays). Every task on the present side
// then becomes old-only/new-only uniformly, via the exact same 'i'/'d'
// opcode handling below - no special-casing needed here for that case.
func alignTasks(oldPlay, newPlay *playbook.PlayNode) []taskAlignment {
	var oldTasks, newTasks []*playbook.TaskNode
	if oldPlay != nil {
		oldTasks = oldPlay.Tasks
	}
	if newPlay != nil {
		newTasks = newPlay.Tasks
	}

	var alignments []taskAlignment
	for _, op := range difflib.NewMatcher(taskNames(oldTasks), taskNames(newTasks)).GetOpCodes() {
		switch op.Tag {
		case 'e':
			for k := 0; k < op.I2-op.I1; k++ {
				alignments = append(alignments, taskAlignment{OldTask: oldTasks[op.I1+k], NewTask: newTasks[op.J1+k]})
			}
		case 'd':
			for _, t := range oldTasks[op.I1:op.I2] {
				alignments = append(alignments, taskAlignment{OldTask: t})
			}
		case 'i':
			for _, t := range newTasks[op.J1:op.J2] {
				alignments = append(alignments, taskAlignment{NewTask: t})
			}
		case 'r':
			// Deliberately NOT paired up despite sharing a position - see
			// this function's own doc comment: correctness (never
			// claiming two differently-named tasks "are the same task,
			// just changed") matters more than trying to be clever about
			// same-position replacements.
			for _, t := range oldTasks[op.I1:op.I2] {
				alignments = append(alignments, taskAlignment{OldTask: t})
			}
			for _, t := range newTasks[op.J1:op.J2] {
				alignments = append(alignments, taskAlignment{NewTask: t})
			}
		}
	}
	return alignments
}

func taskNames(tasks []*playbook.TaskNode) []string {
	names := make([]string, len(tasks))
	for i, t := range tasks {
		names[i] = t.Name
	}
	return names
}

// playAlignment pairs up a play from an "old" run with its counterpart in
// a "new" run, plus its own tasks' alignment (alignTasks). Exactly one of
// OldPlay/NewPlay is nil for a play that only exists on one side.
type playAlignment struct {
	OldPlay, NewPlay *playbook.PlayNode
	Tasks            []taskAlignment
}

// alignPlays is alignTasks's own sibling, one level up: plays matched by
// name across the whole playbook, same algorithm, same reasoning. A whole
// play only existing on one side needs no special handling of its own
// here - every one of its own tasks becomes old-only/new-only via
// alignTasks(nil, play)/alignTasks(play, nil), which is exactly what makes
// it "contain tasks with differences" and show up once rendered, per
// design-docs/Diff.md - see playAlignmentHasDifferences below.
func alignPlays(oldState, newState *playbook.PlaybookState) []playAlignment {
	oldPlays := oldState.Plays
	newPlays := newState.Plays

	var alignments []playAlignment
	for _, op := range difflib.NewMatcher(playNames(oldPlays), playNames(newPlays)).GetOpCodes() {
		switch op.Tag {
		case 'e':
			for k := 0; k < op.I2-op.I1; k++ {
				o, n := oldPlays[op.I1+k], newPlays[op.J1+k]
				alignments = append(alignments, playAlignment{OldPlay: o, NewPlay: n, Tasks: alignTasks(o, n)})
			}
		case 'd':
			for _, p := range oldPlays[op.I1:op.I2] {
				alignments = append(alignments, playAlignment{OldPlay: p, Tasks: alignTasks(p, nil)})
			}
		case 'i':
			for _, p := range newPlays[op.J1:op.J2] {
				alignments = append(alignments, playAlignment{NewPlay: p, Tasks: alignTasks(nil, p)})
			}
		case 'r':
			for _, p := range oldPlays[op.I1:op.I2] {
				alignments = append(alignments, playAlignment{OldPlay: p, Tasks: alignTasks(p, nil)})
			}
			for _, p := range newPlays[op.J1:op.J2] {
				alignments = append(alignments, playAlignment{NewPlay: p, Tasks: alignTasks(nil, p)})
			}
		}
	}
	return alignments
}

func playNames(plays []*playbook.PlayNode) []string {
	names := make([]string, len(plays))
	for i, p := range plays {
		names[i] = p.Name
	}
	return names
}

// taskDiffers reports whether a matched pair (both OldTask/NewTask set -
// see taskAlignment's own doc comment) counts as "different"
// (design-docs/Diff.md's own "What counts as different" section): any
// host present in *both* tasks' own Hosts map has a different outcome, or
// different output (hostOutputDiffers below). A host present on only one
// side is skipped entirely - per design-docs/Diff.md's own "wouldn't
// count a difference in hosts... as a difference." Since display always
// follows the *new* task's own HostOrder (design-docs/Diff.md's "render
// based on the new version"), a host that only existed on the old side
// simply never appears in diff mode at all - nothing further needed for
// that to fall out correctly.
//
// Always true for an unmatched alignment (OldTask or NewTask nil, by
// construction always "different") - callers that already know an
// alignment is unmatched don't need to call this at all, but it's safe to
// either way.
func taskDiffers(a taskAlignment) bool {
	if a.OldTask == nil || a.NewTask == nil {
		return true
	}
	for host := range a.NewTask.Hosts {
		if hostDiffers(a.OldTask, a.NewTask, host) {
			return true
		}
	}
	return false
}

// hostDiffers reports whether host's own result differs between oldTask
// and newTask (outcome or output) - taskDiffers' own per-host building
// block, and diff.go's own row rendering reuses it too, to decide which
// specific hosts get underlined on a matched, differing task's row
// (design-docs/Diff.md's "underline those hosts that are different").
// false whenever host isn't recorded on both sides - same "host-set
// differences don't count" rule taskDiffers itself follows.
func hostDiffers(oldTask, newTask *playbook.TaskNode, host string) bool {
	oldOutcome, ok := oldTask.Hosts[host]
	if !ok {
		return false
	}
	newOutcome, ok := newTask.Hosts[host]
	if !ok {
		return false
	}
	if oldOutcome != newOutcome {
		return true
	}
	return hostOutputDiffers(oldTask, newTask, host)
}

// differingHosts returns the set of hosts (out of NewTask.Hosts) that
// hostDiffers reports as different for a *matched* alignment - nil for an
// unmatched one (OldTask or NewTask nil), since there's no per-host
// comparison to make there; the whole row is marked instead (see
// diff.go's own diffTaskRowText).
func differingHosts(a taskAlignment) map[string]bool {
	if a.OldTask == nil || a.NewTask == nil {
		return nil
	}
	diff := map[string]bool{}
	for host := range a.NewTask.Hosts {
		if hostDiffers(a.OldTask, a.NewTask, host) {
			diff[host] = true
		}
	}
	return diff
}

// hostOutputDiffers compares host's own recorded output between oldTask
// and newTask - design-docs/Diff.md's "Different output (stdout, stderr,
// warning)" - via the exact same fields formatHostOutput already treats
// as distinct sections (PrimaryOutputField's own stdout-vs-msg choice,
// stderr, warnings), decoded from the same Raw[host] JSON both tasks
// already carry. A host missing from either side's own Raw (shouldn't
// happen for a host taskDiffers has already confirmed is present in both
// Hosts maps, but not trusted blindly) decodes to "", comparing equal to
// itself rather than panicking.
func hostOutputDiffers(oldTask, newTask *playbook.TaskNode, host string) bool {
	oldOutput, oldStderr, oldWarnings := hostOutputSignature(oldTask.Raw[host])
	newOutput, newStderr, newWarnings := hostOutputSignature(newTask.Raw[host])
	return oldOutput != newOutput || oldStderr != newStderr || oldWarnings != newWarnings
}

// hostOutputSignature decodes raw once and extracts the three fields
// hostOutputDiffers compares, so a task/host pair's raw JSON is only ever
// decoded once per side rather than once per compared field.
func hostOutputSignature(raw json.RawMessage) (output, stderr, warnings string) {
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", "", ""
	}
	_, output = uikit.PrimaryOutputField(decoded)
	stderr, _ = decoded["stderr"].(string)
	warnings = uikit.JoinedStringList(decoded["warnings"], "\n")
	return output, stderr, warnings
}

// playAlignmentHasDifferences reports whether pa "contains tasks with
// differences" (design-docs/Diff.md) - true if any of its own task
// alignments differ (taskDiffers). A play that only exists on one side
// needs no special case here: every one of its tasks is already an
// unmatched alignment (see alignPlays/alignTasks), and taskDiffers is
// always true for those - a play can't reach zero tasks in the first
// place (aggregate.go's own Apply only ever creates a PlayNode once its
// first task starts), so there's always at least one to make this true.
func playAlignmentHasDifferences(pa playAlignment) bool {
	for _, ta := range pa.Tasks {
		if taskDiffers(ta) {
			return true
		}
	}
	return false
}
