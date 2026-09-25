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
	"fmt"
	"slices"
	"testing"
)

func TestFilteredMatch(t *testing.T) {
	candidates := []string{"web1", "web2", "webdb", "database", "always"}

	cases := []struct {
		name    string
		token   string
		exclude map[string]bool
		want    []string
	}{
		{"prefix match wins over substring", "web", nil, []string{"web1", "web2", "webdb"}},
		{"substring fallback when no prefix matches", "data", nil, []string{"database"}},
		{"excluded candidates are skipped", "web", map[string]bool{"web1": true}, []string{"web2", "webdb"}},
		{"no match at all", "zzz", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FilteredMatch(candidates, c.token, c.exclude)
			if !slices.Equal(got, c.want) {
				t.Errorf("FilteredMatch(%v, %q, %v) = %v, want %v", candidates, c.token, c.exclude, got, c.want)
			}
		})
	}

	t.Run("capped at AutocompleteMaxEntries", func(t *testing.T) {
		var many []string
		for i := 0; i < AutocompleteMaxEntries+5; i++ {
			many = append(many, fmt.Sprintf("task%d", i))
		}
		got := FilteredMatch(many, "task", nil)
		if len(got) != AutocompleteMaxEntries {
			t.Fatalf("FilteredMatch returned %d entries, want %d (capped)", len(got), AutocompleteMaxEntries)
		}
	})
}

func TestMatchSingleValue(t *testing.T) {
	candidates := []string{"web1", "web2", "database"}

	cases := []struct {
		name string
		text string
		want []string
	}{
		{"empty text, no suggestions", "", nil},
		{"whitespace-only text, no suggestions", "   ", nil},
		{"prefix match", "web", []string{"web1", "web2"}},
		{"exact match (any case) suppresses further suggestions", "Web1", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MatchSingleValue(candidates, c.text)
			if !slices.Equal(got, c.want) {
				t.Errorf("MatchSingleValue(%v, %q) = %v, want %v", candidates, c.text, got, c.want)
			}
		})
	}
}
