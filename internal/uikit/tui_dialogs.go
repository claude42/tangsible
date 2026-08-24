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

	"github.com/rivo/tview"
)

// CenteredModal wraps p in nested Flexes so it renders as a fixed-size box
// centered within whatever space its container gives it, instead of
// filling all of it - the standard tview pattern for a page that overlays
// only part of the screen (see tview's own Pages wiki example). Used for
// the filter dialog (see NewLiveTUI).
func CenteredModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

// InRect reports whether (x, y) falls within p's own last-drawn rect - the
// padding Flex items centeredModal wraps a dialog's content in don't touch
// that content's own rect, so this is exactly "is (x, y) inside the
// visible dialog box" for anything built via centeredModal. Used at the
// SetMouseCapture level (both here and in template.go) to let a click
// through to a dialog's own native mouse handling when it lands inside the
// dialog, while still swallowing anything outside it - see NewLiveTUI's
// SetMouseCapture for why swallowing the outside click matters (confirmed
// against tview's own Pages.MouseHandler: it tries every visible page,
// topmost first, so an unswallowed click outside the dialog's own box
// would otherwise fall through to the page underneath).
func InRect(x, y int, p tview.Primitive) bool {
	rx, ry, rw, rh := p.GetRect()
	return x >= rx && x < rx+rw && y >= ry && y < ry+rh
}

// FilterDialogText renders the filter dialog's body - a headline and the
// three filters this dialog itself offers (All/Changed/Failed - the search
// filter is a separate dialog, see NewLiveTUI's searchDialog), each with a
// small marker next to whichever one is currently active. No marker shown
// at all if a search filter is currently active instead - none of these
// three apply then. No tview.Escape() needed - every piece of text here is
// a fixed literal, never external content (same reasoning as
// formatHostOutput's own fixed labels).
//
// The old trailing "Esc/q to cancel" hint line is gone - replaced by a real
// Cancel button in filterFlex (see NewLiveTUI) - Esc/q still work as
// keyboard shortcuts too, but no longer need a text hint now that there's a
// clickable affordance for the same action.
func FilterDialogText(active FilterQuery) string {
	mark := func(mode FilterMode) string {
		if mode == active.Mode {
			return "[aqua]*[-]"
		}
		return " "
	}
	return fmt.Sprintf(
		" [::b]Select filter[::-]\n\n"+
			" %s A - Show all\n"+
			" %s C - Show changed (includes failed)\n"+
			" %s F - Show only failed tasks",
		mark(FilterAll), mark(FilterChanged), mark(FilterFailed),
	)
}
