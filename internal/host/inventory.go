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

// Implements the "inventory" Verb (design-docs/HostVerb.md's "New ideas"):
// a tree view of the whole inventory, groups as nodes and hosts as leaves,
// very similar to `ansible-inventory --graph` - living in this same
// package (not its own) since it reuses host.go's own stub-playbook/ping/
// detail-view machinery directly, no cross-package plumbing needed.
package host

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"code.aw.net/claude/tangsible/internal/config"
	"code.aw.net/claude/tangsible/internal/inventory"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// invNodeKind distinguishes a group node (has children, collapsible) from
// a host leaf (never has children, opens the five-tab detail view
// instead) in the tree buildInventoryTree builds.
type invNodeKind int

const (
	invGroup invNodeKind = iota
	invHost
)

// invNode is one node in the inventory tree - a group (with its own
// children, which may be further groups and/or hosts) or a host leaf.
// parent backs handleLeft's own "collapse the parent, move the cursor
// there" and collapseAll's own cursor-safety walk, both below; expanded is
// only ever read/written for an invGroup node.
type invNode struct {
	kind      invNodeKind
	name      string
	parent    *invNode
	children  []*invNode
	expanded  bool
	hostCount int // unique hosts anywhere beneath this node; invGroup only, see computeHostCounts
}

// buildInventoryTree walks raw (ansible-inventory --list's own decoded
// JSON - the same shape inventory.AnsibleInventoryGroup already reads
// elsewhere) from rootName (always "all" in practice) into an *invNode
// tree mirroring `ansible-inventory --graph`'s own group-then-host
// structure: a group's child groups first, then its own direct hosts, each
// alphabetically. The returned node is rootName itself but is deliberately
// never rendered as its own row (see flattenInventoryRows) - "all" is the
// universal group every host belongs to by definition, and (per the
// existing Groups tab's own identical reasoning, FetchHostGroups) showing
// it as a line of its own would add nothing a user could act on; its
// children become the tree's own (possibly several) top-level rows
// instead, matching design-docs/HostVerb.md's own "the tree shall
// initially only show the top-level groups" wording literally.
//
// A group or host that's a *direct* member of more than one group
// legitimately appears once per parent (confirmed against real --graph
// output, which renders the group DAG as a tree the same way, duplicating
// a child under every parent that claims it) - each occurrence is its own
// distinct *invNode, so cursor/expand state never bleeds between
// occurrences of the "same" group; collectUniqueHostnames (below) is the
// one place that deliberately de-duplicates by name instead, for a
// different reason (sharing one ping result).
//
// ancestors guards against a genuine cycle (a group that's its own
// descendant - a malformed inventory could in principle produce one) by
// tracking the current path, not a global visited set: a global set is
// what inventory.GroupHosts uses for its own "just the flat host set"
// purpose, which is right there (duplicate display doesn't matter) and
// wrong here, where the same group legitimately recurring via a
// *different* ancestor path must still render in full.
func buildInventoryTree(raw map[string]json.RawMessage, rootName string) *invNode {
	groups := make(map[string]inventory.AnsibleInventoryGroup, len(raw))
	for name, data := range raw {
		if name == "_meta" {
			continue
		}
		var g inventory.AnsibleInventoryGroup
		if err := json.Unmarshal(data, &g); err != nil {
			continue
		}
		groups[name] = g
	}

	var walk func(name string, parent *invNode, ancestors map[string]bool) *invNode
	walk = func(name string, parent *invNode, ancestors map[string]bool) *invNode {
		node := &invNode{kind: invGroup, name: name, parent: parent}
		if ancestors[name] {
			return node // cycle - show the node itself once, don't recurse into it again
		}
		g, ok := groups[name]
		if !ok {
			return node
		}
		nextAncestors := make(map[string]bool, len(ancestors)+1)
		for k := range ancestors {
			nextAncestors[k] = true
		}
		nextAncestors[name] = true

		children := append([]string(nil), g.Children...)
		sort.Strings(children)
		for _, c := range children {
			node.children = append(node.children, walk(c, node, nextAncestors))
		}
		hosts := append([]string(nil), g.Hosts...)
		sort.Strings(hosts)
		for _, h := range hosts {
			node.children = append(node.children, &invNode{kind: invHost, name: h, parent: node})
		}
		return node
	}
	return walk(rootName, nil, map[string]bool{})
}

