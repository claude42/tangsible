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

import "strings"

// autocompleteMaxEntries caps how many suggestions the re-run dialog's
// Tags/Skip tags/Hosts fields will ever show at once (design-docs/
// Autocomplete.md) - an arbitrary, fixed budget for keeping the drop-down
// a small, predictable size, in the same spirit as this app's other fixed
// budgets (splitMinTotalWidth, maxHistoryPerPlaybook).
const autocompleteMaxEntries = 8

// lastToken splits text on its last comma - the shape every value in the
// re-run dialog's Tags/Skip tags/Hosts fields is entered in
// (design-docs/Autocomplete.md). token is the trimmed tail (the value
// currently being typed); earlier is the set of every other
// comma-separated value already present (trimmed, non-empty) - used to
// exclude values already picked from being suggested (and picked) again.
func lastToken(text string) (token string, earlier map[string]bool) {
	parts := strings.Split(text, ",")
	earlier = make(map[string]bool, len(parts))
	for _, p := range parts[:len(parts)-1] {
		if t := strings.TrimSpace(p); t != "" {
			earlier[t] = true
		}
	}
	token = strings.TrimSpace(parts[len(parts)-1])
	return token, earlier
}

// matchToken returns candidates matching the token currently being typed
// at the end of text (see lastToken) - the re-run dialog's own
// SetAutocompleteFunc callback for its Tags/Skip tags/Hosts fields
// (design-docs/Autocomplete.md). An empty token (bare focus, or right
// after a comma) returns nil - no drop-down until the user has actually
// started typing something. Case-insensitive prefix match wins; if
// nothing has that prefix, a substring match instead. Candidates already
// present earlier in text are excluded. Capped at autocompleteMaxEntries;
// suggestions are purely advisory, so truncation here only ever means
// "fewer choices shown," never "a valid value became unavailable" - the
// user can still always just finish typing it themselves.
func matchToken(candidates []string, text string) []string {
	token, earlier := lastToken(text)
	if token == "" {
		return nil
	}
	lowerToken := strings.ToLower(token)

	var prefixMatches, substringMatches []string
	for _, c := range candidates {
		if earlier[c] {
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
	if len(matches) > autocompleteMaxEntries {
		matches = matches[:autocompleteMaxEntries]
	}
	return matches
}

// replaceLastToken swaps the token currently being typed at the end of
// text (see lastToken) for replacement, and appends ", " so the field is
// left ready for the next entry - the re-run dialog's own
// SetAutocompletedFunc for its Tags/Skip tags/Hosts fields
// (design-docs/Autocomplete.md), replacing tview's own default behavior
// (which replaces the *whole* field with the picked entry) with "only the
// token being completed." Everything before the last comma is carried
// through untouched.
func replaceLastToken(text, replacement string) string {
	prefix := ""
	if idx := strings.LastIndex(text, ","); idx >= 0 {
		prefix = strings.TrimRight(text[:idx], " ") + ", "
	}
	return prefix + replacement + ", "
}
