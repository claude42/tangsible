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
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// design-docs/TESTING.md's own Phase 7: the selection/scroll bookkeeping
// underneath TreeList's Draw/InputHandler/MouseHandler is plain Go with no
// tcell.Screen involved - SetRect alone is enough to give GetInnerRect a
// real height for ensureVisible's math. Draw/activate/indexAtPoint stay
// untested directly (trivial, and exercised indirectly via MouseHandler/
// InputHandler below); a real tcell.Screen is only needed for Draw itself,
// which this file deliberately doesn't test - same call TESTING.md makes.

func TestTreeListEmpty(t *testing.T) {
	l := NewTreeList()
	if got := l.GetItemCount(); got != 0 {
		t.Fatalf("GetItemCount() = %d, want 0", got)
	}
	if got := l.GetCurrentItem(); got != -1 {
		t.Fatalf("GetCurrentItem() = %d, want -1", got)
	}
}

func TestTreeListAddItemSetsInitialCurrent(t *testing.T) {
	l := NewTreeList()
	l.AddItem("first", nil)
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("GetCurrentItem() after first AddItem = %d, want 0", got)
	}
	l.AddItem("second", nil)
	if got := l.GetItemCount(); got != 2 {
		t.Fatalf("GetItemCount() = %d, want 2", got)
	}
	// A second AddItem must not disturb an already-set current item.
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("GetCurrentItem() after second AddItem = %d, want 0", got)
	}
}

func TestTreeListClearResetsCurrentItem(t *testing.T) {
	l := NewTreeList()
	l.AddItem("a", nil)
	l.AddItem("b", nil)
	l.SetCurrentItem(1)
	l.Clear()
	if got := l.GetItemCount(); got != 0 {
		t.Fatalf("GetItemCount() after Clear = %d, want 0", got)
	}
	if got := l.GetCurrentItem(); got != -1 {
		t.Fatalf("GetCurrentItem() after Clear = %d, want -1", got)
	}
}

func TestTreeListSetCurrentItemClampsNoWraparound(t *testing.T) {
	l := NewTreeList()
	for _, s := range []string{"a", "b", "c"} {
		l.AddItem(s, nil)
	}
	cases := []struct {
		name  string
		index int
		want  int
	}{
		{"negative clamps to 0, not wraparound", -1, 0},
		{"far negative still clamps to 0", -100, 0},
		{"in range", 1, 1},
		{"past the end clamps to last index", 100, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l.SetCurrentItem(c.index)
			if got := l.GetCurrentItem(); got != c.want {
				t.Errorf("SetCurrentItem(%d) -> GetCurrentItem() = %d, want %d", c.index, got, c.want)
			}
		})
	}
}

func TestTreeListSetCurrentItemOnEmptyList(t *testing.T) {
	l := NewTreeList()
	l.SetCurrentItem(5)
	if got := l.GetCurrentItem(); got != -1 {
		t.Fatalf("GetCurrentItem() on empty list = %d, want -1", got)
	}
}

func TestTreeListChangedFuncFiresOnlyOnGenuineChange(t *testing.T) {
	l := NewTreeList()
	for _, s := range []string{"a", "b", "c"} {
		l.AddItem(s, nil)
	}
	var calls []int
	l.SetChangedFunc(func(index int) {
		calls = append(calls, index)
	})

	l.SetCurrentItem(0) // already 0 (AddItem's own initial value) - no-op
	if len(calls) != 0 {
		t.Fatalf("changed fired on a no-op SetCurrentItem(0): %v", calls)
	}

	l.SetCurrentItem(2)
	if len(calls) != 1 || calls[0] != 2 {
		t.Fatalf("changed calls = %v, want [2]", calls)
	}

	l.SetCurrentItem(2) // same index again - must not re-fire
	if len(calls) != 1 {
		t.Fatalf("changed fired again on a repeated SetCurrentItem(2): %v", calls)
	}

	// Clamped requests that land back on the current index must not fire
	// either - SetCurrentItem(100) clamps to 2, which is already current.
	l.SetCurrentItem(100)
	if len(calls) != 1 {
		t.Fatalf("changed fired on a clamped-to-same-index request: %v", calls)
	}
}

func TestTreeListRestoreCurrentItemDoesNotFireChanged(t *testing.T) {
	l := NewTreeList()
	for _, s := range []string{"a", "b", "c"} {
		l.AddItem(s, nil)
	}
	fired := false
	l.SetChangedFunc(func(index int) {
		fired = true
	})

	l.RestoreCurrentItem(2)
	if fired {
		t.Fatal("RestoreCurrentItem must never invoke the changed callback")
	}
	if got := l.GetCurrentItem(); got != 2 {
		t.Fatalf("GetCurrentItem() after RestoreCurrentItem(2) = %d, want 2", got)
	}

	// Clamps the same way SetCurrentItem does.
	l.RestoreCurrentItem(-5)
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("RestoreCurrentItem(-5) -> GetCurrentItem() = %d, want 0", got)
	}
	l.RestoreCurrentItem(50)
	if got := l.GetCurrentItem(); got != 2 {
		t.Fatalf("RestoreCurrentItem(50) -> GetCurrentItem() = %d, want 2", got)
	}
}

