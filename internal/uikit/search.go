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

// Implements the shared "find text in this tab" component
// design-docs/Search.md asks for - used by the drill-down view, the diff
// verb's own drill-down, the host detail view, and the template view,
// every one of which already shows its content as a *tview.TextView (see
// TabbedPane's own doc comment). Deliberately built on tview's own
// region/highlight primitives (SetRegions/Highlight/ScrollToHighlight),
// confirmed directly against tview's source to already do exactly this -
// no need to hand-roll a widget the way treelist.go's TreeList had to,
// since that one really did lack a capability tview.List never offered.
package uikit

import (
	"fmt"
	"strings"

	"github.com/rivo/tview"
)

// searchMatchFg/searchMatchBg color every match that isn't the current
// one; searchCurrentMatchFg/Bg colors the current one - a deliberately
// different hue (orange, not yellow) rather than leaning on tview's own
// Highlight()-driven automatic color-inversion, which would only ever
// invert whatever color a region's surrounding text already happened to
// have, not read as a consistent "this one is current" signal on its own
// - live feedback on an earlier version (plain automatic inversion) was
// that it looked "a bit weird." Both pairs picked deliberately outside
// colorTag's own green/yellow/teal/red/maroon outcome palette's usual
// meaning even though "yellow" itself is reused - this is chrome/
// highlight background, not outcome foreground text, a different enough
// visual pattern that the two aren't likely to be mistaken for each
// other in practice.
const (
	searchMatchFg = "black"
	searchMatchBg = "yellow"

	searchCurrentMatchFg = "black"
	searchCurrentMatchBg = "orange"
)

// matchSpan is one match's byte offsets into TextSearch's own cached
// plain text.
type matchSpan struct {
	start, end int
}

// TextSearch drives one "find text in this tab" session against a single,
// already-displayed *tview.TextView. Deliberately does not try to
// reinsert match markers into the view's own already-tagged text (a
// Diff tab's line colors, Task's YAML key highlighting) - that would need
// a hand-rolled parser to correctly walk tview's own tag grammar without
// corrupting it, a real risk for something that would run on arbitrary
// command output. Instead, StartTextSearch reads the view's own text back
// via GetText(true) - tview's own real tag-stripping, not a
// reimplementation - and redraws the tab from that plain text with match
// regions wound around it. This is a deliberate, documented
// simplification: while a search is active, a tab's own supplementary
// coloring is temporarily not shown, only match highlighting is. The
// view's own original tagged text (before stripping) is cached too
// (original, below) specifically so Stop() can put it back exactly as it
// was - restoring supplementary coloring included - rather than leaving
// every match still visibly highlighted once the search itself has
// closed (a real gap live feedback caught: a caller resetting its own
// surrounding chrome, e.g. the footer bar, on Esc doesn't by itself undo
// anything render() did to the tab's own content).
//
// The plain text and every match's own byte span are found once, up
// front, and cached (plain/matches) rather than re-derived from the view
// on every step - render (below) re-runs on every Next/Prev call, since
// which match is "current" changes which one gets the distinct orange
// color, and re-deriving spans via GetText(true) each time would be both
// needless work and (if the view's own content had somehow changed
// underneath, which it shouldn't while a search is active - see the
// "does not survive" rule above) a source of drift between what render()
// draws and what CurrentMatch()/MatchCount() report.
type TextSearch struct {
	view     *tview.TextView
	query    string
	original string // view's own tagged text exactly as it was when the search began - see Stop's own doc comment
	plain    string
	matches  []matchSpan
	current  int // index into matches; -1 if there are none
}

// StartTextSearch begins a search of view's own currently-displayed text
// for query (case-insensitive substring - this app's one existing
// convention for text matching, shared with the main tree's own Contents
// filter, Filters.md - not a second, differently-behaving rule). Enables
// regions on view, replaces its content with the match-highlighted
// version, and jumps to the first match if there is one. An empty query
// clears back to plain, unhighlighted (but still escaped) text.
func StartTextSearch(view *tview.TextView, query string) *TextSearch {
	view.SetRegions(true)
	original := view.GetText(false)
	plain := view.GetText(true)
	s := &TextSearch{view: view, query: query, original: original, plain: plain, matches: findMatches(plain, query), current: -1}
	if len(s.matches) > 0 {
		s.current = 0
	}
	s.render()
	return s
}

// Stop restores view to its own original, pre-search content, undoing
// render()'s own match-highlight-only rendering - including whatever
// supplementary coloring the tab had before the search began (a Diff
// tab's line colors, Task's key-highlighting - the very thing "while a
// search is active, this isn't shown" gives up temporarily, see this
// type's own doc comment). A caller should call this whenever a search is
// explicitly ending (Esc/cleared) - live feedback on an earlier version
// caught that clearing a search reset the surrounding chrome (the footer
// bar) but left the tab itself still showing every match highlighted,
// since nothing had ever told the view to go back to what it looked like
// before. Harmless, if slightly redundant, to call when the view's
// content is about to be overwritten some other way regardless (a
// navigation, an async fetch landing) - that overwrite happens right
// after this in every real call site.
func (s *TextSearch) Stop() {
	s.view.Highlight()
	s.view.SetText(s.original)
}

