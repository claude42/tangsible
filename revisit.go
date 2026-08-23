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

// Implements the "revisit" verb (design-docs/Revisit.md, Phase 2): browse
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
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// runRevisitVerb is "tangsible revisit [<playbook>] [ansible-playbook
// args...]"'s own entry point. Loops between the list and a selected
// entry's detail view for as long as the user keeps picking entries -
// re-pruning and re-resolving the list fresh every time control returns to
// it, so a re-run started and finished during a later phase's detail
// session (once Phase 3 wires that up) would show up in it, and so files
// deleted externally between selections are noticed too.
func runRevisitVerb(args []string) int {
	shownAnything := false
	for {
		cfg, err := pruneMissingRunLogs(tangsibleStatePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tangsible: couldn't update invocation history in %s: %v\n", tangsibleStatePath, err)
		}
		entries := resolveRevisitEntries(args, cfg)
		if len(entries) == 0 {
			if !shownAnything {
				fmt.Fprintln(os.Stderr, "tangsible: no matching previous runs to revisit")
				return 1
			}
			return 0
		}
		shownAnything = true

		selected, ok := runRevisitListTUI(entries)
		if !ok {
			return 0
		}
		openRevisitEntry(selected)
	}
}

// revisitCommandText reconstructs "how tangsible was called" for one
// entry, e.g. "tangsible run site.yml -l zen" or "tangsible role postfix" -
// the playbook/role is always included (unlike the shorthand in the design
// conversation this implements), since an unfiltered list can otherwise mix
// entries for different targets with no way to tell them apart.
func revisitCommandText(e revisitEntry) string {
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

// formatRevisitTime renders invocationRecord.Time (RFC3339 UTC, as
// appendInvocation stamps it) in the local zone, readably - falling back to
// the raw stored string on a parse failure (shouldn't happen for anything
// this program itself ever wrote, but not trusted blindly, same caveat
// applied to every other stored/event-derived field elsewhere).
func formatRevisitTime(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// revisitRowText renders one list row - a leading status color (green:
// clean success; gray: user-interrupted, same ansibleUserInterruptedExitCode
// main.go itself treats as "not a failure"; red: anything else) on the
// timestamp, then revisitCommandText. Selected styling matches host.go's
// own hostRowText convention (pureBlack on lightgray) rather than
// reinventing a second one.
func revisitRowText(e revisitEntry, selected bool) string {
	ts := formatRevisitTime(e.Time)
	cmd := revisitCommandText(e)
	if selected {
		return fmt.Sprintf("[%s:lightgray:b]%s - %s[-:-:-]", pureBlack, tview.Escape(ts), tview.Escape(cmd))
	}
	statusColor := "green"
	switch {
	case e.ExitCode == ansibleUserInterruptedExitCode:
		statusColor = "gray"
	case e.ExitCode != 0:
		statusColor = "red"
	}
	return fmt.Sprintf("[%s]%s[-] - %s", statusColor, tview.Escape(ts), tview.Escape(cmd))
}

// runRevisitListTUI shows entries (already filtered/sorted newest-first by
// resolveRevisitEntries) in a plain, flat, single-page list - no tree/
// expand-collapse needed here, per your own call that this should look
// like "tangsible hosts"'s own list. Blocks until the user either picks one
// (Enter - returns it with ok=true) or quits (q/Ctrl-C - ok=false).
//
// The cursor-highlighting/rebuild-on-change structure (selectedIdx,
// rebuilding, rebuildRows) is a direct copy of runHostsListTUI's own
// (host.go) - treeList has no built-in "current row" look of its own (see
// that function's own doc comment for why), so every list built on it needs
// this same small amount of bookkeeping.
func runRevisitListTUI(entries []revisitEntry) (revisitEntry, bool) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := newTreeList()

	header := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(" %d previous run(s) - tangsible revisit ", len(entries)))
	header.SetTextStyle(barStyle)
	footer := tview.NewTextView().SetDynamicColors(true).
		SetText(" enter: open  q: quit  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom ")
	footer.SetTextStyle(barStyle)

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(footer, 1, 0, false)

	var selected revisitEntry
	chosen := false

	selectedIdx := 0
	rebuilding := false
	var rebuildRows func()
	rebuildRows = func() {
		rebuilding = true
		defer func() { rebuilding = false }()
		list.Clear()
		for i, e := range entries {
			e := e
			list.AddItem(revisitRowText(e, i == selectedIdx), func() {
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

// openRevisitEntry replays e's own saved .jsonl (runlog.go) into a fresh
// playbookState, then shows it via a real NewLiveTUI - already frozen
// (processDone pre-true, exitCode/HadUnreachable already exactly what they
// were for the original run), with revisitReturn wired so Esc at the bare
// tree level closes this Application and returns control to
// runRevisitVerb's own loop. Blocks until that happens (or the user quits
// outright, q/Ctrl-C, which closes the whole program - see tui.go's
// SetInputCapture, unchanged by any of this).
//
// requestRerun is passed as nil - Phase 2 doesn't wire up re-run-from-
// revisit yet (design-docs/Revisit.md's own phasing); NewLiveTUI's own 'r'
// key handler already no-ops when requestRerun is nil, and
// currentMainBottomBarText already drops the "r: re-run" hint to match.
func openRevisitEntry(e revisitEntry) {
	jsonlPath, _ := runLogPaths(tangsibleStatePath, e.RunID)
	f, err := os.Open(jsonlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't open saved run data: %v\n", err)
		return
	}

	state := &playbookState{}
	for item := range scanEvents(f, nil) {
		if item.isEvent {
			state.Apply(item.ev)
		}
	}
	f.Close()

	// A role-originated entry's own task.Path points at the role session's
	// generated stub playbook, deleted once that session ended - there's
	// no source left to index, and no path to index it from either (a
	// role name isn't a file path). Left as taskSourceIndex's own zero
	// value (nil map): source.go's existing "a miss just means no TASK:
	// section, never an error" convention already covers this gracefully,
	// same as any other lookup miss. Accepted gap, not chased further -
	// design-docs/Revisit.md's own "Open questions."
	var sourceIndex taskSourceIndex
	displayName := filepath.Base(e.Playbook)
	if e.Role != "" {
		displayName = e.Role
	} else {
		sourceIndex = buildTaskSourceIndex(e.Playbook)
	}

	settings := readSettingsConfig(tangsibleConfigPath)
	invArgs := parsePassthroughArgs(historyStringToArgs(e.Args))

	var procH procHandle
	var processDone, quitting atomic.Bool
	var exitCode atomic.Int32
	processDone.Store(true)
	exitCode.Store(int32(e.ExitCode))

	var progH atomic.Pointer[progressTracker]
	progH.Store(newProgressTracker(nil)) // nothing to preview for a run
	// that already happened - Position() reporting (0,0) is exactly what
	// makes the frozen top bar's fill snap straight to 100%, same as any
	// other frozen session.

	var app *tview.Application
	revisitReturn := func() {
		quitting.Store(true) // before Stop() - same race note as main.go's
		// own top-level quitting.Store(true) after app.Run() returns.
		app.Stop()
	}

	app, _ = NewLiveTUI(state, displayName, e.Role != "", &procH, &processDone, &quitting, &exitCode,
		sourceIndex, defaultTreeExpanded(settings), twoPaneLayoutEnabled(settings), colorEnabledByUser(settings),
		invArgs.Tags, invArgs.SkipTags, invArgs.Hosts, false, nil, invArgs.Rest, &progH, revisitReturn)

	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
