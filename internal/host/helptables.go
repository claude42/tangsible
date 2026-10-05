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

// The host detail view's bars: standalone ("tangsible host <name>"), and
// opened from the hosts list full-screen or side by side with it.
const (
	hostDetailBar       = " ?: help  tab: next tab  /: search  y: copy  q: quit "
	hostsDetailBar      = " ?: help  tab: next tab  n/N: host  /: search  y: copy  esc: back  q: quit "
	hostsSplitDetailBar = " ?: help  tab: next tab  n/N: host  /: search  y: copy  q: quit "
)

const (
	hostDetailIDNextHost = "nexthost" // only when opened from the hosts list
	hostDetailIDBack     = "back"     // only full-screen from the hosts list
)

// hostDetailHidden hides the detail view's list-only entries: fromList is
// false for "tangsible host <name>", split true when the list is visible
// beside the detail (Esc has nothing to go back to then).
func hostDetailHidden(fromList, split bool) func(id string) bool {
	return func(id string) bool {
		switch id {
		case hostDetailIDNextHost:
			return !fromList
		case hostDetailIDBack:
			return !fromList || split
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
			{Keys: "host_vars", Desc: "The host's own host_vars file(s)"},
			{Keys: "Everything known", Desc: "ansible-inventory --host output, all variables"},
		}},
		{Title: "Switching what's shown", Entries: []uikit.HelpEntry{
			{Keys: "Tab / Shift-Tab", Desc: "Next / previous tab"},
			{Keys: "n / N", Desc: "Next / previous host, same tab. While a search is active: next / previous match instead", ID: hostDetailIDNextHost},
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
			{Keys: "Esc", Desc: "Back to the list of hosts (when no search is active)", ID: hostDetailIDBack},
			{Keys: "q / Ctrl-C", Desc: "Quit"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click a tab", Desc: "Switch to it"},
		}},
	},
}
