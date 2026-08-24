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

package main

import "fmt"

// row is one flattened, currently-visible line in the list: a play, a task,
// or (if its task is expanded) a host. selected is nil for play/host rows;
// for task rows it toggles that task's expand state. id identifies the row
// across rebuilds (a *playNode, *taskNode, or hostRowID), used to restore
// the selection to the same logical row after the list is repopulated.
type row struct {
	text     string
	selected func()
	id       any
}

// nextInteractiveRow finds the next row index, starting just past from
// and moving by delta (+1/-1), whose own selected callback is non-nil -
// skipping purely decorative rows (statusRowID/statusDividerRowID, and
// design-docs/Recap.md's own recapHeadingRowID rows) entirely, rather
// than stopping the cursor on each one in turn on the way past. -1 (no
// wraparound, matching this app's own convention everywhere else) if the
// search runs off either end without finding one. Backs SetInputCapture's
// own Up/Down/j/k handling for the main tree - a real report: without
// this, moving from the tree's last row down onto the recap section's
// first host line took seven silent keypresses, since none of the
// status/heading rows in between has a visible selected state to move
// through.
func nextInteractiveRow(rows []row, from, delta int) int {
	for i := from + delta; i >= 0 && i < len(rows); i += delta {
		if rows[i].selected != nil {
			return i
		}
	}
	return -1
}

type hostRowID struct {
	task *taskNode
	host string
}

// statusRowID/statusDividerRowID identify the trailing status rows rebuild()
// appends once the run has finished (see statusRowText) - given explicit
// non-nil ids rather than leaving the divider row's id as the implicit zero
// value, so nothing relies on "no other row's id is ever nil" holding by
// coincidence.
type statusRowID struct{}

type statusDividerRowID struct{}

// statusRowText returns the inline status line to append below the last
// task row once the run has finished - every case gets one, including a
// genuine failure (red "Playbook failed"). This used to fall through to
// "" for anything other than success/benign-unreachable/user-interrupted,
// on the reasoning that a genuine failure is already visible from the red
// host rows themselves - but a real run (one host, one task, that task
// failed, nothing else executed) showed no closing line at all, which
// reads as "did this even finish?" rather than a clear verdict; every
// other terminal case already gets an explicit line, so failure should
// too, red like the row(s) that caused it. hadUnreachable mirrors
// main.go's benignHostUnreachable check: exit 4 (ansible-core's own
// overloaded HOST_UNREACHABLE/PARSER_ERROR value) doesn't make main.go treat
// this as a hard tool error once Tangsible has independently observed a real
// unreachable host this run - but that's a distinct decision from what this
// function shows. A run where the only reachable-or-not evidence is "some
// host(s) never responded" is not the same as one where everything actually
// ran clean, and rendering both as identical green "completed successfully"
// text was actively misleading (confirmed live: a single-host playbook whose
// one host is unreachable on its very first task exits 4 with
// hadUnreachable=true and nothing else ever having run) - so this case gets
// its own yellow, distinctly-not-"successfully" message instead.
func statusRowText(code int, hadUnreachable bool) string {
	benignHostUnreachable := code == 4 && hadUnreachable
	switch {
	case code == 0:
		return "[green]Playbook completed successfully[-]"
	case benignHostUnreachable:
		return "[yellow]Playbook completed - one or more hosts were unreachable[-]"
	case code == ansibleUserInterruptedExitCode:
		return "[red]Playbook stopped, press q again to quit tangsible.[-]"
	default:
		return fmt.Sprintf("[red]Playbook failed (exit code %d)[-]", code)
	}
}

// genuineFailure reports whether code represents an actual failure - not
// success, not the benign "some host(s) were unreachable" case (see
// main.go's benignHostUnreachable), and not a user-requested interrupt.
// Structurally the same condition as statusRowText's own default case
// above, pulled out separately so rebuild's one-time post-freeze cursor
// placement (see below) can't silently drift out of agreement with it
// about what counts as "actually failed."
func genuineFailure(code int, hadUnreachable bool) bool {
	benignHostUnreachable := code == 4 && hadUnreachable
	return code != 0 && !benignHostUnreachable && code != ansibleUserInterruptedExitCode
}

// lastFailedTaskAndHost finds the most recent task (in tree order) that
// recorded a Failed or Unreachable host, and that host's own name - the
// first such host recorded on that task, per HostOrder - or (nil, "") if
// none is found (a genuine failure for some other reason, e.g. an
// unparsable exit code, with no Failed/Unreachable host ever recorded).
// Used once, right as a run freezes into a genuine failure (see rebuild),
// to put the cursor exactly where a user drilling into "what failed"
// would want it, without needing to navigate there themselves.
func lastFailedTaskAndHost(state *playbookState) (*taskNode, string) {
	for pi := len(state.Plays) - 1; pi >= 0; pi-- {
		tasks := state.Plays[pi].Tasks
		for ti := len(tasks) - 1; ti >= 0; ti-- {
			t := tasks[ti]
			for _, h := range t.HostOrder {
				if o := t.Hosts[h]; o == outcomeFailed || o == outcomeUnreachable {
					return t, h
				}
			}
		}
	}
	return nil, ""
}

// flattenRows walks state's play/task/host tree into an ordered row list,
// respecting which tasks are currently expanded and (per filter) currently
// visible at all. Rebuilt fresh on every event - cheap at this project's
// target scale (~10 hosts, Purpose.md), and avoids needing to incrementally
// patch a tree structure by hand.
//
// width is the list's current available width (see rebuild), passed
// through to taskLabel for its own no-hosts-yet fallback (see
// computeHostColumnLayout/taskLabel). activeTask (nil once the run has
// finished) gets a spinner prefix on its row instead of an elapsed-time
// readout - frame is the shared spinner frame for this rebuild pass (see
// spinnerAt), computed once and passed in rather than each row picking
// its own, so every active indicator in the UI ticks in lockstep.
// showOutput is called when a host row is selected (Enter), to display
// that host's full result for that task. sourceIndex is only read by
// taskVisible's filterSearch case, to search a task's own source text.
// useColor is threaded straight through to each row's own taskLabel call
// - see its doc comment (design-docs/Morehosts.md).
func flattenRows(state *playbookState, expanded map[*taskNode]bool, width int, layout hostColumnLayout, allHosts []string, activeTask *taskNode, frame rune, filter filterQuery, sourceIndex taskSourceIndex, showOutput func(task *taskNode, host string), useColor bool) []row {
	var rows []row
	for _, play := range state.Plays {
		var playRows []row
		for _, task := range play.Tasks {
			t := task
			if !taskVisible(t, filter, sourceIndex, t == activeTask) {
				continue
			}
			playRows = append(playRows, row{
				text:     taskLabel(t, allHosts, layout, width, t == activeTask, frame, false, useColor),
				id:       t,
				selected: func() { expanded[t] = !expanded[t] },
			})
			if expanded[t] {
				for _, host := range t.HostOrder {
					h := host
					playRows = append(playRows, row{
						text:     hostLabel(t, h, false),
						id:       hostRowID{t, h},
						selected: func() { showOutput(t, h) },
					})
				}
			}
		}
		if len(playRows) == 0 {
			// Same rule as a play with no executed tasks at all (see
			// playbookState's own doc comment) - a play with no *visible*
			// tasks after filtering doesn't get a row either.
			continue
		}
		rows = append(rows, row{text: playRowText(play, false), id: play})
		rows = append(rows, playRows...)
	}
	return rows
}
