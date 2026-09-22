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

package playbook

import (
	"encoding/json"
	"sort"
	"time"
)

// Outcome is a host's result for one task.
type Outcome int

const (
	OutcomeOK Outcome = iota
	OutcomeChanged
	OutcomeSkipped
	OutcomeFailed
	OutcomeUnreachable
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "OK"
	case OutcomeChanged:
		return "Changed"
	case OutcomeSkipped:
		return "Skipped"
	case OutcomeFailed:
		return "Failed"
	case OutcomeUnreachable:
		return "Unreachable"
	default:
		return "?"
	}
}

type TaskNode struct {
	Name string
	// Path is the task's own source location ("<absolute file>:<line>"),
	// straight from the starting event's own task.path - used by
	// tui.go's output drill-down view to look up the task's raw source
	// text via source.go's taskSourceIndex. Empty if the event didn't
	// carry one (shouldn't happen for a real run, but not trusted
	// blindly - same caveat as this file's other event-derived fields).
	Path string
	// IsHandler is RawEvent.Task.IsHandler at the moment this task started
	// - see that field's own doc comment (design-docs/
	// OwnCallbackPlugin.md). Always false for a pre-fork run log, or for
	// any task-start event this app's own plugin didn't stamp. Used by
	// uikit.TaskLabel to render a "[Handler]" tag and by runner.
	// ProgressTracker.Advance to treat a handler's own miss against the
	// progress skeleton (which never lists handlers at all - progress.go's
	// own doc comment) as expected rather than evidence of drift.
	IsHandler bool
	// StartedAt is from the starting event's own _timestamp
	// (RawEvent.Timestamp()), not our wall-clock time.Now() at the moment
	// we process it - "when did Ansible itself start this task." Zero if
	// that timestamp was missing/malformed. Currently unused by any
	// renderer (tui.go shows a spinner rather than an elapsed readout for
	// the active task) - kept because Ansible provides it for free and a
	// future summary/history view (see Findings.md) would want it; remove
	// if that never materializes.
	StartedAt time.Time
	HostOrder []string
	Hosts     map[string]Outcome
	// Raw holds each host's full original result payload for this task,
	// parallel to Hosts - populated alongside it in record, read by
	// tui.go's showOutput on demand. Never formatted here; formatting is
	// a UI concern (see tui.go).
	Raw map[string]json.RawMessage
	// Warnings mirrors Hosts, not Raw: like Outcome, "did this host report
	// a non-empty warnings field for this task" is a classification
	// decided once from the raw payload, not a rendering decision - the
	// same category of fact Outcome already is, computed the same way
	// (once, in record, from this event's own raw bytes), just a second
	// independent axis alongside it (a result can be both Changed and
	// carry a warning). Exists purely so callers rendering many rows
	// across many hosts (tui.go's TaskLabel/HostLabel, recap.go's
	// recapForHost) don't have to re-decode the same JSON on every single
	// redraw just to answer a yes/no question - a real, measured cost:
	// profiling a frozen 40-host/30-task recap showed ~63% of
	// FlattenRecapRows' own CPU time was encoding/json decode calls doing
	// exactly this, repeated on every cursor move. hasNonEmptyWarnings
	// (events.go) deliberately doesn't reuse uikit's own DecodeWarnings/
	// JoinedStringList - this package can't import uikit (uikit already
	// imports this one) - but only needs a plain presence check, not
	// JoinedStringList's own multi-shape text-joining, so duplicating that
	// much is a deliberately small, self-contained trade.
	Warnings map[string]bool
	// HasStderr mirrors Warnings - same shape, same reasoning, same
	// "computed once in record from this event's own raw bytes" timing -
	// just a third independent axis (Filters.md's "Interesting" filter
	// needs "changed/failed/unreachable, has stderr output, or has a
	// warning" without decoding every host's raw JSON on every rebuild).
	HasStderr map[string]bool
	// Ignored mirrors Warnings/HasStderr - same shape, same "computed once
	// in record" timing - true when this host's own result carried
	// "ignore_errors": true (design-docs/OwnCallbackPlugin.md's
	// hasIgnoreErrors), which only a Failed outcome ever sets. Deliberately
	// doesn't change what Outcome itself is: a host whose only failure was
	// ignore_errors: true still records as OutcomeFailed everywhere that
	// matters operationally (tree coloring, FailedHosts, rerun's "Only
	// failed") - this is purely an additional, additive signal for
	// renderers that want to distinguish the two without touching that
	// established, deliberate simplification (see FailedHosts' own doc
	// comment).
	Ignored map[string]bool
	// Started is each host's own "v2_runner_on_start" timestamp - see
	// RawEvent.Host's own doc comment for why this event (and so this map)
	// didn't used to exist under the linear strategy. This is what
	// disambiguates real per-host execution time from queue-wait
	// (design-docs/PerHostTaskTiming.md's "the problem: this delta doesn't
	// mean what it looks like it means"), once paired with Finished below.
	// Absent entry (zero time.Time) means "no v2_runner_on_start ever
	// arrived for this host" - true for every run recorded before this
	// fork existed, and the same "zero means unknown" convention StartedAt
	// already uses.
	Started map[string]time.Time
	// Finished is each host's own outcome-event timestamp (the same
	// ev.Timestamp() already used to decide task.duration.end), recorded
	// per host rather than just once per task - parallel to Hosts/Raw,
	// populated alongside them in record. Combined with Started, this
	// gives a real per-host duration; used alone (Started absent, e.g. a
	// pre-fork run log) it's not meaningful on its own, same caveat
	// PerHostTaskTiming.md raised for the finish-only approach.
	Finished map[string]time.Time
	// inFlight is the set of hosts currently dispatched on this task but
	// not yet reported a terminal outcome - added by a v2_runner_on_start
	// event (Apply's own case), removed by recordHost the moment that
	// host's own outcome arrives. Backs PlaybookState.IncompleteTasks -
	// design-docs/StrategyFree.md's exact (not approximated) replacement
	// for the old single currentTask pointer, which broke down the moment
	// more than one task could be in flight at once (strategy: free).
	// Unexported: nothing outside this package needs per-host in-flight
	// detail, only the task-level "is anything still in flight"
	// aggregate IncompleteTasks provides.
	inFlight map[string]bool
}

