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
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/source"
)

func TestForceLinearStrategy_RewritesInPlaceAndFollowsImports(t *testing.T) {
	proj := t.TempDir()
	main := `- hosts: all
  strategy: free   # race ahead
  tasks:
    - name: a
      command: echo a

- hosts: all
  strategy: "{{ s }}"
  tasks:
    - name: b
      command: echo b

- hosts: all
  strategy: linear
  tasks:
    - name: c
      command: echo c

- import_playbook: other.yml
`
	other := `- hosts: all
  strategy: host_pinned
  roles: []
`
	mainPath := filepath.Join(proj, "site.yml")
	writeFile(t, mainPath, main)
	writeFile(t, filepath.Join(proj, "other.yml"), other)

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	locs := source.ListTaskLocations(mainPath)
	if !ForceLinearStrategy(proj, copyDir, mainPath, locs) {
		t.Fatal("expected a rewrite")
	}

	got, _ := os.ReadFile(filepath.Join(copyDir, "site.yml"))
	if strings.Contains(string(got), "free") || strings.Contains(string(got), "{{ s }}") {
		t.Errorf("non-linear strategies left in the copy:\n%s", got)
	}
	if strings.Count(string(got), "\n") != strings.Count(main, "\n") {
		t.Errorf("line count changed:\n%s", got)
	}
	if o, _ := os.ReadFile(filepath.Join(copyDir, "other.yml")); !strings.Contains(string(o), "strategy: linear") {
		t.Errorf("imported playbook not rewritten:\n%s", o)
	}
	// The original tree stays untouched (hardlink broken before writing).
	if orig, _ := os.ReadFile(mainPath); string(orig) != main {
		t.Errorf("original playbook was modified:\n%s", orig)
	}
}

func TestForceLinearStrategy_NoChangeWhenAlreadyLinear(t *testing.T) {
	proj := t.TempDir()
	p := filepath.Join(proj, "site.yml")
	writeFile(t, p, "- hosts: all\n  tasks:\n    - name: a\n      command: echo a\n")
	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if ForceLinearStrategy(proj, copyDir, p, source.ListTaskLocations(p)) {
		t.Error("nothing to rewrite, but reported a change")
	}
}
