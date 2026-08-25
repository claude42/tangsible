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
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func newTestTabSearchBar(t *testing.T) (*TabSearchBar, *TabbedPane, *tview.TextView) {
	t.Helper()
	app := tview.NewApplication()
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetText(tview.Escape("one two three two one"))
	tabs := NewTabbedPane()
	tabs.SetTabs([]string{"Only"}, []tview.Primitive{tv})
	bar := NewTabSearchBar(app, tabs, " hint text ", tabs.Primitive())
	return bar, tabs, tv
}

func TestTabSearchBarOpenComposeAndSubmit(t *testing.T) {
	bar, _, _ := newTestTabSearchBar(t)

	if bar.IsComposing() || bar.HasActive() {
		t.Fatal("fresh bar should be neither composing nor active")
	}

	bar.Open()
	if !bar.IsComposing() {
		t.Fatal("Open() should set IsComposing")
	}

	for _, r := range "two" {
		bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if bar.IsComposing() {
		t.Error("submitting with Enter should end composing")
	}
	if !bar.HasActive() {
		t.Fatal("submitting a non-empty query should activate a search")
	}
}

func TestTabSearchBarCancelLeavesNoActiveSearch(t *testing.T) {
	bar, _, _ := newTestTabSearchBar(t)
	bar.Open()
	bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	bar.CloseComposing()

	if bar.IsComposing() {
		t.Error("CloseComposing should end composing")
	}
	if bar.HasActive() {
		t.Error("canceling composition should never activate a search")
	}
}

func TestTabSearchBarReopenPrefillsPreviousQuery(t *testing.T) {
	bar, _, _ := newTestTabSearchBar(t)
	bar.Open()
	for _, r := range "two" {
		bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	bar.Open() // reopen while a search is already active
	if got := bar.input.GetText(); got != "two" {
		t.Errorf("reopening the prompt = %q, want it pre-filled with the previous query %q", got, "two")
	}
}

func TestTabSearchBarNextPrevAndClear(t *testing.T) {
	bar, _, tv := newTestTabSearchBar(t)
	bar.Open()
	for _, r := range "two" {
		bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if got := bar.footer.GetText(true); !strings.Contains(got, "match 1 of 2") {
		t.Errorf("footer after search = %q, want it to mention \"match 1 of 2\"", got)
	}
	bar.Next()
	if got := bar.footer.GetText(true); !strings.Contains(got, "match 2 of 2") {
		t.Errorf("footer after Next() = %q, want it to mention \"match 2 of 2\"", got)
	}
	bar.Prev()
	if got := bar.footer.GetText(true); !strings.Contains(got, "match 1 of 2") {
		t.Errorf("footer after Prev() = %q, want it back to \"match 1 of 2\"", got)
	}

	beforeClear := tv.GetText(false)
	bar.Clear()
	if bar.HasActive() {
		t.Error("Clear() should end the active search")
	}
	if got := bar.footer.GetText(true); got != " hint text " {
		t.Errorf("footer after Clear() = %q, want the original hint text back", got)
	}
	if got := tv.GetText(false); got == beforeClear {
		t.Error("searched TextView's own text is unchanged after Clear() - matches are still highlighted, want them cleared too")
	}
	if got := tv.GetText(true); got != "one two three two one" {
		t.Errorf("searched TextView's plain text after Clear() = %q, want the original content back", got)
	}
}

func TestTabSearchBarNextPrevNoOpWithoutActiveSearch(t *testing.T) {
	bar, _, _ := newTestTabSearchBar(t)
	bar.Next() // must not panic
	bar.Prev()
	if bar.HasActive() {
		t.Error("Next/Prev must never themselves activate a search")
	}
}

func TestTabSearchBarClearForView(t *testing.T) {
	bar, _, tv := newTestTabSearchBar(t)
	bar.Open()
	for _, r := range "two" {
		bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	bar.HandleComposingKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
	if !bar.HasActive() {
		t.Fatal("expected an active search")
	}

	other := tview.NewTextView()
	bar.ClearForView(other)
	if !bar.HasActive() {
		t.Error("ClearForView on an unrelated TextView should not clear the active search")
	}

	bar.ClearForView(tv)
	if bar.HasActive() {
		t.Error("ClearForView on the search's own target TextView should clear it")
	}
}
