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

// The numbers and text behind the statistics screen ('s',
// livesession_stats.go, design-docs/Stats.md) - no tview, so it is testable
// on its own.
package session

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// hostStat is the time one host spent on tasks over the whole run.
type hostStat struct {
	Host  string
	Total time.Duration
	// Failed: the host had a genuinely failed task (not one ignored via
	// ignore_errors). Unreachable: it was unreachable for some task. Failed
	// wins when both apply; either colors the host's line.
	Failed      bool
	Unreachable bool
}

// playStat is one play's wall-clock time: its first task start to its last
// finish. Plays run one after another, so these add up to about the run time
// (unlike per-host totals). Failed/Unreachable as in hostStat, over every
// host of the play.
type playStat struct {
	Name        string
	Wall        time.Duration
	Failed      bool
	Unreachable bool
	// Start is when the play's first task started, relative to the run's
	// first task start (the Timeline tab).
	Start time.Duration
	first time.Time
}

// taskStat is one task's span within its play: when its first host started,
// relative to the play's first task start, and how long until its last host
// finished. Timed is false when no host has a known start and finish (the
// task is then listed without a bar).
type taskStat struct {
	Name  string
	Start time.Duration
	Wall  time.Duration
	Timed bool
	Color string // the bar's color: the tree's, by the task's worst outcome
}

// playTasks is one play's tasks in execution order, for the Tasks tab. Wall
// is the play's first task start to last finish - the timeline's scale.
type playTasks struct {
	Name  string
	Wall  time.Duration
	Tasks []taskStat
}

// runStats is what the statistics screen shows: per-host totals, slowest
// first, per-play wall time, slowest first, and the run's wall-clock time
// (first task start to last finish).
type runStats struct {
	Hosts []hostStat
	Plays []playStat
	// Timeline has the same plays in playbook order, for the Timeline tab.
	Timeline []playStat
	Wall     time.Duration
	// PlayTasks has the plays that have any timed task, in playbook order.
	PlayTasks []playTasks
	// AllTasks has every timed task of the run, longest wall time first (the
	// All tasks tab).
	AllTasks []taskStat
}

// computeRunStats sums every task's known per-host duration (uikit.
// HostDuration) per host. A host with no known duration at all (a run log
// from before durations were recorded) is left out; ok is false when no host
// has one. Sorted by total, slowest first, then by name.
func computeRunStats(state *playbook.PlaybookState) (runStats, bool) {
	totals := map[string]time.Duration{}
	failed, unreachable := map[string]bool{}, map[string]bool{}
	var first, last time.Time
	var plays []playStat
	var playTaskList []playTasks
	for i, play := range state.Plays {
		var playFirst, playLast time.Time
		ps := playStat{Name: play.Name}
		if ps.Name == "" {
			ps.Name = fmt.Sprintf("play %d", i+1)
		}
		for _, task := range play.Tasks {
			for host, outcome := range task.Hosts {
				switch {
				case outcome == playbook.OutcomeFailed && !task.Ignored[host]:
					failed[host] = true
					ps.Failed = true
				case outcome == playbook.OutcomeUnreachable:
					unreachable[host] = true
					ps.Unreachable = true
				}
				if d, known := uikit.HostDuration(task, host); known {
					totals[host] += d
				}
				if t := task.Started[host]; !t.IsZero() {
					if first.IsZero() || t.Before(first) {
						first = t
					}
					if playFirst.IsZero() || t.Before(playFirst) {
						playFirst = t
					}
				}
				if t := task.Finished[host]; !t.IsZero() {
					if t.After(last) {
						last = t
					}
					if t.After(playLast) {
						playLast = t
					}
				}
			}
		}
		if pt, ok := playTaskStats(play, ps.Name); ok {
			playTaskList = append(playTaskList, pt)
		}
		if !playFirst.IsZero() && playLast.After(playFirst) {
			ps.Wall = playLast.Sub(playFirst)
			ps.first = playFirst
			plays = append(plays, ps)
		}
	}
	if len(totals) == 0 {
		return runStats{}, false
	}
	stats := runStats{}
	for host, total := range totals {
		stats.Hosts = append(stats.Hosts, hostStat{Host: host, Total: total, Failed: failed[host], Unreachable: unreachable[host]})
	}
	sort.Slice(stats.Hosts, func(i, j int) bool {
		if stats.Hosts[i].Total != stats.Hosts[j].Total {
			return stats.Hosts[i].Total > stats.Hosts[j].Total
		}
		return stats.Hosts[i].Host < stats.Hosts[j].Host
	})
	stats.Timeline = append([]playStat(nil), plays...)
	for i := range stats.Timeline {
		stats.Timeline[i].Start = stats.Timeline[i].first.Sub(first)
	}
	sort.SliceStable(plays, func(i, j int) bool { return plays[i].Wall > plays[j].Wall })
	stats.Plays = plays
	stats.PlayTasks = playTaskList
	for _, pt := range playTaskList {
		for _, t := range pt.Tasks {
			if t.Timed {
				stats.AllTasks = append(stats.AllTasks, t)
			}
		}
	}
	sort.SliceStable(stats.AllTasks, func(i, j int) bool { return stats.AllTasks[i].Wall > stats.AllTasks[j].Wall })
	if !first.IsZero() && last.After(first) {
		stats.Wall = last.Sub(first)
	}
	return stats, true
}

