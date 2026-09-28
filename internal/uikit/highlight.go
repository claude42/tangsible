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
	"regexp"
	"strings"

	"github.com/rivo/tview"
)

// The regex-based, line-by-line "key: value" highlighters below all
// started life in internal/host/host.go (design-docs/HostVerb.md's own
// cosmetic pass) and moved here once the run drill-down's own tabs
// (design-docs/Colors.md) needed the identical green-key/lightsteelblue-
// value treatment - one shared implementation, not two copies drifting
// apart. None of these are a real parser; each is "good enough" for the
// shapes this app itself actually generates, same caveat YamlKeyLine's
// own doc comment already states.

var jsonKeyLine = regexp.MustCompile(`^(\s*)("(?:[^"\\]|\\.)*")(:)(\s.*|)$`)

// jsonValueLine matches a line that's nothing but a bare JSON value, no
// key at all - the shape a line inside a JSON array takes (a string,
// number, or true/false/null element, optionally trailing-comma'd).
// jsonKeyLine's own "key": value shape never matches these (no colon at
// all), which would otherwise leave every array element fully unstyled -
// only an array's own opening "[" (part of its owning key's own value,
// jsonKeyLine's m[4]) gets colored without this.
var jsonValueLine = regexp.MustCompile(`^(\s*)("(?:[^"\\]|\\.)*"|-?\d+(?:\.\d+)?|true|false|null)(,?)$`)

// HighlightJSONKeys highlights each line's own quoted key green, and
// every value - whether it's a key's own "key": value (jsonKeyLine) or a
// bare array element with no key of its own (jsonValueLine) -
// lightsteelblue. A structural-only line (an opening/closing brace or
// bracket with nothing else on it) matches neither and stays unstyled.
func HighlightJSONKeys(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if m := jsonKeyLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[" + KeyHighlightColor + "]" + tview.Escape(m[2]+m[3]) + "[-]" + "[" + ValueHighlightColor + "]" + tview.Escape(m[4]) + "[-]"
			continue
		}
		if m := jsonValueLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[" + ValueHighlightColor + "]" + tview.Escape(m[2]) + "[-]" + tview.Escape(m[3])
			continue
		}
		lines[i] = tview.Escape(line)
	}
	return strings.Join(lines, "\n")
}

// yamlListItemLine matches a bare block-sequence item with no key of its
// own - "- <value>" (any leading indentation) - the shape a YAML list
// holds when its entries are plain scalars or flow-lists ("- alpha",
// "- [a, b]") rather than nested mappings. A mapping entry ("- key:
// value") is already handled by YamlKeyLine itself, whose own
// leading-marker group already accepts an optional "- " prefix - this
// one only ever gets a chance on a line YamlKeyLine has already failed to
// match, i.e. one with no "key:" shape at all.
var yamlListItemLine = regexp.MustCompile(`^(\s*-\s+)(.+)$`)

// yamlBlockScalarOpener matches a key/list-item's own value when it's
// nothing but a block-scalar indicator - "|" or ">", optionally
// chomp-marked ("+"/"-") and/or preceded by a YAML tag (most commonly
// "!vault") - meaning every subsequent, more-indented (or blank) line is
// that one value's own raw continuation text, not a further nested
// key/list-item of its own.
var yamlBlockScalarOpener = regexp.MustCompile(`^(![^\s]+\s+)?[|>][+-]?\d*$`)

// HighlightYAMLKeys highlights each line's own "key:" portion (or, for a
// bare list item, its whole value - yamlListItemLine) green for the key,
// lightsteelblue for the value - the same regex-based approach
// YamlKeyLine's own doc comment already describes. blockScalarIndent
// tracks whether the previous key/list-item's own value opened a raw
// multi-line scalar (yamlBlockScalarOpener, e.g. an ansible-vault-shaped
// "somekey: !vault |" followed by an indented, multi-line blob) - every
// line more indented than that (or blank) is colored as that same
// value's own continuation instead of being re-matched against the
// key/list-item patterns below, until indentation drops back to the
// opener's own level or shallower.
func HighlightYAMLKeys(raw string) string {
	lines := strings.Split(raw, "\n")
	blockScalarIndent := -1
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		trimmed := strings.TrimSpace(line)

		if blockScalarIndent != -1 {
			if trimmed == "" || indent > blockScalarIndent {
				lines[i] = "[" + ValueHighlightColor + "]" + tview.Escape(line) + "[-]"
				continue
			}
			blockScalarIndent = -1
		}

		if m := YamlKeyLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[" + KeyHighlightColor + "]" + tview.Escape(m[2]+m[3]) + "[-]" + "[" + ValueHighlightColor + "]" + tview.Escape(m[4]) + "[-]"
			if yamlBlockScalarOpener.MatchString(strings.TrimSpace(m[4])) {
				blockScalarIndent = indent
			}
			continue
		}
		if m := yamlListItemLine.FindStringSubmatch(line); m != nil {
			lines[i] = tview.Escape(m[1]) + "[" + ValueHighlightColor + "]" + tview.Escape(m[2]) + "[-]"
			if yamlBlockScalarOpener.MatchString(strings.TrimSpace(m[2])) {
				blockScalarIndent = indent
			}
			continue
		}
		lines[i] = tview.Escape(line)
	}
	return strings.Join(lines, "\n")
}

// KeyValueLine matches a plain "Label: value" line - a label of letters/
// digits/spaces (so a multi-word label like "SSH public key" still
// matches), a colon, then the rest of the line. Deliberately not scoped
// to any specific known label set: the drill-down's own Output section
// mixes free-form module stdout/msg text with a handful of genuinely
// label-shaped lines this app generates itself (AdditionalOutputLines'
// own Filename:/Command:/User:/SSH public key: lines) - this matches the
// latter and leaves ordinary prose alone, on the same "good enough, not
// guaranteed to avoid every false positive" terms as every other
// highlighter in this file.
var KeyValueLine = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9 ]*):(\s.*|)$`)

// HighlightKeyValueLines highlights each KeyValueLine-shaped line's own
// label green and its value lightsteelblue; a line that doesn't match
// (ordinary prose, a shell command's own stdout) passes through
// unstyled.
func HighlightKeyValueLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		m := KeyValueLine.FindStringSubmatch(line)
		if m == nil {
			lines[i] = tview.Escape(line)
			continue
		}
		lines[i] = "[" + KeyHighlightColor + "]" + tview.Escape(m[1]) + ":[-]" + "[" + ValueHighlightColor + "]" + tview.Escape(m[2]) + "[-]"
	}
	return strings.Join(lines, "\n")
}

// SplitRolePrefix splits a role-sourced task's own displayed name
// ("<role> : <task>") into its role and task parts. Confirmed
// empirically, both against a real role-using playbook's own
// --list-tasks output and the live jsonl event's own task.name field
// directly: ansible-core prefixes a role-sourced task's name with its
// role and " : " unconditionally, whether or not the task has its own
// explicit name: (an unnamed one gets "<role> : <module>" instead - the
// same mechanism, not a separate one). Returns ("", name) unchanged when
// name carries no such prefix at all - a task defined directly in a
// play.
func SplitRolePrefix(name string) (role, task string) {
	if idx := strings.Index(name, " : "); idx != -1 {
		return name[:idx], name[idx+len(" : "):]
	}
	return "", name
}
