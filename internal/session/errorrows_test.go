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

import (
	"sync/atomic"
	"testing"

	"github.com/rivo/tview"
)

func TestErrorOutputRows_NilLastStderr(t *testing.T) {
	s := &liveSession{}
	if got := s.errorOutputRows(78); got != nil {
		t.Errorf("errorOutputRows() = %v, want nil (s.lastStderr never wired - e.g. a revisit session with no rerun yet)", got)
	}
}

func TestErrorOutputRows_NeverStored(t *testing.T) {
	var ptr atomic.Pointer[[]string]
	s := &liveSession{lastStderr: &ptr}
	if got := s.errorOutputRows(78); got != nil {
		t.Errorf("errorOutputRows() = %v, want nil (lastStderr.Load() itself is nil - no generation has ever stored anything yet)", got)
	}
}

func TestErrorOutputRows_EmptyAfterFilteringWarnings(t *testing.T) {
	var ptr atomic.Pointer[[]string]
	lines := []string{
		"[WARNING]: Host 'host1' is using the discovered Python interpreter...",
		"[WARNING]: Host 'host2' is using the discovered Python interpreter...",
	}
	ptr.Store(&lines)
	s := &liveSession{lastStderr: &ptr}
	if got := s.errorOutputRows(78); got != nil {
		t.Errorf("errorOutputRows() = %v, want nil (nothing survives runner.FilterRedundantWarnings)", got)
	}
}

func TestErrorOutputRows_GenuinelyEmptySlice(t *testing.T) {
	var ptr atomic.Pointer[[]string]
	empty := []string{}
	ptr.Store(&empty)
	s := &liveSession{lastStderr: &ptr}
	if got := s.errorOutputRows(78); got != nil {
		t.Errorf("errorOutputRows() = %v, want nil (a generation that reported zero stderr lines)", got)
	}
}

// TestErrorOutputRows_BuildsHeadingThenWrappedFilteredLines is the actual
// reported case (design-docs/ErrorOutput.md): a real ansible [ERROR]:
// message mixed with a [WARNING]: line that must be dropped, and a long
// paragraph that must wrap while a short, whitespace-significant line
// (the caret pointer) stays untouched.
func TestErrorOutputRows_BuildsHeadingThenWrappedFilteredLines(t *testing.T) {
	var ptr atomic.Pointer[[]string]
	lines := []string{
		"[WARNING]: Host 'host1' is using the discovered Python interpreter...",
		"[ERROR]: The 'ansible.builtin.pause' module bypasses the host loop, which is currently not supported in the free strategy and would instead execute for every host in the inventory list.",
		"Origin: /tmp/site.yml:9:7",
		"",
		"8   handlers:",
		"9     - name: my handler",
		"        ^ column 7",
	}
	ptr.Store(&lines)
	s := &liveSession{lastStderr: &ptr}

	rows := s.errorOutputRows(78)
	if len(rows) < 3 {
		t.Fatalf("got %d rows, want at least a divider + heading + one body line", len(rows))
	}
	if _, ok := rows[0].ID.(errorOutputHeadingRowID); !ok || rows[0].Text != "" {
		t.Errorf("rows[0] = %+v, want the blank divider row", rows[0])
	}
	if rows[1].ID != errorOutputHeading || rows[1].Text != "[white::b]"+errorOutputHeadingText+"[-::-]" {
		t.Errorf("rows[1] = %+v, want the \"Error output:\" heading row", rows[1])
	}

	// The [WARNING]: line must not appear anywhere in the body.
	for _, r := range rows[2:] {
		if _, ok := r.ID.(errorOutputHeadingRowID); ok {
			t.Errorf("a heading-typed row ID appeared in the body: %+v", r)
		}
		if got := r.Text; len(got) >= 10 && got[:10] == "[WARNING]:" {
			t.Errorf("a [WARNING]: line leaked through into the rows: %q", got)
		}
	}

	// Every body row must be individually addressable by its own
	// errorOutputLineRowID, 0-based and contiguous.
	for i, r := range rows[2:] {
		id, ok := r.ID.(errorOutputLineRowID)
		if !ok || int(id) != i {
			t.Errorf("rows[2+%d].ID = %#v, want errorOutputLineRowID(%d)", i, r.ID, i)
		}
	}

	// The long [ERROR]: paragraph must have actually wrapped into more
	// than one row - the whole reason WrapText exists here at all.
	joined := ""
	for _, r := range rows[2:] {
		joined += r.Text
	}
	if joined == "" {
		t.Fatal("no body text produced at all")
	}

	// The short caret-pointer line's exact leading whitespace must
	// survive verbatim (escaped, not reflowed) - the actual motivating
	// case for WrapText's own "leave short lines untouched" rule.
	wantCaret := tview.Escape("        ^ column 7")
	found := false
	for _, r := range rows[2:] {
		if r.Text == wantCaret {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("caret-pointer line %q not found verbatim (escaped) among rows", wantCaret)
	}
}

// TestErrorOutputRows_EscapesLiteralBrackets confirms row text goes
// through tview.Escape - without it, a literal "[ERROR]:" (or any other
// "[...]"-shaped stderr text) would be misparsed as a color tag by
// tview.Print, the same entry point every other row's text already goes
// through (CLAUDE.md's own "Row text still goes through tview.Print's
// same tag-parsing entry point" note).
func TestErrorOutputRows_EscapesLiteralBrackets(t *testing.T) {
	var ptr atomic.Pointer[[]string]
	lines := []string{"[ERROR]: short enough to need no wrapping"}
	ptr.Store(&lines)
	s := &liveSession{lastStderr: &ptr}

	rows := s.errorOutputRows(78)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want exactly 3 (divider + heading + one body line)", len(rows))
	}
	want := tview.Escape(lines[0])
	if rows[2].Text != want {
		t.Errorf("rows[2].Text = %q, want %q (escaped)", rows[2].Text, want)
	}
	if rows[2].Text == lines[0] {
		t.Error("row text equals the raw, unescaped stderr line - literal '[' would be misparsed as a color tag")
	}
}