func (t *TaskNode) record(host string, o Outcome, raw json.RawMessage, finishedAt time.Time) {
	if _, seen := t.Hosts[host]; !seen {
		t.HostOrder = append(t.HostOrder, host)
	}
	t.Hosts[host] = o
	t.Raw[host] = raw
	t.Warnings[host] = hasNonEmptyWarnings(raw)
	t.HasStderr[host] = hasNonEmptyStderr(raw)
	t.Ignored[host] = hasIgnoreErrors(raw)
	t.Finished[host] = finishedAt
	delete(t.inFlight, host)
}

// hasHost reports whether host has already touched this task node, in
// flight or completed - findOrCreateTask's own per-play path-based
// fallback matching (see its own doc comment) uses this to decide whether
// a given host can newly join an existing node for the same source path,
// or needs a fresh one instead (a later loop iteration over the same
// include, revisiting the same path for a host that already has a result
// on the earlier iteration's own node).
func (t *TaskNode) hasHost(host string) bool {
	if _, ok := t.Hosts[host]; ok {
		return true
	}
	_, ok := t.Started[host]
	return ok
}

func (t *TaskNode) Counts() (ok, changed, skipped, failed, unreachable int) {
	for _, o := range t.Hosts {
		switch o {
		case OutcomeOK:
			ok++
		case OutcomeChanged:
			changed++
		case OutcomeSkipped:
			skipped++
		case OutcomeFailed:
			failed++
		case OutcomeUnreachable:
			unreachable++
		}
	}
	return
}

type PlayNode struct {
	Name  string
	Tasks []*TaskNode

	// tasksByPath indexes this play's own Tasks by source path (file:line),
	// in creation order - findOrCreateTask's own fallback index, scoped per
	// play (not run-wide) since two different plays including the exact
	// same file would otherwise collide on one shared path key. Unexported;
	// findOrCreateTask is the only reader/writer.
	tasksByPath map[string][]*TaskNode
}

