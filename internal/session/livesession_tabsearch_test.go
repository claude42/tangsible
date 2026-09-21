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
	"testing"

	"code.aw.net/claude/tangsible/internal/uikit"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// newTestTabSearchPanel mirrors internal/uikit/tabsearchbar_test.go's own
// newTestTabSearchBar - same fixture shape, adapted for this panel's extra
// constructor params (a shared bottomBar it doesn't own outright, and a
// normalStyle callback in place of TabSearchBar's own fixed BarStyle).
func newTestTabSearchPanel(t *testing.T) (p *tabSearchPanel, tabs *uikit.TabbedPane, tv, bottomBar *tview.TextView) {
	t.Helper()
	app := tview.NewApplication()
	tv = tview.NewTextView().SetDynamicColors(true)
	tv.SetText(tview.Escape("one two three two one"))
	tabs = uikit.NewTabbedPane()
	tabs.SetTabs([]string{"Only"}, []tview.Primitive{tv})
	bottomBar = tview.NewTextView().SetText(outputHintBarText)
	normalStyle := func() tcell.Style { return tcell.StyleDefault }
	p = newTabSearchPanel(app, tabs, bottomBar, tabs.Primitive(), normalStyle)
	return p, tabs, tv, bottomBar
}

// typeQuery feeds each rune of s into p's composing input via
// handleComposingKey, then submits with Enter - the same "type it, press
// Enter" shape TabSearchBar's own tests use, exercised through this panel's
// public entry point rather than by calling p.input.SetText directly, so a
// real key-injection bug (like the one handleComposingKey's own doc comment
// documents) would show up here too.
func typeQuery(p *tabSearchPanel, s string) {
	for _, r := range s {
		p.handleComposingKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
	p.handleComposingKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
}

func TestTabSearchPanelOpenComposeAndSubmit(t *testing.T) {
	p, _, _, _ := newTestTabSearchPanel(t)

	if p.composing || p.active != nil {
		t.Fatal("fresh panel should be neither composing nor active")
	}

	p.open()
	if !p.composing {
		t.Fatal("open() should set composing")
	}

	typeQuery(p, "two")

	if p.composing {
		t.Error("submitting with Enter should end composing")
	}
	if p.active == nil {
		t.Fatal("submitting a non-empty query should activate a search")
	}
}

func TestTabSearchPanelDone_EmptyQueryClears(t *testing.T) {
	p, _, _, bottomBar := newTestTabSearchPanel(t)
	p.open()
	// No typing - submit an empty query directly.
	p.handleComposingKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))

	if p.active != nil {
		t.Error("submitting an empty query should never activate a search")
	}
	if got := bottomBar.GetText(true); got != outputHintBarText {
		t.Errorf("bottomBar after empty submit = %q, want the hint text back", got)
	}
}

func TestTabSearchPanelDone_NonEnterCancels(t *testing.T) {
	p, _, _, _ := newTestTabSearchPanel(t)
	p.open()
	p.handleComposingKey(tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModNone))
	p.handleComposingKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))

	if p.composing {
		t.Error("Escape should end composing")
	}
	if p.active != nil {
		t.Error("canceling composition should never activate a search")
	}
}

func TestTabSearchPanelCloseComposing(t *testing.T) {
	p, _, _, _ := newTestTabSearchPanel(t)
	p.open()
	p.closeComposing()
	if p.composing {
		t.Error("closeComposing should end composing")
	}
}

func TestTabSearchPanelCloseComposing_NoOpWhenNotComposing(t *testing.T) {
	p, _, _, _ := newTestTabSearchPanel(t)
	p.closeComposing() // must not panic
	if p.composing {
		t.Error("closeComposing on a fresh panel should leave composing false")
	}
}

func TestTabSearchPanelReopenPrefillsPreviousQuery(t *testing.T) {
	p, _, _, _ := newTestTabSearchPanel(t)
	p.open()
	typeQuery(p, "two")

	p.open() // reopen while a search is already active
	if got := p.input.GetText(); got != "two" {
		t.Errorf("reopening the prompt = %q, want it pre-filled with the previous query %q", got, "two")
	}
}

func TestTabSearchPanelNextPrevAndClear(t *testing.T) {
	p, _, tv, bottomBar := newTestTabSearchPanel(t)
	p.open()
	typeQuery(p, "two")

	if got := bottomBar.GetText(true); !strings.Contains(got, "match 1 of 2") {
		t.Errorf("bottomBar after search = %q, want it to mention \"match 1 of 2\"", got)
	}
	if !p.hasMatches() {
		t.Fatal("hasMatches should be true right after a matching search")
	}

	p.next()
	if got := bottomBar.GetText(true); !strings.Contains(got, "match 2 of 2") {
		t.Errorf("bottomBar after next() = %q, want it to mention \"match 2 of 2\"", got)
	}
	p.prev()
	if got := bottomBar.GetText(true); !strings.Contains(got, "match 1 of 2") {
		t.Errorf("bottomBar after prev() = %q, want it back to \"match 1 of 2\"", got)
	}

	beforeClear := tv.GetText(false)
	p.clear()
	if p.active != nil {
		t.Error("clear() should end the active search")
	}
	if got := bottomBar.GetText(true); got != outputHintBarText {
		t.Errorf("bottomBar after clear() = %q, want the original hint text back", got)
	}
	if got := tv.GetText(false); got == beforeClear {
		t.Error("searched TextView's own text is unchanged after clear() - matches are still highlighted, want them cleared too")
	}
	if got := tv.GetText(true); got != "one two three two one" {
		t.Errorf("searched TextView's plain text after clear() = %q, want the original content back", got)
	}
}

func TestTabSearchPanelNextPrevNoOpWithoutActiveSearch(t *testing.T) {
	p, _, _, _ := newTestTabSearchPanel(t)
	p.next() // must not panic
	p.prev()
	if p.active != nil {
		t.Error("next/prev must never themselves activate a search")
	}
}

// TestTabSearchPanelClearAlwaysResetsBottomBar pins down clear()'s own
// documented "always resets bottomBar, even with no search active" behavior
// - what lets a transient 'y' clipboard-copy status message get superseded
// by the same call sites that already supersede a real search.
func TestTabSearchPanelClearAlwaysResetsBottomBar(t *testing.T) {
	p, _, _, bottomBar := newTestTabSearchPanel(t)
	bottomBar.SetText(" copied \"Only\" tab to clipboard (5 bytes) ")

	p.clear()
	if got := bottomBar.GetText(true); got != outputHintBarText {
		t.Errorf("bottomBar after clear() with no active search = %q, want the hint text back even though nothing was searching", got)
	}
}

func TestTabSearchPanelChangingActiveTabClearsSearch(t *testing.T) {
	p, tabs, tv, _ := newTestTabSearchPanel(t)
	other := tview.NewTextView().SetDynamicColors(true)
	other.SetText(tview.Escape("second tab"))
	tabs.SetTabs([]string{"Only", "Other"}, []tview.Primitive{tv, other})
	p.open()
	typeQuery(p, "two")
	if p.active == nil {
		t.Fatal("expected an active search before switching tabs")
	}

	tabs.Next() // newTabSearchPanel wires tabs.SetChangedFunc(p.clear)
	if p.active != nil {
		t.Error("switching the active tab should clear the search - it only ever describes the tab that was active when it started")
	}
}
