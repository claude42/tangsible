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

package uikit

import (
	"fmt"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
)

// HostDuration returns how long a task actually took on host - the
// difference between its own v2_runner_on_start dispatch time and its
// outcome-event finish time (design-docs/OwnCallbackPlugin.md,
// design-docs/PerHostTaskTiming.md's own rejected finish-only
// approximation). false if either timestamp is the zero value: a task
// still in flight has no Finished entry yet, and a run log predating this
// app's own bundled callback plugin (or replayed from one) never recorded
// a Started entry at all - both are "unknown," not "zero seconds," per
// this codebase's existing "zero means unknown" convention for
// event-derived timestamps (see TaskNode.StartedAt's own doc comment).
func HostDuration(task *playbook.TaskNode, host string) (time.Duration, bool) {
	started, finished := task.Started[host], task.Finished[host]
	if started.IsZero() || finished.IsZero() {
		return 0, false
	}
	return finished.Sub(started), true
}

// FormatDuration renders d as seconds with one decimal place, always -
// deliberately not the multi-threshold ms/minutes+seconds formatting an
// earlier version of this function used. Live use found that switching
// units made different hosts' durations land in different unit systems
// depending on which side of a threshold they fell on (one host "340ms",
// another "1.2s" for the same task), defeating the actual point of
// showing this at all: comparing hosts at a glance (design-docs/
// PerHostTaskTiming.md's original motivation). One consistent unit is what
// HostAndDurationPrefix's own column alignment below needs too - it can
// only align a single number format, not several.
func FormatDuration(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// DurationLayout is the layout every expanded host row shares for one
// rebuild - computed once (ComputeDurationLayout) and threaded into every
// HostAndDurationPrefix call, mirroring HostColumnLayout's own "computed
// once per rebuild, not per-row" convention (tui_layout.go) so every host
// row's number lines up in the same column instead of trailing at a
// different position depending on that row's own detail text length (the
// concrete problem that prompted this: "right now it's on different
// positions based on the length of the rest of the contents of a host
// line").
type DurationLayout struct {
	// ShowDuration is false when no host anywhere in this run/replay has a
	// known duration at all (state.Started is only ever populated by this
	// app's own bundled callback plugin - a pre-fork run log replayed via
	// revisit/diff never has it, for any host, for the whole run) - so a
	// replay that will never have a number to show reserves no dead
	// column space for one, and HostAndDurationPrefix falls back to the
	// plain "<host>: " shape every host row used before this existed.
	ShowDuration bool
	// HostWidth is the fixed field width HostAndDurationPrefix left-
	// justifies each hostname into, already including one space of
	// breathing room before the "(" that follows - the longest name in
	// allHosts (state.AllHosts, the same run-wide set TaskLabel's own
	// collapsed-row column already aligns against), plus 1.
	HostWidth int
	// SecondsWidth is the fixed field width the "X.Y" figure itself
	// (integer part + '.' + one decimal digit) is right-justified into -
	// wide enough for the largest duration seen anywhere in the run so
	// far, so a later, longer-running task never breaks an earlier one's
	// alignment once discovered (widens only, same "monotonically non-
	// decreasing" convention ComputeHostColumnLayout's own title column
	// already follows).
	SecondsWidth int
}

// ComputeDurationLayout walks every task's every host in state, computing
// the widest duration seen and the longest name in allHosts, or
// DurationLayout{} (ShowDuration false) if no host anywhere has a known
// duration at all. Cheap at this project's target scale (~10 hosts,
// Purpose.md) to redo on every rebuild, same reasoning FlattenRows' own
// doc comment already gives for rebuilding the whole row list from
// scratch every time rather than patching incrementally.
func ComputeDurationLayout(state *playbook.PlaybookState, allHosts []string) DurationLayout {
	maxHostLen := 0
	for _, h := range allHosts {
		if len(h) > maxHostLen {
			maxHostLen = len(h)
		}
	}

	var maxSeconds float64
	anyKnown := false
	for _, play := range state.Plays {
		for _, task := range play.Tasks {
			for host := range task.Hosts {
				d, ok := HostDuration(task, host)
				if !ok {
					continue
				}
				anyKnown = true
				if s := d.Seconds(); s > maxSeconds {
					maxSeconds = s
				}
			}
		}
	}
	if !anyKnown {
		return DurationLayout{}
	}

	intPart, _, _ := strings.Cut(fmt.Sprintf("%.1f", maxSeconds), ".")
	return DurationLayout{
		ShowDuration: true,
		HostWidth:    maxHostLen + 1,
		SecondsWidth: len(intPart) + 2, // "." + one decimal digit
	}
}

// HostAndDurationPrefix builds the column-aligned "<host padded> (<secs
// padded>s): " prefix HostLabel starts each expanded host row with - the
// whole point of DurationLayout, so a slow/flaky host's number actually
// stands out against its neighbors instead of being buried at a different
// column on every row. Falls back to the plain "<host>: " shape (rendering
// exactly as it did before this feature existed) when layout.ShowDuration
// is false; falls back to a same-width blank placeholder, not "", for the
// rare individual host that itself lacks a duration while others in the
// same run have one, so the text that follows still lines up.
func HostAndDurationPrefix(task *playbook.TaskNode, host string, layout DurationLayout) string {
	if !layout.ShowDuration {
		return host + ": "
	}
	durText := strings.Repeat(" ", layout.SecondsWidth+3) // "(" + width + "s)"
	if d, ok := HostDuration(task, host); ok {
		durText = fmt.Sprintf("(%*.1fs)", layout.SecondsWidth, d.Seconds())
	}
	return fmt.Sprintf("%-*s%s: ", layout.HostWidth, host, durText)
}
