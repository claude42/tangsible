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

// Implements the "revisit" Verb (design-docs/Revisit.md, Phase 2): browse
// previous tangsible runs and reopen one, with the full tree/drill-down
// view (but not yet re-run - see requestRerun's own nil below - that's
// Phase 3). Two entirely separate tview.Application lifetimes, run
// sequentially in the same process: a small standalone list (this file),
// and - once an entry is selected - a real NewLiveTUI, started already
// frozen from replayed data instead of a live process. NewLiveTUI already
// owns and constructs its own *tview.Application internally, so there's no
// way to fold the list in as "just another page" the way host.go's own
// list-then-detail flow does one level down; see the design doc's own
// "Open questions" for why two Applications was chosen deliberately here.
package revisit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	pb "code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/role"
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/source"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// NewLiveTUIFunc matches tui.go's own NewLiveTUI exactly - injected rather
// than called directly, since tui.go stays in package main (NewLiveTUI
// itself doesn't move until Phase 3 breaks it apart) while this package
// doesn't - the same requestRerun/revisitReturn callback-injection pattern
// already used one level down (OpenRevisitEntry -> NewLiveTUI's own
// requestRerun/revisitReturn params), just one level further out. Passing
// tui.go's NewLiveTUI itself as this parameter's argument needs no adapter:
// Go's assignability rules only require identical underlying function
// types, and package-qualified vs. unqualified references to the same
// imported type (e.g. this file's playbook.PlaybookState vs. tui.go's own,
// unqualified within package main) are the same type either way.
type NewLiveTUIFunc func(state *pb.PlaybookState, playbookName string, isRole bool, procH *runner.ProcHandle, processDone, quitting *atomic.Bool, exitCode *atomic.Int32, sourceIndex source.TaskSourceIndex, knownTags []string, startExpanded, twoPaneLayout, colorEnabled bool, initialTags, initialSkipTags, initialHosts string, startWithRerunDialog bool, requestRerun func(startAtTask, tags, skipTags, hosts string), passthroughArgs []string, progH *atomic.Pointer[runner.ProgressTracker], revisitReturn func(), targetPlaybook, targetRole string) (app *tview.Application, applyLive func(pb.RawEvent))

// RunRevisitVerb is "tangsible revisit [<playbook>] [ansible-playbook
// args...]"'s own entry point. Loops between the list and a selected
// entry's detail view for as long as the user keeps picking entries -
// re-pruning and re-resolving the list fresh every time control returns to
// it, so a re-run started and finished during a later phase's detail
// session (once Phase 3 wires that up) would show up in it, and so files
// deleted externally between selections are noticed too.
//
// newLiveTUI is main.go's own tui.NewLiveTUI - see NewLiveTUIFunc's own
// doc comment for why this is injected rather than called directly.
func RunRevisitVerb(args []string, newLiveTUI NewLiveTUIFunc) int {
	shownAnything := false
	// lastRunID is whichever entry was most recently opened - carried into
	// the next RunRevisitListTUI call so closing that entry (Esc/q) puts
	// the cursor back where it was, rather than always resetting to the
	// first row. Deliberately the entry the user actually picked, not
	// whatever a rerun triggered from within it might have since added to
	// history - see RunRevisitListTUI's own doc comment.
	var lastRunID string
	for {
		cfg, err := config.PruneMissingRunLogs(config.TangsibleStatePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't update invocation history in %s: %v\n", config.TangsibleStatePath, err)
		}
		entries := ResolveRevisitEntries(args, cfg)
		if len(entries) == 0 {
			if !shownAnything {
				fmt.Fprintln(os.Stderr, "tangsible: no matching previous runs to revisit")
				return 1
			}
			return 0
		}
		shownAnything = true

		selected, ok := RunRevisitListTUI(entries, lastRunID)
		if !ok {
			return 0
		}
		lastRunID = selected.RunID
		OpenRevisitEntry(selected, newLiveTUI)
	}
}

