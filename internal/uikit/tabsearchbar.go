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
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TabSearchBar is the shared "search whichever tab is active" footer
// apparatus design-docs/Search.md describes, for surfaces whose tabs are
// long-lived TextViews populated in place via SetText rather than rebuilt
// wholesale via TabbedPane.SetTabs on every navigation (host.go's detail
// view, template.go). tui.go's and diff.go's own drill-downs rebuild
// every tab fresh on each navigation instead, and wire this same pattern
// inline rather than through this type - see their own comments for why
// that difference in shape made a shared abstraction less of a clean fit
// there.
//
// A caller embeds Primitive() in its own footer's Flex slot in place of a
// plain TextView, wires '/' to Open and n/N/Esc to the small methods
// below from its own SetInputCapture, and calls ClearForView(v) whenever
// v's own content changes (an async fetch landing, a host switch) so a
// search can never silently go stale against text it no longer describes.
type TabSearchBar struct {
	app   *tview.Application
	tabs  *TabbedPane
	hint  string
	focus tview.Primitive // regains keyboard focus once the prompt closes

	footer *tview.TextView
	input  *tview.InputField
	pages  *tview.Pages

	search *TextSearch
	view   *tview.TextView // which TextView `search` currently targets
	compo  bool
}

// NewTabSearchBar builds one. tabs is the pane search operates against
// (via ActiveTextView); hint is the bar's own normal, non-search text;
// focus is what should regain keyboard focus once the search prompt
// closes.
func NewTabSearchBar(app *tview.Application, tabs *TabbedPane, hint string, focus tview.Primitive) *TabSearchBar {
	b := &TabSearchBar{app: app, tabs: tabs, hint: hint, focus: focus}

	b.footer = tview.NewTextView().SetText(hint)
	b.footer.SetTextStyle(BarStyle)

	b.input = tview.NewInputField().SetLabel(" Search: ")
	// SetLabelColor alone only sets the label's own foreground - see
	// tui.go's identically-shaped setup for the full story on why
	// SetLabelStyle (both channels) is needed instead.
	b.input.SetLabelStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow))
	b.input.SetFieldBackgroundColor(tcell.ColorYellow)
	b.input.SetFieldTextColor(tcell.ColorBlack)
	b.input.SetBackgroundColor(tcell.ColorYellow)
	b.input.SetDoneFunc(b.done)

	b.pages = tview.NewPages().
		AddPage("hint", b.footer, true, true).
		AddPage("search", b.input, true, false)

	// Switching tabs makes an active search irrelevant - it only ever
	// describes the tab that was active when it started, and highlights
	// would otherwise keep pointing at text the user has since navigated
	// away from. TabbedPane.SetChangedFunc fires for every way the active
	// tab can change (Next/Prev, and - the gap live use actually caught -
	// a mouse click on the tab bar too), so this one registration covers
	// all of them.
	tabs.SetChangedFunc(b.Clear)

	return b
}

// Primitive is what the caller embeds in its own footer's Flex slot.
func (b *TabSearchBar) Primitive() tview.Primitive { return b.pages }

// IsComposing reports whether the search prompt currently has focus.
func (b *TabSearchBar) IsComposing() bool { return b.compo }

// HasActive reports whether a search is currently showing results (as
// opposed to being composed, or not started at all).
func (b *TabSearchBar) HasActive() bool { return b.search != nil }

// HandleComposingKey forwards event directly to the input field's own
// InputHandler, bypassing tview's normal focus-driven dispatch - see
// tui.go's identically-purposed SetInputCapture branch for the full story
// on why that's necessary (confirmed live: a primitive nested this many
// Pages/Flex layers deep doesn't reliably receive forwarded key events via
// root.HasFocus() alone, even when every level's own HasFocus() correctly
// reports true).
func (b *TabSearchBar) HandleComposingKey(event *tcell.EventKey) {
	if handler := b.input.InputHandler(); handler != nil {
		handler(event, func(p tview.Primitive) { b.app.SetFocus(p) })
	}
}

// Open shows the search prompt and moves focus into it, pre-filled with
// the active search's own query if there is one - the same "reopening
// shows the previous term" convention the main tree's own search filter
// dialog already follows.
func (b *TabSearchBar) Open() {
	b.compo = true
	query := ""
	if b.search != nil {
		query = b.search.Query()
	}
	b.input.SetText(query)
	b.pages.SwitchToPage("search")
	b.app.SetFocus(b.input)
}

// CloseComposing backs out of the prompt with no change - Esc/Tab/Backtab
// on the input field, or an external abort (Ctrl-C).
func (b *TabSearchBar) CloseComposing() {
	if !b.compo {
		return
	}
	b.compo = false
	b.pages.SwitchToPage("hint")
	b.app.SetFocus(b.focus)
}

func (b *TabSearchBar) done(key tcell.Key) {
	if key != tcell.KeyEnter {
		b.CloseComposing()
		return
	}
	b.compo = false
	query := b.input.GetText()
	if query == "" {
		b.Clear()
	} else if tv, ok := b.tabs.ActiveTextView(); ok {
		b.search = StartTextSearch(tv, query)
		b.view = tv
		b.footer.SetTextStyle(SearchBarStyle)
		b.footer.SetText(b.statusText())
	}
	b.pages.SwitchToPage("hint")
	b.app.SetFocus(b.focus)
}

func (b *TabSearchBar) statusText() string {
	status := "no matches"
	if b.search.HasMatches() {
		status = fmt.Sprintf("match %d of %d", b.search.CurrentMatch(), b.search.MatchCount())
	}
	return fmt.Sprintf(" /%s - %s   n/N: next/prev match  Esc: clear  tab/shift-tab: switch tab ", b.search.Query(), status)
}

// Clear drops the active search (a no-op if none), restoring the footer
// to its normal hint text/style.
func (b *TabSearchBar) Clear() {
	if b.search == nil {
		return
	}
	b.search.Stop() // undoes the match-highlight-only rendering, restoring
	// the tab's own original content - without this, clearing a search
	// left every match still visibly highlighted (live feedback).
	b.search = nil
	b.view = nil
	b.footer.SetTextStyle(BarStyle)
	b.footer.SetText(b.hint)
	b.pages.SwitchToPage("hint")
}

// ClearForView clears the active search only if it targets view - for a
// caller to invoke whenever some tab's own content changes (an async
// fetch landing, a host switch), so a search never silently goes stale
// against text it no longer describes, without clearing one scoped to a
// different, unaffected tab.
func (b *TabSearchBar) ClearForView(view *tview.TextView) {
	if b.search != nil && b.view == view {
		b.Clear()
	}
}

// Next/Prev step through matches, wrapping at either end (TextSearch's
// own behavior) - no-ops if there's no active search or it has no
// matches.
func (b *TabSearchBar) Next() {
	if b.search == nil || !b.search.HasMatches() {
		return
	}
	b.search.Next()
	b.footer.SetText(b.statusText())
}

func (b *TabSearchBar) Prev() {
	if b.search == nil || !b.search.HasMatches() {
		return
	}
	b.search.Prev()
	b.footer.SetText(b.statusText())
}
