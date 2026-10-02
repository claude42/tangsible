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

	"github.com/rivo/tview"
)

var testHelp = HelpScreen{Title: "test", Sections: []HelpSection{
	{Title: "One", Entries: []HelpEntry{
		{Keys: "a", Desc: "first", Bar: "a: first", BarPos: 20},
		{Keys: "b / B", Desc: "second, with a description long enough to need wrapping", Bar: "b: second", BarPos: 10, ID: "b"},
	}},
	{Title: "Two", Entries: []HelpEntry{
		{Keys: "c", Desc: "only in help", ID: "c"},
	}},
}}

func TestHelpBarText(t *testing.T) {
	if got, want := testHelp.BarText(nil), " b: second  a: first "; got != want {
		t.Errorf("BarText(nil) = %q, want %q (BarPos order, not table order)", got, want)
	}
	hideB := func(id string) bool { return id == "b" }
	if got, want := testHelp.BarText(hideB), " a: first "; got != want {
		t.Errorf("BarText(hide b) = %q, want %q", got, want)
	}
}

func TestHelpText(t *testing.T) {
	plain := func(s string) []string {
		tv := tview.NewTextView().SetDynamicColors(true).SetText(s)
		return strings.Split(tv.GetText(true), "\n")
	}
	lines := plain(HelpText(testHelp, 30, nil))
	want := []string{
		"One",
		"a      first",
		"b / B  second, with a",
		"       description long enough",
		"       to need wrapping",
		"",
		"Two",
		"c      only in help",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("HelpText() =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// Hiding a section's only entry drops the section heading too.
	hideC := func(id string) bool { return id == "c" }
	if got := HelpText(testHelp, 30, hideC); strings.Contains(got, "Two") {
		t.Errorf("HelpText(hide c) still shows the emptied section: %q", got)
	}
}
