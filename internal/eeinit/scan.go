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

// Package eeinit drafts an execution-environment.yml (ansible-builder
// schema v3) from a playbook - `tangsible ee init`,
// design-docs/EEInit.md. The drafting itself (ScanYAML, Resolve, Draft,
// ParseProbe, ParseRequirements) is pure; host.go, probe.go and run.go
// hold the file, ansible and podman access that feeds it.
package eeinit

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModuleUse is one task's module, as written.
type ModuleUse struct {
	Module string
	// Collections is the collections: search list in effect for the task.
	Collections []string
	// Loc is "path:line".
	Loc string
}

// Todo is something the scan could not resolve statically.
type Todo struct {
	Loc, What string
	// TemplatedInclude marks an include/import whose target is a template.
	// There are often dozens of those, so Draft reports them as one.
	TemplatedInclude bool
}

// FileScan is what ScanYAML found in one file.
type FileScan struct {
	Uses  []ModuleUse
	Todos []Todo
}

// taskKeywords are the play, block and task keywords: a task's module is
// the one key that is none of these (and not a with_* loop).
var taskKeywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`
		name when tags register vars vars_files vars_prompt loop loop_control
		become become_user become_method become_flags become_exe delegate_to
		delegate_facts ignore_errors ignore_unreachable changed_when
		failed_when notify listen until retries delay environment args no_log
		check_mode diff any_errors_fatal max_fail_percentage async poll
		connection collections module_defaults run_once throttle timeout
		debugger remote_user port block rescue always hosts roles tasks
		pre_tasks post_tasks handlers gather_facts gather_subset
		gather_timeout fact_path force_handlers strategy serial order
		no_log_values`) {
		taskKeywords[k] = true
	}
}

// moduleName is what a module name can look like; anything else is a
// stray key of a file that isn't a task list after all.
var moduleName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// playTaskLists are the play keys that hold tasks.
var playTaskLists = map[string]bool{"tasks": true, "pre_tasks": true, "post_tasks": true, "handlers": true}

// includeActions load other files or roles; their target can be a template.
var includeActions = map[string]bool{
	"include_tasks": true, "import_tasks": true, "include": true,
	"include_role": true, "import_role": true, "import_playbook": true,
}

// ScanYAML finds the modules the tasks in one YAML file use: a play's
// tasks, pre_tasks, post_tasks and handlers, nested block/rescue/always,
// the action: and local_action: forms, and a role's task files (a file
// whose top level is a task list). own is the collection a collection
// role's file belongs to, which its unqualified names resolve against.
// A file that isn't a task list or a playbook (a vars file, say), or that
// doesn't parse, yields nothing.
func ScanYAML(path string, data []byte, own []string) FileScan {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return FileScan{}
	}
	root := doc.Content[0]
	if root.Kind != yaml.SequenceNode {
		return FileScan{}
	}
	s := &scanner{path: path}
	isPlaybook := len(root.Content) > 0
	for _, item := range root.Content {
		if value(item, "hosts") == nil && value(item, "import_playbook") == nil {
			isPlaybook = false
			break
		}
	}
	if !isPlaybook {
		s.tasks(root, own)
		return s.out
	}
	for _, play := range root.Content {
		colls := append(collectionsOf(play), own...)
		for i := 0; i+1 < len(play.Content); i += 2 {
			if seq := play.Content[i+1]; playTaskLists[play.Content[i].Value] && seq.Kind == yaml.SequenceNode {
				s.tasks(seq, colls)
			}
		}
	}
	return s.out
}

type scanner struct {
	path string
	out  FileScan
}

func (s *scanner) loc(n *yaml.Node) string { return fmt.Sprintf("%s:%d", s.path, n.Line) }

func (s *scanner) tasks(seq *yaml.Node, colls []string) {
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		colls := append(collectionsOf(item), colls...)
		if value(item, "block") != nil {
			for _, key := range []string{"block", "rescue", "always"} {
				if sub := value(item, key); sub != nil && sub.Kind == yaml.SequenceNode {
					s.tasks(sub, colls)
				}
			}
			continue
		}
		s.task(item, colls)
	}
}

func (s *scanner) task(item *yaml.Node, colls []string) {
	for i := 0; i+1 < len(item.Content); i += 2 {
		key, val := item.Content[i], item.Content[i+1]
		name := key.Value
		switch {
		case taskKeywords[name] || strings.HasPrefix(name, "with_"):
			continue
		case name == "action" || name == "local_action":
			name = actionModule(val)
			if name == "" || strings.Contains(name, "{{") {
				s.out.Todos = append(s.out.Todos, Todo{Loc: s.loc(key), What: "module name of " + key.Value + ": can't be determined statically"})
				return
			}
		case !moduleName.MatchString(name):
			continue
		}
		if !moduleName.MatchString(name) {
			return
		}
		s.out.Uses = append(s.out.Uses, ModuleUse{Module: name, Collections: colls, Loc: s.loc(key)})
		if includeActions[lastPart(name)] && templated(val) {
			s.out.Todos = append(s.out.Todos, Todo{Loc: s.loc(key), What: name + " with a templated target", TemplatedInclude: true})
		}
		return
	}
}

// actionModule is the module an action:/local_action: value names: the
// first word of the string form, or module: of the mapping form.
func actionModule(v *yaml.Node) string {
	switch v.Kind {
	case yaml.ScalarNode:
		if f := strings.Fields(v.Value); len(f) > 0 {
			return f[0]
		}
	case yaml.MappingNode:
		if m := value(v, "module"); m != nil && m.Kind == yaml.ScalarNode {
			return strings.TrimSpace(m.Value)
		}
	}
	return ""
}

// templated reports whether an include's argument (a string, or a mapping
// with file:/name:) holds Jinja.
func templated(v *yaml.Node) bool {
	switch v.Kind {
	case yaml.ScalarNode:
		return strings.Contains(v.Value, "{{")
	case yaml.MappingNode:
		for _, k := range []string{"file", "name", "_raw_params"} {
			if n := value(v, k); n != nil && n.Kind == yaml.ScalarNode && strings.Contains(n.Value, "{{") {
				return true
			}
		}
	}
	return false
}

func lastPart(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

func collectionsOf(m *yaml.Node) []string {
	seq := value(m, "collections")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	var out []string
	for _, c := range seq.Content {
		if c.Kind == yaml.ScalarNode && strings.TrimSpace(c.Value) != "" {
			out = append(out, strings.TrimSpace(c.Value))
		}
	}
	return out
}

// value returns the value of key in mapping m, nil if absent.
func value(m *yaml.Node, key string) *yaml.Node {
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
