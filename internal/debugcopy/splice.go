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
	"sort"
	"strings"

	"code.aw.net/claude/tangsible/internal/source"
)

// SpliceDebuggerAlways rewrites every file named by locations (resolved
// against a shadow copy built by BuildShadowCopy from playbookDir) to add
// a "debugger: always" sibling key to each task - design-docs/DebugMode.md's
// uniform-splice approach: every task pauses, and the TUI decides
// client-side whether a given pause is a real breakpoint or gets silently
// auto-continued. locations is typically source.ListTaskLocations's own
// result for the original playbook - paths there point at the original
// tree, resolved here against copyDir via their path relative to
// playbookDir.
func SpliceDebuggerAlways(playbookDir, copyDir string, locations []source.TaskLocation) error {
	byFile := map[string][]source.TaskLocation{}
	for _, loc := range locations {
		byFile[loc.Path] = append(byFile[loc.Path], loc)
	}

	for origPath, locs := range byFile {
		rel, err := filepath.Rel(playbookDir, origPath)
		if err != nil {
			return fmt.Errorf("debugcopy: %s is not under %s: %w", origPath, playbookDir, err)
		}
		if err := spliceFile(filepath.Join(copyDir, rel), locs); err != nil {
			return fmt.Errorf("debugcopy: splicing %s: %w", rel, err)
		}
	}
	return nil
}

// BuildPathMapping returns, for every location, the shadow copy's own
// "path:line" string (exactly as ansible-playbook itself will report it
// in a live event's task.path, once run against the spliced copy) mapped
// back to the *original* tree's "path:line" (exactly matching source.
// BuildTaskSourceIndex's own key format, fmt.Sprintf("%s:%d", path,
// start) - source.go:641, confirmed by reading it rather than assumed).
//
// Needed because SpliceDebuggerAlways's own "debugger: always" lines are
// real, inserted lines, not a zero-width marker - every task's own line
// number shifts downward in the copy by however many *other* tasks
// earlier in the same file already got their own line spliced in above
// it. A task with only one sibling in its file (or the first task in a
// file with several) can come out unshifted, which is exactly what made
// this easy to miss by spot-checking a single-task-per-file playbook -
// confirmed live as a real bug on a multi-task file: the drill-down's own
// "Task definition"/"Resolved" tabs (both keyed by task.Path -
// livesession_output.go, uikit/tui_drilldown.go) came back empty for
// every debug session, since task.Path was never set to anything at all
// before this existed, and would otherwise have pointed at the wrong
// line - and sometimes the wrong file's line entirely - once it was.
func BuildPathMapping(playbookDir, copyDir string, locations []source.TaskLocation) map[string]string {
	byFile := map[string][]source.TaskLocation{}
	for _, loc := range locations {
		byFile[loc.Path] = append(byFile[loc.Path], loc)
	}

	mapping := make(map[string]string, len(locations))
	for origPath, locs := range byFile {
		rel, err := filepath.Rel(playbookDir, origPath)
		if err != nil {
			continue // mirrors SpliceDebuggerAlways's own per-file error handling: best-effort, a mapping miss just means that one task's Task definition/Resolved tabs stay empty
		}
		copyPath := filepath.Join(copyDir, rel)
		for _, loc := range locs {
			shift := 0
			for _, other := range locs {
				// Every other task in this same file whose own spliced
				// line lands at or before loc's own start pushes loc
				// down by exactly one line - see spliceFile's own
				// bottom-to-top insertion, which this mirrors without
				// needing to replay it: insertion order never changes
				// which lines end up where, only the order loc.EndLine
				// clamp/blank-trim steps run in. The not-yet-trimmed
				// EndLine is a safe (if occasionally one line high)
				// upper bound for this purpose - trailing blank lines
				// within one task's own span never contain another
				// task's own StartLine for this comparison to miscount.
				if other.Path == loc.Path && other.EndLine <= loc.StartLine {
					shift++
				}
			}
			copyKey := fmt.Sprintf("%s:%d", copyPath, loc.StartLine+shift)
			origKey := fmt.Sprintf("%s:%d", origPath, loc.StartLine)
			mapping[copyKey] = origKey
		}
	}
	return mapping
}

// spliceFile inserts a "debugger: always" sibling key for every loc into
// one file. Processes locations bottom-to-top (highest EndLine first) so
// each insertion's line-number shift never invalidates an
// already-recorded, not-yet-processed location above it.
//
// Critical correctness detail, confirmed live: path is a hardlink shared
// with the original playbook tree (BuildShadowCopy's whole point). Writing
// into it in place would mutate the original file's own content through
// the shared inode. os.Remove breaks that hardlink (removes just this
// directory entry, leaving the original's data and its own remaining
// links untouched) before a fresh file is written at the same path.
func spliceFile(path string, locs []source.TaskLocation) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	sort.Slice(locs, func(i, j int) bool { return locs[i].EndLine > locs[j].EndLine })

	for _, loc := range locs {
		insertAt := loc.EndLine
		if insertAt > len(lines) {
			insertAt = len(lines)
		}
		// Trim trailing blank lines within this task's own span so the
		// spliced key lands right after the task's last real content
		// line, not stranded after a blank-line gap before the next task.
		for insertAt > loc.StartLine && strings.TrimSpace(lines[insertAt-1]) == "" {
			insertAt--
		}

		newLine := strings.Repeat(" ", loc.IndentCols) + "debugger: always"
		lines = append(lines[:insertAt], append([]string{newLine}, lines[insertAt:]...)...)
	}

	if err := os.Remove(path); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
