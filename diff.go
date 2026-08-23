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

// Implements the "diff" hotkey (design-docs/Diff.md, Phase 2): pick a past
// run to compare the current tree against, and show only what differs.
// No drill-down diffing yet - that's Phase 3.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// diffChromeStyle is design-docs/Diff.md's own "yet another coloring so
// it's clear we are in diff mode" - a fourth chrome color alongside
// barStyle (navy, live) and replayBarStyle (purple, revisit). fuchsia,
// like purple/maroon elsewhere in this app, is a fixed base-16 ANSI
// palette slot rather than an RGB approximation, so it renders reliably
// across terminal themes - see design-docs/Colors.md. The tree's own
// content is NOT recolored to match (see diffTaskRowText/diffHostRowText
// below) - only this chrome is; content keeps the normal outcome palette,
// with underline/strikethrough as the only diff signal, per your own
// "colored as normal... differences only visualized by underlining."
var diffChromeStyle = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorFuchsia).Bold(true)

// replayRun loads a saved run's own .jsonl (runlog.go) into a fresh
// playbookState - the exact replay mechanism openRevisitEntry already
// uses for showing a revisited run, reused here for diff mode's own
// comparison ("old") run.
func replayRun(runID string) (*playbookState, error) {
	jsonlPath, _ := runLogPaths(tangsibleStatePath, runID)
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	state := &playbookState{}
	for item := range scanEvents(f, nil) {
		if item.isEvent {
			state.Apply(item.ev)
		}
	}
	return state, nil
}

