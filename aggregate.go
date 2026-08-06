package main

import (
	"encoding/json"
	"sort"
)

// outcome is a host's result for one task.
type outcome int

const (
	outcomeOK outcome = iota
	outcomeChanged
	outcomeSkipped
	outcomeFailed
	outcomeUnreachable
)

func (o outcome) String() string {
	switch o {
	case outcomeOK:
		return "OK"
	case outcomeChanged:
		return "Changed"
	case outcomeSkipped:
		return "Skipped"
	case outcomeFailed:
		return "Failed"
	case outcomeUnreachable:
		return "Unreachable"
	default:
		return "?"
	}
}

type taskNode struct {
	Name      string
	HostOrder []string
	Hosts     map[string]outcome
	// Raw holds each host's full original result payload for this task,
	// parallel to Hosts - populated alongside it in record, read by
	// tui.go's showOutput on demand. Never formatted here; formatting is
	// a UI concern (see tui.go).
	Raw map[string]json.RawMessage
}

func (t *taskNode) record(host string, o outcome, raw json.RawMessage) {
	if _, seen := t.Hosts[host]; !seen {
		t.HostOrder = append(t.HostOrder, host)
	}
	t.Hosts[host] = o
	t.Raw[host] = raw
}

func (t *taskNode) counts() (ok, changed, skipped, failed, unreachable int) {
	for _, o := range t.Hosts {
		switch o {
		case outcomeOK:
			ok++
		case outcomeChanged:
			changed++
		case outcomeSkipped:
			skipped++
		case outcomeFailed:
			failed++
		case outcomeUnreachable:
			unreachable++
		}
	}
	return
}

type playNode struct {
	Name  string
	Tasks []*taskNode
}

// playbookState is the play -> task -> host tree built up from the live
// event stream. Plays and tasks are appended lazily, on their first task's
// start, so plays with no executed tasks never show up (see TUI.md).
//
// Once its On*Added/OnHostRecorded hooks are wired up (by tui.go), this is
// only safe to mutate from whichever single goroutine calls Apply — no
// mutex, by construction, since that's always tview's event-loop goroutine
// (Apply runs inside an app.QueueUpdateDraw closure). Not a general-purpose
// concurrent data structure.
type playbookState struct {
	Plays []*playNode

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

	pendingPlayName string
	currentPlay     *playNode
	currentTask     *taskNode

	// Optional hooks a UI layer wires up before streaming begins, so a
	// tree can grow incrementally instead of being rebuilt from scratch on
	// every event. nil-checked before every call. Deliberately typed using
	// only this file's own types, so this file stays free of any UI
	// dependency.
	OnPlayAdded    func(play *playNode)
	OnTaskAdded    func(play *playNode, task *taskNode)
	OnHostRecorded func(task *taskNode, host string)
}

func (s *playbookState) Apply(ev rawEvent) {
	switch ev.Event {
	case "v2_playbook_on_play_start":
		if ev.Play != nil {
			s.pendingPlayName = ev.Play.Name
		}
		s.currentPlay = nil

	case "v2_playbook_on_task_start":
		if s.currentPlay == nil {
			s.currentPlay = &playNode{Name: s.pendingPlayName}
			s.Plays = append(s.Plays, s.currentPlay)
			if s.OnPlayAdded != nil {
				s.OnPlayAdded(s.currentPlay)
			}
		}
		if ev.Task != nil {
			s.currentTask = &taskNode{
				Name:  ev.Task.Name,
				Hosts: map[string]outcome{},
				Raw:   map[string]json.RawMessage{},
			}
			s.currentPlay.Tasks = append(s.currentPlay.Tasks, s.currentTask)
			if s.OnTaskAdded != nil {
				s.OnTaskAdded(s.currentPlay, s.currentTask)
			}
		}

	case "v2_runner_on_ok":
		for host, raw := range ev.Hosts {
			r := decodeHostResult(raw)
			o := outcomeOK
			if r.Changed {
				o = outcomeChanged
			}
			s.recordHost(host, o, raw)
		}

	case "v2_runner_on_skipped":
		for host, raw := range ev.Hosts {
			s.recordHost(host, outcomeSkipped, raw)
		}

	case "v2_runner_on_failed":
		for host, raw := range ev.Hosts {
			s.recordHost(host, outcomeFailed, raw)
		}

	case "v2_runner_on_unreachable":
		for host, raw := range ev.Hosts {
			s.recordHost(host, outcomeUnreachable, raw)
		}
	}
}

func (s *playbookState) recordHost(host string, o outcome, raw json.RawMessage) {
	if s.currentTask == nil {
		return
	}
	s.currentTask.record(host, o, raw)
	s.noteHost(host)
	if s.OnHostRecorded != nil {
		s.OnHostRecorded(s.currentTask, host)
	}
}

// noteHost adds host to AllHosts, keeping it sorted, the first time it's
// seen run-wide (across any task). A plain linear scan plus an unconditional
// re-sort on every new host is dead simple and more than fast enough at this
// project's explicit ~10-host target scale (Purpose.md) — not worth a
// membership map or an insertion-sort for that size.
func (s *playbookState) noteHost(host string) {
	for _, h := range s.AllHosts {
		if h == host {
			return
		}
	}
	s.AllHosts = append(s.AllHosts, host)
	sort.Strings(s.AllHosts)
}
