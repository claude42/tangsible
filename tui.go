package main

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var reverseStyle = tcell.StyleDefault.Reverse(true)

// buildTree converts the final playbookState into a tview tree rooted at an
// invisible root node. Task nodes start collapsed so their host children
// are hidden until the user presses Enter (see TUI.md).
func buildTree(s *playbookState) *tview.TreeNode {
	root := tview.NewTreeNode("root").SetSelectable(false)

	for _, play := range s.Plays {
		playNode := tview.NewTreeNode(fmt.Sprintf("PLAY: %s", play.Name)).
			SetSelectable(true)
		root.AddChild(playNode)

		for _, task := range play.Tasks {
			playNode.AddChild(buildTaskNode(task))
		}
	}

	return root
}

func buildTaskNode(task *taskNode) *tview.TreeNode {
	ok, changed, skipped, failed := task.counts()
	label := fmt.Sprintf("TASK: %-40s OK: %d, Chgd: %d, Skip: %d, Fail: %d",
		task.Name, ok, changed, skipped, failed)

	node := tview.NewTreeNode(label).
		SetSelectable(true).
		SetExpanded(false)

	for _, host := range task.HostOrder {
		hostLabel := fmt.Sprintf("%s: %s", host, task.Hosts[host])
		node.AddChild(tview.NewTreeNode(hostLabel).SetSelectable(true))
	}

	return node
}

// RunTUI shows the final playbookState as an interactive tree and blocks
// until the user quits ('q' or Ctrl-C). playbookName is displayed in the
// top bar.
func RunTUI(s *playbookState, playbookName string) error {
	root := buildTree(s)

	tree := tview.NewTreeView().
		SetRoot(root).
		SetTopLevel(1) // hide the synthetic root; show play nodes at the top

	if children := root.GetChildren(); len(children) > 0 {
		tree.SetCurrentNode(children[0])
	} else {
		tree.SetCurrentNode(root)
	}

	// Blindly toggle expand state on Enter. A no-op on childless nodes
	// (host leaves, or a play/task with nothing under it), so no need to
	// special-case which kind of node is selected.
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

	app := tview.NewApplication().SetRoot(flex, true)

	// tcell's raw mode disables OS-level SIGINT-on-Ctrl-C, so it arrives
	// here as a key event instead of a signal - quit explicitly on it.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch {
		case event.Key() == tcell.KeyCtrlC:
			app.Stop()
			return nil
		case event.Key() == tcell.KeyRune && event.Rune() == 'q':
			app.Stop()
			return nil
		}
		return event
	})

	return app.Run()
}
