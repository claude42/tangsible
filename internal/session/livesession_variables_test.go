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
	"encoding/json"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/debugverb"
	"code.aw.net/claude/tangsible/internal/playbook"
)

func TestVariablesWithoutBulk(t *testing.T) {
	got := variablesWithoutBulk(
		json.RawMessage(`{"a":1,"ansible_facts":{"x":1},"hostvars":{},"b":2}`),
		map[string]string{"a": "group_vars/all.yml:1", "ansible_facts": "fact", "hostvars": "built-in"})
	if string(got) != `{"a":1,"b":2}` {
		t.Errorf("got %s", got)
	}
}

func TestDebugVariablesTab(t *testing.T) {
	task := &playbook.TaskNode{Name: "t"}
	key := debugVarsKey{debugverb.TaskLabel("t", false), "h"}
	s := &liveSession{
		debugMode:           true,
		debugTaskVarsCache:  map[debugVarsKey]json.RawMessage{key: json.RawMessage(`{"zeta":"z","alpha":"a","secret":"hunter22","ansible_facts":{"k":1}}`)},
		debugVaultMaskCache: map[debugVarsKey]vaultMask{key: {Vaulted: [][]any{{"secret"}}}},
		debugOriginCache:    map[debugVarsKey]map[string]string{key: {"alpha": "vars/main.yml:3", "ansible_facts": "fact"}},
	}
	text, ok := s.debugVariablesTab(task, "h")
	if !ok {
		t.Fatal("no tab")
	}
	if strings.Contains(text, "hunter22") || strings.Contains(text, "ansible_facts") {
		t.Errorf("secret or facts leaked:\n%s", text)
	}
	if !strings.Contains(text, "# vars/main.yml:3") || strings.Index(text, "alpha") > strings.Index(text, "zeta") {
		t.Errorf("origin comment missing or not sorted:\n%s", text)
	}
	if _, ok := s.debugVariablesTab(&playbook.TaskNode{Name: "other"}, "h"); ok {
		t.Error("a task that never paused must have no tab")
	}
	s.debugMode = false
	if _, ok := s.debugVariablesTab(task, "h"); ok {
		t.Error("only debug mode has the tab")
	}
}
