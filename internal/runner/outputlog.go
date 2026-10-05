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
	"bytes"
	"io"
	"os"
	"regexp"
	"sync"
)

// OutputLog is one generation's console output - the user's own stdout
// callback's output merged with stderr, in arrival order, the way a plain
// ansible-playbook run would have printed it to the terminal
// (design-docs/OwnCallbackPlugin.md's Phase 2 raw-output view). Lines are
// kept raw, ANSI color codes included (SpawnGeneration forces color on);
// rendering them is the viewer's job.
//
// Ordering between the two streams is arrival order across two separate
// pipes, so it can differ from a real terminal's by a line when stdout and
// stderr are written within the same instant - accepted; a single shared
// pipe would be exact but would lose stderr's own separate collection
// (StreamStderr), which the error-output rows and .stderr file depend on.
//
// Safe for concurrent use: exec's stdout copy goroutine and StreamStderr's
// reader append, the TUI's event loop reads.
type OutputLog struct {
	mu     sync.Mutex
	lines  []string
	file   *os.File
	stdout outputStream
	stderr outputStream
}

// outputStream splits one stream's writes into lines, holding back a
// trailing partial line until its newline (or Finish) arrives - so a line
// written in two chunks never shows up as two lines, and never interleaves
// with the other stream mid-line.
type outputStream struct {
	log     *OutputLog
	partial []byte
}

// NewOutputLog returns an empty, live log - SpawnGeneration's own.
func NewOutputLog() *OutputLog {
	l := &OutputLog{}
	l.stdout.log = l
	l.stderr.log = l
	return l
}

// NewOutputLogFromLines returns an already-finished log holding lines -
// "revisit"'s replay of a saved .stdout file.
func NewOutputLogFromLines(lines []string) *OutputLog {
	l := NewOutputLog()
	l.lines = lines
	return l
}

// Stdout/Stderr are the writers SpawnGeneration feeds the child's two
// streams into.
func (l *OutputLog) Stdout() io.Writer { return &l.stdout }
func (l *OutputLog) Stderr() io.Writer { return &l.stderr }

func (w *outputStream) Write(p []byte) (int, error) {
	w.log.mu.Lock()
	defer w.log.mu.Unlock()
	data := p
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			w.partial = append(w.partial, data...)
			return len(p), nil
		}
		line := append(w.partial, data[:i]...)
		w.log.appendLocked(string(bytes.TrimSuffix(line, []byte("\r"))))
		w.partial = w.partial[:0]
		data = data[i+1:]
	}
}

func (w *outputStream) flushLocked() {
	if len(w.partial) > 0 {
		w.log.appendLocked(string(bytes.TrimSuffix(w.partial, []byte("\r"))))
		w.partial = nil
	}
}

func (l *OutputLog) appendLocked(line string) {
	l.lines = append(l.lines, line)
	l.writeFileLocked(line)
}

func (l *OutputLog) writeFileLocked(line string) {
	if l.file == nil {
		return
	}
	if _, err := l.file.WriteString(line + "\n"); err != nil {
		// Best-effort, same as every other run-data file: stop trying
		// rather than failing every later line the same way.
		l.file.Close()
		l.file = nil
	}
}

// AttachFile starts saving to f (config.CreateRunOutput's own file),
// beginning with every line already collected - SpawnGeneration only knows
// the generation's RunID once the child has already started, and output
// can arrive before that. A nil f is a no-op.
func (l *OutputLog) AttachFile(f *os.File) {
	if f == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file = f
	for _, line := range l.lines {
		l.writeFileLocked(line)
	}
}

// Finish flushes any trailing partial lines and closes the saved file.
// Called once the child has exited and both streams are fully drained;
// idempotent.
func (l *OutputLog) Finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stdout.flushLocked()
	l.stderr.flushLocked()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

// Len reports how many complete lines have been collected so far.
func (l *OutputLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

// LinesFrom returns a copy of every line from index from onward - the
// viewer's "what's new since I last looked".
func (l *OutputLog) LinesFrom(from int) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if from >= len(l.lines) {
		return nil
	}
	return append([]string(nil), l.lines[from:]...)
}

// ansiSequence matches the CSI escape sequences (SGR color codes, in
// practice) ansible-playbook emits once color is forced on.
var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// StripANSI removes ANSI CSI escape sequences from s.
func StripANSI(s string) string {
	if !bytes.Contains([]byte(s), []byte{0x1b}) {
		return s
	}
	return ansiSequence.ReplaceAllString(s, "")
}

// SplitANSI splits s into alternating plain-text and escape-sequence
// segments, in order - isEscape[i] tells which. Lets a renderer escape only
// the plain text for its own markup (tview treats a literal "[" as a tag)
// without mangling the escape sequences themselves.
func SplitANSI(s string) (segments []string, isEscape []bool) {
	pos := 0
	for _, m := range ansiSequence.FindAllStringIndex(s, -1) {
		if m[0] > pos {
			segments = append(segments, s[pos:m[0]])
			isEscape = append(isEscape, false)
		}
		segments = append(segments, s[m[0]:m[1]])
		isEscape = append(isEscape, true)
		pos = m[1]
	}
	if pos < len(s) {
		segments = append(segments, s[pos:])
		isEscape = append(isEscape, false)
	}
	return segments, isEscape
}