// Query returns the search term this session was started with - so a
// caller reopening the search prompt on an already-active session can
// pre-fill it, the same "reopening shows the previous term" convention
// the main tree's own search filter dialog already follows.
func (s *TextSearch) Query() string { return s.query }

// HasMatches reports whether the query matched anything at all.
func (s *TextSearch) HasMatches() bool { return len(s.matches) > 0 }

// MatchCount is the total number of matches found.
func (s *TextSearch) MatchCount() int { return len(s.matches) }

// CurrentMatch is the 1-based index of the currently highlighted match, or
// 0 if there are none - "match X of Y" wants 1-based, not a raw slice
// index into matches.
func (s *TextSearch) CurrentMatch() int { return s.current + 1 }

// Next/Prev step to the next/previous match, wrapping at either end - the
// same "small, bounded content, wraparound is the natural gesture"
// reasoning TabbedPane.Next/Prev already applies to cycling tabs, unlike
// the main tree's own deliberate no-wraparound row navigation. No-ops if
// there are no matches.
func (s *TextSearch) Next() { s.step(1) }
func (s *TextSearch) Prev() { s.step(-1) }

func (s *TextSearch) step(delta int) {
	if len(s.matches) == 0 {
		return
	}
	s.current = (s.current + delta + len(s.matches)) % len(s.matches)
	s.render()
}

// render redraws the view from the cached plain text, wrapping every
// match in its own region tag (for Highlight/ScrollToHighlight) plus a
// color tag - orange for whichever one is current, yellow for every
// other - and escaping everything else. Runs on every call, not just the
// first, specifically so the current match's own distinct color moves
// with it on each Next/Prev rather than being expressed only through
// tview's own transient highlight state.
func (s *TextSearch) render() {
	var b strings.Builder
	pos := 0
	for i, m := range s.matches {
		b.WriteString(tview.Escape(s.plain[pos:m.start]))
		fg, bg := searchMatchFg, searchMatchBg
		if i == s.current {
			// Pre-swapped, deliberately: Highlight() below renders its
			// target region with fg/bg swapped *on top of* whatever color
			// tag the region already carries - confirmed directly against
			// a live render, not just inferred from its doc comment
			// ("background and foreground colors swapped"). Tagging the
			// current match with the swapped pair here is what makes it
			// actually render as searchCurrentMatchFg-on-
			// searchCurrentMatchBg once Highlight() does its own
			// inversion - tagging it with the pair un-swapped (the naive
			// approach) double-inverts it instead, which is what an
			// earlier version of this did (live feedback: the current
			// match rendered "yellow on black," not the intended orange
			// block).
			fg, bg = searchCurrentMatchBg, searchCurrentMatchFg
		}
		id := fmt.Sprintf("match-%d", i)
		fmt.Fprintf(&b, "[%q][%s:%s]%s[-:-:-][\"\"]", id, fg, bg, tview.Escape(s.plain[m.start:m.end]))
		pos = m.end
	}
	b.WriteString(tview.Escape(s.plain[pos:]))
	s.view.SetText(b.String())

	if s.current >= 0 {
		s.view.Highlight(fmt.Sprintf("match-%d", s.current))
		s.view.ScrollToHighlight()
	}
}

// findMatches scans plain for every case-insensitive, non-overlapping
// occurrence of query, returning their byte spans in document order. An
// empty query matches nothing (there's no dedicated "show all" reading
// the way the main tree's own empty-search filter has - an empty tab-
// search query just means nothing is highlighted yet).
//
// Matching scans fixed-length byte windows of plain with strings.EqualFold
// rather than case-folding the whole string up front and searching that:
// case-folding can change a substring's own byte length for some non-ASCII
// scripts, which would make offsets found in a folded copy land wrong in
// the original. Scanning byte windows of the original directly sidesteps
// that entirely, at the cost of only being exactly correct for equal-byte-
// length case folds (everything ASCII, effectively all of this app's own
// real output) - a documented, deliberately narrow heuristic, the same
// "correct for what this app actually shows, not chased further" style as
// taskLabel's own truncation or primaryOutputField's stdout-vs-msg choice.
func findMatches(plain, query string) []matchSpan {
	if query == "" {
		return nil
	}
	n := len(query)
	var spans []matchSpan
	for i := 0; i+n <= len(plain); i++ {
		if !strings.EqualFold(plain[i:i+n], query) {
			continue
		}
		spans = append(spans, matchSpan{start: i, end: i + n})
		i += n - 1 // skip past this match - no overlapping matches
	}
	return spans
}
