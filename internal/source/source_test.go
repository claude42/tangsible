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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildTaskSourceIndex(t *testing.T) {
	// Copied into an isolated temp dir rather than indexed directly out of
	// testdata/: BuildTaskSourceIndex indexes every .yml/.yaml file in the
	// playbook's whole directory tree (roles/ alongside it, etc. - see its
	// own doc comment), so pointing it straight at testdata/outcomes.yml
	// would also pick up the other fixture playbooks living next to it,
	// making the exact count below wrong through no fault of the function
	// itself.
	src, err := os.ReadFile("../../testdata/outcomes.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "outcomes.yml")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}

	index, _ := BuildTaskSourceIndex(path)

	wantNames := []string{"ok task", "changed task", "skipped task", "failed task", "after failure"}
	if len(index) != len(wantNames) {
		t.Fatalf("got %d indexed entries, want %d (index: %v)", len(index), len(wantNames), index)
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
}

func TestBuildTaskSourceIndex_NonexistentPlaybook(t *testing.T) {
	// The directory itself must not exist either - BuildTaskSourceIndex
	// indexes every .yml/.yaml file found alongside the playbook, so a
	// nonexistent playbook inside a real, populated directory (e.g.
	// testdata/) would still pick up its siblings and not actually be
	// empty. An empty temp dir is the only way to get a genuinely empty
	// result.
	path := filepath.Join(t.TempDir(), "does-not-exist.yml")
	index, tags := BuildTaskSourceIndex(path)
	if len(index) != 0 {
		t.Errorf("got %d entries, want 0 for a nonexistent playbook in an empty directory", len(index))
	}
	if len(tags) != len(ReservedTagNames) {
		t.Errorf("got %d tags, want just the %d reserved names for a nonexistent playbook", len(tags), len(ReservedTagNames))
	}
}

func TestBuildTaskSourceIndex_CollectsTags(t *testing.T) {
	playbookYAML := `
- hosts: localhost
  gather_facts: false
  tags: playlevel
  tasks:
    - name: tagged task
      debug:
        msg: hi
      tags: [foo, bar]
    - name: single scalar tag
      debug:
        msg: hi
      tags: baz
    - name: templated tag is skipped
      debug:
        msg: hi
      tags: "{{ some_var }}"
    - name: reserved tag used explicitly
      debug:
        msg: hi
      tags: always
    - name: untagged task
      debug:
        msg: hi
  roles:
    - role: withtags
      tags: [roletag1, roletag2]
    - bareroleref
`
	path := filepath.Join(t.TempDir(), "tagged.yml")
	if err := os.WriteFile(path, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	_, tags := BuildTaskSourceIndex(path)

	want := map[string]bool{
		"foo": true, "bar": true, "baz": true,
		"playlevel": true, "roletag1": true, "roletag2": true,
	}
	for t2 := range want {
		found := false
		for _, got := range tags {
			if got == t2 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tags = %v, missing discovered tag %q", tags, t2)
		}
	}
	for _, reserved := range ReservedTagNames {
		found := false
		for _, got := range tags {
			if got == reserved {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tags = %v, missing reserved tag %q", tags, reserved)
		}
	}
	for _, got := range tags {
		if strings.Contains(got, "{{") {
			t.Errorf("tags = %v, a templated tag value leaked through unresolved", tags)
		}
	}
	// "always" is both a reserved name and used literally as a task's own
	// tag above - must appear exactly once, not duplicated by the union.
	count := 0
	for _, got := range tags {
		if got == "always" {
			count++
		}
	}
	if count != 1 {
		t.Errorf(`tags contains "always" %d times, want exactly 1 (reserved ∪ discovered must dedupe)`, count)
	}
}
