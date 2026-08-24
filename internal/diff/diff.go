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

// Implements the "diff" hotkey (design-docs/Diff.md): pick a past run to
// compare the current tree against, show only what differs, and drill
// down into a matched task's own per-tab unified diff between the two
// runs.
package diff

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/ansibledoc"
	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/revisit"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DiffChromeStyle is design-docs/Diff.md's own "yet another coloring so
// it's clear we are in diff mode" - a fourth chrome color alongside
// BarStyle (navy, live) and ReplayBarStyle (purple, revisit). fuchsia,
// like purple/maroon elsewhere in this app, is a fixed base-16 ANSI
// palette slot rather than an RGB approximation, so it renders reliably
// across terminal themes - see design-docs/Colors.md. The tree's own
// content is NOT recolored to match (see DiffTaskRowText/DiffHostRowText
// below) - only this chrome is; content keeps the normal outcome palette,
// with underline/strikethrough as the only diff signal, per your own
// "colored as normal... differences only visualized by underlining."
var DiffChromeStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorFuchsia).Bold(true)

// ReplayRun loads a saved run's own .jsonl (runlog.go) into a fresh
// PlaybookState - the exact replay mechanism OpenRevisitEntry already
// uses for showing a revisited run, reused here for diff mode's own
// comparison ("old") run.
func ReplayRun(runID string) (*playbook.PlaybookState, error) {
	jsonlPath, _ := config.RunLogPaths(config.TangsibleStatePath, runID)
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	state := &playbook.PlaybookState{}
	for item := range runner.ScanEvents(f, nil) {
		if item.IsEvent {
			state.Apply(item.Ev)
		}
	}
	return state, nil
}

// LastRunID returns the most recently recorded RunID for playbook/role in
// cfg - a stand-in for "the current session's own saved run," looked up
// fresh from state.toml rather than threaded through NewLiveTUI's own
// live-generation plumbing (which changes across reruns and would need
// its own cross-goroutine-synchronized tracker to stay correct). Safe
// because, by the time 'd' is even pressable (processDone), the current
// generation's own invocation record has already been finalized
// (FinalizeInvocation) and is the last entry recorded for this target -
// nothing else can have appended after it without the user having done
// something else in a separate session concurrently.
func LastRunID(cfg config.StateConfig, playbook, role string) string {
	for _, h := range cfg.History {
		if h.Playbook != playbook || h.Role != role {
			continue
		}
		if len(h.Invocations) == 0 {
			return ""
		}
		return h.Invocations[len(h.Invocations)-1].RunID
	}
	return ""
}

// RunDiffFlow is the 'd' key's own entry point (tui.go's SetInputCapture,
// design-docs/Diff.md) - called from inside app.Suspend, so it owns the
// real terminal for as long as it runs and hands it back automatically on
// return. Loops between the candidate-run list (reusing revisit's own
// list UI, RunRevisitListTUI - just fed ResolveDiffCandidates instead of
// ResolveRevisitEntries) and the diff tree view, for as long as the user
// keeps picking runs to compare against - the same "list <-> detail" loop
// shape RunRevisitVerb already uses, one level down (a diff tree instead
// of a full live NewLiveTUI).
func RunDiffFlow(currentState *playbook.PlaybookState, targetPlaybook, targetRole, currentTags, currentHosts string, currentSourceIndex source.TaskSourceIndex) {
	for {
		cfg, _ := config.PruneMissingRunLogs(config.TangsibleStatePath)
		currentRunID := LastRunID(cfg, targetPlaybook, targetRole)
		candidates := ResolveDiffCandidates(targetPlaybook, targetRole, currentTags, currentHosts, currentRunID, cfg)
		if len(candidates) == 0 {
			return // nothing comparable on record - silent no-op, same
			// gating style as 'r'/'d' itself already have for
			// !processDone.
		}

		selected, ok := revisit.RunRevisitListTUI(candidates)
		if !ok {
			return
		}

		oldState, err := ReplayRun(selected.RunID)
		if err != nil {
			continue // couldn't load it (deleted since the list was
			// built?) - loop back to a freshly re-pruned list rather
			// than getting stuck on a dead entry.
		}

		// The comparison run's own Task-definition source: BuildTaskSourceIndex
		// against its own playbook, same as revisit.go's own OpenRevisitEntry -
		// empty for a role-originated entry (its own generated stub is long
		// gone, same accepted gap documented there).
		var oldSourceIndex source.TaskSourceIndex
		if selected.Role == "" {
			oldSourceIndex = source.BuildTaskSourceIndex(selected.Playbook)
		}

		RunDiffTreeTUI(AlignPlays(oldState, currentState), currentSourceIndex, oldSourceIndex)
	}
}

