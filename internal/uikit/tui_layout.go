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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
	"github.com/rivo/tview"
)

// SpinnerAt returns the spinner frame for a given elapsed duration - shared
// by the top bar's own heartbeat and taskLabel's active-task prefix so both
// tick the same frame at the same instant when driven from the same elapsed
// value (see rebuild).
func SpinnerAt(elapsed time.Duration) rune {
	return SpinnerFrames[int(elapsed/SpinnerInterval)%len(SpinnerFrames)]
}

// MinutesSeconds splits d into whole minutes and the remaining seconds
// (0-59), both floored - shared by the top bar's own elapsed display and
// taskLabel's active-task elapsed suffix, which are two independent
// measures (see NewLiveTUI's startedAt vs TaskNode.StartedAt) that should
// at least agree on formatting.
func MinutesSeconds(d time.Duration) (mm, ss int) {
	return int(d / time.Minute), int(d/time.Second) % 60
}

// TopBarText renders the top bar: a Playbook:/Role: label (isRole picks
// which), the currently active filter (see Filters.md's "title bar shows
// the currently selected filter" requirement; shown unconditionally,
// including "All", rather than only when a filter is actually narrowing
// anything), every host seen so far (state.AllHosts, same set taskLabel
// greys-in against), and a heartbeat - a spinner frame (dropped once
// frozen - simplicity over cuteness rather than stuck on an arbitrary
// frame or swapped for a checkmark) and total elapsed time since the TUI
// itself started (our own time.Now(), NOT any event's _timestamp - "has
// our program been alive/responsive," a different question from any one
// task's own duration). The heartbeat is always right-aligned to width;
// the host list is what gives way (via truncateHostsList) when the line
// is too narrow for everything to fit, so the heartbeat never gets
// pushed off-screen.
//
// The whole bar doubles as a literal progress fill (design-docs' own
// "the headline as a progress bar" idea) via progressFillLine - see its
// own doc comment for the fill/escaping details. composeTopBarLine below
// is this function's own plain-text half, split out only because
// TopBarText itself is a thin composeTopBarLine+progressFillLine
// wrapper - split mode uses its own, separately-composed
// composeSplitHeaderLine instead of this one (see its own doc comment
// for why a shared tree-only composer isn't reused there).
func TopBarText(playbookName string, isRole bool, hosts []string, elapsed time.Duration, frozen bool, filter FilterQuery, progressPos, progressTotal int, width int, bgColorName string, showElapsed bool) string {
	return ProgressFillLine(
		ComposeTopBarLine(playbookName, isRole, hosts, elapsed, frozen, filter, progressPos, progressTotal, width, showElapsed),
		progressPos, progressTotal, frozen, bgColorName)
}

