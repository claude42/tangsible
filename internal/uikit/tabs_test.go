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

package uikit

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func primitives(n int) []tview.Primitive {
	out := make([]tview.Primitive, n)
	for i := range out {
		out[i] = tview.NewTextView()
	}
	return out
}

func TestTabbedPaneSetTabsAndActiveName(t *testing.T) {
	p := NewTabbedPane()
	if got := p.ActiveName(); got != "" {
		t.Errorf("ActiveName() on an empty pane = %q, want \"\"", got)
	}

	p.SetTabs([]string{"Task", "Output", "Details"}, primitives(3))
	if got := p.ActiveName(); got != "Task" {
		t.Errorf("ActiveName() after SetTabs = %q, want \"Task\" (first tab)", got)
	}
}

func TestTabbedPaneSetTabsPreservesActiveByName(t *testing.T) {
	p := NewTabbedPane()
	p.SetTabs([]string{"Task", "Output", "Details"}, primitives(3))
	p.Next() // -> Output
	if got := p.ActiveName(); got != "Output" {
		t.Fatalf("ActiveName() after Next() = %q, want Output", got)
	}

	// A new tab list that still contains "Output" - e.g. navigating to a
	// different host whose task also has an Output tab - should stay on
	// Output, not silently reset to the first tab.
	p.SetTabs([]string{"Task", "Output", "Resolved", "Details"}, primitives(4))
	if got := p.ActiveName(); got != "Output" {
		t.Errorf("ActiveName() after SetTabs with Output still present = %q, want Output", got)
	}
}

func TestTabbedPaneSetTabsFallsBackWhenActiveNameGone(t *testing.T) {
	p := NewTabbedPane()
	p.SetTabs([]string{"Task", "Output", "Details"}, primitives(3))
	p.Next() // -> Output
	if got := p.ActiveName(); got != "Output" {
		t.Fatalf("ActiveName() after Next() = %q, want Output", got)
	}

	// A new tab list with no "Output" at all (e.g. the newly-selected
	// host/task has no Output content) falls back to the first tab,
	// rather than an out-of-range or stale index.
	p.SetTabs([]string{"Task", "Details"}, primitives(2))
	if got := p.ActiveName(); got != "Task" {
		t.Errorf("ActiveName() after SetTabs with Output gone = %q, want Task (fallback)", got)
	}
}

func TestTabbedPaneNextPrevWrap(t *testing.T) {
	p := NewTabbedPane()
	p.SetTabs([]string{"A", "B", "C"}, primitives(3))

	if got := p.ActiveName(); got != "A" {
		t.Fatalf("initial ActiveName() = %q, want A", got)
	}
	p.Next()
	if got := p.ActiveName(); got != "B" {
		t.Errorf("after Next() = %q, want B", got)
	}
	p.Next()
	p.Next() // A, B, C, wraps back to A
	if got := p.ActiveName(); got != "A" {
		t.Errorf("after wrapping past the last tab = %q, want A", got)
	}
	p.Prev() // wraps backward past the first tab
	if got := p.ActiveName(); got != "C" {
		t.Errorf("after Prev() wrapping past the first tab = %q, want C", got)
	}
}

func TestTabbedPaneNextPrevOnEmptyPane(t *testing.T) {
	p := NewTabbedPane()
	// Must not panic with no tabs at all.
	p.Next()
	p.Prev()
	if got := p.ActiveName(); got != "" {
		t.Errorf("ActiveName() on an empty pane after Next/Prev = %q, want \"\"", got)
	}
}

func TestTabbedPaneSetTabsPanicsOnLengthMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetTabs with mismatched lengths did not panic")
		}
	}()
	p := NewTabbedPane()
	p.SetTabs([]string{"A", "B"}, primitives(1))
}

func TestTabbedPaneHandleClick(t *testing.T) {
	p := NewTabbedPane()
	p.SetTabs([]string{"Task", "Output", "Details"}, primitives(3))
	// header is at row 0 in this pane's own root Flex; give it a rect
	// exactly as the real layout would during Draw(), without needing an
	// actual terminal/screen - Box.SetRect can be called directly.
	p.header.SetRect(0, 5, 80, 1)

	// " Task " (6 cols, 0-5), "  " gap (6-7), " Output " (8 cols, 8-15),
	// "  " gap (16-17), " Details " (9 cols, 18-26).
	cases := []struct {
		name       string
		x, y       int
		wantClick  bool
		wantActive string
	}{
		{name: "inside Task", x: 2, y: 5, wantClick: true, wantActive: "Task"},
		{name: "inside Output", x: 10, y: 5, wantClick: true, wantActive: "Output"},
		{name: "inside Details", x: 20, y: 5, wantClick: true, wantActive: "Details"},
		{name: "in the gap between tabs", x: 6, y: 5, wantClick: false, wantActive: "Task"},
		{name: "wrong row entirely", x: 2, y: 6, wantClick: false, wantActive: "Task"},
		{name: "past the last tab", x: 60, y: 5, wantClick: false, wantActive: "Task"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p.setActive(0) // reset before each sub-case
			got := p.HandleClick(c.x, c.y)
			if got != c.wantClick {
				t.Errorf("HandleClick(%d, %d) = %v, want %v", c.x, c.y, got, c.wantClick)
			}
			if active := p.ActiveName(); active != c.wantActive {
				t.Errorf("ActiveName() after HandleClick(%d, %d) = %q, want %q", c.x, c.y, active, c.wantActive)
			}
		})
	}
}

func TestTabbedPaneRenderHeaderHighlightsActiveTab(t *testing.T) {
	p := NewTabbedPane()
	p.SetTabs([]string{"Task", "Output"}, primitives(2))
	got := p.header.GetText(false)
	// Every color fully specified per-segment, not left to inherit an
	// attribute-only tag's empty fields (confirmed live that reliably
	// inheriting the header's own base TextStyle through an empty-field
	// tag like [::R] doesn't actually happen) - the active tab's own
	// segment is distinguished by swapped (navy-on-white) colors instead.
	if !strings.Contains(got, "[navy:white:B] Task [white:navy:-]") {
		t.Errorf("header text = %q, want the active tab (Task) wrapped in [navy:white:B]...[white:navy:-]", got)
	}
	if strings.Contains(got, "[navy:white:B] Output") {
		t.Errorf("header text = %q, want Output (inactive) not styled as the active tab", got)
	}
}