// DiffPlayName is a PlayAlignment's own display name - whichever side is
// present (they share a name by construction when matched, see
// AlignPlays).
func DiffPlayName(pa PlayAlignment) *playbook.PlayNode {
	if pa.NewPlay != nil {
		return pa.NewPlay
	}
	return pa.OldPlay
}

// DiffTaskKey is a TaskAlignment's own identity for expand/collapse
// tracking - whichever side is present.
func DiffTaskKey(a TaskAlignment) *playbook.TaskNode {
	if a.NewTask != nil {
		return a.NewTask
	}
	return a.OldTask
}

// DiffHostRowID identifies one expanded host row for cursor-preservation/
// selected-row-rendering purposes (RunDiffTreeTUI) - mirrors the live
// tree's own HostRowID{task, host} shape.
type DiffHostRowID struct {
	task *playbook.TaskNode
	host string
}

// FlattenDiffRows walks alignments into an ordered row list - only plays
// that contain a difference (PlayAlignmentHasDifferences), only tasks
// that are themselves different (TaskDiffers), each task's own host rows
// included only while expanded[its own key] is true. Mirrors FlattenRows'
// general shape (same row type, same expand-by-identity convention) but
// is deliberately simpler: no shared host-column width/shrink algorithm
// (ComputeHostColumnLayout) - diff mode shows far fewer, more focused
// rows than the live tree ever does, so that sophistication isn't needed
// here.
//
// selectedID is whichever row's own id (a *PlayNode, *TaskNode, or
// DiffHostRowID) is currently under the cursor - every row is rendered in
// one pass, unlike the live tree's own FlattenRows (which renders
// unselected first and patches the one selected row's text afterward,
// needing a shared column-width computed once regardless of which row
// ends up selected). No such shared state exists here, so a single pass
// comparing each row's own id against selectedID as it's built is simpler
// and sufficient. Reported live: without doing this at all (an earlier
// version), no row was EVER rendered with its own selected styling -
// TreeList (unlike tview.List) has no built-in "current row" look of its
// own, so the cursor was completely invisible.
func FlattenDiffRows(alignments []PlayAlignment, expanded map[*playbook.TaskNode]bool, selectedID any, showOutput func(TaskAlignment, string)) []uikit.Row {
	titleColWidth := DiffTitleColWidth(alignments)

	var rows []uikit.Row
	for _, pa := range alignments {
		if !PlayAlignmentHasDifferences(pa) {
			continue
		}
		// design-docs/Diff.md: "don't render a play line differently" -
		// PlayRowText reused exactly as the live tree uses it.
		playID := DiffPlayName(pa)
		rows = append(rows, uikit.Row{Text: uikit.PlayRowText(playID, playID == selectedID), ID: playID})

		for _, ta := range pa.Tasks {
			if !TaskDiffers(ta) {
				continue
			}
			ta := ta
			key := DiffTaskKey(ta)
			rows = append(rows, uikit.Row{
				Text: DiffTaskRowText(ta, titleColWidth, key == selectedID),
				ID:   key,
				Selected: func() {
					expanded[key] = !expanded[key]
				},
			})
			if expanded[key] {
				rows = append(rows, DiffHostRows(ta, selectedID, showOutput)...)
			}
		}
	}
	return rows
}

// DiffTitleColWidth returns the widest (in runes) task title among every
// row FlattenDiffRows would currently produce for alignments - every task
// row shares this one column width (DiffTaskLine below) so the trailing
// host list lines up at the same column regardless of any one row's own
// title length, the same alignment requirement the live tree's own
// ComputeHostColumnLayout satisfies for the main tree. A real, reported
// gap in the first version of this: with no shared column at all, every
// task row's own host list started immediately after THAT row's own
// title, at a different column per row - "the formatting is totally
// off," not a cosmetic nicety. Deliberately simpler than
// ComputeHostColumnLayout: no shrink-to-fit pass for a narrow terminal or
// a very long title - diff mode's own scale (a debugging aid, opened
// occasionally, not the constantly-redrawn main tree) doesn't call for
// that sophistication, just the basic shared-padding part that actually
// caused the reported problem.
func DiffTitleColWidth(alignments []PlayAlignment) int {
	width := 0
	for _, pa := range alignments {
		if !PlayAlignmentHasDifferences(pa) {
			continue
		}
		for _, ta := range pa.Tasks {
			if !TaskDiffers(ta) {
				continue
			}
			if w := DiffTaskDisplayWidth(ta); w > width {
				width = w
			}
		}
	}
	return width
}

