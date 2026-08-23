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

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcHandle(t *testing.T) {
	var h procHandle
	if got := h.Load(); got != nil {
		t.Errorf("Load() on a fresh procHandle = %v, want nil", got)
	}

	cmd := exec.Command("sh", "-c", "sleep 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	defer cmd.Wait()

	h.Store(cmd.Process)
	if got := h.Load(); got != cmd.Process {
		t.Errorf("Load() = %v, want %v", got, cmd.Process)
	}
}

func TestExitCodeOf(t *testing.T) {
	t.Run("nil error means a clean exit", func(t *testing.T) {
		if got := exitCodeOf(nil); got != 0 {
			t.Errorf("exitCodeOf(nil) = %d, want 0", got)
		}
	})

	t.Run("a real ExitError yields the process's actual exit code", func(t *testing.T) {
		err := exec.Command("sh", "-c", "exit 7").Run()
		if got := exitCodeOf(err); got != 7 {
			t.Errorf("exitCodeOf(%v) = %d, want 7", err, got)
		}
	})

	t.Run("a non-ExitError is not a real exit code", func(t *testing.T) {
		if got := exitCodeOf(errors.New("boom")); got != -1 {
			t.Errorf("exitCodeOf(non-ExitError) = %d, want -1", got)
		}
	})
}

func TestStreamStderr(t *testing.T) {
	t.Run("splits into lines", func(t *testing.T) {
		got := streamStderr(strings.NewReader("line one\nline two\n"))
		want := []string{"line one", "line two"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("streamStderr() = %v, want %v", got, want)
		}
	})

	t.Run("empty reader yields no lines", func(t *testing.T) {
		got := streamStderr(strings.NewReader(""))
		if len(got) != 0 {
			t.Errorf("streamStderr(empty) = %v, want no lines", got)
		}
	})
}

func TestScanEvents(t *testing.T) {
	t.Run("valid JSON line then a garbage line", func(t *testing.T) {
		input := `{"_event":"v2_playbook_on_play_start","play":{"name":"my play"}}` + "\n" +
			"not valid json at all\n"
		ch := scanEvents(strings.NewReader(input), nil)

		first := <-ch
		if !first.isEvent || first.ev.Event != "v2_playbook_on_play_start" {
			t.Errorf("first item = %+v, want a decoded v2_playbook_on_play_start event", first)
		}

		second := <-ch
		if second.isEvent {
			t.Errorf("second item = %+v, want isEvent=false for a non-JSON line", second)
		}

		if _, ok := <-ch; ok {
			t.Error("expected the channel to be closed after both lines were consumed")
		}
	})

	t.Run("v2_playbook_on_stats decodes like any other event", func(t *testing.T) {
		input := `{"_event":"v2_playbook_on_stats","stats":{"web1":{"ok":1}}}` + "\n"
		ch := scanEvents(strings.NewReader(input), nil)

		item := <-ch
		if !item.isEvent || item.ev.Event != "v2_playbook_on_stats" {
			t.Errorf("item = %+v, want a decoded v2_playbook_on_stats event", item)
		}
	})

	t.Run("empty reader closes the channel immediately", func(t *testing.T) {
		ch := scanEvents(strings.NewReader(""), nil)
		if _, ok := <-ch; ok {
			t.Error("expected the channel to close with no items for an empty reader")
		}
	})
}

// TestScanEventsTeesRawLinesToLogFile covers design-docs/Revisit.md's own
// save mechanism: every line scanEvents reads is written to logFile
// byte-identical to the input, including a malformed line - before any
// trimming/decoding - so a saved run can later be replayed through this
// exact same scan-and-decode logic.
func TestScanEventsTeesRawLinesToLogFile(t *testing.T) {
	input := `{"_event":"v2_playbook_on_play_start","play":{"name":"my play"}}` + "\n" +
		"not valid json at all\n"

	logPath := filepath.Join(t.TempDir(), "run.jsonl")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("creating log file: %v", err)
	}

	ch := scanEvents(strings.NewReader(input), logFile)
	for range ch {
		// Drain fully - scanEvents closes logFile itself once done, so
		// the file below isn't safe to read until the channel closes.
	}

	got, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading back the tee'd log file: %v", err)
	}
	if string(got) != input {
		t.Errorf("logged content = %q, want byte-identical to input %q", got, input)
	}
}
