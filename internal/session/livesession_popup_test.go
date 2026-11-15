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

import "testing"

func TestWordAround(t *testing.T) {
	line := []rune("  msg: {{ foo.bar_1 }}. done")
	cases := []struct {
		col  int
		want string
	}{
		{2, "msg"},
		{4, "msg"},
		{5, ""}, // the colon
		{9, ""}, // space before the word
		{10, "foo.bar_1"},
		{18, "foo.bar_1"}, // last character of the word
		{19, ""},          // the space after it
		{22, ""},          // the sentence dot after "}}"
		{24, "done"},
		{-1, ""},
		{99, ""},
	}
	for _, c := range cases {
		if got := wordAround(line, c.col); got != c.want {
			t.Errorf("wordAround(col %d) = %q, want %q", c.col, got, c.want)
		}
	}
	if got := wordAround([]rune("x foo. y"), 3); got != "foo" {
		t.Errorf("trailing dot kept: %q", got)
	}
}

func TestVarPopupPlace(t *testing.T) {
	cases := []struct {
		name                 string
		ax, ay, w, h, sw, sh int
		wantX, wantY         int
	}{
		{"below the click", 10, 5, 20, 6, 80, 24, 10, 6},
		{"shifted left at the right edge", 70, 5, 20, 6, 80, 24, 60, 6},
		{"flipped above at the bottom", 10, 22, 20, 6, 80, 24, 10, 16},
		{"wider than the screen", 3, 5, 100, 6, 80, 24, 0, 6},
		{"taller than the room either way", 10, 2, 20, 30, 80, 24, 10, 0},
	}
	for _, c := range cases {
		x, y := popupPlace(c.ax, c.ay, c.w, c.h, c.sw, c.sh)
		if x != c.wantX || y != c.wantY {
			t.Errorf("%s: got (%d,%d), want (%d,%d)", c.name, x, y, c.wantX, c.wantY)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abc", 5); got != "abc" {
		t.Errorf("short: %q", got)
	}
	if got := truncateRunes("äbcdef", 4); got != "äbc…" {
		t.Errorf("long: %q", got)
	}
}

func TestTruncateRunesLeft(t *testing.T) {
	if got := truncateRunesLeft("a/b.yml:5", 20); got != "a/b.yml:5" {
		t.Errorf("short: %q", got)
	}
	if got := truncateRunesLeft("/long/dir/vars.yml:5", 12); got != "…/vars.yml:5" {
		t.Errorf("long: %q", got)
	}
}

func TestWordSpan(t *testing.T) {
	line := []rune("  src: a.txt.")
	for col, want := range map[int][2]int{2: {2, 4}, 4: {2, 4}, 7: {7, 11}, 11: {7, 11}} {
		start, end, ok := wordSpan(line, col)
		if !ok || start != want[0] || end != want[1] {
			t.Errorf("col %d: (%d,%d,%v), want %v", col, start, end, ok, want)
		}
	}
	for _, col := range []int{0, 5, 6, 12, 99} { // spaces, colon, the trailing dot, out of range
		if _, _, ok := wordSpan(line, col); ok {
			t.Errorf("col %d should not be a word", col)
		}
	}
}

func TestWrapText(t *testing.T) {
	if got := wrapText("one two three four", 9, 4); got != "one two\nthree\nfour" {
		t.Errorf("wrap: %q", got)
	}
	if got := wrapText("aa bb cc dd ee ff", 5, 2); got != "aa bb\ncc d…" {
		t.Errorf("cap: %q", got)
	}
	if got := wrapText("aaaaa bbbbb", 5, 1); got != "aaaa…" {
		t.Errorf("cap at full width: %q", got)
	}
	if got := wrapText("supercalifragilistic", 8, 4); got != "superca…" {
		t.Errorf("long word: %q", got)
	}
	if got := wrapText("  ", 8, 4); got != "" {
		t.Errorf("blank: %q", got)
	}
}