// RevisitCommandText reconstructs "how tangsible was called" for one
// entry, e.g. "tangsible run site.yml -l zen" or "tangsible role postfix" -
// the playbook/role is always included (unlike the shorthand in the design
// conversation this implements), since an unfiltered list can otherwise mix
// entries for different targets with no way to tell them apart.
func RevisitCommandText(e RevisitEntry) string {
	verb, target := "run", e.Playbook
	if e.Role != "" {
		verb, target = "role", e.Role
	}
	cmd := fmt.Sprintf("tangsible %s %s", verb, target)
	if e.Args != "" {
		cmd += " " + e.Args
	}
	return cmd
}

// FormatRevisitTime renders InvocationRecord.Time (RFC3339 UTC, as
// AppendInvocation stamps it) in the local zone, readably - falling back to
// the raw stored string on a parse failure (shouldn't happen for anything
// this program itself ever wrote, but not trusted blindly, same caveat
// applied to every other stored/event-derived field elsewhere).
func FormatRevisitTime(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// ansible-core's own TaskQueueManager exit-code bits
// (lib/ansible/executor/task_queue_manager.py). ansible-playbook's real
// exit code is a bitmask of these, ORed together across a multi-play run -
// not a flat enum - so e.g. 6 means "some hosts failed AND some were
// unreachable in the same run" (tqmFailedHostsBit|tqmUnreachableHostsBit),
// not some sixth, independent state. tqmMaxBitmask (15) is every bit set at
// once - the upper bound of what a genuine ansible-playbook run can ever
// legitimately produce this way; a code outside that range (having reached
// here past the fixed cases below) is something this app doesn't
// recognize, not a bitmask to decode.
const (
	tqmErrorBit            = 1 // generic/unhandled AnsibleError
	tqmFailedHostsBit      = 2 // at least one host failed a task
	tqmUnreachableHostsBit = 4 // at least one host was unreachable
	tqmBreakPlayBit        = 8 // any_errors_fatal aborted a play early
	tqmMaxBitmask          = tqmErrorBit | tqmFailedHostsBit | tqmUnreachableHostsBit | tqmBreakPlayBit
)

// unhandledExceptionExitCode is ansible-playbook's own fixed exit code for
// an uncaught Python exception in the CLI's outermost error handler (a
// traceback on stderr) - fixed, like AnsibleUserInterruptedExitCode, not
// part of the TaskQueueManager bitmask above.
const unhandledExceptionExitCode = 250

// classifyExit is the single place that decides what an exit code *means*
// - RevisitStatusLabel/RevisitStatusColor are thin readers of its result,
// so the two can never silently disagree on what a given code represents.
// hadUnreachable resolves the one genuine ambiguity left after the fixed
// cases and the bitmask are both accounted for: exit code 4 alone is
// produced by *two* unrelated ansible-playbook code paths that happen to
// hardcode the identical value - TaskQueueManager's own
// RUN_UNREACHABLE_HOSTS bit, and the CLI's entirely separate
// AnsibleParserError handler (a static-include syntax error, resolved
// before TaskQueueManager ever runs) - and the exit code alone can't tell
// them apart. Only real event evidence can (resolveHadUnreachable, below);
// nil means "couldn't determine" (log missing/pruned), which is treated as
// the safer of the two readings, "Failed", rather than assumed benign. Any
// other bit being set alongside 4 already resolves unambiguously via the
// bitmask cases below - the collision only exists when 4 is the *entire*
// code.
func classifyExit(exitCode int, hadUnreachable *bool) (label, color string) {
	switch exitCode {
	case -1: // this app's own sentinel: the process never even started -
		// see SpawnGeneration/runner.NewRequestRerun's own spawn-failure path.
		return "Failed", "red"
	case 0:
		return "Success", "green"
	case runner.AnsibleUserInterruptedExitCode: // 99, the user's own q/Ctrl-C
		return "Aborted", "gray"
	case unhandledExceptionExitCode: // 250
		return "Failed", "red"
	}

	if exitCode > 0 && exitCode <= tqmMaxBitmask {
		switch {
		case exitCode&tqmFailedHostsBit != 0:
			// Dominates regardless of what else is set - 6
			// (failed|unreachable) reads as "Host failed," not
			// "Success," since a genuine failure happened too.
			return "Host failed", "red"
		case exitCode&tqmBreakPlayBit != 0:
			return "Failed", "red"
		case exitCode&tqmErrorBit != 0:
			// Also covers AnsibleOptionsError's own fixed exit code 5
			// (bad CLI args) colliding with the bitmask reading
			// tqmErrorBit|tqmUnreachableHostsBit - both readings are
			// already "Failed," so there's nothing to disambiguate.
			return "Failed", "red"
		case exitCode == tqmUnreachableHostsBit: // == 4, and ONLY that bit
			if hadUnreachable != nil && *hadUnreachable {
				return "Success", "green"
			}
			return "Failed", "red"
		}
	}
	return fmt.Sprintf("Failed (%d)", exitCode), "red" // genuinely unrecognized
}

// RevisitStatusLabel renders e's own exit code as a short, explicit status
// word via classifyExit - see its own doc comment for what each word
// means and why. Spelled out rather than left to color alone: a
// color-only "red timestamp means it failed" turned out not to read
// clearly on its own (live feedback) - matches this app's own existing
// precedent elsewhere of never relying on color as the sole signal
// (Morehosts.md's monochrome-terminal fallback for the tree's own
// host-color list). hadUnreachable is only consulted for the exit-4
// collision (classifyExit); pass nil when it's not known/relevant.
func RevisitStatusLabel(exitCode int, hadUnreachable *bool) string {
	label, _ := classifyExit(exitCode, hadUnreachable)
	return label
}

// RevisitStatusColor is RevisitStatusLabel's own color, shared with the
// selected-row case only insofar as an unselected row applies it directly
// (a selected row uses the uniform PureBlack-on-lightgray convention
// instead, matching host.go's own HostRowText - see RevisitRowText).
func RevisitStatusColor(exitCode int, hadUnreachable *bool) string {
	_, color := classifyExit(exitCode, hadUnreachable)
	return color
}

// resolveHadUnreachable answers classifyExit's own "did this entry's run
// really see an unreachable host?" question by scanning its saved .jsonl
// for a real v2_runner_on_unreachable event - the same ground truth the
// live tree's own HadUnreachable is built from (aggregate.go), just
// replayed from disk instead of live events, and only for entries whose
// exit code actually needs it (see the ExitCode == tqmUnreachableHostsBit
// guard at this function's one call site, RunRevisitListTUI) - most
// entries never pay for this I/O at all. Returns nil - "couldn't
// determine" - if the log can't be opened; ResolveRevisitEntries already
// only ever produces entries with a non-empty RunID, but a file can still
// vanish between resolving the list and opening it here, and this
// function makes no assumption either way.
func resolveHadUnreachable(e RevisitEntry) *bool {
	jsonlPath, _ := config.RunLogPaths(config.TangsibleStatePath, e.RunID)
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	found := false
	for item := range runner.ScanEvents(f, nil) {
		if item.IsEvent && item.Ev.Event == "v2_runner_on_unreachable" {
			found = true
			break // no need to scan the rest of a possibly-large run
		}
	}
	return &found
}

// RevisitRowText renders one list row: <timestamp> - <status, padded to
// labelWidth and colored by RevisitStatusColor> - <RevisitCommandText>.
// labelWidth is the widest RevisitStatusLabel across the whole list
// currently shown (computed once by RunRevisitListTUI, not per row), so
// every row's own trailing " - tangsible ..." column lines up regardless
// of which row's own label happens to be shortest. hadUnreachable is
// e's own precomputed resolveHadUnreachable result (nil for every entry
// that doesn't need it - see RunRevisitListTUI) - resolving it here
// instead would re-scan that entry's .jsonl on every single cursor move,
// since this function is called fresh for every row on every rebuild.
// Selected styling matches host.go's own HostRowText convention (uniform
// PureBlack on lightgray, no per-segment color) rather than reinventing a
// second one.
func RevisitRowText(e RevisitEntry, hadUnreachable *bool, labelWidth int, selected bool) string {
	ts := FormatRevisitTime(e.Time)
	cmd := RevisitCommandText(e)
	label, color := classifyExit(e.ExitCode, hadUnreachable)
	padded := label + strings.Repeat(" ", labelWidth-len([]rune(label)))
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s - %s - %s[-:-:-]", uikit.PureBlack, tview.Escape(ts), tview.Escape(padded), tview.Escape(cmd))
	}
	return fmt.Sprintf("[white]%s[-] - [%s]%s[-] - %s", tview.Escape(ts), color, tview.Escape(padded), tview.Escape(cmd))
}

