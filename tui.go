package main

import (
	"fmt"
	"os"
	"strings"
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
//
// width is the list's current available width (see rebuild), used to
// right-align each TASK row's counts segment (see taskLabel).
func flattenRows(state *playbookState, expanded map[*taskNode]bool, width int) []row {
	var rows []row
	for _, play := range state.Plays {
		rows = append(rows, row{text: fmt.Sprintf("PLAY: %s", play.Name), id: play})
		for _, task := range play.Tasks {
			t := task
			rows = append(rows, row{
				text:     taskLabel(t, width),
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
	list.SetWrapAround(false)

	expanded := map[*taskNode]bool{}
	var currentRows []row
	var currentID any
	var rebuilding bool
	following := true    // auto-follow the newest row until the user navigates away
	var jumpingToEnd bool // true only while our own End/G handler drives SetCurrentItem

	var rebuild func()
	rebuild = func() {
		rebuilding = true
		defer func() { rebuilding = false }()

		_, _, width, _ := list.GetInnerRect()
		// Belt-and-suspenders only: tview.Box's own zero-value width is 15
		// (never 0), and QueueUpdateDraw can't run this closure before
		// Run()'s first real-size draw pass anyway - taskLabel is also
		// panic-safe for any width - but clamp defensively in case that
		// ordering assumption ever changes.
		if width < 20 {
			width = 20
		}
		currentRows = flattenRows(state, expanded, width)
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
		if following {
			// Keep pinned to the newest row; currentID is intentionally
			// left stale here - it's never read while following, and gets
			// refreshed the instant a genuine navigation disengages it.
			list.SetCurrentItem(list.GetItemCount() - 1)
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
		if !jumpingToEnd {
			following = false
		}
	})

	state.OnPlayAdded = func(*playNode) { rebuild() }
	state.OnTaskAdded = func(*playNode, *taskNode) { rebuild() }
	state.OnHostRecorded = func(*taskNode, string) { rebuild() }

	topBar := tview.NewTextView().SetText(fmt.Sprintf(" %s ", playbookName))
	topBar.SetTextStyle(reverseStyle)

	bottomBar := tview.NewTextView().SetText(" ↑/↓ navigate  home/end/G: top/bottom  enter: expand/collapse  q: quit ")
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
		if isQuit {
			if processDone.Load() {
				quitting.Store(true) // before Stop() - see main.go's race note
				app.Stop()
			} else {
				_ = proc.Signal(os.Interrupt) // best-effort; child may race-exit
			}
			return nil
		}

		isJumpToEnd := event.Key() == tcell.KeyEnd ||
			(event.Key() == tcell.KeyRune && event.Rune() == 'G')
		if isJumpToEnd {
			following = true
			jumpingToEnd = true
			if list.GetItemCount() > 0 {
				list.SetCurrentItem(list.GetItemCount() - 1)
			}
			jumpingToEnd = false
			return nil
		}

		return event
	})

	applyLive = func(ev rawEvent) {
		app.QueueUpdateDraw(func() {
			state.Apply(ev)
		})
	}

	return app, applyLive
}

const taskIndent = "  "

// colorTag returns the tview style-tag foreground color name for o, per
// TUI.md's OK/Changed/Skipped/Failed = green/yellow/cyan/red convention
// (using tcell's/W3C's "teal" as the closest named match for cyan).
func colorTag(o outcome) string {
	switch o {
	case outcomeOK:
		return "green"
	case outcomeChanged:
		return "yellow"
	case outcomeSkipped:
		return "teal"
	case outcomeFailed:
		return "red"
	default:
		return "-"
	}
}

// taskLabel builds one TASK row's full text, including its leading indent.
// Per TUI.md, the "OK: 01, Chgd: 01, Skip: 01, Fail: 00" counts segment is
// right-aligned to the far right of avail (the row's full available width,
// e.g. straight from the list's GetInnerRect); the title is truncated with
// an ellipsis if title+counts wouldn't otherwise fit, down to an empty
// title in the extreme case where even that doesn't help.
//
// avail reflects the terminal size as of the last rebuild (see
// flattenRows) - a bare terminal resize with no new incoming event won't
// re-flow existing rows until the next event triggers one. Accepted
// limitation, not a bug.
//
// Uses rune count as a proxy for on-screen width when truncating, same as
// the previous %-40s formatting did - undercounts wide (e.g. CJK)
// characters in a task name, a pre-existing simplification, not a new gap.
func taskLabel(task *taskNode, avail int) string {
	ok, changed, skipped, failed := task.counts()
	counts := fmt.Sprintf(
		"[%s]OK: %02d[-], [%s]Chgd: %02d[-], [%s]Skip: %02d[-], [%s]Fail: %02d[-]",
		colorTag(outcomeOK), ok, colorTag(outcomeChanged), changed,
		colorTag(outcomeSkipped), skipped, colorTag(outcomeFailed), failed,
	)
	countsWidth := tview.TaggedStringWidth(counts)

	availContent := avail - len(taskIndent)
	if availContent < 0 {
		availContent = 0
	}

	const gap = 1 // minimum space between title and counts
	targetTitleWidth := availContent - gap - countsWidth
	if targetTitleWidth < 0 {
		targetTitleWidth = 0
	}

	rawTitle := "TASK: " + task.Name
	titleRunes := []rune(rawTitle)
	var title string
	switch {
	case len(titleRunes) <= targetTitleWidth:
		title = rawTitle
	case targetTitleWidth > 1:
		title = string(titleRunes[:targetTitleWidth-1]) + "…"
	default:
		title = ""
	}
	// Escape only after truncating the raw text, so slicing can never cut
	// into an escape sequence Escape() would otherwise have produced.
	title = tview.Escape(title)

	padding := availContent - tview.TaggedStringWidth(title) - countsWidth
	if padding < 1 {
		padding = 1
	}

	return taskIndent + title + strings.Repeat(" ", padding) + counts
}

// hostLabel builds one host row's text, colored uniformly by its single
// outcome. No width-based truncation/alignment applies here - that rule is
// TASK-row-specific per TUI.md.
func hostLabel(task *taskNode, host string) string {
	o := task.Hosts[host]
	return fmt.Sprintf("[%s]%s: %s[-]", colorTag(o), tview.Escape(host), o)
}