// PlaybookState is the play -> task -> host tree built up from the live
// event stream. Plays and tasks are appended lazily, on their first task's
// start, so plays with no executed tasks never show up (see TUI.md).
//
// Once its On*Added/OnHostRecorded hooks are wired up (by tui.go), this is
// only safe to mutate from whichever single goroutine calls Apply — no
// mutex, by construction, since that's always tview's event-loop goroutine
// (Apply runs inside an app.QueueUpdateDraw closure). Not a general-purpose
// concurrent data structure.
type PlaybookState struct {
	Plays []*PlayNode

	// AllHosts is the run-wide, alphabetically-sorted set of hosts that have
	// reported anything, for any task, so far this run. It only ever grows.
	// tui.go's taskLabel uses it to show every known host on every task's
	// collapsed row (grey if this specific task hasn't recorded a result
	// for it yet) instead of only the hosts each task has itself heard
	// from — see TUI.md's "New ideas for the task lines". There's no
	// upstream event that reveals a task's target hosts before its first
	// result, so a task's very first appearance in a run necessarily starts
	// this list from empty; every later task inherits whatever's already
	// been discovered.
	AllHosts []string

	// HadUnreachable is true once any host has been recorded as
	// OutcomeUnreachable, at any point in this run - run-wide, once true,
	// never cleared, like AllHosts. Exists so main.go can disambiguate
	// ansible-playbook's own overloaded exit code 4 (ansible-core's own
	// ExitCode enum assigns 4 to both HOST_UNREACHABLE and PARSER_ERROR,
	// with its own "FIXME: conflicts" comment) using evidence Tangsible
	// independently observed via the real v2_runner_on_unreachable event
	// stream, rather than trusting the exit code alone.
	HadUnreachable bool

	pendingPlayName string
	currentPlay     *PlayNode
	// tasksByID resolves a task's own identity (RawEvent.Task.ID) to its
	// TaskNode, regardless of which event first revealed it - replaces the
	// old single currentTask pointer (design-docs/StrategyFree.md), which
	// assumed Ansible's linear strategy (one task in flight at a time) and
	// silently misattributed results under strategy: free, where several
	// tasks can be genuinely simultaneous. findOrCreateTask is the only
	// reader/writer.
	tasksByID map[string]*TaskNode

	// Optional hooks a UI layer wires up before streaming begins, so a
	// tree can grow incrementally instead of being rebuilt from scratch on
	// every event. nil-checked before every call. Deliberately typed using
	// only this file's own types, so this file stays free of any UI
	// dependency.
	OnPlayAdded    func(play *PlayNode)
	OnTaskAdded    func(play *PlayNode, task *TaskNode)
	OnHostRecorded func(task *TaskNode, host string)
	// OnPlayStarted fires on every real v2_playbook_on_play_start event,
	// unconditionally - unlike OnPlayAdded, which only ever fires once a
	// play gets its own first task (see the tree's own "plays with no
	// executed tasks never appear" rule). Confirmed empirically: a play
	// whose hosts: pattern matches zero hosts in this run's inventory
	// still gets a real v2_playbook_on_play_start event, even though none
	// of its tasks ever fire a single event afterward - so this is the
	// only reliable signal that such a play (and, transitively, every
	// task nested inside it) has been passed over entirely. progress.go's
	// tracker uses this as a resync point precisely because of that -
	// see NewLiveTUI's own wiring for why per-task matching alone can't
	// recover from an entirely-skipped play on its own.
	OnPlayStarted func(name string)
}

// Reset clears every field Apply/recordHost populate during a run, back to
// the same zero state a fresh &PlaybookState{} starts in - used when
// starting a rerun (Rerun.md) so the next Apply sequence builds a brand new
// tree instead of appending onto the previous run's. The OnPlayAdded/
// OnTaskAdded/OnHostRecorded hooks are deliberately left untouched: they're
// wired once by tui.go and need to keep firing for the new run's events
// exactly as they did for the run before it.
func (s *PlaybookState) Reset() {
	s.Plays = nil
	s.AllHosts = nil
	s.HadUnreachable = false
	s.pendingPlayName = ""
	s.currentPlay = nil
	s.tasksByID = nil
}

