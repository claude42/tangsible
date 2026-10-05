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

package diff

import "code.aw.net/claude/tangsible/internal/uikit"

// Diff mode's help tables and bottom bars (design-docs/Help.md) - the
// place to edit when one of RunDiffTreeTUI's bindings changes. (The
// comparison-run picker before it is revisit's run list - see
// revisit/helptables.go.)

const diffTreeBar = " ?: help  enter: expand/open  esc: back "

var diffTreeHelp = uikit.HelpScreen{
	Title: "diff",
	Sections: []uikit.HelpSection{
		{Title: "Reading the tree", Entries: []uikit.HelpEntry{
			{Keys: "underlined host", Desc: "This host's outcome or output differs between the two runs"},
			{Keys: "(new only)", Desc: "Task only in the current run (underlined)"},
			{Keys: "(old only)", Desc: "Task only in the older run (struck through, italic)"},
		}},
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "First / last row - also Ctrl-A / Ctrl-E, < / > and G"},
			{Keys: "Enter", Desc: "Expand / collapse a task; on a host row, compare its result in both runs"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc / q", Desc: "Back to the list of runs to compare against"},
			{Keys: "Ctrl-C", Desc: "Back to the list as well"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click", Desc: "Same as Enter on that row"},
		}},
	},
}

const diffOutputBar = " ?: help  tab: next tab  /: search  y: copy  esc: back "

var diffOutputHelp = uikit.HelpScreen{
	Title: "diff result",
	Sections: []uikit.HelpSection{
		{Title: "Tabs", Entries: []uikit.HelpEntry{
			{Keys: "Tab / Shift-Tab", Desc: "Next / previous tab. Each shows a unified diff between the two runs; a task present in only one run shows that run's result instead"},
		}},
		{Title: "Scrolling", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Scroll a line"},
			{Keys: "PgUp / PgDn", Desc: "Scroll a page - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "Top / bottom - also Ctrl-A / Ctrl-E, < / > and G"},
		}},
		{Title: "This tab", Entries: []uikit.HelpEntry{
			{Keys: "/", Desc: "Search this tab's text"},
			{Keys: "n / N", Desc: "Next / previous match"},
			{Keys: "y", Desc: "Copy this tab's text to the clipboard"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc / Enter / q", Desc: "Back to the diff tree. Esc clears an active search first"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click a tab", Desc: "Switch to it"},
		}},
	},
}
