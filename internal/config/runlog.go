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

// RunOutputPath is where a generation's merged stdout+stderr - the user's
// own stdout callback's output, design-docs/OwnCallbackPlugin.md's Phase 2
// raw-output view - is saved for the given runID, sibling to RunLogPaths'
// own two files. Kept separate from RunLogPaths rather than widening its
// return values, since most of RunLogPaths' callers only ever want the
// .jsonl.
func RunOutputPath(statePath, runID string) string {
	return filepath.Join(RunsDir(statePath), runID+".stdout")
}

// CreateRunOutput opens <runID>.stdout for writing - same best-effort,
// nil-on-failure, 0o600 contract as CreateRunLog, and for the same reasons
// (it holds the same task output, just rendered by a stdout callback).
func CreateRunOutput(statePath, runID string) *os.File {
	if runID == "" {
		return nil
	}
	path := RunOutputPath(statePath, runID)
	if err := EnsureParentDir(path); err != nil {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// ReadRunOutput reads back CreateRunOutput's own file as lines. ok is
// false when there's nothing saved at all (runID empty, or the file
// missing - e.g. a run saved before this file existed), as opposed to a
// saved-but-empty output, so a caller can tell "no output view for this
// run" apart from "the run printed nothing".
func ReadRunOutput(statePath, runID string) (lines []string, ok bool) {
	if runID == "" {
		return nil, false
	}
	data, err := os.ReadFile(RunOutputPath(statePath, runID))
	if err != nil {
		return nil, false
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil, true
	}
	return strings.Split(text, "\n"), true
}

// CreateRunLog creates statePath's runsDir (if needed) and opens
// <runID>.jsonl for writing - the file spawnGeneration passes to
// scanEvents so it can tee each raw stdout line as the generation runs.
// Best-effort: returns nil on any failure (directory couldn't be created,
// file couldn't be opened) rather than an error - the caller's response is
// always "this generation just isn't revisitable," never anything that
// should interrupt the run itself, same tolerance appendInvocation already
// has for its own failures.
//
// Opened at 0o600, not os.Create's own 0o666 (umask-adjusted) - this file
// holds a generation's complete raw Ansible result payload (module args,
// registered vars, command output, diffs, ...), which ansible's own
// no_log doesn't necessarily scrub before it ever reaches this app (see
// EnsureParentDir's own note). No separate self-heal chmod needed here
// unlike WriteState's - runID (NewRunID) is nanosecond-unique per
// generation, so this always creates a brand new file, never reopens an
// older one that might predate this fix.
func CreateRunLog(statePath, runID string) *os.File {
	jsonlPath, _ := RunLogPaths(statePath, runID)
	if err := EnsureParentDir(jsonlPath); err != nil {
		return nil
	}
	f, err := os.OpenFile(jsonlPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
//
// 0o600, not the previous, explicitly-wrong 0o644 - collected stderr can
// contain the same kind of sensitive task output CreateRunLog's own note
// describes for the sibling .jsonl, and there's no principled reason for
// the two files to have different exposure.
func WriteRunStderr(statePath, runID string, lines []string) {
	if runID == "" {
		return
	}
	_, stderrPath := RunLogPaths(statePath, runID)
	if err := EnsureParentDir(stderrPath); err != nil {
		return
	}
	_ = os.WriteFile(stderrPath, []byte(strings.Join(lines, "\n")), 0o600)
}

// ReadRunStderr reads back a generation's saved stderr (WriteRunStderr's
// own file) - "revisit" uses this to seed design-docs/ErrorOutput.md's
// inline error-output block for a replayed failed run, the same way a
// live one already gets it. Returns nil - not an error - for every case
// that means "nothing to show": runID empty (this generation's own
// CreateRunLog never succeeded, so WriteRunStderr never had anywhere to
// save to either), the file missing (an entry saved before this reader
// existed, or any other reason to just degrade quietly - same tolerance
// every other read/write in this file already has), or the file existing
// but genuinely empty (a generation that reported zero stderr lines -
// WriteRunStderr's own strings.Join(nil, "\n") writes zero bytes for
// that case, which strings.Split would otherwise turn back into a
// single-element []string{""} rather than staying "nothing").
func ReadRunStderr(statePath, runID string) []string {
	if runID == "" {
		return nil
	}
	_, stderrPath := RunLogPaths(statePath, runID)
	data, err := os.ReadFile(stderrPath)
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(string(data), "\n")
}

// DeleteRunLog removes a generation's saved .jsonl/.stderr/.stdout files, if any -
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
	_ = os.Remove(RunOutputPath(statePath, runID))
}
