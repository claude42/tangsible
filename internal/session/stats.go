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
	"github.com/rivo/tview"
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

// runStats is what the Hosts tab shows: per-host totals, slowest first, and
// the run's wall-clock time (first task start to last finish).
type runStats struct {
	Hosts []hostStat
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
	for _, play := range state.Plays {
		for _, task := range play.Tasks {
			for host, outcome := range task.Hosts {
				switch {
				case outcome == playbook.OutcomeFailed && !task.Ignored[host]:
					failed[host] = true
				case outcome == playbook.OutcomeUnreachable:
					unreachable[host] = true
				}
				if d, known := uikit.HostDuration(task, host); known {
					totals[host] += d
				}
				if t := task.Started[host]; !t.IsZero() && (first.IsZero() || t.Before(first)) {
					first = t
				}
				if t := task.Finished[host]; t.After(last) {
					last = t
				}
			}
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
	if !first.IsZero() && last.After(first) {
		stats.Wall = last.Sub(first)
	}
	return stats, true
}

// elapsedLabel names the run's own bar, the first row of the Hosts tab.
const elapsedLabel = "elapsed"

// renderHostsTab renders the Hosts tab for a view width cells wide: first a
// bar for the run's total elapsed time (when known), then one bar per host
// with its total time, slowest first - all scaled to the longest, a blank
// line between rows. A failed host's whole line is red, an unreachable one's
// maroon (the tree's own colors); the elapsed bar is in the annotation color.
func renderHostsTab(stats runStats, width int) string {
	type row struct {
		label      string
		total      time.Duration
		labelColor string
		valueColor string
	}
	var rows []row
	if stats.Wall > 0 {
		rows = append(rows, row{elapsedLabel, stats.Wall, uikit.AnnotationColor, uikit.AnnotationColor})
	}
	for _, h := range stats.Hosts {
		r := row{h.Host, h.Total, uikit.KeyHighlightColor, uikit.ValueHighlightColor}
		switch {
		case h.Failed:
			r.labelColor, r.valueColor = uikit.OutcomeFailedColor, uikit.OutcomeFailedColor
		case h.Unreachable:
			r.labelColor, r.valueColor = uikit.OutcomeUnreachableColor, uikit.OutcomeUnreachableColor
		}
		rows = append(rows, r)
	}

	labelWidth, durWidth := 0, 0
	var longest time.Duration
	for _, r := range rows {
		if n := len([]rune(r.label)); n > labelWidth {
			labelWidth = n
		}
		if n := len(uikit.FormatDuration(r.total)); n > durWidth {
			durWidth = n
		}
		if r.total > longest {
			longest = r.total
		}
	}
	barWidth := width - labelWidth - durWidth - 3 // label + " " + dur + " " + bar, 1 spare
	if barWidth < 10 {
		barWidth = 10
	}

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		fraction := 0.0
		if longest > 0 {
			fraction = float64(r.total) / float64(longest)
		}
		pad := strings.Repeat(" ", labelWidth-len([]rune(r.label)))
		fmt.Fprintf(&b, "[%s]%s[-]%s [%s]%*s %s[-]\n",
			r.labelColor, tview.Escape(r.label), pad,
			r.valueColor, durWidth, uikit.FormatDuration(r.total), uikit.BarEighths(fraction, barWidth))
	}
	return b.String()
}
