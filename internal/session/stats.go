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
}

// runStats is what the statistics screen shows: per-host totals, slowest
// first, per-play wall time, slowest first, and the run's wall-clock time
// (first task start to last finish).
type runStats struct {
	Hosts []hostStat
	Plays []playStat
	Wall  time.Duration
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
		if !playFirst.IsZero() && playLast.After(playFirst) {
			ps.Wall = playLast.Sub(playFirst)
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
	sort.SliceStable(plays, func(i, j int) bool { return plays[i].Wall > plays[j].Wall })
	stats.Plays = plays
	if !first.IsZero() && last.After(first) {
		stats.Wall = last.Sub(first)
	}
	return stats, true
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
