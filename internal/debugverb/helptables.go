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

package debugverb

import "code.aw.net/claude/tangsible/internal/uikit"

// The debug verb's help table and bottom bar (design-docs/Help.md) - the
// place to edit when one of runDebugTUI's own bindings changes.

// Note: 'b' is already TranslateNavAlias's own Page-Up alias (shared app-
// wide, uikit/tui_dialogs.go) - confirmed live that it never reaches this
// package's own key handling at all, so breakpoint-toggle uses 't'
// instead, not 'b' as a debugger's muscle memory might expect.
const (
	debugBarIdle     = " ?: help  t: breakpoint  enter: run  s: step  q: quit "
	debugBarRunning  = " running... (ctrl-c to interrupt) "
	debugBarPaused   = " ?: help  t: breakpoint  enter: run to next breakpoint  s: step  v: variables  q: quit "
	debugBarFinished = " ?: help  finished - q: quit "
	debugBarVars     = " esc / v: back "
)

var debugHelp = uikit.HelpScreen{
	Title: "debug",
	Sections: []uikit.HelpSection{
		{Title: "Before the run starts", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor"},
			{Keys: "t", Desc: "Toggle a breakpoint on the selected task"},
			{Keys: "enter", Desc: "Run - stops at the first armed breakpoint, or runs to completion with none set"},
			{Keys: "s", Desc: "Step - stops at the very first task regardless of breakpoints"},
		}},
		{Title: "While paused", Entries: []uikit.HelpEntry{
			{Keys: "enter", Desc: "Continue - runs to the next armed breakpoint (or to completion)"},
			{Keys: "s", Desc: "Step - stops at the very next task, armed or not"},
			{Keys: "t", Desc: "Toggle a breakpoint on the selected task - takes effect immediately"},
			{Keys: "v", Desc: "View this task's own variables (task_vars, the pre-task snapshot)"},
		}},
		{Title: "Reading the tree", Entries: []uikit.HelpEntry{
			{Keys: "●  (red)", Desc: "A breakpoint is armed on this task"},
			{Keys: "●  (green/yellow/teal/red/maroon)", Desc: "A host's own outcome for this task: ok/changed/skipped/failed/unreachable"},
			{Keys: "(paused: host)", Desc: "The task currently paused, and which host"},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "q / Ctrl-C", Desc: "Interrupt the child while it's running, quit once it's finished (or immediately before any run starts)"},
		}},
	},
}
