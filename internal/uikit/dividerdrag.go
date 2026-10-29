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
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// DividerDrag lets the user resize a two-pane layout
// (design-docs/TwoPanedLayout.md) by dragging the divider between the panes
// with the mouse. One per session; the chosen width lives as long as it does
// (not persisted) and is re-clamped against the terminal's current width on
// every use, so a later resize can't leave it out of range. All methods run
// on tview's event loop.
type DividerDrag struct {
	override int // tree pane width the user dragged to; 0 = none
	dragging bool
}

// TreeWidth is the tree pane's width for a terminal totalWidth columns wide:
// the dragged-to width if any, else SplitTreeWidth's growth rule.
func (d *DividerDrag) TreeWidth(totalWidth int) int {
	if d.override > 0 {
		return ClampSplitTreeWidth(totalWidth, d.override)
	}
	return SplitTreeWidth(totalWidth)
}

// Cancel abandons a drag in progress (e.g. the split closed under it).
func (d *DividerDrag) Cancel() { d.dragging = false }

// HandleMouse is for the split layout's mouse capture, called only while the
// split is on screen. A left press on divider's column starts a drag; moves
// until the release set the tree width so the divider follows the pointer
// (bodyX is the split row's left edge) and call relayout; the release ends
// it. handled reports that the event was consumed - the returned event is
// then what the capture should return (nil, except for a move whose button
// release was missed outside the terminal, which is passed on). A click is
// never synthesized from the swallowed press, which is fine: nothing else
// lives under the divider.
func (d *DividerDrag) HandleMouse(event *tcell.EventMouse, action tview.MouseAction, divider tview.Primitive, bodyX, totalWidth int, relayout func()) (result *tcell.EventMouse, handled bool) {
	x, y := event.Position()
	switch action {
	case tview.MouseLeftDown:
		if InRect(x, y, divider) {
			d.dragging = true
			return nil, true
		}
	case tview.MouseMove:
		if !d.dragging {
			return nil, false
		}
		if event.Buttons()&tcell.Button1 == 0 {
			d.dragging = false
			return event, true
		}
		d.override = ClampSplitTreeWidth(totalWidth, x-bodyX)
		relayout()
		return nil, true
	case tview.MouseLeftUp:
		if d.dragging {
			d.dragging = false
			return nil, true
		}
	}
	return nil, false
}
