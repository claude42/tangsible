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
	"fmt"

	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// outputHintBarText is bottomBar's normal, non-search text - the drill-down
// view's own shortcut reminder. A package-level const (was a local inside
// NewLiveTUI) since newTabSearchPanel now needs it too, to restore this text
// whenever a search clears.
const outputHintBarText = " tab/shift-tab: switch tab  n/N: next/prev task  ←/→: prev/next host  /: search tab  y: copy tab  esc/enter: back  ↑/↓/j/k: navigate  CTRL-A/E: top/bottom "

// tabSearchPanel is design-docs/Search.md's "find text in the active tab"
// apparatus for the live drill-down specifically - increment 3 of the
// NewLiveTUI refactor (see the plan this session worked from). It is
// deliberately NOT internal/uikit.TabSearchBar, the equivalent type
// host.go/template.go already share: those two surfaces populate long-lived
// TextViews via SetText, while tui.go's own drill-down (like diff.go's)
// rebuilds every tab fresh on each navigation - TabSearchBar's own doc
// comment explains why that difference in shape made a shared abstraction
// less of a clean fit here. This type mirrors TabSearchBar's own shape
// anyway (same method names/behavior) rather than inventing something new,
// since it solves the identical problem.
//
// Unlike TabSearchBar, this panel does not own bottomBar/tabs outright -
// liveSession's own outputBottomBar/outputTabs fields are shared far beyond
// search (general drill-down chrome, the 'y' clipboard-copy status, the
// wider Flex layout construction in both full-screen and two-pane mode) -
// so it only holds references to them, passed in at construction. footer
// (a Pages toggling bottomBar's own Flex slot between the normal hint text
// and this panel's own input field) and input are genuinely this panel's
// own, and it builds both itself.
type tabSearchPanel struct {
	app   *tview.Application
	focus tview.Primitive // regains focus once composing closes - s.list

	tabs      *uikit.TabbedPane // shared with liveSession; ActiveTextView + tab-change clearing
	bottomBar *tview.TextView   // shared with liveSession (s.outputBottomBar)
	footer    *tview.Pages      // owned here; liveSession slots this into its own Flex layouts
	input     *tview.InputField // owned here

	// normalStyle resolves bottomBar's non-search style fresh on every
	// call (liveSession.outputBottomBarNormalStyle) - needed because
	// clearing a search has to restore bottomBar to whatever its normal
	// style currently is, not whatever it was when this panel was built;
	// see that method's own doc comment for the full "why".
	normalStyle func() tcell.Style

	active    *uikit.TextSearch // non-nil while a search is showing results
	composing bool              // true while input has focus and is being typed into
}

// newTabSearchPanel builds one. tabs is the pane search operates against;
// bottomBar is the shared status bar this panel's own footer Pages will
// swap in place of; focus is what should regain keyboard focus once the
// search prompt closes; normalStyle resolves bottomBar's own non-search
// style on demand (see the struct's own field doc).
//
// input's colors are fixed black-on-yellow regardless of the session's own
// chrome (uikit.SearchBarStyle) so composing a search always reads as "you're
// in search mode" the same way no matter the session's live/revisit/check
// chrome.
func newTabSearchPanel(app *tview.Application, tabs *uikit.TabbedPane, bottomBar *tview.TextView, focus tview.Primitive, normalStyle func() tcell.Style) *tabSearchPanel {
	p := &tabSearchPanel{app: app, tabs: tabs, bottomBar: bottomBar, focus: focus, normalStyle: normalStyle}

	p.input = tview.NewInputField().SetLabel(" Search: ")
	// SetLabelColor alone only sets the label's own foreground - confirmed
	// directly against inputfield.go: unlike SetFieldBackgroundColor/
	// SetFieldTextColor (which both compose into the same underlying
	// style), there's no SetLabelBackgroundColor to pair with it, so the
	// label's own background silently stayed at tview's default (reading
	// as black-on-black against a terminal's typical dark theme - live
	// feedback). SetLabelStyle sets both channels directly.
	p.input.SetLabelStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow))
	p.input.SetFieldBackgroundColor(tcell.ColorYellow)
	p.input.SetFieldTextColor(tcell.ColorBlack)
	p.input.SetBackgroundColor(tcell.ColorYellow)
	p.input.SetDoneFunc(p.done)

	// footer lets bottomBar's own Flex slot hold either the normal hint
	// TextView or input's real text-entry field, without ever restructuring
	// the surrounding Flex itself - the same "Pages inside one slot" idiom
	// this app already uses one level up (liveSession.pages itself,
	// switching between "main"/"output"/"split").
	p.footer = tview.NewPages().
		AddPage("hint", bottomBar, true, true).
		AddPage("search", p.input, true, false)

	// Switching tabs makes an active search irrelevant - it only ever
	// describes the tab that was active when it started, and highlights
	// would otherwise keep pointing at text the user has since navigated
	// away from. TabbedPane.SetChangedFunc fires for every way the active
	// tab can change (Tab/Backtab, and a mouse click on the tab bar too -
	// a real gap live use once caught with no clearing at all), so this
	// one registration covers all of them.
	tabs.SetChangedFunc(p.clear)

	return p
}

