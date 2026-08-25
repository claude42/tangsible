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
}

func (t *TaskNode) record(host string, o Outcome, raw json.RawMessage) {
	if _, seen := t.Hosts[host]; !seen {
		t.HostOrder = append(t.HostOrder, host)
	}
	t.Hosts[host] = o
	t.Raw[host] = raw
	t.Warnings[host] = hasNonEmptyWarnings(raw)
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
	currentTask     *TaskNode

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
	s.currentTask = nil
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
	// this used to) meant a handler never got its own TaskNode or
	// advanced currentTask at all: its later v2_runner_on_* events still
	// fired and still went through recordHost below, silently
	// attributing the handler's own result onto whatever task genuinely
	// started last - a real bug report, not a hypothetical, that could
	// corrupt an already-completed (and already-displayed) task's own
	// Raw/Hosts entries with a same-named host's handler result recorded
	// afterward. Treating both events identically - own TaskNode, own
	// row, own currentTask - is what makes a handler run visible in the
	// tree at all instead of silently overwriting something else's data.
	case "v2_playbook_on_task_start", "v2_playbook_on_handler_task_start":
		if s.currentPlay == nil {
			s.currentPlay = &PlayNode{Name: s.pendingPlayName}
			s.Plays = append(s.Plays, s.currentPlay)
			if s.OnPlayAdded != nil {
				s.OnPlayAdded(s.currentPlay)
			}
		}
		if ev.Task != nil {
			s.currentTask = &TaskNode{
				Name:      ev.Task.Name,
				Path:      ev.Task.Path,
				StartedAt: ev.Timestamp(),
				Hosts:     map[string]Outcome{},
				Raw:       map[string]json.RawMessage{},
				Warnings:  map[string]bool{},
			}
			s.currentPlay.Tasks = append(s.currentPlay.Tasks, s.currentTask)
			if s.OnTaskAdded != nil {
				s.OnTaskAdded(s.currentPlay, s.currentTask)
			}
		}

	case "v2_runner_on_ok":
		for host, raw := range ev.Hosts {
			r := DecodeHostResult(raw)
			o := OutcomeOK
			if r.Changed {
				o = OutcomeChanged
			}
			s.recordHost(host, o, raw)
		}

	case "v2_runner_on_skipped":
		for host, raw := range ev.Hosts {
			s.recordHost(host, OutcomeSkipped, raw)
		}

	case "v2_runner_on_failed":
		for host, raw := range ev.Hosts {
			s.recordHost(host, OutcomeFailed, raw)
		}

	case "v2_runner_on_unreachable":
		for host, raw := range ev.Hosts {
			s.recordHost(host, OutcomeUnreachable, raw)
		}
	}
}

func (s *PlaybookState) recordHost(host string, o Outcome, raw json.RawMessage) {
	if s.currentTask == nil {
		return
	}
	s.currentTask.record(host, o, raw)
	s.noteHost(host)
	if o == OutcomeUnreachable {
		s.HadUnreachable = true
	}
	if s.OnHostRecorded != nil {
		s.OnHostRecorded(s.currentTask, host)
	}
}

// CurrentTask returns the task currently receiving events, or nil before
// the first task of the run has started. Exposed read-only for tui.go,
// which uses it to mark the active task's row with a spinner; Apply/
// recordHost remain the only code that ever assigns currentTask. Per the
// linear-strategy assumption already documented above, this stays pointing
// at the most recently started task even after all its hosts have
// reported - there's no separate "this task is now done" signal - so a
// caller-side "active" indicator keeps showing on that task until either
// the next task starts or the run finishes. Pre-existing approximation,
// not something this accessor introduces.
func (s *PlaybookState) CurrentTask() *TaskNode {
	return s.currentTask
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
