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

// Per-generation saved run data (design-docs/Revisit.md): each
// ansible-playbook generation's raw jsonl stdout and collected stderr are
// saved to a runs/ directory sibling to state.toml (typically
// .tangsible/runs/), named by the invocationRecord.RunID (history.go) that
// references them. Phase 1 (this file) only ever writes and deletes these
// files - nothing yet reads them back; that's the "revisit" verb itself, a
// later phase.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunsDir returns the runs/ directory sibling to statePath's own directory
// (typically .tangsible/state.toml -> .tangsible/runs). Derived from
// statePath itself rather than a fixed global (unlike tangsibleStatePath,
// which every real call site does pass through unchanged) so a caller that
// passes a test-local state.toml path gets an isolated, test-local runs/
// dir too - exactly like state.toml itself already is via readState/
// writeState.
func RunsDir(statePath string) string {
	return filepath.Join(filepath.Dir(statePath), "runs")
}

// NewRunID names one generation's saved run data - nanosecond-precision so
// collisions are a non-issue at this app's own scale (a handful of
// generations per session, one project worked in at a time). No colons or
// other path-hostile characters, so it doubles directly as a filename stem.
func NewRunID(t time.Time) string {
	return t.UTC().Format("20060102T150405.000000000") + "Z"
}

// RunLogPaths returns the two file paths a generation's saved run data
// lives in for the given runID, under statePath's own runsDir.
func RunLogPaths(statePath, runID string) (jsonlPath, stderrPath string) {
	dir := RunsDir(statePath)
	return filepath.Join(dir, runID+".jsonl"), filepath.Join(dir, runID+".stderr")
}

// CreateRunLog creates statePath's runsDir (if needed) and opens
// <runID>.jsonl for writing - the file spawnGeneration passes to
// scanEvents so it can tee each raw stdout line as the generation runs.
// Best-effort: returns nil on any failure (directory couldn't be created,
// file couldn't be opened) rather than an error - the caller's response is
// always "this generation just isn't revisitable," never anything that
// should interrupt the run itself, same tolerance appendInvocation already
// has for its own failures.
func CreateRunLog(statePath, runID string) *os.File {
	jsonlPath, _ := RunLogPaths(statePath, runID)
	if err := EnsureParentDir(jsonlPath); err != nil {
		return nil
	}
	f, err := os.Create(jsonlPath)
	if err != nil {
		return nil
	}
	return f
}

// WriteRunStderr saves lines (already fully collected by runGeneration, via
// streamStderr) to <runID>.stderr, one per line - a plain text file,
// sibling to the same generation's .jsonl. A no-op if runID is empty (this
// generation's own createRunLog never succeeded, so there's nowhere for
// stderr to live either). Best-effort, same reasoning as createRunLog - a
// failure here doesn't invalidate the jsonl side, so it isn't gated on this
// succeeding.
func WriteRunStderr(statePath, runID string, lines []string) {
	if runID == "" {
		return
	}
	_, stderrPath := RunLogPaths(statePath, runID)
	if err := EnsureParentDir(stderrPath); err != nil {
		return
	}
	_ = os.WriteFile(stderrPath, []byte(strings.Join(lines, "\n")), 0o644)
}

// DeleteRunLog removes a generation's saved .jsonl/.stderr files, if any -
// called by appendInvocation when the invocationRecord referencing them is
// evicted from history (the maxHistoryPerPlaybook cap). A no-op if runID is
// empty (nothing was ever saved for that entry). Best-effort: errors
// (including "file doesn't exist," e.g. a save that only partially
// succeeded) are ignored - a leftover file from a failed delete is no worse
// than one this program never knew to try deleting in the first place.
func DeleteRunLog(statePath, runID string) {
	if runID == "" {
		return
	}
	jsonlPath, stderrPath := RunLogPaths(statePath, runID)
	_ = os.Remove(jsonlPath)
	_ = os.Remove(stderrPath)
}
