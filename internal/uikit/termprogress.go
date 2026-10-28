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

// Implements design-docs/ProgressIndicator.md's OSC 9;4 terminal progress
// (ConEmu/Windows Terminal's "taskbar progress" sequence, since adopted by
// iTerm2, WezTerm, Ghostty and Ptyxis): ESC ] 9 ; 4 ; state ; percent ST.
// Detection follows Rust's anstyle-progress crate (what Cargo uses) -
// there's no way to query support, and a terminal that only knows OSC 9 as
// iTerm2's notification form would pop up "4;1;42" as a message, so the
// default "auto" mode only enables it for terminals known to understand it.
package uikit

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// TerminalProgressState is OSC 9;4's own state parameter.
type TerminalProgressState int

const (
	TerminalProgressHidden        TerminalProgressState = 0
	TerminalProgressNormal        TerminalProgressState = 1
	TerminalProgressError         TerminalProgressState = 2
	TerminalProgressIndeterminate TerminalProgressState = 3
	TerminalProgressPaused        TerminalProgressState = 4
)

// TerminalProgress is one OSC 9;4 value: Percent is only meaningful for
// Normal/Error/Paused.
type TerminalProgress struct {
	State   TerminalProgressState
	Percent int
}

// Sequence renders p as the escape sequence (ST-terminated, like
// SetWindowTitle). Percent is clamped to 0-100.
func (p TerminalProgress) Sequence() string {
	pct := min(max(p.Percent, 0), 100)
	if p.State == TerminalProgressHidden || p.State == TerminalProgressIndeterminate {
		pct = 0
	}
	return fmt.Sprintf("\x1b]9;4;%d;%d\x1b\\", int(p.State), pct)
}

// TerminalProgressSupported reports whether the terminal described by
// getenv (os.Getenv in production) is known to support OSC 9;4. Inside
// tmux TERM_PROGRAM is "tmux" and the outer terminal's variables usually
// aren't visible, so auto stays off there - "on" is the override.
func TerminalProgressSupported(getenv func(string) string) bool {
	if hasTermFeature(getenv("TERM_FEATURES"), "P") {
		return true
	}
	switch getenv("TERM_PROGRAM") {
	case "WezTerm", "ghostty":
		return true
	}
	// iTerm2's shell integration forwards these over ssh (via SendEnv/
	// AcceptEnv LC_*), unlike TERM_FEATURES/TERM_PROGRAM. The minimum
	// version is a conservative guess (a terminal that only knows OSC 9 as
	// a notification would show a bogus one) - "on" overrides it.
	if getenv("LC_TERMINAL") == "iTerm2" && versionAtLeast(getenv("LC_TERMINAL_VERSION"), 3, 6, 6) {
		return true
	}
	if getenv("WT_SESSION") != "" || getenv("ConEmuANSI") == "ON" {
		return true
	}
	if v := getenv("PTYXIS_VERSION"); v != "" {
		major, _, _ := strings.Cut(v, ".")
		if n, err := strconv.Atoi(major); err == nil && n >= 48 {
			return true
		}
	}
	return false
}

// versionAtLeast reports whether a dotted version like "3.7.3" or
// "3.6.6beta2" (non-numeric suffixes ignored) is >= major.minor.patch.
func versionAtLeast(v string, want ...int) bool {
	parts := strings.Split(v, ".")
	for i, w := range want {
		n := 0
		if i < len(parts) {
			digits := parts[i]
			for j := 0; j < len(digits); j++ {
				if digits[j] < '0' || digits[j] > '9' {
					digits = digits[:j]
					break
				}
			}
			if digits == "" {
				return false
			}
			n, _ = strconv.Atoi(digits)
		}
		if n != w {
			return n > w
		}
	}
	return true
}

// hasTermFeature reports whether iTerm2's TERM_FEATURES (concatenated
// feature codes: an uppercase letter followed by optional lowercase
// letters/digits, e.g. "MB2Sx") lists exactly code.
func hasTermFeature(features, code string) bool {
	for i := 0; i < len(features); {
		j := i + 1
		for j < len(features) && (features[j] < 'A' || features[j] > 'Z') {
			j++
		}
		if features[i:j] == code {
			return true
		}
		i = j
	}
	return false
}

// ComputeTerminalProgress maps the live session's state to what the
// terminal should show. started is false until something has run (the
// re-run dialog is open, or debug hasn't hit F5): nothing to show. Same
// while probePending (the --list-tasks probe hasn't returned yet, so
// there's no total): showing indeterminate for that first moment and then
// switching to a percentage reads as a glitch.
// frozen is a finished run: a genuine failure keeps an error bar (cleared
// by the next rerun or on exit - a signal that's still useful from another
// window), anything else clears it. Running: indeterminate without a
// skeleton, and also once the prediction claims completion while the
// process still runs (the prediction misses handlers and looped includes,
// ProgressIndicator.md) - a full bar that keeps going would look broken.
func ComputeTerminalProgress(started, probePending, frozen, failed, paused bool, pos, total int) TerminalProgress {
	if !started {
		return TerminalProgress{State: TerminalProgressHidden}
	}
	if frozen {
		if failed {
			return TerminalProgress{State: TerminalProgressError, Percent: 100}
		}
		return TerminalProgress{State: TerminalProgressHidden}
	}
	if probePending {
		return TerminalProgress{State: TerminalProgressHidden}
	}
	if total <= 0 || pos >= total {
		return TerminalProgress{State: TerminalProgressIndeterminate}
	}
	state := TerminalProgressNormal
	if paused {
		state = TerminalProgressPaused
	}
	return TerminalProgress{State: state, Percent: pos * 100 / total}
}

// terminalProgressActive is true while a non-hidden state is on screen, so
// ClearTerminalProgress only ever writes after this process wrote one -
// never a stray OSC 9 at a terminal that didn't opt in.
var terminalProgressActive atomic.Bool

// SetTerminalProgress writes p to the terminal. Same event-loop-only
// contract as writeTermSeq.
func SetTerminalProgress(p TerminalProgress) error {
	terminalProgressActive.Store(p.State != TerminalProgressHidden)
	return writeTermSeq(p.Sequence())
}

// ClearTerminalProgress hides the bar if this process showed one.
func ClearTerminalProgress() error {
	if !terminalProgressActive.Swap(false) {
		return nil
	}
	return writeTermSeq(TerminalProgress{State: TerminalProgressHidden}.Sequence())
}
