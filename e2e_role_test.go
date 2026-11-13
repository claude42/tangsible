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

//go:build e2e

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Roles inside collections (design-docs/Tangsible role.md), against the real
// ansible-playbook. The collection is a hand-made directory tree - an
// ansible-galaxy install produces the same layout, so downloading one would
// only add network flakiness - placed *outside* the project and reached via
// ANSIBLE_COLLECTIONS_PATH: a collection under the playbook's own directory
// is indexed by the plain directory walk and would pass without any of the
// collection lookup. What these tests pin down is that the path ansible
// reports for a collection role's tasks matches the source index's keys, so
// the drill-down's "Task definition" tab (omitted when empty) shows up.

// writeE2EFiles writes files (path -> content, relative to dir).
func writeE2EFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// collectionFixture writes the acme.tools collection into a fresh directory
// and returns that directory (a COLLECTIONS_PATHS entry). Role outer depends
// on greet, so the first task of a run of outer belongs to the dependency.
func collectionFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	roles := "ansible_collections/acme/tools/roles/"
	writeE2EFiles(t, dir, map[string]string{
		roles + "greet/tasks/main.yml": "- name: greet from collection\n  debug:\n    msg: hello\n",
		roles + "outer/meta/main.yml":  "dependencies:\n  - acme.tools.greet\n",
		roles + "outer/tasks/main.yml": "- name: outer task\n  debug:\n    msg: outer\n",
	})
	return dir
}

// openFirstTaskDrillDown moves from the finished run's tree onto the first
// task's host row and opens the drill-down.
func (s *tmuxSession) openFirstTaskDrillDown() string {
	s.t.Helper()
	s.send("Home")
	s.send("Down")  // onto the first task row
	s.send("Right") // expand it
	s.send("Down")  // onto its host row
	s.send("Enter")
	return s.waitFor("Task definition", 5*time.Second)
}

// TestE2E_Role_CollectionRoleAndItsDependencyGetTaskDefinition runs
// "tangsible role ns.coll.role" for a role whose meta dependency is another
// role of the same collection.
func TestE2E_Role_CollectionRoleAndItsDependencyGetTaskDefinition(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	colls := collectionFixture(t)

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && ANSIBLE_COLLECTIONS_PATH=%s ANSIBLE_GATHERING=explicit %s %s role acme.tools.outer -i localhost, -c local",
		shellQuote(workDir), shellQuote(colls), e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	got := s.waitFor("Playbook completed successfully", 20*time.Second)
	for _, want := range []string{"greet from collection", "outer task"} {
		if !strings.Contains(got, want) {
			t.Fatalf("tree lacks %q:\n%s", want, got)
		}
	}
	// The first task is the dependency's: found only through outer's
	// meta/main.yml, in a collection outside the project.
	s.openFirstTaskDrillDown()
	s.quitCleanly()
}

// TestE2E_Run_CollectionsKeywordRoleGetsTaskDefinition runs a playbook whose
// play names a role by its short name and the collection via collections:.
func TestE2E_Run_CollectionsKeywordRoleGetsTaskDefinition(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	colls := collectionFixture(t)
	writeE2EFiles(t, workDir, map[string]string{
		"site.yml": "- hosts: localhost\n  gather_facts: false\n  connection: local\n  collections:\n    - acme.tools\n  roles:\n    - greet\n",
	})

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && ANSIBLE_COLLECTIONS_PATH=%s %s %s run site.yml",
		shellQuote(workDir), shellQuote(colls), e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	s.waitFor("Playbook completed successfully", 20*time.Second)
	s.openFirstTaskDrillDown()
	s.quitCleanly()
}
