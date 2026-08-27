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

package source

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// TaskSourceIndex maps a task's Ansible-reported path ("<absolute
// file>:<line>", exactly matching RawEvent's task.path field) to its own
// raw YAML source text, verbatim from the file - not reformatted or
// re-serialized, so the user's own formatting/comments are preserved.
type TaskSourceIndex map[string]string

// blockTaskListKeys/playTaskListKeys name the mapping keys whose value is
// itself a task-list sequence, at the task level (nested inside a block:)
// and the play level respectively - shared by walkMappingForTaskLists.
var blockTaskListKeys = map[string]bool{"block": true, "rescue": true, "always": true}
var playTaskListKeys = map[string]bool{"tasks": true, "pre_tasks": true, "post_tasks": true, "handlers": true}

// ReservedTagNames are Ansible's own built-in tag values (they select
// behavior rather than naming a task the way a user-defined tag does) -
// always offered as autocomplete candidates alongside whatever tags are
// actually found in the playbook (design-docs/Autocomplete.md), since a
// playbook using none of them doesn't mean a user typing --tags never
// wants them.
var ReservedTagNames = []string{"always", "never", "tagged", "untagged", "all"}

// BuildTaskSourceIndex discovers every .yml/.yaml file under playbookPath's
// own directory tree (the playbook itself, plus any roles/** alongside it),
// indexes every task found in each by its own source location, and
// collects every literal tags: value it encounters along the way. Never
// fails outward - unreadable directories/files or YAML parse errors just
// mean less coverage, not a crash; a lookup miss at display time simply
// means no Task definition section for that entry (see tui.go's
// formatHostOutput).
//
// Deliberately does not trace the playbook's own roles:/import_tasks/
// include_tasks references to figure out which files to parse - instead,
// every YAML file found is independently classified by its own top-level
// shape (see indexFile), which covers role tasks and included files
// without needing to model Ansible's own include/role resolution at all.
// Confirmed empirically (a throwaway test playbook, not kept): even a
// templated include_tasks: "{{ a_var }}.yml" path is resolved by Ansible
// itself, which reports the real target file's own path in every event -
// so a plain map lookup by that reported path finds it regardless of how
// the file was reached.
//
// The second return value is the sorted, deduplicated union of every
// literal tag string found (design-docs/Autocomplete.md) with
// ReservedTagNames - confirmed empirically that Ansible's own jsonl event
// stream never carries a task's tags at all, so this static scan is the
// only way to source any tag-autocomplete candidates whatsoever, not just
// a nice-to-have alternative to a live source.
//
// The third return value is the sorted, deduplicated set of every literal
// task name: found the same way, for the re-run dialog's "Start with
// task" field (design-docs/Autocomplete.md) - unlike tags/hosts, task
// names *are* also visible live (PlaybookState.AllTasks), but the static
// scan is used here instead, deliberately: it's the same pass already
// paying for tags, needs no dependency on a generation having run yet
// (the rerun verb's very first dialog, before anything has run, would
// otherwise have nothing to suggest from), and doesn't miss a task whose
// own when: never let it execute in the run that happens to be on screen.
func BuildTaskSourceIndex(playbookPath string) (TaskSourceIndex, []string, []string) {
	index := TaskSourceIndex{}
	tagSet := map[string]bool{}
	for _, t := range ReservedTagNames {
		tagSet[t] = true
	}
	nameSet := map[string]bool{}

	abs, err := filepath.Abs(playbookPath)
	if err != nil {
		return index, sortedSet(tagSet), sortedSet(nameSet)
	}

	root := filepath.Dir(abs)
	files := []string{abs}

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry - skip it, not fatal to the walk
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if path == abs {
			return nil // already included above
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yml", ".yaml":
			files = append(files, path)
		}
		return nil
	})

	for _, f := range files {
		indexFile(f, index, tagSet, nameSet)
	}
	return index, sortedSet(tagSet), sortedSet(nameSet)
}

// topLevelPlay is one entry in a playbook file's own top-level sequence -
// shared by ListTopLevelPlayNames and TrimPlaybookToPlay
// (design-docs/StartWithPlay.md), so both agree on exactly what counts as
// "a play" and where it starts without parsing the file twice in two
// different ways.
type topLevelPlay struct {
	name      string
	startLine int // 1-indexed - the line the play's own mapping starts at.
}