// open shows input in place of the normal hint bar and moves focus into it -
// pre-filled with the currently active search's own query, if there is one,
// matching the tree's own search filter dialog's "reopening shows the
// previous term" convention.
func (p *tabSearchPanel) open() {
	p.composing = true
	query := ""
	if p.active != nil {
		query = p.active.Query()
	}
	p.input.SetText(query)
	p.footer.SwitchToPage("search")
	p.app.SetFocus(p.input)
}

// closeComposing backs out of the search prompt with no change - shared by
// Ctrl-C (SetInputCapture) so it can never leave the prompt focused after an
// abort, and input's own SetDoneFunc's non-Enter cases (done, below).
func (p *tabSearchPanel) closeComposing() {
	if !p.composing {
		return
	}
	p.composing = false
	p.footer.SwitchToPage("hint")
	p.app.SetFocus(p.focus)
}

// done backs input.SetDoneFunc, which fires on Enter/Esc/Tab/Backtab -
// InputField's own fixed "done" key set. Enter starts (or replaces) a search
// against whichever tab is currently active; anything else (Esc/Tab/Backtab)
// cancels with no change - there's nothing else in this one-field prompt to
// Tab to, matching the tree's own search dialog's identical reasoning for
// the same key set.
func (p *tabSearchPanel) done(key tcell.Key) {
	if key != tcell.KeyEnter {
		p.closeComposing()
		return
	}
	p.composing = false
	query := p.input.GetText()
	if query == "" {
		p.clear()
	} else if tv, ok := p.tabs.ActiveTextView(); ok {
		p.active = uikit.StartTextSearch(tv, query)
		p.bottomBar.SetTextStyle(uikit.SearchBarStyle)
		p.bottomBar.SetText(p.statusText())
	}
	p.footer.SwitchToPage("hint")
	p.app.SetFocus(p.focus)
}

// clear drops whichever search is currently active (a no-op if none is),
// restoring bottomBar to its own normal hint text/style and the searched
// tab itself back to its own original content via TextSearch.Stop() -
// without that, the tab kept showing every match still highlighted after
// the search closed (live feedback). Harmless when the tab's content is
// about to be rebuilt fresh anyway by whatever triggered this call
// (renderOutputTabs calls this before rebuilding for exactly that reason) -
// the restore is simply superseded a moment later. Always resets bottomBar,
// even with no search active - this is also what clears a transient 'y'
// clipboard-copy status message, which has no separate lifecycle of its own
// and relies on every one of this method's own call sites (a tab switch,
// host/task navigation, closing the view) to supersede it the same way they
// already supersede a real search.
func (p *tabSearchPanel) clear() {
	p.bottomBar.SetTextStyle(p.normalStyle())
	p.bottomBar.SetText(outputHintBarText)
	p.footer.SwitchToPage("hint")
	if p.active == nil {
		return
	}
	p.active.Stop()
	p.active = nil
}

// statusText formats the active search's own "match N of M" status bar
// text. Panics if called with no active search - every caller already
// guards on that (hasMatches, or active != nil directly).
func (p *tabSearchPanel) statusText() string {
	status := "no matches"
	if p.active.HasMatches() {
		status = fmt.Sprintf("match %d of %d", p.active.CurrentMatch(), p.active.MatchCount())
	}
	return fmt.Sprintf(" Search: %s - %s   n/N: next/prev match  Esc: clear  tab/shift-tab: switch tab ", p.active.Query(), status)
}

// hasMatches reports whether there's an active search with at least one
// match - SetInputCapture's own n/N handling uses this to decide whether
// those keys step through matches instead of their normal task-hop meaning
// (an active-but-empty search falls through to task-hop, so a stale "no
// matches" search can't silently strand these keys).
func (p *tabSearchPanel) hasMatches() bool {
	return p.active != nil && p.active.HasMatches()
}

// next/prev step through matches and refresh the status bar - no-ops if
// there's no active search or it has no matches (callers are expected to
// have already checked hasMatches, but these stay safe either way).
func (p *tabSearchPanel) next() {
	if !p.hasMatches() {
		return
	}
	p.active.Next()
	p.bottomBar.SetText(p.statusText())
}

func (p *tabSearchPanel) prev() {
	if !p.hasMatches() {
		return
	}
	p.active.Prev()
	p.bottomBar.SetText(p.statusText())
}

// handleComposingKey forwards event directly to input's own InputHandler,
// bypassing tview's normal focus-driven dispatch. Confirmed live that the
// normal path silently fails here even though app.SetFocus(input) and every
// level's own HasFocus() check (input's, its ancestors', the root Pages')
// all correctly report true - input sits three tview.Pages/Flex layers deep
// (the app's root pages -> "output"/outputFlex -> this panel's own footer
// -> "search" -> input), and something in that specific dispatch chain (not
// fully isolated - tried and ruled out: TreeList's own list never retaining
// stale focus, Flex/Pages.Draw() never touching focus, TabbedPane's
// SetTabs-triggered RemovePage/AddPage focus churn once that's separately
// guarded against in renderOutputTabs) still doesn't route the event
// through, despite focus being correct at every layer checked. Invoking the
// field's own InputHandler directly sidesteps whichever part of that chain
// is at fault - the same "route around tview instead of fighting it" call
// this codebase already made once for treeList (treelist.go) when
// tview.List's own behavior fell short.
func (p *tabSearchPanel) handleComposingKey(event *tcell.EventKey) {
	if handler := p.input.InputHandler(); handler != nil {
		handler(event, func(prim tview.Primitive) { p.app.SetFocus(prim) })
	}
}