func TestTreeListRestoreCurrentItemOnEmptyList(t *testing.T) {
	l := NewTreeList()
	l.RestoreCurrentItem(3)
	if got := l.GetCurrentItem(); got != -1 {
		t.Fatalf("GetCurrentItem() on empty list = %d, want -1", got)
	}
}

func TestTreeListOffset(t *testing.T) {
	l := NewTreeList()
	if got := l.GetOffset(); got != 0 {
		t.Fatalf("GetOffset() on a fresh list = %d, want 0", got)
	}
	l.SetOffset(7)
	if got := l.GetOffset(); got != 7 {
		t.Fatalf("GetOffset() after SetOffset(7) = %d, want 7", got)
	}
}

func TestTreeListEnsureVisibleViaSetCurrentItem(t *testing.T) {
	l := NewTreeList()
	for i := 0; i < 20; i++ {
		l.AddItem("row", nil)
	}
	l.SetRect(0, 0, 80, 5) // viewport height 5

	// Moving forward past the bottom edge scrolls the minimum amount
	// necessary to bring the new current item into view.
	l.SetCurrentItem(10)
	if got := l.GetOffset(); got != 6 { // 10 - 5 + 1
		t.Fatalf("GetOffset() after SetCurrentItem(10) = %d, want 6", got)
	}

	// Selecting a row already inside the viewport must not move the
	// offset at all - only the minimum-necessary scroll, never a
	// re-center.
	l.SetCurrentItem(7)
	if got := l.GetOffset(); got != 6 {
		t.Fatalf("GetOffset() after selecting a still-visible row = %d, want unchanged 6", got)
	}

	// Moving above the current offset scrolls up to meet it exactly.
	l.SetCurrentItem(2)
	if got := l.GetOffset(); got != 2 {
		t.Fatalf("GetOffset() after SetCurrentItem(2) = %d, want 2", got)
	}
}

func TestTreeListEnsureVisibleNoOpWithZeroHeight(t *testing.T) {
	l := NewTreeList()
	for i := 0; i < 5; i++ {
		l.AddItem("row", nil)
	}
	// No SetRect call at all - GetInnerRect's height is 0, so
	// ensureVisible must bail out rather than dividing by/against a
	// meaningless viewport.
	l.SetCurrentItem(4)
	if got := l.GetOffset(); got != 0 {
		t.Fatalf("GetOffset() with no viewport height = %d, want 0 (unchanged)", got)
	}
}

func noopSetFocus(tview.Primitive) {}

func TestTreeListInputHandlerNavigation(t *testing.T) {
	l := NewTreeList()
	for i := 0; i < 10; i++ {
		l.AddItem("row", nil)
	}
	l.SetRect(0, 0, 80, 3)
	handler := l.InputHandler()

	send := func(key tcell.Key, r rune) {
		handler(tcell.NewEventKey(key, r, tcell.ModNone), noopSetFocus)
	}

	send(tcell.KeyDown, 0)
	if got := l.GetCurrentItem(); got != 1 {
		t.Fatalf("after Down: GetCurrentItem() = %d, want 1", got)
	}
	send(tcell.KeyUp, 0)
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("after Up: GetCurrentItem() = %d, want 0", got)
	}
	// Up at the very top must not wrap around to the bottom.
	send(tcell.KeyUp, 0)
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("Up at index 0 wrapped around: GetCurrentItem() = %d, want 0", got)
	}

	send(tcell.KeyEnd, 0)
	if got := l.GetCurrentItem(); got != 9 {
		t.Fatalf("after End: GetCurrentItem() = %d, want 9", got)
	}
	// Down at the very bottom must not wrap around to the top.
	send(tcell.KeyDown, 0)
	if got := l.GetCurrentItem(); got != 9 {
		t.Fatalf("Down at last index wrapped around: GetCurrentItem() = %d, want 9", got)
	}

	send(tcell.KeyHome, 0)
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("after Home: GetCurrentItem() = %d, want 0", got)
	}

	send(tcell.KeyPgDn, 0) // height 3, so currentItem + 3
	if got := l.GetCurrentItem(); got != 3 {
		t.Fatalf("after PgDn: GetCurrentItem() = %d, want 3", got)
	}
	send(tcell.KeyPgUp, 0)
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("after PgUp: GetCurrentItem() = %d, want 0", got)
	}
}

