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

// Variable pop-up (design-docs/DebugMode.md, "Mouse support"): a click on a
// word in the drill-down's Task definition tab opens a small pop-up at the
// click with the variable's name, value and origin, and a Watch button that
// adds it to the variable explorer. Mouse-only, read-only, debug mode only.
//
// It isn't a tview page: it's painted from the application's after-draw hook,
// so it never takes tview focus and Pages' focus delegation stays untouched.
// While open, handleKey/handleMouse claim every event first.

const (
	varPopupTitle      = " Variable "
	varPopupWatchLabel = " Watch "
	varPopupMaxText    = 56 // widest value/origin text; longer ones are cut
)

// varPopup is the pop-up's state; its zero value is "closed".
type varPopup struct {
	open                bool
	expr, value, origin string
	anchorX, anchorY    int // the clicked cell

	// Last painted rects (screen coordinates), for mouse hit-testing.
	boxX, boxY, boxW, boxH int
	btnX, btnY, btnW       int
}

// isWordRune reports whether r can be part of a variable word: identifier
// characters and '.', the same leniency the explorer's input field has.
func isWordRune(r rune) bool {
	return r == '_' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// wordAround returns the variable word covering column col of line (a row of
// screen cells), or "" if that cell isn't part of one. Leading and trailing
// dots are not part of the word (a sentence-ending "foo." is just "foo").
func wordAround(line []rune, col int) string {
	if col < 0 || col >= len(line) || !isWordRune(line[col]) {
		return ""
	}
	start, end := col, col
	for start > 0 && isWordRune(line[start-1]) {
		start--
	}
	for end+1 < len(line) && isWordRune(line[end+1]) {
		end++
	}
	return strings.Trim(string(line[start:end+1]), ".")
}

// varPopupPlace positions a w x h box for a click at (ax, ay) on a screen of
// sw x sh cells: just below the click, flipped above it when there's no room
// below, and shifted left or up so it never leaves the screen.
func varPopupPlace(ax, ay, w, h, sw, sh int) (x, y int) {
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

// openVarPopup opens the pop-up for a click at screen cell (x, y) if it
// landed on a defined variable in the Task definition tab; reports whether it
// opened. The word is read back from the terminal's own cells, so wrapping,
// scrolling and colour tags need no handling.
func (s *liveSession) openVarPopup(x, y int) bool {
	if !s.debugMode || !s.viewingOutput || s.screen == nil ||
		s.outputTabs.ActiveName() != "Task definition" {
		return false
	}
	tv, ok := s.outputTabs.ActiveTextView()
	if !ok {
		return false
	}
	ix, iy, iw, ih := tv.GetInnerRect()
	if x < ix || x >= ix+iw || y < iy || y >= iy+ih {
		return false
	}
	line := make([]rune, iw)
	for i := range line {
		line[i], _, _, _ = s.screen.GetContent(ix+i, y)
	}
	word := wordAround(line, x-ix)
	if word == "" {
		return false
	}
	snapshot, _ := s.watchCurrentSnapshot()
	res := evaluateWatchExpr(word, snapshot)
	if res.kind != watchResolved {
		return false // not a defined (scalar) variable: no pop-up
	}
	s.varPopup = varPopup{
		open:    true,
		expr:    word,
		value:   formatWatchScalar(res.value),
		origin:  s.watchOriginLabel(word),
		anchorX: x,
		anchorY: y,
	}
	return true
}

func (s *liveSession) closeVarPopup() {
	s.varPopup = varPopup{}
}

// varPopupWatch adds the pop-up's variable to the variable explorer (once)
// and closes the pop-up.
func (s *liveSession) varPopupWatch() {
	expr := s.varPopup.expr
	s.closeVarPopup()
	for _, e := range s.watch.entries {
		if e.expr == expr {
			return
		}
	}
	s.watch.entries = append(s.watch.entries, watchEntry{expr: expr})
	s.rebuild()
}

// drawVarPopup paints the pop-up on top of whatever tview just drew; wired as
// the application's after-draw func.
func (s *liveSession) drawVarPopup(screen tcell.Screen) {
	p := &s.varPopup
	if !p.open {
		return
	}
	sw, sh := screen.Size()
	type row struct{ label, text string }
	rows := []row{{"Name:    ", p.expr}, {"Value:   ", truncateRunes(p.value, varPopupMaxText)}}
	if p.origin != "" {
		rows = append(rows, row{"Defined: ", truncateRunesLeft(p.origin, varPopupMaxText)})
	}
	inner := len(varPopupWatchLabel)
	for _, r := range rows {
		inner = max(inner, tview.TaggedStringWidth(r.label)+len([]rune(r.text)))
	}
	w := min(inner+4, sw) // border + one column of padding each side
	h := len(rows) + 4    // border, rows, blank, button row, border
	x, y := varPopupPlace(p.anchorX, p.anchorY, w, h, sw, sh)
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
	tview.Print(screen, varPopupTitle, x+1, y, w-2, tview.AlignLeft, border)

	for i, r := range rows {
		_, lw := tview.Print(screen, r.label, x+2, y+1+i, w-4, tview.AlignLeft, label)
		tview.Print(screen, tview.Escape(r.text), x+2+lw, y+1+i, w-4-lw, tview.AlignLeft, text)
	}

	p.btnX, p.btnY, p.btnW = x+2, y+h-2, len(varPopupWatchLabel)
	btn := tcell.StyleDefault.
		Foreground(tcell.GetColor(uikit.ControlTextColor)).
		Background(tcell.GetColor(uikit.ControlBackgroundColor))
	for i, r := range varPopupWatchLabel {
		screen.SetContent(p.btnX+i, p.btnY, r, nil, btn)
	}
}

// handleVarPopupKey owns the keyboard while the pop-up is open: Esc closes,
// Enter presses Watch. Everything else is swallowed - except Ctrl-C, which
// handleKey's own handleCtrlC already claimed before this ran.
func (s *liveSession) handleVarPopupKey(event *tcell.EventKey) (*tcell.EventKey, bool) {
	if !s.varPopup.open {
		return nil, false
	}
	switch event.Key() {
	case tcell.KeyEscape:
		s.closeVarPopup()
	case tcell.KeyEnter:
		s.varPopupWatch()
	}
	return nil, true
}

// handleVarPopupMouse: while open, a left click on Watch adds the variable, a
// click elsewhere inside the pop-up does nothing, and a click outside closes
// it. Only MouseLeftClick is swallowed - tview synthesizes the click from the
// move/down/up events, and a nil return for those would kill it. Without an
// open pop-up, a click in the Task definition tab tries to open one (and
// passes through to the tab as usual).
func (s *liveSession) handleVarPopupMouse(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction, bool) {
	p := &s.varPopup
	x, y := event.Position()
	if !p.open {
		if action == tview.MouseLeftClick && s.openVarPopup(x, y) {
			return nil, tview.MouseConsumed, true // consumed: tview redraws
		}
		return nil, action, false
	}
	if action == tview.MouseScrollUp || action == tview.MouseScrollDown {
		s.closeVarPopup() // the text moves under it; let the scroll through
		return event, action, true
	}
	if action != tview.MouseLeftClick {
		return event, action, true
	}
	inBox := x >= p.boxX && x < p.boxX+p.boxW && y >= p.boxY && y < p.boxY+p.boxH
	switch {
	case y == p.btnY && x >= p.btnX && x < p.btnX+p.btnW:
		s.varPopupWatch()
	case !inBox:
		s.closeVarPopup()
	}
	return nil, tview.MouseConsumed, true
}
