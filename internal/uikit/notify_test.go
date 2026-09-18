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
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/config"
)

func TestNotificationSequenceOSC9HasNoTitle(t *testing.T) {
	seq, ok := notificationSequence(config.NotificationOSC9, "Tangsible", "playbook finished")
	if !ok {
		t.Fatal("expected ok=true for NotificationOSC9")
	}
	if !strings.HasPrefix(seq, "\x1b]9;") || !strings.HasSuffix(seq, "\a") {
		t.Fatalf("sequence = %q, want an OSC 9 sequence terminated with BEL", seq)
	}
	if strings.Contains(seq, "Tangsible") {
		t.Errorf("sequence = %q, OSC 9 has no title field and should not contain it", seq)
	}
	if !strings.Contains(seq, "playbook finished") {
		t.Errorf("sequence = %q, want it to contain the body", seq)
	}
}

func TestNotificationSequenceOSC777HasTitleAndBody(t *testing.T) {
	seq, ok := notificationSequence(config.NotificationOSC777, "Tangsible", "playbook finished")
	if !ok {
		t.Fatal("expected ok=true for NotificationOSC777")
	}
	const want = "\x1b]777;notify;Tangsible;playbook finished\x1b\\"
	if seq != want {
		t.Errorf("sequence = %q, want %q", seq, want)
	}
}

func TestNotificationSequenceOSC99HasTwoChunks(t *testing.T) {
	seq, ok := notificationSequence(config.NotificationOSC99, "Tangsible", "playbook finished")
	if !ok {
		t.Fatal("expected ok=true for NotificationOSC99")
	}
	if !strings.Contains(seq, "p=title;Tangsible\x1b\\") {
		t.Errorf("sequence = %q, want a title chunk", seq)
	}
	if !strings.Contains(seq, "p=body;playbook finished\x1b\\") {
		t.Errorf("sequence = %q, want a body chunk", seq)
	}
}

func TestNotificationSequenceBell(t *testing.T) {
	seq, ok := notificationSequence(config.NotificationBell, "Tangsible", "playbook finished")
	if !ok {
		t.Fatal("expected ok=true for NotificationBell")
	}
	if seq != "\a" {
		t.Errorf("sequence = %q, want a plain BEL", seq)
	}
}

func TestNotificationSequenceOffIsNoop(t *testing.T) {
	if _, ok := notificationSequence(config.NotificationOff, "Tangsible", "body"); ok {
		t.Error("expected ok=false for NotificationOff")
	}
}

func TestNotificationSequenceSanitizesControlBytes(t *testing.T) {
	seq, ok := notificationSequence(config.NotificationOSC777, "Tangsible\x1b]52", "body\x07here")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if strings.Contains(seq[len("\x1b]777;notify;"):], "\x1b") && !strings.HasSuffix(seq, "\x1b\\") {
		t.Errorf("sequence = %q, an embedded ESC in title should have been sanitized away", seq)
	}
	if strings.Count(seq, "\x1b") != 2 {
		t.Errorf("sequence = %q, want exactly the sequence's own leading ESC and ST-terminator ESC, embedded ones sanitized", seq)
	}
	if strings.Contains(seq, "\a") {
		t.Errorf("sequence = %q, an embedded BEL in body should have been sanitized away", seq)
	}
}

func TestSendNotificationWritesPlainOutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	got := withCapturedStdout(t, func() {
		if err := SendNotification(config.NotificationOSC9, "Tangsible", "hi"); err != nil {
			t.Fatalf("SendNotification: %v", err)
		}
	})
	if !strings.HasPrefix(got, "\x1b]9;") {
		t.Errorf("stdout = %q, want a plain OSC 9 sequence (no tmux wrap)", got)
	}
}

func TestSendNotificationWrapsForTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	got := withCapturedStdout(t, func() {
		if err := SendNotification(config.NotificationOSC777, "Tangsible", "hi"); err != nil {
			t.Fatalf("SendNotification: %v", err)
		}
	})
	if !strings.HasPrefix(got, "\x1bPtmux;") {
		t.Errorf("stdout = %q, want it wrapped in tmux's DCS passthrough envelope", got)
	}
}

func TestSendNotificationBellNeverWrapsForTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1234,0")
	got := withCapturedStdout(t, func() {
		if err := SendNotification(config.NotificationBell, "Tangsible", "hi"); err != nil {
			t.Fatalf("SendNotification: %v", err)
		}
	})
	if got != "\a" {
		t.Errorf("stdout = %q, want a plain unwrapped BEL even under tmux", got)
	}
}

func TestSendNotificationOffWritesNothing(t *testing.T) {
	got := withCapturedStdout(t, func() {
		if err := SendNotification(config.NotificationOff, "Tangsible", "hi"); err != nil {
			t.Fatalf("SendNotification: %v", err)
		}
	})
	if got != "" {
		t.Errorf("stdout = %q, want nothing written for NotificationOff", got)
	}
}

func TestPlaybookFinishedBody(t *testing.T) {
	cases := []struct {
		name                           string
		genuineFailure, hadUnreachable bool
		want                           string
	}{
		{"success", false, false, "site.yml finished successfully"},
		{"benign unreachable", false, true, "site.yml finished (unreachable hosts)"},
		{"genuine failure", true, false, "site.yml finished with failures"},
		{"genuine failure with unreachable", true, true, "site.yml finished with failures"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PlaybookFinishedBody("site.yml", tc.genuineFailure, tc.hadUnreachable)
			if got != tc.want {
				t.Errorf("PlaybookFinishedBody(%v, %v) = %q, want %q", tc.genuineFailure, tc.hadUnreachable, got, tc.want)
			}
		})
	}
}

func TestTaskFailedBody(t *testing.T) {
	got := TaskFailedBody("Install package", "web1")
	const want = "Install package failed on web1"
	if got != want {
		t.Errorf("TaskFailedBody = %q, want %q", got, want)
	}
}

func TestSuppressedTaskFailuresBody(t *testing.T) {
	got := SuppressedTaskFailuresBody(3)
	const want = "3 further task failures suppressed for this run"
	if got != want {
		t.Errorf("SuppressedTaskFailuresBody(3) = %q, want %q", got, want)
	}
}
