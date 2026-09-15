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

package uikit

import (
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestOSC52SequenceRoundTrips(t *testing.T) {
	seq, err := osc52Sequence("hello")
	if err != nil {
		t.Fatalf("osc52Sequence returned an error: %v", err)
	}
	const want = "\x1b]52;c;"
	if !strings.HasPrefix(seq, want) {
		t.Fatalf("sequence = %q, want it to start with %q", seq, want)
	}
	if !strings.HasSuffix(seq, "\x1b\\") {
		t.Fatalf("sequence = %q, want it to end with the ST terminator", seq)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(seq, want), "\x1b\\")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload isn't valid base64: %v", err)
	}
	if string(decoded) != "hello" {
		t.Errorf("decoded payload = %q, want %q", decoded, "hello")
	}
}

func TestOSC52SequenceEmptyTextErrors(t *testing.T) {
	if _, err := osc52Sequence(""); err == nil {
		t.Error("osc52Sequence(\"\") should return an error, got nil")
	}
}

func TestOSC52SequenceOversizeErrors(t *testing.T) {
	// Each byte becomes ~4/3 bytes of base64, so this comfortably clears
	// osc52MaxEncoded once encoded.
	huge := strings.Repeat("x", osc52MaxEncoded)
	if _, err := osc52Sequence(huge); err == nil {
		t.Error("osc52Sequence on oversize text should return an error, got nil")
	}
}

func TestWrapTmuxPassthroughDoublesEscapes(t *testing.T) {
	wrapped := wrapTmuxPassthrough("\x1b]52;c;AAA\x1b\\")
	const wantPrefix = "\x1bPtmux;"
	const wantSuffix = "\x1b\\"
	if !strings.HasPrefix(wrapped, wantPrefix) || !strings.HasSuffix(wrapped, wantSuffix) {
		t.Fatalf("wrapped = %q, want prefix %q and suffix %q", wrapped, wantPrefix, wantSuffix)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(wrapped, wantPrefix), wantSuffix)
	if strings.Count(inner, "\x1b") != 4 {
		// Original had 2 ESCs (one leading the OSC, one leading ST) - each
		// should be doubled.
		t.Errorf("wrapped inner content = %q, want every ESC doubled (4 total)", inner)
	}
}

// withCapturedStdout redirects os.Stdout for the duration of fn, returning
// whatever was written - the only way to observe CopyToClipboard's actual
// output, since it deliberately writes straight to os.Stdout rather than
// through an injectable writer (see its own doc comment for why).
func withCapturedStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(out)
}

func TestCopyToClipboardWritesPlainOSC52OutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	got := withCapturedStdout(t, func() {
		if err := CopyToClipboard("hi"); err != nil {
			t.Fatalf("CopyToClipboard: %v", err)
		}
	})
	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Errorf("stdout = %q, want a plain OSC 52 sequence (no tmux wrap)", got)
	}
	if strings.Contains(got, "Ptmux;") {
		t.Errorf("stdout = %q, should not be tmux-wrapped when $TMUX is unset", got)
	}
}

func TestCopyToClipboardWrapsForTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	got := withCapturedStdout(t, func() {
		if err := CopyToClipboard("hi"); err != nil {
			t.Fatalf("CopyToClipboard: %v", err)
		}
	})
	if !strings.HasPrefix(got, "\x1bPtmux;") {
		t.Errorf("stdout = %q, want it wrapped in tmux's DCS passthrough envelope since $TMUX is set", got)
	}
}

func TestCopyActiveTabToClipboardAndStatus(t *testing.T) {
	t.Setenv("TMUX", "")
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetText(tview.Escape("some tab content"))
	tabs := NewTabbedPane()
	tabs.SetTabs([]string{"Output"}, []tview.Primitive{tv})

	var name string
	var n int
	var err error
	withCapturedStdout(t, func() {
		name, n, err = CopyActiveTabToClipboard(tabs)
	})
	if err != nil {
		t.Fatalf("CopyActiveTabToClipboard: %v", err)
	}
	if name != "Output" {
		t.Errorf("name = %q, want %q", name, "Output")
	}
	if n != len("some tab content") {
		t.Errorf("n = %d, want %d", n, len("some tab content"))
	}

	var status string
	withCapturedStdout(t, func() {
		status = CopyActiveTabStatus(tabs)
	})
	if !strings.Contains(status, "Output") || !strings.Contains(status, "copied") {
		t.Errorf("status = %q, want it to mention the tab name and \"copied\"", status)
	}
}

func TestCopyActiveTabToClipboardNoTabs(t *testing.T) {
	tabs := NewTabbedPane()
	if _, _, err := CopyActiveTabToClipboard(tabs); err == nil {
		t.Error("CopyActiveTabToClipboard with no tabs should return an error, got nil")
	}
}
