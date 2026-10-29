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

package host

import "code.aw.net/claude/tangsible/internal/uikit"

// The host/hosts help tables and bottom bars (design-docs/Help.md) - the
// place to edit when one of RunHostDetailStandalone's/RunHostsListTUI's
// bindings changes.

const hostsListBar = " ?: help  enter: open  q: quit "

var hostsListHelp = uikit.HelpScreen{
	Title: "hosts",
	Sections: []uikit.HelpSection{
		{Title: "Reading the list", Entries: []uikit.HelpEntry{
			{Keys: "● grey / green / red", Desc: "Ping still running / host reachable / host unreachable"},
		}},
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "First / last host - also Ctrl-A / Ctrl-E, < / > and G"},
			{Keys: "Enter", Desc: "Show everything known about the host"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "q / Ctrl-C", Desc: "Quit"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click", Desc: "Same as Enter on that host"},
		}},
	},
}

// The "inventory" Verb's own tree view bar/help (design-docs/HostVerb.md's
// "New ideas") - deliberately narrower than hostsListHelp/hostDetailHelp's
// own keymap: no n/N (no "next task" equivalent among groups) and no /
// (left out of this first iteration) at the tree level - see
// RunInventoryTreeTUI's own doc comment. The detail view it opens into
// reuses hostDetailHelp/hostsDetailBar/hostsSplitDetailBar unchanged, so
// its own n/N/search/copy keep working exactly as they already do from
// "tangsible hosts".
const inventoryTreeBar = " ?: help  enter: open  →/←: expand/collapse  E/C: expand/collapse all  q: quit "

var inventoryTreeHelp = uikit.HelpScreen{
	Title: "inventory",
	Sections: []uikit.HelpSection{
		{Title: "Reading the tree", Entries: []uikit.HelpEntry{
			{Keys: "name (N)", Desc: "A group and its unique host count, direct or nested"},
			{Keys: "grey name (0)", Desc: "An empty group - no hosts anywhere beneath it"},
			{Keys: "● grey / green / red", Desc: "Ping still running / host reachable / host unreachable"},
		}},
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "First / last row - also Ctrl-A / Ctrl-E, < / > and G"},
		}},
		{Title: "Expanding and collapsing", Entries: []uikit.HelpEntry{
			{Keys: "Enter", Desc: "Toggle a group, or show everything known about a host"},
			{Keys: "→", Desc: "Expand a collapsed group"},
			{Keys: "←", Desc: "Collapse an expanded group - or its parent, if it's already collapsed"},
			{Keys: "E / C", Desc: "Expand / collapse every group"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "q / Ctrl-C", Desc: "Quit"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click a group", Desc: "Toggle it"},
			{Keys: "Click a host", Desc: "Same as Enter on it"},
		}},
	},
}

// The host detail view's bars: standalone ("tangsible host <name>"), and
// opened from the hosts list full-screen or side by side with it. Esc and
// q both close back to the list (hostsDetailBar/hostsSplitDetailBar say
// "esc: back" only, the same single-canonical-hint convention the live
// run tree's own outputBar uses for its identical Esc/q pair) - split
// mode is no exception, matching that same drill-down (design-docs/
// TwoPanedLayout.md).
const (
	hostDetailBar       = " ?: help  tab: next tab  /: search  y: copy  q: quit "
	hostsDetailBar      = " ?: help  tab: next tab  n/N, ←/→: host  /: search  y: copy  esc: back "
	hostsSplitDetailBar = " ?: help  tab: next tab  n/N, ←/→: host  /: search  y: copy  esc: back "
)

const (
	hostDetailIDNextHost       = "nexthost"       // only when opened from the hosts list
	hostDetailIDBack           = "back"           // only when opened from the hosts list
	hostDetailIDQuitStandalone = "quitstandalone" // only "tangsible host <name>" - q quits outright there
	hostDetailIDQuitFromList   = "quitfromlist"   // only when opened from the hosts list - q goes back instead, only Ctrl-C quits
)

// hostDetailHidden hides the detail view's list-only entries - fromList is
// false only for "tangsible host <name>" standalone, which has no list to
// hop among or go back to in the first place, so q quits there same as
// Ctrl-C rather than going back to anything.
func hostDetailHidden(fromList bool) func(id string) bool {
	return func(id string) bool {
		switch id {
		case hostDetailIDNextHost, hostDetailIDBack, hostDetailIDQuitFromList:
			return !fromList
		case hostDetailIDQuitStandalone:
			return fromList
		}
		return false
	}
}

var hostDetailHelp = uikit.HelpScreen{
	Title: "host",
	Sections: []uikit.HelpSection{
		{Title: "Tabs", Entries: []uikit.HelpEntry{
			{Keys: "Summary", Desc: "Key facts, gathered live (or from the fact cache)"},
			{Keys: "Groups", Desc: "Every group the host belongs to, including inherited ones"},
			{Keys: "Plays", Desc: "Which plays and tasks would run on this host"},
			{Keys: "host_vars", Desc: "The host's own inventory variables and host_vars file(s)"},
			{Keys: "Everything known", Desc: "ansible-inventory --host output, all variables"},
		}},
		{Title: "Switching what's shown", Entries: []uikit.HelpEntry{
			{Keys: "Tab / Shift-Tab", Desc: "Next / previous tab"},
			{Keys: "n / N, ← / →", Desc: "Next / previous host, same tab. While a search is active: n / N jump to the next / previous match instead", ID: hostDetailIDNextHost},
		}},
		{Title: "Scrolling", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Scroll a line"},
			{Keys: "PgUp / PgDn", Desc: "Scroll a page - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "Top / bottom - also Ctrl-A / Ctrl-E, < / > and G"},
		}},
		{Title: "This tab", Entries: []uikit.HelpEntry{
			{Keys: "/", Desc: "Search this tab's text; n / N step through the matches"},
			{Keys: "y", Desc: "Copy this tab's text to the clipboard"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc", Desc: "Clear an active search"},
			{Keys: "Esc / q", Desc: "Back to the list of hosts (when no search is active)", ID: hostDetailIDBack},
			{Keys: "Ctrl-C", Desc: "Quit", ID: hostDetailIDQuitFromList},
			{Keys: "q / Ctrl-C", Desc: "Quit", ID: hostDetailIDQuitStandalone},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click a tab", Desc: "Switch to it"},
			{Keys: "Drag the divider", Desc: "Side-by-side layout only: resize the two panes"},
		}},
	},
}
