package main

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var reverseStyle = tcell.StyleDefault.Reverse(true)

// NewLiveTUI builds an initially-empty tree UI and wires it to state's
// hooks so it grows incrementally as events arrive, instead of ever being
// rebuilt from scratch (which would reset expand/collapse state and the
// current selection on every event). It does not block — the caller must
// call app.Run() and feed events through applyLive.
//
// proc is ansible-playbook's process, used so Ctrl-C/q can forward SIGINT
// to it while it's still running (tcell's raw mode disables the OS's own
// Ctrl-C-to-SIGINT delivery, so without this the child would stop
// receiving the interrupt it used to get for free — see Purpose.md's
// Ctrl-C decision). processDone/quitting are shared with the caller:
// this function only reads processDone and only writes quitting.
func NewLiveTUI(state *playbookState, playbookName string, proc *os.Process, processDone, quitting *atomic.Bool) (app *tview.Application, applyLive func(rawEvent)) {
	root := tview.NewTreeNode("root").SetSelectable(false)
	tree := tview.NewTreeView().
		SetRoot(root).
		SetTopLevel(1) // hide the synthetic root; show play nodes at the top
	tree.SetCurrentNode(root)

	// Bookkeeping so the hooks below know which existing tview.TreeNode to
	// attach a new child to or update in place.
	playNodes := map[*playNode]*tview.TreeNode{}
	taskNodes := map[*taskNode]*tview.TreeNode{}
	hostLeaves := map[*taskNode]map[string]*tview.TreeNode{}

	state.OnPlayAdded = func(play *playNode) {
		node := tview.NewTreeNode(fmt.Sprintf("PLAY: %s", play.Name)).SetSelectable(true)
		firstPlay := len(root.GetChildren()) == 0
		playNodes[play] = node
		root.AddChild(node)
		if firstPlay {
			// Bootstrap a valid selection now that there's something to
			// select; only ever fires once, for the very first play.
			tree.SetCurrentNode(node)
		}
	}

	state.OnTaskAdded = func(play *playNode, task *taskNode) {
		node := tview.NewTreeNode(taskLabel(task)).
			SetSelectable(true).
			SetExpanded(false)
		taskNodes[task] = node
		hostLeaves[task] = map[string]*tview.TreeNode{}
		playNodes[play].AddChild(node)
	}

	state.OnHostRecorded = func(task *taskNode, host string) {
		taskNodes[task].SetText(taskLabel(task))
		if leaf, ok := hostLeaves[task][host]; ok {
			leaf.SetText(hostLabel(task, host))
			return
		}
		leaf := tview.NewTreeNode(hostLabel(task, host)).SetSelectable(true)
		hostLeaves[task][host] = leaf
		taskNodes[task].AddChild(leaf)
	}

	// Blindly toggle expand state on Enter. A no-op on childless nodes, so
	// no need to special-case which kind of node is selected.
	tree.SetSelectedFunc(func(node *tview.TreeNode) {
		node.SetExpanded(!node.IsExpanded())
	})

	topBar := tview.NewTextView().SetText(fmt.Sprintf(" %s ", playbookName))
	topBar.SetTextStyle(reverseStyle)

	bottomBar := tview.NewTextView().SetText(" ↑/↓ navigate  enter: expand/collapse  q: quit ")
	bottomBar.SetTextStyle(reverseStyle)

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(topBar, 1, 0, false).
		AddItem(tree, 0, 1, true).
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