// computeHostCounts fills in every group node's own hostCount: the number
// of *distinct* hostnames anywhere in its subtree, counting a host once no
// matter how many branches beneath this particular group repeat it
// (buildInventoryTree's own doc comment on duplicate occurrences) - the
// group-row label's own "(N)" suffix below, and also how invGroupRowText
// decides whether a group counts as empty (nothing_reported coloring).
// Walked once per node bottom-up; cheap at this app's target scale (~10
// hosts, CLAUDE.md).
func computeHostCounts(root *invNode) {
	var walk func(n *invNode) map[string]bool
	walk = func(n *invNode) map[string]bool {
		if n.kind == invHost {
			return map[string]bool{n.name: true}
		}
		seen := map[string]bool{}
		for _, c := range n.children {
			for h := range walk(c) {
				seen[h] = true
			}
		}
		n.hostCount = len(seen)
		return seen
	}
	walk(root)
}

// applyDefaultExpand sets every group node's own initial expand state,
// recursively - root (always the invisible "all", buildInventoryTree's own
// doc comment) is always expanded unconditionally, since it's never
// rendered as a row in the first place and so has no meaningful "collapsed"
// state of its own; every real, visible descendant defaults to
// descendantsExpanded, which the caller derives from
// general.default_tree_state (config.DefaultTreeExpanded) - the exact same
// key the live run tree already reads for its own first task
// (design-docs/HostVerb.md's own decision to reuse it rather than invent a
// new one). "collapsed" (the project default) leaves only the top-level
// groups visible; "expanded" opens the whole tree at once.
func applyDefaultExpand(root *invNode, descendantsExpanded bool) {
	root.expanded = true
	var apply func(n *invNode)
	apply = func(n *invNode) {
		if n.kind != invGroup {
			return
		}
		n.expanded = descendantsExpanded
		for _, c := range n.children {
			apply(c)
		}
	}
	for _, c := range root.children {
		apply(c)
	}
}