// RunRevisitListTUI shows entries (already filtered/sorted newest-first by
// ResolveRevisitEntries) in a plain, flat, single-page list - no tree/
// expand-collapse needed here, per your own call that this should look
// like "tangsible hosts"'s own list. Blocks until the user either picks one
// (Enter - returns it with ok=true) or quits (q/Ctrl-C - ok=false).
//
// The cursor-highlighting/rebuild-on-change structure (selectedIdx,
// rebuilding, rebuildRows) is a direct copy of RunHostsListTUI's own
// (host.go) - TreeList has no built-in "current row" look of its own (see
// that function's own doc comment for why), so every list built on it needs
// this same small amount of bookkeeping.
// initialRunID, if it matches one of entries' own RunID, puts the cursor
// there instead of at the top - so a caller looping back to this list
// after the user closes a previously opened entry (RunRevisitVerb below;
// design-docs/Diff.md's own RunDiffFlow, diff.go) can put the cursor back
// where it was, rather than always resetting to the first row. "" (or no
// match - e.g. the entry has since been pruned) falls back to the top,
// same as before this parameter existed.
func RunRevisitListTUI(entries []RevisitEntry, initialRunID string) (RevisitEntry, bool) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := uikit.NewTreeList()

	header := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(" %d previous run(s) - tangsible revisit ", len(entries)))
	header.SetTextStyle(uikit.BarStyle)
	footer := tview.NewTextView().SetDynamicColors(true).
		SetText(" enter: open  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	footer.SetTextStyle(uikit.BarStyle)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)

	// hadUnreachable resolves classifyExit's own exit-4 ambiguity once per
	// entry, up front - not per row-render, which would re-scan a
	// possibly-large .jsonl on every single cursor move (rebuildRows runs
	// on every SetChangedFunc). Only entries whose exit code is exactly 4
	// pay for this I/O at all; every other entry's map lookup below is
	// just a nil, matching classifyExit's own "unknown" default.
	hadUnreachable := make(map[string]*bool, len(entries))
	for _, e := range entries {
		if e.ExitCode == tqmUnreachableHostsBit {
			hadUnreachable[e.RunID] = resolveHadUnreachable(e)
		}
	}

	// labelWidth is the widest status label across the whole list, computed
	// once up front (entries is fully known already, unlike the live tree's
	// incrementally-growing rows) so every row's own label column lines up -
	// see RevisitRowText's own doc comment.
	labelWidth := 0
	for _, e := range entries {
		if w := len([]rune(RevisitStatusLabel(e.ExitCode, hadUnreachable[e.RunID]))); w > labelWidth {
			labelWidth = w
		}
	}

	var selected RevisitEntry
	chosen := false

	selectedIdx := 0
	for i, e := range entries {
		if e.RunID == initialRunID {
			selectedIdx = i
			break
		}
	}
	rebuilding := false
	var rebuildRows func()
	rebuildRows = func() {
		rebuilding = true
		defer func() { rebuilding = false }()
		list.Clear()
		for i, e := range entries {
			e := e
			list.AddItem(RevisitRowText(e, hadUnreachable[e.RunID], labelWidth, i == selectedIdx), func() {
				selected = e
				chosen = true
				app.Stop()
			})
		}
		list.SetCurrentItem(selectedIdx)
	}
	list.SetChangedFunc(func(index int) {
		if rebuilding {
			return
		}
		selectedIdx = index
		rebuildRows()
	})
	rebuildRows()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyCtrlC, event.Key() == tcell.KeyRune && event.Rune() == 'q':
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
	return selected, chosen
}