func TestTreeListInputHandlerActivatesOnEnterAndSpace(t *testing.T) {
	l := NewTreeList()
	var enterCount, spaceCount int
	l.AddItem("only row", func() { enterCount++ })
	l.SetRect(0, 0, 80, 5)
	handler := l.InputHandler()

	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), noopSetFocus)
	if enterCount != 1 {
		t.Fatalf("Enter did not activate the row: enterCount = %d", enterCount)
	}

	l.Clear()
	l.AddItem("only row", func() { spaceCount++ })
	handler = l.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone), noopSetFocus)
	if spaceCount != 1 {
		t.Fatalf("Space did not activate the row: spaceCount = %d", spaceCount)
	}
}

func TestTreeListInputHandlerOnEmptyListIsNoOp(t *testing.T) {
	l := NewTreeList()
	handler := l.InputHandler()
	// Must not panic on an empty list.
	handler(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), noopSetFocus)
	if got := l.GetCurrentItem(); got != -1 {
		t.Fatalf("GetCurrentItem() after Down on empty list = %d, want -1", got)
	}
}

func TestTreeListMouseHandlerClickActivatesAndSelectsInOrder(t *testing.T) {
	l := NewTreeList()
	var order []string
	l.AddItem("row0", func() { order = append(order, "activate0") })
	l.AddItem("row1", func() { order = append(order, "activate1") })
	l.AddItem("row2", func() { order = append(order, "activate2") })
	l.SetRect(0, 0, 80, 10)
	l.SetChangedFunc(func(index int) {
		order = append(order, "changed")
	})

	handler := l.MouseHandler()
	// Row 1 is at screen y=1 given rect origin (0,0).
	consumed, _ := handler(tview.MouseLeftClick, tcell.NewEventMouse(2, 1, tcell.Button1, tcell.ModNone), noopSetFocus)
	if !consumed {
		t.Fatal("MouseLeftClick on a row was not consumed")
	}
	if got := l.GetCurrentItem(); got != 1 {
		t.Fatalf("GetCurrentItem() after click = %d, want 1", got)
	}
	// activate must fire before the cursor-move (changed) callback - the
	// same ordering tview.List.MouseHandler uses, per treelist.go's own
	// doc comment.
	want := []string{"activate1", "changed"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("call order = %v, want %v", order, want)
	}
}

func TestTreeListMouseHandlerClickOutsideRectIsIgnored(t *testing.T) {
	l := NewTreeList()
	l.AddItem("row0", func() { t.Fatal("selected callback fired on an out-of-rect click") })
	l.SetRect(0, 0, 80, 10)
	handler := l.MouseHandler()
	consumed, _ := handler(tview.MouseLeftClick, tcell.NewEventMouse(200, 200, tcell.Button1, tcell.ModNone), noopSetFocus)
	if consumed {
		t.Fatal("click outside the widget's rect was consumed")
	}
}

func TestTreeListMouseHandlerClickBelowLastRowMovesNothing(t *testing.T) {
	l := NewTreeList()
	l.AddItem("row0", nil)
	l.SetRect(0, 0, 80, 10) // only 1 row, but a 10-row-tall viewport
	handler := l.MouseHandler()
	consumed, _ := handler(tview.MouseLeftClick, tcell.NewEventMouse(2, 5, tcell.Button1, tcell.ModNone), noopSetFocus)
	if !consumed {
		t.Fatal("click inside the widget's rect (but past the last row) was not consumed")
	}
	if got := l.GetCurrentItem(); got != 0 {
		t.Fatalf("GetCurrentItem() after clicking below the last row = %d, want unchanged 0", got)
	}
}

func TestTreeListMouseHandlerWheelScrollBounded(t *testing.T) {
	l := NewTreeList()
	for i := 0; i < 20; i++ {
		l.AddItem("row", nil)
	}
	l.SetRect(0, 0, 80, 5)
	handler := l.MouseHandler()

	scroll := func(action tview.MouseAction) {
		handler(action, tcell.NewEventMouse(2, 2, 0, tcell.ModNone), noopSetFocus)
	}

	// Scrolling up from offset 0 must not go negative.
	scroll(tview.MouseScrollUp)
	if got := l.GetOffset(); got != 0 {
		t.Fatalf("GetOffset() after scrolling up from 0 = %d, want 0", got)
	}

	scroll(tview.MouseScrollDown)
	if got := l.GetOffset(); got != 1 {
		t.Fatalf("GetOffset() after one scroll down = %d, want 1", got)
	}
	scroll(tview.MouseScrollUp)
	if got := l.GetOffset(); got != 0 {
		t.Fatalf("GetOffset() after scrolling back up = %d, want 0", got)
	}

	// Scrolling down must stop once the last page is reached - it never
	// panning past having fewer than one full viewport of rows left.
	for i := 0; i < 30; i++ {
		scroll(tview.MouseScrollDown)
	}
	if got := l.GetOffset(); got != 15 { // 20 rows - height 5
		t.Fatalf("GetOffset() after scrolling well past the end = %d, want capped at 15", got)
	}
}
