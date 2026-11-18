package eebuild

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// Step is a marker podman prints as a build step starts:
// "[2/4] STEP 11/13: RUN ...". Stage is 0 where the marker has no stage
// (older podman prints "STEP 3/13: ..."). A stage's step count is printed only
// when that stage starts and the stages do not run in order, so there is no
// overall percentage to derive from these.
type Step struct {
	Stage, Stages int
	Step, Steps   int
}

var (
	stepMarker   = regexp.MustCompile(`^(?:\[(\d+)/(\d+)\] )?STEP (\d+)/(\d+): ?(.*)$`)
	ansiEscape   = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")
	layerHashRow = regexp.MustCompile(`^--> [0-9a-f]{12,}$`)
)

// ParseStep reads a step marker from line (colours are ignored); text is what
// the step does ("RUN mkdir ...").
func ParseStep(line string) (s Step, text string, ok bool) {
	m := stepMarker.FindStringSubmatch(strings.TrimSpace(ansiEscape.ReplaceAllString(line, "")))
	if m == nil {
		return Step{}, "", false
	}
	n := func(i int) int { v, _ := strconv.Atoi(m[i]); return v }
	s = Step{Stage: n(1), Stages: n(2), Step: n(3), Steps: n(4)}
	if s.Step == 0 || s.Steps == 0 || (m[1] != "" && (s.Stage == 0 || s.Stages == 0)) {
		return Step{}, "", false
	}
	return s, m[5], true
}

func (s Step) String() string {
	if s.Stage == 0 {
		return fmt.Sprintf("STEP %d/%d", s.Step, s.Steps)
	}
	return fmt.Sprintf("[%d/%d] STEP %d/%d", s.Stage, s.Stages, s.Step, s.Steps)
}

// Tracker follows a build's output: the latest step marker and the latest
// line worth showing.
type Tracker struct {
	step    Step
	hasStep bool
	last    string
}

// Feed takes one line of output (without its line end).
func (t *Tracker) Feed(line string) {
	line = strings.TrimSpace(ansiEscape.ReplaceAllString(line, ""))
	if line == "" {
		return
	}
	if s, text, ok := ParseStep(line); ok {
		t.step, t.hasStep, t.last = s, true, text
		return
	}
	// A bare layer id says nothing; the line before it does.
	if layerHashRow.MatchString(line) {
		return
	}
	t.last = line
}

// Format is the status line for a build that has run for elapsed, at most
// width columns wide (one less, so the terminal never wraps it; a wrapped line
// cannot be redrawn): "building  1m12s  [2/4] STEP 11/13  Collecting jmespath".
// The time and the marker are kept before the text is cut.
func (t *Tracker) Format(elapsed time.Duration, width int) string {
	head := "building  " + elapsed.Round(time.Second).String()
	if t.hasStep {
		head += "  " + t.step.String()
	}
	line := head
	if t.last != "" {
		line += "  " + t.last
	}
	return truncate(line, width-1)
}

// truncate cuts s to at most n display columns (runes), ending in an ellipsis
// when it had to cut.
func truncate(s string, n int) string {
	r := []rune(s)
	switch {
	case n <= 0:
		return ""
	case len(r) <= n:
		return s
	case n == 1:
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// Progress is the status line: an io.Writer for the build's output that keeps
// a Tracker and redraws one line on out - once a second and on each new step
// marker - until Close erases it. Writes never fail.
type Progress struct {
	out   io.Writer
	width func() int
	start time.Time

	mu      sync.Mutex
	t       Tracker
	partial []byte
	closed  bool

	stop chan struct{}
	done chan struct{}
}

// NewProgress starts the status line on out; width reports the terminal's
// width when asked.
func NewProgress(out io.Writer, width func() int) *Progress {
	p := &Progress{out: out, width: width, start: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-tick.C:
				p.mu.Lock()
				p.drawLocked()
				p.mu.Unlock()
			}
		}
	}()
	return p
}

// Write splits what it is given into lines on "\n" and "\r" (a progress bar
// that rewrites itself is its latest state), keeping a partial line until its
// end arrives.
func (p *Progress) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	before := p.t.step
	data := append(p.partial, b...)
	start := 0
	for i, c := range data {
		if c == '\n' || c == '\r' {
			p.t.Feed(string(data[start:i]))
			start = i + 1
		}
	}
	p.partial = append([]byte(nil), data[start:]...)
	if p.t.step != before {
		p.drawLocked()
	}
	return len(b), nil
}

func (p *Progress) drawLocked() {
	if p.closed {
		return
	}
	fmt.Fprintf(p.out, "\r%s\x1b[K", p.t.Format(time.Since(p.start), p.width()))
}

// Close stops the redraws and erases the line.
func (p *Progress) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	close(p.stop)
	<-p.done
	fmt.Fprint(p.out, "\r\x1b[K")
}

// terminalWidth is the width of the terminal behind f, else $COLUMNS, else 80.
func terminalWidth(f *os.File) func() int {
	return func() int {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
		if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
			return w
		}
		return 80
	}
}

// Output is where a build's output goes besides the log file: the status line
// on a terminal, the complete output with showOutput (terminal or not), or
// nothing. The returned writer is the one to give Execute; stop erases the
// status line and must be called when the build has ended.
func Output(logFile io.Writer, stderr io.Writer, showOutput bool) (w io.Writer, stop func()) {
	if showOutput {
		return io.MultiWriter(logFile, stderr), func() {}
	}
	if f, ok := stderr.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		p := NewProgress(f, terminalWidth(f))
		return io.MultiWriter(logFile, p), p.Close
	}
	return logFile, func() {}
}
