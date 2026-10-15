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

func TestListTaskLocations(t *testing.T) {
	dir := t.TempDir()
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: simple task
      command: echo hi

    - name: multiline task
      debug:
        msg: >
          a long
          folded description
      register: reg

    - name: flow wrapper
      block:
        - { name: inner flow task, command: echo flow }

    - block:
        - name: nested in block
          command: echo nested
      rescue:
        - name: nested in rescue
          command: echo rescued
`
	path := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(path, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(playbook, "\n")

	locs := ListTaskLocations(path)

	byStart := map[int]TaskLocation{}
	for _, l := range locs {
		byStart[l.StartLine] = l
	}

	// "simple task" starts at line 4, indented 6 cols ("      name: ...").
	simple, ok := byStart[4]
	if !ok {
		t.Fatalf("no location for simple task at line 4 (locs: %v)", locs)
	}
	if simple.IndentCols != 6 {
		t.Errorf("simple task IndentCols = %d, want 6", simple.IndentCols)
	}
	if simple.EndLine < simple.StartLine {
		t.Errorf("simple task EndLine %d < StartLine %d", simple.EndLine, simple.StartLine)
	}

	// "multiline task" starts at line 7; its EndLine must not land inside
	// the folded scalar value (lines 9-11) - splicing there would corrupt it.
	multi, ok := byStart[7]
	if !ok {
		t.Fatalf("no location for multiline task at line 7 (locs: %v)", locs)
	}
	if multi.EndLine < 12 {
		t.Errorf("multiline task EndLine = %d, want >= 12 (register: reg) so a splice can't land inside the folded scalar", multi.EndLine)
	}

	// The flow-style inner task ("- { name: inner flow task, ... }") must
	// be skipped - a line-based splice can't safely extend it.
	for _, l := range locs {
		if strings.Contains(lines[l.StartLine-1], "inner flow task") {
			t.Errorf("flow-style task at line %d should have been skipped, got a location: %+v", l.StartLine, l)
		}
	}

	// block:/rescue: wrappers themselves (lines 14 and 18) are skipped,
	// but the real tasks nested inside them are still found.
	for _, wrapperLine := range []int{14, 18} {
		if _, ok := byStart[wrapperLine]; ok {
			t.Errorf("block/rescue wrapper at line %d should have been skipped as a non-task", wrapperLine)
		}
	}
	foundNested := 0
	for _, l := range locs {
		text := lines[l.StartLine-1]
		if strings.Contains(text, "nested in block") || strings.Contains(text, "nested in rescue") {
			foundNested++
		}
	}
	if foundNested != 2 {
		t.Errorf("expected 2 nested block/rescue task locations, got %d (locs: %+v)", foundNested, locs)
	}
}

// TestListTaskLocations_SkipsLeftoverTangsibleDir guards a real scenario,
// not just a hypothetical one: a debug session killed before its own
// cleanup runs (crash, kill -9, a tmux session torn down instead of
// quitting via "q") leaves a stale ".tangsible/debug/<runID>/" shadow copy
// behind under the playbook's own directory - itself a full copy of the
// playbook's directory tree, debugger: already spliced onto every task.
// A later debug invocation's own ListTaskLocations must not walk into it:
// doing so produces locations pointing at files the *new* shadow copy
// never receives (debugcopy.BuildShadowCopy excludes ".tangsible" when
// building one), which previously made SpliceDebuggerAlways fail with a
// plain "no such file or directory" for a reason nothing in that error
// pointed back to.
func TestListTaskLocations_SkipsLeftoverTangsibleDir(t *testing.T) {
	dir := t.TempDir()
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: real task
      command: echo hi
`
	path := filepath.Join(dir, "playbook.yml")
	if err := os.WriteFile(path, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := filepath.Join(dir, ".tangsible", "debug", "stale-run")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	stalePlaybook := `- hosts: all
  gather_facts: false
  tasks:
    - name: real task
      command: echo hi
      debugger: always
`
	if err := os.WriteFile(filepath.Join(stale, "playbook.yml"), []byte(stalePlaybook), 0o644); err != nil {
		t.Fatal(err)
	}

	locs := ListTaskLocations(path)
	if len(locs) != 1 {
		t.Fatalf("expected 1 location (the real playbook's own task only), got %d: %+v", len(locs), locs)
	}
	for _, l := range locs {
		if strings.Contains(l.Path, ".tangsible") {
			t.Errorf("location %+v points inside the leftover .tangsible dir, should have been skipped", l)
		}
	}

	index, _, names := BuildTaskSourceIndex(path)
	if len(index) != 1 {
		t.Errorf("BuildTaskSourceIndex: expected 1 indexed source, got %d: %v", len(index), index)
	}
	if len(names) != 1 {
		t.Errorf("BuildTaskSourceIndex: expected 1 task name, got %d: %v", len(names), names)
	}
}
