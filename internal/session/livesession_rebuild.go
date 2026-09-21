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

package session

import (
	"strings"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
)

// progressPosition reads whatever runner.ProgressTracker the current
// generation has (progH.Load() is nil-safe to call Position() on - see
// progress.go - both before this session's very first skeleton has ever
// been built, and for "rerun"'s own startup dialog, where nothing has run
// yet at all).
func (s *liveSession) progressPosition() (position, total int) {
	return s.progH.Load().Position()
}

// activeTaskNow returns the run's current in-progress task, or nil once
// the run has finished - the same "frozen means no active task" rule
// s.rebuild() applies to its own activeTask local, pulled out so
// navigateMainTask/navigateOutputTask/applyFilter (all outside rebuild)
// can compute the identical thing when deciding what a filter should keep
// visible (see TaskVisible's isActive parameter).
func (s *liveSession) activeTaskNow() *playbook.TaskNode {
	if s.processDone.Load() {
		return nil
	}
	return s.state.CurrentTask()
}

// revealExpandedTask, called right after a task row's Enter/Space/click
// toggle (or the Right-arrow handler, see handleRight below) just
// expanded it, scrolls the s.list down - if needed, and only as far as it
// can - so the newly revealed host rows are actually visible, rather than
// landing below the bottom of the screen with no visible change. The
// cursor stays on the task row itself throughout, so TreeList's own
// ensureVisible (see treelist.go - it only runs when SetCurrentItem's
// index actually changes) never fires here on its own; this is the sole
// mechanism that scrolls to reveal a task's newly-expanded children. Only
// ever scrolls further down from wherever the view already was, never
// up. If the whole block (the task row plus all its hosts) doesn't fit
// in the viewport at all, this simply reveals as much of the tail as
// fits.
func (s *liveSession) revealExpandedTask(t *playbook.TaskNode) {
	_, _, _, height := s.list.GetInnerRect()
	if height <= 0 {
		return
	}
	taskIndex := -1
	for i, r := range s.currentRows {
		if r.ID == t {
			taskIndex = i
			break
		}
	}
	if taskIndex == -1 {
		return
	}
	blockEnd := taskIndex + len(t.HostOrder) // last newly-revealed row's index
	desired := blockEnd - height + 1
	if desired > s.list.GetOffset() {
		s.list.SetOffset(desired)
	}
}

// currentMainBottomBarText appends the revisit-only "Esc: back to list"
// hint onto MainBottomBarText for as long as s.revisitActive stays true,
// and - independently, regardless of s.revisitActive, since s.checkMode
// never goes false once true - a "CHECK MODE" note whenever this
// session's generation was invoked with --check: chrome color alone
// doesn't reach a NO_COLOR/monochrome terminal, so an explicit place to
// put a textual mode indicator was already established as the pattern
// for something color-driven chrome can't carry on its own. Reads both
// flags fresh on every call rather than being decided once, same
// reasoning s.chromeStyle/s.chromeBg above don't need (those are only
// ever applied at construction, with submitRerun resetting the actual
// widgets directly afterward) - s.bottomBar's text, unlike its style, is
// legitimately re-set many times over a session's life (closeOutput,
// rebuild's own split-mode toggle), and each of those call sites should
// see s.revisitActive's current value, not a snapshot from construction.
func (s *liveSession) currentMainBottomBarText() string {
	text := uikit.MainBottomBarText
	if s.requestRerun == nil {
		// Matches SetInputCapture's own 'r' guard below: nothing to
		// advertise a key that's a guaranteed no-op right now (a Phase 2
		// revisit session, per design-docs/Revisit.md, before
		// rerun-from-revisit exists).
		text = strings.Replace(text, "r: re-run  ", "", 1)
	}
	if s.checkMode {
		text += " [CHECK MODE - dry run] "
	}
	if s.revisitActive {
		text += " Esc: back to the list "
	}
	return text
}

// chromeColorName is s.chromeBg's own tag-name equivalent - "navy"/
// "olive"/"purple" - for the progress-fill lines (TopBarText/
// ComposeSplitHeaderLine/s.outputTopBar's own plain fill, all below),
// which bake their unfilled-portion background into inline
// [white:<name>:b] tags rather than reading it from the TextView's own
// SetTextStyle the way every other chrome bar does (see s.chromeStyle
// above) - a single tcell.Style can't vary per-column the way a sweeping
// fill needs to. Read fresh on every call, same reasoning as
// currentMainBottomBarText just above: these are called from within
// s.rebuild() on every redraw, not just once at construction, so this
// needs to see s.revisitActive's current value each time, not a snapshot
// - discovered the hard way, live: s.chromeStyle/s.chromeBg alone left
// the top/split/output bars still showing plain navy under their own
// progress-fill text, since SetTextStyle never actually painted those
// characters at all.
func (s *liveSession) chromeColorName() string {
	if s.revisitActive {
		return "purple"
	}
	return s.liveChromeColorName
}

