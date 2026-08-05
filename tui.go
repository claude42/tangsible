package main

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var reverseStyle = tcell.StyleDefault.Reverse(true)

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

type hostRowID struct {
	task *taskNode
	host string
}

// flattenRows walks state's play/task/host tree into an ordered row list,
// respecting which tasks are currently expanded. Rebuilt fresh on every
// event - cheap at this project's target scale (~10 hosts, Purpose.md), and
// avoids needing to incrementally patch a tree structure by hand.
func flattenRows(state *playbookState, expanded map[*taskNode]bool) []row {
	var rows []row
	for _, play := range state.Plays {
		rows = append(rows, row{text: fmt.Sprintf("PLAY: %s", play.Name), id: play})
		for _, task := range play.Tasks {
			t := task
			rows = append(rows, row{
				text:     "  " + taskLabel(t),
				id:       t,
				selected: func() { expanded[t] = !expanded[t] },
			})
			if expanded[t] {
				for _, host := range t.HostOrder {
					rows = append(rows, row{text: "    " + hostLabel(t, host), id: hostRowID{t, host}})
				}
			}
		}
	}
	return rows
}

// NewLiveTUI builds an initially-empty list UI and wires it to state's
// hooks so it grows as events arrive. It does not block — the caller must
// call app.Run() and feed events through applyLive.
//
// proc is ansible-playbook's process, used so Ctrl-C/q can forward SIGINT
// to it while it's still running (tcell's raw mode disables the OS's own
// Ctrl-C-to-SIGINT delivery, so without this the child would stop
// receiving the interrupt it used to get for free — see Purpose.md's
// Ctrl-C decision). processDone/quitting are shared with the caller:
// this function only reads processDone and only writes quitting.
func NewLiveTUI(state *playbookState, playbookName string, proc *os.Process, processDone, quitting *atomic.Bool) (app *tview.Application, applyLive func(rawEvent)) {
	list := tview.NewList().ShowSecondaryText(false)

	expanded := map[*taskNode]bool{}
	var currentRows []row
	var currentID any
	var rebuilding bool

	var rebuild func()
	rebuild = func() {
		rebuilding = true
		defer func() { rebuilding = false }()

		currentRows = flattenRows(state, expanded)
		list.Clear()
		for _, r := range currentRows {
			r := r
			var selected func()
			if r.selected != nil {
				selected = func() {
					r.selected()
					rebuild()
				}
			}
			list.AddItem(r.text, "", 0, selected)
		}
		if list.GetItemCount() == 0 {
			return
		}
		for i, r := range currentRows {
			if r.id == currentID {
				list.SetCurrentItem(i)
				return
			}
		}
		list.SetCurrentItem(0)
	}

	list.SetChangedFunc(func(index int, _ string, _ string, _ rune) {
		if rebuilding {
			// List.Clear()+AddItem() fires spurious "changed" events while
			// rebuild() is repopulating (e.g. as soon as the first item
			// lands back in the now-empty list) - ignore those, rebuild()
			// restores the real selection itself once done.
			return
		}
		if index >= 0 && index < len(currentRows) {
			currentID = currentRows[index].id
		}
	})

	state.OnPlayAdded = func(*playNode) { rebuild() }
	state.OnTaskAdded = func(*playNode, *taskNode) { rebuild() }
	state.OnHostRecorded = func(*taskNode, string) { rebuild() }

	topBar := tview.NewTextView().SetText(fmt.Sprintf(" %s ", playbookName))
	topBar.SetTextStyle(reverseStyle)

	bottomBar := tview.NewTextView().SetText(" ↑/↓ navigate  enter: expand/collapse  q: quit ")
	bottomBar.SetTextStyle(reverseStyle)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(bottomBar, 1, 0, false)

	app = tview.NewApplication().SetRoot(flex, true)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		isQuit := event.Key() == tcell.KeyCtrlC ||
			(event.Key() == tcell.KeyRune && event.Rune() == 'q')
		if !isQuit {
			return event
		}
		if processDone.Load() {
			quitting.Store(true) // before Stop() - see main.go's race note
			app.Stop()
		} else {
			_ = proc.Signal(os.Interrupt) // best-effort; child may race-exit
		}
		return nil
	})

	applyLive = func(ev rawEvent) {
		app.QueueUpdateDraw(func() {
			state.Apply(ev)
		})
	}

	return app, applyLive
}

func taskLabel(task *taskNode) string {
	ok, changed, skipped, failed := task.counts()
	return fmt.Sprintf("TASK: %-40s OK: %d, Chgd: %d, Skip: %d, Fail: %d",
		task.Name, ok, changed, skipped, failed)
}

func hostLabel(task *taskNode, host string) string {
	return fmt.Sprintf("%s: %s", host, task.Hosts[host])
}
