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

package ansibledoc

import "testing"

const taskSrc = `    - name: install it
      ansible.builtin.copy:
        src: a.txt
        dest: /etc/a
        validate:
          cmd: check %s
      when: ok
      vars:
        src: not an option
`

func TestLocateTaskKey(t *testing.T) {
	cases := []struct {
		name         string
		line, col    int
		word, action string
		want         KeyRef
	}{
		{"task keyword", 1, 7, "name", "copy", KeyRef{KeyKeyword, []string{"name"}}},
		{"module key, FQCN in source", 2, 7, "ansible.builtin.copy", "copy", KeyRef{KeyModule, []string{"ansible.builtin.copy"}}},
		{"option", 3, 9, "src", "ansible.builtin.copy", KeyRef{KeyOption, []string{"src"}}},
		{"second option", 4, 9, "dest", "copy", KeyRef{KeyOption, []string{"dest"}}},
		{"suboption", 6, 11, "cmd", "copy", KeyRef{KeyOption, []string{"validate", "cmd"}}},
		{"keyword after the module", 7, 7, "when", "copy", KeyRef{KeyKeyword, []string{"when"}}},
		{"key under vars is nothing", 9, 9, "src", "copy", KeyRef{}},
		{"a value, not a key", 3, 14, "a", "copy", KeyRef{}},
		{"wrong column", 3, 10, "src", "copy", KeyRef{}},
		{"unknown action: module key is just a keyword", 2, 7, "ansible.builtin.copy", "", KeyRef{KeyKeyword, []string{"ansible.builtin.copy"}}},
		{"different collection is another module", 2, 7, "ansible.builtin.copy", "community.general.copy", KeyRef{KeyKeyword, []string{"ansible.builtin.copy"}}},
	}
	for _, c := range cases {
		got := LocateTaskKey(taskSrc, c.line, c.col, c.word, c.action)
		if got.Kind != c.want.Kind || len(got.Path) != len(c.want.Path) || (len(got.Path) > 0 && got.Path[len(got.Path)-1] != c.want.Path[len(c.want.Path)-1]) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestLocateTaskKeyArgsAndSuboptionList(t *testing.T) {
	src := "- name: x\n  uri:\n    headers:\n      - name: h\n  args:\n    url: u\n"
	if got := LocateTaskKey(src, 4, 9, "name", "uri"); got.Kind != KeyOption || len(got.Path) != 2 || got.Path[0] != "headers" || got.Path[1] != "name" {
		t.Errorf("list-of-mappings suboption: %+v", got)
	}
	if got := LocateTaskKey(src, 6, 5, "url", "uri"); got.Kind != KeyOption || got.Path[0] != "url" {
		t.Errorf("args: option: %+v", got)
	}
}

func TestLocateTaskKeyBadInput(t *testing.T) {
	for _, src := range []string{"", "just text", "- [1, 2]\n", "a: [unclosed"} {
		if got := LocateTaskKey(src, 1, 1, "a", "x"); got.Kind != KeyNone && src != "a: [unclosed" {
			t.Errorf("%q: %+v", src, got)
		}
	}
}