// findOrCreateTask resolves ev's own task to its TaskNode, creating both
// the task and (lazily, same as before) its enclosing play if this is the
// first event to reveal either - design-docs/StrategyFree.md's core fix.
// Keyed primarily by ev.Task.ID (stable across every event referencing the
// same task - task-start, terminal, v2_runner_on_start alike, confirmed
// present on all of them even under stock ansible.posix.jsonl, not just
// this app's own fork) rather than by "whichever task happened to start
// most recently" - the old currentTask assumption, which silently
// misattributed results the moment more than one task could be in flight
// at once (strategy: free structurally guarantees that; a duplicate task
// name under strategy: linear already made a plain name-based lookup
// wrong too). Returns nil, doing nothing, if ev carries no Task at all -
// callers must treat that the same as "no task to record against",
// exactly as recordHost's own old currentTask-nil check did.
//
// host is the specific host this resolution is for - "" when none applies
// (the task-start cases below, which are never per-host to begin with).
// Needed for the fallback matching just below, not just bookkeeping: a
// terminal event only ever carries one host in ev.Hosts in practice (see
// its own callers), but the *resolution* still has to be host-aware, not
// just event-aware.
//
// A real gap confirmed live, not assumed: under strategy: free, a
// dynamically-included task (include_tasks/include_role) gets its own
// freshly-constructed Ansible Task object - and so its own distinct
// task.id - independently per host, since free's own per-host
// independence means Ansible never merges hosts into one shared batch the
// way linear does for an identical include. Two hosts hitting the very
// same "included.yml:3" task ended up with two different task.id values,
// which id-only lookup would show as two separate rows instead of one
// shared row with two host badges - a real user report. task.path
// ("<file>:<line>") is still identical across hosts in that case - it
// names a source location, and no two distinct tasks in a real playbook
// share one - so once id itself can't be trusted to mean "the same task,"
// path is what still does.
//
// Path alone isn't safe to match on unconditionally, though - also
// confirmed live: a loop: around an include_tasks produces one real
// task-start-shaped event *per iteration*, all sharing the exact same
// included file's path, and those genuinely are separate tasks that must
// stay separate rows. What actually distinguishes "the same dispatch,
// split across hosts" from "a later, distinct iteration at the same path"
// is host membership: a host can only ever belong to one "occurrence" of
// a given path at a time. PlayNode.tasksByPath keeps every TaskNode ever
// created for a given path, in creation order; the fallback below walks
// them and claims the first one host hasn't already touched (TaskNode.
// hasHost) - if every existing occurrence already has this host, this
// must be a new iteration, so a fresh node is created instead. Scoped per
// play (tasksByPath lives on PlayNode, not PlaybookState), not run-wide -
// two different plays including the exact same file would otherwise
// collide on one shared path key despite being unrelated.
func (s *PlaybookState) findOrCreateTask(ev RawEvent, host string) *TaskNode {
	if ev.Task == nil {
		return nil
	}
	if s.currentPlay == nil {
		s.currentPlay = &PlayNode{Name: s.pendingPlayName}
		s.Plays = append(s.Plays, s.currentPlay)
		if s.OnPlayAdded != nil {
			s.OnPlayAdded(s.currentPlay)
		}
	}
	if t, ok := s.tasksByID[ev.Task.ID]; ok {
		// Found by ID alone - deliberately doesn't touch Name/Path/
		// IsHandler even if this particular event's own Task carries
		// them (or doesn't): under lockstep, a v2_runner_on_start event's
		// own Task is minimal ({"id": ...} only, tangsible_jsonl.py's own
		// lockstep branch - design-docs/StrategyFree.md), and blindly
		// overwriting an already-populated Name/Path with empty strings
		// from that shape would erase what the real task-start event
		// already correctly set.
		return t
	}
	if host != "" && ev.Task.Path != "" {
		for _, t := range s.currentPlay.tasksByPath[ev.Task.Path] {
			if !t.hasHost(host) {
				if s.tasksByID == nil {
					s.tasksByID = map[string]*TaskNode{}
				}
				s.tasksByID[ev.Task.ID] = t // alias this host's own id too, so its later events for this task hit the id-only fast path above directly
				return t
			}
		}
	}
	t := &TaskNode{
		Name:      ev.Task.Name,
		Path:      ev.Task.Path,
		IsHandler: ev.Task.IsHandler,
		StartedAt: ev.Timestamp(),
		Hosts:     map[string]Outcome{},
		Raw:       map[string]json.RawMessage{},
		Warnings:  map[string]bool{},
		HasStderr: map[string]bool{},
		Ignored:   map[string]bool{},
		Started:   map[string]time.Time{},
		Finished:  map[string]time.Time{},
		inFlight:  map[string]bool{},
	}
	s.currentPlay.Tasks = append(s.currentPlay.Tasks, t)
	if s.tasksByID == nil {
		s.tasksByID = map[string]*TaskNode{}
	}
	s.tasksByID[ev.Task.ID] = t
	if ev.Task.Path != "" {
		if s.currentPlay.tasksByPath == nil {
			s.currentPlay.tasksByPath = map[string][]*TaskNode{}
		}
		s.currentPlay.tasksByPath[ev.Task.Path] = append(s.currentPlay.tasksByPath[ev.Task.Path], t)
	}
	if s.OnTaskAdded != nil {
		s.OnTaskAdded(s.currentPlay, t)
	}
	return t
}

