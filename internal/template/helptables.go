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

package template

import "code.aw.net/claude/tangsible/internal/uikit"

// The template view's help table and bottom bar (design-docs/Help.md) -
// the place to edit when one of the template view's bindings changes.

const templateBar = " ?: help  tab: next tab  e: edit  h: host  /: search  y: copy  q: quit "

var templateHelp = uikit.HelpScreen{
	Title: "template",
	Sections: []uikit.HelpSection{
		{Title: "Tabs", Entries: []uikit.HelpEntry{
			{Keys: "<host>", Desc: "The template rendered for that host. A red tab name means rendering failed - the tab shows the error"},
			{Keys: "Source", Desc: "The template file itself"},
			{Keys: "Tab / Shift-Tab", Desc: "Next / previous tab"},
		}},
		{Title: "Editing", Entries: []uikit.HelpEntry{
			{Keys: "e", Desc: "Edit the template in $VISUAL / $EDITOR, then render it again for every host"},
			{Keys: "h", Desc: "Render the last viewed host tab for a different host instead (or switch to that host's tab if it's already open)"},
		}},
		{Title: "Scrolling", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Scroll a line"},
			{Keys: "PgUp / PgDn", Desc: "Scroll a page"},
			{Keys: "Home / End", Desc: "Top / bottom - also Ctrl-A / Ctrl-E"},
		}},
		{Title: "This tab", Entries: []uikit.HelpEntry{
			{Keys: "/", Desc: "Search this tab's text"},
			{Keys: "n / N", Desc: "Next / previous match"},
			{Keys: "y", Desc: "Copy this tab's text to the clipboard"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc", Desc: "Clear an active search (otherwise does nothing here, so it can't quit by accident)"},
			{Keys: "q / Ctrl-C", Desc: "Quit"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click a tab", Desc: "Switch to it"},
		}},
	},
}
