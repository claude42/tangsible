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

package debugverb

import (
	"encoding/json"

	"code.aw.net/claude/tangsible/internal/debugrun"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
)

// DebugConfig bundles everything internal/session's own NewLiveTUI needs
// to build a debug session - zero value (BPS == nil) for every other
// Verb, which is what liveSession's own debugMode field is derived from.
// Defined here, not in internal/session itself, specifically so
// internal/revisit's own injected NewLiveTUIFunc type (revisit.go - kept
// deliberately separate from a direct import of internal/session, to
// avoid an import cycle: internal/session already imports internal/
// revisit) can reference it too, without importing internal/session.
type DebugConfig struct {
	// CopyPlaybookPath is the absolute path into the shadow copy - what
	// F5/F10 actually spawn (PrepareSession's own CopyPlaybookPath).
	CopyPlaybookPath string
	// BPS is this session's own armed-breakpoint set.
	BPS *debugrun.BreakpointSet
	// Cursor resolves a live jsonl event against the skeleton-seeded
	// state already passed as NewLiveTUI's own state parameter
	// (SeedState's second return value, wrapped - built by main.go right
	// alongside state itself).
	Cursor *Cursor
}

// SeedState builds a *playbook.PlaybookState directly from skeleton
// (runner.BuildProgressSkeleton, probed against the already-spliced
// shadow copy) - every play/task visible up front, before anything has
// run, rather than growing incrementally via PlaybookState.Apply the way
// a plain run's own state does.
//
// This is deliberate, not a shortcut: Apply's own task/play identity
// resolution (findOrCreateTask, aggregate.go) assumes incremental
// arrival - it matches purely by the live event's own task.id, or a
// path/host fallback, and v2_playbook_on_play_start always starts a
// fresh PlayNode (never reused by name). Pre-populating state.Plays from
// this skeleton and then letting real events flow through Apply as
// normal would not reuse these nodes at all - it would create a second,
// parallel set of real PlayNode/TaskNodes once the run actually starts,
// leaving these frozen and orphaned. So a debug session's own events
// never go through Apply - see Cursor below instead, which resolves a
// live event directly against nodes (this function's own second return
// value, skeleton's own index parallel) using the same runner.
// ProgressTracker forward-matching the progress bar already uses to
// predict a percentage, repurposed here to answer "which of these
// already-built rows does this event belong to" instead.
//
// Because rebuild(), uikit.FlattenRows, the drill-down, two-pane layout,
// etc. only ever read state.Plays/*TaskNode fields - they never call
// Apply themselves - all of that machinery works unchanged on top of a
// state built this way; only the write path (Cursor.Apply below) differs
// from a plain run's.
func SeedState(skeleton []runner.ProgressEntry) (state *playbook.PlaybookState, nodes []*playbook.TaskNode) {
	state = &playbook.PlaybookState{}
	var currentPlay *playbook.PlayNode
	lastPlayName := ""
	first := true
	for _, e := range skeleton {
		if first || e.Play != lastPlayName {
			currentPlay = &playbook.PlayNode{Name: e.Play}
			state.Plays = append(state.Plays, currentPlay)
			lastPlayName = e.Play
			first = false
		}
		task := &playbook.TaskNode{
			Name: e.Task,
			// Hosts/Raw/Ignored initialized (not left nil): written into
			// directly by Cursor.Apply below, from outside the playbook
			// package - a nil map panics on write, unlike a nil-map read
			// elsewhere in this package's own rendering path.
			Hosts:   map[string]playbook.Outcome{},
			Raw:     map[string]json.RawMessage{},
			Ignored: map[string]bool{},
		}
		currentPlay.Tasks = append(currentPlay.Tasks, task)
		nodes = append(nodes, task)
	}
	return state, nodes
}

// Cursor tracks where a live debug session's own jsonl event stream
// currently is, relative to a SeedState-built tree - the replacement for
// PlaybookState's own private currentPlay/pendingPlayName bookkeeping,
// which Apply can't be reused for here (see SeedState's own doc
// comment).
type Cursor struct {
	// Nodes is SeedState's own second return value - never reassigned
	// after construction.
	Nodes []*playbook.TaskNode
	// Tracker drives the same skeleton forward-matching the progress bar
	// already uses (runner.NewProgressTracker(skeleton)) - shared, not a
	// second instance, so a debug session's progress percentage and its
	// own row-resolution never disagree about where the run currently is.
	Tracker *runner.ProgressTracker
	// PathMapping is SessionPrep's own field of the same name
	// (debugcopy.BuildPathMapping) - translates a live event's own
	// task.path (against the shadow copy) back to the real tree's
	// "path:line", so TaskNode.Path ends up something sourceIndex (built
	// from the real tree) can actually look up. A missing entry (a task
	// BuildPathMapping couldn't resolve, or a nil map for a caller that
	// never built one - e.g. a test) just leaves Path "", same as if
	// this field didn't exist at all - the Task definition/Resolved tabs
	// already treat an empty Path as "nothing to show," not an error.
	PathMapping map[string]string

	playName string
	// CurrentTask is the task the most recent task-start event resolved
	// to - nil until the first one arrives. Exported so a caller
	// (liveSession) can read it directly for chrome/auto-open-drilldown/
	// follow-cursor decisions, the same way the old debugApp's own
	// currentRowIdx was read from several call sites.
	CurrentTask *playbook.TaskNode
}

