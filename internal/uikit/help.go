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
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Context-sensitive help (design-docs/Help.md): each screen describes its
// shortcuts once, as a HelpScreen, rendered as the '?' help dialog. A
// screen's bottom bar is a separate hand-written string, defined right
// next to its HelpScreen so the two are reviewed together.

// HelpEntry is one shortcut (or a group of equivalent ones).
type HelpEntry struct {
	Keys string // as shown in the help dialog, e.g. "n / N"
	Desc string // full description for the help dialog
	// ID lets a screen leave an entry out depending on context (e.g. no
	// "re-run" in a session that can't rerun) - see HelpText's hide
	// parameter. "" for entries that always apply.
	ID string
}

// HelpSection is a titled group of entries in the help dialog.
type HelpSection struct {
	Title   string
	Entries []HelpEntry
}

// HelpScreen is one screen's complete shortcut description.
type HelpScreen struct {
	Title    string // the dialog's title, e.g. "Tree view"
	Sections []HelpSection
}

// helpMaxKeyColumn caps the key column's width, so one unusually long key
// list can't squeeze every description into a narrow strip; a longer key
// text simply pushes its own description onto the next line.
const helpMaxKeyColumn = 22

// HelpText renders h as the help dialog's content (tview-tagged): section
// titles, then a two-column key/description list with descriptions
// word-wrapped to fit width (the dialog's inner width) and continuation
// lines aligned under the description column. Entries hide reports true
// for are left out (hide may be nil), as is a section left with nothing
// to show.
func HelpText(h HelpScreen, width int, hide func(id string) bool) string {
	keyCol := 0
	for _, sec := range h.Sections {
		for _, e := range sec.Entries {
			if n := len([]rune(e.Keys)); n > keyCol && n <= helpMaxKeyColumn {
				keyCol = n
			}
		}
	}
	const gap = 2
	descWidth := width - keyCol - gap
	if descWidth < 20 {
		descWidth = 20
	}
	indent := strings.Repeat(" ", keyCol+gap)

	var b strings.Builder
	first := true
	for _, sec := range h.Sections {
		var entries []HelpEntry
		for _, e := range sec.Entries {
			if !(hide != nil && e.ID != "" && hide(e.ID)) {
				entries = append(entries, e)
			}
		}
		if len(entries) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		fmt.Fprintf(&b, "[%s::b]%s[-::-]\n", HeadingColor, tview.Escape(sec.Title))
		for _, e := range entries {
			keys := e.Keys
			descLines := WrapText(e.Desc, descWidth)
			if n := len([]rune(keys)); n > keyCol {
				// Too long for the column: keys on a line of their own.
				fmt.Fprintf(&b, "[%s]%s[-]\n", KeyHighlightColor, tview.Escape(keys))
				for _, l := range descLines {
					b.WriteString(indent + tview.Escape(l) + "\n")
				}
				continue
			}
			pad := strings.Repeat(" ", keyCol-len([]rune(keys))+gap)
			for i, l := range descLines {
				if i == 0 {
					fmt.Fprintf(&b, "[%s]%s[-]%s%s\n", KeyHighlightColor, tview.Escape(keys), pad, tview.Escape(l))
				} else {
					b.WriteString(indent + tview.Escape(l) + "\n")
				}
			}
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// HelpDialogMaxWidth is the help dialog's preferred width (design-docs/
// Help.md: narrower than the screen, so what's behind it stays partly
// visible); NewHelpDialog shrinks it on a narrower terminal.
const HelpDialogMaxWidth = 70

// NewHelpDialog builds the help dialog for h, sized for a screen of
// screenWidth x screenHeight: up to HelpDialogMaxWidth wide, as tall as its
// content needs, both capped to leave a margin around it. view is the
// scrollable content (the caller routes keys/mouse to it); modal is the
// centered primitive to show as an overlay page.
func NewHelpDialog(h HelpScreen, screenWidth, screenHeight int, hide func(id string) bool) (view *tview.TextView, modal tview.Primitive) {
	width := min(HelpDialogMaxWidth, screenWidth-4)
	width = max(width, 30)
	const chrome = 4 // border (2) + horizontal padding (2)
	text := HelpText(h, width-chrome, hide)

	lines := strings.Count(text, "\n") + 1
	height := min(lines+2, screenHeight-2) // + border
	height = max(height, 5)

	// No scrollbar in a TextView - say so in the title when there's more
	// than fits, or the rest is easy to miss.
	title := fmt.Sprintf(" Help: %s - esc to close ", h.Title)
	if lines > height-2 {
		title = fmt.Sprintf(" Help: %s - ↑/↓ to scroll, esc to close ", h.Title)
	}

	// Descriptions and the title are untagged, so they need an explicit
	// color: tview's own default is white, invisible on a light
	// [colors] background.
	view = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	view.SetTextColor(tcell.GetColor(StandardTextColor))
	view.SetText(text)
	view.SetBorder(true).SetBorderPadding(0, 0, 1, 1).
		SetBorderColor(tcell.GetColor(DialogBorderColor)).
		SetTitleColor(tcell.GetColor(StandardTextColor)).
		SetTitle(title)
	return view, CenteredModal(view, width, height)
}

// HelpOverlay is the help dialog's runtime half: shows a HelpScreen as an
// overlay page on top of pages (so the view behind it keeps updating),
// owns the keyboard and mouse while open, and hands focus back to
// whatever had it on close. One per tview.Application; every screen of
// that application opens it with its own HelpScreen.
type HelpOverlay struct {
	app         *tview.Application
	pages       *tview.Pages
	view        *tview.TextView
	returnFocus tview.Primitive
	open        bool
}

// helpPageName is the overlay's own page in pages - added last on every
// Open, so it's always drawn on top.
const helpPageName = "help"

// NewHelpOverlay returns an overlay for app, drawn into pages (the app's
// root, or at least a Pages covering the whole screen). app may be set
// later via SetApp, for callers that build their pages before their app.
func NewHelpOverlay(app *tview.Application, pages *tview.Pages) *HelpOverlay {
	return &HelpOverlay{app: app, pages: pages}
}

// SetApp sets the application, for an overlay built before it existed.
func (h *HelpOverlay) SetApp(app *tview.Application) { h.app = app }

// IsOpen reports whether the dialog is showing.
func (h *HelpOverlay) IsOpen() bool { return h.open }

// Open shows screen, rebuilt for the terminal's current size (hide drops
// entries that don't apply right now - may be nil). Reopening replaces it.
func (h *HelpOverlay) Open(screen HelpScreen, hide func(id string) bool) {
	_, _, width, height := h.pages.GetInnerRect()
	view, modal := NewHelpDialog(screen, width, height, hide)
	if !h.open {
		h.returnFocus = h.app.GetFocus()
	}
	h.view = view
	h.open = true
	h.pages.AddPage(helpPageName, modal, true, true)
	h.app.SetFocus(view)
}

// Close hides the dialog (a no-op if it isn't open) and gives focus back
// to whatever had it when Open was called.
func (h *HelpOverlay) Close() {
	if !h.open {
		return
	}
	h.open = false
	h.pages.RemovePage(helpPageName)
	if h.returnFocus != nil {
		h.app.SetFocus(h.returnFocus)
	}
}

// IsHelpKey reports whether event is one of the keys that open help: '?'
// or F1. Only F1 where '?' must stay typeable (text fields).
func IsHelpKey(event *tcell.EventKey) bool {
	return event.Key() == tcell.KeyF1 || event.Key() == tcell.KeyRune && event.Rune() == '?'
}

// HandleKey is for the app's input capture, checked first: while open,
// the dialog owns the keyboard. Esc, ?, F1 and q close it - q closes
// rather than quits, as in every other read-only overlay in this app;
// navigation keys and their usual aliases scroll it; everything else is
// swallowed. handled is false when the dialog isn't open - and for Ctrl-C,
// which closes the dialog but is left for the caller to handle as it
// always does (interrupt/quit), whichever order the caller checks in.
func (h *HelpOverlay) HandleKey(event *tcell.EventKey) (handled bool) {
	if !h.open {
		return false
	}
	if event.Key() == tcell.KeyCtrlC {
		h.Close()
		return false
	}
	if event.Key() == tcell.KeyEscape || IsHelpKey(event) || event.Key() == tcell.KeyRune && event.Rune() == 'q' {
		h.Close()
		return true
	}
	key := event.Key()
	if key == tcell.KeyRune {
		switch event.Rune() {
		case 'j':
			key = tcell.KeyDown
		case 'k':
			key = tcell.KeyUp
		case ' ':
			key = tcell.KeyPgDn
		case 'b':
			key = tcell.KeyPgUp
		case 'G', '>':
			key = tcell.KeyEnd
		case '<', 'g':
			key = tcell.KeyHome
		}
	}
	switch key {
	case tcell.KeyCtrlF:
		key = tcell.KeyPgDn
	case tcell.KeyCtrlB:
		key = tcell.KeyPgUp
	case tcell.KeyCtrlA:
		key = tcell.KeyHome
	case tcell.KeyCtrlE:
		key = tcell.KeyEnd
	}
	switch key {
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
		// Called directly rather than returned for tview's focus-driven
		// dispatch, which has proven unreliable for primitives nested in
		// this app's Pages (see session's tabSearchPanel.handleComposingKey).
		if handler := h.view.InputHandler(); handler != nil {
			handler(tcell.NewEventKey(key, 0, tcell.ModNone), func(p tview.Primitive) { h.app.SetFocus(p) })
		}
	}
	return true
}

// HandleMouse is for the app's mouse capture, checked first: while open,
// the wheel scrolls the dialog and everything else - inside it or not - is
// swallowed, so nothing behind it reacts and no click moves focus away.
// handled is false when the dialog isn't open.
func (h *HelpOverlay) HandleMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	if !h.open {
		return nil, action, false
	}
	if event == nil {
		return nil, action, true
	}
	if x, y := event.Position(); InRect(x, y, h.view) &&
		(action == tview.MouseScrollUp || action == tview.MouseScrollDown) {
		return event, action, true
	}
	return nil, action, true
}
