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
	"bytes"
	"encoding/json"

	"code.aw.net/claude/tangsible/internal/debugverb"
	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// variablesTabNote heads the debug drill-down's Variables tab.
const variablesTabNote = "Variables at this task's pause; facts and ansible's own variables are left out\n\n"

// debugVariablesTab renders the drill-down's Variables tab for task on host
// (debug mode only): the variables the debugger saw when the task paused,
// vault values masked, each with where it was defined. ok is false when
// there is nothing to show (not debug mode, or the task never paused).
func (s *liveSession) debugVariablesTab(task *playbook.TaskNode, host string) (text string, ok bool) {
	if !s.debugMode {
		return "", false
	}
	key := debugVarsKey{debugverb.TaskLabel(task.Name, task.IsHandler), host}
	snapshot, have := s.debugTaskVarsCache[key]
	if !have {
		return "", false
	}
	snapshot = maskSnapshot(snapshot, s.debugVaultMaskCache[key])
	labels := s.debugOriginCache[key]
	text, err := uikit.RenderVariablesYAML(variablesWithoutBulk(snapshot, labels), labels,
		"["+uikit.AnnotationColor+"]"+variablesTabNote+"[-]", "No variables.\n")
	if err != nil {
		return "", false
	}
	return text, true
}

// variablesWithoutBulk drops the top-level variables labeled "fact" or
// "built-in" (the Facts of a host are huge, and hostvars/groups are ansible's
// own): the Variables tab is about what the user defined.
func variablesWithoutBulk(vars json.RawMessage, labels map[string]string) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(vars))
	dec.UseNumber()
	var all map[string]json.RawMessage
	if dec.Decode(&all) != nil {
		return vars
	}
	for name := range all {
		if l := labels[name]; l == "fact" || l == "built-in" {
			delete(all, name)
		}
	}
	out, err := json.Marshal(all)
	if err != nil {
		return vars
	}
	return out
}