// showElapsed suppresses the top/split bars' own spinner/mm:ss clock for
// as long as s.revisitActive stays true - a revisit session's elapsed is
// always ~0 (design-docs/Revisit.md: only a run's start time was ever
// saved, never its duration), and showing that would read as "this just
// finished in no time" rather than as the honest "we don't know" it
// actually is. Read fresh on every call, same reasoning as
// chromeColorName/currentMainBottomBarText just above.
func (s *liveSession) showElapsed() bool { return !s.revisitActive }

// outputBottomBarNormalStyle resolves s.outputBottomBar's own non-search
// style fresh on every call, the same "s.revisitActive decides, s.chromeStyle
// alone goes stale after a real rerun" pattern submitRerun's own bar reset
// already established - needed because clearing a tab search
// (tabSearchPanel.clear, livesession_tabsearch.go) has to restore
// s.outputBottomBar to whatever its normal style currently is, not whatever
// s.chromeStyle happened to be when NewLiveTUI first ran. Also used
// directly by the 'y' clipboard-copy key handler (SetInputCapture), which
// needs the identical restore before showing its own transient status text.
func (s *liveSession) outputBottomBarNormalStyle() tcell.Style {
	if s.revisitActive {
		return s.chromeStyle
	}
	return s.liveChromeStyle
}

// switchPage, alongside s.currentPageName (set to "main" at construction
// - see NewLiveTUI), tracks which of "main"/"output"/"split" is
// currently frontmost, so s.rebuild()'s own live resize-reactivity (see
// design-docs/TwoPanedLayout.md) can tell whether a page switch is
// actually needed before calling s.pages.SwitchToPage - which, per
// tview's own source, re-focuses the new front page every time it's
// called, even redundantly. Calling it unconditionally on every rebuild
// (every heartbeat tick while a drill-down is open) would be harmless in
// practice but is needless churn; gating on a real change avoids it for
// free.
func (s *liveSession) switchPage(name string) {
	if name == s.currentPageName {
		return
	}
	s.currentPageName = name
	s.pages.SwitchToPage(name)
}

