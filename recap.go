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
// PlaybookState already tracks (a scan over Plays/Tasks/Hosts) - no
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
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"github.com/rivo/tview"
)

// recapCategory is one non-empty bucket for one host - "non-empty"
// because recapForHost only ever includes a category here when it has at
// least one task, per design-docs/Recap.md's own "if there's zero tasks
// in a category then don't even render that line" decision. Label is
// the discriminator used everywhere (row ids, matching a category back
// up by name) rather than outcome, because "warnings" isn't tied to any
// single outcome - a warning can appear on an OK, Changed, Failed, or
// any other result, so unlike the other five categories it has no
// natural outcome value of its own. Color is resolved once per category
// (recapCategoryColor) rather than re-deriving it from Label at every
// render site.
type recapCategory struct {
	Label string
	Color string
	Tasks []*playbook.TaskNode
}

// recapCategoryColor maps a category's own label to its display color -
// colorTag's own palette for the five outcome-based categories, and
// warningColor for "warnings", which cuts across all of them rather than
// being tied to one. Shared by recapForHost (building a fresh
// recapCategory) and rebuild's own selected-row re-render (which only
// has a label, not a full recapCategory, to work from), so the two can't
// disagree about what color a given category is.
func recapCategoryColor(label string) string {
	switch label {
	case "ok":
		return colorTag(playbook.OutcomeOK)
	case "skipped":
		return colorTag(playbook.OutcomeSkipped)
	case "changed":
		return colorTag(playbook.OutcomeChanged)
	case "unreachable":
		return colorTag(playbook.OutcomeUnreachable)
	case "failed":
		return colorTag(playbook.OutcomeFailed)
	case "warnings":
		return warningColor
	default:
		return "white"
	}
}

// recapHostSummary is one host's own recap: every count (including zero
// ones - the summary line itself always shows all six, matching
// ansible's own recap line for the first five, plus warnings as a
// tangsible-specific addition) plus the non-empty categories available
// to expand into. Warnings is deliberately not mutually exclusive with
// the other five - a task keeps its own outcome bucket (ok/changed/...)
// and can *also* show up under "warnings", since a warning is orthogonal
// to outcome (confirmed empirically: buildOutputTab already shows a
// task's own "warnings" field regardless of outcome or module).
type recapHostSummary struct {
	OK, Changed, Unreachable, Failed, Skipped, Warnings int
	Categories                                          []recapCategory
}