func (s *PlaybookState) Apply(ev RawEvent) {
	switch ev.Event {
	case "v2_playbook_on_play_start":
		if ev.Play != nil {
			s.pendingPlayName = ev.Play.Name
			if s.OnPlayStarted != nil {
				s.OnPlayStarted(ev.Play.Name)
			}
		}
		s.currentPlay = nil

	// v2_playbook_on_task_start and v2_playbook_on_handler_task_start are
	// two entirely distinct event names (confirmed empirically, not
	// documented upstream) - fired for a regular task and a
	// notify:-triggered handler respectively, but otherwise carrying the
	// identical task{name,path,...} shape. Handling only the former (as
	// this used to) meant a handler never got its own TaskNode at all:
	// its later v2_runner_on_* events still fired and still went through
	// recordHost below, silently attributing the handler's own result
	// onto whatever task genuinely started last - a real bug report, not
	// a hypothetical, that could corrupt an already-completed (and
	// already-displayed) task's own Raw/Hosts entries with a same-named
	// host's handler result recorded afterward. Treating both events
	// identically - own TaskNode, own row - is what makes a handler run
	// visible in the tree at all instead of silently overwriting
	// something else's data.
	case "v2_playbook_on_task_start", "v2_playbook_on_handler_task_start":
		s.findOrCreateTask(ev, "")

	// Resolved once *per host*, not once per event, even though a
	// terminal event only ever carries one host in ev.Hosts in practice
	// (jsonl.py's own _record_task_result writes one result per callback
	// invocation) - findOrCreateTask's own path-based fallback (see its
	// doc comment) needs to know which host it's resolving for, so
	// resolution has to happen inside this loop, not once above it.
	case "v2_runner_on_ok":
		for host, raw := range ev.Hosts {
			task := s.findOrCreateTask(ev, host)
			r := DecodeHostResult(raw)
			o := OutcomeOK
			if r.Changed {
				o = OutcomeChanged
			}
			s.recordHost(task, host, o, raw, ev.Timestamp())
		}

	case "v2_runner_on_skipped":
		for host, raw := range ev.Hosts {
			task := s.findOrCreateTask(ev, host)
			s.recordHost(task, host, OutcomeSkipped, raw, ev.Timestamp())
		}

	case "v2_runner_on_failed":
		for host, raw := range ev.Hosts {
			task := s.findOrCreateTask(ev, host)
			s.recordHost(task, host, OutcomeFailed, raw, ev.Timestamp())
		}

	case "v2_runner_on_unreachable":
		for host, raw := range ev.Hosts {
			task := s.findOrCreateTask(ev, host)
			s.recordHost(task, host, OutcomeUnreachable, raw, ev.Timestamp())
		}

	// Only ever arrives from this app's own bundled callback plugin fork
	// (RawEvent.Host's own doc comment) - a pre-fork run log simply never
	// produces this case, leaving Started/inFlight empty exactly like
	// every other "unknown" event-derived field in this package. Resolves
	// its own task by ID (findOrCreateTask) rather than trusting
	// currentTask, the same as every other case above now - under
	// strategy: free the task this event refers to may not have been
	// created by anything else yet (no task-start event ever fires there
	// at all), so this is the one case that can legitimately be the very
	// first event to reveal a given task - and, for a dynamically
	// included task specifically, the one case that actually needs
	// findOrCreateTask's own path-based fallback (its own doc comment):
	// this is where a per-host-diverging id first gets resolved: later
	// terminal events for the very same id already hit the id-only fast
	// path directly, since this case's own call below is what aliases it.
	case "v2_runner_on_start":
		task := s.findOrCreateTask(ev, ev.Host)
		if task != nil && ev.Host != "" {
			task.Started[ev.Host] = ev.Timestamp()
			task.inFlight[ev.Host] = true
		}
	}
}

func (s *PlaybookState) recordHost(task *TaskNode, host string, o Outcome, raw json.RawMessage, finishedAt time.Time) {
	if task == nil {
		return
	}
	task.record(host, o, raw, finishedAt)
	s.noteHost(host)
	if o == OutcomeUnreachable {
		s.HadUnreachable = true
	}
	if s.OnHostRecorded != nil {
		s.OnHostRecorded(task, host)
	}
}

