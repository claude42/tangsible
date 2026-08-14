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

	"github.com/rivo/tview"
)

func TestBuildDiffTabNoDiffKey(t *testing.T) {
	if got := buildDiffTab(map[string]interface{}{"changed": true}); got != "" {
		t.Errorf("buildDiffTab() = %q, want empty (no diff key at all)", got)
	}
}

func TestBuildDiffTabEmptyDiff(t *testing.T) {
	cases := []struct {
		name string
		diff interface{}
	}{
		{"empty dict", map[string]interface{}{}},
		{"empty list", []interface{}{}},
		{"nil", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			decoded := map[string]interface{}{"diff": c.diff}
			if got := buildDiffTab(decoded); got != "" {
				t.Errorf("buildDiffTab() = %q, want empty", got)
			}
		})
	}
}

func TestBuildDiffTabSimpleChange(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": map[string]interface{}{
			"before": "line one\nline two\n",
			"after":  "line one\nline three\n",
		},
	}
	got := buildDiffTab(decoded)
	if got == "" {
		t.Fatal("buildDiffTab() = empty, want a rendered diff")
	}
	if !strings.Contains(got, "[teal]@@") {
		t.Errorf("buildDiffTab() = %q, want a [teal]-tagged @@ hunk header", got)
	}
	if !strings.Contains(got, "[red]-line two") {
		t.Errorf("buildDiffTab() = %q, want a [red]-tagged removed line", got)
	}
	if !strings.Contains(got, "[green]+line three") {
		t.Errorf("buildDiffTab() = %q, want a [green]-tagged added line", got)
	}
}

func TestBuildDiffTabIdenticalBeforeAfter(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": map[string]interface{}{
			"before": "same\n",
			"after":  "same\n",
		},
	}
	if got := buildDiffTab(decoded); got != "" {
		t.Errorf("buildDiffTab() = %q, want empty (no real change)", got)
	}
}

func TestBuildDiffTabMissingAndNilFields(t *testing.T) {
	cases := []struct {
		name string
		diff map[string]interface{}
	}{
		{"after missing", map[string]interface{}{"before": "x\n"}},
		{"before nil, after nil", map[string]interface{}{"before": nil, "after": nil}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			decoded := map[string]interface{}{"diff": c.diff}
			// Must not panic; content doesn't matter for these cases.
			_ = buildDiffTab(decoded)
		})
	}
}

func TestBuildDiffTabBinaryNotice(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": map[string]interface{}{
			"dst_binary": 1.0,
		},
	}
	got := buildDiffTab(decoded)
	if !strings.Contains(got, "destination file appears to be binary") {
		t.Errorf("buildDiffTab() = %q, want a binary-skip notice", got)
	}
}

func TestBuildDiffTabListOfEntries(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": []interface{}{
			map[string]interface{}{"before": "a\n", "after": "b\n"},
			map[string]interface{}{"before": "c\n", "after": "d\n"},
		},
	}
	got := buildDiffTab(decoded)
	if !strings.Contains(got, "-a") || !strings.Contains(got, "+b") {
		t.Errorf("buildDiffTab() = %q, missing first entry's diff", got)
	}
	if !strings.Contains(got, "-c") || !strings.Contains(got, "+d") {
		t.Errorf("buildDiffTab() = %q, missing second entry's diff", got)
	}
}

func TestBuildDiffTabPrepared(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": map[string]interface{}{
			"prepared": "--- literal preformatted diff text ---",
		},
	}
	got := buildDiffTab(decoded)
	if !strings.Contains(got, "literal preformatted diff text") {
		t.Errorf("buildDiffTab() = %q, want the prepared text verbatim", got)
	}
}

func TestBuildDiffTabNonStringBeforeAfter(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": map[string]interface{}{
			"before": map[string]interface{}{"mode": "0644"},
			"after":  map[string]interface{}{"mode": "0755"},
		},
	}
	got := buildDiffTab(decoded)
	if !strings.Contains(got, "0644") || !strings.Contains(got, "0755") {
		t.Errorf("buildDiffTab() = %q, want both JSON-rendered mode values present", got)
	}
}

func TestBuildDiffTabEscapesBrackets(t *testing.T) {
	decoded := map[string]interface{}{
		"diff": map[string]interface{}{
			"before": "tags: [a, b]\n",
			"after":  "tags: [a, b, c]\n",
		},
	}
	got := buildDiffTab(decoded)
	if strings.Contains(got, "[a, b, c]") {
		t.Errorf("buildDiffTab() = %q, want the literal bracket escaped, not left as an unescaped tag", got)
	}
	if !strings.Contains(got, tview.Escape("[a, b, c]")) {
		t.Errorf("buildDiffTab() = %q, want the escaped form of the added line's brackets", got)
	}
}
