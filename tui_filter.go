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

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// filterMode is one of the main tree's row filter kinds (Filters.md). The
// zero value, filterAll, is the default - no filtering at all.
type filterMode int

const (
	filterAll filterMode = iota
	filterChanged
	filterFailed
	filterSearch // Filters.md's "Contents"/M filter - see filterQuery.search
)

// filterQuery is the main tree's complete currently active filter: the
// kind, plus the search term filterSearch matches against (meaningless for
// the other three kinds). Kept as one comparable value (both fields are,
// so switching filters can be detected with a plain !=) rather than
// threading mode and search as two separate parameters through every
// function that needs "the current filter" - flattenRows, visibleTasks,
// visibleTasksForHost, taskVisible, applyFilter - since the two always
// travel together. sourceIndex (needed by taskVisible's filterSearch case
// to look up a task's source text) is deliberately NOT part of this type:
// it's a map, which isn't comparable, so it's threaded as its own
// parameter everywhere instead - the same parameter NewLiveTUI itself
// already receives and passes to formatHostOutput.
type filterQuery struct {
	mode   filterMode
	search string
}

// label is q's own display name, used by the top bar and the filter
// dialog.
func (q filterQuery) label() string {
	switch q.mode {
	case filterChanged:
		return "Changed"
	case filterFailed:
		return "Failed"
	case filterSearch:
		return fmt.Sprintf("Search: %q", q.search)
	default:
		return "All"
	}
}

// taskHasAnyOutcome reports whether any of t's hosts currently have one of
// the given outcomes.
func taskHasAnyOutcome(t *taskNode, outcomes ...outcome) bool {
	for _, o := range t.Hosts {
		for _, want := range outcomes {
			if o == want {
				return true
			}
		}
	}
	return false
}