// IncompleteTasks returns every task, across all plays, in first-seen
// (run) order, that currently has at least one host dispatched but not yet
// reporting a terminal outcome (TaskNode.inFlight, set by a
// v2_runner_on_start event, cleared the moment that host's own result
// arrives - see Apply/TaskNode.record). Exposed read-only for tui.go, which
// uses it to mark every currently-active task's row with a spinner and to
// keep an in-progress task visible regardless of the active filter -
// design-docs/StrategyFree.md's exact replacement for the old single
// CurrentTask()/currentTask pointer, which assumed Ansible's linear
// strategy (never more than one task genuinely in flight) and simply broke
// under strategy: free, where several tasks can be simultaneously
// incomplete. Under linear this returns exactly the same single task
// CurrentTask() used to (Started is populated per host by this app's own
// bundled callback plugin fork - RawEvent.Host's own doc comment - for
// both lockstep and free strategies alike); a pre-fork run log (no
// v2_runner_on_start events at all, e.g. a saved run replayed via revisit/
// diff) simply never populates inFlight for anything, so this always
// returns empty for one - harmless, since a replayed log is already frozen
// (processDone pre-true) by the time anything asks. A plain scan over
// Plays/Tasks, not a maintained index - same "simple beats clever at
// ~10-host scale" reasoning hostsWithOutcome below already follows.
func (s *PlaybookState) IncompleteTasks() []*TaskNode {
	var tasks []*TaskNode
	for _, play := range s.Plays {
		for _, t := range play.Tasks {
			if len(t.inFlight) > 0 {
				tasks = append(tasks, t)
			}
		}
	}
	return tasks
}

// FailedHosts returns the sorted, deduplicated set of hosts that recorded
// OutcomeFailed on any task, anywhere in this run - design-docs/Rerun.md's
// "Only failed hosts" checkbox and the host-half of "Resume where failed."
// Deliberately reuses OutcomeFailed as-is, same as everywhere else in this
// package: a host whose only failure was an ignore_errors: true task is
// still counted here, matching this app's existing simplification (Ansible
// itself would count that toward "ok" + a separate "ignored" tally, not
// "failed" - see TaskNode.Counts' own doc comment history).
func (s *PlaybookState) FailedHosts() []string {
	return s.hostsWithOutcome(OutcomeFailed)
}

// UnreachableHosts returns the sorted, deduplicated set of hosts that
// recorded OutcomeUnreachable on any task, anywhere in this run -
// design-docs/Rerun.md's "Only unreachable hosts" checkbox.
func (s *PlaybookState) UnreachableHosts() []string {
	return s.hostsWithOutcome(OutcomeUnreachable)
}

// hostsWithOutcome walks every play's every task's Hosts map, collecting
// every host that was ever recorded as target at least once, sorted and
// deduplicated - shared by FailedHosts/UnreachableHosts so the two can't
// silently drift on how "recorded as X anywhere in the run" is computed.
func (s *PlaybookState) hostsWithOutcome(target Outcome) []string {
	seen := map[string]bool{}
	var hosts []string
	for _, play := range s.Plays {
		for _, task := range play.Tasks {
			for host, o := range task.Hosts {
				if o == target && !seen[host] {
					seen[host] = true
					hosts = append(hosts, host)
				}
			}
		}
	}
	sort.Strings(hosts)
	return hosts
}

// EarliestFailingPlay returns the name of the first play, in run order,
// containing at least one host recorded as OutcomeFailed on any of its
// tasks - design-docs/Rerun.md's "Resume where failed" own "Start with
// play" default. Returns "" if there were no failures at all this run.
func (s *PlaybookState) EarliestFailingPlay() string {
	for _, play := range s.Plays {
		for _, task := range play.Tasks {
			for _, o := range task.Hosts {
				if o == OutcomeFailed {
					return play.Name
				}
			}
		}
	}
	return ""
}

// noteHost adds host to AllHosts, keeping it sorted, the first time it's
// seen run-wide (across any task). A plain linear scan plus an unconditional
// re-sort on every new host is dead simple and more than fast enough at this
// project's explicit ~10-host target scale (Purpose.md) — not worth a
// membership map or an insertion-sort for that size.
func (s *PlaybookState) noteHost(host string) {
	for _, h := range s.AllHosts {
		if h == host {
			return
		}
	}
	s.AllHosts = append(s.AllHosts, host)
	sort.Strings(s.AllHosts)
}
