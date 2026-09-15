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

// Implements the 'y' shortcut design-docs/CopyToClipboard.md asks for -
// copy the currently active tab's own content (the same plain,
// tag-stripped text TextSearch already searches) to the real, local
// clipboard via an OSC 52 terminal escape sequence, so it works over SSH
// and inside tmux, not just when the terminal itself is local.
package uikit

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

// osc52MaxEncoded is a conservative shared ceiling on the base64 payload
// OSC 52 carries, picked from xterm's own long-standing compiled-in
// default (its terminal.c maxSetTitleLen, which also gates OSC 52) rather
// than from anything Tangsible itself imposes - the tightest limit in
// common real-world use, and this project's own ~10-host target scale
// (Purpose.md) means a single tab's content realistically never comes
// close to it anyway. Enforced up front, with a plain error, rather than
// silently sending a truncated/oversize sequence some terminals would
// otherwise just drop.
const osc52MaxEncoded = 100000

// CopyToClipboard writes text to the system clipboard via
// `ESC ] 52 ; c ; <base64> ESC \` (OSC 52, "c" = the clipboard selection
// specifically, not primary/cut-buffer), written directly to os.Stdout -
// bypassing tcell's own Screen.SetClipboard (see this function's own
// design-docs/CopyToClipboard.md for why): that path only fires when the
// active terminfo entry is marked XTermLike, which tmux's and screen's own
// entries deliberately aren't (confirmed against tcell's terminfo
// database), making it a silent no-op for exactly this feature's
// motivating case. Writing straight to os.Stdout is safe against tview's
// own screen redraws racing it: every caller here reaches this from inside
// an app.SetInputCapture handler, which - like every Draw() tview itself
// triggers, whether direct or via QueueUpdateDraw - runs on the
// application's single event-loop goroutine (the same assumption
// aggregate.go's own playbookState.Apply relies on), so there's no other
// goroutine that could be mid-write to the same fd concurrently.
//
// When $TMUX is set, the sequence is wrapped in tmux's own DCS passthrough
// envelope first - the standard workaround real-world OSC 52 tools use
// (e.g. vim-oscyank, neovim's built-in osc52 clipboard provider) since
// tmux's own native OSC 52 interception/forwarding (controlled by its
// `set-clipboard` option) has historically been inconsistent across
// versions/configs, while passthrough reaches the outer terminal directly
// - at the cost of needing `set -g allow-passthrough on` in tmux >= 3.3,
// which is the one piece of setup this doesn't automate away.
func CopyToClipboard(text string) error {
	seq, err := osc52Sequence(text)
	if err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		seq = wrapTmuxPassthrough(seq)
	}
	_, err = io.WriteString(os.Stdout, seq)
	return err
}

// osc52Sequence builds the plain (not tmux-wrapped) OSC 52 escape sequence
// for text, split out from CopyToClipboard as its own pure function
// specifically so the encoding/validation logic is unit-testable without
// actually writing to a terminal.
func osc52Sequence(text string) (string, error) {
	if text == "" {
		return "", fmt.Errorf("nothing to copy")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if len(encoded) > osc52MaxEncoded {
		return "", fmt.Errorf("too large to copy (%d bytes)", len(text))
	}
	return "\x1b]52;c;" + encoded + "\x1b\\", nil
}

// wrapTmuxPassthrough wraps seq in tmux's own DCS passthrough envelope
// (`ESC P tmux ; <seq, every ESC doubled> ESC \`) - doubling every ESC
// inside seq is required so tmux's own DCS parser doesn't mistake it for
// the end of the passthrough block partway through.
func wrapTmuxPassthrough(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// CopyActiveTabToClipboard copies tabs' currently active tab to the
// clipboard (see CopyToClipboard) and reports what happened - the active
// tab's own name and the number of bytes copied on success, or an error
// (including "nothing to copy" for an empty tab or no tabs at all).
func CopyActiveTabToClipboard(tabs *TabbedPane) (name string, n int, err error) {
	tv, ok := tabs.ActiveTextView()
	if !ok {
		return "", 0, fmt.Errorf("nothing to copy")
	}
	text := tv.GetText(true)
	if err := CopyToClipboard(text); err != nil {
		return tabs.ActiveName(), 0, err
	}
	return tabs.ActiveName(), len(text), nil
}

// CopyActiveTabStatus copies tabs' active tab to the clipboard
// (CopyActiveTabToClipboard) and formats a ready-to-display status line
// describing what happened, success or failure alike - so every 'y'-key
// call site can hand it straight to whichever footer/hint bar it already
// uses for transient status (TabSearchBar.ShowMessage, or the equivalent
// inline SetText tui.go/diff.go use for their own search status), the same
// convention TextSearch's own "no matches"/"match N of M" status follows.
func CopyActiveTabStatus(tabs *TabbedPane) string {
	name, n, err := CopyActiveTabToClipboard(tabs)
	if err != nil {
		return fmt.Sprintf(" copy failed: %s ", err)
	}
	return fmt.Sprintf(" copied %q tab to clipboard (%d bytes) ", name, n)
}
