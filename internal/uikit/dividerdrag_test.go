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

func TestDividerDrag(t *testing.T) {
	const total = 200
	divider := tview.NewBox()
	divider.SetRect(80, 1, SplitDividerWidth, 30)
	var d DividerDrag
	relayouts := 0
	ev := func(x, y int, b tcell.ButtonMask) *tcell.EventMouse {
		return tcell.NewEventMouse(x, y, b, 0)
	}
	handle := func(e *tcell.EventMouse, a tview.MouseAction) (*tcell.EventMouse, bool) {
		return d.HandleMouse(e, a, divider, 0, total, func() { relayouts++ })
	}

	if got, want := d.TreeWidth(total), SplitTreeWidth(total); got != want {
		t.Errorf("TreeWidth before any drag = %d, want %d", got, want)
	}
	// A press or move away from the divider is not ours.
	if _, handled := handle(ev(50, 5, tcell.Button1), tview.MouseLeftDown); handled {
		t.Error("press off the divider was handled")
	}
	if _, handled := handle(ev(50, 5, tcell.Button1), tview.MouseMove); handled {
		t.Error("move without a drag was handled")
	}
	// Press on it, drag, release.
	if _, handled := handle(ev(80, 5, tcell.Button1), tview.MouseLeftDown); !handled {
		t.Fatal("press on the divider was not handled")
	}
	if out, handled := handle(ev(55, 9, tcell.Button1), tview.MouseMove); !handled || out != nil {
		t.Errorf("drag move: handled=%v out=%v, want handled and swallowed", handled, out)
	}
	if got := d.TreeWidth(total); got != 55 || relayouts != 1 {
		t.Errorf("after drag to 55: TreeWidth=%d relayouts=%d, want 55 and 1", got, relayouts)
	}
	handle(ev(5, 9, tcell.Button1), tview.MouseMove)
	if got := d.TreeWidth(total); got != SplitMinTreeWidth {
		t.Errorf("drag far left: TreeWidth=%d, want %d", got, SplitMinTreeWidth)
	}
	if _, handled := handle(ev(5, 9, 0), tview.MouseLeftUp); !handled {
		t.Error("release was not handled")
	}
	if _, handled := handle(ev(60, 9, tcell.Button1), tview.MouseMove); handled {
		t.Error("move after release was handled")
	}
	// A release lost outside the terminal ends the drag on the next move.
	handle(ev(80, 5, tcell.Button1), tview.MouseLeftDown)
	if _, handled := handle(ev(60, 9, 0), tview.MouseMove); !handled || d.dragging {
		t.Errorf("move with no button held: handled=%v dragging=%v, want handled and drag over", handled, d.dragging)
	}
	// A later, narrower terminal re-clamps the kept width.
	if got, want := d.TreeWidth(SplitMinTotalWidth), SplitMinTreeWidth; got != want {
		t.Errorf("TreeWidth at the minimum total = %d, want %d", got, want)
	}
}