// ComposeTopBarLine builds the top bar's own plain, unfilled, already-
// width-padded text - see topBarText's own doc comment for what it
// shows. Kept separate from the fill step itself (progressFillLine/
// progressFillLineAt) so a caller that needs to coordinate this bar's
// own fill boundary against something else (split mode's own tree-bar +
// divider + drill-down-bar alignment, in rebuild) can do so without
// duplicating this composition logic.
//
// showElapsed false (design-docs/Revisit.md: a revisit session, where
// elapsed is always ~0 - Phase 1 only ever saved a run's start time, never
// its duration, so there's nothing real to show) drops the spinner/mm:ss
// clock entirely rather than displaying a value that would just read as
// wrong - "Task x/y" alone still shows if there's a progressPrefix to show
// (never the case for a revisit session in practice, since replay never
// builds a progress skeleton either, but this function doesn't assume
// that - the two are independent knobs).
func ComposeTopBarLine(playbookName string, isRole bool, hosts []string, elapsed time.Duration, frozen bool, filter FilterQuery, progressPos, progressTotal int, width int, showElapsed bool) string {
	label := "Playbook"
	if isRole {
		label = "Role"
	}
	mm, ss := MinutesSeconds(elapsed)
	// progressPrefix: the prototype "Task x/y" indicator (progress.go) -
	// deliberately never clamped to progressTotal, even once frozen
	// (unlike progressFillLine's own fill, see its doc comment) - an
	// unmatched-to-the-end run (e.g. dynamic includes the skeleton
	// couldn't predict) stays visible here as useful signal about how
	// well this prototype's matching actually tracked this run.
	var progressPrefix string
	if progressTotal > 0 {
		progressPrefix = fmt.Sprintf("Task %d/%d  ", progressPos, progressTotal)
	}
	var right string
	switch {
	case !showElapsed:
		right = progressPrefix
	case frozen:
		right = fmt.Sprintf("%s%02d:%02d ", progressPrefix, mm, ss)
	default:
		right = fmt.Sprintf("%s%c %02d:%02d ", progressPrefix, SpinnerAt(elapsed), mm, ss)
	}

	prefix := fmt.Sprintf(" %s: %s   Filter: %s   Hosts: ", label, playbookName, filter.label())
	hostsBudget := width - len([]rune(prefix)) - len([]rune(right))
	left := prefix + TruncateHostsList(hosts, hostsBudget)

	pad := width - len([]rune(left)) - len([]rune(right))
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// ComposeSplitHeaderLine builds split mode's own combined header line -
// a single, self-contained composition padded against the *full*
// terminal width (width) in one pass, deliberately NOT reusing
// composeTopBarLine (padded against just the tree pane's own share) plus
// a second, separately-composed piece concatenated after it. An earlier
// version did exactly that, and it produced two real, reported bugs at
// once: when the tree-pane sub-line's own content didn't fit its own
// narrower budget, composeTopBarLine's own overflow guard (pad clamped
// to a minimum of 1) pushed the "Task x/y  elapsed" segment out of its
// own right-aligned position and into the middle of the row, with the
// drill-down's own host/task trailing after it rather than sitting right
// after "Hosts:" - and that same overflow silently fed a combined line
// whose real length disagreed with the width the fill was computed
// against, corrupting where the fill boundary actually landed. A single
// composition against one width, computed once, can't disagree with
// itself.
//
// Layout, left to right: Playbook:/Role:, Filter:, "Hosts: " followed by
// hostAndTask (the drill-down's own currently-shown "<host>   <task
// title>", three spaces between the two - matching every other field
// separator on this line - there's exactly one relevant host/task
// pane-side, not "every host seen so far" the way the tree-only bar's
// own host list is), then - flush right, same as topBarText's own right
// side - the "Task x/y" progress indicator and the heartbeat (spinner +
// elapsed, or just elapsed once frozen).
func ComposeSplitHeaderLine(playbookName string, isRole bool, hostAndTask string, elapsed time.Duration, frozen bool, filter FilterQuery, progressPos, progressTotal int, width int, showElapsed bool) string {
	label := "Playbook"
	if isRole {
		label = "Role"
	}
	mm, ss := MinutesSeconds(elapsed)
	var progressPrefix string
	if progressTotal > 0 {
		progressPrefix = fmt.Sprintf("Task %d/%d  ", progressPos, progressTotal)
	}
	var right string
	switch {
	case !showElapsed:
		right = progressPrefix
	case frozen:
		right = fmt.Sprintf("%s%02d:%02d ", progressPrefix, mm, ss)
	default:
		right = fmt.Sprintf("%s%c %02d:%02d ", progressPrefix, SpinnerAt(elapsed), mm, ss)
	}

	left := fmt.Sprintf(" %s: %s   Filter: %s   Hosts: %s", label, playbookName, filter.label(), hostAndTask)

	pad := width - len([]rune(left)) - len([]rune(right))
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

// ProgressFillWidth returns how many of a totalWidth-rune-wide line
// should be considered "filled" for the given progress - shared by
// progressFillLine's own self-contained fill computation. 0 whenever
// there's no skeleton at all (progressTotal <= 0 - see progressFillLine's
// own doc comment for why that means "no fill effect", not "0%
// filled"); totalWidth itself once frozen, regardless of how far
// progressPos actually got (same "snap to 100% rather than read as
// broken" reasoning as progressFillLine).
func ProgressFillWidth(totalWidth, progressPos, progressTotal int, frozen bool) int {
	if progressTotal <= 0 {
		return 0
	}
	if frozen {
		return totalWidth
	}
	fillWidth := totalWidth * progressPos / progressTotal
	if fillWidth > totalWidth {
		fillWidth = totalWidth
	}
	if fillWidth < 0 {
		fillWidth = 0
	}
	return fillWidth
}

// ProgressFillLineAt applies the progress-fill background (see
// progressFillLine's own doc comment for the visual/escaping details) at
// an explicit, already-decided fillWidth rather than computing its own
// proportionally from progressPos/progressTotal - the building block
// progressFillLine itself uses internally.
func ProgressFillLineAt(line string, fillWidth int, bgColorName string) string {
	runes := []rune(line)
	if fillWidth > len(runes) {
		fillWidth = len(runes)
	}
	if fillWidth < 0 {
		fillWidth = 0
	}
	filled, unfilled := string(runes[:fillWidth]), string(runes[fillWidth:])
	return fmt.Sprintf("[white:%s:b]%s[-:-:-][white:%s:b]%s[-:-:-]", ProgressFillColor, tview.Escape(filled), bgColorName, tview.Escape(unfilled))
}

// ProgressFillLine wraps an already-composed, already-width-padded plain
// bar line with the progress-fill background (design-docs' own "the
// headline as a progress bar" idea): progressFillColor from the left
// edge up to whatever fraction of progressPos/progressTotal the line's
// own length represents (progressFillWidth), and bgColorName (plain
// "navy", except during a revisit session - design-docs/Revisit.md - where
// NewLiveTUI's own chromeColorName passes "purple" instead) for the
// remainder - sweeping across whatever content the line holds, rather
// than being confined to some reserved strip.
//
// No fill/color effect at all (the line renders exactly as plain
// white-on-bgColorName text always has) when progressTotal is 0 - no skeleton
// at all, e.g. the throwaway --list-tasks probe failed, or the run
// hasn't spawned yet during "rerun"'s own startup dialog - same "omit
// rather than show something misleading" convention this file already
// uses elsewhere. (progressFillWidth already returns 0 in this case, but
// that alone would still produce a filled/unfilled *tag pair* with an
// empty filled half - indistinguishable in the end, but this short-
// circuits it explicitly rather than relying on that coincidence.)
//
// Callers must enable dynamic colors on whichever TextView this feeds,
// to make the fill possible at all (a single tcell.Style can't vary
// per-column) - which means line's own content, if it came from external
// data (a user's own filenames, a search term, an inventory hostname, a
// task's own name), needs escaping. Done here, once, on the two already-
// sliced halves rather than by each caller on each piece as it's built -
// simpler, and safe: tview.Escape() only ever expands a literal "[" in
// place within whichever half it's applied to, which cannot shift
// anything in the OTHER half or retroactively invalidate the split point
// already computed against the unescaped line.
func ProgressFillLine(line string, progressPos, progressTotal int, frozen bool, bgColorName string) string {
	if progressTotal <= 0 {
		return fmt.Sprintf("[white:%s:b]%s[-:-:-]", bgColorName, tview.Escape(line))
	}
	return ProgressFillLineAt(line, ProgressFillWidth(len([]rune(line)), progressPos, progressTotal, frozen), bgColorName)
}

// TruncateHostsList renders hosts as a comma-separated list, shortened to
// fit maxWidth by dropping hosts off the end and appending ", ..." (same
// "documented heuristic, not chased to 100%" style as taskLabel's own
// hostname-shrink loop elsewhere in this file) - never breaks a single
// hostname mid-word, only ever drops whole hosts from the tail.
func TruncateHostsList(hosts []string, maxWidth int) string {
	full := strings.Join(hosts, ", ")
	if maxWidth <= 0 {
		return ""
	}
	if len([]rune(full)) <= maxWidth {
		return full
	}

	const suffix = "..."
	var b strings.Builder
	kept := 0
	for i, h := range hosts {
		candidate := b.String()
		if i > 0 {
			candidate += ", "
		}
		candidate += h
		if len([]rune(candidate))+len(", "+suffix) > maxWidth {
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(h)
		kept++
	}
	if kept == 0 {
		return suffix
	}
	return b.String() + ", " + suffix
}

// PlayRowText builds one PLAY row's text - just the play's name, white and
// bold normally. selected switches to the cursor-row styling (see
// taskLabel/hostLabel's own selected parameter and NewLiveTUI's
// SetSelectedStyle comment for why this can't just be a single uniform
// List-wide style): black bold text on a light gray background across the
// whole name.
func PlayRowText(play *playbook.PlayNode, selected bool) string {
	name := tview.Escape(play.Name)
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s[-:-:-]", PureBlack, name)
	}
	return fmt.Sprintf("[white::b]%s[-::-]", name)
}

// ColorTag returns the tview style-tag foreground color name for o, per
// TUI.md's OK/Changed/Skipped/Failed = green/yellow/cyan/red convention
// (using tcell's/W3C's "teal" as the closest named match for cyan).
func ColorTag(o playbook.Outcome) string {
	switch o {
	case playbook.OutcomeOK:
		return "green"
	case playbook.OutcomeChanged:
		return "yellow"
	case playbook.OutcomeSkipped:
		return "teal"
	case playbook.OutcomeFailed:
		return "red"
	case playbook.OutcomeUnreachable:
		return "maroon" // deliberately muted vs "red" - see TUI.md; both are
		// base ANSI-16 names (index 9 vs 1), not RGB-approximated extended
		// W3C names, so they stay reliably distinct across terminal themes.
	default:
		return "-"
	}
}

// SummaryFieldColor is hostSummaryColoredText's own per-field color
// choice, design-docs/Morehosts.md - colorTag(o) when n is greater than
// zero, or grayTag when n is zero. Same "gray out zero counts" rule the
// recap's own recapSummaryFieldColor already established (recap.go) -
// reused here in spirit, not by calling it directly, since that one is
// keyed by a label string tied to its own recapColumnWidths fields
// rather than by outcome.
func SummaryFieldColor(o playbook.Outcome, n int) string {
	if n == 0 {
		return GrayTag
	}
	return ColorTag(o)
}

// HostSummaryPlainText renders task.counts()'s five values as
// design-docs/Morehosts.md's own fixed-format summary string - raw,
// untagged, unescaped text. Shared by widestSummaryWidth (for measuring)
// and taskLabel's own selected-row rendering (which - like every other
// selected row in this file - uses a single uniform light-gray
// background rather than per-field color, so there's nothing to tag
// there either).
func HostSummaryPlainText(ok, changed, skipped, failed, unreachable int) string {
	return fmt.Sprintf("OK:%d/Chgd:%d/Skip:%d/Fail:%d/Unrch:%d", ok, changed, skipped, failed, unreachable)
}

// HostSummaryColoredText is hostSummaryPlainText's own tagged rendering
// for an unselected row: each field wrapped in summaryFieldColor's own
// tag when useColor is true, or the identical plain text (escaped, no
// tags at all) when it's false - design-docs/Morehosts.md's explicit
// "otherwise it shall be simply an uncolored string." Labels and digits
// are always safe, fixed literal text, never external data - no
// tview.Escape needed on the colored branch's own tag/label/digit
// pieces, only (defensively, consistent with this file's own discipline
// elsewhere) on the plain-text fallback.
func HostSummaryColoredText(ok, changed, skipped, failed, unreachable int, useColor bool) string {
	if !useColor {
		return tview.Escape(HostSummaryPlainText(ok, changed, skipped, failed, unreachable))
	}
	seg := func(label string, o playbook.Outcome, n int) string {
		return fmt.Sprintf("[%s]%s:%d[-]", SummaryFieldColor(o, n), label, n)
	}
	return strings.Join([]string{
		seg("OK", playbook.OutcomeOK, ok),
		seg("Chgd", playbook.OutcomeChanged, changed),
		seg("Skip", playbook.OutcomeSkipped, skipped),
		seg("Fail", playbook.OutcomeFailed, failed),
		seg("Unrch", playbook.OutcomeUnreachable, unreachable),
	}, "/")
}

// WidestSummaryWidth is the widest hostSummaryPlainText rune width across
// every task the run has produced so far (allTasks(state), same
// unconditional-of-expand/collapse/filter scope computeHostColumnLayout's
// own desiredTitleWidth already uses) - what summary mode sizes
// TitleColWidth against instead of the host list's own width, since
// every task's counts (and so its own summary string's digit widths) can
// differ.
func WidestSummaryWidth(state *playbook.PlaybookState) int {
	widest := 0
	for _, t := range AllTasks(state) {
		ok, changed, skipped, failed, unreachable := t.Counts()
		if w := len([]rune(HostSummaryPlainText(ok, changed, skipped, failed, unreachable))); w > widest {
			widest = w
		}
	}
	return widest
}

// HostTransition builds the halfBlock separator cell between two adjacent
// hostnames' color tags - left's color bleeds into right's across that one
// cell, rather than an abrupt full-cell jump from one solid color to the
// next.
func HostTransition(leftTag, rightTag string) string {
	return fmt.Sprintf("[%s:%s:-]%s[-:-:-]", leftTag, rightTag, HalfBlock)
}

// HostColumnLayout is the layout every TASK row shares for one rebuild
// (TUI.md's "Tree View - third iteration") - computed once by
// computeHostColumnLayout and threaded into every taskLabel call (both
// flattenRows' own per-row calls and rebuild()'s separate selected-row
// re-render), so every row uses the identical values. TitleColWidth is
// how wide the title "column" is - every row pads its own title with
// spaces up to this width, or truncates down to it if its own title is
// longer. HostDisplay is the (possibly globally-shrunk) display text for
// each host in PlaybookState.AllHosts, same order - shared verbatim by
// every row; only each row's per-host *color* varies (task.Hosts[host]),
// never the text. SummaryMode (design-docs/Morehosts.md) is true when
// the per-host list should be replaced with each row's own
// OK/Changed/Skipped/Failed/Unreachable count summary instead
// (computeHostColumnLayout's own forceSummary/hostsTooNarrow) - taskLabel
// ignores HostDisplay entirely in that case (left empty).
type HostColumnLayout struct {
	TitleColWidth int
	HostDisplay   []string
	SummaryMode   bool
}

// SplitTreeWidth implements design-docs/TwoPanedLayout.md's growth rule for
// the two-pane drill-down: the tree pane grows first, from splitMinTreeWidth
// up to splitMaxTreeWidth, before any further width goes to the drill-down
// pane. Only meaningful (and only ever called) once totalWidth has already
// been checked to be at least splitMinTotalWidth - showOutput is the sole
// caller, right after that same check.
func SplitTreeWidth(totalWidth int) int {
	extra := totalWidth - SplitMinTotalWidth
	return SplitMinTreeWidth + min(extra, SplitMaxTreeWidth-SplitMinTreeWidth)
}

// ComputeHostColumnLayout implements TUI.md's third-iteration algorithm:
// hosts start at the same column on every row, regardless of that row's
// own title length. Unlike the previous per-row-independent right-align,
// this needs to look across every task the run has produced so far to
// find the column width every row will share.
//
// TitleColWidth is normally the widest *natural* (untruncated) title width
// across every task in state.Plays - not just currently visible ones,
// deliberately: allTasks(state) is unconditional (ignores expand/collapse
// and the active filter), so the column never shifts just because the
// user expanded/collapsed something. Since state.Plays only ever grows
// during a run (tasks are appended, never removed - see aggregate.go) and
// resets to empty via Reset() on a rerun, recomputing this fresh from
// state.Plays on every rebuild already gives a monotonically
// non-decreasing column for free, with no separate tracked state needed -
// exactly the "only ever grows" behavior the user asked for, so
// expanding/collapsing a task never makes the tree jump.
//
// If the widest title, the full host list, and titleHostGapFloor's worth
// of breathing room don't all fit within avail, the column shrinks first
// (down to minTaskTitleName, same floor the old per-row algorithm used),
// and only if that alone still isn't enough are hostnames shrunk next -
// same "reduce the currently-longest hostname by one, repeat" loop the
// old taskLabel used per-row, now run once here instead: since every row
// shares the same column and the same avail width, every row's own
// available host-list width is identical by construction, so per-row
// shrinking would only ever produce the identical result anyway. Running
// it once is what makes the truncated host text - not just the column's
// start - line up column-by-column down every row for free, without
// needing a second alignment mechanism.
//
// forceSummary (design-docs/Morehosts.md) is NewLiveTUI's own useColor,
// inverted - true whenever color isn't usable at all (terminal
// incapable, NO_COLOR set, or general.color = false), in which case the
// per-host list is skipped entirely in favor of summary mode below,
// regardless of whether it would actually have fit. When forceSummary is
// false, the host-shrink loop above still runs first (color being usable
// doesn't mean there's room) - if it needed to shrink any real host name
// (longer than 2 runes to begin with) down to 2 runes or fewer, that's
// Morehosts.md's own "not enough space" trigger, and this falls through
// to summary mode too rather than returning the now-illegibly-truncated
// host list.
func ComputeHostColumnLayout(state *playbook.PlaybookState, allHosts []string, avail int, forceSummary bool) HostColumnLayout {
	availContent := avail - len(TaskIndent)
	if availContent < 0 {
		availContent = 0
	}

	desiredTitleWidth := 0
	for _, t := range AllTasks(state) {
		if w := len([]rune(t.Name)); w > desiredTitleWidth {
			desiredTitleWidth = w
		}
	}

	if len(allHosts) == 0 {
		// Nothing to align to yet (true run-wide, briefly, before the very
		// first host of the entire run has reported anything at all) - the
		// column has no meaning without hosts; taskLabel's own !haveHosts
		// path ignores TitleColWidth entirely in this case, so its exact
		// value here doesn't matter. Also too early to know whether summary
		// mode will even be needed once hosts do show up, so this never
		// forces it just from being empty.
		return HostColumnLayout{TitleColWidth: desiredTitleWidth}
	}

	if !forceSummary {
		hostRunes := make([][]rune, len(allHosts))
		for i, h := range allHosts {
			hostRunes[i] = []rune(h)
		}
		hostsWidth := func() int {
			w := 0
			for i, hr := range hostRunes {
				w += len(hr)
				if i > 0 {
					w++ // fixed 1-space separator between adjacent host names -
					// not itself a shrink target, same as before.
				}
			}
			return w
		}

		fits := func(colWidth int) bool {
			return colWidth+TitleHostGapFloor+hostsWidth() <= availContent
		}

		titleColWidth := desiredTitleWidth
		if !fits(titleColWidth) {
			floor := desiredTitleWidth
			if floor > MinTaskTitleName {
				floor = MinTaskTitleName
			}
			target := availContent - TitleHostGapFloor - hostsWidth()
			if target < floor {
				target = floor
			}
			titleColWidth = target
			if titleColWidth < 0 {
				titleColWidth = 0
			}
		}

		for !fits(titleColWidth) {
			longest := -1
			for i, hr := range hostRunes {
				if len(hr) > 1 && (longest == -1 || len(hr) > len(hostRunes[longest])) {
					longest = i
				}
			}
			if longest == -1 {
				// Every hostname is already at its 1-character floor and it
				// still doesn't fit even alongside a column floored at
				// minTaskTitleName. Accept the overflow, same as before.
				break
			}
			hostRunes[longest] = hostRunes[longest][:len(hostRunes[longest])-1]
		}

		tooNarrow := false
		for i, hr := range hostRunes {
			if len(hr) <= 2 && len([]rune(allHosts[i])) > 2 {
				tooNarrow = true
				break
			}
		}

		if !tooNarrow {
			hostDisplay := make([]string, len(hostRunes))
			for i, hr := range hostRunes {
				hostDisplay[i] = string(hr)
			}
			return HostColumnLayout{TitleColWidth: titleColWidth, HostDisplay: hostDisplay}
		}
		// Falls through to summary mode below - the host list just
		// computed is discarded, the whole point of switching being to
		// stop needing it.
	}

	// Summary mode: TitleColWidth is sized against the widest
	// OK/Changed/Skipped/Failed/Unreachable summary string across every
	// task instead of the host list's own width - same shrink-to-
	// minTaskTitleName-then-accept-overflow pattern as above, just against
	// a different, much narrower and hostname-count-independent content
	// width.
	summaryWidth := WidestSummaryWidth(state)
	titleColWidth := desiredTitleWidth
	if titleColWidth+TitleHostGapFloor+summaryWidth > availContent {
		floor := desiredTitleWidth
		if floor > MinTaskTitleName {
			floor = MinTaskTitleName
		}
		target := availContent - TitleHostGapFloor - summaryWidth
		if target < floor {
			target = floor
		}
		titleColWidth = target
		if titleColWidth < 0 {
			titleColWidth = 0
		}
	}
	return HostColumnLayout{TitleColWidth: titleColWidth, SummaryMode: true}
}

// TaskLabel builds one TASK row's full text, including its leading indent.
// Per TUI.md's "Tree View - third iteration", every host in allHosts (the
// run-wide, alphabetically-sorted set of hosts seen so far - see
// PlaybookState.AllHosts) is shown left-aligned after the task title,
// starting at the same column on every row (layout.TitleColWidth, shared
// across every row for one rebuild - see computeHostColumnLayout), each
// colored by its outcome for this specific task, or gray if this task
// hasn't recorded a result for it yet. If allHosts is empty (nothing has
// been discovered run-wide yet - always true right up until the first
// result of the run lands, for whichever task that turns out to be), the
// row is just the title, with no trailing gap or content - the one case
// where avail (not layout) still governs the title's own truncation,
// since there's no shared column yet to align to.
//
// Fitting/truncation happens entirely in plain, untagged rune space (raw
// task name, raw host names) and only wraps the final, already-correctly-
// sized pieces in color tags and tview.Escape() once, at the end - avoids
// repeated tview.TaggedStringWidth calls, and mirrors the
// truncate-raw-then-escape discipline this function has always used.
//
// A row's own title is truncated (with "…", same convention as before)
// only if its own natural width exceeds layout.TitleColWidth - this is
// how a title that isn't itself the widest can still end up truncated,
// if the shared column ended up narrower than that title (e.g. the
// column was shrunk to fit avail - see computeHostColumnLayout). A
// shorter title is padded with spaces up to TitleColWidth instead, plus
// titleHostGapFloor more before the host list - deterministic, unlike the
// old right-aligned version's "whatever's left over" padding. Hostname
// truncation has no ellipsis marker (per TUI.md's own example) - already
// applied uniformly to every row via layout.HostDisplay, never
// recomputed here.
//
// active marks the currently-executing task (see flattenRows); when true,
// frame (this rebuild's shared spinner frame - see spinnerAt) renders in
// place of the row's leading indent (taskIndent) instead of as a trailing
// suffix after the title - the space it needs comes out of the
// already-existing indent rather than the title column, so every row's
// title column stays the same width regardless of which task, if any,
// happens to be active. When active is false and any host recorded a
// warning for this task (taskHasWarnings - the task-level aggregate;
// hostLabel's own per-host marker gives the precise "which host" once
// expanded), a warningColor ⚠ renders in that same slot instead; taskIndent's
// own plain spaces render there only when neither applies. The spinner
// always wins over the warning glyph when both would otherwise apply -
// in practice they never actually do, since a task's own warnings are
// only knowable for certain once it's finished recording every host's
// result, by which point it's no longer active anyway. Known, accepted
// limitation: U+26A0 has "ambiguous" East Asian Width in Unicode's own
// terms, so a terminal that renders it two columns wide rather than one
// would shift everything after it by one column - not chased further
// here, same tolerance this project already extends to the spinner's own
// Braille frames elsewhere.
//
// selected marks this as the row currently under the cursor (see rebuild's
// selected-row patch, and NewLiveTUI's SetSelectedStyle comment for why
// this can't just be a single uniform List-wide style): the title gets
// black bold text on a light gray background, and each hostname gets black
// bold text on its own outcome color as a background instead of a
// foreground - the inverse of the normal rendering below.
//
// useColor (design-docs/Morehosts.md) only ever matters when
// layout.SummaryMode is true - it decides whether that row's own
// OK/Changed/Skipped/Failed/Unreachable summary renders in color
// (hostSummaryColoredText) or not; the per-host list's own coloring
// (below) is untouched by it, since Morehosts.md scopes this feature to
// summary mode alone, not a whole-app monochrome option.
func TaskLabel(task *playbook.TaskNode, allHosts []string, layout HostColumnLayout, avail int, active bool, frame rune, selected bool, useColor bool) string {
	// One prefix fills taskIndent's single slot (see its own doc comment) -
	// the active spinner takes priority; otherwise a warningColor ⚠ if the
	// task has finished with at least one host's warning recorded; plain
	// spaces otherwise. tview.Escape's own guaranteed-correct handling of
	// "[...]"-shaped text applies to the plain-text branches here too (list
	// rows parse tags, unlike the top bar) - harmless no-op on taskIndent's
	// plain spaces or the spinner rune, neither of which is ever
	// "["-shaped. The warning branch's own tag is constructed here, from a
	// fixed literal and warningColor, never from external data - already
	// safe tag syntax, not something that needs (or should) survive
	// Escape() the way the other two branches do.
	var prefix string
	switch {
	case active:
		prefix = tview.Escape(string(frame) + " ")
	case TaskHasWarnings(task):
		prefix = fmt.Sprintf("[%s]⚠[-] ", WarningColor)
	default:
		prefix = tview.Escape(TaskIndent)
	}

	nameRunes := []rune(task.Name)
	haveHosts := len(allHosts) > 0

	// Normally a foreground-only tag (background left untouched, so it
	// shows whatever the row's base background already is), regular
	// weight, per TUI.md's task-line styling. "silver", not "lightgray":
	// deliberately not grayTag's plain "gray" either - that's already the
	// established color for "host hasn't reported for this task yet";
	// reusing it here for the title itself would blur that distinction.
	// When selected, black bold text on an explicit light gray background
	// instead - see this function's own selected doc above. pureBlack (a
	// hex value, not the named "black") is used everywhere selected text
	// needs black: tcell's named "black" is the base-16 ANSI slot, which
	// some terminal themes remap to a dark gray rather than true black -
	// the same base-16-vs-fixed-value trap already documented for
	// red/maroon (colorTag) elsewhere in this file. A hex value is a fixed
	// RGB, immune to that remapping.
	if !haveHosts {
		// No shared column exists yet (see computeHostColumnLayout) - fall
		// back to fitting the title against the raw available width
		// directly, same as this function always did before a column
		// existed to align to.
		availContent := avail - len(TaskIndent)
		if availContent < 0 {
			availContent = 0
		}
		nameWidth := len(nameRunes)
		truncated := false
		if nameWidth > availContent {
			nameWidth = availContent
			truncated = true
		}
		var rawTitle string
		if truncated && nameWidth >= 1 {
			rawTitle = string(nameRunes[:nameWidth-1]) + "…"
		} else {
			rawTitle = string(nameRunes[:nameWidth])
		}
		title := tview.Escape(rawTitle)
		if selected {
			return prefix + "[" + PureBlack + ":lightgray:b]" + title + "[-:-:-]"
		}
		return prefix + "[silver::-]" + title + "[-::-]"
	}

	nameWidth := len(nameRunes)
	truncatedName := nameWidth > layout.TitleColWidth
	if truncatedName {
		nameWidth = layout.TitleColWidth
	}
	var rawTitle string
	if truncatedName && nameWidth >= 1 {
		rawTitle = string(nameRunes[:nameWidth-1]) + "…"
	} else {
		rawTitle = string(nameRunes[:nameWidth])
	}
	// Escape only now that the raw text is already correctly sized, so
	// slicing above can never cut into an escape sequence Escape() would
	// otherwise have produced.
	title := tview.Escape(rawTitle)

	// Deterministic, unlike the old right-aligned version's "whatever's
	// left over" padding: exactly enough spaces to reach TitleColWidth
	// (0 for the row whose own title defines the column, since
	// nameWidth == TitleColWidth there), plus titleHostGapFloor more -
	// the minimum gap every row gets, per TUI.md.
	padding := layout.TitleColWidth - nameWidth + TitleHostGapFloor

	if layout.SummaryMode {
		ok, changed, skipped, failed, unreachable := task.Counts()
		if selected {
			// Same uniform light-gray-background treatment as the
			// !haveHosts fallback above, applied to title+string as one
			// block, rather than per-field color-as-background segments
			// the way the host-list branch below does - there's no
			// established blending convention for a handful of short
			// label:count fields the way there is for hostnames, and
			// Morehosts.md doesn't ask for one.
			plain := tview.Escape(HostSummaryPlainText(ok, changed, skipped, failed, unreachable))
			return prefix + "[" + PureBlack + ":lightgray:b]" + title + strings.Repeat(" ", padding) + plain + "[-:-:-]"
		}
		styledTitle := "[silver::-]" + title + "[-::-]"
		return prefix + styledTitle + strings.Repeat(" ", padding) + HostSummaryColoredText(ok, changed, skipped, failed, unreachable, useColor)
	}

	if selected {
		// No neutral/uncolored cells anywhere: the gray title background
		// extends right up to one space before the first hostname (that
		// last space, and every hostname's own leading space thereafter,
		// belongs to that hostname's own color block instead) - so hosts
		// are concatenated directly, not joined with a separate plain " ".
		greyPadding := padding - 1
		if greyPadding < 0 {
			greyPadding = 0
		}
		var b strings.Builder
		b.WriteString(prefix)
		b.WriteString("[" + PureBlack + ":lightgray:b]")
		b.WriteString(title)
		b.WriteString(strings.Repeat(" ", greyPadding))
		b.WriteString("[-:-:-]")
		// Host[0]'s own leading space stays solid-colored (transitioning
		// from the title's grey isn't attempted here - the user's ask was
		// specifically about the space between adjacent hostnames). Every
		// later host's leading space is replaced by a halfBlock transition
		// cell blending the previous host's color into this one's, instead
		// of just restating this host's own color again.
		var prevTag string
		for i, h := range allHosts {
			o, done := task.Hosts[h]
			tag := GrayTag
			if done {
				tag = ColorTag(o)
			}
			name := tview.Escape(layout.HostDisplay[i])
			if i == 0 {
				fmt.Fprintf(&b, "[%s:%s:b] %s[-:-:-]", PureBlack, tag, name)
			} else {
				b.WriteString(HostTransition(prevTag, tag))
				fmt.Fprintf(&b, "[%s:%s:b]%s[-:-:-]", PureBlack, tag, name)
			}
			prevTag = tag
		}
		return b.String()
	}

	// Plain foreground-colored text on a plain " " separator - tried the
	// same halfBlock transition used in the selected branch above here too,
	// but confirmed (by looking at it) that it doesn't read well against
	// unselected hostnames' plain foreground-only coloring - reverted.
	styledTitle := "[silver::-]" + title + "[-::-]"
	hostSegments := make([]string, len(allHosts))
	for i, h := range allHosts {
		o, done := task.Hosts[h]
		tag := GrayTag
		if done {
			tag = ColorTag(o)
		}
		hostSegments[i] = fmt.Sprintf("[%s]%s[-]", tag, tview.Escape(layout.HostDisplay[i]))
	}

	return prefix + styledTitle + strings.Repeat(" ", padding) + strings.Join(hostSegments, " ")
}

// OutcomeDetail returns the extra parenthesized bit hostLabel (and
// recap.go's own recapTaskRowText) append after a host's outcome for one
// task - what it is depends on the outcome: only OK/Changed/Failed and
// Skipped have one defined so far; "" for Unreachable, rendering exactly
// as before.
func OutcomeDetail(task *playbook.TaskNode, host string) string {
	switch task.Hosts[host] {
	case playbook.OutcomeOK, playbook.OutcomeChanged, playbook.OutcomeFailed:
		return OutputSummary(task.Raw[host])
	case playbook.OutcomeSkipped:
		return SkipDetail(task.Raw[host])
	}
	return ""
}

// DecodeWarnings decodes raw and returns its own "warnings" field (nil
// if raw doesn't decode, or carries none) - shared by hasWarnings (a
// plain presence check, backing the tree's own ⚠ indicators) and
// recap.go's own recapTaskDetail (which needs the actual joined text),
// so both agree on exactly what "this result has a warning" means: any
// result carrying a non-empty "warnings" field, "regardless of outcome
// or module" - the same rule buildOutputTab's own Warnings section in
// the drill-down view already follows.
func DecodeWarnings(raw json.RawMessage) interface{} {
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return decoded["warnings"]
}

// HasWarnings reports whether raw's own "warnings" field is present and
// non-empty. Backs hostLabel's own per-host ⚠ marker and taskHasWarnings
// below - only presence matters there, not the warning text itself
// (that's what drilling down, or the recap's own "warnings" category,
// is for).
func HasWarnings(raw json.RawMessage) bool {
	return JoinedStringList(DecodeWarnings(raw), "\n") != ""
}

// TaskHasWarnings reports whether any host recorded a warning for task -
// backs taskLabel's own aggregate ⚠ marker on the collapsed row (which
// has no per-host granularity to show at all, the same reason its
// outcome coloring is already per-host-segment rather than a single
// verdict for the whole row - expanding reveals which host, via
// hostLabel's own marker).
func TaskHasWarnings(task *playbook.TaskNode) bool {
	for _, raw := range task.Raw {
		if HasWarnings(raw) {
			return true
		}
	}
	return false
}

// HostLabel builds one host row's text, colored uniformly by its single
// outcome. No width-based truncation/alignment applies here - that rule is
// TASK-row-specific per TUI.md. selected mirrors taskLabel's own parameter -
// black bold text on the outcome color as a background, instead of the
// outcome color as a foreground.
//
// The returned text includes its own leading indent (hostIndent): a
// warningColor ⚠ in column 1, followed by plain spaces out to
// hostIndent's own width, when this host's own result for task carries a
// warning - warnings are orthogonal to outcome (confirmed empirically:
// buildOutputTab already shows a task's own "warnings" field regardless
// of outcome or module), so the marker deliberately doesn't inherit
// whatever color the outcome itself is - or hostIndent's own plain spaces
// unchanged when there's no warning to show. Originally this was a
// trailing marker appended after the outcome detail instead, then a
// leading marker one column right of column 1 - both reverted after live
// use: a marker at the row's very first column, with the hostname itself
// never shifting regardless of whether it's showing, reads clearest.
func HostLabel(task *playbook.TaskNode, host string, selected bool) string {
	o := task.Hosts[host]
	line := fmt.Sprintf("%s: %s%s", tview.Escape(host), o, tview.Escape(OutcomeDetail(task, host)))
	prefix := tview.Escape(HostIndent)
	if HasWarnings(task.Raw[host]) {
		prefix = fmt.Sprintf("[%s]⚠[-]%s", WarningColor, strings.Repeat(" ", len(HostIndent)-1))
	}
	if selected {
		return prefix + fmt.Sprintf("[%s:%s:b]%s[-:-:-]", PureBlack, ColorTag(o), line)
	}
	return prefix + fmt.Sprintf("[%s]%s[-]", ColorTag(o), line)
}
