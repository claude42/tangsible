package eebuild

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseStep(t *testing.T) {
	for _, c := range []struct {
		line     string
		want     Step
		wantText string
		ok       bool
	}{
		{"[2/4] STEP 11/13: RUN mkdir -p /usr/share/ansible", Step{2, 4, 11, 13}, "RUN mkdir -p /usr/share/ansible", true},
		{"\x1b[1m[1/4] STEP 1/12: FROM base AS base\x1b[0m", Step{1, 4, 1, 12}, "FROM base AS base", true},
		{"STEP 3/13: WORKDIR /build", Step{0, 0, 3, 13}, "WORKDIR /build", true},
		{"[4/4] STEP 22/22: COMMIT x", Step{4, 4, 22, 22}, "COMMIT x", true},
		{"  [3/4] STEP 2/13: ENV A=1  ", Step{3, 4, 2, 13}, "ENV A=1", true},
		{"[0/0] STEP 1/1: X", Step{}, "", false},
		{"[2/4] STEP 0/13: X", Step{}, "", false},
		{"+ echo STEP 1/2: not a marker", Step{}, "", false},
		{"Collecting jmespath", Step{}, "", false},
		{"", Step{}, "", false},
	} {
		got, text, ok := ParseStep(c.line)
		if ok != c.ok || got != c.want || text != c.wantText {
			t.Errorf("ParseStep(%q) = %+v, %q, %v; want %+v, %q, %v", c.line, got, text, ok, c.want, c.wantText, c.ok)
		}
	}
	if got := (Step{2, 4, 11, 13}).String(); got != "[2/4] STEP 11/13" {
		t.Errorf("String = %q", got)
	}
	if got := (Step{0, 0, 3, 13}).String(); got != "STEP 3/13" {
		t.Errorf("String without stage = %q", got)
	}
}

// buildExcerpt is from a real `ansible-builder build -v 3` log (stages do not
// run in order; set -x lines and bare layer ids are noise).
const buildExcerpt = `[1/4] STEP 12/12: RUN /output/scripts/pip_install $PYCMD
+ PYCMD=/usr/bin/python3
Requirement already satisfied: pip in /usr/local/lib/python3.14/site-packages (26.0.1)
--> 166933a0c7a6
[3/4] STEP 1/13: FROM a9132481bcab AS builder
[2/4] STEP 13/13: RUN ANSIBLE_GALAXY_DISABLE_GPG_VERIFY=1 ansible-galaxy collection install
Starting galaxy collection install process
--> 6eeb1df65e8a
`

func TestTracker(t *testing.T) {
	var tr Tracker
	if got := tr.Format(5*time.Second, 80); got != "building  5s" {
		t.Errorf("empty: %q", got)
	}
	for _, l := range strings.Split(buildExcerpt, "\n") {
		tr.Feed(l)
	}
	// the latest marker and the latest line that says something (not the layer id)
	if got := tr.Format(72*time.Second, 80); got != "building  1m12s  [2/4] STEP 13/13  Starting galaxy collection install process" {
		t.Errorf("got %q", got)
	}
	// a progress bar's own redrawing, and colours
	tr.Feed("\x1b[32mCopying blob sha256:abc\x1b[0m")
	if got := tr.Format(time.Hour+2*time.Minute+3*time.Second, 200); !strings.HasSuffix(got, "  Copying blob sha256:abc") || !strings.HasPrefix(got, "building  1h2m3s") {
		t.Errorf("got %q", got)
	}
}

func TestFormatWidth(t *testing.T) {
	var tr Tracker
	tr.Feed("[2/4] STEP 11/13: RUN something long that does not fit")
	got := tr.Format(72*time.Second, 40)
	if n := len([]rune(got)); n > 39 {
		t.Errorf("%d columns: %q", n, got)
	}
	if !strings.HasPrefix(got, "building  1m12s  [2/4] STEP 11/13  RUN") || !strings.HasSuffix(got, "…") {
		t.Errorf("got %q", got)
	}
	// narrower than the head: cut, never wrapped
	if got := tr.Format(72*time.Second, 12); len([]rune(got)) > 11 {
		t.Errorf("tiny: %q", got)
	}
	if got := tr.Format(time.Second, 1); got != "" {
		t.Errorf("width 1: %q", got)
	}
	// multi-byte characters are cut on a character boundary
	tr.Feed("ünïcödé ünïcödé ünïcödé ünïcödé")
	got = tr.Format(time.Second, 22)
	if len([]rune(got)) > 21 || !strings.HasSuffix(got, "…") {
		t.Errorf("runes: %q", got)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestProgressWriter(t *testing.T) {
	var out lockedBuffer
	p := NewProgress(&out, func() int { return 100 })
	// chunks that cut lines (and a marker) in two, and a \r-rewritten line
	for _, chunk := range []string{"[1/4] STE", "P 2/12: ARG X\nCopying blob sha256:1\rCopying blob sha256:2\rCopying", " blob sha256:3\n"} {
		if n, err := p.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	p.Close()
	p.Close() // idempotent
	s := out.String()
	if !strings.Contains(s, "\r") || !strings.Contains(s, "[1/4] STEP 2/12") {
		t.Errorf("no status line drawn: %q", s)
	}
	if !strings.HasSuffix(s, "\r\x1b[K") {
		t.Errorf("the line is not erased at the end: %q", s)
	}
	// the marker arrived in two chunks and was drawn once it was whole
	if strings.Contains(s, "STE\x1b") {
		t.Errorf("a partial line was drawn: %q", s)
	}
	if got := p.t.last; got != "Copying blob sha256:3" {
		t.Errorf("latest line = %q", got)
	}
}

func TestOutputShowOutputAndPlain(t *testing.T) {
	var log, errOut bytes.Buffer
	w, stop := Output(&log, &errOut, true)
	w.Write([]byte("line one\n"))
	stop()
	if log.String() != "line one\n" || errOut.String() != "line one\n" {
		t.Errorf("show-output: log %q, stderr %q", log.String(), errOut.String())
	}
	// not a terminal: the log only, nothing drawn
	log.Reset()
	errOut.Reset()
	w, stop = Output(&log, &errOut, false)
	w.Write([]byte("[1/4] STEP 1/2: X\n"))
	stop()
	if log.String() != "[1/4] STEP 1/2: X\n" || errOut.Len() != 0 {
		t.Errorf("plain: log %q, stderr %q", log.String(), errOut.String())
	}
}
