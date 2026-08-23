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

package main

import (
	"strings"
	"testing"
)

// extractTopBarFill parses topBarText's own two shapes - the plain,
// single-tag form used when progressTotal is 0, or the two-tag filled/
// unfilled form otherwise - and returns the filled/unfilled inner text.
// hasFill reports which shape was found.
func extractTopBarFill(t *testing.T, s string) (filled, unfilled string, hasFill bool) {
	t.Helper()
	fillPrefix := "[white:" + progressFillColor + ":b]"
	if !strings.HasPrefix(s, fillPrefix) {
		const plainPrefix = "[white:navy:b]"
		const plainSuffix = "[-:-:-]"
		if !strings.HasPrefix(s, plainPrefix) || !strings.HasSuffix(s, plainSuffix) {
			t.Fatalf("topBarText() = %q, unrecognized shape", s)
		}
		return "", strings.TrimSuffix(strings.TrimPrefix(s, plainPrefix), plainSuffix), false
	}
	const sep = "[-:-:-][white:navy:b]"
	const suffix = "[-:-:-]"
	rest := strings.TrimPrefix(s, fillPrefix)
	idx := strings.Index(rest, sep)
	if idx < 0 || !strings.HasSuffix(rest, suffix) {
		t.Fatalf("topBarText() = %q, unrecognized filled shape", s)
	}
	filled = rest[:idx]
	unfilled = strings.TrimSuffix(rest[idx+len(sep):], suffix)
	return filled, unfilled, true
}

func TestTopBarTextProgressFill(t *testing.T) {
	call := func(pos, total int, frozen bool) string {
		return topBarText("site.yml", false, nil, 0, frozen, filterQuery{}, pos, total, 100, "navy", true)
	}

	t.Run("no skeleton at all: plain, no fill color used", func(t *testing.T) {
		got := call(0, 0, false)
		if strings.Contains(got, progressFillColor) {
			t.Errorf("topBarText() = %q, want no fill color when progressTotal is 0", got)
		}
		if _, _, hasFill := extractTopBarFill(t, got); hasFill {
			t.Errorf("topBarText() unexpectedly used the fill form with progressTotal == 0")
		}
	})

	t.Run("zero progress: nothing filled yet", func(t *testing.T) {
		filled, _, hasFill := extractTopBarFill(t, call(0, 10, false))
		if !hasFill {
			t.Fatal("want the fill form")
		}
		if filled != "" {
			t.Errorf("filled = %q, want empty at 0/10 progress", filled)
		}
	})

	t.Run("full progress, not yet frozen: entirely filled", func(t *testing.T) {
		_, unfilled, hasFill := extractTopBarFill(t, call(10, 10, false))
		if !hasFill {
			t.Fatal("want the fill form")
		}
		if unfilled != "" {
			t.Errorf("unfilled = %q, want empty at 10/10 progress", unfilled)
		}
	})

	t.Run("partial progress is reflected proportionally", func(t *testing.T) {
		filledLow, _, _ := extractTopBarFill(t, call(1, 10, false))
		filledHigh, _, _ := extractTopBarFill(t, call(9, 10, false))
		if len([]rune(filledHigh)) <= len([]rune(filledLow)) {
			t.Errorf("filled width did not grow with progress: 1/10 -> %d runes, 9/10 -> %d runes",
				len([]rune(filledLow)), len([]rune(filledHigh)))
		}
	})

	t.Run("frozen snaps the fill to 100% even if the tracker undercounted", func(t *testing.T) {
		_, unfilled, hasFill := extractTopBarFill(t, call(3, 10, true)) // frozen, well short of the total
		if !hasFill {
			t.Fatal("want the fill form")
		}
		if unfilled != "" {
			t.Errorf("unfilled = %q, want empty (100%% fill) once frozen, regardless of an undercounted position", unfilled)
		}
	})

	t.Run("the 'Task x/y' text itself stays honest, not clamped, once frozen", func(t *testing.T) {
		got := call(3, 10, true)
		if !strings.Contains(got, "Task 3/10") {
			t.Errorf("topBarText() = %q, want the literal, un-clamped 'Task 3/10' text even though the fill snaps to 100%%", got)
		}
	})

	t.Run("external content shaped like a tag is escaped, not corrupted", func(t *testing.T) {
		got := topBarText("weird[name]", false, nil, 0, false, filterQuery{}, 0, 0, 100, "navy", true)
		if strings.Contains(got, "weird[name]") {
			t.Errorf("topBarText() = %q, contains an un-escaped tag-shaped bracket", got)
		}
		if !strings.Contains(got, "weird[name[]") {
			t.Errorf("topBarText() = %q, want the tview.Escape()'d form 'weird[name[]'", got)
		}
	})

	t.Run("showElapsed false drops the clock entirely - design-docs/Revisit.md", func(t *testing.T) {
		got := topBarText("site.yml", false, nil, 0, true, filterQuery{}, 0, 0, 100, "navy", false)
		if strings.Contains(got, "00:00") {
			t.Errorf("topBarText(showElapsed=false) = %q, want no elapsed clock at all", got)
		}
	})

	t.Run("showElapsed false still shows a real Task x/y prefix if there is one", func(t *testing.T) {
		got := topBarText("site.yml", false, nil, 0, true, filterQuery{}, 3, 10, 100, "navy", false)
		if !strings.Contains(got, "Task 3/10") {
			t.Errorf("topBarText(showElapsed=false) = %q, want the Task x/y prefix kept", got)
		}
		if strings.Contains(got, "00:00") {
			t.Errorf("topBarText(showElapsed=false) = %q, want no elapsed clock alongside it", got)
		}
	})
}