// DiffTaskDisplayWidth is DiffTitleColWidth's own per-row width
// measurement - the task's own title, plus, for an unmatched task, the
// plain-text UnmatchedMarker appended right after it (DiffTaskLine) - has
// to be accounted for here too, or the shared host column would stop
// lining up for exactly the rows that carry a marker.
func DiffTaskDisplayWidth(a TaskAlignment) int {
	w := len([]rune(DiffTaskKey(a).Name))
	if m := UnmatchedMarker(a); m != "" {
		w += len([]rune(m))
	}
	return w
}

// UnmatchedMarker is DiffTaskLine's own plain-text signal for an
// unmatched task - "" for a matched one. Added alongside
// wholeLineFlag's strikethrough/italic, not instead of it, after live
// testing found strikethrough (design-docs/Diff.md) doesn't render at
// all over mosh, in any terminal - a marker needs no text-attribute
// support whatsoever, so it's the one signal guaranteed visible
// regardless of what the user's terminal (or terminal multiplexer)
// actually implements.
func UnmatchedMarker(a TaskAlignment) string {
	switch {
	case a.NewTask == nil:
		return " (old only)"
	case a.OldTask == nil:
		return " (new only)"
	default:
		return ""
	}
}

// DiffTaskRowText renders one task alignment's collapsed row, indented by
// TaskIndent - the same prefix the live tree's own TaskLabel uses to set
// a task row apart from a play row's own column-0 title (another part of
// "the formatting is totally off": task rows had no indent of their own
// at all in the first version of this, landing flush with play rows).
//
// Unmatched (only present on one side): the whole line - title and every
// host - carries two marks at once: underline ("u") for a task only
// present in the new run, strikethrough+italic ("si") for one only
// present in the old run (strikethrough alone, originally - see
// design-docs/Diff.md's mosh note for why italic joined it rather than
// replacing it: strikethrough still reads as "something's gone" for
// terminals that render it, italic is what survives on the ones that
// don't), plus UnmatchedMarker's own plain-text "(old only)"/"(new
// only)" suffix, which needs no text-attribute support at all. Rendered
// from whichever side actually has data (there's nothing else to render
// it from).
//
// Matched (present, and different, on both sides): rendered from the NEW
// task's own data (design-docs/Diff.md's "render based on the new
// version"), normal outcome coloring throughout, with only the
// individual hosts DifferingHosts identifies actually underlined.
func DiffTaskRowText(a TaskAlignment, titleColWidth int, selected bool) string {
	switch {
	case a.NewTask == nil:
		return DiffTaskLine(a.OldTask, titleColWidth, "si", UnmatchedMarker(a), nil, selected)
	case a.OldTask == nil:
		return DiffTaskLine(a.NewTask, titleColWidth, "u", UnmatchedMarker(a), nil, selected)
	default:
		return DiffTaskLine(a.NewTask, titleColWidth, "", "", DifferingHosts(a), selected)
	}
}

// DiffTaskLine is DiffTaskRowText's own shared renderer for both the
// unmatched (wholeLineFlag/marker set, underlineHosts nil) and matched
// (wholeLineFlag/marker "", underlineHosts naming the differing ones)
// cases. marker (UnmatchedMarker's own plain-text "(old only)"/"(new
// only)", or "" for a matched task) renders right after the title, inside
// the same styled span - so it picks up wholeLineFlag's own
// strikethrough/italic/underline too, reinforcing rather than competing
// with it. Padding to titleColWidth (plus TitleHostGapFloor's worth of
// breathing room, same floor the live tree's own TaskLabel uses) is what
// actually lines hosts up across every row - see DiffTitleColWidth, which
// already accounts for marker's own width via DiffTaskDisplayWidth.
// selected uses the same uniform PureBlack-on-lightgray convention
// host.go's own HostRowText/revisit.go's own RevisitRowText already do
// for a read-only browsing list - not the live tree's own per-host
// colored-background blending (TaskLabel's selected variant), which
// would be considerably more machinery for a view this feature doesn't
// ask for; the title's own padding is included in that highlighted
// block, the hosts themselves (already individually colored by
// DiffHostList) are not.
func DiffTaskLine(task *playbook.TaskNode, titleColWidth int, wholeLineFlag, marker string, underlineHosts map[string]bool, selected bool) string {
	name := task.Name
	displayWidth := len([]rune(name)) + len([]rune(marker))
	pad := titleColWidth - displayWidth + uikit.TitleHostGapFloor
	if pad < 1 {
		pad = 1
	}
	nameEscaped := tview.Escape(name) + tview.Escape(marker)
	padding := strings.Repeat(" ", pad)
	hosts := DiffHostList(task, wholeLineFlag, underlineHosts)
	if selected {
		return fmt.Sprintf("%s[%s:lightgray:b%s]%s%s[-:-:-]%s", uikit.TaskIndent, uikit.PureBlack, wholeLineFlag, nameEscaped, padding, hosts)
	}
	return fmt.Sprintf("%s[silver::%s]%s[-::-]%s%s", uikit.TaskIndent, wholeLineFlag, nameEscaped, padding, hosts)
}

