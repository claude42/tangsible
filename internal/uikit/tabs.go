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

// Implements the shared tabbed-content component design-docs/Tabbed UI.md
// asks for - used by both the output drill-down view and the template
// page. Unlike treelist.go's treeList, this doesn't need a from-scratch
// tview.Primitive: tview has no built-in tabs widget (checked directly
// against its own source, same as when a filterable-list widget was
// looked into separately), but tview.Pages already *is* the "switch
// between named content panels" building block, and it's already used
// throughout this app. What's actually missing is just the tab-bar header
// itself (a TextView using the same inline [color]...[-] tag convention
// already used everywhere else) plus the glue to switch pages,
// re-highlight the active tab, and hit-test a mouse click against the
// header row - all composable from existing primitives, with tab
// switching driven the same way this app already handles every other
// key/mouse override: at the Application level (SetInputCapture/
// SetMouseCapture), not inside a custom widget's own InputHandler/
// MouseHandler.
package uikit

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TabbedPane is a small, reusable "named content panels with a tab bar
// header" component. root is what a caller actually adds to its own
// layout; Next/Prev/HandleClick/SetTabs are the only things a caller
// needs to drive it.
type TabbedPane struct {
	header          *tview.TextView
	pages           *tview.Pages
	root            *tview.Flex
	names           []string
	active          int
	changed         func()
	headerColorName string // renderHeader's own tag-name equivalent of
	// header's current base style - "navy" by default, matching BarStyle
	// (see NewTabbedPane) - see SetHeaderStyle's own doc comment for why
	// this needs to be kept alongside a real tcell.Style rather than
	// derived from one.
}

// SetChangedFunc registers f to be called whenever the active tab changes,
// for any reason (Next/Prev/HandleClick/SetTabs's own by-name active-tab
// resolution) - design-docs/Search.md's own "switching tabs implicitly
// clears an active in-tab search" requirement is why this exists: one
// registration per caller covers every way the active tab can change,
// rather than each call site (keyboard Tab/Backtab, mouse click) having to
// remember to clear it individually - a real gap live use caught, since
// mouse-click tab switching had no such call at all before this existed.
// Harmless to fire on SetTabs's own initial call too (nothing to clear
// yet, callers register this after building whatever it would clear).
func (p *TabbedPane) SetChangedFunc(f func()) {
	p.changed = f
}

// NewTabbedPane constructs an empty pane - call SetTabs before it has
// anything to show.
func NewTabbedPane() *TabbedPane {
	header := tview.NewTextView().SetDynamicColors(true)
	header.SetTextStyle(BarStyle)

	pages := tview.NewPages()

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(pages, 0, 1, true)

	return &TabbedPane{header: header, pages: pages, root: root, headerColorName: "navy"}
}

// SetHeaderStyle updates the tab bar's own chrome to match whichever of
// BarStyle/ReplayBarStyle/CheckBarStyle/DiffChromeStyle (or a session's
// own current pick among them - see internal/session/tui.go's
// chromeStyle/chromeColorName, internal/diff/diff.go's DiffChromeStyle)
// the surrounding session is actually using right now, and re-renders
// immediately so a call after tabs already exist takes effect without
// waiting for the next SetTabs/Next/Prev. Callers that never call this at
// all keep the default navy/BarStyle look renderHeader has always had -
// this exists for the sessions that switch chrome (revisit's purple,
// check mode's olive, the diff verb's fuchsia), not a requirement on
// every caller.
//
// Takes both style and colorName, not just one: style sets header's own
// base TextStyle (the trailing blank space past the last tab's own label,
// which renderHeader's own explicit per-tab tags don't cover), while
// colorName is what actually gets baked into those tags themselves -
// [<colorName>:white:B] for the active tab, [white:<colorName>:-] for
// every inactive one - the same "a single tcell.Style can't vary
// per-column, so the fill/tag text bakes in a tag-name string instead"
// reasoning topBar's own ComposeTopBarLine/chromeColorName already
// established for the exact same class of problem.
func (p *TabbedPane) SetHeaderStyle(style tcell.Style, colorName string) {
	p.headerColorName = colorName
	p.header.SetTextStyle(style)
	p.renderHeader()
}

// Primitive returns the pane's own root, for embedding in a caller's
// layout - the pane itself is not a tview.Primitive (see this file's own
// doc comment for why that's deliberate).
func (p *TabbedPane) Primitive() tview.Primitive {
	return p.root
}

// SetTabs replaces the pane's own tab list wholesale - names (also used
// as each tab's own Pages page name, so they must be unique) paired 1:1
// with content primitives. Preserves the active tab by name if it's still
// present in the new list, falling back to index 0 otherwise - callers
// rebuild the tab list whenever the underlying content changes (e.g. the
// drill-down's own dynamic tab visibility as the user navigates between
// hosts/tasks), and the active tab shouldn't visibly jump back to the
// first one just because the set of tabs available elsewhere changed;
// staying on e.g. "Output" while stepping through several hosts in
// sequence is the more useful default.
func (p *TabbedPane) SetTabs(names []string, content []tview.Primitive) {
	if len(names) != len(content) {
		panic("tabbedPane.SetTabs: names and content must be the same length")
	}

	prevActiveName := ""
	if p.active >= 0 && p.active < len(p.names) {
		prevActiveName = p.names[p.active]
	}

	// tview.Pages has no Clear() (unlike List/treeList) - each previously
	// tracked name has to be removed individually first.
	for _, name := range p.names {
		p.pages.RemovePage(name)
	}
	for i, name := range names {
		p.pages.AddPage(name, content[i], true, i == 0)
	}
	p.names = names

	newActive := 0
	for i, name := range names {
		if name == prevActiveName {
			newActive = i
			break
		}
	}
	p.setActive(newActive)
}

