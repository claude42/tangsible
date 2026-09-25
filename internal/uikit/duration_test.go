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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
)

func newTaskNode() *playbook.TaskNode {
	return &playbook.TaskNode{
		Hosts:    map[string]playbook.Outcome{},
		Raw:      map[string]json.RawMessage{},
		Started:  map[string]time.Time{},
		Finished: map[string]time.Time{},
		Ignored:  map[string]bool{},
	}
}

func TestHostDuration(t *testing.T) {
	base := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

	t.Run("both timestamps known", func(t *testing.T) {
		task := newTaskNode()
		task.Started["web1"] = base
		task.Finished["web1"] = base.Add(340 * time.Millisecond)

		got, ok := HostDuration(task, "web1")
		if !ok {
			t.Fatal("HostDuration ok = false, want true")
		}
		if got != 340*time.Millisecond {
			t.Errorf("HostDuration = %v, want 340ms", got)
		}
	})

	t.Run("no Started entry (pre-fork run log)", func(t *testing.T) {
		task := newTaskNode()
		task.Finished["web1"] = base

		if _, ok := HostDuration(task, "web1"); ok {
			t.Error("HostDuration ok = true, want false (Started missing)")
		}
	})

	t.Run("no Finished entry (task still in flight)", func(t *testing.T) {
		task := newTaskNode()
		task.Started["web1"] = base

		if _, ok := HostDuration(task, "web1"); ok {
			t.Error("HostDuration ok = true, want false (Finished missing)")
		}
	})

	t.Run("neither timestamp known", func(t *testing.T) {
		task := newTaskNode()
		if _, ok := HostDuration(task, "web1"); ok {
			t.Error("HostDuration ok = true, want false (nothing recorded)")
		}
	})
}

// TestFormatDuration locks in the always-seconds format (no more ms/
// minutes+seconds thresholds - reverted after live use showed switching
// units defeated the actual point: comparing hosts at a glance, since two
// hosts doing the same task could land in different unit systems).
func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0.0s"},
		{340 * time.Millisecond, "0.3s"},
		{999 * time.Millisecond, "1.0s"},
		{time.Second, "1.0s"},
		{1300 * time.Millisecond, "1.3s"},
		{59900 * time.Millisecond, "59.9s"},
		{time.Minute, "60.0s"},
		{65 * time.Second, "65.0s"},
		{2*time.Minute + 3*time.Second, "123.0s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.d); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestOutcomeDetail_MergesDurationWhenKnown covers design-docs/
// OwnCallbackPlugin.md's step 5: every outcome kind gets HostDuration's
// figure merged into its existing parenthetical (or, for Unreachable,
// becomes its only content) when Started/Finished are both known.
func TestOutcomeDetail_MergesDurationWhenKnown(t *testing.T) {
	base := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

	t.Run("OK merges duration alongside the output summary", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeOK
		task.Raw["web1"] = json.RawMessage(`{"stdout":"hi"}`)
		task.Started["web1"] = base
		task.Finished["web1"] = base.Add(1200 * time.Millisecond)

		if got, want := OutcomeDetail(task, "web1"), " (hi; 1.2s)"; got != want {
			t.Errorf("OutcomeDetail = %q, want %q", got, want)
		}
	})

	t.Run("Skipped merges duration alongside the skip reason", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeSkipped
		task.Raw["web1"] = json.RawMessage(`{"skip_reason":"Conditional result was False"}`)
		task.Started["web1"] = base
		task.Finished["web1"] = base.Add(5 * time.Millisecond)

		if got, want := OutcomeDetail(task, "web1"), " (Conditional result was False; 0.0s)"; got != want {
			t.Errorf("OutcomeDetail = %q, want %q", got, want)
		}
	})

	t.Run("Unreachable gets a detail for the first time", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeUnreachable
		task.Raw["web1"] = json.RawMessage(`{}`)
		task.Started["web1"] = base
		task.Finished["web1"] = base.Add(30 * time.Second)

		if got, want := OutcomeDetail(task, "web1"), " (30.0s)"; got != want {
			t.Errorf("OutcomeDetail = %q, want %q", got, want)
		}
	})

	t.Run("Unreachable stays empty when duration is unknown", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeUnreachable
		task.Raw["web1"] = json.RawMessage(`{}`)

		if got := OutcomeDetail(task, "web1"); got != "" {
			t.Errorf("OutcomeDetail = %q, want \"\" (rendering exactly as before this existed)", got)
		}
	})

	t.Run("OK with no duration data renders exactly as before (regression)", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeOK
		task.Raw["web1"] = json.RawMessage(`{"stdout":"hi"}`)

		if got, want := OutcomeDetail(task, "web1"), " (hi)"; got != want {
			t.Errorf("OutcomeDetail = %q, want %q (pre-fork run log, no Started/Finished)", got, want)
		}
	})
}

