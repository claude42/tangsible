package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// taskSourceIndex maps a task's or a play's Ansible-reported path
// ("<absolute file>:<line>", exactly matching rawEvent's task.path/
// play.path fields) to its own raw YAML source text, verbatim from the
// file - not reformatted or re-serialized, so the user's own formatting/
// comments are preserved. One shared map for both: a play's own start
// line never collides with any of its tasks' start lines (a play always
// precedes its own tasks in the same file), so there's no ambiguity in
// keying them together.
type taskSourceIndex map[string]string

// blockTaskListKeys/playTaskListKeys name the mapping keys whose value is
// itself a task-list sequence, at the task level (nested inside a block:)
// and the play level respectively - shared by walkMappingForTaskLists.
var blockTaskListKeys = map[string]bool{"block": true, "rescue": true, "always": true}
var playTaskListKeys = map[string]bool{"tasks": true, "pre_tasks": true, "post_tasks": true, "handlers": true}

// buildTaskSourceIndex discovers every .yml/.yaml file under playbookPath's
// own directory tree (the playbook itself, plus any roles/** alongside it)
// and indexes every task - and, for an actual playbook file, every play
// too - found in each by its own source location. Never fails outward -
// unreadable directories/files or YAML parse errors just mean less
// coverage, not a crash; a lookup miss at display time simply means no
// Task definition/Play definition section for that entry (see tui.go's
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
func buildTaskSourceIndex(playbookPath string) taskSourceIndex {
	index := taskSourceIndex{}

	abs, err := filepath.Abs(playbookPath)
	if err != nil {
		return index
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
		indexFile(f, index)
	}
	return index
}

// indexFile parses one YAML file and indexes every task it can find in it.
// A parse error, an unreadable file, or a top-level shape that isn't a
// sequence (e.g. a vars file, which is a mapping) simply indexes nothing
// from this file - never propagated as an error.
func indexFile(path string, index taskSourceIndex) {
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
			// The play itself, verbatim - same treatment as a task, and
			// indexed into the same map (see recordNode) - what backs the
			// output drill-down view's "Play definition" section
			// (tui.go), alongside its own tasks indexed right after.
			recordNode(path, play, lines, playEnd-1, index)
			walkMappingForTaskLists(path, play, playTaskListKeys, lines, playEnd, index)
		}
		return
	}

	indexTaskList(path, root, lines, totalLines+1, index)
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
func walkMappingForTaskLists(path string, mapping *yaml.Node, taskListKeys map[string]bool, lines []string, limit int, index taskSourceIndex) {
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
		indexTaskList(path, val, lines, nested, index)
	}
}

// indexTaskList records every item of seq (a task-list sequence) into
// index, and recurses into any block:/rescue:/always: key found within
// each item. limit is this sequence's own end boundary, inherited from
// whatever encloses it - used as the last item's fallback boundary; every
// earlier item's boundary is simply the next item's own start line.
func indexTaskList(path string, seq *yaml.Node, lines []string, limit int, index taskSourceIndex) {
	for i, item := range seq.Content {
		itemEnd := limit
		if i+1 < len(seq.Content) {
			itemEnd = seq.Content[i+1].Line
		}
		recordNode(path, item, lines, itemEnd-1, index)
		walkMappingForTaskLists(path, item, blockTaskListKeys, lines, itemEnd, index)
	}
}

// recordNode indexes one mapping node's raw source text - a task-list item
// or a whole play (see indexFile's isPlaybook branch) - from its own start
// line through endLine (inclusive), trimmed of trailing blank lines. Not
// task-specific despite the task-list callers: it just indexes whatever
// mapping node it's given by that node's own start line, which is exactly
// what a play needs too. Indexes every mapping item unconditionally when
// called from indexTaskList - including a block: wrapper itself, alongside
// its nested tasks - which is harmless: Ansible's own task.path never
// points at a block wrapper in practice, so that entry simply never gets
// looked up.
func recordNode(path string, item *yaml.Node, lines []string, endLine int, index taskSourceIndex) {
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
