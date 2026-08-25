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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestFindMatches(t *testing.T) {
	cases := []struct {
		name      string
		plain     string
		query     string
		wantCount int
	}{
		{"no query", "hello world", "", 0},
		{"no match", "hello world", "xyz", 0},
		{"one match", "hello world", "world", 1},
		{"case-insensitive", "Hello World", "world", 1},
		{"multiple matches", "foo bar foo baz foo", "foo", 3},
		{"adjacent matches don't overlap", "aaaa", "aa", 2},
		{"match containing a literal bracket", "tags: [rolestuff]", "[rolestuff]", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spans := findMatches(c.plain, c.query)
			if len(spans) != c.wantCount {
				t.Errorf("findMatches(%q, %q) found %d matches, want %d (%v)", c.plain, c.query, len(spans), c.wantCount, spans)
			}
			for _, sp := range spans {
				if got := c.plain[sp.start:sp.end]; !strings.EqualFold(got, c.query) {
					t.Errorf("span %v = %q, want a case-insensitive match of %q", sp, got, c.query)
				}
			}
		})
	}
}

func TestTextSearchEscapesNonMatchText(t *testing.T) {
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetText(tview.Escape("tags: [rolestuff] and foo"))

	s := StartTextSearch(view, "foo")
	if s.MatchCount() != 1 {
		t.Fatalf("got %d matches, want 1", s.MatchCount())
	}
	// The literal, un-matched "[rolestuff]" must come back escaped (doubled
	// closing bracket, tview.Escape's own convention) in the view's own
	// raw buffer, so it renders as a literal bracket rather than being
	// misread as a color/region tag.
	if raw := view.GetText(false); !strings.Contains(raw, tview.Escape("[rolestuff]")) {
		t.Errorf("view's raw text = %q, want the literal bracket text escaped", raw)
	}
}

func TestTextSearchRoundTripsThroughTextView(t *testing.T) {
	// Confirms tview's own parser both recognizes every region (Highlight
	// accepts each ID - it silently drops unrecognized ones, per its own
	// doc comment, so this only passes if the tags actually parsed) and
	// fully consumes the tags themselves (GetText(true) leaves no stray
	// "[...]" debris behind, the original plain text comes back exactly).
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetText(tview.Escape("foo bar foo baz"))
	s := StartTextSearch(view, "foo")

	if s.MatchCount() != 2 {
		t.Fatalf("got %d matches, want 2", s.MatchCount())
	}
	view.Highlight("match-0", "match-1")
	if got := view.GetHighlights(); len(got) != 2 {
		t.Errorf("GetHighlights() = %v, want both region IDs accepted - Highlight silently drops any it doesn't recognize", got)
	}
	if got := view.GetText(true); got != "foo bar foo baz" {
		t.Errorf("GetText(true) after a search round-trip = %q, want the original plain text back", got)
	}
}

// cellColorAt draws view onto a real (simulated) screen and returns the
// resolved foreground/background color names of the rune at the given
// column on row 0. Deliberately renders through tview's actual Draw()
// path (which is what applies TextView.Highlight's own fg/bg-swap-on-top-
// of-the-region's-existing-color-tag behavior) rather than asserting on
// the raw tagged string - a raw-string assertion on "[fg:bg]" would have
// happily passed against the exact bug live feedback caught (the current
// match's own tag was correct in isolation; it only rendered wrong once
// Highlight() inverted it a second time).
func cellColorAt(t *testing.T, view *tview.TextView, col int) (fg, bg string) {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init(): %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 3)
	view.SetRect(0, 0, 80, 3)
	view.Draw(screen)
	screen.Show()
	contents, _, _ := screen.GetContents()
	if col >= len(contents) {
		t.Fatalf("column %d out of range (screen width 80)", col)
	}
	f, b, _ := contents[col].Style.Decompose()
	return f.String(), b.String()
}

