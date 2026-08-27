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

	index, _, _ := BuildTaskSourceIndex(path)

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
	index, tags, names := BuildTaskSourceIndex(path)
	if len(index) != 0 {
		t.Errorf("got %d entries, want 0 for a nonexistent playbook in an empty directory", len(index))
	}
	if len(tags) != len(ReservedTagNames) {
		t.Errorf("got %d tags, want just the %d reserved names for a nonexistent playbook", len(tags), len(ReservedTagNames))
	}
	if len(names) != 0 {
		t.Errorf("got %d task names, want 0 for a nonexistent playbook in an empty directory", len(names))
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

	_, tags, _ := BuildTaskSourceIndex(path)

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

func TestBuildTaskSourceIndex_CollectsTaskNames(t *testing.T) {
	playbookYAML := `
- hosts: localhost
  gather_facts: false
  tasks:
    - name: plain task
      debug:
        msg: hi
    - name: "{{ dynamic_name }}"
      debug:
        msg: hi
    - debug:
        msg: no name at all
    - name: grouped tasks
      block:
        - name: inside the block
          debug:
            msg: hi
      rescue:
        - name: inside rescue
          debug:
            msg: hi
  roles:
    - myrole
`
	roleTasksYAML := `
- name: role task
  debug:
    msg: hi
`
	dir := t.TempDir()
	path := filepath.Join(dir, "site.yml")
	if err := os.WriteFile(path, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	roleTasksDir := filepath.Join(dir, "roles", "myrole", "tasks")
	if err := os.MkdirAll(roleTasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roleTasksDir, "main.yml"), []byte(roleTasksYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, names := BuildTaskSourceIndex(path)

	want := []string{"plain task", "inside the block", "inside rescue", "role task"}
	for _, w := range want {
		found := false
		for _, got := range names {
			if got == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("names = %v, missing %q", names, w)
		}
	}
	dontWant := []string{"grouped tasks", "{{ dynamic_name }}", ""}
	for _, dw := range dontWant {
		for _, got := range names {
			if got == dw {
				t.Errorf("names = %v, unexpectedly contains %q", names, dw)
			}
		}
	}
}

func TestListTopLevelPlayNames(t *testing.T) {
	playbookYAML := `
- name: first play
  hosts: web

- hosts: db

- name: "{{ templated_name }}"
  hosts: all

- name: third play
  hosts: all
`
	path := filepath.Join(t.TempDir(), "site.yml")
	if err := os.WriteFile(path, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	got := ListTopLevelPlayNames(path)
	want := []string{"first play", "third play"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (nameless/templated plays must be skipped, order preserved)", i, got[i], want[i])
		}
	}
}

func TestListTopLevelPlayNames_NotPlaybookShaped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.yml")
	if err := os.WriteFile(path, []byte("- name: a bare task\n  debug:\n    msg: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ListTopLevelPlayNames(path); len(got) != 0 {
		t.Errorf("got %v, want none for a task-list-shaped file", got)
	}
}

func TestTrimPlaybookToPlay(t *testing.T) {
	playbookYAML := `- name: first play
  hosts: web
  roles:
    - unbound

- name: second play
  hosts: db
  tasks:
    - name: db task
      debug:
        msg: hi
`
	dir := t.TempDir()
	path := filepath.Join(dir, "site.yml")
	if err := os.WriteFile(path, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	tempPath, cleanup, ok, err := TrimPlaybookToPlay(path, "second play")
	if err != nil {
		t.Fatalf("TrimPlaybookToPlay: %v", err)
	}
	if !ok {
		t.Fatal("TrimPlaybookToPlay: ok = false, want true")
	}
	defer cleanup()

	if filepath.Dir(tempPath) != dir {
		t.Errorf("temp file dir = %q, want %q (must sit beside the original for role/include resolution)", filepath.Dir(tempPath), dir)
	}

	got, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "first play") {
		t.Errorf("trimmed playbook still contains the dropped play:\n%s", got)
	}
	if !strings.Contains(string(got), "second play") || !strings.Contains(string(got), "db task") {
		t.Errorf("trimmed playbook is missing the target play's own content:\n%s", got)
	}

	// A second top-level sequence, re-parsed from the trimmed file itself,
	// must still be exactly one playbook-shaped play - confirms the slice
	// boundary landed on a real list-item boundary, not mid-mapping.
	if names := ListTopLevelPlayNames(tempPath); len(names) != 1 || names[0] != "second play" {
		t.Errorf("re-parsing the trimmed file gave play names %v, want [\"second play\"]", names)
	}

	cleanup()
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Errorf("cleanup() did not remove %q", tempPath)
	}
}

func TestTrimPlaybookToPlay_NoSuchPlay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "site.yml")
	if err := os.WriteFile(path, []byte("- name: only play\n  hosts: all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, cleanup, ok, err := TrimPlaybookToPlay(path, "no such play")
	if err != nil {
		t.Fatalf("TrimPlaybookToPlay: %v", err)
	}
	if ok {
		t.Error("ok = true, want false for a play name that doesn't exist")
	}
	if cleanup != nil {
		t.Error("cleanup != nil, want nil alongside ok = false")
	}
}

func TestTrimPlaybookToPlay_DuplicateNameUsesFirstMatch(t *testing.T) {
	playbookYAML := `- name: dup
  hosts: web
  tasks:
    - name: from first
      debug:
        msg: hi

- name: dup
  hosts: db
  tasks:
    - name: from second
      debug:
        msg: hi
`
	path := filepath.Join(t.TempDir(), "site.yml")
	if err := os.WriteFile(path, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	tempPath, cleanup, ok, err := TrimPlaybookToPlay(path, "dup")
	if err != nil || !ok {
		t.Fatalf("TrimPlaybookToPlay: ok=%v err=%v", ok, err)
	}
	defer cleanup()

	got, err := os.ReadFile(tempPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "from first") {
		t.Errorf("expected the first matching play to win, got:\n%s", got)
	}
}

func TestMergeSourceIndex(t *testing.T) {
	playbookYAML := `- name: first play
  hosts: web
  tasks:
    - name: first play task
      debug:
        msg: hi

- name: second play
  hosts: db
  tasks:
    - name: second play task
      debug:
        msg: hi
`
	path := filepath.Join(t.TempDir(), "site.yml")
	if err := os.WriteFile(path, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	dst, _, _ := BuildTaskSourceIndex(path)
	if !containsSourceFor(dst, "first play task") || !containsSourceFor(dst, "second play task") {
		t.Fatalf("sanity check failed: dst should already index both tasks from the untrimmed file: %v", dst)
	}

	tempPath, cleanup, ok, err := TrimPlaybookToPlay(path, "second play")
	if err != nil || !ok {
		t.Fatalf("TrimPlaybookToPlay: ok=%v err=%v", ok, err)
	}
	defer cleanup()

	before := len(dst)
	MergeSourceIndex(dst, tempPath)

	if len(dst) <= before {
		t.Errorf("MergeSourceIndex added no new entries (got %d, had %d) - want at least one new key for the trimmed file's own path", len(dst), before)
	}
	// The original file's own entries must survive the merge untouched -
	// MergeSourceIndex only ever adds, never removes.
	if !containsSourceFor(dst, "first play task") || !containsSourceFor(dst, "second play task") {
		t.Errorf("MergeSourceIndex must not drop entries already present: %v", dst)
	}
	if !containsSourceFor(dst, "second play task") {
		t.Errorf("expected an entry (old or new) for the trimmed file's own task: %v", dst)
	}

	foundTempKey := false
	for k := range dst {
		if strings.HasPrefix(k, tempPath+":") {
			foundTempKey = true
			break
		}
	}
	if !foundTempKey {
		t.Errorf("expected at least one entry keyed under the trimmed file's own path %q: %v", tempPath, dst)
	}
}

func containsSourceFor(index TaskSourceIndex, taskName string) bool {
	for _, src := range index {
		if strings.Contains(src, "name: "+taskName) {
			return true
		}
	}
	return false
}
