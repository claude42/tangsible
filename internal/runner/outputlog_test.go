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

package runner

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOutputLogSplitsAndMergesStreams(t *testing.T) {
	l := NewOutputLog()
	// A line split across two writes must stay one line, and the other
	// stream's complete line landing in between must not interleave with it.
	l.Stdout().Write([]byte("PLAY [all] **\nTASK [one"))
	l.Stderr().Write([]byte("[WARNING]: careful\r\n"))
	l.Stdout().Write([]byte("] **\nok: [h1]"))
	if got, want := l.LinesFrom(0), []string{"PLAY [all] **", "[WARNING]: careful", "TASK [one] **"}; !slices.Equal(got, want) {
		t.Fatalf("before Finish: %q, want %q", got, want)
	}
	l.Finish() // flushes the trailing partial "ok: [h1]"
	if got := l.Len(); got != 4 {
		t.Fatalf("Len() after Finish = %d, want 4", got)
	}
	if got, want := l.LinesFrom(2), []string{"TASK [one] **", "ok: [h1]"}; !slices.Equal(got, want) {
		t.Errorf("LinesFrom(2) = %q, want %q", got, want)
	}
	if got := l.LinesFrom(10); got != nil {
		t.Errorf("LinesFrom past the end = %q, want nil", got)
	}
}

func TestOutputLogAttachFileWritesBacklogAndLaterLines(t *testing.T) {
	l := NewOutputLog()
	l.Stdout().Write([]byte("early\n"))
	path := filepath.Join(t.TempDir(), "out.stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	l.AttachFile(f)
	l.Stderr().Write([]byte("late"))
	l.Finish()
	l.Finish() // idempotent - must not double-close or re-flush
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "early\nlate\n" {
		t.Errorf("saved file = %q, want %q", got, "early\nlate\n")
	}
}

func TestStripAndSplitANSI(t *testing.T) {
	colored := "\x1b[0;32mok: [h1]\x1b[0m done"
	if got := StripANSI(colored); got != "ok: [h1] done" {
		t.Errorf("StripANSI() = %q", got)
	}
	segs, esc := SplitANSI(colored)
	if want := []string{"\x1b[0;32m", "ok: [h1]", "\x1b[0m", " done"}; !slices.Equal(segs, want) {
		t.Errorf("SplitANSI() segments = %q, want %q", segs, want)
	}
	if want := []bool{true, false, true, false}; !slices.Equal(esc, want) {
		t.Errorf("SplitANSI() isEscape = %v, want %v", esc, want)
	}
	if got := strings.Join(segs, ""); got != colored {
		t.Errorf("segments don't rejoin to the input: %q", got)
	}
}

func TestStreamStderrStripsANSI(t *testing.T) {
	got := StreamStderr(strings.NewReader("\x1b[1;35m[WARNING]: x\x1b[0m\n"))
	if want := []string{"[WARNING]: x"}; !slices.Equal(got, want) {
		t.Errorf("StreamStderr() = %q, want %q", got, want)
	}
}
