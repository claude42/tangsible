package session

import (
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
)

func statsFixture() *playbook.PlaybookState {
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	at := func(sec float64) time.Time { return base.Add(time.Duration(sec * float64(time.Second))) }
	task := func(durs map[string]float64, start float64) *playbook.TaskNode {
		t := &playbook.TaskNode{
			Hosts:    map[string]playbook.Outcome{},
			Started:  map[string]time.Time{},
			Finished: map[string]time.Time{},
		}
		for h, d := range durs {
			t.Hosts[h] = playbook.OutcomeOK
			t.Started[h] = at(start)
			t.Finished[h] = at(start + d)
		}
		return t
	}
	return &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{
		task(map[string]float64{"shunya": 20, "hydra": 1.5, "artha": 30}, 0),
		task(map[string]float64{"shunya": 5.3, "hydra": 2, "artha": 30}, 30),
	}}}}
}

func TestComputeRunStats(t *testing.T) {
	stats, ok := computeRunStats(statsFixture())
	if !ok {
		t.Fatal("expected stats")
	}
	var order []string
	for _, h := range stats.Hosts {
		order = append(order, h.Host)
	}
	if got := strings.Join(order, ","); got != "artha,shunya,hydra" {
		t.Errorf("order = %s, want slowest first: artha,shunya,hydra", got)
	}
	if stats.Hosts[0].Total != 60*time.Second || stats.Hosts[1].Total != 25300*time.Millisecond {
		t.Errorf("totals = %v", stats.Hosts)
	}
	if stats.Wall != 60*time.Second {
		t.Errorf("wall = %v, want 60s", stats.Wall)
	}
}

func TestComputeRunStatsNoDurations(t *testing.T) {
	state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{{
		Hosts: map[string]playbook.Outcome{"h": playbook.OutcomeOK},
	}}}}}
	if _, ok := computeRunStats(state); ok {
		t.Error("a run without recorded durations has no stats")
	}
}

func TestRenderHostsTab(t *testing.T) {
	stats, _ := computeRunStats(statsFixture())
	got := renderHostsTab(stats, 60)
	for _, want := range []string{"artha", "60.0s", "25.3s", "hydra", "elapsed", "████"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Index(got, "artha") > strings.Index(got, "shunya") || strings.Index(got, "shunya") > strings.Index(got, "hydra") {
		t.Errorf("not sorted by duration:\n%s", got)
	}
	if strings.Contains(got, "Time each host") || strings.Contains(got, "Run time") {
		t.Errorf("the description lines are gone, got:\n%s", got)
	}
	if strings.Index(got, "elapsed") > strings.Index(got, "artha") {
		t.Errorf("the elapsed bar comes first:\n%s", got)
	}
}

func TestComputeRunStatsFailures(t *testing.T) {
	state := statsFixture()
	tasks := state.Plays[0].Tasks
	tasks[0].Hosts["hydra"] = playbook.OutcomeFailed
	tasks[1].Hosts["shunya"] = playbook.OutcomeUnreachable
	tasks[0].Hosts["artha"] = playbook.OutcomeFailed
	tasks[0].Ignored = map[string]bool{"artha": true} // ignore_errors: not a failure
	stats, _ := computeRunStats(state)
	got := map[string]hostStat{}
	for _, h := range stats.Hosts {
		got[h.Host] = h
	}
	if !got["hydra"].Failed || got["hydra"].Unreachable {
		t.Errorf("hydra = %+v, want Failed", got["hydra"])
	}
	if got["shunya"].Failed || !got["shunya"].Unreachable {
		t.Errorf("shunya = %+v, want Unreachable", got["shunya"])
	}
	if got["artha"].Failed {
		t.Errorf("artha = %+v, an ignored failure is not a failure", got["artha"])
	}
}

func TestRenderHostsTabColorsAndSpacing(t *testing.T) {
	stats := runStats{Hosts: []hostStat{
		{Host: "a", Total: 3 * time.Second, Failed: true},
		{Host: "b", Total: 2 * time.Second, Unreachable: true},
		{Host: "c", Total: time.Second},
	}}
	got := renderHostsTab(stats, 40)
	for _, want := range []string{"[red]a[-]", "[maroon]b[-]", "[green]c[-]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// a, blank, b, blank, c
	if len(lines) != 5 || lines[1] != "" || lines[3] != "" {
		t.Errorf("want a blank line between hosts, got:\n%q", lines)
	}
}

func TestRenderHostsTabElapsedBar(t *testing.T) {
	stats := runStats{Wall: 10 * time.Second, Hosts: []hostStat{{Host: "a", Total: 5 * time.Second}}}
	got := renderHostsTab(stats, 33) // label 7 + dur 5 + 3 spare -> 18-cell bars
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "elapsed") || !strings.Contains(lines[0], "10.0s") || !strings.Contains(lines[0], strings.Repeat("█", 18)) {
		t.Errorf("elapsed row = %q, want a full-width bar", lines[0])
	}
	if !strings.Contains(lines[2], strings.Repeat("█", 9)) || strings.Contains(lines[2], strings.Repeat("█", 10)) {
		t.Errorf("a host at half the elapsed time gets a half bar, got %q", lines[2])
	}
	if got := renderHostsTab(runStats{Hosts: stats.Hosts}, 33); strings.Contains(got, "elapsed") {
		t.Errorf("no elapsed bar without a known run time, got %q", got)
	}
}
