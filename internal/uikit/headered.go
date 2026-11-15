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

// HeaderedText is a one-line header primitive (a drop-down, say) above a
// TextView, as one tab body. Unlike a Flex it draws the header *last*, so a
// drop-down's open list isn't painted over by the text below it. Only
// drawing is its own: the owner routes keys to Header and Body directly.
type HeaderedText struct {
	*tview.Box
	Header tview.Primitive
	Body   *tview.TextView
}

// NewHeaderedText wraps header and body.
func NewHeaderedText(header tview.Primitive, body *tview.TextView) *HeaderedText {
	return &HeaderedText{Box: tview.NewBox(), Header: header, Body: body}
}

// BodyTextView is what TabbedPane.ActiveTextView reads the text from.
func (h *HeaderedText) BodyTextView() *tview.TextView { return h.Body }

// Draw lays out the header on the first row, the body below it.
func (h *HeaderedText) Draw(screen tcell.Screen) {
	h.Box.DrawForSubclass(screen, h)
	x, y, w, ht := h.GetInnerRect()
	h.Header.SetRect(x, y, w, 1)
	h.Body.SetRect(x, y+1, w, ht-1)
	h.Body.Draw(screen)
	h.Header.Draw(screen)
}
