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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTaskSourceIndex(t *testing.T) {
	// Copied into an isolated temp dir rather than indexed directly out of
	// testdata/: buildTaskSourceIndex indexes every .yml/.yaml file in the
	// playbook's whole directory tree (roles/ alongside it, etc. - see its
	// own doc comment), so pointing it straight at testdata/outcomes.yml
	// would also pick up the other fixture playbooks living next to it,
	// making the exact count below wrong through no fault of the function
	// itself.
	src, err := os.ReadFile("testdata/outcomes.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "outcomes.yml")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}

	index := buildTaskSourceIndex(path)

	// 5 tasks plus the play itself (indexed the same way since source.go's
	// change to also cover plays, not just tasks - see recordNode).
	wantNames := []string{"ok task", "changed task", "skipped task", "failed task", "after failure"}
	wantTotal := len(wantNames) + 1
	if len(index) != wantTotal {
		t.Fatalf("got %d indexed entries, want %d (index: %v)", len(index), wantTotal, index)
	}
	for _, name := range wantNames {
		found := false
		for _, source := range index {
			if strings.Contains(source, "name: "+name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no indexed task source contains %q", "name: "+name)
		}
	}

	playFound := false
	for _, source := range index {
		if strings.Contains(source, "hosts: localhost") {
			playFound = true
			break
		}
	}
	if !playFound {
		t.Error("no indexed source contains the play's own \"hosts: localhost\"")
	}
}

// TestBuildTaskSourceIndex_PlaysAreIndexedByTheirOwnStartLine confirms the
// play's own entry is keyed correctly (its own start line, not shifted by
// or colliding with its tasks') and contains the play's full block,
// verbatim - not just a "hosts:" line, the whole thing through its last
// task, matching Task definition's own "whole node verbatim" treatment.
func TestBuildTaskSourceIndex_PlaysAreIndexedByTheirOwnStartLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pb.yml")
	content := "- hosts: localhost\n  gather_facts: false\n  tasks:\n    - name: a task\n      debug:\n        msg: hi\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	index := buildTaskSourceIndex(path)

	playKey := path + ":1"
	playSource, ok := index[playKey]
	if !ok {
		t.Fatalf("no entry for %q; index: %v", playKey, index)
	}
	if !strings.Contains(playSource, "hosts: localhost") || !strings.Contains(playSource, "a task") {
		t.Errorf("play source = %q, want it to contain both the play's own hosts: and its task", playSource)
	}

	taskKey := path + ":4"
	if _, ok := index[taskKey]; !ok {
		t.Errorf("no entry for the task at %q - play indexing must not have displaced it; index: %v", taskKey, index)
	}
}

func TestBuildTaskSourceIndex_NonexistentPlaybook(t *testing.T) {
	// The directory itself must not exist either - buildTaskSourceIndex
	// indexes every .yml/.yaml file found alongside the playbook, so a
	// nonexistent playbook inside a real, populated directory (e.g.
	// testdata/) would still pick up its siblings and not actually be
	// empty. An empty temp dir is the only way to get a genuinely empty
	// result.
	path := filepath.Join(t.TempDir(), "does-not-exist.yml")
	index := buildTaskSourceIndex(path)
	if len(index) != 0 {
		t.Errorf("got %d entries, want 0 for a nonexistent playbook in an empty directory", len(index))
	}
}
