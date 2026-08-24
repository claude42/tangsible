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

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRunIDFormatAndUniqueness(t *testing.T) {
	t1 := time.Date(2026, 8, 23, 15, 0, 0, 123456789, time.UTC)
	got := NewRunID(t1)
	want := "20260823T150000.123456789Z"
	if got != want {
		t.Errorf("newRunID() = %q, want %q", got, want)
	}

	// No path-hostile characters (this doubles directly as a filename
	// stem) - specifically no colons, which the timestamp's own hh:mm:ss
	// would otherwise contain.
	if filepath.Base(got) != got {
		t.Errorf("newRunID() = %q, not a bare filename stem", got)
	}

	// Nanosecond precision means two calls a handful of nanoseconds apart
	// - the realistic gap between two generations in the same session -
	// never collide.
	t2 := t1.Add(1)
	if NewRunID(t2) == got {
		t.Error("newRunID() produced the same id for two distinct nanosecond timestamps")
	}
}

func TestRunsDirIsSiblingOfStatePath(t *testing.T) {
	got := RunsDir(filepath.Join("project", ".tangsible", "state.toml"))
	want := filepath.Join("project", ".tangsible", "runs")
	if got != want {
		t.Errorf("runsDir() = %q, want %q", got, want)
	}
}

func TestCreateRunLogWriteReadDelete(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".tangsible", "state.toml")
	runID := "20260823T150000.000000000Z"

	f := CreateRunLog(statePath, runID)
	if f == nil {
		t.Fatal("createRunLog() = nil, want a real file")
	}
	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatalf("writing to the created log: %v", err)
	}
	f.Close()

	jsonlPath, stderrPath := RunLogPaths(statePath, runID)
	got, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("reading back the jsonl file: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("jsonl content = %q, want %q", got, "hello\n")
	}

	WriteRunStderr(statePath, runID, []string{"line one", "line two"})
	gotStderr, err := os.ReadFile(stderrPath)
	if err != nil {
		t.Fatalf("reading back the stderr file: %v", err)
	}
	if string(gotStderr) != "line one\nline two" {
		t.Errorf("stderr content = %q, want %q", gotStderr, "line one\nline two")
	}

	DeleteRunLog(statePath, runID)
	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Errorf("jsonl file still exists after deleteRunLog() (stat err=%v)", err)
	}
	if _, err := os.Stat(stderrPath); !os.IsNotExist(err) {
		t.Errorf("stderr file still exists after deleteRunLog() (stat err=%v)", err)
	}
}

// TestRunLogEmptyRunIDIsANoOp covers spawnGeneration's own convention: an
// empty runID means createRunLog never actually opened anything (e.g. the
// runs/ directory couldn't be created), so writeRunStderr/deleteRunLog must
// tolerate being called with "" and do nothing, rather than erroring or
// operating on some literal "<statePath's runs dir>/.jsonl" file.
func TestRunLogEmptyRunIDIsANoOp(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".tangsible", "state.toml")

	// Neither call should panic or create anything under runsDir.
	WriteRunStderr(statePath, "", []string{"should never be written"})
	DeleteRunLog(statePath, "")

	if _, err := os.Stat(RunsDir(statePath)); !os.IsNotExist(err) {
		t.Errorf("runsDir() was created despite an empty runID (stat err=%v)", err)
	}
}

func TestDeleteRunLogMissingFilesIsHarmless(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), ".tangsible", "state.toml")
	// Nothing was ever created for this runID - deleteRunLog must not
	// error out or panic on a plain "file doesn't exist."
	DeleteRunLog(statePath, "20260823T150000.000000000Z")
}
