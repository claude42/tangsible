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

// Implements mirroring design-docs/ProgressIndicator.md's temporarily-
// reinstated percentage into the terminal's own window title, not just
// the top bar - so progress is visible from another window/tab too.
// PushWindowTitle/PopWindowTitle use xterm's window-title stack (CSI
// 22/23;2t, XTWINOPS) specifically so the caller never has to query or
// remember whatever title was there before: push once before touching
// it, overwrite freely via SetWindowTitle, pop once done to restore it
// exactly. A terminal that doesn't understand these sequences (or has
// window-title manipulation disabled, as some hardened configs do)
// simply ignores them - safe by construction, not by feature-detection.
package uikit

import (
	"io"
	"os"

	"github.com/rivo/tview"
)

// PushWindowTitle asks the terminal to save its current window title onto
// its own title stack (CSI 22;2t - "2" means title only, not the combined
// icon+title form OSC 0/CSI ...;0t would use).
func PushWindowTitle() error {
	return writeTermSeq("\x1b[22;2t")
}

// PopWindowTitle restores whatever title PushWindowTitle most recently
// saved (CSI 23;2t) - the counterpart that makes SetWindowTitle safe to
// call repeatedly with no separate bookkeeping.
func PopWindowTitle() error {
	return writeTermSeq("\x1b[23;2t")
}

// SetWindowTitle sets the terminal's window title via OSC 2 (title only,
// ST-terminated) - text is run through sanitizeNotificationText (notify.go)
// first, the same defense-in-depth against an embedded ESC/BEL corrupting
// or prematurely ending the sequence.
func SetWindowTitle(title string) error {
	return writeTermSeq("\x1b]2;" + sanitizeNotificationText(title) + "\x1b\\")
}

// RunWithTitleStack wraps app.Run() with PushWindowTitle before and
// PopWindowTitle after (deferred, so it still runs if app.Run() returns
// an error) - the caller (main.go/revisit.go, both live-TUI call sites
// that might overwrite the title via SetWindowTitle from inside app's own
// rebuild) never has to know or restore whatever title was there before.
func RunWithTitleStack(app *tview.Application) error {
	_ = PushWindowTitle()
	defer func() { _ = PopWindowTitle() }()
	return app.Run()
}

// writeTermSeq writes seq directly to os.Stdout, wrapped in tmux's own DCS
// passthrough envelope first when running inside tmux - same mechanism/
// reasoning as clipboard.go's wrapTmuxPassthrough (tmux intercepts window-
// title sequences for its own pane-title bookkeeping; whether it also
// forwards them to the outer terminal depends on tmux options this can't
// see, while passthrough reaches the outer terminal directly). Safe to
// call only from tview's own event-loop goroutine - same single-writer
// assumption CopyToClipboard's own doc comment explains.
func writeTermSeq(seq string) error {
	if os.Getenv("TMUX") != "" {
		seq = wrapTmuxPassthrough(seq)
	}
	_, err := io.WriteString(os.Stdout, seq)
	return err
}
