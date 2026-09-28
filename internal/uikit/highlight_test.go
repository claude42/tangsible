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

import "testing"

func TestHighlightYAMLKeys(t *testing.T) {
	got := HighlightYAMLKeys("name: install packages\n  - foo: bar\n# a comment\n")
	want := "[green]name:[-][lightsteelblue] install packages[-]\n  - [green]foo:[-][lightsteelblue] bar[-]\n# a comment\n"
	if got != want {
		t.Errorf("HighlightYAMLKeys() = %q, want %q", got, want)
	}
}

func TestHighlightJSONKeys(t *testing.T) {
	got := HighlightJSONKeys(`{
    "ansible_host": "192.0.2.254",
    "nested": {
        "inner": 1
    }
}`)
	want := "{\n    [green]\"ansible_host\":[-][lightsteelblue] \"192.0.2.254\",[-]\n    [green]\"nested\":[-][lightsteelblue] {[-]\n        [green]\"inner\":[-][lightsteelblue] 1[-]\n    }\n}"
	if got != want {
		t.Errorf("HighlightJSONKeys() = %q, want %q", got, want)
	}
}

// TestHighlightYAMLKeys_ListItems confirms a bare block-sequence item
// (no key of its own, e.g. "- alpha") gets its value highlighted too -
// previously only a list's own flow-style opening (attached to its
// owning key) was colored at all.
func TestHighlightYAMLKeys_ListItems(t *testing.T) {
	got := HighlightYAMLKeys("mylist:\n  - alpha\n  - beta\n")
	want := "[green]mylist:[-][lightsteelblue][-]\n  - [lightsteelblue]alpha[-]\n  - [lightsteelblue]beta[-]\n"
	if got != want {
		t.Errorf("HighlightYAMLKeys() = %q, want %q", got, want)
	}
}

// TestHighlightYAMLKeys_BlockScalar confirms an ansible-vault-shaped
// multi-line block scalar ("key: !vault |" followed by more-indented
// continuation lines) gets its whole value highlighted, not just the
// "!vault |" opener itself - and that a later, equal-or-lower-indented
// key correctly ends the continuation and resumes normal key/value
// highlighting.
func TestHighlightYAMLKeys_BlockScalar(t *testing.T) {
	got := HighlightYAMLKeys("secret: !vault |\n  line one\n  line two\nplain: value\n")
	want := "[green]secret:[-][lightsteelblue] !vault |[-]\n" +
		"[lightsteelblue]  line one[-]\n" +
		"[lightsteelblue]  line two[-]\n" +
		"[green]plain:[-][lightsteelblue] value[-]\n"
	if got != want {
		t.Errorf("HighlightYAMLKeys() = %q, want %q", got, want)
	}
}

// TestHighlightJSONKeys_ArrayElements confirms a bare array element (no
// key of its own) gets highlighted too - previously only an array's own
// opening "[" (part of its owning key's own value) was ever colored.
func TestHighlightJSONKeys_ArrayElements(t *testing.T) {
	got := HighlightJSONKeys("{\n    \"tags\": [\n        \"web\",\n        \"prod\"\n    ]\n}")
	want := "{\n    [green]\"tags\":[-][lightsteelblue] [[-]\n        [lightsteelblue]\"web\"[-],\n        [lightsteelblue]\"prod\"[-]\n    ]\n}"
	if got != want {
		t.Errorf("HighlightJSONKeys() = %q, want %q", got, want)
	}
}

func TestHighlightKeyValueLines(t *testing.T) {
	got := HighlightKeyValueLines("Filename: /etc/hosts\nsome plain stdout text\nCommand: ls -la /tmp\n")
	want := "[green]Filename:[-][lightsteelblue] /etc/hosts[-]\n" +
		"some plain stdout text\n" +
		"[green]Command:[-][lightsteelblue] ls -la /tmp[-]\n"
	if got != want {
		t.Errorf("HighlightKeyValueLines() = %q, want %q", got, want)
	}
}

func TestSplitRolePrefix(t *testing.T) {
	if role, task := SplitRolePrefix("webserver : install packages"); role != "webserver" || task != "install packages" {
		t.Errorf("SplitRolePrefix() = (%q, %q), want (%q, %q)", role, task, "webserver", "install packages")
	}
	if role, task := SplitRolePrefix("say hi"); role != "" || task != "say hi" {
		t.Errorf("SplitRolePrefix() = (%q, %q), want (%q, %q)", role, task, "", "say hi")
	}
}
