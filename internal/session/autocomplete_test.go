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
	"reflect"
	"testing"
)

func TestLastToken(t *testing.T) {
	cases := []struct {
		text        string
		wantToken   string
		wantEarlier []string
	}{
		{"", "", nil},
		{"web", "web", nil},
		{"web1, web2, we", "we", []string{"web1", "web2"}},
		{"web1,web2,", "", []string{"web1", "web2"}},
		{"web1, , web2", "web2", []string{"web1"}},
		{" web1 ", "web1", nil},
	}
	for _, c := range cases {
		gotToken, gotEarlier := lastToken(c.text)
		if gotToken != c.wantToken {
			t.Errorf("lastToken(%q) token = %q, want %q", c.text, gotToken, c.wantToken)
		}
		for _, e := range c.wantEarlier {
			if !gotEarlier[e] {
				t.Errorf("lastToken(%q) earlier missing %q, got %v", c.text, e, gotEarlier)
			}
		}
		if len(gotEarlier) != len(c.wantEarlier) {
			t.Errorf("lastToken(%q) earlier = %v, want %v", c.text, gotEarlier, c.wantEarlier)
		}
	}
}

func TestMatchToken(t *testing.T) {
	candidates := []string{"web1", "web2", "webdb", "database", "always"}

	cases := []struct {
		name string
		text string
		want []string
	}{
		{"empty token, no suggestions", "", nil},
		{"empty token after trailing comma", "web1, ", nil},
		{"prefix match, case-insensitive", "WEB", []string{"web1", "web2", "webdb"}},
		{"substring fallback when no prefix match", "atab", []string{"database"}},
		{"already-present token excluded", "web1, web", []string{"web2", "webdb"}},
		{"no match at all", "zzz", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchToken(candidates, c.text)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("matchToken(%v, %q) = %v, want %v", candidates, c.text, got, c.want)
			}
		})
	}
}

func TestMatchTokenCapsAtMaxEntries(t *testing.T) {
	var candidates []string
	for i := 0; i < autocompleteMaxEntries+5; i++ {
		candidates = append(candidates, "host"+string(rune('a'+i)))
	}
	got := matchToken(candidates, "host")
	if len(got) != autocompleteMaxEntries {
		t.Fatalf("matchToken returned %d entries, want %d (capped)", len(got), autocompleteMaxEntries)
	}
}

func TestReplaceLastToken(t *testing.T) {
	cases := []struct {
		text        string
		replacement string
		want        string
	}{
		{"", "web1", "web1, "},
		{"web1, we", "web2", "web1, web2, "},
		{"web1,we", "web2", "web1, web2, "},
		{"we", "web1", "web1, "},
	}
	for _, c := range cases {
		got := replaceLastToken(c.text, c.replacement)
		if got != c.want {
			t.Errorf("replaceLastToken(%q, %q) = %q, want %q", c.text, c.replacement, got, c.want)
		}
	}
}
