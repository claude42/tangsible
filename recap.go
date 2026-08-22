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

// Implements the recap section (design-docs/Recap.md): a per-host
// ansible-style summary line ("hostname : ok=159  changed=94  ...")
// appended below the tree once a run finishes, expandable into its own
// non-empty outcome categories, each expandable into the individual
// tasks that landed in it. Deliberately built entirely on data
// playbookState already tracks (a scan over Plays/Tasks/Hosts) - no
// aggregate.go changes needed, and no rescued/ignored categories: those
// aren't derivable per-task from this app's own event consumption (a
// rescued task reports as an entirely ordinary v2_runner_on_ok, and
// ignore_errors is never included in the jsonl callback's own emitted
// per-task JSON - confirmed by reading ansible.posix.jsonl's own source
// directly), and were explicitly descoped rather than shown as
// count-only fields with no drill-down to back them up.
package main

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// recapCategory is one non-empty outcome bucket for one host - "non-empty"
// because recapForHost only ever includes a category here when it has at
// least one task, per design-docs/Recap.md's own "if there's zero tasks
// in a category then don't even render that line" decision.
type recapCategory struct {
	Label   string
	Outcome outcome
	Tasks   []*taskNode
}

// recapHostSummary is one host's own recap: every count (including zero
// ones - the summary line itself always shows all five, matching
// ansible's own recap line) plus the non-empty categories available to
// expand into.
type recapHostSummary struct {
	OK, Changed, Unreachable, Failed, Skipped int
	Categories                                []recapCategory
}

// recapForHost scans every task across every play for this one host's
// own outcome, in run order - the same data taskLabel/hostLabel already
// read, just tallied per host instead of per task. Cheap at this
// project's own ~10-host/handful-of-hundred-task target scale, computed
// fresh on every rebuild rather than tracked incrementally, matching
// aggregate.go's own "no second source of truth" convention for counts.
func recapForHost(state *playbookState, host string) recapHostSummary {
	var ok, changed, unreachable, failed, skipped []*taskNode
	for _, play := range state.Plays {
		for _, task := range play.Tasks {
			o, present := task.Hosts[host]
			if !present {
				continue
			}
			switch o {
			case outcomeOK:
				ok = append(ok, task)
			case outcomeChanged:
				changed = append(changed, task)
			case outcomeUnreachable:
				unreachable = append(unreachable, task)
			case outcomeFailed:
				failed = append(failed, task)
			case outcomeSkipped:
				skipped = append(skipped, task)
			}
		}
	}

	s := recapHostSummary{
		OK:          len(ok),
		Changed:     len(changed),
		Unreachable: len(unreachable),
		Failed:      len(failed),
		Skipped:     len(skipped),
	}
	for _, c := range []recapCategory{
		{"ok", outcomeOK, ok},
		{"skipped", outcomeSkipped, skipped},
		{"changed", outcomeChanged, changed},
		{"unreachable", outcomeUnreachable, unreachable},
		{"failed", outcomeFailed, failed},
	} {
		if len(c.Tasks) > 0 {
			s.Categories = append(s.Categories, c)
		}
	}
	return s
}

// recapHostRowID/recapCategoryRowID/recapTaskRowID identify the recap
// section's own three row kinds - separate types from hostRowID (rather
// than reusing it for recapTaskRowID, even though both ultimately mean
// "this task, this host") because a task's own row can legitimately
// appear twice at once in currentRows once frozen - once as an expanded
// host row in the live tree above, once as a recap task line below -
// and sharing one identity between them would make rebuild()'s own
// selection-restoration ambiguous about which occurrence currentID
// actually refers to.
type recapHostRowID string
type recapCategoryRowID struct {
	host    string
	outcome outcome
}
type recapTaskRowID struct {
	host string
	task *taskNode
}

// recapHeadingRowID identifies one of the recap section's own four
// leading decoration rows (a blank spacer, "Summary", its "===="
// underline, another blank spacer) - four distinct values of the same
// named type, rather than reusing statusDividerRowID's own empty-struct
// sentinel for all of them: that type's own zero size makes every
// instance compare equal, which would make rebuild()'s identity-based
// selection-restoration unable to tell these apart if the cursor ever
// happened to be sitting on one of them when a rebuild ran. None of
// these four rows carries a selected callback - like
// statusRowID/statusDividerRowID, the cursor can still be moved onto one
// by plain arrow-key navigation, but Enter does nothing there.
type recapHeadingRowID int