// playTaskStats lays out play's tasks on a timeline starting at its first
// task start; ok is false when no task has a known span.
func playTaskStats(play *playbook.PlayNode, name string) (playTasks, bool) {
	type span struct{ first, last time.Time }
	spans := make([]span, len(play.Tasks))
	var playFirst, playLast time.Time
	for i, task := range play.Tasks {
		for host := range task.Hosts {
			st, fin := task.Started[host], task.Finished[host]
			if st.IsZero() || fin.IsZero() {
				continue
			}
			if spans[i].first.IsZero() || st.Before(spans[i].first) {
				spans[i].first = st
			}
			if fin.After(spans[i].last) {
				spans[i].last = fin
			}
		}
		if f := spans[i].first; !f.IsZero() && (playFirst.IsZero() || f.Before(playFirst)) {
			playFirst = f
		}
		if l := spans[i].last; l.After(playLast) {
			playLast = l
		}
	}
	if playFirst.IsZero() {
		return playTasks{}, false
	}
	pt := playTasks{Name: name, Wall: playLast.Sub(playFirst)}
	for i, task := range play.Tasks {
		ts := taskStat{Name: task.Name, Color: taskColor(task)}
		if !spans[i].first.IsZero() {
			ts.Timed = true
			ts.Start = spans[i].first.Sub(playFirst)
			ts.Wall = spans[i].last.Sub(spans[i].first)
		}
		pt.Tasks = append(pt.Tasks, ts)
	}
	return pt, true
}

// taskColor is the tree's color for the task's worst outcome across its
// hosts: failed, unreachable, an ignored failure, changed, ok, skipped.
func taskColor(task *playbook.TaskNode) string {
	var failed, unreachable, ignored, changed, ok, skipped bool
	for host, o := range task.Hosts {
		switch {
		case o == playbook.OutcomeFailed && task.Ignored[host]:
			ignored = true
		case o == playbook.OutcomeFailed:
			failed = true
		case o == playbook.OutcomeUnreachable:
			unreachable = true
		case o == playbook.OutcomeChanged:
			changed = true
		case o == playbook.OutcomeOK:
			ok = true
		case o == playbook.OutcomeSkipped:
			skipped = true
		}
	}
	switch {
	case failed:
		return uikit.ColorTag(playbook.OutcomeFailed)
	case unreachable:
		return uikit.ColorTag(playbook.OutcomeUnreachable)
	case ignored:
		return uikit.IgnoredColor
	case changed:
		return uikit.ColorTag(playbook.OutcomeChanged)
	case ok:
		return uikit.ColorTag(playbook.OutcomeOK)
	case skipped:
		return uikit.ColorTag(playbook.OutcomeSkipped)
	}
	return uikit.NothingReportedColor
}

// elapsedLabel names the run's own bar, the first row of the Hosts tab.
const elapsedLabel = "elapsed"

// statRow is one bar of a statistics tab: its label (drawn inside the bar), its
// value, and the bar's color.
type statRow struct {
	label string
	total time.Duration
	color string
}

// statusColor is a row's bar color: red when it failed, maroon when
// unreachable, else normal.
func statusColor(failed, unreachable bool, normal string) string {
	switch {
	case failed:
		return uikit.OutcomeFailedColor
	case unreachable:
		return uikit.OutcomeUnreachableColor
	}
	return normal
}

// renderBars renders rows for a view width cells wide, shared by every tab:
// each row is a bar with its label drawn inside it (uikit.LabeledBar -
// inverted on the filled cells, plain where it runs past the bar) and the
// duration in a right-aligned column after it. Bars share one scale, the
// longest row filling the bar area; a blank line separates rows.
func renderBars(rows []statRow, width int) string {
	durWidth := 0
	var longest time.Duration
	for _, r := range rows {
		if n := len(uikit.FormatDuration(r.total)); n > durWidth {
			durWidth = n
		}
		if r.total > longest {
			longest = r.total
		}
	}
	barWidth := width - durWidth - 2 // bar + " " + dur, 1 spare
	if barWidth < 20 {
		barWidth = 20
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		fraction := 0.0
		if longest > 0 {
			fraction = float64(r.total) / float64(longest)
		}
		fmt.Fprintf(&b, "%s [%s]%*s[-]\n",
			uikit.LabeledBar(r.label, fraction, barWidth, r.color, uikit.ContrastTextColor(r.color), uikit.StandardTextColor),
			r.color, durWidth, uikit.FormatDuration(r.total))
	}
	return b.String()
}

