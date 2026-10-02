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

	view = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	view.SetText(text)
	view.SetBorder(true).SetBorderPadding(0, 0, 1, 1).
		SetBorderColor(tcell.GetColor(DialogBorderColor)).
		SetTitle(title)
	return view, CenteredModal(view, width, height)
}
