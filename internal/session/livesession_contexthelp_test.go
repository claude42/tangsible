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
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/ansibledoc"
)

func TestOptionPopupRows(t *testing.T) {
	o := &ansibledoc.OptionDoc{
		Description: strings.Repeat("word ", 60), Type: "str", Required: true,
		Default: "safe", HasDefault: true, Choices: []string{"all", "none", "safe"},
	}
	p := optionPopup("follow_redirects", o)
	var labels []string
	for _, r := range p.rows {
		labels = append(labels, r.label)
	}
	if got := strings.Join(labels, " "); got != "Name: Type: Required: Default: Choices: About:" {
		t.Errorf("rows: %s", got)
	}
	about := p.rows[len(p.rows)-1].text
	if n := strings.Count(about, "\n") + 1; n != popupDescLines || !strings.HasSuffix(about, "…") {
		t.Errorf("description not capped to %d lines with an ellipsis: %q", popupDescLines, about)
	}
	if p.button != "" {
		t.Error("context help has no button")
	}

	bare := optionPopup("x", &ansibledoc.OptionDoc{Type: "int"})
	if len(bare.rows) != 2 {
		t.Errorf("an option with nothing else shows name and type only: %+v", bare.rows)
	}
}

func TestKeywordAndModulePopupRows(t *testing.T) {
	k := keywordPopup("when", ansibledoc.KeywordDoc{Description: "Conditional.", Type: "string", AppliesTo: []string{"Block", "Task"}})
	if len(k.rows) != 4 || k.rows[2].text != "Block, Task" {
		t.Errorf("keyword: %+v", k.rows)
	}
	m := modulePopup(&ansibledoc.ModuleDoc{Name: "ansible.builtin.copy", ShortDescription: "Copy files to remote locations"})
	if len(m.rows) != 2 || m.rows[0].text != "ansible.builtin.copy" || m.title != " Module " {
		t.Errorf("module: %+v", m)
	}
}

func TestKeyLinesMatching(t *testing.T) {
	lines := []string{
		"- name: x",
		"  copy:",
		"    dest: /a/very/long/path/that/the/view/wraps",
		"    mode: \"0644\"",
		"    mode: \"0644\"",
	}
	cases := []struct {
		name  string
		row   string
		start int
		word  string
		want  []int
	}{
		{"plain line", "    mode: \"0644\"   ", 4, "mode", []int{4, 5}}, // identical lines: both candidates
		{"wrapped line's first chunk", "    dest: /a/very/long/path", 4, "dest", []int{3}},
		{"a continuation chunk is not a line start", "that/the/view/wraps", 0, "that", nil},
		{"word not at that column", "    mode: \"0644\"", 5, "mode", nil},
		{"not followed by a colon", "- name: x", 2, "nam", nil},
		{"blank row", "    ", 4, "x", nil},
	}
	for _, c := range cases {
		got := keyLinesMatching(lines, c.row, c.start, c.word)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}
