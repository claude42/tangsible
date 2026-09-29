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

	"code.aw.net/claude/tangsible/internal/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestResolveColorOverride(t *testing.T) {
	orange := "orange"
	invalid := "not-a-real-color"

	if got := ResolveColorOverride("outcome_ok", nil, "green"); got != "green" {
		t.Errorf("ResolveColorOverride(nil) = %q, want the fallback %q", got, "green")
	}
	if got := ResolveColorOverride("outcome_ok", &orange, "green"); got != "orange" {
		t.Errorf("ResolveColorOverride(%q) = %q, want %q", orange, got, orange)
	}
	if got := ResolveColorOverride("outcome_ok", &invalid, "green"); got != "green" {
		t.Errorf("ResolveColorOverride(%q) = %q, want the fallback %q (invalid color)", invalid, got, "green")
	}

	hex := "#ff8800"
	if got := ResolveColorOverride("outcome_ok", &hex, "green"); got != hex {
		t.Errorf("ResolveColorOverride(%q) = %q, want %q (valid #rrggbb hex)", hex, got, hex)
	}
}

// TestApplyColorConfig covers a valid override actually landing, an
// invalid one falling back to whatever the var already held rather than
// corrupting it, and a plain-string override reaching a *derived*
// tcell.Style var (BarStyle) built from it, not just its own plain-tag-
// string form - the exact gap design-docs/ColorConfig.md's own Open
// questions once flagged as still-open for SearchBarStyle (fixed
// separately) and is the whole reason ApplyColorConfig rebuilds
// BarStyle/ReplayBarStyle/CheckBarStyle/SearchBarStyle instead of only
// ever touching the plain-string vars.
//
// Every var this test touches is package-level, shared mutable state -
// every other test in this package (and several in other packages)
// asserts against these vars' own hardcoded defaults, so each one this
// test overrides is saved and restored via t.Cleanup, never left
// changed once this test returns.
func TestApplyColorConfig(t *testing.T) {
	origOutcomeOK := OutcomeOKColor
	origChromeLive := ChromeLiveColor
	origBarStyle := BarStyle
	origBackground := BackgroundColor
	origPrimitiveBg := tview.Styles.PrimitiveBackgroundColor
	origDialogBorder := DialogBorderColor
	t.Cleanup(func() {
		OutcomeOKColor = origOutcomeOK
		ChromeLiveColor = origChromeLive
		BarStyle = origBarStyle
		BackgroundColor = origBackground
		tview.Styles.PrimitiveBackgroundColor = origPrimitiveBg
		DialogBorderColor = origDialogBorder
	})

	valid := "orange"
	invalid := "not-a-real-color"
	background := "silver"
	dialogBorder := "aqua"
	ApplyColorConfig(config.ColorsConfig{
		OutcomeOK:    &valid,
		ChromeLive:   &invalid,
		Background:   &background,
		DialogBorder: &dialogBorder,
	})

	if OutcomeOKColor != "orange" {
		t.Errorf("OutcomeOKColor = %q, want %q", OutcomeOKColor, "orange")
	}
	if ChromeLiveColor != origChromeLive {
		t.Errorf("ChromeLiveColor = %q, want it left at %q (invalid override falls back)", ChromeLiveColor, origChromeLive)
	}
	if want := buildChromeStyle("white", ChromeLiveColor); BarStyle != want {
		t.Errorf("BarStyle wasn't rebuilt from the (fallen-back) ChromeLiveColor")
	}
	if BackgroundColor != "silver" {
		t.Errorf("BackgroundColor = %q, want %q", BackgroundColor, "silver")
	}
	if want := tcell.GetColor("silver"); tview.Styles.PrimitiveBackgroundColor != want {
		t.Errorf("tview.Styles.PrimitiveBackgroundColor = %v, want %v (BackgroundColor must actually reach it)", tview.Styles.PrimitiveBackgroundColor, want)
	}
	if DialogBorderColor != "aqua" {
		t.Errorf("DialogBorderColor = %q, want %q", DialogBorderColor, "aqua")
	}
}
