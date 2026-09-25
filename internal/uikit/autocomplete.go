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

import "strings"

// AutocompleteMaxEntries caps how many suggestions any autocomplete
// drop-down in the app shows at once (design-docs/Autocomplete.md) - an
// arbitrary, fixed budget shared by every field that wires FilteredMatch,
// wherever it lives: the re-run dialog's own multi-value Tags/Skip
// tags/Hosts fields (internal/session), and the template Verb's own
// single-value host/group field (internal/template, design-docs/Tangsible
// template.md).
const AutocompleteMaxEntries = 8

// FilteredMatch is the core case-insensitive matcher every autocomplete
// field in the app shares: a prefix match against token wins; if nothing
// has that prefix, a substring match instead. Any candidate present in
// exclude (nil is fine) is skipped. Capped at AutocompleteMaxEntries -
// suggestions are purely advisory, so truncation here only ever means
// "fewer choices shown," never "a valid value became unavailable" - the
// user can still always just finish typing it themselves.
func FilteredMatch(candidates []string, token string, exclude map[string]bool) []string {
	lowerToken := strings.ToLower(token)

	var prefixMatches, substringMatches []string
	for _, c := range candidates {
		if exclude[c] {
			continue
		}
		lc := strings.ToLower(c)
		switch {
		case strings.HasPrefix(lc, lowerToken):
			prefixMatches = append(prefixMatches, c)
		case strings.Contains(lc, lowerToken):
			substringMatches = append(substringMatches, c)
		}
	}

	matches := prefixMatches
	if len(matches) == 0 {
		matches = substringMatches
	}
	if len(matches) > AutocompleteMaxEntries {
		matches = matches[:AutocompleteMaxEntries]
	}
	return matches
}

// MatchSingleValue returns candidates matching text as a whole - for a
// field with exactly one value and no comma-separated token to isolate
// (the re-run dialog's "Start with play" field, and the template Verb's
// own host/group field). An empty/whitespace-only text returns nil, same
// "no drop-down until the user has actually started typing something"
// rule FilteredMatch's own multi-value callers apply via their own empty-
// token check.
//
// A text that already exactly (case-insensitively) equals one of the
// candidates also returns nil - a real bug hit live, not a hypothetical:
// a plain whole-field-replace apply function (no trailing ", " the way a
// comma-separated field's own apply leaves, which is what makes its next
// match empty right after a pick) leaves behind text that would otherwise
// keep self-matching on the very next call, permanently stealing Enter
// away from whatever "submit" means in that field's own dialog.
func MatchSingleValue(candidates []string, text string) []string {
	token := strings.TrimSpace(text)
	if token == "" {
		return nil
	}
	lowerToken := strings.ToLower(token)
	for _, c := range candidates {
		if strings.ToLower(c) == lowerToken {
			return nil
		}
	}
	return FilteredMatch(candidates, token, nil)
}