// OpenRevisitEntry replays e's own saved .jsonl (runlog.go) into a fresh
// PlaybookState, then shows it via a real NewLiveTUI - already frozen
// (processDone pre-true, exitCode/HadUnreachable already exactly what they
// were for the original run), with revisitReturn wired so Esc at the bare
// tree level closes this Application and returns control to
// RunRevisitVerb's own loop, unless a real rerun (Phase 3, below) is
// confirmed first - once that happens (submitRerun/SetInputCapture, tui.go)
// this session is no longer "old data" in any sense: chrome/clock revert to
// normal and Esc stops meaning "back to the list." q/Ctrl-C's own meaning is
// unchanged either way, before or after a rerun: it closes THIS Application
// once processDone (same as tui.go's own SetInputCapture always does),
// which - here, unlike main.go's own top-level session - still just returns
// control to RunRevisitVerb's own loop, showing the list again rather than
// exiting the program outright. Only q/Ctrl-C *at the list itself* does
// that (RunRevisitListTUI). Deliberate for now, not yet settled - see
// design-docs/Revisit.md's own open note on this.
//
// requestRerun is a real runner.NewRequestRerun (generation.go) - the same
// mechanism main.go's own run/rerun/role session uses, reused rather than
// duplicated. A role-originated entry gets a freshly generated stub
// (StartRoleSession) up front, reused for every rerun within this one
// session exactly as any other role session's own stub is - see the
// "playbook" local's own doc comment below for why this is built
// unconditionally, and what it does/doesn't fix about the historical
// drill-down's own source lookup.
func OpenRevisitEntry(e RevisitEntry, newLiveTUI NewLiveTUIFunc) {
	jsonlPath, _ := config.RunLogPaths(config.TangsibleStatePath, e.RunID)
	f, err := os.Open(jsonlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't open saved run data: %v\n", err)
		return
	}

	state := &pb.PlaybookState{}
	for item := range runner.ScanEvents(f, nil) {
		if item.IsEvent {
			state.Apply(item.Ev)
		}
	}
	f.Close()

	// playbook is what a rerun (Phase 3, below) would actually spawn -
	// e.Playbook itself for a plain playbook entry, or a freshly generated
	// stub for a role entry, exactly as "tangsible role"/"tangsible rerun
	// <role>" already do at their own session's start (StartRoleSession) -
	// a role session's stub is only ever reused for reruns *within* one
	// process's lifetime, never persisted, so there's no way to reopen the
	// *original* one here regardless. Built unconditionally (even if the
	// user never actually presses 'r') - cheap (a single small file
	// write, not an ansible invocation) and consistent with how eagerly
	// every other role session already does this.
	//
	// The replayed events' own task.Path values, in contrast, still point
	// at that original, already-deleted stub - a fresh one's path can't
	// retroactively fix that lookup, so sourceIndex built from THIS stub
	// only ever benefits a *rerun's* own fresh tasks, not the historical
	// ones already on screen. The historical drill-down's own "no TASK:
	// section for a role entry" gap (design-docs/Revisit.md's own "Open
	// questions") is accepted, unchanged by any of this.
	playbook := e.Playbook
	displayName := filepath.Base(e.Playbook)
	var cleanup func()
	if e.Role != "" {
		playbook, cleanup = role.StartRoleSession(e.Role)
		displayName = e.Role
	}
	if cleanup != nil {
		defer cleanup()
	}
	sourceIndex, knownTags := source.BuildTaskSourceIndex(playbook)

	settings := config.ReadSettingsConfig(config.TangsibleConfigPath)
	invArgs := config.ParsePassthroughArgs(config.HistoryStringToArgs(e.Args))

	var procH runner.ProcHandle
	var processDone, quitting atomic.Bool
	var exitCode atomic.Int32
	processDone.Store(true)
	exitCode.Store(int32(e.ExitCode))

	var progH atomic.Pointer[runner.ProgressTracker]
	progH.Store(runner.NewProgressTracker(nil)) // nothing to preview for a run
	// that already happened - Position() reporting (0,0) is exactly what
	// makes the frozen top bar's fill snap straight to 100%, same as any
	// other frozen session. Rebuilt for real (BuildProgressSkeleton) by
	// runner.NewRequestRerun below, the moment a real rerun actually starts -
	// same as any other session.

	var outcomesMu sync.Mutex
	var outcomes []runner.GenerationOutcome // one appended per rerun triggered
	// from this entry's own session, if any - printed once this
	// session's own app.Run() returns, below. Unlike main.go's own
	// top-level accumulation (kept for the whole process's lifetime),
	// this is scoped to just one entry-viewing session: the terminal is
	// genuinely back in normal mode between this Application's Run()
	// returning and RunRevisitVerb's own next list Application starting
	// (tview's own Screen.Fini(), same as between any two sequential
	// Application lifetimes), so printing here is exactly as safe as
	// main.go's own equivalent, just scoped one level down.

	var app *tview.Application
	var applyLive func(pb.RawEvent)
	apply := func(item runner.StreamItem) {
		if item.IsEvent && !quitting.Load() {
			applyLive(item.Ev)
		}
	}
	recordOutcome := func(o runner.GenerationOutcome) {
		outcomesMu.Lock()
		outcomes = append(outcomes, o)
		outcomesMu.Unlock()
	}
	requestRerun := runner.NewRequestRerun(playbook, e.Role, invArgs.Rest, state, &procH, &processDone, &exitCode, &progH, apply, recordOutcome)

	revisitReturn := func() {
		quitting.Store(true) // before Stop() - same race note as main.go's
		// own top-level quitting.Store(true) after app.Run() returns.
		app.Stop()
	}

	app, applyLive = newLiveTUI(state, displayName, e.Role != "", &procH, &processDone, &quitting, &exitCode,
		sourceIndex, knownTags, config.DefaultTreeExpanded(settings), config.TwoPaneLayoutEnabled(settings), config.ColorEnabledByUser(settings),
		invArgs.Tags, invArgs.SkipTags, invArgs.Hosts, false, requestRerun, invArgs.Rest, &progH, revisitReturn,
		e.Playbook, e.Role)

	runErr := app.Run()
	quitting.Store(true) // defensive: same reasoning as main.go's own
	// post-Run() store - stop the streamer/heartbeat/resize-watcher
	// goroutines if Run() ever returns for a reason other than our own
	// Stop() calls (revisitReturn, or plain q/Ctrl-C once processDone).
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", runErr)
	}

	// Same suppression main.go's own top-level printing applies: a 99
	// exit means the user asked (via q/Ctrl-C) to interrupt a rerun
	// triggered from this session, not a failure - nothing useful to
	// report about that. Unlike main.go, a genuine failure here doesn't
	// change this program's own exit status - a failed rerun while
	// browsing history shouldn't take the whole "revisit" session down;
	// the failure is already visible in the tree itself, and the user can
	// navigate back to the list normally.
	outcomesMu.Lock()
	all := outcomes
	outcomesMu.Unlock()
	for _, o := range all {
		if o.ExitCode != runner.AnsibleUserInterruptedExitCode {
			for _, l := range o.ChildStderr {
				fmt.Fprintln(os.Stderr, "[ansible-playbook stderr]", l)
			}
		}
	}
}
