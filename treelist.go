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

package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// treeList is a minimal, purpose-built stand-in for tview.List, written
// specifically to decouple the viewport's scroll position from the cursor
// (currentItem) - something tview.List cannot do. tview.List.Draw()
// unconditionally re-clamps its scroll offset to keep currentItem visible
// on every single redraw (confirmed directly against list.go - there is no
// flag to disable this), which defeats plain mouse-wheel/trackpad panning:
// panning the viewport away from the cursor only ever lasts until the next
// redraw snaps it back. See NewLiveTUI's mouse-wheel comment for the
// approach that was tried and reverted before landing here (driving
// List's own clamp via SetCurrentItem - which moved the cursor on every
// wheel tick and, worse, could wrap it around at the ends).
//
// Deliberately minimal: this only implements the slice of List's behavior
// tui.go actually exercises today - single-line rows (this app never
// enabled List's secondary-text mode), no shortcuts, no horizontal
// scrolling (Left/Right never reach this widget at all - NewLiveTUI's
// SetInputCapture intercepts them first), and no wraparound (this app
// always ran with wraparound off). Row text is still fully tag-colored via
// the exported tview.Print, the same tag-parsing entry point List's own
// rendering uses internally, so every existing [color]...[-] row-text
// producer (playRowText/taskLabel/hostLabel) needs no changes.
type treeList struct {
	*tview.Box

	rows        []treeListRow
	currentItem int // -1 when rows is empty
	itemOffset  int // index of the first visible row - independent of
	// currentItem; see ensureVisible for the only place the two are
	// deliberately reconciled.

	changed func(index int)
}

type treeListRow struct {
	text     string
	selected func()
}

func newTreeList() *treeList {
	return &treeList{
		Box:         tview.NewBox(),
		currentItem: -1,
	}
}

// Clear removes all rows. currentItem resets to -1 (empty) - every caller
// in tui.go immediately repopulates via AddItem and then calls
// SetCurrentItem before the next Draw, mirroring how tview.List's own
// Clear()+refill dance already worked.
func (t *treeList) Clear() *treeList {
	t.rows = nil
	t.currentItem = -1
	return t
}

// AddItem appends one row. selected, if non-nil, is invoked when this row
// is activated (Enter/Space on it, or a click) - the equivalent of
// tview.List's own per-item Selected callback, minus the shortcut/
// secondary-text parameters this app never used.
func (t *treeList) AddItem(text string, selected func()) *treeList {
	t.rows = append(t.rows, treeListRow{text: text, selected: selected})
	if t.currentItem == -1 {
		t.currentItem = 0
	}
	return t
}

func (t *treeList) GetItemCount() int {
	return len(t.rows)
}

func (t *treeList) GetCurrentItem() int {
	return t.currentItem
}

// SetChangedFunc sets the function called whenever the selected index
// actually changes - via SetCurrentItem, keyboard navigation, or a click.
// Mirrors tview.List.SetChangedFunc's own contract of only firing on a
// genuine change, which tui.go's rebuild()/rebuilding-guard machinery
// already depends on (see NewLiveTUI).
func (t *treeList) SetChangedFunc(handler func(index int)) *treeList {
	t.changed = handler
	return t
}

// SetCurrentItem sets the selected row by index, clamped into range with
// no wraparound: an out-of-range index (including negative) clamps to the
// nearest end, rather than tview.List.SetCurrentItem's own "negative
// counts from the back" behavior - nothing in tui.go relies on that, and
// it's exactly what let mouse-wheel panning silently wrap around at the
// top when this app used to drive List's own SetCurrentItem from the
// wheel (see NewLiveTUI's mouse comment).
//
// The changed callback fires, and the viewport scrolls to reveal the new
// selection (ensureVisible), only when the index actually changes - a
// no-op reselection (rebuild() re-asserting the same logical row after a
// data-only change, e.g. a new host result arriving) must never yank the
// scroll position back. That guarantee is this type's entire reason for
// existing.
func (t *treeList) SetCurrentItem(index int) *treeList {
	if len(t.rows) == 0 {
		t.currentItem = -1
		return t
	}
	if index < 0 {
		index = 0
	}
	if index >= len(t.rows) {
		index = len(t.rows) - 1
	}
	if index != t.currentItem {
		t.currentItem = index
		t.ensureVisible()
		if t.changed != nil {
			t.changed(index)
		}
	}
	return t
}

// GetOffset returns the index of the first visible row.
func (t *treeList) GetOffset() int {
	return t.itemOffset
}

// SetOffset sets the index of the first visible row directly, with no
// relation to currentItem - the mechanism both revealExpandedTask and the
// mouse wheel handler use to scroll the viewport without touching
// selection at all.
func (t *treeList) SetOffset(offset int) *treeList {
	t.itemOffset = offset
	return t
}