// parseTopLevelPlays reads playbookPath and returns every *named* entry in
// its own top-level sequence, in file order. Returns nil - never an error -
// if the file can't be read/parsed, or its top-level shape isn't
// playbook-shaped (every item a mapping with its own "hosts" key, the same
// check indexFile already makes) - same "less coverage rather than a
// crash" posture as BuildTaskSourceIndex. A play with no literal "name:"
// (or a templated one) is silently skipped: v1's own scope note
// (design-docs/StartWithPlay.md) - there's nothing for a user to type or
// pick that would ever identify it, the same restraint collectTaskName
// already applies to a task with no literal name.
//
// Deliberately does not follow import_playbook - v1 only ever addresses a
// playbook's own top-level plays (design-docs/StartWithPlay.md).
func parseTopLevelPlays(playbookPath string) []topLevelPlay {
	data, err := os.ReadFile(playbookPath)
	if err != nil {
		return nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.SequenceNode || len(root.Content) == 0 {
		return nil
	}
	for _, item := range root.Content {
		if mappingValue(item, "hosts") == nil {
			return nil // not playbook-shaped
		}
	}

	var plays []topLevelPlay
	for _, item := range root.Content {
		val := mappingValue(item, "name")
		if val == nil || val.Kind != yaml.ScalarNode {
			continue
		}
		name := strings.TrimSpace(val.Value)
		if name == "" || strings.Contains(name, "{{") {
			continue
		}
		plays = append(plays, topLevelPlay{name: name, startLine: item.Line})
	}
	return plays
}

// ListTopLevelPlayNames returns the name of every named top-level play in
// playbookPath, in file order (duplicates included - ambiguity is
// TrimPlaybookToPlay's own concern, not this function's) - the "Start with
// play" re-run dialog field's autocomplete candidate list
// (design-docs/StartWithPlay.md), mirroring how BuildTaskSourceIndex's own
// third return value backs "Start with task".
func ListTopLevelPlayNames(playbookPath string) []string {
	plays := parseTopLevelPlays(playbookPath)
	names := make([]string, len(plays))
	for i, p := range plays {
		names[i] = p.name
	}
	return names
}

// MergeSourceIndex adds every task indexed under trimmedPlaybookPath into
// dst, in place. Needed wherever a generation actually spawns against a
// TrimPlaybookToPlay copy rather than the original file
// (design-docs/StartWithPlay.md): every RawEvent.Task.Path the running
// generation reports for a task defined directly in the top-level
// playbook (as opposed to one reached via roles:/include_tasks/
// import_tasks, whose own file is untouched and unaffected) points into
// the *trimmed* file's own path and line numbers - which a sourceIndex
// built only from the original file, at session start, can never contain,
// silently dropping the drill-down view's "Task definition" tab for every
// such task. Re-indexing the trimmed file and merging its entries in
// fixes that; role/include files get harmlessly re-indexed with identical
// content along the way (BuildTaskSourceIndex always walks a playbook's
// whole directory tree, not just the one file), which is wasted work but
// not wrong. A no-op, not an error, if trimmedPlaybookPath can't be
// read/parsed - same posture as everything else in this file.
func MergeSourceIndex(dst TaskSourceIndex, trimmedPlaybookPath string) {
	src, _, _ := BuildTaskSourceIndex(trimmedPlaybookPath)
	for k, v := range src {
		dst[k] = v
	}
}

// TrimPlaybookToPlay writes a temporary copy of playbookPath containing
// only the named play onward, in the same directory as playbookPath -
// not a system temp directory - so that any role/include reference
// resolved relative to the playbook's own directory still finds exactly
// what it would for the original file (design-docs/StartWithPlay.md).
// cleanup removes the temp file; ok is false (with cleanup nil) if no
// top-level play named playName exists (a duplicate name resolves to the
// first match, in file order, the same way --start-at-task's own
// name-matching does); err is non-nil only for the underlying file I/O.
func TrimPlaybookToPlay(playbookPath, playName string) (tempPath string, cleanup func(), ok bool, err error) {
	var start int
	found := false
	for _, p := range parseTopLevelPlays(playbookPath) {
		if p.name == playName {
			start = p.startLine
			found = true
			break
		}
	}
	if !found {
		return "", nil, false, nil
	}

	data, err := os.ReadFile(playbookPath)
	if err != nil {
		return "", nil, false, err
	}
	lines := strings.Split(string(data), "\n")
	if start < 1 || start > len(lines) {
		return "", nil, false, nil
	}
	trimmed := strings.Join(lines[start-1:], "\n")

	f, err := os.CreateTemp(filepath.Dir(playbookPath), ".tangsible-startplay-*.yml")
	if err != nil {
		return "", nil, false, err
	}
	if _, err := f.WriteString(trimmed); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, false, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", nil, false, err
	}

	path := f.Name()
	return path, func() { os.Remove(path) }, true, nil
}

