package session

import (
	"regexp"
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
	plain := tagPatternForTest.ReplaceAllString(got, "")
	for _, want := range []string{"artha", "60.0s", "25.3s", "hydra", "elapsed"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in:\n%s", want, plain)
		}
	}
	if strings.Index(plain, "artha") > strings.Index(plain, "shunya") || strings.Index(plain, "shunya") > strings.Index(plain, "hydra") {
		t.Errorf("not sorted by duration:\n%s", plain)
	}
	if strings.Contains(plain, "Time each host") || strings.Contains(plain, "Run time") {
		t.Errorf("the description lines are gone, got:\n%s", plain)
	}
	if strings.Index(plain, "elapsed") > strings.Index(plain, "artha") {
		t.Errorf("the elapsed bar comes first:\n%s", plain)
	}
	// The host names are drawn inside their bars (inverted), like the Plays tab.
	if !strings.Contains(got, ":lightsteelblue] artha") {
		t.Errorf("the host name should sit inside its bar:\n%s", got)
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
	for _, want := range []string{":red] a", ":maroon] b", ":lightsteelblue] c"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	// leading blank, a, blank, b, blank, c
	if len(lines) != 6 || lines[0] != "" || lines[2] != "" || lines[4] != "" {
		t.Errorf("want a blank line between hosts, got:\n%q", lines)
	}
}

// segment returns the text of the first inverted run that starts with prefix
// (a "[fg:bg]" tag), up to the closing "[-:-]" - the filled part of a bar.
func segment(s, prefix string) string {
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	if j := strings.Index(rest, "[-:-]"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestRenderHostsTabElapsedBar(t *testing.T) {
	stats := runStats{Wall: 10 * time.Second, Hosts: []hostStat{{Host: "a", Total: 5 * time.Second}}}
	got := renderHostsTab(stats, 33) // duration column 5 -> 26-cell bars
	if n := len([]rune(segment(got, "[#ffffff:gray]"))); n != 26 {
		t.Errorf("the elapsed bar should fill all 26 cells, got %d in:\n%s", n, got)
	}
	if n := len([]rune(segment(got, "[#000000:lightsteelblue]"))); n != 13 {
		t.Errorf("a host at half the elapsed time gets half the cells, got %d in:\n%s", n, got)
	}
	if got := renderHostsTab(runStats{Hosts: stats.Hosts}, 33); strings.Contains(got, "elapsed") {
		t.Errorf("no elapsed bar without a known run time, got %q", got)
	}
}

var tagPatternForTest = regexp.MustCompile(`\[[^\]]*\]`)

func playsFixture() *playbook.PlaybookState {
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	at := func(sec float64) time.Time { return base.Add(time.Duration(sec * float64(time.Second))) }
	task := func(host string, start, end float64, o playbook.Outcome) *playbook.TaskNode {
		return &playbook.TaskNode{
			Hosts:    map[string]playbook.Outcome{host: o},
			Started:  map[string]time.Time{host: at(start)},
			Finished: map[string]time.Time{host: at(end)},
		}
	}
	return &playbook.PlaybookState{Plays: []*playbook.PlayNode{
		{Name: "Very long play", Tasks: []*playbook.TaskNode{
			task("h1", 0, 4, playbook.OutcomeOK), task("h2", 1, 21.3, playbook.OutcomeOK),
		}},
		{Name: "", Tasks: []*playbook.TaskNode{task("h1", 22, 25, playbook.OutcomeFailed)}},
		{Name: "Short", Tasks: []*playbook.TaskNode{task("h1", 25, 26, playbook.OutcomeOK)}},
	}}
}

func TestComputeRunStatsPlays(t *testing.T) {
	stats, ok := computeRunStats(playsFixture())
	if !ok {
		t.Fatal("expected stats")
	}
	var got []string
	for _, p := range stats.Plays {
		got = append(got, p.Name)
	}
	if strings.Join(got, "|") != "Very long play|play 2|Short" {
		t.Errorf("plays = %v, want slowest first, an unnamed play numbered", got)
	}
	if stats.Plays[0].Wall != 21300*time.Millisecond {
		t.Errorf("wall = %v, want first start to last finish: 21.3s", stats.Plays[0].Wall)
	}
	if stats.Plays[0].Failed || !stats.Plays[1].Failed {
		t.Errorf("failed flags = %v %v", stats.Plays[0].Failed, stats.Plays[1].Failed)
	}
}

func TestRenderPlaysTab(t *testing.T) {
	stats, _ := computeRunStats(playsFixture())
	got := renderPlaysTab(stats, 60)
	plain := tagPatternForTest.ReplaceAllString(got, "")
	for _, want := range []string{"Very long play", "21.3s", "play 2", "Short"} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "elapsed") {
		t.Errorf("the Plays tab has no elapsed bar:\n%s", plain)
	}
	// The failed play's bar is red; the long play's name sits inside its bar.
	if !strings.Contains(got, ":red]") {
		t.Errorf("a failed play should have a red bar:\n%s", got)
	}
	if !strings.Contains(got, ":lightsteelblue] Very long play") {
		t.Errorf("the name should be inverted inside the bar:\n%s", got)
	}
	// The short play's name runs past its bar: it is drawn plain after it.
	if !strings.Contains(got, "[silver]") {
		t.Errorf("text beyond a short bar should be drawn normally:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimRight(plain, "\n"), "\n") {
		if len([]rune(line)) > 60 {
			t.Errorf("line too wide (%d): %q", len([]rune(line)), line)
		}
	}
}

func TestRenderPlaysTabEmpty(t *testing.T) {
	if got := renderPlaysTab(runStats{}, 60); !strings.Contains(got, "No timing") {
		t.Errorf("got %q", got)
	}
}