// ActiveName returns the currently active tab's own name, or "" if there
// are no tabs at all.
func (p *TabbedPane) ActiveName() string {
	if p.active < 0 || p.active >= len(p.names) {
		return ""
	}
	return p.names[p.active]
}

// ActiveTextView returns the currently active tab's own content as a
// *tview.TextView, and whether that succeeded - false if there are no
// tabs, or the active one isn't a *tview.TextView. Every tab at every
// current call site is one, but this doesn't assume that structurally;
// SetTabs itself only ever hands content straight to p.pages (there's no
// separate field retaining it), so this reads it back the same way
// anything else asks tview.Pages what's registered under a page name -
// design-docs/Search.md's own reason this exists: a search controller
// driven by tab position, not by every caller re-deriving which widget
// that is from its own locally-held slice.
func (p *TabbedPane) ActiveTextView() (*tview.TextView, bool) {
	name := p.ActiveName()
	if name == "" {
		return nil, false
	}
	tv, ok := p.pages.GetPage(name).(*tview.TextView)
	return tv, ok
}

// Next/Prev switch to the next/previous tab, wrapping at either end - a
// small, fixed-size tab bar (unlike the main tree's own potentially many
// rows) is exactly the case where wraparound is the expected, natural
// gesture (the same way Ctrl-Tab cycles in a browser), unlike the tree's
// own deliberate no-wraparound row navigation.
func (p *TabbedPane) Next() { p.setActive(p.active + 1) }
func (p *TabbedPane) Prev() { p.setActive(p.active - 1) }

func (p *TabbedPane) setActive(index int) {
	if len(p.names) == 0 {
		p.active = 0
		p.header.SetText("")
		return
	}
	if index < 0 {
		index = len(p.names) - 1
	}
	if index >= len(p.names) {
		index = 0
	}
	p.active = index
	p.pages.SwitchToPage(p.names[index])
	p.renderHeader()
	if p.changed != nil {
		p.changed()
	}
}

// renderHeader draws every tab name in order, one space of padding on
// each side, two spaces of gap between tabs - the active tab's own
// padded label wrapped in [::R] (reverse video - tview's own uppercase
// attribute-flag letters, confirmed against its own tag parser; not to be
// confused with a lowercase 'r', which isn't a recognized flag at all) so
// it renders in a different, inverted style per design-docs/Tabbed UI.md.
// HandleClick's own hit-testing math below must stay in exact lockstep
// with the padding/gap widths used here.
func (p *TabbedPane) renderHeader() {
	var b strings.Builder
	for i, name := range p.names {
		if i > 0 {
			b.WriteString("  ")
		}
		if i == p.active {
			// Explicit <headerColorName>-on-white (the session's own
			// current chrome color, white-on-<name> swapped - see
			// SetHeaderStyle) rather than relying on [::R]'s empty color
			// fields to inherit the header's base TextStyle - confirmed
			// live that inheritance doesn't reliably happen the way a
			// bare attribute-only tag might suggest, so every segment
			// here fully specifies its own colors instead, active and
			// inactive alike, leaving nothing to inherit at all.
			fmt.Fprintf(&b, "[%s:white:B] %s [white:%s:-]", p.headerColorName, tview.Escape(name), p.headerColorName)
		} else {
			fmt.Fprintf(&b, "[white:%s:-] %s [white:%s:-]", p.headerColorName, tview.Escape(name), p.headerColorName)
		}
	}
	p.header.SetText(b.String())
}

// tabWidth is the on-screen column width of tab i's own label, per
// renderHeader's own " name " padding - shared by renderHeader's own
// implicit layout and HandleClick's hit-testing so the two can never
// drift apart.
func (p *TabbedPane) tabWidth(i int) int {
	return len([]rune(p.names[i])) + 2
}

// HandleClick reports whether (x, y) falls within this pane's own tab-bar
// row and, if so, switches to whichever tab was clicked and returns true
// (event consumed). Callers (via app.SetMouseCapture) should let the
// event through normally when this returns false, so it still reaches
// e.g. the active tab's own content for scroll-wheel handling.
func (p *TabbedPane) HandleClick(x, y int) bool {
	hx, hy, hw, hh := p.header.GetRect()
	if y < hy || y >= hy+hh || x < hx || x >= hx+hw {
		return false
	}
	col := hx
	for i := range p.names {
		if i > 0 {
			col += 2 // the "  " gap renderHeader writes between tabs
		}
		w := p.tabWidth(i)
		if x >= col && x < col+w {
			p.setActive(i)
			return true
		}
		col += w
	}
	return false
}