// renderHostsTab renders the Hosts tab: first a bar for the run's total
// elapsed time (gray, when known), then one bar per host with its total task
// time, slowest first. A failed host's bar is red, an unreachable one's maroon
// (the tree's own colors).
func renderHostsTab(stats runStats, width int) string {
	var rows []statRow
	if stats.Wall > 0 {
		rows = append(rows, statRow{elapsedLabel, stats.Wall, uikit.AnnotationColor})
	}
	for _, h := range stats.Hosts {
		rows = append(rows, statRow{h.Host, h.Total, statusColor(h.Failed, h.Unreachable, uikit.ValueHighlightColor)})
	}
	return renderBars(rows, width)
}

// renderPlaysTab renders the Plays tab: one bar per play with its wall time,
// slowest first. Red when a host failed in the play, maroon when one was
// unreachable. No elapsed bar here: the plays are the parts of it.
func renderPlaysTab(stats runStats, width int) string {
	if len(stats.Plays) == 0 {
		return "No timing was recorded for the plays."
	}
	var rows []statRow
	for _, p := range stats.Plays {
		rows = append(rows, statRow{p.Name, p.Wall, statusColor(p.Failed, p.Unreachable, uikit.ValueHighlightColor)})
	}
	return renderBars(rows, width)
}

// renderAllTasksTab renders the All tasks tab: one bar per task of the whole
// run with its wall time (first host start to last host finish), longest
// first, colored as in the tree by the task's worst outcome. Tasks without
// timing are left out.
func renderAllTasksTab(stats runStats, width int) string {
	if len(stats.AllTasks) == 0 {
		return "No timing was recorded for the tasks."
	}
	rows := make([]statRow, len(stats.AllTasks))
	for i, t := range stats.AllTasks {
		rows[i] = statRow{t.Name, t.Wall, t.Color}
	}
	return renderBars(rows, width)
}

// timelineRow is one bar of a timeline tab: where it starts and how long it
// lasts, relative to the scale the whole tab shares.
type timelineRow struct {
	label       string
	start, wall time.Duration
	timed       bool
	color       string
}

// renderTimeline renders rows as a timeline, scale (the full width of the bar
// area) being the span they all fall within: each bar starts where its row
// began and is as long as its wall time. The name is inside the bar or, for a
// late short one, right-aligned to the end of the bar area (uikit.SpanBar);
// the duration is in the right column. The first row is a gray full-width
// "elapsed" bar for scale itself, the ruler the others are read against. An untimed row is a bare name. Blank
// lines separate rows.
func renderTimeline(rows []timelineRow, scale time.Duration, width int) string {
	if scale > 0 {
		rows = append([]timelineRow{{elapsedLabel, 0, scale, true, uikit.AnnotationColor}}, rows...)
	}
	durWidth := 0
	for _, r := range rows {
		if r.timed {
			if n := len(uikit.FormatDuration(r.wall)); n > durWidth {
				durWidth = n
			}
		}
	}
	barWidth := width - durWidth - 2
	if barWidth < 20 {
		barWidth = 20
	}
	var b strings.Builder
	b.WriteString("\n")
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		if !r.timed {
			b.WriteString(uikit.SpanBar(r.label, 0, 0, barWidth, r.color, "", uikit.StandardTextColor) + "\n")
			continue
		}
		var start, length float64
		if scale > 0 {
			start = float64(r.start) / float64(scale)
			length = float64(r.wall) / float64(scale)
		}
		fmt.Fprintf(&b, "%s [%s]%*s[-]\n",
			uikit.SpanBar(r.label, start, length, barWidth, r.color, uikit.ContrastTextColor(r.color), uikit.StandardTextColor),
			r.color, durWidth, uikit.FormatDuration(r.wall))
	}
	return b.String()
}

// renderTasksTab renders one play's timeline for the Tasks tab: a bar per
// task in execution order, starting where the task began and as long as its
// wall time (first host start to last host finish); the play's own wall time
// fills the bar area.
func renderTasksTab(p playTasks, width int) string {
	rows := make([]timelineRow, len(p.Tasks))
	for i, t := range p.Tasks {
		rows[i] = timelineRow{t.Name, t.Start, t.Wall, t.Timed, t.Color}
	}
	return renderTimeline(rows, p.Wall, width)
}

// renderTimelineTab renders the Timeline tab: the playbook's plays in
// execution order, each bar starting when the play began and as long as its
// wall time, the run's elapsed time filling the bar area. Colored as on the
// Plays tab.
func renderTimelineTab(stats runStats, width int) string {
	if len(stats.Timeline) == 0 || stats.Wall <= 0 {
		return "No timing was recorded for the plays."
	}
	rows := make([]timelineRow, len(stats.Timeline))
	for i, p := range stats.Timeline {
		rows[i] = timelineRow{p.Name, p.Start, p.Wall, true, statusColor(p.Failed, p.Unreachable, uikit.ValueHighlightColor)}
	}
	return renderTimeline(rows, stats.Wall, width)
}