// SortedHostOrder returns a copy of hostOrder sorted alphabetically -
// hostOrder itself is report-arrival order (aggregate.go's TaskNode.record
// appends whichever host happens to answer first), which is deliberately
// what render.go's plain-text dump follows, but is meaningless (and, with
// parallel forks, different from task to task) as a *display* order. The
// live tree's own TaskLabel never has this problem since it iterates
// state.AllHosts (alphabetically sorted) instead of any one task's
// HostOrder - diff mode has no equivalent run-wide host set spanning both
// the old and new PlaybookState trees, so it sorts locally here instead.
func SortedHostOrder(hostOrder []string) []string {
	sorted := make([]string, len(hostOrder))
	copy(sorted, hostOrder)
	sort.Strings(sorted)
	return sorted
}

// DiffHostList renders task's own HostOrder as a space-separated,
// per-host colored list (ColorTag per outcome, matching TaskLabel's own
// collapsed-row convention in spirit - though without its shared-column-
// width shrink algorithm, deliberately, see FlattenDiffRows' own doc
// comment). wholeLineFlag, if non-empty, applies uniformly to every host
// (an unmatched task's whole line); otherwise underlineHosts says which
// specific hosts get their own "u" (a matched, differing task).
func DiffHostList(task *playbook.TaskNode, wholeLineFlag string, underlineHosts map[string]bool) string {
	parts := make([]string, 0, len(task.HostOrder))
	for _, host := range SortedHostOrder(task.HostOrder) {
		flag := wholeLineFlag
		if flag == "" && underlineHosts[host] {
			flag = "u"
		}
		parts = append(parts, fmt.Sprintf("[%s]%s[-::-]", DiffColorTag(task.Hosts[host], flag), tview.Escape(host)))
	}
	return strings.Join(parts, " ")
}

// DiffColorTag is ColorTag(o) with flag ("u"/"s"/"") appended as the tag's
// own third (flags) component when set - tview's own "[fg::flags]" form,
// already used elsewhere in this file (PlayRowText's own "[white::b]").
func DiffColorTag(o playbook.Outcome, flag string) string {
	if flag == "" {
		return uikit.ColorTag(o)
	}
	return uikit.ColorTag(o) + "::" + flag
}

// DiffHostRows renders a TaskAlignment's own expanded host rows - shown
// while its collapsed row is expanded (FlattenDiffRows). An unmatched
// alignment renders every host from whichever side is present, all marked
// uniformly - the whole task is new/gone, so every one of its hosts is
// relevant. A matched, differing one renders *only* the hosts DifferingHosts
// actually flags: unlike the collapsed row (DiffHostList), which shows
// every host so the shared-column host list stays recognizable at a
// glance, expanding a task is a deliberate "show me what's different" -
// per a live bug report, a task with e.g. 9 unchanged hosts and 1 changed
// one listed all 10 host lines, burying the one that actually mattered.
// Enter on a host row calls showOutput(a, host) - the drill-down view
// (showDiffOutput, below). selectedID - see FlattenDiffRows' own doc
// comment.
func DiffHostRows(a TaskAlignment, selectedID any, showOutput func(TaskAlignment, string)) []uikit.Row {
	task := a.NewTask
	wholeLineFlag := ""
	switch {
	case task == nil:
		task = a.OldTask
		wholeLineFlag = "s"
	case a.OldTask == nil:
		wholeLineFlag = "u"
	}
	diffHosts := DifferingHosts(a)

	rows := make([]uikit.Row, 0, len(task.HostOrder))
	for _, host := range SortedHostOrder(task.HostOrder) {
		host := host
		flag := wholeLineFlag
		if flag == "" && diffHosts[host] {
			flag = "u"
		} else if flag == "" {
			continue // matched, unchanged host - collapsed row already covers it
		}
		id := DiffHostRowID{task: task, host: host}
		rows = append(rows, uikit.Row{
			Text:     DiffHostRowText(task, host, flag, id == selectedID),
			ID:       id,
			Selected: func() { showOutput(a, host) },
		})
	}
	return rows
}

