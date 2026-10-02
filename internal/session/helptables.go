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

package session

import "code.aw.net/claude/tangsible/internal/uikit"

// Shortcut descriptions for the live session's screens (design-docs/
// Help.md) - the place to edit when a binding changes: each screen has its
// '?' help table and, right next to it, its bottom bar. The bar is a
// separate hand-written string (the essentials only); keep the two
// consistent with each other, with the dispatchers in livesession_input.go/
// livesession_mouse.go, and with design-docs/Keyboard-shortcuts.md, which
// stays the long-form reference.

// Entry IDs a session can hide depending on context (treeHelpHidden).
const (
	helpIDRerun   = "rerun"   // no requestRerun wired
	helpIDRevisit = "revisit" // only in a revisit session
)

// treeBar is the tree view's bottom bar. currentMainBottomBarText drops
// "r: re-run" when nothing can rerun and appends the revisit and check-mode
// notes - the same contexts treeHelpHidden covers for the help dialog.
const treeBar = " ?: help  f: filter  /: search  o: output  r: re-run  q: quit "

var treeHelp = uikit.HelpScreen{
	Title: "tree view",
	Sections: []uikit.HelpSection{
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor (stops following)"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "First / last row - also Ctrl-A / Ctrl-E, < / > and G. Doesn't resume following"},
			{Keys: "n / N", Desc: "Next / previous task, keeping the selected host where possible"},
			{Keys: "F", Desc: "Jump to the newest row and follow new ones again - the only key that resumes following"},
		}},
		{Title: "Expanding", Entries: []uikit.HelpEntry{
			{Keys: "Enter", Desc: "Expand / collapse a task; on a host row, open its result"},
			{Keys: "→ / ←", Desc: "Expand / collapse a task (← on a host row jumps to its task)"},
			{Keys: "E / C", Desc: "Expand / collapse all. While running, new tasks start the same way"},
		}},
		{Title: "Filtering", Entries: []uikit.HelpEntry{
			{Keys: "f", Desc: "Filter: all / interesting / changed / failed tasks"},
			{Keys: "/", Desc: "Show only tasks whose name, source or output contains a text"},
		}},
		{Title: "More views", Entries: []uikit.HelpEntry{
			{Keys: "o", Desc: "ansible-playbook's own console output, following it while the run is live"},
			{Keys: "r", Desc: "Re-run, optionally with other tags/hosts or from a given play (once the run has finished)", ID: helpIDRerun},
			{Keys: "d", Desc: "Compare this run with a previous one (once the run has finished)"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc", Desc: "Back to the list of previous runs", ID: helpIDRevisit},
			{Keys: "q / Ctrl-C", Desc: "Interrupt the run while it's going; quit once it has finished"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll (stops following)"},
			{Keys: "Click", Desc: "Same as Enter on that row"},
		}},
	},
}

// treeHelpHidden drops the entries that don't apply to this session - the
// same conditions the key handlers themselves check.
func (s *liveSession) treeHelpHidden(id string) bool {
	switch id {
	case helpIDRerun:
		return s.requestRerun == nil
	case helpIDRevisit:
		return !s.revisitActive
	}
	return false
}
