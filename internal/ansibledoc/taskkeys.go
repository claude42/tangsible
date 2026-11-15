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

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// KeyKind says what a key in a task's YAML is, for context help.
type KeyKind int

const (
	KeyNone    KeyKind = iota // not something we have help for
	KeyKeyword                // a task keyword: name, when, register, ...
	KeyModule                 // the module's own key: copy:
	KeyOption                 // an option (Path: option, then suboptions)
)

// KeyRef is LocateTaskKey's answer. Path is the key's name for a keyword or
// the module, and for an option the names from the module's own mapping down
// to it.
type KeyRef struct {
	Kind KeyKind
	Path []string
}

// LocateTaskKey classifies the key named word that starts at 1-based line
// and column of src, a task's own YAML (as shown in the Task definition
// tab). action is the task's module as ansible reported it; the module's key
// is the top-level one naming it, with or without its collection prefix. The
// same name can be a keyword or an option (name: at task level vs. under
// user:), which is why this goes by position, not by name. Only mapping
// keys count: values, list items and keys under vars:, environment: and the
// like give KeyNone.
func LocateTaskKey(src string, line, column int, word, action string) KeyRef {
	var doc yaml.Node
	if yaml.Unmarshal([]byte(src), &doc) != nil || len(doc.Content) == 0 {
		return KeyRef{}
	}
	task := doc.Content[0]
	if task.Kind == yaml.SequenceNode && len(task.Content) > 0 {
		task = task.Content[0]
	}
	if task.Kind != yaml.MappingNode {
		return KeyRef{}
	}
	at := func(k *yaml.Node) bool { return k.Line == line && k.Column == column && k.Value == word }

	for i := 0; i+1 < len(task.Content); i += 2 {
		key, val := task.Content[i], task.Content[i+1]
		isModule := action != "" && sameAction(key.Value, action)
		if at(key) {
			if isModule {
				return KeyRef{KeyModule, []string{word}}
			}
			return KeyRef{KeyKeyword, []string{word}}
		}
		switch {
		case isModule:
			if ref, ok := locateOption(val, nil, at); ok {
				return ref
			}
		case key.Value == "args":
			if ref, ok := locateOption(val, nil, at); ok {
				return ref
			}
		}
	}
	return KeyRef{}
}

// locateOption searches the mapping (or list of mappings) node for the key
// at() accepts, descending into mapping values as suboptions.
func locateOption(node *yaml.Node, path []string, at func(*yaml.Node) bool) (KeyRef, bool) {
	switch node.Kind {
	case yaml.SequenceNode: // a list of suboption mappings shares one path
		for _, item := range node.Content {
			if ref, ok := locateOption(item, path, at); ok {
				return ref, true
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, val := node.Content[i], node.Content[i+1]
			here := append(append([]string(nil), path...), key.Value)
			if at(key) {
				return KeyRef{KeyOption, here}, true
			}
			if ref, ok := locateOption(val, here, at); ok {
				return ref, true
			}
		}
	}
	return KeyRef{}, false
}

// sameAction reports whether a task's module key and the action ansible
// reported name the same module: equal, or equal once the collection prefix
// is dropped from either side.
func sameAction(key, action string) bool {
	if key == action {
		return true
	}
	return lastSegment(key) == lastSegment(action) &&
		(!strings.Contains(key, ".") || !strings.Contains(action, "."))
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}
