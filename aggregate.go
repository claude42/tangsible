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
type playbookState struct {
	Plays []*playNode

	pendingPlayName string
	currentPlay     *playNode
	currentTask     *taskNode
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
		}
		if ev.Task != nil {
			s.currentTask = &taskNode{Name: ev.Task.Name, Hosts: map[string]outcome{}}
			s.currentPlay.Tasks = append(s.currentPlay.Tasks, s.currentTask)
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
}
