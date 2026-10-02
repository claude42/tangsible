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

package revisit

import "code.aw.net/claude/tangsible/internal/uikit"

// The run list's help table and bottom bars (design-docs/Help.md) - the
// place to edit when one of RunRevisitListTUI's bindings changes. The list
// serves two callers ("revisit" itself, and diff mode's picker), so the bar
// and the context-dependent entries vary by its allowRerun/nested flags.

func runListBar(allowRerun, nested bool) string {
	switch {
	case nested:
		return " ?: help  enter: compare  esc: back "
	case allowRerun:
		return " ?: help  enter: open  r: re-run  q: quit "
	}
	return " ?: help  enter: open  q: quit "
}

const (
	runListIDOpen    = "open"    // "revisit": open the run
	runListIDCompare = "compare" // diff picker: compare against the run
	runListIDRerun   = "rerun"
	runListIDQuit    = "quit"
	runListIDBack    = "back"
)

func runListHelpHidden(allowRerun, nested bool) func(id string) bool {
	return func(id string) bool {
		switch id {
		case runListIDOpen, runListIDQuit:
			return nested
		case runListIDCompare, runListIDBack:
			return !nested
		case runListIDRerun:
			return !allowRerun
		}
		return false
	}
}

var runListHelp = uikit.HelpScreen{
	Title: "previous runs",
	Sections: []uikit.HelpSection{
		{Title: "Moving around", Entries: []uikit.HelpEntry{
			{Keys: "↑ / ↓, j / k", Desc: "Move the cursor"},
			{Keys: "PgUp / PgDn", Desc: "Page up / down"},
			{Keys: "Home / End", Desc: "First / last run - also Ctrl-A / Ctrl-E"},
		}},
		{Title: "Actions", Entries: []uikit.HelpEntry{
			{Keys: "Enter", Desc: "Open the run as a tree, as it looked when it finished", ID: runListIDOpen},
			{Keys: "Enter", Desc: "Compare the current run against this one", ID: runListIDCompare},
			{Keys: "r", Desc: "Open the run straight into the re-run dialog", ID: runListIDRerun},
		}},
		{Title: "General", Entries: []uikit.HelpEntry{
			{Keys: "? / F1", Desc: "This help"},
			{Keys: "q / Ctrl-C", Desc: "Quit", ID: runListIDQuit},
			{Keys: "Esc / q", Desc: "Back to the tree", ID: runListIDBack},
		}},
		{Title: "Mouse", Entries: []uikit.HelpEntry{
			{Keys: "Wheel", Desc: "Scroll"},
			{Keys: "Click", Desc: "Same as Enter on that run"},
		}},
	},
}