// lastRunID returns the most recently recorded RunID for playbook/role in
// cfg - a stand-in for "the current session's own saved run," looked up
// fresh from state.toml rather than threaded through NewLiveTUI's own
// live-generation plumbing (which changes across reruns and would need
// its own cross-goroutine-synchronized tracker to stay correct). Safe
// because, by the time 'd' is even pressable (processDone), the current
// generation's own invocation record has already been finalized
// (finalizeInvocation) and is the last entry recorded for this target -
// nothing else can have appended after it without the user having done
// something else in a separate session concurrently.
func lastRunID(cfg stateConfig, playbook, role string) string {
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

// runDiffFlow is the 'd' key's own entry point (tui.go's SetInputCapture,
// design-docs/Diff.md) - called from inside app.Suspend, so it owns the
// real terminal for as long as it runs and hands it back automatically on
// return. Loops between the candidate-run list (reusing revisit's own
// list UI, runRevisitListTUI - just fed resolveDiffCandidates instead of
// resolveRevisitEntries) and the diff tree view, for as long as the user
// keeps picking runs to compare against - the same "list <-> detail" loop
// shape runRevisitVerb already uses, one level down (a diff tree instead
// of a full live NewLiveTUI).
func runDiffFlow(currentState *playbookState, targetPlaybook, targetRole, currentTags, currentHosts string) {
	for {
		cfg, _ := pruneMissingRunLogs(tangsibleStatePath)
		currentRunID := lastRunID(cfg, targetPlaybook, targetRole)
		candidates := resolveDiffCandidates(targetPlaybook, targetRole, currentTags, currentHosts, currentRunID, cfg)
		if len(candidates) == 0 {
			return // nothing comparable on record - silent no-op, same
			// gating style as 'r'/'d' itself already have for
			// !processDone.
		}

		selected, ok := runRevisitListTUI(candidates)
		if !ok {
			return
		}

		oldState, err := replayRun(selected.RunID)
		if err != nil {
			continue // couldn't load it (deleted since the list was
			// built?) - loop back to a freshly re-pruned list rather
			// than getting stuck on a dead entry.
		}

		runDiffTreeTUI(alignPlays(oldState, currentState))
	}
}

// diffPlayName is a playAlignment's own display name - whichever side is
// present (they share a name by construction when matched, see
// alignPlays).
func diffPlayName(pa playAlignment) *playNode {
	if pa.NewPlay != nil {
		return pa.NewPlay
	}
	return pa.OldPlay
}

// diffTaskKey is a taskAlignment's own identity for expand/collapse
// tracking - whichever side is present.
func diffTaskKey(a taskAlignment) *taskNode {
	if a.NewTask != nil {
		return a.NewTask
	}
	return a.OldTask
}

// flattenDiffRows walks alignments into an ordered row list - only plays
// that contain a difference (playAlignmentHasDifferences), only tasks
// that are themselves different (taskDiffers), each task's own host rows
// included only while expanded[its own key] is true. Mirrors flattenRows'
// general shape (same row type, same expand-by-identity convention) but
// is deliberately simpler: no shared host-column width/shrink algorithm
// (computeHostColumnLayout) - diff mode shows far fewer, more focused
// rows than the live tree ever does, so that sophistication isn't needed
// here.
func flattenDiffRows(alignments []playAlignment, expanded map[*taskNode]bool) []row {
	var rows []row
	for _, pa := range alignments {
		if !playAlignmentHasDifferences(pa) {
			continue
		}
		// design-docs/Diff.md: "don't render a play line differently" -
		// playRowText reused exactly as the live tree uses it.
		rows = append(rows, row{text: playRowText(diffPlayName(pa), false)})

		for _, ta := range pa.Tasks {
			if !taskDiffers(ta) {
				continue
			}
			ta := ta
			key := diffTaskKey(ta)
			rows = append(rows, row{
				text: diffTaskRowText(ta, false),
				id:   key,
				selected: func() {
					expanded[key] = !expanded[key]
				},
			})
			if expanded[key] {
				rows = append(rows, diffHostRows(ta)...)
			}
		}
	}
	return rows
}

// diffTaskRowText renders one task alignment's collapsed row.
//
// Unmatched (only present on one side): the whole line - title and every
// host - carries a single mark: underline ("u") for a task only present
// in the new run, strikethrough ("s") for one only present in the old
// run, per design-docs/Diff.md. Rendered from whichever side actually has
// data (there's nothing else to render it from).
//
// Matched (present, and different, on both sides): rendered from the NEW
// task's own data (design-docs/Diff.md's "render based on the new
// version"), normal outcome coloring throughout, with only the
// individual hosts differingHosts identifies actually underlined.
func diffTaskRowText(a taskAlignment, selected bool) string {
	switch {
	case a.NewTask == nil:
		return diffTaskLine(a.OldTask, "s", nil, selected)
	case a.OldTask == nil:
		return diffTaskLine(a.NewTask, "u", nil, selected)
	default:
		return diffTaskLine(a.NewTask, "", differingHosts(a), selected)
	}
}

// diffTaskLine is diffTaskRowText's own shared renderer for both the
// unmatched (wholeLineFlag set, underlineHosts nil) and matched
// (wholeLineFlag "", underlineHosts naming the differing ones) cases.
// selected uses the same uniform pureBlack-on-lightgray convention
// host.go's own hostRowText/revisit.go's own revisitRowText already do
// for a read-only browsing list - not the live tree's own per-host
// colored-background blending (taskLabel's selected variant), which
// would be considerably more machinery for a view this feature doesn't
// ask for.
func diffTaskLine(task *taskNode, wholeLineFlag string, underlineHosts map[string]bool, selected bool) string {
	name := tview.Escape(task.Name)
	hosts := diffHostList(task, wholeLineFlag, underlineHosts)
	if selected {
		return fmt.Sprintf("[%s:lightgray:b%s]%s[-:-:-]  %s", pureBlack, wholeLineFlag, name, hosts)
	}
	return fmt.Sprintf("[silver::%s]%s[-::-]  %s", wholeLineFlag, name, hosts)
}

// diffHostList renders task's own HostOrder as a space-separated,
// per-host colored list (colorTag per outcome, matching taskLabel's own
// collapsed-row convention in spirit - though without its shared-column-
// width shrink algorithm, deliberately, see flattenDiffRows' own doc
// comment). wholeLineFlag, if non-empty, applies uniformly to every host
// (an unmatched task's whole line); otherwise underlineHosts says which
// specific hosts get their own "u" (a matched, differing task).
func diffHostList(task *taskNode, wholeLineFlag string, underlineHosts map[string]bool) string {
	parts := make([]string, 0, len(task.HostOrder))
	for _, host := range task.HostOrder {
		flag := wholeLineFlag
		if flag == "" && underlineHosts[host] {
			flag = "u"
		}
		parts = append(parts, fmt.Sprintf("[%s]%s[-::-]", diffColorTag(task.Hosts[host], flag), tview.Escape(host)))
	}
	return strings.Join(parts, " ")
}

// diffColorTag is colorTag(o) with flag ("u"/"s"/"") appended as the tag's
// own third (flags) component when set - tview's own "[fg::flags]" form,
// already used elsewhere in this file (playRowText's own "[white::b]").
func diffColorTag(o outcome, flag string) string {
	if flag == "" {
		return colorTag(o)
	}
	return colorTag(o) + "::" + flag
}

// diffHostRows renders a taskAlignment's own expanded host rows - shown
// while its collapsed row is expanded (flattenDiffRows). Same source-of-
// truth rule as the collapsed row: an unmatched alignment renders every
// host from whichever side is present, all marked uniformly; a matched,
// differing one renders the NEW task's own hosts, each individually
// marked only if differingHosts says so.
func diffHostRows(a taskAlignment) []row {
	task := a.NewTask
	wholeLineFlag := ""
	switch {
	case task == nil:
		task = a.OldTask
		wholeLineFlag = "s"
	case a.OldTask == nil:
		wholeLineFlag = "u"
	}
	diffHosts := differingHosts(a)

	rows := make([]row, 0, len(task.HostOrder))
	for _, host := range task.HostOrder {
		flag := wholeLineFlag
		if flag == "" && diffHosts[host] {
			flag = "u"
		}
		rows = append(rows, row{text: diffHostRowText(task, host, flag, false)})
	}
	return rows
}

// diffHostRowText mirrors hostLabel's own format (host: outcome + detail)
// with flag ("u"/"s"/"") layered on as an extra tag component - see
// diffColorTag. selected uses the same uniform convention diffTaskLine
// does, not hostLabel's own per-outcome-colored-background variant.
func diffHostRowText(task *taskNode, host string, flag string, selected bool) string {
	o := task.Hosts[host]
	line := fmt.Sprintf("%s: %s%s", tview.Escape(host), o, tview.Escape(outcomeDetail(task, host)))
	prefix := tview.Escape(hostIndent)
	if selected {
		return prefix + fmt.Sprintf("[%s:lightgray:b%s]%s[-:-:-]", pureBlack, flag, line)
	}
	return prefix + fmt.Sprintf("[%s]%s[-::-]", diffColorTag(o, flag), line)
}

// runDiffTreeTUI shows alignments' own filtered, diff-annotated tree in a
// standalone Application - not another NewLiveTUI (the data model here is
// pairs of tasks/hosts, not one playbookState; forcing it through
// NewLiveTUI's single-state-oriented plumbing - sourceIndex, requestRerun,
// live-generation bookkeeping - would fight it at every turn rather than
// reuse it, per design-docs/Diff.md's own implementation notes). Esc/q
// closes it - no 'd' binding inside diff mode itself, confirmed
// deliberate, not just unaddressed.
//
// The cursor-highlighting/rebuild-on-toggle structure mirrors
// runRevisitListTUI's own (revisit.go) - treeList has no built-in
// "current row" look of its own - but rebuilds on more than a selection
// change: toggling a task's own expand state changes the row *count*
// too, handled the same way (recompute currentRows, re-add everything,
// restore the cursor by index).
func runDiffTreeTUI(alignments []playAlignment) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := newTreeList()
	expanded := map[*taskNode]bool{}

	header := tview.NewTextView().SetDynamicColors(true).SetText(" tangsible diff ")
	header.SetTextStyle(diffChromeStyle)
	footer := tview.NewTextView().SetDynamicColors(true).
		SetText(" enter: expand/collapse task  q/esc: back  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	footer.SetTextStyle(diffChromeStyle)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)

	selectedIdx := 0
	rebuilding := false
	var rebuildRows func()
	rebuildRows = func() {
		rebuilding = true
		defer func() { rebuilding = false }()

		currentRows := flattenDiffRows(alignments, expanded)
		if selectedIdx >= len(currentRows) {
			selectedIdx = len(currentRows) - 1
		}
		if selectedIdx < 0 {
			selectedIdx = 0
		}

		list.Clear()
		for _, r := range currentRows {
			r := r
			toggle := r.selected
			list.AddItem(r.text, func() {
				if toggle != nil {
					toggle()
				}
				rebuildRows()
			})
		}
		if len(currentRows) > 0 {
			list.SetCurrentItem(selectedIdx)
		}
	}
	list.SetChangedFunc(func(index int) {
		if rebuilding {
			return
		}
		selectedIdx = index
	})
	rebuildRows()

	if list.GetItemCount() == 0 {
		// Shouldn't normally happen - runDiffFlow only reaches here after
		// the user picked a real comparison run from a non-empty
		// candidate list - but resolveDiffCandidates/alignPlays finding
		// literally nothing different isn't impossible (an exact rerun
		// with no changes at all), so this is worth saying rather than
		// showing a silently empty screen.
		header.SetText(" tangsible diff - no differences found ")
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyCtrlC, event.Key() == tcell.KeyEscape, event.Key() == tcell.KeyRune && event.Rune() == 'q':
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

	app.SetRoot(flex, true).SetFocus(list)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