// TestOutcomeDetailText covers HostLabel's own no-duration detail text -
// never includes duration, unlike OutcomeDetail, since HostLabel shows it
// via HostAndDurationPrefix instead.
func TestOutcomeDetailText(t *testing.T) {
	base := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

	task := newTaskNode()
	task.Hosts["web1"] = playbook.OutcomeOK
	task.Raw["web1"] = json.RawMessage(`{"stdout":"hi"}`)
	task.Started["web1"] = base
	task.Finished["web1"] = base.Add(1200 * time.Millisecond)

	if got, want := OutcomeDetailText(task, "web1"), " (hi)"; got != want {
		t.Errorf("OutcomeDetailText = %q, want %q (no duration, even though HostDuration is known)", got, want)
	}

	task.Hosts["web2"] = playbook.OutcomeUnreachable
	if got := OutcomeDetailText(task, "web2"); got != "" {
		t.Errorf("OutcomeDetailText(Unreachable) = %q, want \"\" (only OutcomeDetail gives Unreachable a detail, via duration)", got)
	}
}

// TestOutcomeDetailText_MergesIgnored covers design-docs/
// OwnCallbackPlugin.md: a Failed result whose task.Ignored[host] is true
// gets "ignored" merged into the live tree's own detail text, so an
// ignore_errors: true failure reads differently from a genuine one
// without waiting for the run to finish - an ordinary failure, and any
// other outcome kind, are unaffected.
func TestOutcomeDetailText_MergesIgnored(t *testing.T) {
	task := newTaskNode()
	task.Hosts["web1"] = playbook.OutcomeFailed
	task.Raw["web1"] = json.RawMessage(`{"msg":"boom"}`)
	task.Ignored["web1"] = true

	task.Hosts["web2"] = playbook.OutcomeFailed
	task.Raw["web2"] = json.RawMessage(`{"msg":"boom"}`)
	// web2 not marked Ignored.

	if got, want := OutcomeDetailText(task, "web1"), " (boom; ignored)"; got != want {
		t.Errorf("ignored failure: OutcomeDetailText = %q, want %q", got, want)
	}
	if got, want := OutcomeDetailText(task, "web2"), " (boom)"; got != want {
		t.Errorf("ordinary failure: OutcomeDetailText = %q, want %q (no \"ignored\" merged in)", got, want)
	}
}

func TestComputeDurationLayout(t *testing.T) {
	base := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

	t.Run("no duration anywhere", func(t *testing.T) {
		state := &playbook.PlaybookState{
			Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{newTaskNode()}}},
		}
		got := ComputeDurationLayout(state, []string{"somehost", "foo"})
		if got.ShowDuration {
			t.Errorf("ComputeDurationLayout = %+v, want ShowDuration=false", got)
		}
	})

	t.Run("widest host and widest duration set the widths", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["somehost"] = playbook.OutcomeOK
		task.Hosts["otherhost"] = playbook.OutcomeOK
		task.Hosts["foo"] = playbook.OutcomeOK
		task.Started["somehost"] = base
		task.Finished["somehost"] = base.Add(5300 * time.Millisecond)
		task.Started["otherhost"] = base
		task.Finished["otherhost"] = base.Add(10200 * time.Millisecond)
		task.Started["foo"] = base
		task.Finished["foo"] = base.Add(900 * time.Millisecond)

		state := &playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{task}}}}
		got := ComputeDurationLayout(state, []string{"somehost", "otherhost", "foo"})
		want := DurationLayout{ShowDuration: true, HostWidth: len("otherhost") + 1, SecondsWidth: len("10") + 2}
		if got != want {
			t.Errorf("ComputeDurationLayout = %+v, want %+v", got, want)
		}
	})
}