// collectUniqueHostnames returns every distinct hostname leaf under root,
// deduplicated, in display (depth-first) order - design-docs/HostVerb.md's
// own "ping each host once, share the result across every duplicate row":
// a host that's a direct member of several groups gets one *invNode per
// occurrence (buildInventoryTree's own doc comment), but only one
// ansible.builtin.ping is ever worth running for it, keyed by hostname
// exactly the way RunHostsListTUI's own pingKnown/pingOK maps already are.
func collectUniqueHostnames(root *invNode) []string {
	seen := map[string]bool{}
	var names []string
	var walk func(n *invNode)
	walk = func(n *invNode) {
		if n.kind == invHost {
			if !seen[n.name] {
				seen[n.name] = true
				names = append(names, n.name)
			}
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, c := range root.children {
		walk(c)
	}
	return names
}

// countInventoryNodes reports how many distinct group names and distinct
// hostnames root's tree contains - the list header's own "N groups, M
// hosts" count, deliberately counting distinct *names* rather than raw
// node/occurrence counts, since a group or host repeated under several
// parents (buildInventoryTree's own doc comment) is still just the one
// group or host as far as a user reading this count is concerned.
func countInventoryNodes(root *invNode) (groups, hosts int) {
	seenGroups := map[string]bool{}
	seenHosts := map[string]bool{}
	var walk func(n *invNode)
	walk = func(n *invNode) {
		switch n.kind {
		case invGroup:
			seenGroups[n.name] = true
		case invHost:
			seenHosts[n.name] = true
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, c := range root.children {
		walk(c)
	}
	return len(seenGroups), len(seenHosts)
}

// allHostNodesInOrder returns every host leaf *invNode under root, in full
// depth-first display order as if every group were expanded - backs
// navigateHostDetail's own Left/Right (and n/N) host-hop: "the host that
// would come next/previous vertically if the tree were completely
// expanded," literally, walking real tree occurrences rather than a
// deduplicated/alphabetical name list. A host that's a direct member of
// several groups (buildInventoryTree's own doc comment) appears once per
// occurrence here too, each a distinct entry - hopping through this list
// visits every occurrence in true visual order, including repeats,
// instead of jumping by hostname identity (which a first version of this
// did, and which could make Left/Right teleport to a seemingly unrelated
// occurrence of the same name instead of advancing to whichever host
// actually sits next to the current one on screen).
func allHostNodesInOrder(root *invNode) []*invNode {
	var nodes []*invNode
	var walk func(n *invNode)
	walk = func(n *invNode) {
		if n.kind == invHost {
			nodes = append(nodes, n)
			return
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	for _, c := range root.children {
		walk(c)
	}
	return nodes
}

// expandAncestors force-expands every group node on the path from n up to
// (but not including) the invisible root, so n is guaranteed to have a
// visible row the next time the tree is flattened - needed whenever the
// cursor jumps straight to a node that might currently be hidden behind a
// collapsed ancestor (navigateHostDetail's own n/N hop), the same role
// tui.go's own revealExpandedTask/navigateMainTask play for the live run
// tree's own task-hop.
func expandAncestors(n *invNode) {
	for p := n.parent; p != nil; p = p.parent {
		if p.kind == invGroup {
			p.expanded = true
		}
	}
}

// invRow is one flattened, currently-visible row - mirrors uikit.Row's own
// shape (text plus an identifying value) but deliberately local to this
// file rather than reusing that type: uikit.Row's own ID is read generically
// by the live run tree's own cursor-restore logic across several concrete
// ID shapes (*playbook.PlayNode/*playbook.TaskNode/HostRowID), where this
// tree only ever has the one (*invNode), so a plain, typed field reads
// clearer than an any-typed one here.
type invRow struct {
	node *invNode
	text string
}

// invGroupRowText renders one group node's own row - design-docs/HostVerb.md's
// own "re-use the coloring from the run tree" decision: plain white/bold
// (uikit.PlayTitleColor, the exact color a live run's own play rows use),
// no key/value split at all - a group node plays the same "non-leaf,
// nothing of its own to color differently" structural role a play row
// does there. No collapsed/expanded chevron (the live run tree has none
// either, and this view shouldn't invent a convention it lacks). A group
// with no hosts anywhere beneath it (hostCount == 0, computeHostCounts)
// renders in nothing_reported instead - the same "nothing to act on" signal
// that color already carries elsewhere (recap.go, HostRowText's own
// ping-pending dot) - overridden by the selected-row color exactly as
// PlayTitleColor already is below, selection always wins.
func invGroupRowText(n *invNode, depth int, selected bool) string {
	label := fmt.Sprintf("%s (%d)", n.name, n.hostCount)
	indent := strings.Repeat("  ", depth)
	if selected {
		return indent + fmt.Sprintf("[%s:%s:b]%s[-:-:-]", uikit.SelectedRowTextColor, uikit.SelectedRowBackgroundColor, tview.Escape(label))
	}
	color := uikit.PlayTitleColor
	if n.hostCount == 0 {
		color = uikit.NothingReportedColor
	}
	return indent + fmt.Sprintf("[%s::b]%s[-::-]", color, tview.Escape(label))
}

// invHostRowText renders one host leaf - HostRowText's own ping-dot
// rendering verbatim (design-docs/HostVerb.md's own "visualized in a
// similar way as tangsible hosts"), indented to this occurrence's own
// depth in the tree. pingKnown/pingOK are looked up by hostname, so every
// duplicate occurrence of the same host (buildInventoryTree's own doc
// comment) renders the identical, shared dot.
func invHostRowText(n *invNode, depth int, selected, pingKnown, pingOK bool) string {
	return strings.Repeat("  ", depth) + HostRowText(n.name, selected, pingKnown, pingOK)
}

// flattenInventoryRows walks root's tree into an ordered, currently-visible
// row list, respecting each group's own expanded state - root itself is
// never emitted as a row (buildInventoryTree's own doc comment: "all"
// would read as a line with nothing to say), so the walk starts directly
// from its children, each becoming a depth-0 row.
func flattenInventoryRows(root *invNode, pingKnown, pingOK map[string]bool, selected *invNode) []invRow {
	var rows []invRow
	var walk func(n *invNode, depth int)
	walk = func(n *invNode, depth int) {
		isSelected := n == selected
		switch n.kind {
		case invGroup:
			rows = append(rows, invRow{node: n, text: invGroupRowText(n, depth, isSelected)})
			if n.expanded {
				for _, c := range n.children {
					walk(c, depth+1)
				}
			}
		case invHost:
			rows = append(rows, invRow{node: n, text: invHostRowText(n, depth, isSelected, pingKnown[n.name], pingOK[n.name])})
		}
	}
	for _, c := range root.children {
		walk(c, 0)
	}
	return rows
}

// ParseInventoryArgs splits args (everything after the "inventory" Verb)
// into an optional playbook and everything else as passthrough args -
// "tangsible inventory [<playbook>] [-i ...] [-e ...]" (design-docs/
// HostVerb.md), the exact same shape RunHostsVerb already uses for
// "hosts", via config.SplitPlaybookArgs.
func RunInventoryVerb(args []string) int {
	playbookArg, rest, _ := config.SplitPlaybookArgs(args)
	playbook := playbookArg
	if playbook == "" {
		playbook, _ = config.ResolvePlaybook()
	}

	raw, err := inventory.ListInventoryRaw(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't list inventory: %v\n", err)
		return 1
	}

	root := buildInventoryTree(raw, "all")
	if len(root.children) == 0 {
		fmt.Fprintln(os.Stderr, "tangsible: no groups or hosts found in the inventory")
		return 1
	}
	computeHostCounts(root)
	applyDefaultExpand(root, config.DefaultTreeExpanded(config.MergedSettingsConfig()))

	stubPath, err := WriteHostSummaryStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(stubPath)

	pingStubPath, err := WriteHostPingStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tangsible: couldn't create stub playbook: %v\n", err)
		return 1
	}
	defer os.Remove(pingStubPath)

	RunInventoryTreeTUI(root, playbook, rest, stubPath, pingStubPath)
	return 0
}

// RunInventoryTreeTUI implements "tangsible inventory"'s own tree-then-
// detail flow (design-docs/HostVerb.md's "New ideas"): a scrollable
// TreeList of the whole inventory, groups as collapsible nodes and hosts
// as leaves sharing hosts' own ping-dot rendering. Opening a host (Enter,
// or a click) shows the identical five-tab detail view "tangsible hosts"
// already shows, including its own two-pane split layout on a wide enough
// terminal - BuildHostDetailPrimitive, reused completely unchanged, same
// as the hosts verb's own RunHostsListTUI.
//
// Deliberately narrower than the live run tree's own keymap
// (design-docs/HostVerb.md's own "New ideas" Q&A): no n/N (no "next task"
// equivalent among groups) and no in-tab search/copy *for the tree
// itself* - both left out of this first iteration. The *detail view*
// opened from a host leaf keeps its full existing keymap unchanged
// (n/N host-hop, /, y), exactly as it already works when reached from
// "tangsible hosts" - reusing BuildHostDetailPrimitive/hostDetailHelp/
// hostsDetailBar/hostsSplitDetailBar verbatim is what guarantees that,
// not a separate decision made here.
//
// navigateHostDetail's own Left/Right/n/N host-hop moves through
// allHostNodesInOrder's own full depth-first tree order (every occurrence
// of a repeated host counted separately) rather than through
// inventory.FlattenInventoryHosts' own deduplicated/alphabetical list - a
// first version of this used the latter, matching "hosts"' own hop order,
// but that let Left/Right jump to a seemingly unrelated occurrence of the
// same hostname instead of advancing to whichever host actually sits
// next to the current one on screen, reported live as the tree
// "jump[ing] randomly around" once a repeated host's occurrences were
// spread across different branches.
//
// Two-pane behavior is now unified across tangsible run/hosts/inventory
// (design-docs/TwoPanedLayout.md): no split pane exists at all until a
// host is actually opened (Enter or a click), no matter how wide the
// terminal is; once one is, Esc closes it again (split or full-screen
// alike), real keyboard focus moves to the open detail view's own tabs
// (so arrows/Home/End/PgUp/PgDn scroll its content, matching full-screen
// mode), and Left/Right hop to the previous/next host - a plain alias for
// n/N here, same addition as RunHostsListTUI's own. Tree-level Right/
// Left/E/C (expand/collapse) only apply while no detail view is open at
// all; once one is, they're superseded by the detail view's own keymap,
// same as the live run tree has no expand/collapse-all concept while
// viewingOutput either. A mouse click/wheel on the tree pane while split
// still moves real focus back onto list for as long as the user
// navigates there (TreeList's own MouseHandler), reverting to the detail
// view's tabs on the next host selection.
func RunInventoryTreeTUI(root *invNode, playbook string, rest []string, stubPath, pingStubPath string) {
	app := tview.NewApplication()
	app.EnableMouse(true)

	list := uikit.NewTreeList()
	pages := tview.NewPages()

	groupCount, hostCount := countInventoryNodes(root)
	listHeader := tview.NewTextView().SetDynamicColors(true).
		SetText(fmt.Sprintf(" %d groups, %d hosts ", groupCount, hostCount))
	listHeader.SetTextStyle(uikit.BarStyle)
	listFooter := tview.NewTextView().SetDynamicColors(true).
		SetText(inventoryTreeBar)
	listFooter.SetTextStyle(uikit.BarStyle)

	listFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(listHeader, 1, 0, false).
		AddItem(list, 0, 1, true).
		AddItem(listFooter, 1, 0, false)
	pages.AddPage("list", listFlex, true, true)

	splitDivider := tview.NewBox().SetBackgroundColor(tcell.ColorNavy)

	var (
		viewingDetail   bool
		splitMode       bool
		detailTabs      *uikit.TabbedPane
		detailHeader    *tview.TextView
		detailSearch    *uikit.TabSearchBar
		detailPrimitive tview.Primitive
		currentHostNode *invNode
		lastTotalWidth  int
		quitting        atomic.Bool
		selectedNode    *invNode
		rebuilding      bool
		currentNodes    []*invNode
	)
	if len(root.children) > 0 {
		selectedNode = root.children[0]
	}

	detailFooterText := hostsDetailBar
	splitDetailFooterText := hostsSplitDetailBar
	help := uikit.NewHelpOverlay(app, pages)
	detailCache := NewHostDetailCache()

	pingKnown := map[string]bool{}
	pingOK := map[string]bool{}

	var showDetail func(n *invNode)
	var layout func()
	var rebuildRows func()

	rebuildRows = func() {
		rebuilding = true
		defer func() { rebuilding = false }()
		rows := flattenInventoryRows(root, pingKnown, pingOK, selectedNode)
		list.Clear()
		currentNodes = make([]*invNode, len(rows))
		for i := range rows {
			n := rows[i].node
			currentNodes[i] = n
			switch n.kind {
			case invGroup:
				list.AddItem(rows[i].text, func() {
					n.expanded = !n.expanded
					selectedNode = n
					rebuildRows()
				})
			case invHost:
				list.AddItem(rows[i].text, func() {
					selectedNode = n
					showDetail(n)
				})
			}
		}
		idx := 0
		for i, n := range currentNodes {
			if n == selectedNode {
				idx = i
				break
			}
		}
		list.SetCurrentItem(idx)
	}

	// navigateHostDetail hops to the previous/next host *occurrence* in
	// allHostNodesInOrder's own fixed, full depth-first order (no
	// wraparound, this app's own convention everywhere else) - "the host
	// that would come next/previous vertically if the tree were
	// completely expanded," literally, by node identity rather than
	// hostname (allHostNodesInOrder's own doc comment explains why),
	// force-expanding the target's own ancestors (expandAncestors) so
	// it's guaranteed a visible row once the tree pane is shown again.
	navigateHostDetail := func(delta int) {
		nodes := allHostNodesInOrder(root)
		idx := -1
		for i, n := range nodes {
			if n == currentHostNode {
				idx = i
				break
			}
		}
		if idx == -1 {
			return
		}
		newIdx := idx + delta
		if newIdx < 0 || newIdx >= len(nodes) {
			return
		}
		target := nodes[newIdx]
		activeTabName := detailTabs.ActiveName()
		expandAncestors(target)
		selectedNode = target
		rebuildRows()
		showDetail(target)
		for i := 0; i < TabIndexByName(HostDetailTabNames, activeTabName); i++ {
			detailTabs.Next()
		}
	}

	showDetail = func(n *invNode) {
		_, _, totalWidth, _ := pages.GetInnerRect()
		footer := detailFooterText
		if totalWidth >= uikit.SplitMinTotalWidth {
			footer = splitDetailFooterText
		}
		detail, tabs, hdr, bar := BuildHostDetailPrimitive(app, stubPath, n.name, playbook, rest, footer, pingKnown[n.name], pingOK[n.name], detailCache)
		pages.RemovePage("detail")
		pages.AddPage("detail", detail, true, true)
		detailTabs, detailHeader, detailSearch, detailPrimitive = tabs, hdr, bar, detail
		currentHostNode = n
		viewingDetail = true
		layout()
	}

	layout = func() {
		_, _, totalWidth, _ := pages.GetInnerRect()
		lastTotalWidth = totalWidth
		if !viewingDetail {
			// No host opened yet - full-width tree regardless of terminal
			// width, matching the live run tree's own rule (design-docs/
			// TwoPanedLayout.md): a split pane only ever exists once a
			// drill-down is actually open. splitMode itself is
			// meaningless until then.
			splitMode = false
			pages.SwitchToPage("list")
			app.SetFocus(list)
			return
		}
		splitMode = totalWidth >= uikit.SplitMinTotalWidth
		if splitMode {
			splitFlex := tview.NewFlex().SetDirection(tview.FlexColumn).
				AddItem(listFlex, uikit.SplitTreeWidth(totalWidth), 0, false).
				AddItem(splitDivider, uikit.SplitDividerWidth, 0, false).
				AddItem(detailPrimitive, 0, 1, true)
			pages.RemovePage("split")
			pages.AddPage("split", splitFlex, true, true)
			pages.SwitchToPage("split")
			// detailTabs, not list, now that a host is actually open -
			// matching RunHostsListTUI's own choice (and the live run
			// tree's own drill-down): ↑/↓/PgUp/PgDn/Home/End scroll the
			// open tab's own content, and Left/Right/n/N hop to the
			// previous/next host regardless of which widget has real
			// focus (handleKey intercepts those centrally). A mouse
			// click/wheel on the tree pane still moves real focus back
			// onto list for as long as the user navigates there
			// (TreeList's own MouseHandler), reverting to detailTabs on
			// the next host selection (showDetail/layout).
			app.SetFocus(detailTabs.Primitive())
			return
		}
		pages.SwitchToPage("detail")
		app.SetFocus(detailTabs.Primitive())
	}

	handleRight := func() {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(currentNodes) {
			return
		}
		n := currentNodes[idx]
		if n.kind == invGroup && !n.expanded && len(n.children) > 0 {
			n.expanded = true
			selectedNode = n
			rebuildRows()
		}
	}

	handleLeft := func() {
		idx := list.GetCurrentItem()
		if idx < 0 || idx >= len(currentNodes) {
			return
		}
		n := currentNodes[idx]
		if n.kind == invGroup && n.expanded {
			n.expanded = false
			selectedNode = n
			rebuildRows()
			return
		}
		// Either an already-collapsed group or a host leaf: both mean
		// "move up to the parent and collapse it" - for a host this
		// mirrors the live run tree's own HostRowID handling
		// (livesession_nav.go's handleLeft: collapsing the parent task
		// removes this row, so the cursor moves up to the task row left
		// behind); for an already-collapsed group, this is this tree's
		// own extension of that same idea to its own extra depth - the
		// live run tree never needed it, a task row has no parent of its
		// own to climb to.
		parent := n.parent
		if parent == nil || parent.parent == nil {
			return // n is a top-level group, or (unusually) a direct
			// child of "all" itself - nothing rendered above it to move to.
		}
		parent.expanded = false
		selectedNode = parent
		rebuildRows()
	}

	expandAllInv := func() {
		var apply func(n *invNode)
		apply = func(n *invNode) {
			if n.kind != invGroup {
				return
			}
			n.expanded = true
			for _, c := range n.children {
				apply(c)
			}
		}
		for _, c := range root.children {
			apply(c)
		}
		rebuildRows()
	}

	collapseAllInv := func() {
		// Snap the cursor to its own top-level ancestor first -
		// collapsing every group would otherwise strand it on a row
		// about to vanish (mirrors the live run tree's own
		// collapseAll/HostRowID cursor-safety, livesession_nav.go).
		if selectedNode != nil {
			n := selectedNode
			for n.parent != nil && n.parent.parent != nil {
				n = n.parent
			}
			selectedNode = n
		}
		var apply func(n *invNode)
		apply = func(n *invNode) {
			if n.kind != invGroup {
				return
			}
			n.expanded = false
			for _, c := range n.children {
				apply(c)
			}
		}
		for _, c := range root.children {
			apply(c)
		}
		rebuildRows()
	}

	list.SetChangedFunc(func(index int) {
		if rebuilding {
			return
		}
		if index < 0 || index >= len(currentNodes) {
			return
		}
		n := currentNodes[index]
		selectedNode = n
		rebuildRows()
		if splitMode && n.kind == invHost {
			// Live-sync, matching RunHostsListTUI's own identical
			// decision: once a detail view is already open and split,
			// moving the cursor onto a host immediately retargets the
			// detail pane, no Enter needed (only reachable via a mouse
			// click/wheel on the tree pane - see layout's own doc comment
			// - since real keyboard focus sits on the detail view's own
			// tabs whenever one is open). Landing on a group row leaves
			// whatever detail was last showing untouched - a group has no
			// detail of its own to show.
			showDetail(n)
		}
	})
	rebuildRows()

	for _, h := range collectUniqueHostnames(root) {
		h := h
		go func() {
			ok := FetchHostPing(pingStubPath, h, rest)
			app.QueueUpdateDraw(func() {
				pingKnown[h] = true
				pingOK[h] = ok
				rebuildRows()
			})
		}()
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			if viewingDetail {
				detailSearch.CloseComposing()
			}
			quitting.Store(true)
			app.Stop()
			return nil
		}
		if help.HandleKey(event) {
			return nil
		}
		if viewingDetail && detailSearch.IsComposing() {
			detailSearch.HandleComposingKey(event)
			return nil
		}
		if translated, ok := uikit.TranslateNavAlias(event); ok {
			return translated
		}
		switch {
		case uikit.IsHelpKey(event) && viewingDetail:
			help.Open(hostDetailHelp, hostDetailHidden(true))
			return nil
		case uikit.IsHelpKey(event):
			help.Open(inventoryTreeHelp, nil)
			return nil
		// q while a detail view is open just closes it, back to the tree -
		// matching the live run tree's own handleOutputViewQuit - rather
		// than quitting outright. Checked here, before the viewingDetail
		// switch below, so it closes unconditionally even with an active
		// search (same precedence as handleOutputViewQuit, which also
		// bypasses Escape's own "clear the search first" nuance).
		case event.Rune() == 'q' && viewingDetail:
			viewingDetail = false
			layout()
			return nil
		case event.Rune() == 'q':
			quitting.Store(true)
			app.Stop()
			return nil
		}
		if viewingDetail {
			switch {
			case event.Key() == tcell.KeyEscape && detailSearch.HasActive():
				detailSearch.Clear()
				return nil
			case event.Key() == tcell.KeyEscape:
				// Closes the detail view in split mode too now, matching
				// the live run tree's own drill-down (design-docs/
				// TwoPanedLayout.md) - no longer gated on !splitMode.
				viewingDetail = false
				layout()
				return nil
			case event.Key() == tcell.KeyTab:
				detailTabs.Next()
				return nil
			case event.Key() == tcell.KeyBacktab:
				detailTabs.Prev()
				return nil
			// Left/Right are a plain alias for n/N here, same as
			// RunHostsListTUI's own identical addition - this tree has
			// only one dimension to hop a host along, so both pairs of
			// keys do the identical navigateHostDetail call.
			case event.Key() == tcell.KeyLeft:
				navigateHostDetail(-1)
				return nil
			case event.Key() == tcell.KeyRight:
				navigateHostDetail(1)
				return nil
			case event.Rune() == 'n':
				if detailSearch.HasActive() {
					detailSearch.Next()
				} else {
					navigateHostDetail(1)
				}
				return nil
			case event.Rune() == 'N':
				if detailSearch.HasActive() {
					detailSearch.Prev()
				} else {
					navigateHostDetail(-1)
				}
				return nil
			case event.Rune() == '/':
				detailSearch.Open()
				return nil
			case event.Rune() == 'y':
				detailSearch.ShowMessage(uikit.CopyActiveTabStatus(detailTabs))
				return nil
			}
		}
		// Tree-level bindings - only while no detail view is open at all,
		// matching the live run tree's own drill-down (no expand/collapse-
		// all concept while viewingOutput either): once a host is open,
		// Left/Right mean host-hop instead (above), even in split mode
		// alongside the tree. Right/Left/E/C aren't understood by
		// TreeList itself (design-docs/HostVerb.md's own decision to only
		// reuse the subset of the run tree's keymap that's actually
		// meaningful here), so they're intercepted centrally, the same
		// way tui.go's own handleRight/handleLeft/E/C are, before ever
		// reaching list's own InputHandler.
		if !viewingDetail {
			switch {
			case event.Key() == tcell.KeyRight:
				handleRight()
				return nil
			case event.Key() == tcell.KeyLeft:
				handleLeft()
				return nil
			case event.Rune() == 'E':
				expandAllInv()
				return nil
			case event.Rune() == 'C':
				collapseAllInv()
				return nil
			}
		}
		return event
	})

	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event == nil {
			return nil, action
		}
		if ev, act, handled := help.HandleMouse(event, action); handled {
			return ev, act
		}
		if !viewingDetail {
			if x, y := event.Position(); uikit.InRect(x, y, listHeader) || uikit.InRect(x, y, listFooter) {
				return nil, action
			}
			return event, action
		}
		if x, y := event.Position(); uikit.InRect(x, y, detailHeader) || uikit.InRect(x, y, detailSearch.Primitive()) {
			return nil, action
		}
		if action == tview.MouseLeftClick {
			if x, y := event.Position(); detailTabs.HandleClick(x, y) {
				return nil, action
			}
		}
		if splitMode {
			if x, y := event.Position(); uikit.InRect(x, y, listHeader) || uikit.InRect(x, y, listFooter) {
				return nil, action
			}
		}
		return event, action
	})

	go func() {
		ticker := time.NewTicker(uikit.SpinnerInterval)
		defer ticker.Stop()
		for range ticker.C {
			if quitting.Load() {
				return
			}
			app.QueueUpdate(func() {
				_, _, totalWidth, _ := pages.GetInnerRect()
				if totalWidth != lastTotalWidth {
					layout()
					app.ForceDraw()
				}
			})
		}
	}()

	app.SetRoot(pages, true).SetFocus(list)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "TUI error:", err)
	}
}
