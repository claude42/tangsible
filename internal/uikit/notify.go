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

// Implements design-docs/Notifications.md's terminal notifications -
// playbook-finished/task-failed events rendered as an OSC 9, OSC 777, or
// kitty OSC 99 desktop notification, or a plain BEL. Built from each
// protocol's own published documentation (iTerm2's OSC 9, rxvt-unicode/
// Konsole's OSC 777 "notify" subcommand, kitty's OSC 99 desktop
// notifications protocol) rather than against a real terminal - unlike
// OSC 52 (CopyToClipboard.md), which this project could verify live against
// its own tmux/terminal setup, these three are documented but unconfirmed
// here; re-verify against real terminals (kitty, Konsole, iTerm2/WezTerm/
// Windows Terminal) before relying on the exact byte format.
package uikit

import (
	"fmt"
	"io"
	"os"
	"strings"

	"code.aw.net/claude/tangsible/internal/config"
)

// sanitizeNotificationText strips ASCII control characters (including ESC
// and BEL) from s, replacing each with a space - title/body text is always
// a playbook/task/host name here, never expected to contain one, but a
// stray control byte would otherwise either break the escape sequence's own
// framing (an embedded ESC) or prematurely end it (an embedded BEL/ST-like
// byte), so this is defense-in-depth, not a real-world case.
func sanitizeNotificationText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// notificationSequence builds the terminal escape sequence for kind - the
// plain, not tmux-wrapped, form. ok is false for NotificationOff (or any
// other unrecognized kind), meaning there's nothing to send.
func notificationSequence(kind config.NotificationKind, title, body string) (seq string, ok bool) {
	title = sanitizeNotificationText(title)
	body = sanitizeNotificationText(body)
	switch kind {
	case config.NotificationOSC9:
		// iTerm2's own "Post Notification" escape code - body only, no
		// title, terminated with BEL (not ST - this is the one OSC variant
		// documented that way, unlike 777/99 below).
		return "\x1b]9;" + body + "\a", true
	case config.NotificationOSC777:
		// rxvt-unicode/Konsole's "notify" OSC 777 subcommand:
		// `ESC ] 777 ; notify ; <title> ; <body> ST`.
		return "\x1b]777;notify;" + title + ";" + body + "\x1b\\", true
	case config.NotificationOSC99:
		// kitty's desktop notifications protocol - two chunks sharing one
		// notification id ("i=1"): a title chunk (d=0, "not done yet") and
		// a body chunk (d=1, the default, so left implicit) - both must
		// arrive for kitty to show a title alongside the body rather than
		// just a bare message.
		return "\x1b]99;i=1:d=0:p=title;" + title + "\x1b\\" +
			"\x1b]99;i=1:p=body;" + body + "\x1b\\", true
	case config.NotificationBell:
		return "\a", true
	default:
		return "", false
	}
}

// SendNotification emits one design-docs/Notifications.md notification of
// the given kind, title, and body, written directly to os.Stdout - same
// rationale as CopyToClipboard (clipboard.go): bypasses tcell entirely
// (there's no tcell.Screen equivalent for these protocols at all, unlike
// OSC 52's SetClipboard), and is safe against a concurrent screen redraw
// only because every caller reaches this from tview's own single event-loop
// goroutine (an app.QueueUpdateDraw callback, or something already running
// inside one) - never from a background goroutine racing tcell's own writes
// to the same fd. A no-op, returning nil, for NotificationOff.
//
// Under tmux, the OSC variants (not bell - a plain BEL needs no framing and
// already passes through tmux natively) are wrapped in tmux's own DCS
// passthrough envelope first, exactly like CopyToClipboard's own OSC 52 -
// see wrapTmuxPassthrough's doc comment for why plain OSC forwarding can't
// be relied on instead.
func SendNotification(kind config.NotificationKind, title, body string) error {
	seq, ok := notificationSequence(kind, title, body)
	if !ok {
		return nil
	}
	if kind != config.NotificationBell && os.Getenv("TMUX") != "" {
		seq = wrapTmuxPassthrough(seq)
	}
	_, err := io.WriteString(os.Stdout, seq)
	return err
}

// PlaybookFinishedBody formats notify_playbook_finished's body text
// (design-docs/Notifications.md's "Notification content" section):
// playbook name plus outcome, one of three buckets - genuine failure,
// benign-unreachable, or success. Exported for session's own use (no
// notification-specific state lives in this package) and independently
// testable from the escape-sequence plumbing above.
func PlaybookFinishedBody(playbookName string, genuineFailure, hadUnreachable bool) string {
	switch {
	case genuineFailure:
		return fmt.Sprintf("%s finished with failures", playbookName)
	case hadUnreachable:
		return fmt.Sprintf("%s finished (unreachable hosts)", playbookName)
	default:
		return fmt.Sprintf("%s finished successfully", playbookName)
	}
}

// TaskFailedBody formats notify_task_failed's body text: the failed task
// and host.
func TaskFailedBody(taskName, host string) string {
	return fmt.Sprintf("%s failed on %s", taskName, host)
}

// SuppressedTaskFailuresBody formats the one-time notice sent once a
// generation finishes with more task failures than notify_task_failed_max
// allowed individual notifications for (design-docs/Notifications.md).
func SuppressedTaskFailuresBody(suppressed int) string {
	return fmt.Sprintf("%d further task failures suppressed for this run", suppressed)
}

// NotificationTitle is every notification's fixed title (design-docs/
// Notifications.md's "Notification content" section) - OSC 9 has no
// separate title field at all (see notificationSequence above), so this is
// only ever used for OSC 777/99.
const NotificationTitle = "Tangsible"
