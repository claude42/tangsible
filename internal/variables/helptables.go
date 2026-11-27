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

package variables

import "code.aw.net/claude/tangsible/internal/uikit"

// The help table and bottom bar of `tangsible variables` (design-docs/Help.md)
// - the place to edit when a binding in run.go changes.

const treeBar = " ?: help  enter: expand or show file  e: edit  ←/→: host  h: pick host  R: reload  q: quit "

var treeHelp = uikit.HelpScreen{
	Title: "variables",
	Sections: []uikit.HelpSection{
		{Title: "Reading the list", Entries: []uikit.HelpEntry{
			{Keys: "name  value  where", Desc: "One line per variable: the value of the definition that wins, and the file and line it is written in"},
			{Keys: "Enter", Desc: "Show every definition of the variable, the winning one first, each with its place in Ansible's order of precedence. Enter on a definition shows its file"},
			{Keys: "?", Desc: "after a name: Ansible's own answer differs from the order we worked out - the order of the other definitions is the uncertain part"},
			{Keys: "<vaulted>", Desc: "A vault-encrypted value, never shown"},
		}},
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "First / last row - also Ctrl-A / Ctrl-E, < / > and G"},
			{Keys: "E / C", Desc: "Expand / collapse every variable"},
			{Keys: "← / →", Desc: "The previous / next host of the inventory"},
			{Keys: "h", Desc: "Pick a host from the list"},
		}},
		{Title: "Files", Entries: []uikit.HelpEntry{
			{Keys: "e", Desc: "Open the file the row's definition is in, in your editor; the list is read again afterwards"},
			{Keys: "R", Desc: "Read the files again"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "q / Ctrl-C", Desc: "Quit"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Click", Desc: "Same as Enter on that row"},
			{Keys: "Wheel", Desc: "Scroll"},
		}},
	},
}

const fileBar = " esc: back  ↑/↓: scroll  /: search  y: copy  e: edit  ?: help "

var fileHelp = uikit.HelpScreen{
	Title: "variables - file",
	Sections: []uikit.HelpSection{
		{Title: "Reading the file", Entries: []uikit.HelpEntry{
			{Keys: "highlighted lines", Desc: "The definition you opened - the file is scrolled so they are on screen"},
		}},
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Scroll"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "Top / bottom of the file - also Ctrl-A / Ctrl-E, < / > and G"},
		}},
		{Title: "Search and copy", Entries: []uikit.HelpEntry{
			{Keys: "/", Desc: "Search the file; n / N for the next / previous match"},
			{Keys: "y", Desc: "Copy the whole file to the clipboard"},
		}},
		{Title: "Files", Entries: []uikit.HelpEntry{
			{Keys: "e", Desc: "Open the file in your editor; everything is read again afterwards"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "Esc / q", Desc: "Back to the list (Esc first ends a search)"},
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Ctrl-C", Desc: "Quit"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Click a definition", Desc: "In the two-pane layout: show that definition's file instead"},
			{Keys: "Drag the divider", Desc: "Resize the two panes"},
		}},
	},
}
