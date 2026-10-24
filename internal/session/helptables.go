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

// Entry IDs a session can hide depending on context (helpEntryHidden).
const (
	helpIDRerun   = "rerun"   // no requestRerun wired
	helpIDRevisit = "revisit" // only in a revisit session
	helpIDDebug   = "debug"   // only in a debug session
)

// treeBar is the tree view's bottom bar. currentMainBottomBarText drops
// "r: re-run" when nothing can rerun and appends the revisit and check-mode
// notes - the same contexts helpEntryHidden covers for the help dialog.
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
		{Title: "Debug mode", Entries: []uikit.HelpEntry{
			{Keys: "F5", Desc: "Run - to the next armed breakpoint, or to completion with none set", ID: helpIDDebug},
			{Keys: "F9", Desc: "Arm / disarm a breakpoint on the selected task (or click its row)", ID: helpIDDebug},
			{Keys: "F10", Desc: "Next task - skip the rest of this task's hosts, stop at the next task's first one", ID: helpIDDebug},
			{Keys: "F11", Desc: "Step - stop at the very next pause, armed or not (host by host within the same task too)", ID: helpIDDebug},
			{Keys: "F12", Desc: "Redo - re-run the task/host currently paused at, waiting for that exact result", ID: helpIDDebug},
			{Keys: "v", Desc: "Open the variable watch explorer (type expr = value to set one)", ID: helpIDDebug},
			{Keys: "q", Desc: "While running: soft stop - finish this task, then pause (ctrl-c still force-kills)", ID: helpIDDebug},
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

// helpEntryHidden drops the entries that don't apply to this session - the
// same conditions the key handlers themselves check. Shared by treeHelp
// and outputHelp (passed as both HelpOverlay.Open calls' own hide
// parameter) since F5/F9/F10/F11 work identically whichever of the two is
// frontmost (handleDebugKey's own doc comment) - nothing here is actually
// tree-specific despite the historical name.
func (s *liveSession) helpEntryHidden(id string) bool {
	switch id {
	case helpIDRerun:
		return s.requestRerun == nil
	case helpIDRevisit:
		return !s.revisitActive
	case helpIDDebug:
		return !s.debugMode
	}
	return false
}

// outputBar is the drill-down's bottom bar. tabSearchPanel restores it
// whenever a search or a 'y' copy status clears.
const outputBar = " ?: help  tab: next tab  ←/→: host  n/N: task  /: search  y: copy  esc: back "

var outputHelp = uikit.HelpScreen{
	Title: "result view",
	Sections: []uikit.HelpSection{
		{Title: "Switching what's shown", Entries: []uikit.HelpEntry{
			{Keys: "Tab / Shift-Tab", Desc: "Next / previous tab"},
			{Keys: "→ / ←", Desc: "Same task, next / previous host"},
			{Keys: "n / N", Desc: "Same host, next / previous task (skipping tasks the filter hides). While a search has matches: next / previous match instead"},
		}},
		{Title: "Scrolling", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Scroll a line"},
			{Keys: "PgUp / PgDn", Desc: "Scroll a page - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "Top / bottom - also Ctrl-A / Ctrl-E, < / > and G"},
		}},
		{Title: "This tab", Entries: []uikit.HelpEntry{
			{Keys: "/", Desc: "Search this tab's text"},
			{Keys: "y", Desc: "Copy this tab's text to the clipboard"},
			{Keys: "e", Desc: "Open the task's source file in $VISUAL / $EDITOR"},
		}},
		{Title: "Debug mode", Entries: []uikit.HelpEntry{
			{Keys: "F5", Desc: "Run - to the next armed breakpoint, or to completion with none set", ID: helpIDDebug},
			{Keys: "F9", Desc: "Arm / disarm a breakpoint on the selected task (or click its row)", ID: helpIDDebug},
			{Keys: "F10", Desc: "Next task - skip the rest of this task's hosts, stop at the next task's first one", ID: helpIDDebug},
			{Keys: "F11", Desc: "Step - stop at the very next pause, armed or not (host by host within the same task too)", ID: helpIDDebug},
			{Keys: "F12", Desc: "Redo - re-run the task/host currently paused at, waiting for that exact result", ID: helpIDDebug},
			{Keys: "v", Desc: "Open the variable watch explorer (type expr = value to set one)", ID: helpIDDebug},
			{Keys: "q", Desc: "While running: soft stop - finish this task, then pause (ctrl-c still force-kills)", ID: helpIDDebug},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc / Enter / q", Desc: "Back to the tree, cursor on this task and host. Esc clears an active search first"},
			{Keys: "Ctrl-C", Desc: "Interrupt the run while it's going; quit once it has finished"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click a tab", Desc: "Switch to it"},
			{Keys: "Click a tree row", Desc: "Side-by-side layout only: expand / collapse a task, or show a host's result"},
		}},
	},
}

// stdoutBar is the raw-output view's ('o') bottom bar.
const stdoutBar = " ?: help  F: follow  /: search  y: copy  esc: back "

var stdoutHelp = uikit.HelpScreen{
	Title: "console output",
	Sections: []uikit.HelpSection{
		{Title: "Following", Entries: []uikit.HelpEntry{
			{Keys: "F", Desc: "Jump to the end and follow new output while the run is live - the only key that resumes following"},
		}},
		{Title: "Scrolling (stops following)", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Scroll a line"},
			{Keys: "PgUp / PgDn", Desc: "Scroll a page - also b / Space and Ctrl-B / Ctrl-F"},
			{Keys: "Home / End", Desc: "Top / bottom - also Ctrl-A / Ctrl-E, < / > and G. End doesn't resume following"},
		}},
		{Title: "Search and copy", Entries: []uikit.HelpEntry{
			{Keys: "/", Desc: "Search the output. Output arriving meanwhile is held back (the top bar counts it) and appears once the search is cleared"},
			{Keys: "n / N", Desc: "Next / previous match"},
			{Keys: "y", Desc: "Copy the whole output to the clipboard"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "Esc / o / q", Desc: "Back to the tree. Esc clears an active search first"},
			{Keys: "Ctrl-C", Desc: "Interrupt the run while it's going; quit once it has finished"},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll (stops following)"},
		}},
	},
}

// The re-run dialog has no bottom bar of its own - its title carries the
// key hints. Help opens with F1 only: '?' must stay typeable in its fields.
var rerunHelp = uikit.HelpScreen{
	Title: "re-run",
	Sections: []uikit.HelpSection{
		{Title: "Fields - all optional", Entries: []uikit.HelpEntry{
			{Keys: "Start with play", Desc: "Skip every play before this one (a top-level play's name; offers completions). Runs a temporary copy of the playbook with the earlier plays removed"},
			{Keys: "Limit tags to", Desc: "--tags: comma-separated tags (offers completions)"},
			{Keys: "Skip tags", Desc: "--skip-tags: comma-separated tags (offers completions)"},
			{Keys: "Limit hosts to", Desc: "--limit: comma-separated hosts or groups (offers completions)"},
		}},
		{Title: "Checkboxes - shown only when they apply to the last run", Entries: []uikit.HelpEntry{
			{Keys: "Resume where failed", Desc: "Start with the earliest play that had a failure, and only on the hosts that failed (checks \"Only failed\" too)"},
			{Keys: "Only failed", Desc: "Limit hosts to the ones that failed in the last run"},
			{Keys: "Only unreachable", Desc: "Limit hosts to the ones that were unreachable in the last run - combines with \"Only failed\""},
		}},
		{Title: "Keys", Entries: []uikit.HelpEntry{
			{Keys: "Enter", Desc: "Run (or pick the highlighted completion)"},
			{Keys: "Tab / Shift-Tab", Desc: "Next / previous field"},
			{Keys: "Space", Desc: "Toggle the focused checkbox"},
			{Keys: "Esc", Desc: "Cancel (or close the completion list)"},
			{Keys: "F1", Desc: "This help"},
		}},
	},
}