func TestTextSearchCurrentMatchColorDiffersFromOthers(t *testing.T) {
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetText(tview.Escape("foo bar foo baz foo"))

	s := StartTextSearch(view, "foo")
	if s.MatchCount() != 3 {
		t.Fatalf("got %d matches, want 3", s.MatchCount())
	}

	// Match positions: "foo"(0-2) bar "foo"(8-10) baz "foo"(16-18). Index 0
	// (columns 0-2) starts current.
	wantColor := func(t *testing.T, col int, wantFg, wantBg string) {
		t.Helper()
		fg, bg := cellColorAt(t, view, col)
		if fg != wantFg || bg != wantBg {
			t.Errorf("cell %d: fg=%s bg=%s, want fg=%s bg=%s", col, fg, bg, wantFg, wantBg)
		}
	}
	wantColor(t, 0, searchCurrentMatchFg, searchCurrentMatchBg)
	wantColor(t, 8, searchMatchFg, searchMatchBg)
	wantColor(t, 16, searchMatchFg, searchMatchBg)

	s.Next() // index 1 (columns 8-10) is now current
	wantColor(t, 0, searchMatchFg, searchMatchBg)
	wantColor(t, 8, searchCurrentMatchFg, searchCurrentMatchBg)
	wantColor(t, 16, searchMatchFg, searchMatchBg)
}

func TestStartTextSearch(t *testing.T) {
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetText(tview.Escape("one two three two one"))

	s := StartTextSearch(view, "two")
	if !s.HasMatches() {
		t.Fatal("HasMatches() = false, want true")
	}
	if got := s.MatchCount(); got != 2 {
		t.Errorf("MatchCount() = %d, want 2", got)
	}
	if got := s.CurrentMatch(); got != 1 {
		t.Errorf("CurrentMatch() = %d, want 1 (first match on start)", got)
	}
	if got := view.GetHighlights(); len(got) != 1 || got[0] != "match-0" {
		t.Errorf("GetHighlights() = %v, want [match-0]", got)
	}

	s.Next()
	if got := s.CurrentMatch(); got != 2 {
		t.Errorf("after Next(), CurrentMatch() = %d, want 2", got)
	}
	s.Next() // wraps back to the first match
	if got := s.CurrentMatch(); got != 1 {
		t.Errorf("after wrapping Next(), CurrentMatch() = %d, want 1", got)
	}
	s.Prev() // wraps back to the last match
	if got := s.CurrentMatch(); got != 2 {
		t.Errorf("after wrapping Prev(), CurrentMatch() = %d, want 2", got)
	}
}

func TestTextSearchStopRestoresOriginalContent(t *testing.T) {
	view := tview.NewTextView().SetDynamicColors(true)
	original := "[green::b]changed[-::-] to foo, twice: foo"
	view.SetText(original)

	s := StartTextSearch(view, "foo")
	if s.MatchCount() != 2 {
		t.Fatalf("got %d matches, want 2", s.MatchCount())
	}
	if got := view.GetText(false); got == original {
		t.Fatalf("view's raw text after StartTextSearch = %q, want it to differ from the original (match highlighting should have been applied)", got)
	}

	s.Stop()
	if got := view.GetText(false); got != original {
		t.Errorf("view's raw text after Stop() = %q, want the exact original %q back - including its own supplementary coloring, not just plain unhighlighted text", got, original)
	}
	if got := view.GetHighlights(); len(got) != 0 {
		t.Errorf("GetHighlights() after Stop() = %v, want none", got)
	}
}

func TestStartTextSearchNoMatches(t *testing.T) {
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetText(tview.Escape("nothing relevant here"))

	s := StartTextSearch(view, "zzz")
	if s.HasMatches() {
		t.Error("HasMatches() = true, want false")
	}
	if got := s.MatchCount(); got != 0 {
		t.Errorf("MatchCount() = %d, want 0", got)
	}
	if got := s.CurrentMatch(); got != 0 {
		t.Errorf("CurrentMatch() = %d, want 0 (no current match)", got)
	}
	// Next/Prev on an empty result set must not panic (a %-based wrap by
	// zero would).
	s.Next()
	s.Prev()
}

func TestStartTextSearchReadsAlreadyColoredText(t *testing.T) {
	// The whole point of reading back via GetText(true) rather than the
	// view's own raw buffer: a tab's normal content already carries this
	// app's own [color] tags (CLAUDE.md's "Color in the output view"), and
	// search still has to find matches inside that, not just in text that
	// happens to have no tags at all yet.
	view := tview.NewTextView().SetDynamicColors(true)
	view.SetText("[green::b]changed[-::-] to foo")

	s := StartTextSearch(view, "foo")
	if got := s.MatchCount(); got != 1 {
		t.Errorf("MatchCount() = %d, want 1 (search should see through the existing [green] tag)", got)
	}
}
