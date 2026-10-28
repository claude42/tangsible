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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// CenteredModal wraps p in nested Flexes so it renders as a fixed-size box
// centered within whatever space its container gives it, instead of
// filling all of it - the standard tview pattern for a page that overlays
// only part of the screen (see tview's own Pages wiki example). Used for
// the filter dialog (see NewLiveTUI). p is wrapped in clearedPrimitive, so a
// dialog row with no widget of its own doesn't show what is underneath.
func CenteredModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(clearedPrimitive{p}, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

// clearedPrimitive paints its whole rect in the background color before
// drawing the wrapped primitive. tview's Flex and Grid are created with
// dontClear set, so any row of a Flex-built dialog that holds no widget
// (spare space at the bottom, say) would otherwise keep whatever the page
// underneath drew there. Every other method is promoted from the wrapped
// primitive, so focus, keys and mouse handling are unchanged.
type clearedPrimitive struct{ tview.Primitive }

func (c clearedPrimitive) Draw(screen tcell.Screen) {
	x, y, w, h := c.GetRect()
	style := tcell.StyleDefault.Background(tview.Styles.PrimitiveBackgroundColor)
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			screen.SetContent(col, row, ' ', nil, style)
		}
	}
	c.Primitive.Draw(screen)
}

// StyleAutocomplete gives field's completion drop-down the control
// colors, matching the field it belongs to: entries look like the field
// (control_text on control_background), the highlighted one swapped -
// the same convention as a focused button or checkbox. Replaces tview's
// own defaults (the background color on green, highlighted entry the
// background color on white - white on white with a light [colors]
// background).
func StyleAutocomplete(field *tview.InputField) *tview.InputField {
	return field.SetAutocompleteStyles(tcell.GetColor(ControlBackgroundColor), controlStyle(), controlFocusStyle())
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
// four filters this dialog itself offers (All/Interesting/Changed/Failed -
// the search filter is a separate dialog, see NewLiveTUI's searchDialog),
// each with a small marker next to whichever one is currently active. No
// marker shown at all if a search filter is currently active instead -
// none of these four apply then. No tview.Escape() needed - every piece of
// text here is a fixed literal, never external content (same reasoning as
// formatHostOutput's own fixed labels).
//
// The old trailing "Esc/q to cancel" hint line is gone - replaced by a real
// Cancel button in filterFlex (see NewLiveTUI) - Esc/q still work as
// keyboard shortcuts too, but no longer need a text hint now that there's a
// clickable affordance for the same action.
func FilterDialogText(active FilterQuery) string {
	mark := func(mode FilterMode) string {
		if mode == active.Mode {
			return "[" + SelectionColor + "]*[-]"
		}
		return " "
	}
	return fmt.Sprintf(
		" [::b]Select filter[::-]\n\n"+
			" %s A - Show all\n"+
			" %s I - Show interesting tasks\n"+
			" %s C - Show only changed tasks\n"+
			" %s F - Show only failed tasks",
		mark(FilterAll), mark(FilterInteresting), mark(FilterChanged), mark(FilterFailed),
	)
}

// TranslateNavAlias maps this app's navigation aliases onto the native
// keys tview's TextView and TreeList already handle: j/k -> Down/Up,
// Space/b and Ctrl-F/Ctrl-B -> PgDn/PgUp, Ctrl-A/Ctrl-E, </> and G ->
// Home/End. ok is false for any other key. Returning the translated event
// from an app's input capture makes tview forward it to the focused
// primitive as if that key had been typed. Shared by every app (the live
// session and every standalone verb), so the aliases work the same
// everywhere - only call it once a text field (search prompt, dialog) can
// no longer be the one receiving the key, since these are all ordinary
// typed characters there.
func TranslateNavAlias(event *tcell.EventKey) (*tcell.EventKey, bool) {
	key := tcell.KeyNUL
	switch event.Key() {
	case tcell.KeyCtrlF:
		key = tcell.KeyPgDn
	case tcell.KeyCtrlB:
		key = tcell.KeyPgUp
	case tcell.KeyCtrlA:
		key = tcell.KeyHome
	case tcell.KeyCtrlE:
		key = tcell.KeyEnd
	case tcell.KeyRune:
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
		case '<':
			key = tcell.KeyHome
		}
	}
	if key == tcell.KeyNUL {
		return nil, false
	}
	return tcell.NewEventKey(key, 0, tcell.ModNone), true
}

// controlStyle/controlFocusStyle are the control_text/control_background
// colors (ControlTextColor/ControlBackgroundColor) as tcell styles - plain,
// and swapped for a focused button or checkbox.
func controlStyle() tcell.Style {
	return tcell.StyleDefault.Foreground(tcell.GetColor(ControlTextColor)).Background(tcell.GetColor(ControlBackgroundColor))
}

func controlFocusStyle() tcell.Style {
	return tcell.StyleDefault.Foreground(tcell.GetColor(ControlBackgroundColor)).Background(tcell.GetColor(ControlTextColor))
}

// StyleInputField gives a standalone text input field (one not inside a
// tview.Form - see StyleForm) the control colors instead of tview's
// built-in white on blue.
func StyleInputField(f *tview.InputField) *tview.InputField {
	return f.SetFieldStyle(controlStyle())
}

// StyleButton gives a standalone button the control colors, swapped
// while it has focus.
func StyleButton(b *tview.Button) *tview.Button {
	return b.SetStyle(controlStyle()).SetActivatedStyle(controlFocusStyle())
}

// StyleForm gives every field, checkbox and button of f the control
// colors - Form re-applies its field colors to each item on every draw,
// so this covers items added later too. A checkbox's focused look isn't
// part of that; StyleCheckbox sets it per checkbox.
func StyleForm(f *tview.Form) *tview.Form {
	return f.SetFieldStyle(controlStyle()).
		SetButtonStyle(controlStyle()).
		SetButtonActivatedStyle(controlFocusStyle())
}

// StyleCheckbox sets a checkbox's focused look (the control colors,
// swapped) - its unfocused look comes from StyleForm.
func StyleCheckbox(c *tview.Checkbox) *tview.Checkbox {
	return c.SetActivatedStyle(controlFocusStyle())
}
