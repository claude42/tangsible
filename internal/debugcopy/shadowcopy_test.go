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

package debugcopy

import (
	"os"
	"path/filepath"
	"testing"
)

func sameInode(t *testing.T, a, b string) bool {
	t.Helper()
	sa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	return os.SameFile(sa, sb)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildShadowCopy_Hardlinks(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, "playbook.yml"), "- hosts: all\n")
	writeFile(t, filepath.Join(proj, "roles", "myrole", "tasks", "main.yml"), "- name: a task\n  command: echo hi\n")
	writeFile(t, filepath.Join(proj, "group_vars", "all.yml"), "x: 1\n")

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	for _, rel := range []string{"playbook.yml", "roles/myrole/tasks/main.yml", "group_vars/all.yml"} {
		orig := filepath.Join(proj, rel)
		cp := filepath.Join(copyDir, rel)
		if !sameInode(t, orig, cp) {
			t.Errorf("%s: copy is not a hardlink to the original", rel)
		}
	}
}

func TestBuildShadowCopy_ExcludesAppStateAndGit(t *testing.T) {
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, "playbook.yml"), "- hosts: all\n")
	writeFile(t, filepath.Join(proj, ".tangsible", "state.toml"), "fake state\n")
	writeFile(t, filepath.Join(proj, ".git", "config"), "fake git config\n")

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(copyDir, ".git", "config")); !os.IsNotExist(err) {
		t.Errorf(".git/config should not have been copied, stat err = %v", err)
	}
	// .tangsible/debug/<runID> is where the copy itself lives, nested
	// inside the real .tangsible - the exclusion must still keep any
	// *other* .tangsible content (like state.toml) out of the copy.
	if _, err := os.Stat(filepath.Join(copyDir, ".tangsible", "state.toml")); !os.IsNotExist(err) {
		t.Errorf(".tangsible/state.toml should not have been copied, stat err = %v", err)
	}
}

func TestBuildShadowCopy_CleanupDoesNotTouchOriginal(t *testing.T) {
	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	writeFile(t, playbookPath, "- hosts: all\n")

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()

	if _, err := os.Stat(copyDir); !os.IsNotExist(err) {
		t.Errorf("copy dir should be gone after cleanup, stat err = %v", err)
	}
	data, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "- hosts: all\n" {
		t.Errorf("original playbook content changed: %q", data)
	}
}