// taskOutputText returns primaryOutputField's text for host's result on
// task, or "" if there's none (no result recorded yet, undecodable, or the
// module simply didn't report stdout/msg) - shared support for the search
// filter's "Output" criterion below, reusing exactly the same decode-then-
// primaryOutputField path outputSummary/formatHostOutput already use, so
// "the output" means the same thing everywhere in this app.
func taskOutputText(t *taskNode, host string) string {
	raw, ok := t.Raw[host]
	if !ok {
		return ""
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	_, text := primaryOutputField(decoded)
	return text
}

// taskAction returns host's own "action" result field on task - the
// module's name exactly as Ansible reported it (its FQCN when the task
// was written that way, e.g. "ansible.builtin.copy") - or "" if there's
// no result recorded yet, it's undecodable, or the result simply has no
// action field. Backs the Docs tab's ansible-doc lookup (showOutput),
// same decode-then-extract shape as taskOutputText above.
func taskAction(t *taskNode, host string) string {
	raw, ok := t.Raw[host]
	if !ok {
		return ""
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return ""
	}
	action, _ := decoded["action"].(string)
	return action
}

// taskMatchesSearch reports whether t matches the "Contents" search filter
// (Filters.md's "Contents"/M filter): term found in the task's own title,
// its source ("ansible command" - the same TASK: section source text
// formatHostOutput shows, from sourceIndex/source.go), or any host's
// Output (taskOutputText above - the same field the drill-down view and
// the collapsed OK/Changed line both already call "the output"). An empty
// term matches everything (same as filterAll) rather than hiding
// everything - a safer default than making an accidental blank search
// (pressing Enter on an empty text box) look like "nothing matches, is
// this broken?". Case-insensitive substring match, nothing fancier (no
// regex/fuzzy matching) - matches this project's existing "documented
// heuristic, not chased further" style elsewhere (taskLabel's truncation,
// primaryOutputField's own stdout-vs-msg choice).
func taskMatchesSearch(t *taskNode, term string, sourceIndex taskSourceIndex) bool {
	if term == "" {
		return true
	}
	term = strings.ToLower(term)
	if strings.Contains(strings.ToLower(t.Name), term) {
		return true
	}
	if source, ok := sourceIndex[t.Path]; ok && strings.Contains(strings.ToLower(source), term) {
		return true
	}
	for host := range t.Hosts {
		if strings.Contains(strings.ToLower(taskOutputText(t, host)), term) {
			return true
		}
	}
	return false
}

// taskVisible reports whether t should get a row under q - see Filters.md's
// Acceptance criteria. A task's status for filtering purposes is
// host-level: it matches "Changed"/"Failed" if *at least one* of its hosts
// has that outcome (a task can have hosts in different states), and when
// it matches, flattenRows below shows all of its hosts - not just the
// matching ones. Unreachable hosts count as a failure for this purpose too
// - same bucket lastFailedTaskAndHost already treats it as for the
// auto-jump-on-failure feature. isActive means t is the run's current
// in-progress task (see playbookState.CurrentTask) - always shown
// regardless of filter, since it may simply not have recorded any host
// outcome yet (and, for filterSearch specifically, wouldn't have any
// output to search yet either).
func taskVisible(t *taskNode, q filterQuery, sourceIndex taskSourceIndex, isActive bool) bool {
	if isActive || q.mode == filterAll {
		return true
	}
	failed := taskHasAnyOutcome(t, outcomeFailed, outcomeUnreachable)
	switch q.mode {
	case filterFailed:
		return failed
	case filterSearch:
		return taskMatchesSearch(t, q.search, sourceIndex)
	default: // filterChanged
		return failed || taskHasAnyOutcome(t, outcomeChanged)
	}
}

// allTasks returns every task across every play, in run order (play order,
// then task order within each play) - the host-agnostic sibling of
// tasksForHost below, backing the main tree's n/p task-hop and E (expand
// all) shortcuts.
func allTasks(state *playbookState) []*taskNode {
	var tasks []*taskNode
	for _, play := range state.Plays {
		tasks = append(tasks, play.Tasks...)
	}
	return tasks
}

// inheritedExpandState is state.OnTaskAdded's own decision, pulled out as
// a pure function so it's testable without constructing a whole
// NewLiveTUI: a newly-added task inherits whatever expand state the task
// added immediately before it currently has in expanded (present tense -
// including any manual toggle since that task was added, not its state
// "as of" when it was added), or startExpanded (from .tangsible's
// general.default_tree_state) if task is the very first task of this
// generation (fewer than 2 tasks exist yet in all). all is the full,
// current allTasks(state) result, with task itself already the last
// element (state.OnTaskAdded fires after task has already been appended
// to its play, and that play to state.Plays) - the second-to-last is "the
// task added right before it."
func inheritedExpandState(all []*taskNode, expanded map[*taskNode]bool, startExpanded bool) bool {
	if len(all) < 2 {
		return startExpanded
	}
	return expanded[all[len(all)-2]]
}

// tasksForHost returns, in run order (play order, then task order within
// each play), every task that has recorded a result for host - used by the
// output drill-down view's prev/next-task navigation (see NewLiveTUI's
// navigateOutputTask) to step through one host's results across tasks,
// skipping tasks that host wasn't part of.
func tasksForHost(state *playbookState, host string) []*taskNode {
	var tasks []*taskNode
	for _, play := range state.Plays {
		for _, t := range play.Tasks {
			if _, ok := t.Hosts[host]; ok {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

// visibleTasks is allTasks' filtered sibling - every task that gets a row
// under filter (see taskVisible), in the same run order. Used by
// navigateMainTask (n/p) and the filter-switch cursor fallback so neither
// ever targets a task flattenRows wouldn't actually have rendered a row
// for.
func visibleTasks(state *playbookState, filter filterQuery, sourceIndex taskSourceIndex, activeTask *taskNode) []*taskNode {
	var tasks []*taskNode
	for _, t := range allTasks(state) {
		if taskVisible(t, filter, sourceIndex, t == activeTask) {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// visibleTasksForHost is tasksForHost's filtered sibling, for the output
// drill-down view's prev/next-task navigation (see navigateOutputTask).
func visibleTasksForHost(state *playbookState, host string, filter filterQuery, sourceIndex taskSourceIndex, activeTask *taskNode) []*taskNode {
	var tasks []*taskNode
	for _, t := range tasksForHost(state, host) {
		if taskVisible(t, filter, sourceIndex, t == activeTask) {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// taskSet turns a task slice into a membership set - shared by
// nearestVisibleTask and navigateMainTask's play-row case below, both of
// which need repeated "is this task currently visible" checks against the
// same visibleTasks() result.
func taskSet(tasks []*taskNode) map[*taskNode]bool {
	set := make(map[*taskNode]bool, len(tasks))
	for _, t := range tasks {
		set[t] = true
	}
	return set
}

// firstVisibleTask returns play's own first task that's currently visible
// (per the visible set, built from visibleTasks), or nil if it has none -
// used by navigateMainTask's play-row case so "next" from a play row never
// targets a task the active filter is hiding. Never actually returns nil in
// practice: flattenRows only ever gives a play a row at all when it has at
// least one visible task, and navigateMainTask only reaches this for a play
// the cursor is currently sitting on.
func firstVisibleTask(play *playNode, visible map[*taskNode]bool) *taskNode {
	for _, t := range play.Tasks {
		if visible[t] {
			return t
		}
	}
	return nil
}

// nearestVisibleTask finds, within visible (a filtered, order-preserving
// subset of all), the task closest to anchor's original position: the next
// visible task at or after it in run order, or failing that, the last
// visible task before it. Returns nil if visible is empty. Used when
// switching filters removes the row the cursor was pinned to (see
// NewLiveTUI's applyFilter) - "removed" always means a whole task (and, per
// Filters.md, all of its still-shown hosts) disappearing at once, never an
// individual host row on its own, so a task is always the right granularity
// to land on.
func nearestVisibleTask(all []*taskNode, anchor *taskNode, visible []*taskNode) *taskNode {
	if len(visible) == 0 {
		return nil
	}
	visibleSet := taskSet(visible)
	anchorIdx := -1
	for i, t := range all {
		if t == anchor {
			anchorIdx = i
			break
		}
	}
	if anchorIdx == -1 {
		return visible[0]
	}
	for i := anchorIdx; i < len(all); i++ {
		if visibleSet[all[i]] {
			return all[i]
		}
	}
	for i := anchorIdx - 1; i >= 0; i-- {
		if visibleSet[all[i]] {
			return all[i]
		}
	}
	return visible[0] // unreachable given visible is non-empty and a subset of all
}