// TestHostAndDurationPrefix_MatchesRequestedExample reproduces the exact
// alignment requested (three hosts of different name lengths, durations
// with one and two integer digits) and checks the output character for
// character - the whole point of DurationLayout is this alignment, so
// "close" isn't good enough here.
func TestHostAndDurationPrefix_MatchesRequestedExample(t *testing.T) {
	base := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

	task := newTaskNode()
	for host, ms := range map[string]int{"somehost": 5300, "otherhost": 10200, "foo": 900} {
		task.Hosts[host] = playbook.OutcomeOK
		task.Started[host] = base
		task.Finished[host] = base.Add(time.Duration(ms) * time.Millisecond)
	}

	layout := ComputeDurationLayout(
		&playbook.PlaybookState{Plays: []*playbook.PlayNode{{Tasks: []*playbook.TaskNode{task}}}},
		[]string{"somehost", "otherhost", "foo"},
	)

	cases := []struct {
		host string
		want string
	}{
		{"somehost", "somehost  ( 5.3s): "},
		{"otherhost", "otherhost (10.2s): "},
		{"foo", "foo       ( 0.9s): "},
	}
	for _, c := range cases {
		if got := HostAndDurationPrefix(task, c.host, layout); got != c.want {
			t.Errorf("HostAndDurationPrefix(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

func TestHostAndDurationPrefix(t *testing.T) {
	base := time.Date(2026, 9, 18, 8, 0, 0, 0, time.UTC)

	t.Run("ShowDuration false renders the plain pre-existing shape", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeOK
		if got, want := HostAndDurationPrefix(task, "web1", DurationLayout{}), "web1: "; got != want {
			t.Errorf("HostAndDurationPrefix = %q, want %q", got, want)
		}
	})

	t.Run("a host missing duration gets a same-width blank placeholder", func(t *testing.T) {
		withDuration := newTaskNode()
		withDuration.Hosts["web1"] = playbook.OutcomeOK
		withDuration.Started["web1"] = base
		withDuration.Finished["web1"] = base.Add(5300 * time.Millisecond)

		withoutDuration := newTaskNode()
		withoutDuration.Hosts["web1"] = playbook.OutcomeOK // no Started/Finished

		layout := DurationLayout{ShowDuration: true, HostWidth: 5, SecondsWidth: 4}
		withText := HostAndDurationPrefix(withDuration, "web1", layout)
		withoutText := HostAndDurationPrefix(withoutDuration, "web1", layout)

		if len(withoutText) != len(withText) {
			t.Errorf("blank-placeholder width = %d, want the same width a real duration takes (%d): %q vs %q",
				len(withoutText), len(withText), withoutText, withText)
		}
		if strings.ReplaceAll(withoutText, " ", "") != "web1:" {
			t.Errorf("HostAndDurationPrefix (no duration) = %q, want only \"web1:\" once all spaces are stripped", withoutText)
		}
	})

	t.Run("base example: base+1.2s and known SecondsWidth", func(t *testing.T) {
		task := newTaskNode()
		task.Hosts["web1"] = playbook.OutcomeOK
		task.Started["web1"] = base
		task.Finished["web1"] = base.Add(1200 * time.Millisecond)
		layout := DurationLayout{ShowDuration: true, HostWidth: 5, SecondsWidth: 3}
		if got, want := HostAndDurationPrefix(task, "web1", layout), "web1 (1.2s): "; got != want {
			t.Errorf("HostAndDurationPrefix = %q, want %q", got, want)
		}
	})
}