// DiffHostRowText mirrors HostLabel's own format (host: outcome + detail)
// with flag ("u"/"s"/"") layered on as an extra tag component - see
// DiffColorTag. selected uses the same uniform convention DiffTaskLine
// does, not HostLabel's own per-outcome-colored-background variant.
func DiffHostRowText(task *playbook.TaskNode, host string, flag string, selected bool) string {
	o := task.Hosts[host]
	line := fmt.Sprintf("%s: %s%s", tview.Escape(host), o, tview.Escape(uikit.OutcomeDetail(task, host)))
	prefix := tview.Escape(uikit.HostIndent)
	if selected {
		return prefix + fmt.Sprintf("[%s:lightgray:b%s]%s[-:-:-]", uikit.PureBlack, flag, line)
	}
	return prefix + fmt.Sprintf("[%s]%s[-::-]", DiffColorTag(o, flag), line)
}

// TviewTagPattern matches a real tview color/style tag - the same tag
// grammar tview.Escape/Unescape themselves use internally (confirmed
// against tview's own util.go), reused here rather than reinvented.
// Deliberately doesn't match "[[]", the literal-bracket escape sequence
// tview.Escape produces (that pattern's own character class excludes a
// second "[", so a genuine "[[]" is never mistaken for a tag here).
var TviewTagPattern = regexp.MustCompile(`\[[a-zA-Z0-9_,;: \-\."#]+\]`)

// StripTags removes tview color/style tags from s, leaving the plain text
// a tab's own content actually says - needed before diffing two runs'
// tab content (BuildDiffOutputTabs below): the existing tab builders
// (BuildTaskTab etc.) return tag-decorated text meant for direct display,
// and diffing that as-is would treat a plain color change (e.g. an
// outcome going from green to red) as a text difference, which is noise -
// the tree's own underline marking already surfaces outcome changes
// separately. Not a full tview tag parser, same "documented heuristic,
// good enough" tolerance this project already applies elsewhere (e.g.
// ColorizeYAML).
func StripTags(s string) string {
	return TviewTagPattern.ReplaceAllString(s, "")
}

