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
	"encoding/json"
	"testing"
)

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
		x, y := varPopupPlace(c.ax, c.ay, c.w, c.h, c.sw, c.sh)
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

func TestVarPopupValueText(t *testing.T) {
	if got := varPopupValueText(vaultPlaceholder); got != "<vaulted>" {
		t.Errorf("placeholder: %q", got)
	}
	if got := varPopupValueText("hello"); got != `"hello"` {
		t.Errorf("string: %q", got)
	}
	if got := varPopupValueText(float64(8080)); got != "8080" {
		t.Errorf("number: %q", got)
	}
}

// A vaulted value (and a secret inside a derived string) is masked before
// the pop-up evaluates a word, so it can only ever show the placeholder.
func TestVarPopupMasksVaulted(t *testing.T) {
	vars := json.RawMessage(`{"pw":"hunter2hunter2","url":"db://hunter2hunter2@host","plain":"ok"}`)
	masked := maskSnapshot(vars, vaultMask{Vaulted: [][]any{{"pw"}}, Secrets: []string{"hunter2hunter2"}})
	for expr, want := range map[string]string{"pw": "<vaulted>", "url": "db://<vaulted>@host", "plain": `"ok"`} {
		res := evaluateWatchExpr(expr, masked)
		if res.kind != watchResolved {
			t.Fatalf("%s: not resolved", expr)
		}
		got := varPopupValueText(res.value)
		if expr == "url" {
			got = res.value.(string)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}
