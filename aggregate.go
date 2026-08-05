package main

// outcome is a host's result for one task. unreachable hosts fold into
// outcomeFailed for now — see TUI.md.
type outcome int

const (
	outcomeOK outcome = iota
	outcomeChanged
	outcomeSkipped
	outcomeFailed
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
	default:
		return "?"
	}
}

type taskNode struct {
	Name      string
	HostOrder []string
	Hosts     map[string]outcome
}

func (t *taskNode) record(host string, o outcome) {
	if _, seen := t.Hosts[host]; !seen {
		t.HostOrder = append(t.HostOrder, host)
	}
	t.Hosts[host] = o
}

func (t *taskNode) counts() (ok, changed, skipped, failed int) {
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
			s.currentTask = &taskNode{Name: ev.Task.Name, Hosts: map[string]outcome{}}
			s.currentPlay.Tasks = append(s.currentPlay.Tasks, s.currentTask)
			if s.OnTaskAdded != nil {
				s.OnTaskAdded(s.currentPlay, s.currentTask)
			}
		}

	case "v2_runner_on_ok":
		for host, r := range ev.Hosts {
			o := outcomeOK
			if r.Changed {
				o = outcomeChanged
			}
			s.recordHost(host, o)
		}

	case "v2_runner_on_skipped":
		for host := range ev.Hosts {
			s.recordHost(host, outcomeSkipped)
		}

	case "v2_runner_on_failed", "v2_runner_on_unreachable":
		for host := range ev.Hosts {
			s.recordHost(host, outcomeFailed)
		}
	}
}

func (s *playbookState) recordHost(host string, o outcome) {
	if s.currentTask == nil {
		return
	}
	s.currentTask.record(host, o)
	if s.OnHostRecorded != nil {
		s.OnHostRecorded(s.currentTask, host)
	}
}
