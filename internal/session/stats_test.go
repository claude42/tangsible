package session

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
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
	// The host names are drawn inside their bars (inverted), like the Slowest plays tab.
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
	// legend, a, blank, b, blank, c (the blank lines carry the average line)
	blank := func(i int) bool { return strings.TrimSpace(tagPatternForTest.ReplaceAllString(lines[i], "")) == "┆" }
	if len(lines) != 6 || !strings.Contains(lines[0], "average 2.0s") || !blank(2) || !blank(4) {
		t.Errorf("want a blank line between hosts, got:\n%q", lines)
	}
}

func TestRenderBarsAverageLine(t *testing.T) {
	plain := func(s string) string { return tagPatternForTest.ReplaceAllString(s, "") }
	stats := runStats{Wall: 100 * time.Second, Hosts: []hostStat{
		{Host: "a", Total: 30 * time.Second},
		{Host: "b", Total: 10 * time.Second},
	}}
	got := renderHostsTab(stats, 52) // duration column 6 -> 44-cell bars, 100s = 44 cells
	if !strings.Contains(plain(got), "average 20.0s") {
		t.Errorf("the legend names the average of the hosts, not the elapsed bar:\n%s", plain(got))
	}
	// 20s of 100s on 44 cells: column 8, on every line of the bar area.
	for i, line := range strings.Split(strings.TrimRight(plain(got), "\n"), "\n")[1:] {
		if r := []rune(line); len(r) < 9 || r[8] != '┆' {
			t.Errorf("line %d has no marker: %q", i, line)
		}
	}
	if one := renderHostsTab(runStats{Hosts: stats.Hosts[:1]}, 52); strings.Contains(one, "┆") {
		t.Errorf("one row has no average line:\n%s", one)
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
		t.Errorf("the Slowest plays tab has no elapsed bar:\n%s", plain)
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

// tasksFixture: one play, three tasks on a 10s timeline - 0-4s (ok), 4-5s
// (changed, on two hosts that overlap) and 5-10s (one host failed).
func tasksFixture() *playbook.PlaybookState {
	base := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	at := func(sec float64) time.Time { return base.Add(time.Duration(sec * float64(time.Second))) }
	mk := func(name string, o playbook.Outcome, spans map[string][2]float64) *playbook.TaskNode {
		t := &playbook.TaskNode{Name: name, Hosts: map[string]playbook.Outcome{},
			Started: map[string]time.Time{}, Finished: map[string]time.Time{}}
		for h, s := range spans {
			t.Hosts[h] = o
			t.Started[h], t.Finished[h] = at(s[0]), at(s[1])
		}
		return t
	}
	return &playbook.PlaybookState{Plays: []*playbook.PlayNode{
		{Name: "first", Tasks: []*playbook.TaskNode{
			mk("one", playbook.OutcomeOK, map[string][2]float64{"a": {0, 4}}),
			mk("two", playbook.OutcomeChanged, map[string][2]float64{"a": {4, 4.5}, "b": {4.2, 5}}),
			mk("three", playbook.OutcomeFailed, map[string][2]float64{"a": {5, 10}}),
			{Name: "untimed", Hosts: map[string]playbook.Outcome{"a": playbook.OutcomeOK}},
		}},
		{Tasks: []*playbook.TaskNode{{Name: "no timing", Hosts: map[string]playbook.Outcome{"a": playbook.OutcomeOK}}}},
	}}
}

func TestComputeRunStatsTasks(t *testing.T) {
	stats, ok := computeRunStats(tasksFixture())
	if !ok {
		t.Fatal("expected stats")
	}
	if len(stats.PlayTasks) != 1 || stats.PlayTasks[0].Name != "first" {
		t.Fatalf("PlayTasks = %+v, want only the timed play", stats.PlayTasks)
	}
	p := stats.PlayTasks[0]
	if p.Wall != 10*time.Second || len(p.Tasks) != 4 {
		t.Fatalf("play = %+v", p)
	}
	two := p.Tasks[1]
	// first host start to last host finish: 4.0s .. 5.0s
	if two.Start != 4*time.Second || two.Wall != time.Second || !two.Timed {
		t.Errorf("two = %+v", two)
	}
	if p.Tasks[3].Timed {
		t.Errorf("a task without timing must not be timed: %+v", p.Tasks[3])
	}
	if p.Tasks[2].Color != "red" || p.Tasks[1].Color == p.Tasks[0].Color {
		t.Errorf("colors = %q %q %q, want the tree's per outcome", p.Tasks[0].Color, p.Tasks[1].Color, p.Tasks[2].Color)
	}
}

func TestRenderTasksTab(t *testing.T) {
	stats, _ := computeRunStats(tasksFixture())
	got := renderTasksTab(stats.PlayTasks[0], 60)
	plain := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(got, "")
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	// leading blank, elapsed, blank, one, blank, two, blank, three, blank, untimed
	if len(lines) != 10 || lines[0] != "" || lines[2] != "" {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.HasPrefix(lines[1], " elapsed") || !strings.HasSuffix(lines[1], "10.0s") {
		t.Errorf("elapsed row = %q, want the play's 10.0s", lines[1])
	}
	if !strings.HasSuffix(lines[3], "4.0s") || !strings.HasSuffix(lines[7], "5.0s") {
		t.Errorf("durations missing: %q", lines)
	}
	// "one" starts at the left edge; "three" starts halfway.
	if !strings.HasPrefix(lines[3], " one") || strings.Index(lines[7], "three") < 20 {
		t.Errorf("offsets wrong: %q", lines)
	}
	if strings.TrimSpace(lines[9]) != "untimed" {
		t.Errorf("untimed row = %q, want a bare name", lines[9])
	}
}

func TestComputeRunStatsTimeline(t *testing.T) {
	stats, _ := computeRunStats(playsFixture())
	if len(stats.Timeline) != 3 {
		t.Fatalf("Timeline = %+v", stats.Timeline)
	}
	// playbook order (not slowest first), starts relative to the run's start
	want := []struct {
		name  string
		start time.Duration
	}{{"Very long play", 0}, {"play 2", 22 * time.Second}, {"Short", 25 * time.Second}}
	for i, w := range want {
		if got := stats.Timeline[i]; got.Name != w.name || got.Start != w.start {
			t.Errorf("Timeline[%d] = %s @ %v, want %s @ %v", i, got.Name, got.Start, w.name, w.start)
		}
	}
}

func TestRenderTimelineTab(t *testing.T) {
	stats, _ := computeRunStats(playsFixture())
	plain := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(renderTimelineTab(stats, 60), "")
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	// leading blank, elapsed, blank, play, blank, play, blank, play
	if len(lines) != 8 || lines[0] != "" {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.HasPrefix(lines[1], " elapsed") || !strings.HasSuffix(lines[1], uikit.FormatDuration(stats.Wall)) {
		t.Errorf("elapsed row = %q, want the run's elapsed time", lines[1])
	}
	if !strings.HasPrefix(lines[3], " Very long play") || !strings.HasSuffix(lines[3], "21.3s") {
		t.Errorf("first row = %q", lines[3])
	}
	if strings.Index(lines[5], "play 2") < 35 {
		t.Errorf("play 2 should start late: %q", lines[5])
	}
	if got := renderTimelineTab(runStats{}, 60); !strings.Contains(got, "No timing") {
		t.Errorf("empty = %q", got)
	}
}

func TestRenderAllTasksTab(t *testing.T) {
	stats, _ := computeRunStats(tasksFixture())
	var names []string
	for _, task := range stats.AllTasks {
		names = append(names, task.Name)
	}
	if got := strings.Join(names, ","); got != "three,one,two" {
		t.Fatalf("AllTasks = %s, want the timed tasks longest first, the untimed one left out", got)
	}
	plain := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(renderAllTasksTab(stats, 60), "")
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	// leading blank, three, blank, one, blank, two
	if len(lines) != 6 || !strings.HasSuffix(lines[1], "5.0s") || !strings.HasSuffix(lines[5], "1.0s") {
		t.Errorf("lines = %q", lines)
	}
	if got := renderAllTasksTab(runStats{}, 60); !strings.Contains(got, "No timing") {
		t.Errorf("empty = %q", got)
	}
}

func TestWriteTimingCSV(t *testing.T) {
	state := statsFixture()
	state.Plays[0].Name = `Play, "one"`
	state.Plays[0].Tasks[0].Name = "first"
	state.Plays[0].Tasks[0].HostOrder = []string{"hydra", "artha"}
	state.Plays[0].Tasks[0].Hosts["artha"] = playbook.OutcomeChanged
	var b strings.Builder
	if err := writeTimingCSV(&b, state); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if lines[0] != "host,play,task,start time,end time,result" {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], `hydra,"Play, ""one""",first,`) || !strings.HasSuffix(lines[1], ",OK") {
		t.Errorf("first record = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "artha,") || !strings.HasSuffix(lines[2], ",Changed") {
		t.Errorf("second record = %q", lines[2])
	}
}

func TestTimingCSVFileName(t *testing.T) {
	s := &liveSession{targetPlaybook: "site.yml", passthroughArgs: []string{"-i", "hosts"}}
	s.startedAt = time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	got := s.timingCSVFileName()
	if !strings.HasSuffix(got, "_run_site_-i_hosts.csv") {
		t.Errorf("name = %q", got)
	}
	s.statsCSVName = "night_run.csv"
	if got := s.timingCSVFileName(); got != "night_run.csv" {
		t.Errorf("a revisited run's name = %q", got)
	}
}

func TestCSVTimeFormat(t *testing.T) {
	ts := time.Date(2026, 11, 21, 18, 0, 23, 567_000_000, time.Local)
	if got := csvTime(ts); got != "2026-11-21 18:00:23" {
		t.Errorf("csvTime = %q", got)
	}
	if csvTime(time.Time{}) != "" {
		t.Error("a zero time must be an empty field")
	}
}