// NewCursor returns a Cursor ready to track nodes/tracker/pathMapping
// from the very start of a session (before any event has arrived).
func NewCursor(nodes []*playbook.TaskNode, tracker *runner.ProgressTracker, pathMapping map[string]string) *Cursor {
	return &Cursor{Nodes: nodes, Tracker: tracker, PathMapping: pathMapping}
}

// Apply resolves one live jsonl event and, for a terminal outcome event,
// records it directly into the right pre-built TaskNode's Hosts/Raw/
// Ignored maps - never through PlaybookState.Apply (see SeedState).
// Returns the host just recorded ("" if this call didn't record one -
// a play-start/task-start event, or a terminal event that arrived before
// any task had started) and whether CurrentTask just changed to a
// different task (so a caller can clear its own "paused here" state the
// same way the old debugApp's own applyEvent did, right alongside moving
// on).
func (c *Cursor) Apply(ev playbook.RawEvent) (recordedHost string, taskChanged bool) {
	switch ev.Event {
	case "v2_playbook_on_play_start":
		if ev.Play != nil {
			c.playName = ev.Play.Name
			c.Tracker.AdvanceToPlay(ev.Play.Name)
		}
	case "v2_playbook_on_task_start", "v2_playbook_on_handler_task_start":
		if ev.Task != nil {
			c.Tracker.Advance(c.playName, ev.Task.Name, ev.Task.IsHandler)
			if pos, _ := c.Tracker.Position(); pos > 0 && pos <= len(c.Nodes) {
				next := c.Nodes[pos-1]
				if next != c.CurrentTask {
					c.CurrentTask = next
					taskChanged = true
				}
				// Set once, the first time this task is ever seen - never
				// overwritten afterward (every later event for the same
				// task reports the identical shadow-copy path/line
				// anyway, so re-translating it every time would just be
				// wasted map lookups, not a correctness concern either
				// way).
				if next.Path == "" && ev.Task.Path != "" {
					if orig, ok := c.PathMapping[ev.Task.Path]; ok {
						next.Path = orig
					}
				}
			}
		}
	case "v2_runner_on_ok", "v2_runner_on_changed", "v2_runner_on_skipped", "v2_runner_on_failed", "v2_runner_on_unreachable":
		if c.CurrentTask != nil {
			for host, raw := range ev.Hosts {
				// HostOrder, not just Hosts: FlattenRows' own Enter-to-
				// expand loop (uikit/tui_rows.go) iterates t.HostOrder to
				// build a task's own host rows, never t.Hosts directly
				// (map iteration order isn't stable, and HostOrder is the
				// single source of truth every other renderer - uikit.
				// LastFailedTaskAndHost, the recap - already agrees on).
				// Mirrors aggregate.go's own TaskNode.record exactly:
				// appended the first time this host is ever seen for this
				// task, before Hosts itself is written. Confirmed live as
				// a real, reported bug without this: a task's own host
				// rows never appeared on Enter at all, armed breakpoint
				// or not - HostOrder stayed permanently empty since
				// nothing else in this package ever touched it.
				if _, seen := c.CurrentTask.Hosts[host]; !seen {
					c.CurrentTask.HostOrder = append(c.CurrentTask.HostOrder, host)
				}
				c.CurrentTask.Hosts[host] = OutcomeFor(ev.Event, raw)
				// Raw/Ignored, not just Hosts: HostLabel's own detail text
				// (OutcomeDetailText) and HostColorTag's "ignored, not a
				// genuine failure" coloring both need them too, now that
				// the main tree's own Enter-to-expand can show this host
				// as its own HostLabel row, same as any other task's.
				c.CurrentTask.Raw[host] = raw
				if ev.Event == "v2_runner_on_failed" {
					c.CurrentTask.Ignored[host] = playbook.HasIgnoreErrors(raw)
				}
				recordedHost = host // only ever one host per terminal event in practice (jsonl.py's own _record_task_result)
			}
		}
	}
	return recordedHost, taskChanged
}

// OutcomeFor classifies one host's raw result the same way aggregate.
// go's own PlaybookState.Apply does (confirmed by reading it first, not
// guessed): "v2_runner_on_ok" splits into OK/Changed via the result's
// own "changed" flag; every other terminal event name maps 1:1 to its
// own Outcome.
func OutcomeFor(eventName string, raw json.RawMessage) playbook.Outcome {
	switch eventName {
	case "v2_runner_on_skipped":
		return playbook.OutcomeSkipped
	case "v2_runner_on_failed":
		return playbook.OutcomeFailed
	case "v2_runner_on_unreachable":
		return playbook.OutcomeUnreachable
	default: // v2_runner_on_ok
		if playbook.DecodeHostResult(raw).Changed {
			return playbook.OutcomeChanged
		}
		return playbook.OutcomeOK
	}
}

// TaskLabel is a task row's own breakpoint-set key, matching the
// debugger's own prompt text exactly (internal/debugrun's promptRe) -
// design-docs/DebugMode.md's accepted limitation: two tasks sharing an
// exact name alias to the same breakpoint.
func TaskLabel(taskName string) string {
	return "TASK: " + taskName
}