const (
	recapDividerBeforeHeading recapHeadingRowID = iota
	recapHeadingRow
	recapHeadingUnderlineRow
	recapDividerAfterHeading
)

// recapHeadingText is the plain "Summary" heading (design-docs/Recap.md)
// shown right above the recap section - white bold, matching
// playRowText's own heading-like styling elsewhere in this tree rather
// than introducing a new color with no outcome to justify it.
const recapHeadingText = "Summary"

func recapHeadingRowText() string {
	return "[white::b]" + recapHeadingText + "[-::-]"
}

// recapHeadingUnderlineRowText underlines recapHeadingText with a run of
// "=" of the identical length - the same "bold heading, plain "="
// underline of the same length, both one color" convention
// sectionLabel's own section headers already use in the drill-down view,
// translated to a plain tree row here since a tree row can't hold
// sectionLabel's own embedded newlines the way a TextView can.
func recapHeadingUnderlineRowText() string {
	return "[white]" + strings.Repeat("=", len(recapHeadingText)) + "[-]"
}

// recapColumnWidths is the recap section's own per-column alignment,
// computed once across every host (recapComputeColumnWidths) rather than
// per row: a single host's own summary line has no way to know how wide
// its neighbors' hostnames or counts are, but the whole point of
// column alignment is that every row agrees on the same widths.
type recapColumnWidths struct {
	Host                                      int
	OK, Changed, Unreachable, Failed, Skipped int
}

// recapComputeColumnWidths scans every host's own recap once (reusing
// recapForHost - cheap at this project's own target scale, see its own
// doc comment) to find the widest hostname and the widest digit count
// each of the five fields ever needs, so every host's summary line can
// right-align its numbers and left-align its hostname to the identical
// columns - matching ansible's own real recap output, which does the
// same per-field alignment across hosts rather than sizing each line to
// its own content.
func recapComputeColumnWidths(state *playbookState) recapColumnWidths {
	var w recapColumnWidths
	digits := func(n int) int { return len(fmt.Sprintf("%d", n)) }
	for _, host := range state.AllHosts {
		if l := len([]rune(host)); l > w.Host {
			w.Host = l
		}
		s := recapForHost(state, host)
		if l := digits(s.OK); l > w.OK {
			w.OK = l
		}
		if l := digits(s.Changed); l > w.Changed {
			w.Changed = l
		}
		if l := digits(s.Unreachable); l > w.Unreachable {
			w.Unreachable = l
		}
		if l := digits(s.Failed); l > w.Failed {
			w.Failed = l
		}
		if l := digits(s.Skipped); l > w.Skipped {
			w.Skipped = l
		}
	}
	return w
}

// recapHostRowText renders one host's own summary line, hostname
// left-padded and each count right-padded to w's own columns so every
// host's line lines up - and each "label=N" segment colored via
// colorTag by its own outcome (green ok, yellow changed, ...), rather
// than picking one dominant color for the whole line, so the same
// "color signals outcome" convention this app uses everywhere else
// applies per field here too. selected inverts each segment's own color
// to a background (black bold text on it) instead of a foreground - the
// same "outcome color becomes a background under the cursor" convention
// taskLabel/hostLabel already use, just applied per segment here rather
// than per hostname. The hostname portion itself gets the plain light
// gray "this is the identifying label" background every other selected
// row's own title/hostname already uses. No neutral, unstyled gap
// anywhere on the selected line - the same rule taskLabel's own selected
// rendering already follows for its hostname segments - each segment's
// own leading two-space gap is folded into its own color block rather
// than left plain between tags, so the row reads as one continuous
// highlight instead of colored blocks with visible holes between them.
func recapHostRowText(host string, s recapHostSummary, w recapColumnWidths, selected bool) string {
	hostPadded := host + strings.Repeat(" ", w.Host-len([]rune(host)))
	if selected {
		// firstSeg has no leading gap of its own - the title already ends
		// in " : ", so folding a gap onto ok too (like every later
		// segment) would double it up against that trailing space,
		// visibly shifting the "ok=" column by one space compared to the
		// unselected rendering right above/below it.
		firstSeg := func(label string, width, n int, o outcome) string {
			return fmt.Sprintf("[%s:%s:b]%s=%*d[-:-:-]", pureBlack, colorTag(o), label, width, n)
		}
		seg := func(label string, width, n int, o outcome) string {
			return fmt.Sprintf("[%s:%s:b]  %s=%*d[-:-:-]", pureBlack, colorTag(o), label, width, n)
		}
		return fmt.Sprintf("[%s:lightgray:b]%s : [-:-:-]%s%s%s%s%s",
			pureBlack, tview.Escape(hostPadded),
			firstSeg("ok", w.OK, s.OK, outcomeOK),
			seg("skipped", w.Skipped, s.Skipped, outcomeSkipped),
			seg("changed", w.Changed, s.Changed, outcomeChanged),
			seg("unreachable", w.Unreachable, s.Unreachable, outcomeUnreachable),
			seg("failed", w.Failed, s.Failed, outcomeFailed),
		)
	}
	seg := func(label string, width, n int, o outcome) string {
		return fmt.Sprintf("[%s]%s=%*d[-]", colorTag(o), label, width, n)
	}
	return fmt.Sprintf("[white::b]%s[-::-] : %s  %s  %s  %s  %s",
		tview.Escape(hostPadded),
		seg("ok", w.OK, s.OK, outcomeOK),
		seg("skipped", w.Skipped, s.Skipped, outcomeSkipped),
		seg("changed", w.Changed, s.Changed, outcomeChanged),
		seg("unreachable", w.Unreachable, s.Unreachable, outcomeUnreachable),
		seg("failed", w.Failed, s.Failed, outcomeFailed),
	)
}

