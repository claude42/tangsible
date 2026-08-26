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
// Task/Tags/Skip tags/Hosts fields will ever show at once (design-docs/
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
// started typing something. Candidates already present earlier in text
// are excluded. See filteredMatch for the matching/capping rules
// themselves, shared with matchTaskName below.
func matchToken(candidates []string, text string) []string {
	token, earlier := lastToken(text)
	if token == "" {
		return nil
	}
	return filteredMatch(candidates, token, earlier)
}

// matchTaskName returns candidates matching text as a whole - the re-run
// dialog's own SetAutocompleteFunc callback for its single-valued "Start
// with task" field (design-docs/Autocomplete.md), unlike
// tagsField/skipTagsField/hostsField above: there's no comma-separated
// token to isolate, and no "earlier" entries to exclude (a task is only
// ever entered once). An empty/whitespace-only text returns nil, same
// "no drop-down until the user has actually started typing" rule as
// matchToken.
//
// A text that already exactly (case-insensitively) equals one of the
// candidates also returns nil - a real bug hit live, not a hypothetical:
// wireAutocomplete's own apply func for this field is a plain whole-field
// replace (no trailing ", " the way replaceLastToken leaves for the
// comma-separated fields, which is what makes their own next match empty
// right after a pick), so without this check, the text left behind by
// picking "changed task" still self-matches "changed task" on the very
// next SetAutocompleteFunc call. autocompleteOpenNow (tui.go) would then
// keep reporting the drop-down as open even though tview's own
// autocompleteList is already nil, permanently stealing that field's
// Enter key away from submitRerun.
func matchTaskName(candidates []string, text string) []string {
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
	return filteredMatch(candidates, token, nil)
}

// filteredMatch is matchToken/matchTaskName's shared core: case-insensitive
// prefix match against token wins; if nothing has that prefix, a substring
// match instead. Any candidate present in exclude (nil is fine - matchTaskName
// has nothing to exclude) is skipped. Capped at autocompleteMaxEntries;
// suggestions are purely advisory, so truncation here only ever means
// "fewer choices shown," never "a valid value became unavailable" - the
// user can still always just finish typing it themselves.
func filteredMatch(candidates []string, token string, exclude map[string]bool) []string {
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
