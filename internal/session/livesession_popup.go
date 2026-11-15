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
	"strings"
	"unicode"

	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The click pop-up of the drill-down's Task definition tab: a small box at
// the click with labelled rows and an optional button. Two users: a
// variable (design-docs/DebugMode.md, "Mouse support"; Watch button, debug
// only) and context help for a module option, module or task keyword
// (design-docs/Context Help.md; no button). Mouse-only; Esc, or a click
// outside, closes it.
//
// It isn't a tview page: it's painted from the application's after-draw
// hook, so it never takes tview focus and Pages' focus delegation stays
// untouched. While open, handleKey/handleMouse claim every event first.

const (
	popupMaxText   = 56 // widest text column; longer lines are cut
	popupDescLines = 4  // description rows are wrapped, then capped at this
)

// popupRow is one labelled row; text may hold several lines ("\n").
type popupRow struct {
	label string
	text  string
}

// infoPopup is the pop-up's state; its zero value is "closed".
type infoPopup struct {
	open             bool
	title            string
	rows             []popupRow
	button           string // "" = no button
	onButton         func()
	anchorX, anchorY int // the clicked cell

	// Last painted rects (screen coordinates), for mouse hit-testing.
	boxX, boxY, boxW, boxH int
	btnX, btnY, btnW       int
}

// isWordRune reports whether r can be part of a word to look up: identifier
// characters and '.', the same leniency the explorer's input field has.
func isWordRune(r rune) bool {
	return r == '_' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// wordSpan returns the [start, end] columns of the word covering column col
// of line (a row of screen cells), or ok=false if that cell isn't part of
// one. Leading and trailing dots are not part of the word (a sentence-ending
// "foo." is just "foo").
func wordSpan(line []rune, col int) (start, end int, ok bool) {
	if col < 0 || col >= len(line) || !isWordRune(line[col]) {
		return 0, 0, false
	}
	start, end = col, col
	for start > 0 && isWordRune(line[start-1]) {
		start--
	}
	for end+1 < len(line) && isWordRune(line[end+1]) {
		end++
	}
	for start <= end && line[start] == '.' {
		start++
	}
	for end >= start && line[end] == '.' {
		end--
	}
	if start > end || col < start || col > end {
		return 0, 0, false
	}
	return start, end, true
}

// wordAround returns the word wordSpan finds, or "".
func wordAround(line []rune, col int) string {
	start, end, ok := wordSpan(line, col)
	if !ok {
		return ""
	}
	return string(line[start : end+1])
}

// popupPlace positions a w x h box for a click at (ax, ay) on a screen of
// sw x sh cells: just below the click, flipped above it when there's no room
// below, and shifted left or up so it never leaves the screen.
func popupPlace(ax, ay, w, h, sw, sh int) (x, y int) {
	x, y = ax, ay+1
	if x+w > sw {
		x = sw - w
	}
	if y+h > sh {
		y = ay - h
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// truncateRunes cuts s to at most n runes, ending in "…" when it cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// truncateRunesLeft is truncateRunes cutting the start instead, for a path
// whose tail (file and line) is the informative part.
func truncateRunesLeft(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[len(r)-n:])
	}
	return "…" + string(r[len(r)-n+1:])
}

// wrapText word-wraps s to width columns and keeps at most maxLines lines,
// ending the last one in "…" if text was left out. A word longer than a line
// is cut.
func wrapText(s string, width, maxLines int) string {
	var lines []string
	cur := ""
	flush := func() {
		if cur != "" {
			lines = append(lines, cur)
			cur = ""
		}
	}
	for _, word := range strings.Fields(s) {
		word = truncateRunes(word, width)
		switch {
		case cur == "":
			cur = word
		case len([]rune(cur))+1+len([]rune(word)) <= width:
			cur += " " + word
		default:
			flush()
			cur = word
		}
	}
	flush()
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		last := []rune(lines[maxLines-1])
		if len(last) >= width {
			last = last[:width-1]
		}
		lines[maxLines-1] = string(last) + "…"
	}
	return strings.Join(lines, "\n")
}

// showPopup opens p anchored at the clicked cell (x, y).
func (s *liveSession) showPopup(p infoPopup, x, y int) {
	p.open, p.anchorX, p.anchorY = true, x, y
	s.popup = p
}

func (s *liveSession) closePopup() {
	s.popup = infoPopup{}
}