// rebuild recomputes the tree's current state (elapsed/frozen status,
// progress fill, split/pane mode, tree/recap row flattening,
// notifications, selection) and re-renders it - the central closure
// design-docs/Restructuring.md's own postponed "Phase 3" singled out as
// needing to become a real method first, since nearly every other
// closure in NewLiveTUI calls it to trigger a redraw. Untouched by this
// extraction beyond the mechanical rename every closure in this refactor
// gets (bare state -> s.field): its own internal logic is unchanged.
func (s *liveSession) rebuild() {
	s.rebuilding = true
	defer func() { s.rebuilding = false }()

	now := time.Now() // captured once per rebuild - shared by the top
	// bar's elapsed/spinner and every active row's spinner below, so a
	// single pass renders a self-consistent instant rather than
	// drifting per-row/per-call time.Now() reads.
	frozen := s.processDone.Load()
	elapsed := now.Sub(s.startedAt)
	if frozen {
		if !s.haveFrozenElapsed {
			s.frozenElapsed = elapsed
			s.haveFrozenElapsed = true
		}
		elapsed = s.frozenElapsed
	}
	// Read once per rebuild and shared by both s.topBar and (while a
	// drill-down is open) s.outputTopBar below - ProgressFillLine's own
	// fill needs the identical (progressPos, progressTotal, frozen)
	// triple for both bars to stay in visual agreement with each
	// other, and there's no reason to re-read the tracker twice for
	// one rebuild pass anyway.
	progressPos, progressTotal := s.progressPosition()

	// Two-pane layout (design-docs/TwoPanedLayout.md) is live, not
	// decided once at open time: every rebuild - including ones driven
	// purely by a terminal resize, with no other event to piggyback on
	// (see the resize-watcher goroutine and the heartbeat ticker below)
	// - re-evaluates whether the terminal is currently wide enough for
	// a split view, and keeps the tree pane's own width current within
	// that range. s.pages itself, not s.list, is the width source: as the
	// app's own root primitive it always reports the true current
	// terminal size no matter which page is frontmost, unlike s.list's
	// own width, which stops tracking the terminal 1:1 the moment a
	// two-pane session fixes it to the tree pane's own share (see
	// width/s.lastTotalWidth below - two genuinely different quantities
	// now, not one).
	_, _, totalWidth, _ := s.pages.GetInnerRect()
	s.lastTotalWidth = totalWidth // compared against s.pages' *current*
	// width by the resize-watcher goroutine below, to notice a resize
	// that happened with no other event to piggyback a rebuild on.
	if s.viewingOutput {
		s.splitMode = s.twoPaneLayout && totalWidth >= uikit.SplitMinTotalWidth
		if s.splitMode {
			s.splitBody.ResizeItem(s.treeBody, uikit.SplitTreeWidth(totalWidth), 0)
			s.bottomBar.SetText(uikit.SplitBottomBarText)
			s.switchPage("split")
		} else {
			s.bottomBar.SetText(s.currentMainBottomBarText())
			s.switchPage("output")
		}

		if s.splitMode {
			// s.splitHeader is one single widget spanning the terminal's
			// true full width (totalWidth) - unlike an earlier version
			// of this, which kept s.topBar/s.outputTopBar as two
			// independently-positioned widgets either side of
			// s.splitDivider and tried to keep their own fills in
			// agreement: that was reported live, twice, to leave a
			// couple of columns right at the seam the wrong color
			// regardless of how carefully the two widths were derived
			// to match each other. One widget's own width trivially
			// agrees with itself, which is what actually closes that
			// class of bug. s.splitDivider itself (the body rows below
			// this one) is deliberately not part of this string at
			// all - this header has no separate divider glyph of its
			// own, so the single column that visually sits above it
			// just participates in the fill like any other character.
			//
			// ComposeSplitHeaderLine (not ComposeTopBarLine +
			// s.outputTopBarPlainText concatenated after it - a second
			// live report caught two real bugs in that approach at
			// once, see its own doc comment) builds hostAndTask from
			// s.outputHost/s.outputTask directly - the one host/task this
			// drill-down is actually showing, not s.state.AllHosts (the
			// tree-only bar's own "every host seen so far" s.list).
			hostAndTask := s.outputHost
			if s.outputTask != nil {
				hostAndTask = s.outputHost + "   " + s.outputTask.Name
			}
			s.splitHeader.SetText(uikit.ProgressFillLine(
				uikit.ComposeSplitHeaderLine(s.playbookName, s.isRole, hostAndTask, elapsed, frozen, s.currentFilter, totalWidth, s.showElapsed()),
				progressPos, progressTotal, frozen, s.chromeColorName()))
		} else {
			// Padded to the full terminal width before the fill is
			// applied (same reason ComposeTopBarLine pads its own
			// line) - s.outputTopBar's own "host — task" text is
			// usually much shorter than the row, and a fill tag only
			// colors runes actually present in the string.
			full := s.outputTopBarPlainText
			if gap := totalWidth - len([]rune(full)); gap > 0 {
				full += strings.Repeat(" ", gap)
			}
			s.outputTopBar.SetText(uikit.ProgressFillLine(full, progressPos, progressTotal, frozen, s.chromeColorName()))
		}
	}

	// width is derived from totalWidth/s.splitMode (both already decided
	// just above, from s.pages' own rect - always accurate regardless of
	// which page is frontmost), not s.list.GetInnerRect() directly - a
	// real, reported bug: tview only updates a primitive's own rect
	// during its next Draw() pass, which hasn't happened yet at this
	// point in s.rebuild() whenever THIS very call is what just changed
	// which page is frontmost (e.g. closeOutput's own
	// s.switchPage("main") followed immediately by s.rebuild()) - so
	// s.list.GetInnerRect() would still report whatever narrower width
	// it had as part of s.splitBody a moment ago. Reported live: closing
	// a two-pane drill-down left the host-column-shrink algorithm
	// (ComputeHostColumnLayout/FlattenRows below) rendering far too
	// narrow, only correcting itself once some *other* event (a real
	// terminal resize) forced a genuine Draw() pass first. s.list fills
	// its own outer Flex row's entire width whenever "main" is
	// frontmost (same "s.topBar shares s.list's own width" reasoning just
	// below), so totalWidth itself already *is* s.list's own eventual
	// width in that case - deriving it directly sidesteps the stale-
	// rect problem entirely rather than working around it.
	width := totalWidth
	if s.splitMode {
		width = uikit.SplitTreeWidth(totalWidth)
	}
	// Belt-and-suspenders only: TaskLabel is panic-safe for any width,
	// but clamp defensively in case totalWidth is ever unexpectedly
	// tiny (e.g. before Run()'s first real-size draw pass).
	if width < 20 {
		width = 20
	}
	if !s.splitMode {
		// s.topBar shares s.list's own width here (both are full-width
		// children of the same outer Flex row when "main" is
		// frontmost) - reused below for TopBarText's own right-
		// alignment/truncation too rather than re-deriving a second
		// width from s.topBar.GetInnerRect(). Skipped entirely in split
		// mode, where s.splitHeader (above) shows this same information
		// instead - s.topBar itself sits unused, off-page, for the
		// duration of a split session.
		s.topBar.SetText(uikit.TopBarText(s.playbookName, s.isRole, s.state.AllHosts, elapsed, frozen, s.currentFilter, progressPos, progressTotal, width, s.chromeColorName(), s.showElapsed()))
	}

	// One-time, right on the running-to-frozen transition: for a
	// genuine failure (see GenuineFailure - shared with StatusRowText
	// below so the two can't disagree on what counts as one), jump
	// straight to the host that actually failed, expanding its task,
	// so a single Enter shows the drill-down with no navigation
	// needed. Must happen before FlattenRows runs below, since it
	// reads s.expanded to decide which host rows to include - setting
	// it after would miss the newly-expanded row in this same pass.
	//
	// Gated on the failed task still matching the currently active
	// filter (Filters.md's own open question about this, resolved
	// once the search filter existed to make it a real case: "filter
	// wins, skip the auto-jump" - simpler than forcing a non-matching
	// row into view, and doesn't quietly break the filter's own
	// promise that only matching tasks are ever shown). isActive is
	// unconditionally false here rather than s.activeTaskNow() - a frozen
	// run has no in-progress task by definition, so there's no need to
	// even call it. A/C/F can't actually trigger this: a failed task
	// always matches "Changed" and "Failed" by definition, so only a
	// search term that happens not to match the failure can skip the
	// jump.
	if frozen && !s.failureCursorPlaced {
		s.failureCursorPlaced = true
		if uikit.GenuineFailure(int(s.exitCode.Load()), s.state.HadUnreachable, runner.AnsibleUserInterruptedExitCode) {
			if t, h := uikit.LastFailedTaskAndHost(s.state); t != nil && uikit.TaskVisible(t, s.currentFilter, s.sourceIndex, false) {
				s.expanded[t] = true
				s.currentID = uikit.HostRowID{Task: t, Host: h}
				s.following = false
			}
		}
	}

	// notify_playbook_finished (design-docs/Notifications.md) - same
	// one-shot running-to-frozen transition as the auto-jump just
	// above, gated on s.everStarted for the same reason hasStatusRow
	// below is: a "rerun" session's startup dialog starts frozen with
	// nothing having actually run yet (see s.everStarted's own doc
	// comment), which must never read as a finished playbook.
	// Excluded for a user-interrupted generation (exit 99) - design-
	// docs/Notifications.md's "should not be fired when it's a direct,
	// immediate result of a user interaction" - the user just pressed
	// q/Ctrl-C themselves, so a notification telling them that would be
	// pure noise. The doc's other named exclusion, a pre-flight-gate
	// failure, needs no code here at all: that path never spawns a TUI
	// in the first place (see runner.StartFirstGeneration), so
	// s.rebuild() - and this whole closure - never runs for it.
	// Also fires s.suppressedTaskFailures' own one-time "N further task
	// failures suppressed" notice, right alongside, if notify_task_failed
	// suppressed any - only knowable now that the generation is done.
	if frozen && s.everStarted && !s.finishedNotifySent {
		s.finishedNotifySent = true
		if code := int(s.exitCode.Load()); code != runner.AnsibleUserInterruptedExitCode {
			if s.notifyPlaybookFinishedKind != config.NotificationOff {
				genuineFailure := uikit.GenuineFailure(code, s.state.HadUnreachable, runner.AnsibleUserInterruptedExitCode)
				body := uikit.PlaybookFinishedBody(s.playbookName, genuineFailure, s.state.HadUnreachable)
				_ = uikit.SendNotification(s.notifyPlaybookFinishedKind, uikit.NotificationTitle, body)
			}
			if s.suppressedTaskFailures > 0 && s.notifyTaskFailedKind != config.NotificationOff {
				_ = uikit.SendNotification(s.notifyTaskFailedKind, uikit.NotificationTitle, uikit.SuppressedTaskFailuresBody(s.suppressedTaskFailures))
			}
		}
	}

	activeTask := s.activeTaskNow()

	// treeAllHosts is s.state.AllHosts normally, or nil while a two-pane
	// drill-down session is open (design-docs/TwoPanedLayout.md): hosts
	// aren't shown on collapsed tree rows in that mode (the drill-down
	// pane already shows exactly which host is selected - see
	// s.showOutput's live-sync). ComputeHostColumnLayout/TaskLabel both
	// already have a documented allHosts == nil fallback - no shared
	// column, title rendered alone against avail - normally only
	// reachable transiently before the run's first host reports
	// anything; reused here deliberately rather than adding a second
	// code path. s.state.AllHosts itself is untouched - only these two
	// call sites (and the selected-row re-render below) see the
	// override, so the top bar/filters/etc. keep seeing the real list.
	treeAllHosts := s.state.AllHosts
	if s.splitMode {
		treeAllHosts = nil
	}

	// Computed once per rebuild and reused for every row - both
	// FlattenRows' own per-row TaskLabel calls and the standalone
	// selected-row re-render just below - so the cursor row always
	// aligns to the identical column every other row uses (see
	// ComputeHostColumnLayout).
	layout := uikit.ComputeHostColumnLayout(s.state, treeAllHosts, width, !s.useColor)

	s.currentRows = uikit.FlattenRows(s.state, s.expanded, width, layout, treeAllHosts, activeTask, uikit.SpinnerAt(elapsed), s.currentFilter, s.sourceIndex, s.showOutput, s.useColor)
	hasStatusRow := false
	if frozen && s.everStarted {
		if text := uikit.StatusRowText(int(s.exitCode.Load()), s.state.HadUnreachable, runner.AnsibleUserInterruptedExitCode); text != "" {
			s.currentRows = append(s.currentRows,
				uikit.Row{Text: "", ID: uikit.StatusDividerRowID{}},
				uikit.Row{Text: text, ID: uikit.StatusRowID{}},
			)
			hasStatusRow = true
		}
		// Recap (design-docs/Recap.md) - appended below the status rows
		// regardless of whether one was actually shown, so this doesn't
		// silently disappear if StatusRowText's own "always non-empty"
		// guarantee ever changes. Rendered as more rows in the exact
		// same flat s.list the live tree already uses, not a separate
		// page - Home/End/PageUp/PageDown/arrow navigation all already
		// work on it for free this way. A blank spacer, the "Summary"
		// heading, its underline, and another blank spacer come first,
		// setting the section off visually from the status line above.
		s.currentRows = append(s.currentRows,
			uikit.Row{Text: "", ID: recapDividerBeforeHeading},
			uikit.Row{Text: recapHeadingRowText(), ID: recapHeadingRow},
			uikit.Row{Text: recapHeadingUnderlineRowText(), ID: recapHeadingUnderlineRow},
			uikit.Row{Text: "", ID: recapDividerAfterHeading},
			uikit.Row{Text: recapNarrativeRowText(s.state, elapsed), ID: recapNarrativeRow},
			uikit.Row{Text: "", ID: recapDividerAfterNarrative},
		)
		s.currentRows = append(s.currentRows, flattenRecapRows(s.state, s.recapHostExpanded, s.recapCategoryExpanded, s.showOutputFromRecap)...)
	}

	if len(s.currentRows) == 0 {
		s.list.Clear()
		s.lastAppliedSelectedIndex = -1 // whatever appears once real rows
		// exist again must be treated as a genuine first selection, not
		// coincidentally matched against whatever index was applied
		// before everything was cleared.
		return
	}

	// Determine which row the cursor belongs on *before* AddItem, not
	// after - see the patch step right below, which needs to know this
	// to re-render that one row's text. s.following pins to the newest
	// *real* row; otherwise restore by s.currentID's identity (row order
	// shifts as things are appended, so a raw index can't be trusted
	// across rebuilds), defaulting to 0 if that id no longer exists
	// (shouldn't happen - nothing is ever removed - but not indexing
	// out of range if it somehow did).
	selectedIndex := 0
	if s.following {
		// Skip back past the trailing status/divider rows (see
		// StatusRowText) - they have no selected-row rendering
		// variant (see the switch below), so s.following would
		// otherwise land the cursor on a row that looks identical
		// whether selected or not: from the user's perspective, the
		// cursor simply vanishes once a run finishes. Landing on the
		// last real row instead keeps the existing, visible
		// highlight - this now always applies, since StatusRowText
		// stopped ever returning "" for a finished run.
		selectedIndex = len(s.currentRows) - 1
		for selectedIndex > 0 {
			_, isDivider := s.currentRows[selectedIndex].ID.(uikit.StatusDividerRowID)
			_, isStatus := s.currentRows[selectedIndex].ID.(uikit.StatusRowID)
			if !isDivider && !isStatus {
				break
			}
			selectedIndex--
		}
	} else {
		for i, r := range s.currentRows {
			if r.ID == s.currentID {
				selectedIndex = i
				break
			}
		}
	}

	// Re-render just the row under the cursor with its selected
	// styling (see PlayRowText/TaskLabel/HostLabel's own selected
	// parameter, and NewLiveTUI's SetSelectedStyle comment for why
	// this is done here rather than via a single List-wide style).
	// StatusRowID/StatusDividerRowID rows have no selected variant and
	// fall through untouched - they have no selected callback either
	// (see FlattenRows), so the cursor never deliberately lands there
	// via Enter, only by navigating past them.
	switch id := s.currentRows[selectedIndex].ID.(type) {
	case *playbook.PlayNode:
		s.currentRows[selectedIndex].Text = uikit.PlayRowText(id, true)
	case *playbook.TaskNode:
		s.currentRows[selectedIndex].Text = uikit.TaskLabel(id, treeAllHosts, layout, width, id == activeTask, uikit.SpinnerAt(elapsed), true, s.useColor)
	case uikit.HostRowID:
		s.currentRows[selectedIndex].Text = uikit.HostLabel(id.Task, id.Host, true)
	case recapHostRowID:
		s.currentRows[selectedIndex].Text = recapHostRowText(string(id), recapForHost(s.state, string(id)), recapComputeColumnWidths(s.state), true)
	case recapCategoryRowID:
		for _, cat := range recapForHost(s.state, id.host).Categories {
			if cat.Label == id.label {
				s.currentRows[selectedIndex].Text = recapCategoryRowText(cat, true)
				break
			}
		}
	case recapTaskRowID:
		detail := recapTaskDetail(id.task, id.host, id.label)
		s.currentRows[selectedIndex].Text = recapTaskRowText(id.task, detail, recapCategoryColor(id.label), true)
	}

	s.list.Clear()
	for _, r := range s.currentRows {
		r := r
		var selected func()
		if r.Selected != nil {
			selected = func() {
				r.Selected()
				s.rebuild()
				if t, ok := r.ID.(*playbook.TaskNode); ok && s.expanded[t] {
					s.revealExpandedTask(t)
				}
			}
		}
		s.list.AddItem(r.Text, selected)
	}
	if selectedIndex == s.lastAppliedSelectedIndex {
		// Same logical selection as last time - just reassert it after
		// Clear()/AddItem() reset s.list's own currentItem, without
		// re-clamping the viewport (see RestoreCurrentItem's doc
		// comment and s.lastAppliedSelectedIndex's above).
		s.list.RestoreCurrentItem(selectedIndex)
	} else {
		s.list.SetCurrentItem(selectedIndex)
		s.lastAppliedSelectedIndex = selectedIndex
	}

	// Reveal the trailing status row(s) on the running-to-frozen
	// transition, same bug class revealExpandedTask already exists
	// for: s.following's own selectedIndex deliberately stays on the
	// last *real* row (see above, past the status divider/text rows,
	// which have no selected-row rendering), and that row was already
	// this s.list's currentItem throughout the run (s.following kept it
	// pinned to the newest row as it streamed in) - so
	// SetCurrentItem's index doesn't actually change here, and
	// TreeList's own ensureVisible (only runs on a genuine index
	// change) never fires. Reported live: once a run's output filled
	// more than one screen, the final "Playbook completed..." line
	// stayed just below the bottom edge until manually scrolled to.
	// Gated on s.following - once the user has navigated away (or the
	// failure-cursor auto-jump above has already turned it off),
	// their own cursor placement wins and this must not fight it by
	// yanking the view back down to the status row.
	if s.following && hasStatusRow {
		if _, _, _, height := s.list.GetInnerRect(); height > 0 {
			desired := len(s.currentRows) - 1 - height + 1
			if desired > s.list.GetOffset() {
				s.list.SetOffset(desired)
			}
		}
	}
}