// sortedSet returns set's own keys, alphabetically sorted - a plain slice
// is what tui.go's autocomplete matching wants, not a map. Shared by
// BuildTaskSourceIndex's tag and task-name collection - both are just "the
// deduplicated keys of a set," nothing tag- or name-specific about the
// sorting itself.
func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// indexFile parses one YAML file, indexes every task it can find in it,
// and adds every literal tags: value and task name: value it encounters
// (at any task/block/play level, for tags; per-task, for names) to
// tagSet/nameSet respectively. A parse error, an unreadable file, or a
// top-level shape that isn't a sequence (e.g. a vars file, which is a
// mapping) simply indexes nothing from this file - never propagated as an
// error.
func indexFile(path string, index TaskSourceIndex, tagSet, nameSet map[string]bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return
	}
	root := doc.Content[0] // yaml.Unmarshal into a *yaml.Node wraps the
	// real top-level node one level down in a DocumentNode - root here is
	// that actual top-level node.
	if root.Kind != yaml.SequenceNode {
		return
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	// isPlaybook: every item in the top-level sequence is a mapping with
	// its own "hosts" key - the standard shape of a playbook file, as
	// opposed to a task-list file (role tasks/main.yml, or any
	// import_tasks/include_tasks target), whose top-level sequence items
	// are task mappings directly.
	isPlaybook := len(root.Content) > 0
	for _, item := range root.Content {
		if mappingValue(item, "hosts") == nil {
			isPlaybook = false
			break
		}
	}

	if isPlaybook {
		for i, play := range root.Content {
			playEnd := totalLines + 1
			if i+1 < len(root.Content) {
				playEnd = root.Content[i+1].Line
			}
			collectTags(play, tagSet)
			collectRoleTags(play, tagSet)
			walkMappingForTaskLists(path, play, playTaskListKeys, lines, playEnd, index, tagSet, nameSet)
		}
		return
	}

	indexTaskList(path, root, lines, totalLines+1, index, tagSet, nameSet)
}

// collectTags reads mapping's own "tags:" value, if any - a scalar
// ("tags: foo") or a sequence ("tags: [foo, bar]" or block form) - and
// adds every non-empty literal string found to tagSet. A templated value
// (containing "{{") is skipped - it isn't a literal tag name to suggest,
// the same "don't evaluate Jinja, show it as literal text or skip it"
// restraint this package's own task-source rendering already follows
// elsewhere (CLAUDE.md's Task source lookup section).
func collectTags(mapping *yaml.Node, tagSet map[string]bool) {
	val := mappingValue(mapping, "tags")
	if val == nil {
		return
	}
	var scalars []*yaml.Node
	switch val.Kind {
	case yaml.ScalarNode:
		scalars = []*yaml.Node{val}
	case yaml.SequenceNode:
		scalars = val.Content
	default:
		return
	}
	for _, s := range scalars {
		if s.Kind != yaml.ScalarNode {
			continue
		}
		t := strings.TrimSpace(s.Value)
		if t == "" || strings.Contains(t, "{{") {
			continue
		}
		tagSet[t] = true
	}
}

// collectRoleTags reads play's own "roles:" sequence, if any, and adds
// each role reference's own tags: value to tagSet - a role can carry tags
// of its own right where it's included ("- role: foo\n  tags: [...]" or
// the equivalent "- {role: foo, tags: [...]}" flow form), a common enough
// pattern that it's a real gap otherwise: collectTags's own call against
// the play mapping only ever sees the play's own top-level tags: key, not
// anything nested inside roles:, and role entries aren't a task list
// walkMappingForTaskLists/indexTaskList would ever reach either (a role
// reference isn't a task - it has no path a jsonl event could report,
// only the tasks it eventually expands into do, indexed separately when
// this same walk reaches that role's own tasks/main.yml as a task-list
// file in its own right). The bare "- somerole" shorthand form carries no
// tags of its own and is silently skipped (not a mapping, nothing to read).
func collectRoleTags(play *yaml.Node, tagSet map[string]bool) {
	roles := mappingValue(play, "roles")
	if roles == nil || roles.Kind != yaml.SequenceNode {
		return
	}
	for _, r := range roles.Content {
		if r.Kind == yaml.MappingNode {
			collectTags(r, tagSet)
		}
	}
}