// drawPopup paints the pop-up on top of whatever tview just drew; wired as
// the application's after-draw func.
func (s *liveSession) drawPopup(screen tcell.Screen) {
	p := &s.popup
	if !p.open {
		return
	}
	sw, sh := screen.Size()

	labelW := 0
	for _, r := range p.rows {
		labelW = max(labelW, tview.TaggedStringWidth(r.label))
	}
	if labelW > 0 {
		labelW++ // gap between label and text
	}
	textW, nLines := 0, 0
	for _, r := range p.rows {
		for _, l := range strings.Split(r.text, "\n") {
			textW = max(textW, len([]rune(truncateRunes(l, popupMaxText))))
			nLines++
		}
	}
	btnW := 0
	if p.button != "" {
		btnW = len(p.button)
	}
	inner := max(labelW+textW, btnW, len(p.title))
	w := min(inner+4, sw) // border + one column of padding each side
	h := nLines + 2       // border, rows, border
	if p.button != "" {
		h += 2 // blank, button row
	}
	x, y := popupPlace(p.anchorX, p.anchorY, w, h, sw, sh)
	p.boxX, p.boxY, p.boxW, p.boxH = x, y, w, h

	text := tcell.GetColor(uikit.StandardTextColor)
	label := tcell.GetColor(uikit.AnnotationColor)
	border := tcell.GetColor(uikit.DialogBorderColor)
	bg := tview.Styles.PrimitiveBackgroundColor
	base := tcell.StyleDefault.Background(bg).Foreground(text)

	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			screen.SetContent(cx, cy, ' ', nil, base)
		}
	}
	bs := base.Foreground(border)
	for cx := x + 1; cx < x+w-1; cx++ {
		screen.SetContent(cx, y, tview.Borders.Horizontal, nil, bs)
		screen.SetContent(cx, y+h-1, tview.Borders.Horizontal, nil, bs)
	}
	for cy := y + 1; cy < y+h-1; cy++ {
		screen.SetContent(x, cy, tview.Borders.Vertical, nil, bs)
		screen.SetContent(x+w-1, cy, tview.Borders.Vertical, nil, bs)
	}
	screen.SetContent(x, y, tview.Borders.TopLeft, nil, bs)
	screen.SetContent(x+w-1, y, tview.Borders.TopRight, nil, bs)
	screen.SetContent(x, y+h-1, tview.Borders.BottomLeft, nil, bs)
	screen.SetContent(x+w-1, y+h-1, tview.Borders.BottomRight, nil, bs)
	tview.Print(screen, tview.Escape(p.title), x+1, y, w-2, tview.AlignLeft, border)

	row := y + 1
	for _, r := range p.rows {
		for i, l := range strings.Split(r.text, "\n") {
			if i == 0 {
				tview.Print(screen, r.label, x+2, row, w-4, tview.AlignLeft, label)
			}
			tview.Print(screen, tview.Escape(truncateRunes(l, popupMaxText)), x+2+labelW, row, w-4-labelW, tview.AlignLeft, text)
			row++
		}
	}

	if p.button != "" {
		p.btnX, p.btnY, p.btnW = x+2, y+h-2, len(p.button)
		btn := tcell.StyleDefault.
			Foreground(tcell.GetColor(uikit.ControlTextColor)).
			Background(tcell.GetColor(uikit.ControlBackgroundColor))
		for i, r := range p.button {
			screen.SetContent(p.btnX+i, p.btnY, r, nil, btn)
		}
	}
}

// handlePopupKey owns the keyboard while the pop-up is open: Esc closes,
// Enter presses the button (or, with none, closes). Everything else is
// swallowed - except Ctrl-C, which handleKey's own handleCtrlC already
// claimed before this ran.
func (s *liveSession) handlePopupKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.popup.open {
		return nil, false
	}
	switch event.Key() {
	case tcell.KeyEscape:
		s.closePopup()
	case tcell.KeyEnter:
		s.pressPopupButton()
	}
	return nil, true
}

// pressPopupButton runs the button's action and closes the pop-up; a
// button-less pop-up just closes.
func (s *liveSession) pressPopupButton() {
	on := s.popup.onButton
	s.closePopup()
	if on != nil {
		on()
	}
}

// handlePopupMouse: while open, a left click on the button presses it, a
// click elsewhere inside the pop-up does nothing, and a click outside closes
// it. Only MouseLeftClick is swallowed - tview synthesizes the click from the
// move/down/up events, and a nil return for those would kill it. Without an
// open pop-up, a click in the Task definition tab tries to open one (and
// consumes the click if it did).
func (s *liveSession) handlePopupMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	p := &s.popup
	x, y := event.Position()
	if !p.open {
		if action == tview.MouseLeftClick && s.openContextPopup(x, y) {
			return nil, tview.MouseConsumed, true // consumed: tview redraws
		}
		return nil, action, false
	}
	if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
		s.closePopup() // the text moves under it; let the scroll through
		return event, action, true
	}
	if action != tview.MouseLeftClick {
		return event, action, true
	}
	inBox := x >= p.boxX && x < p.boxX+p.boxW && y >= p.boxY && y < p.boxY+p.boxH
	switch {
	case p.button != "" && y == p.btnY && x >= p.btnX && x < p.btnX+p.btnW:
		s.pressPopupButton()
	case !inBox:
		s.closePopup()
	}
	return nil, tview.MouseConsumed, true
}