// recapCategoryRowText renders one "  label (N)" line under a host,
// colored via colorTag the same way every other outcome-colored row in
// this app already is.
func recapCategoryRowText(c recapCategory, selected bool) string {
	line := fmt.Sprintf("  %s (%d)", c.Label, len(c.Tasks))
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", pureBlack, line)
	}
	return fmt.Sprintf("[%s]%s[-]", colorTag(c.Outcome), line)
}

// recapTaskRowText renders one task line under a category - the task's
// own name (already "role : task name" when role-sourced, straight from
// task.Name - see source.go/CLAUDE.md's own note that this is how a real
// event's task.name already renders) plus outcomeDetail's own
// parenthesized message/output, the identical detail hostLabel shows for
// the same (task, host) pair in the live tree.
func recapTaskRowText(task *taskNode, host string, o outcome, selected bool) string {
	line := fmt.Sprintf("    %s%s", tview.Escape(task.Name), tview.Escape(outcomeDetail(task, host)))
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", pureBlack, line)
	}
	return fmt.Sprintf("[%s]%s[-]", colorTag(o), line)
}

// flattenRecapRows builds the recap section's own rows - appended after
// the trailing status rows once a run has finished (rebuild), one
// summary row per host (state.AllHosts, already alphabetically sorted),
// expandable into its own non-empty categories, each expandable into one
// row per task. Deliberately ignores the tree's own active filter -
// unlike the long chronological tree above it, the recap is already a
// short, complete index of the whole run, so narrowing it the same way
// wasn't judged worth the added interaction (design-docs/Recap.md never
// asked for it either).
func flattenRecapRows(state *playbookState, hostExpanded map[string]bool, categoryExpanded map[recapCategoryRowID]bool, showOutput func(task *taskNode, host string)) []row {
	widths := recapComputeColumnWidths(state)
	var rows []row
	for _, host := range state.AllHosts {
		host := host
		summary := recapForHost(state, host)
		rows = append(rows, row{
			text: recapHostRowText(host, summary, widths, false),
			id:   recapHostRowID(host),
			selected: func() {
				hostExpanded[host] = !hostExpanded[host]
			},
		})
		if !hostExpanded[host] {
			continue
		}
		for _, cat := range summary.Categories {
			cat := cat
			catID := recapCategoryRowID{host: host, outcome: cat.Outcome}
			rows = append(rows, row{
				text: recapCategoryRowText(cat, false),
				id:   catID,
				selected: func() {
					categoryExpanded[catID] = !categoryExpanded[catID]
				},
			})
			if !categoryExpanded[catID] {
				continue
			}
			for _, task := range cat.Tasks {
				task := task
				rows = append(rows, row{
					text: recapTaskRowText(task, host, cat.Outcome, false),
					id:   recapTaskRowID{host: host, task: task},
					selected: func() {
						showOutput(task, host)
					},
				})
			}
		}
	}
	return rows
}
