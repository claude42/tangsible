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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/source"
	"gopkg.in/yaml.v3"
)

func TestSpliceDebuggerAlways_InsertsAtCorrectIndentAndPreservesOriginal(t *testing.T) {
	proj := t.TempDir()
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
`
	playbookPath := filepath.Join(proj, "playbook.yml")
	writeFile(t, playbookPath, playbook)

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 2 {
		t.Fatalf("expected 2 task locations, got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	// The original must be byte-identical to before, and the copy must no
	// longer share an inode with it (unlink-then-write, not an in-place
	// write through the shared hardlink).
	origData, err := os.ReadFile(playbookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(origData) != playbook {
		t.Errorf("original playbook was mutated:\n%s", origData)
	}
	if sameInode(t, playbookPath, filepath.Join(copyDir, "playbook.yml")) {
		t.Errorf("copy still shares an inode with the original after splicing - would have corrupted it")
	}

	splicedData, err := os.ReadFile(filepath.Join(copyDir, "playbook.yml"))
	if err != nil {
		t.Fatal(err)
	}
	spliced := string(splicedData)

	// The result must still be valid YAML with both tasks intact and a
	// debugger: always key on each - the actual property that matters,
	// not a specific line layout.
	var doc yaml.Node
	if err := yaml.Unmarshal(splicedData, &doc); err != nil {
		t.Fatalf("spliced file is not valid YAML: %v\n---\n%s", err, spliced)
	}
	if n := strings.Count(spliced, "debugger: always"); n != 2 {
		t.Errorf("expected 2 occurrences of 'debugger: always', got %d:\n%s", n, spliced)
	}
	if !strings.Contains(spliced, "a long\n          folded description") {
		t.Errorf("multiline task's folded scalar was corrupted:\n%s", spliced)
	}
	if !strings.Contains(spliced, "register: reg") {
		t.Errorf("multiline task's register: key was lost:\n%s", spliced)
	}

	// Re-index the spliced copy itself and confirm every task's own
	// re-derived location still parses as a normal task (sanity check
	// that the splice didn't silently break structure in a way that just
	// happens to still be valid YAML, e.g. folding two tasks into one).
	locsAfter := source.ListTaskLocations(filepath.Join(copyDir, "playbook.yml"))
	if len(locsAfter) != 2 {
		t.Errorf("expected 2 task locations after splicing, got %d: %+v", len(locsAfter), locsAfter)
	}
}

func TestSpliceDebuggerAlways_MultipleTasksInOneFileProcessedBottomUp(t *testing.T) {
	proj := t.TempDir()
	playbook := `- hosts: all
  tasks:
    - name: task one
      command: echo one
    - name: task two
      command: echo two
    - name: task three
      command: echo three
`
	playbookPath := filepath.Join(proj, "playbook.yml")
	writeFile(t, playbookPath, playbook)

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 3 {
		t.Fatalf("expected 3 task locations, got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	splicedData, err := os.ReadFile(filepath.Join(copyDir, "playbook.yml"))
	if err != nil {
		t.Fatal(err)
	}
	spliced := string(splicedData)

	var doc yaml.Node
	if err := yaml.Unmarshal(splicedData, &doc); err != nil {
		t.Fatalf("spliced file is not valid YAML: %v\n---\n%s", err, spliced)
	}
	if n := strings.Count(spliced, "debugger: always"); n != 3 {
		t.Errorf("expected 3 occurrences of 'debugger: always', got %d:\n%s", n, spliced)
	}
	for _, name := range []string{"task one", "task two", "task three"} {
		if !strings.Contains(spliced, "name: "+name) {
			t.Errorf("lost task %q:\n%s", name, spliced)
		}
	}
}

// TestBuildPathMapping_AccountsForEveryEarlierSplicedLine confirms the
// mapping's own line-shift math against the *real* spliced file's own
// actual line numbers (found by searching it directly), not just against
// a hand-derived expectation - the whole point being to catch exactly
// the kind of off-by-N error that's easy to get wrong on paper.
func TestBuildPathMapping_AccountsForEveryEarlierSplicedLine(t *testing.T) {
	proj := t.TempDir()
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: task one
      command: echo one

    - name: task two
      command: echo two

    - name: task three
      command: echo three
`
	playbookPath := filepath.Join(proj, "playbook.yml")
	writeFile(t, playbookPath, playbook)

	locs := source.ListTaskLocations(playbookPath)
	if len(locs) != 3 {
		t.Fatalf("expected 3 task locations, got %d: %+v", len(locs), locs)
	}

	copyDir, cleanup, err := BuildShadowCopy(proj, "run1")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := SpliceDebuggerAlways(proj, copyDir, locs); err != nil {
		t.Fatal(err)
	}

	mapping := BuildPathMapping(proj, copyDir, locs)

	splicedData, err := os.ReadFile(filepath.Join(copyDir, "playbook.yml"))
	if err != nil {
		t.Fatal(err)
	}
	splicedLines := strings.Split(string(splicedData), "\n")

	for _, name := range []string{"task one", "task two", "task three"} {
		// Find this task's own real line number (1-indexed) in the
		// actual spliced file - the ground truth BuildPathMapping's own
		// arithmetic must agree with.
		wantCopyLine := -1
		for i, line := range splicedLines {
			if strings.Contains(line, "name: "+name) {
				wantCopyLine = i + 1
				break
			}
		}
		if wantCopyLine == -1 {
			t.Fatalf("couldn't find %q in the spliced file:\n%s", name, string(splicedData))
		}

		var loc *source.TaskLocation
		for i := range locs {
			if strings.HasSuffix(strings.TrimSpace(strings.Split(playbook, "\n")[locs[i].StartLine-1]), name) {
				loc = &locs[i]
				break
			}
		}
		if loc == nil {
			t.Fatalf("couldn't find original TaskLocation for %q", name)
		}

		copyKey := mapping[fmt.Sprintf("%s:%d", filepath.Join(copyDir, "playbook.yml"), wantCopyLine)]
		wantOrigKey := fmt.Sprintf("%s:%d", playbookPath, loc.StartLine)
		if copyKey != wantOrigKey {
			t.Errorf("%s: mapping[copy:%d] = %q, want %q (the task's own real original line)", name, wantCopyLine, copyKey, wantOrigKey)
		}
	}
}
