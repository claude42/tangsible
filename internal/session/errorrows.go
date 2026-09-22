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

// Backs design-docs/ErrorOutput.md: a real user report that "Playbook
// failed" gave no hint of *why* until the TUI was closed (stderr can't be
// printed live - it would corrupt the alternate screen - so it used to be
// collected silently and only ever dumped after the whole session ended).
// This surfaces it inline instead, as ordinary rows right below the
// status line, the same "just append more rows" mechanism the recap
// section (recap.go) already uses for its own post-run summary.
package session

import (
	"code.aw.net/claude/tangsible/internal/runner"
	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/rivo/tview"
)

// errorOutputHeadingRowID identifies the error-output block's own two
// fixed decoration rows (a blank spacer, then the "Error output:"
// heading) - mirroring recapHeadingRowID's own reasoning (recap.go):
// distinct typed int values, not StatusDividerRowID's shared zero-size
// sentinel, so rebuild()'s identity-based selection-restoration can still
// tell them apart from each other (and errorOutputLineRowID below is its
// own distinct type too, so a heading row and a body line can never
// collide just because they happen to share the same underlying int).
type errorOutputHeadingRowID int

const (
	errorOutputDivider errorOutputHeadingRowID = iota
	errorOutputHeading
)

// errorOutputLineRowID identifies one word-wrapped line of the block's
// own body text, by its index within it.
type errorOutputLineRowID int

const errorOutputHeadingText = "Error output:"

// errorOutputRows builds the rows design-docs/ErrorOutput.md's display
// shows right below the "Playbook failed" status row, once a generation
// ends in a genuine failure (rebuild()'s own GenuineFailure gate, same
// predicate the auto-jump-to-failed-host feature already uses) - a blank
// divider, a plain heading (styled like recap.go's own "Summary" heading:
// bold white, not a failure color, so it's never mistaken for part of the
// stderr text itself), then the collected stderr word-wrapped to width
// (uikit.WrapText - TreeList's own rows are single-line and don't wrap on
// their own, see its own doc comment). Filtered through runner.
// FilterRedundantWarnings first, the same filter the post-quit stderr
// dump already applies: a "[WARNING]:" line duplicates what the tree's
// own ⚠ marker/host detail already shows, so printing it a second time
// here would be pure noise, not new information. Returns nil - no rows
// at all - once nothing survives that filter (a genuine failure with no
// interesting stderr of its own, e.g. a bad exit code from a module that
// reported everything through its own JSON result instead), so an empty
// block never appears; also nil before any generation has ever reported
// anything here at all (s.lastStderr nil-checked - a revisit session has
// no live generation to report on until/unless a rerun happens within
// it) or reported an explicitly empty result.
func (s *liveSession) errorOutputRows(width int) []uikit.Row {
	if s.lastStderr == nil {
		return nil
	}
	raw := s.lastStderr.Load()
	if raw == nil {
		return nil
	}
	lines := runner.FilterRedundantWarnings(*raw)
	if len(lines) == 0 {
		return nil
	}

	rows := []uikit.Row{
		{Text: "", ID: errorOutputDivider},
		{Text: "[white::b]" + errorOutputHeadingText + "[-::-]", ID: errorOutputHeading},
	}
	idx := 0
	for _, l := range lines {
		for _, wrapped := range uikit.WrapText(l, width) {
			rows = append(rows, uikit.Row{Text: tview.Escape(wrapped), ID: errorOutputLineRowID(idx)})
			idx++
		}
	}
	return rows
}