// recapForHost scans every task across every play for this one host's
// own outcome, in run order - the same data taskLabel/hostLabel already
// read, just tallied per host instead of per task. Cheap at this
// project's own ~10-host/handful-of-hundred-task target scale, computed
// fresh on every rebuild rather than tracked incrementally, matching
// aggregate.go's own "no second source of truth" convention for counts.
func recapForHost(state *playbook.PlaybookState, host string) recapHostSummary {
	var ok, changed, unreachable, failed, skipped, warned []*playbook.TaskNode
	for _, play := range state.Plays {
		for _, task := range play.Tasks {
			o, present := task.Hosts[host]
			if !present {
				continue
			}
			switch o {
			case playbook.OutcomeOK:
				ok = append(ok, task)
			case playbook.OutcomeChanged:
				changed = append(changed, task)
			case playbook.OutcomeUnreachable:
				unreachable = append(unreachable, task)
			case playbook.OutcomeFailed:
				failed = append(failed, task)
			case playbook.OutcomeSkipped:
				skipped = append(skipped, task)
			}
			if hasWarnings(task.Raw[host]) {
				warned = append(warned, task)
			}
		}
	}

	s := recapHostSummary{
		OK:          len(ok),
		Changed:     len(changed),
		Unreachable: len(unreachable),
		Failed:      len(failed),
		Skipped:     len(skipped),
		Warnings:    len(warned),
	}
	for _, c := range []recapCategory{
		{"ok", recapCategoryColor("ok"), ok},
		{"skipped", recapCategoryColor("skipped"), skipped},
		{"changed", recapCategoryColor("changed"), changed},
		{"unreachable", recapCategoryColor("unreachable"), unreachable},
		{"failed", recapCategoryColor("failed"), failed},
		{"warnings", recapCategoryColor("warnings"), warned},
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
// host row in the live tree above, once as a recap task line below - and
// sharing one identity between them would make rebuild()'s own
// selection-restoration ambiguous about which occurrence currentID
// actually refers to. recapTaskRowID also carries its own category label,
// not just (host, task): since "warnings" isn't mutually exclusive with
// the other categories, the identical task can now legitimately appear
// as two separate rows for the same host (once under its own outcome,
// once under "warnings") - without the label, those two rows would share
// one identity and hit the exact same ambiguity all over again.
type recapHostRowID string
type recapCategoryRowID struct {
	host  string
	label string
}
type recapTaskRowID struct {
	host  string
	label string
	task  *playbook.TaskNode
}

// recapHeadingRowID identifies one of the recap section's own six leading
// decoration rows (a blank spacer, "Summary", its "====" underline,
// another blank spacer, the free-text narrative line - see
// recapNarrativeSummary - and one more blank spacer after it) - six
// distinct values of the same named type, rather than reusing
// statusDividerRowID's own empty-struct sentinel for all of them: that
// type's own zero size makes every instance compare equal, which would
// make rebuild()'s identity-based selection-restoration unable to tell
// these apart if the cursor ever happened to be sitting on one of them
// when a rebuild ran. None of these six rows carries a selected callback -
// like statusRowID/statusDividerRowID, the cursor can still be moved onto
// one by plain arrow-key navigation, but Enter does nothing there; Up/
// Down/j/k skip over all of them entirely instead, for free, via
// nextInteractiveRow's own generic "selected == nil" rule - matching
// design-docs/Recap.md's explicit "these are additional lines... the
// cursor hast to jump over" requirement for the narrative line
// specifically, with no row-specific code needed to honor it.
type recapHeadingRowID int

const (
	recapDividerBeforeHeading recapHeadingRowID = iota
	recapHeadingRow
	recapHeadingUnderlineRow
	recapDividerAfterHeading
	recapNarrativeRow
	recapDividerAfterNarrative
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

// pluralS returns "" for n == 1, "s" otherwise - shared by every
// singular/plural word recapNarrativeSummary builds, all of which happen
// to just take a trailing "s" (task/tasks, host/hosts).
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// recapNarrativeSummary renders design-docs/Recap.md's own "Additional
// idea" - a plain-language overview of the whole run, shown between the
// "Summary" heading and the per-host lines below it. Deliberately plain,
// untagged text (no per-field coloring the way recapHostRowText's own
// "label=N" segments get) - this is free-flowing prose, not one more
// structured line to align to columns.
//
// Always exactly one to three separate sentences, one clause each,
// rather than a single sentence joining multiple facts with a comma - a
// comma-joined clause ("...in xx:xx minutes, 1 host failed...,
// 2 were not reachable") can't have its middle clause dropped without
// leaving the punctuation wrong; a period between every clause means
// each one can be included or omitted independently with no cleanup
// needed elsewhere. The first sentence (elapsed time, task/host counts)
// always appears; the second (hosts that failed) and third (hosts that
// went unreachable) only appear when their own count is greater than
// zero - a fully clean run gets just the first sentence.
//
// "Reachable" hosts is every host in state.AllHosts except one currently
// counted Unreachable for at least one task (recapForHost's own
// Unreachable field) - not necessarily disjoint from "failed" (a host
// could conceivably fail a task before later going unreachable), but
// that's an accepted edge case, not something this narrative sentence
// tries to resolve; real runs overwhelmingly land in one bucket or the
// other. elapsed is the run's own total wall-clock duration (rebuild's
// frozenElapsed, same value topBarText's own elapsed readout freezes to),
// not any one task's or host's own timing.
func recapNarrativeSummary(state *playbook.PlaybookState, elapsed time.Duration) string {
	totalTasks := len(allTasks(state))
	totalHosts := len(state.AllHosts)

	var unreachableHosts, failedHosts int
	for _, host := range state.AllHosts {
		s := recapForHost(state, host)
		if s.Unreachable > 0 {
			unreachableHosts++
		}
		if s.Failed > 0 {
			failedHosts++
		}
	}
	reachableHosts := totalHosts - unreachableHosts

	mm, ss := minutesSeconds(elapsed)
	sentence := fmt.Sprintf("Completed %d task%s on %d reachable host%s in %02d:%02d minutes.",
		totalTasks, pluralS(totalTasks),
		reachableHosts, pluralS(reachableHosts),
		mm, ss)

	if failedHosts > 0 {
		sentence += fmt.Sprintf(" %d host%s failed before the end of the playbook.",
			failedHosts, pluralS(failedHosts))
	}
	if unreachableHosts > 0 {
		was := "was"
		if unreachableHosts != 1 {
			was = "were"
		}
		sentence += fmt.Sprintf(" %d host%s %s not reachable.",
			unreachableHosts, pluralS(unreachableHosts), was)
	}
	return sentence
}

// recapNarrativeRowText wraps recapNarrativeSummary's own text for
// display as a tree row - tview.Escape'd like every other piece of
// dynamic/derived content in this app once dynamic colors are on,
// even though this particular text is built entirely from this app's
// own fixed sentence templates and never echoes anything user-authored
// (a hostname, a task name) that could itself contain a literal "[".
func recapNarrativeRowText(state *playbook.PlaybookState, elapsed time.Duration) string {
	return tview.Escape(recapNarrativeSummary(state, elapsed))
}

// recapColumnWidths is the recap section's own per-column alignment,
// computed once across every host (recapComputeColumnWidths) rather than
// per row: a single host's own summary line has no way to know how wide
// its neighbors' hostnames or counts are, but the whole point of
// column alignment is that every row agrees on the same widths.
type recapColumnWidths struct {
	Host                                                int
	OK, Changed, Unreachable, Failed, Skipped, Warnings int
}

// recapComputeColumnWidths scans every host's own recap once (reusing
// recapForHost - cheap at this project's own target scale, see its own
// doc comment) to find the widest hostname and the widest digit count
// each of the six fields ever needs, so every host's summary line can
// right-align its numbers and left-align its hostname to the identical
// columns - matching ansible's own real recap output, which does the
// same per-field alignment across hosts rather than sizing each line to
// its own content.
func recapComputeColumnWidths(state *playbook.PlaybookState) recapColumnWidths {
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
		if l := digits(s.Warnings); l > w.Warnings {
			w.Warnings = l
		}
	}
	return w
}

// recapSummaryFieldColor is recapHostRowText's own per-field color choice:
// recapCategoryColor's own outcome color (green ok, yellow changed, ...,
// pink warnings) when n is greater than zero, or grayTag - the exact same
// dark gray already used elsewhere for a host that hasn't reported for a
// task yet - when n is zero. A live, real-world recap experiment showed
// six always-colored "label=N" segments read as fairly uniform "wall of
// color" regardless of what actually happened, one field per outcome;
// graying out the zero counts makes only the fields with something to
// report stand out, at a glance, without reading every number.
func recapSummaryFieldColor(label string, n int) string {
	if n == 0 {
		return grayTag
	}
	return recapCategoryColor(label)
}

// recapHostRowText renders one host's own summary line, hostname
// left-padded and each count right-padded to w's own columns so every
// host's line lines up - and each "label=N" segment colored via
// recapSummaryFieldColor, rather than picking one dominant color for the
// whole line, so the same "color signals meaning" convention this app
// uses everywhere else applies per field here too. warnings=N is a
// tangsible-specific addition, not something real ansible-playbook's own
// recap line includes - placed last, after the five fields that do
// mirror it, since it's a different, cross-cutting kind of count rather
// than one more slice of the same partition. selected inverts each
// segment's own color to a background (black bold text on it) instead of
// a foreground - the same "outcome color becomes a background under the
// cursor" convention taskLabel/hostLabel already use, just applied per
// segment here rather than per hostname. The hostname portion itself
// gets the plain light gray "this is the identifying label" background
// every other selected row's own title/hostname already uses. No
// neutral, unstyled gap anywhere on the selected line - the same rule
// taskLabel's own selected rendering already follows for its hostname
// segments - each segment's own leading two-space gap is folded into its
// own color block rather than left plain between tags, so the row reads
// as one continuous highlight instead of colored blocks with visible
// holes between them.
func recapHostRowText(host string, s recapHostSummary, w recapColumnWidths, selected bool) string {
	hostPadded := host + strings.Repeat(" ", w.Host-len([]rune(host)))
	if selected {
		// firstSeg has no leading gap of its own - the title already ends
		// in " : ", so folding a gap onto ok too (like every later
		// segment) would double it up against that trailing space,
		// visibly shifting the "ok=" column by one space compared to the
		// unselected rendering right above/below it.
		firstSeg := func(label string, width, n int) string {
			return fmt.Sprintf("[%s:%s:b]%s=%*d[-:-:-]", pureBlack, recapSummaryFieldColor(label, n), label, width, n)
		}
		seg := func(label string, width, n int) string {
			return fmt.Sprintf("[%s:%s:b]  %s=%*d[-:-:-]", pureBlack, recapSummaryFieldColor(label, n), label, width, n)
		}
		return fmt.Sprintf("[%s:lightgray:b]%s : [-:-:-]%s%s%s%s%s%s",
			pureBlack, tview.Escape(hostPadded),
			firstSeg("ok", w.OK, s.OK),
			seg("skipped", w.Skipped, s.Skipped),
			seg("changed", w.Changed, s.Changed),
			seg("unreachable", w.Unreachable, s.Unreachable),
			seg("failed", w.Failed, s.Failed),
			seg("warnings", w.Warnings, s.Warnings),
		)
	}
	seg := func(label string, width, n int) string {
		return fmt.Sprintf("[%s]%s=%*d[-]", recapSummaryFieldColor(label, n), label, width, n)
	}
	return fmt.Sprintf("[white::b]%s[-::-] : %s  %s  %s  %s  %s  %s",
		tview.Escape(hostPadded),
		seg("ok", w.OK, s.OK),
		seg("skipped", w.Skipped, s.Skipped),
		seg("changed", w.Changed, s.Changed),
		seg("unreachable", w.Unreachable, s.Unreachable),
		seg("failed", w.Failed, s.Failed),
		seg("warnings", w.Warnings, s.Warnings),
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
	return fmt.Sprintf("[%s]%s[-]", c.Color, line)
}

// recapTaskDetail returns the parenthesized detail to show after a task's
// own name on one recap task line - outcomeDetail (the identical detail
// hostLabel already shows for the same (task, host) pair in the live
// tree) for every category except "warnings", which instead shows the
// task's own warning text(s), semicolon-joined rather than newline-joined
// like the drill-down's own Warnings section: a recap row is always a
// single line, unlike that section's own free-standing TextView content.
func recapTaskDetail(task *playbook.TaskNode, host, label string) string {
	if label == "warnings" {
		if joined := joinedStringList(decodeWarnings(task.Raw[host]), "; "); joined != "" {
			return fmt.Sprintf(" (%s)", joined)
		}
		return ""
	}
	return outcomeDetail(task, host)
}

// recapTaskRowText renders one task line under a category - the task's
// own name (already "role : task name" when role-sourced, straight from
// task.Name - see source.go/CLAUDE.md's own note that this is how a real
// event's task.name already renders) plus detail (recapTaskDetail).
func recapTaskRowText(task *playbook.TaskNode, detail, color string, selected bool) string {
	line := fmt.Sprintf("    %s%s", tview.Escape(task.Name), tview.Escape(detail))
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", pureBlack, line)
	}
	return fmt.Sprintf("[%s]%s[-]", color, line)
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
func flattenRecapRows(state *playbook.PlaybookState, hostExpanded map[string]bool, categoryExpanded map[recapCategoryRowID]bool, showOutput func(task *playbook.TaskNode, host string)) []row {
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
			catID := recapCategoryRowID{host: host, label: cat.Label}
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
					text: recapTaskRowText(task, recapTaskDetail(task, host, cat.Label), cat.Color, false),
					id:   recapTaskRowID{host: host, label: cat.Label, task: task},
					selected: func() {
						showOutput(task, host)
					},
				})
			}
		}
	}
	return rows
}
