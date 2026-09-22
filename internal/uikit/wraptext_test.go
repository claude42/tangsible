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
	"slices"
	"testing"
)

func TestWrapText_ShortLineUnchanged(t *testing.T) {
	if got := WrapText("short line", 78); !slices.Equal(got, []string{"short line"}) {
		t.Errorf("WrapText() = %v, want unchanged single line", got)
	}
}

// TestWrapText_PreservesWhitespaceOnShortLines locks in the deliberate
// asymmetry: a line that already fits is never touched, so a source
// snippet's own indentation or a caret pointer's leading spaces survive
// exactly - the actual motivating case (an ansible [ERROR]:'s own
// "Origin:" context block).
func TestWrapText_PreservesWhitespaceOnShortLines(t *testing.T) {
	lines := []string{
		"6     state: stopped",
		"7",
		"8 - name: Pause before restart",
		"    ^ column 3",
	}
	for _, l := range lines {
		got := WrapText(l, 78)
		if len(got) != 1 || got[0] != l {
			t.Errorf("WrapText(%q) = %v, want unchanged", l, got)
		}
	}
}

func TestWrapText_WrapsOverlongLineAtWordBoundaries(t *testing.T) {
	text := "The 'ansible.builtin.pause' module bypasses the host loop, which is currently not supported in the free strategy and would instead execute for every host in the inventory list."
	got := WrapText(text, 78)
	if len(got) < 2 {
		t.Fatalf("WrapText() = %v, want more than one line for %d-rune input at width 78", got, len([]rune(text)))
	}
	for i, line := range got {
		if n := len([]rune(line)); n > 78 {
			t.Errorf("line %d = %q has %d runes, want <= 78", i, line, n)
		}
	}
	// Rejoining with single spaces must reproduce every word in order -
	// wrapping must never drop or reorder content, only reflow it.
	if got, want := joinWithSpace(got), text; got != want {
		t.Errorf("rejoined = %q, want %q", got, want)
	}
}

func joinWithSpace(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += " "
		}
		out += l
	}
	return out
}

func TestWrapText_MultipleParagraphLinesEachWrapIndependently(t *testing.T) {
	text := "short\n" + "a very long line that will definitely need to be wrapped across more than one row of output"
	got := WrapText(text, 20)
	if got[0] != "short" {
		t.Errorf("first line = %q, want unchanged %q", got[0], "short")
	}
	for i, line := range got {
		if n := len([]rune(line)); n > 20 {
			t.Errorf("line %d = %q has %d runes, want <= 20", i, line, n)
		}
	}
}

func TestWrapText_EmptyInput(t *testing.T) {
	if got := WrapText("", 78); !slices.Equal(got, []string{""}) {
		t.Errorf("WrapText(\"\") = %v, want a single empty line", got)
	}
}

// TestWrapText_SingleOverlongWordLeftUnbroken covers the documented
// "don't hard-break mid-word" choice - a long URL/path with no spaces
// stays on its own line even past width, rather than being chopped.
func TestWrapText_SingleOverlongWordLeftUnbroken(t *testing.T) {
	word := "https://example.com/a/very/long/path/that/has/no/spaces/in/it/at/all"
	got := WrapText(word, 20)
	if len(got) != 1 || got[0] != word {
		t.Errorf("WrapText(%q, 20) = %v, want the single unbroken word", word, got)
	}
}