// DecodeTaskHostResult decodes task's own raw result for host - the same
// decode step BuildOutputTabs already does inline, pulled out here since
// diff mode needs it independently for both the old and new side of a
// matched alignment.
func DecodeTaskHostResult(task *playbook.TaskNode, host string) (map[string]interface{}, bool) {
	raw := task.Raw[host]
	if len(raw) == 0 {
		return nil, false
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

// FetchDocsSync fetches ansible-doc's own text for action synchronously -
// design-docs/Diff.md's Docs tab is "left as is" (shown once, undiffed),
// but diff mode's own drill-down doesn't have a live session to hang the
// main tree's async fetch-then-redraw-plus-cache machinery
// (NewLiveTUI's docsCache/resolveCache) off of, and this view is opened
// rare enough, relative to the live tree's own constant back-and-forth,
// that a brief synchronous ansible-doc invocation is an acceptable
// simplification rather than replicating that whole apparatus here too.
func FetchDocsSync(action string) uikit.ResolvedRender {
	if action == "" {
		return uikit.ResolvedRender{}
	}
	text, err := ansibledoc.FetchAnsibleDoc(action)
	if err != nil {
		return uikit.ResolvedRender{Err: err.Error()}
	}
	return uikit.ResolvedRender{Text: text}
}

// DiffTwoTexts unified-diffs a's vs b's own plain text (after StripTags) -
// "" if they're identical, same "nothing to show for this tab" convention
// BuildOutputTabs' own add() already uses.
func DiffTwoTexts(a, b string) string {
	a, b = StripTags(a), StripTags(b)
	if a == b {
		return ""
	}
	return uikit.ColorizedUnifiedDiff(uikit.DiffLinesWithMarker(a), uikit.DiffLinesWithMarker(b), "old run", "new run")
}

// SingleRunTabs is BuildOutputTabs' own result, filtered down to what
// design-docs/Diff.md's drill-down asks for even for an unmatched
// (old-only/new-only) task alignment, which has nothing to diff against -
// "each tab just shows that one run's own single content, same as the
// normal (non-diff) drill-down would." Diff (ansible's own before/after)
// is dropped, same reasoning as the matched case below ("too confusing"
// applies whether or not there's a second run to compare against); so is
// Resolved, deliberately - see BuildDiffOutputTabs' own doc comment for
// why diff mode skips it for now.
//
// side is "old"/"new" when task genuinely exists only on that side of the
// alignment - UnmatchedTaskNote is then prepended to the Task tab so the
// drill-down says outright why there's nothing diffed here, rather than
// silently looking identical to a normal single-run view (real user
// confusion, reported live: a task shown in the diff tree - e.g. a
// notify:-triggered handler that fired in one run and wasn't notified in
// the other - drilled into and showing no diff markup at all read as "no
// differences found" rather than "this task didn't run on the other
// side"). "" (the decode-failure fallback in BuildDiffOutputTabs, where
// the task genuinely exists on both sides) skips the note entirely - it
// would be actively wrong there.
func SingleRunTabs(task *playbook.TaskNode, host string, sourceIndex source.TaskSourceIndex, docs uikit.ResolvedRender, side string) (names []string, contents []string) {
	allNames, allContents := uikit.BuildOutputTabs(task, host, sourceIndex, uikit.ResolvedRender{}, docs)
	for i, n := range allNames {
		if n == "Diff" || n == "Resolved" {
			continue
		}
		content := allContents[i]
		if n == "Task" && side != "" {
			content = UnmatchedTaskNote(side) + content
		}
		names = append(names, n)
		contents = append(contents, content)
	}
	return names, contents
}

// UnmatchedTaskNote is SingleRunTabs' own callout, prepended to the Task
// tab for an old-only/new-only task alignment - see SingleRunTabs' doc
// comment for why this exists. Deliberately terse (one line, no
// elaboration) per live feedback - the strikethrough-marked tree row
// already says "unmatched"; this just confirms it in the one place a
// user might otherwise read "no diff shown" as "nothing found."
func UnmatchedTaskNote(side string) string {
	this := "old"
	if side == "new" {
		this = "new"
	}
	return fmt.Sprintf("[yellow::b]Task only present in the %s run.[-::-]\n\n", this)
}

// BuildDiffOutputTabs is diff mode's own drill-down tab builder
// (design-docs/Diff.md): for a matched task alignment, unified-diffs each
// tab's own plain-text content between the two runs; for an unmatched
// one, falls back to SingleRunTabs (nothing to diff against). Docs is
// always shown once, undiffed - module docs don't vary per run. Resolved
// is deliberately omitted here, unlike the live tree's own drill-down:
// it doesn't represent a genuinely per-run recorded fact in the first
// place (design-docs/Drilldown, Resolved Values.md - it always re-resolves
// against *current* vars, regardless of which run's task is shown), and
// for a matched pair whose own source is unchanged it would just diff
// identically-resolved text against itself for no reason; when the
// source genuinely did change between runs, Task definition's own diff
// already surfaces that. Can be added later if it proves genuinely
// wanted once this is in front of you - not chased further for now.
func BuildDiffOutputTabs(a TaskAlignment, host string, newSourceIndex, oldSourceIndex source.TaskSourceIndex) (names []string, contents []string) {
	// A real, reported crash: TaskAction(a.NewTask, host) was called
	// unconditionally, before ever checking a.NewTask == nil below - for
	// an old-only task (present only in the comparison run), a.NewTask is
	// nil, and TaskAction's own t.Raw[host] is a nil pointer dereference.
	// Reproducible every time by expanding an old-only (strikethrough)
	// task and opening one of its hosts.
	var action string
	if a.NewTask != nil {
		action = uikit.TaskAction(a.NewTask, host)
	} else if a.OldTask != nil {
		action = uikit.TaskAction(a.OldTask, host)
	}
	docs := FetchDocsSync(action)

	switch {
	case a.NewTask == nil:
		return SingleRunTabs(a.OldTask, host, oldSourceIndex, docs, "old")
	case a.OldTask == nil:
		return SingleRunTabs(a.NewTask, host, newSourceIndex, docs, "new")
	}

	oldDecoded, oldOK := DecodeTaskHostResult(a.OldTask, host)
	newDecoded, newOK := DecodeTaskHostResult(a.NewTask, host)
	if !oldOK || !newOK {
		// Shouldn't happen for a host TaskDiffers already confirmed is
		// present and decodable on both sides, but not trusted blindly -
		// fall back to the new run's own single-run tabs rather than
		// showing nothing. side is "" here (not "new") - the task genuinely
		// exists on both sides, so UnmatchedTaskNote's "only present in..."
		// framing would be wrong.
		return SingleRunTabs(a.NewTask, host, newSourceIndex, docs, "")
	}

	add := func(name, oldText, newText string) {
		if diff := DiffTwoTexts(oldText, newText); diff != "" {
			names = append(names, name)
			contents = append(contents, diff)
		}
	}

	add("Task",
		uikit.BuildTaskTab(a.OldTask, host, oldDecoded, a.OldTask.Hosts[host]),
		uikit.BuildTaskTab(a.NewTask, host, newDecoded, a.NewTask.Hosts[host]))
	add("Output",
		uikit.BuildOutputTab(oldDecoded, a.OldTask.Hosts[host]),
		uikit.BuildOutputTab(newDecoded, a.NewTask.Hosts[host]))
	add("Task definition",
		uikit.BuildSourceTab(a.OldTask.Path, oldSourceIndex),
		uikit.BuildSourceTab(a.NewTask.Path, newSourceIndex))
	add("Details",
		uikit.BuildDetailsTab(oldDecoded, a.OldTask.Raw[host]),
		uikit.BuildDetailsTab(newDecoded, a.NewTask.Raw[host]))

	if !uikit.DocsTabHidden(docs) {
		names = append(names, "Docs")
		contents = append(contents, uikit.BuildDocsTab(docs))
	}

	return names, contents
}

// RunDiffTreeTUI shows alignments' own filtered, diff-annotated tree in a
// standalone Application - not another NewLiveTUI (the data model here is
// pairs of tasks/hosts, not one PlaybookState; forcing it through
// NewLiveTUI's single-state-oriented plumbing - sourceIndex, requestRerun,
// live-generation bookkeeping - would fight it at every turn rather than
// reuse it, per design-docs/Diff.md's own implementation notes).
// newSourceIndex/oldSourceIndex back the drill-down's own Task-definition
// diffing (BuildDiffOutputTabs) - the current session's own sourceIndex,
// and one freshly built for the comparison run (RunDiffFlow).
//
// Two nested "back" levels, both Esc/q: from the drill-down (viewingOutput)
// back to the tree, then from the tree back out of this Application
// entirely (to RunDiffFlow's own candidate list) - no 'd' binding inside
// diff mode itself, confirmed deliberate, not just unaddressed.
//
// The cursor-highlighting/rebuild-on-toggle structure mirrors
// RunRevisitListTUI's own (revisit.go) - TreeList has no built-in
// "current row" look of its own - but rebuilds on more than a selection
// change: toggling a task's own expand state changes the row *count*
// too, handled the same way (recompute currentRows, re-add everything,
// restore the cursor by index).
func RunDiffTreeTUI(alignments []PlayAlignment, newSourceIndex, oldSourceIndex source.TaskSourceIndex) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := uikit.NewTreeList()
	expanded := map[*playbook.TaskNode]bool{}

	header := tview.NewTextView().SetDynamicColors(true).SetText(" tangsible diff ")
	header.SetTextStyle(DiffChromeStyle)
	footer := tview.NewTextView().SetDynamicColors(true).
		SetText(" enter: expand/collapse task, open host  q/esc: back  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	footer.SetTextStyle(DiffChromeStyle)

	treeFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)

	outputTabs := uikit.NewTabbedPane()
	outputHeader := tview.NewTextView().SetDynamicColors(true).SetText(" tangsible diff ")
	outputHeader.SetTextStyle(DiffChromeStyle)
	outputFooter := tview.NewTextView().SetDynamicColors(true).
		SetText(" tab/shift-tab: switch tab  q/esc/enter: back  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	outputFooter.SetTextStyle(DiffChromeStyle)
	outputFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(outputHeader, 1, 0, false).
		AddItem(outputTabs.Primitive(), 0, 1, true).
		AddItem(outputFooter, 1, 0, false)

	pages := tview.NewPages().
		AddPage("tree", treeFlex, true, true).
		AddPage("output", outputFlex, true, false)
	viewingOutput := false

	showDiffOutput := func(a TaskAlignment, host string) {
		names, contents := BuildDiffOutputTabs(a, host, newSourceIndex, oldSourceIndex)
		prims := make([]tview.Primitive, len(names))
		for i, content := range contents {
			tv := tview.NewTextView().SetDynamicColors(true)
			tv.SetText(content)
			prims[i] = tv
		}
		outputTabs.SetTabs(names, prims)
		viewingOutput = true
		pages.SwitchToPage("output")
	}
	closeDiffOutput := func() {
		viewingOutput = false
		pages.SwitchToPage("tree")
	}

	// currentID is whichever row's own id (a *PlayNode, *TaskNode, or
	// DiffHostRowID) is logically under the cursor - tracked by identity,
	// not raw index, since expand/collapse shifts row order (same
	// convention the live tree's own currentID uses). nil only before the
	// very first rebuildRows() call. lastRows is what rebuildRows most
	// recently built - SetChangedFunc needs it to translate a genuine
	// index change (arrow-key navigation) back into an id.
	var currentID any
	var lastRows []uikit.Row
	rebuilding := false
	var rebuildRows func()
	rebuildRows = func() {
		rebuilding = true
		defer func() { rebuilding = false }()

		// Two passes, not one - a real, reported bug caught the hard way:
		// a single pass built with currentID's OLD value (nil, on the
		// very first call - nothing has been selected yet, so nothing
		// COULD render as selected) can only discover the right index
		// *after* that render already happened, too late to reflect it -
		// the cursor was rendered invisible on open, every time, until
		// some later navigation happened to trigger a second rebuild.
		//
		// Pass 1 (probe): build once with currentID as it stands now,
		// purely to find out which index it lands on (expand/collapse
		// can move it) - or, if it's nil or no longer present, fall back
		// to index 0. Nothing from this pass is kept.
		probeRows := FlattenDiffRows(alignments, expanded, currentID, showDiffOutput)
		selectedIdx := 0
		if currentID != nil {
			for i, r := range probeRows {
				if r.ID == currentID {
					selectedIdx = i
					break
				}
			}
		}
		if selectedIdx >= len(probeRows) {
			selectedIdx = len(probeRows) - 1
		}
		if selectedIdx < 0 {
			selectedIdx = 0
		}
		if len(probeRows) > 0 {
			currentID = probeRows[selectedIdx].ID
		}

		// Pass 2 (real): currentID now definitely matches
		// probeRows[selectedIdx], so THIS render correctly marks exactly
		// that one row selected - TreeList (unlike tview.List) has no
		// built-in "current row" look of its own, so this is the entire
		// highlighting mechanism.
		lastRows = FlattenDiffRows(alignments, expanded, currentID, showDiffOutput)

		list.Clear()
		for _, r := range lastRows {
			r := r
			toggle := r.Selected
			list.AddItem(r.Text, func() {
				if toggle != nil {
					toggle()
				}
				rebuildRows()
			})
		}
		if len(lastRows) > 0 {
			list.SetCurrentItem(selectedIdx)
		}
	}
	list.SetChangedFunc(func(index int) {
		if rebuilding {
			return
		}
		// A genuine navigation (arrow keys, etc.) - TreeList's own
		// SetCurrentItem already moved the cursor; note which row that
		// now logically is, then rebuild so ITS row (and no longer the
		// previous one) actually gets rendered with the selected
		// styling - same reasoning as the comment in rebuildRows above.
		if index >= 0 && index < len(lastRows) {
			currentID = lastRows[index].ID
		}
		rebuildRows()
	})
	rebuildRows()

	if list.GetItemCount() == 0 {
		// Shouldn't normally happen - RunDiffFlow only reaches here after
		// the user picked a real comparison run from a non-empty
		// candidate list - but ResolveDiffCandidates/AlignPlays finding
		// literally nothing different isn't impossible (an exact rerun
		// with no changes at all), so this is worth saying rather than
		// showing a silently empty screen.
		header.SetText(" tangsible diff - no differences found ")
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			app.Stop()
			return nil
		}
		if viewingOutput {
			switch {
			case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyEnter, event.Key() == tcell.KeyRune && event.Rune() == 'q':
				closeDiffOutput()
				return nil
			case event.Key() == tcell.KeyTab:
				outputTabs.Next()
				return nil
			case event.Key() == tcell.KeyBacktab:
				outputTabs.Prev()
				return nil
			case event.Key() == tcell.KeyRune && event.Rune() == 'j':
				return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
			case event.Key() == tcell.KeyRune && event.Rune() == 'k':
				return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
			}
			return event
		}
		switch {
		case event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyRune && event.Rune() == 'q':
			app.Stop()
			return nil
		case event.Key() == tcell.KeyCtrlA:
			return tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone)
		case event.Key() == tcell.KeyCtrlE:
			return tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'j':
			return tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)
		case event.Key() == tcell.KeyRune && event.Rune() == 'k':
			return tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)
		}
		return event
	})

	app.SetRoot(pages, true).SetFocus(list)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