// mappingValue returns the value node for key within mapping node m, or
// nil if m isn't a mapping or doesn't have that key.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// walkMappingForTaskLists scans mapping's own key/value pairs in document
// order for any key in taskListKeys whose value is a sequence, and indexes
// that sequence as a task list. limit is mapping's own overall end
// boundary (the line at which whatever comes after the entire mapping
// begins) - used as the fallback boundary for the last matching key found;
// an earlier matching key's boundary is instead the next key in this same
// mapping. This keeps every nested task list's span tightly bounded even
// when interleaved with unrelated keys - e.g. a play's own "vars:" sitting
// between "tasks:" and "handlers:", or "rescue:" following "block:".
func walkMappingForTaskLists(path string, mapping *yaml.Node, taskListKeys map[string]bool, lines []string, limit int, index TaskSourceIndex, tagSet, nameSet map[string]bool) {
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for k := 0; k+1 < len(mapping.Content); k += 2 {
		key := mapping.Content[k]
		val := mapping.Content[k+1]
		if !taskListKeys[key.Value] || val.Kind != yaml.SequenceNode {
			continue
		}
		nested := limit
		if k+2 < len(mapping.Content) {
			nested = mapping.Content[k+2].Line
		}
		indexTaskList(path, val, lines, nested, index, tagSet, nameSet)
	}
}

// indexTaskList records every item of seq (a task-list sequence) into
// index, and recurses into any block:/rescue:/always: key found within
// each item. limit is this sequence's own end boundary, inherited from
// whatever encloses it - used as the last item's fallback boundary; every
// earlier item's boundary is simply the next item's own start line.
func indexTaskList(path string, seq *yaml.Node, lines []string, limit int, index TaskSourceIndex, tagSet, nameSet map[string]bool) {
	for i, item := range seq.Content {
		itemEnd := limit
		if i+1 < len(seq.Content) {
			itemEnd = seq.Content[i+1].Line
		}
		recordNode(path, item, lines, itemEnd-1, index)
		collectTags(item, tagSet)
		collectTaskName(item, nameSet)
		walkMappingForTaskLists(path, item, blockTaskListKeys, lines, itemEnd, index, tagSet, nameSet)
	}
}

// collectTaskName reads item's own literal "name:" value and adds it to
// nameSet - design-docs/Autocomplete.md's task-name autocomplete source
// for the re-run dialog's "Start with task" field. Skipped entirely if
// item is itself a block:/rescue:/always: wrapper (mappingValue(item,
// "block") != nil): a wrapper's own name, if any, never appears as a real
// task in Ansible's own event stream - only the individual tasks nested
// inside it do, each collected separately when this same walk recurses
// into it via walkMappingForTaskLists/indexTaskList above. A missing
// name: (Ansible falls back to auto-generating one from the action, not a
// literal string this walk can see) or a templated one (containing "{{")
// is skipped too, same restraint collectTags already applies to tags.
func collectTaskName(item *yaml.Node, nameSet map[string]bool) {
	if mappingValue(item, "block") != nil {
		return
	}
	val := mappingValue(item, "name")
	if val == nil || val.Kind != yaml.ScalarNode {
		return
	}
	n := strings.TrimSpace(val.Value)
	if n == "" || strings.Contains(n, "{{") {
		return
	}
	nameSet[n] = true
}

// recordNode indexes one task-list item's raw source text, from its own
// start line through endLine (inclusive), trimmed of trailing blank lines.
// Indexes every mapping item unconditionally when called from
// indexTaskList - including a block: wrapper itself, alongside its nested
// tasks - which is harmless: Ansible's own task.path never points at a
// block wrapper in practice, so that entry simply never gets looked up.
func recordNode(path string, item *yaml.Node, lines []string, endLine int, index TaskSourceIndex) {
	if item.Kind != yaml.MappingNode {
		return
	}
	start := item.Line
	if endLine < start {
		endLine = start
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	text := strings.TrimRight(strings.Join(lines[start-1:endLine], "\n"), "\n \t")
	if text == "" {
		return
	}
	index[fmt.Sprintf("%s:%d", path, start)] = text
}
