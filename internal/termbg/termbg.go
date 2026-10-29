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

// Package termbg detects whether the terminal has a dark or light
// background (general.color_scheme = "auto", design-docs/ColorConfig.md),
// by asking it: OSC 11 ("report background color") followed by DA1
// ("primary device attributes"). A terminal answers queries in order, and
// every terminal answers DA1, so the DA1 reply doubles as an end-of-replies
// marker: a terminal that doesn't know OSC 11 (mosh, the Linux console,
// many multiplexers) is recognized the moment its DA1 reply arrives, with
// no timeout to sit out, and no late reply is left to leak into the TUI as
// keystrokes. The query is sent by Start and collected by Wait so the
// round trip can overlap other start-up work. $COLORFGBG (rxvt convention,
// static) is the fallback when the terminal can't be asked.
package termbg

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Result is a detected background: OK is false when nothing could be
// determined (the caller keeps its default).
type Result struct {
	Light bool
	OK    bool
}

// queryString asks for the background color (OSC 11, ST-terminated), then
// for device attributes (DA1) as the end-of-replies marker.
const queryString = "\x1b]11;?\x1b\\" + "\x1b[c"

// replyTimeout bounds the wait for terminals that don't even answer DA1.
const replyTimeout = 500 * time.Millisecond

// source delivers the terminal's reply bytes - an interface so the read
// loop is testable without a tty.
type source interface {
	// read returns what arrives within timeout (nil, nil on timeout).
	read(timeout time.Duration) ([]byte, error)
}

// Query is a background query in flight.
type Query struct {
	done chan Result
	once sync.Once
	res  Result
}

// Start sends the query (if stdin and stdout are a terminal that can be
// asked) and returns immediately; a nil Query is valid and reports "not
// determined". The terminal is put into no-echo, non-canonical mode until
// Wait's result is ready - a few milliseconds locally, one round trip over
// ssh - so a reply never shows up on screen.
func Start() *Query {
	q := &Query{done: make(chan Result, 1)}
	if !openAndAsk(q) {
		q.done <- fromColorFGBG(os.Getenv("COLORFGBG"))
	}
	return q
}

// Wait returns the result, blocking until it is ready (at most
// replyTimeout after Start). Safe to call repeatedly.
func (q *Query) Wait() Result {
	if q == nil {
		return Result{}
	}
	q.once.Do(func() { q.res = <-q.done })
	return q.res
}

// finish is what the reading goroutine delivers: the terminal's answer,
// else $COLORFGBG.
func finish(r Result) Result {
	if r.OK {
		return r
	}
	return fromColorFGBG(os.Getenv("COLORFGBG"))
}

// detect reads replies from src until the DA1 reply arrives (the terminal
// has answered everything it is going to), or timeout passes.
func detect(src source, timeout time.Duration) Result {
	var buf []byte
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		b, err := src.read(remaining)
		if err != nil {
			break
		}
		buf = append(buf, b...)
		if bytes.Contains(buf, []byte("\x1b[?")) && da1Complete(buf) {
			break
		}
	}
	r, g, b, ok := parseOSC11(buf)
	if !ok {
		return Result{}
	}
	return Result{Light: perceivedBrightness(r, g, b) > 0.5, OK: true}
}

// da1Complete reports whether buf holds a whole DA1 reply (ESC [ ? ... c).
func da1Complete(buf []byte) bool {
	i := bytes.Index(buf, []byte("\x1b[?"))
	if i < 0 {
		return false
	}
	return bytes.IndexByte(buf[i:], 'c') >= 0
}

// parseOSC11 extracts the color from a reply of the form
// ESC ] 11 ; rgb:RRRR/GGGG/BBBB (BEL | ESC \) - 1 to 4 hex digits per
// component, "rgba:" (with a trailing alpha, ignored) accepted too - as
// 0..1 fractions.
func parseOSC11(buf []byte) (r, g, b float64, ok bool) {
	i := bytes.Index(buf, []byte("\x1b]11;"))
	if i < 0 {
		return 0, 0, 0, false
	}
	body := buf[i+len("\x1b]11;"):]
	end := bytes.IndexAny(body, "\x07\x1b")
	if end < 0 {
		return 0, 0, 0, false // terminator not here (yet)
	}
	spec := string(body[:end])
	spec, found := strings.CutPrefix(spec, "rgba:")
	if !found {
		if spec, found = strings.CutPrefix(string(body[:end]), "rgb:"); !found {
			return 0, 0, 0, false
		}
	}
	parts := strings.Split(spec, "/")
	if len(parts) < 3 {
		return 0, 0, 0, false
	}
	var c [3]float64
	for i := range c {
		v, ok := hexFraction(parts[i])
		if !ok {
			return 0, 0, 0, false
		}
		c[i] = v
	}
	return c[0], c[1], c[2], true
}

// hexFraction parses 1-4 hex digits as a fraction of the maximum for that
// many digits.
func hexFraction(s string) (float64, bool) {
	if len(s) < 1 || len(s) > 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, false
	}
	max := float64(uint64(1)<<(4*uint(len(s))) - 1)
	return float64(n) / max, true
}

// perceivedBrightness is the usual weighted channel average (0 black,
// 1 white); a mid-gray background is a coin flip either way.
func perceivedBrightness(r, g, b float64) float64 {
	return 0.299*r + 0.587*g + 0.114*b
}

// fromColorFGBG interprets rxvt's $COLORFGBG ("fg;bg", sometimes
// "fg;default;bg"): the last field is the background's palette index -
// 0-6 and 8 are dark, 7 and 9-15 light (vim's reading of it). Static, set
// when the terminal started, so only a fallback.
func fromColorFGBG(v string) Result {
	if v == "" {
		return Result{}
	}
	parts := strings.Split(v, ";")
	n, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || n < 0 || n > 15 {
		return Result{}
	}
	return Result{Light: n == 7 || n >= 9, OK: true}
}