// ensureVisible scrolls the minimum amount necessary to bring currentItem
// back into the viewport - the same two-branch clamp tview.List.Draw()
// used to apply unconditionally on every single redraw. Calling it only
// from SetCurrentItem (guarded there to only run on a genuine index
// change) and from keyboard navigation below, rather than from Draw()
// itself, is what makes plain viewport panning possible at all - the whole
// point of this type.
func (t *treeList) ensureVisible() {
	_, _, _, height := t.GetInnerRect()
	if height <= 0 {
		return
	}
	if t.currentItem < t.itemOffset {
		t.itemOffset = t.currentItem
	} else if t.currentItem-t.itemOffset >= height {
		t.itemOffset = t.currentItem - height + 1
	}
	if t.itemOffset < 0 {
		t.itemOffset = 0
	}
}

// Draw draws the visible slice of rows starting at itemOffset. Deliberately
// does NOT re-clamp itemOffset to currentItem's position here (see the
// type's own doc comment) - only bounded defensively against itemOffset
// having drifted past the end of a since-shrunk row list (e.g. collapsing
// a task removes its host rows), which would otherwise render a blank
// screen rather than snapping back to the last valid page.
func (t *treeList) Draw(screen tcell.Screen) {
	t.Box.DrawForSubclass(screen, t)

	x, y, width, height := t.GetInnerRect()
	if height <= 0 || width <= 0 {
		return
	}

	if maxOffset := len(t.rows) - 1; t.itemOffset > maxOffset {
		t.itemOffset = maxOffset
	}
	if t.itemOffset < 0 {
		t.itemOffset = 0
	}

	for i := 0; i < height && t.itemOffset+i < len(t.rows); i++ {
		tview.Print(screen, t.rows[t.itemOffset+i].text, x, y+i, width, tview.AlignLeft, tview.Styles.PrimaryTextColor)
	}
}

// activate invokes the row's own selected callback, if any - the
// equivalent of tview.List calling a listItem's Selected() function.
func (t *treeList) activate(index int) {
	if index < 0 || index >= len(t.rows) {
		return
	}
	if selected := t.rows[index].selected; selected != nil {
		selected()
	}
}

// InputHandler handles exactly the key bindings tui.go relies on reaching
// this widget: Up/Down/Home/End/PgUp/PgDn move the cursor (no wraparound),
// Enter/Space activate the current row. Left/Right, shortcuts, and
// secondary-text navigation - all real tview.List features - are omitted
// because they never reach here: NewLiveTUI's SetInputCapture intercepts
// Left/Right itself before dispatch, and this app never used shortcuts or
// secondary text.
func (t *treeList) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return t.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if len(t.rows) == 0 {
			return
		}
		_, _, _, height := t.GetInnerRect()
		switch event.Key() {
		case tcell.KeyDown:
			t.SetCurrentItem(t.currentItem + 1)
		case tcell.KeyUp:
			t.SetCurrentItem(t.currentItem - 1)
		case tcell.KeyHome:
			t.SetCurrentItem(0)
		case tcell.KeyEnd:
			t.SetCurrentItem(len(t.rows) - 1)
		case tcell.KeyPgDn:
			t.SetCurrentItem(t.currentItem + height)
		case tcell.KeyPgUp:
			t.SetCurrentItem(t.currentItem - height)
		case tcell.KeyEnter:
			t.activate(t.currentItem)
		case tcell.KeyRune:
			if event.Rune() == ' ' {
				t.activate(t.currentItem)
			}
		}
	})
}

// indexAtPoint returns the row index under screen position (x, y), or -1
// if there is none - single-line rows only, unlike List's own
// secondary-text-aware version.
func (t *treeList) indexAtPoint(x, y int) int {
	rectX, rectY, width, height := t.GetInnerRect()
	if x < rectX || x >= rectX+width || y < rectY || y >= rectY+height {
		return -1
	}
	index := t.itemOffset + (y - rectY)
	if index >= len(t.rows) {
		return -1
	}
	return index
}

// MouseHandler handles clicks (activate-then-select, matching
// tview.List's own click ordering exactly) and wheel scroll - plain,
// bounded-in-both-directions itemOffset panning that never touches
// currentItem at all. That last part is the point of this whole type: see
// NewLiveTUI's mouse-wheel comment for why driving currentItem from the
// wheel (tried first) was wrong.
func (t *treeList) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return t.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
		x, y := event.Position()
		if !t.InRect(x, y) {
			return false, nil
		}
		switch action {
		case tview.MouseLeftClick:
			setFocus(t)
			if index := t.indexAtPoint(x, y); index != -1 {
				// Activate first, then move the cursor there - the same
				// order tview.List.MouseHandler used, so a click's own
				// row-toggle/output-open effect (which may itself trigger
				// a rebuild) happens before the resulting cursor-move
				// callback fires.
				t.activate(index)
				t.SetCurrentItem(index)
			}
			consumed = true
		case tview.MouseScrollUp:
			if t.itemOffset > 0 {
				t.itemOffset--
			}
			consumed = true
		case tview.MouseScrollDown:
			_, _, _, height := t.GetInnerRect()
			if len(t.rows)-t.itemOffset > height {
				t.itemOffset++
			}
			consumed = true
		}
		return
	})
}
