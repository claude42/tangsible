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

package main

import (
	"slices"
	"testing"
)

// realListTasksOutput is a real
// `ansible-playbook ... --list-tasks --list-hosts` transcript (captured
// live against a throwaway fixture with a named play,
// pre_tasks/roles/tasks/post_tasks, and a tags-only "plain task"; both
// plays match the inventory here), not hand-written - pinning
// parseListTasksOutput against actual ansible-core output rather than an
// assumed shape.
const realListTasksOutput = `
playbook: /tmp/named_probe.yml

  play #1 (all): First play does setup	TAGS: []
    pattern: ['all']
    hosts (1):
      localhost
    tasks:
      a pre task	TAGS: []
      myrole : role task one	TAGS: []
      myrole : role task two	TAGS: []
      plain task	TAGS: [bar, foo]
      a post task	TAGS: []

  play #2 (all): Second play	TAGS: []
    pattern: ['all']
    hosts (1):
      localhost
    tasks:
      only task	TAGS: []
`

// realListTasksOutputWithZeroHostPlay is a real
// `ansible-playbook ... --list-tasks --list-hosts` transcript (captured
// live against a throwaway fixture with a play targeting a group absent
// from the inventory - hosts (0) - followed by a real play), trimmed
// from 40 unreachable tasks down to 2 for brevity - the trimmed lines
// are otherwise byte-identical to what was actually captured.
const realListTasksOutputWithZeroHostPlay = `
playbook: /tmp/unreachable_gap.yml

  play #1 (nogroup): Targets a group not in this inventory	TAGS: []
    pattern: ['nogroup']
    hosts (0):
    tasks:
      unreachable task 0	TAGS: []
      unreachable task 1	TAGS: []

  play #2 (all): Real play	TAGS: []
    pattern: ['all']
    hosts (1):
      localhost
    tasks:
      unique finisher after unreachable play	TAGS: []
`

func TestParseListTasksOutput(t *testing.T) {
	t.Run("real ansible-core transcript, two plays, a role, tags", func(t *testing.T) {
		got := parseListTasksOutput(realListTasksOutput)
		want := []progressEntry{
			{Play: "First play does setup", Task: "a pre task"},
			{Play: "First play does setup", Task: "myrole : role task one"},
			{Play: "First play does setup", Task: "myrole : role task two"},
			{Play: "First play does setup", Task: "plain task"},
			{Play: "First play does setup", Task: "a post task"},
			{Play: "Second play", Task: "only task"},
		}
		if !slices.Equal(got, want) {
			t.Errorf("parseListTasksOutput() = %+v, want %+v", got, want)
		}
	})

	t.Run("unrecognized output yields an empty, non-fatal skeleton", func(t *testing.T) {
		got := parseListTasksOutput("not --list-tasks output at all\njust some noise\n")
		if len(got) != 0 {
			t.Errorf("parseListTasksOutput() = %v, want empty", got)
		}
	})

	t.Run("empty output", func(t *testing.T) {
		got := parseListTasksOutput("")
		if len(got) != 0 {
			t.Errorf("parseListTasksOutput() = %v, want empty", got)
		}
	})

	t.Run("a zero-host play's tasks are excluded from the skeleton entirely", func(t *testing.T) {
		got := parseListTasksOutput(realListTasksOutputWithZeroHostPlay)
		want := []progressEntry{
			{Play: "Real play", Task: "unique finisher after unreachable play"},
		}
		if !slices.Equal(got, want) {
			t.Errorf("parseListTasksOutput() = %+v, want %+v", got, want)
		}
	})
}

func TestProgressTracker(t *testing.T) {
	skeleton := []progressEntry{
		{Play: "P", Task: "one"},
		{Play: "P", Task: "two"},
		{Play: "P", Task: "three"},
	}

	t.Run("nil tracker is a safe no-op", func(t *testing.T) {
		var tr *progressTracker
		tr.Advance("P", "one")
		pos, total := tr.Position()
		if pos != 0 || total != 0 {
			t.Errorf("Position() = (%d, %d), want (0, 0)", pos, total)
		}
	})

	t.Run("empty skeleton reports (0, 0) and never matches", func(t *testing.T) {
		tr := newProgressTracker(nil)
		tr.Advance("P", "one")
		pos, total := tr.Position()
		if pos != 0 || total != 0 {
			t.Errorf("Position() = (%d, %d), want (0, 0)", pos, total)
		}
	})

	t.Run("sequential matches advance position monotonically", func(t *testing.T) {
		tr := newProgressTracker(skeleton)
		tr.Advance("P", "one")
		if pos, total := tr.Position(); pos != 1 || total != 3 {
			t.Fatalf("after 'one': Position() = (%d, %d), want (1, 3)", pos, total)
		}
		tr.Advance("P", "two")
		if pos, total := tr.Position(); pos != 2 || total != 3 {
			t.Fatalf("after 'two': Position() = (%d, %d), want (2, 3)", pos, total)
		}
		tr.Advance("P", "three")
		if pos, total := tr.Position(); pos != 3 || total != 3 {
			t.Fatalf("after 'three': Position() = (%d, %d), want (3, 3)", pos, total)
		}
	})

	t.Run("a miss (e.g. a handler) leaves the position untouched", func(t *testing.T) {
		tr := newProgressTracker(skeleton)
		tr.Advance("P", "one")
		tr.Advance("P", "my handler") // not in the skeleton at all
		if pos, total := tr.Position(); pos != 1 || total != 3 {
			t.Errorf("after miss: Position() = (%d, %d), want (1, 3) unchanged", pos, total)
		}
	})

	t.Run("a repeated name only matches forward from the cursor, never backward", func(t *testing.T) {
		// A task file included twice produces the same (play, task) key
		// twice in the skeleton - the second real occurrence must match
		// the second skeleton entry, not re-match the first one already
		// consumed.
		dup := []progressEntry{
			{Play: "P", Task: "shared"},
			{Play: "P", Task: "unique"},
			{Play: "P", Task: "shared"},
		}
		tr := newProgressTracker(dup)
		tr.Advance("P", "shared")
		if pos, _ := tr.Position(); pos != 1 {
			t.Fatalf("first 'shared': Position() pos = %d, want 1", pos)
		}
		tr.Advance("P", "unique")
		if pos, _ := tr.Position(); pos != 2 {
			t.Fatalf("'unique': Position() pos = %d, want 2", pos)
		}
		tr.Advance("P", "shared")
		if pos, _ := tr.Position(); pos != 3 {
			t.Errorf("second 'shared': Position() pos = %d, want 3 (not back to 1)", pos)
		}
	})

	t.Run("a match beyond the base window is treated as not found on the first try", func(t *testing.T) {
		far := make([]progressEntry, progressBaseLookahead+5)
		for i := range far {
			far[i] = progressEntry{Play: "P", Task: "filler"}
		}
		far[progressBaseLookahead+2] = progressEntry{Play: "P", Task: "distant"}
		tr := newProgressTracker(far)
		tr.Advance("P", "distant")
		if pos, _ := tr.Position(); pos != 0 {
			t.Errorf("Position() pos = %d, want 0 (match was outside the base window)", pos)
		}
	})

	t.Run("a long unmatched stretch (e.g. a dynamic include block) is eventually bridged, not permanently stuck", func(t *testing.T) {
		// Simulates the real-world case that motivated the adaptive
		// window: a block of dynamic content bigger than the base window
		// sits between the cursor and the next real, unique, statically-
		// listed task - a fixed window can never recover from this; the
		// adaptive one must.
		const gap = 500
		big := make([]progressEntry, gap+1)
		for i := range big {
			big[i] = progressEntry{Play: "P", Task: "filler"}
		}
		big[gap] = progressEntry{Play: "P", Task: "unique finisher"}
		tr := newProgressTracker(big)
		// A run of real events with no static counterpart at all (the
		// dynamic block's own children) - each one misses and widens the
		// window for the next.
		for i := 0; i < 10; i++ {
			tr.Advance("P", "some dynamically-included task")
		}
		tr.Advance("P", "unique finisher")
		if pos, total := tr.Position(); pos != gap+1 || total != gap+1 {
			t.Errorf("Position() = (%d, %d), want (%d, %d) - the finisher should eventually be found", pos, total, gap+1, gap+1)
		}
	})

	t.Run("a successful match resets the window back to the tight base case", func(t *testing.T) {
		skeleton := make([]progressEntry, 400)
		for i := range skeleton {
			skeleton[i] = progressEntry{Play: "P", Task: "filler"}
		}
		skeleton[200] = progressEntry{Play: "P", Task: "recovered"}
		skeleton[210] = progressEntry{Play: "P", Task: "next"}
		skeleton[399] = progressEntry{Play: "P", Task: "far away"}

		tr := newProgressTracker(skeleton)
		// Widen the window with misses, then land on "recovered".
		for i := 0; i < 6; i++ {
			tr.Advance("P", "dynamic child")
		}
		tr.Advance("P", "recovered")
		if pos, _ := tr.Position(); pos != 201 {
			t.Fatalf("after 'recovered': pos = %d, want 201", pos)
		}
		// Immediately after a hit, a distant unrelated match must NOT be
		// trusted - if the window failed to reset, this would wrongly
		// jump straight to "far away" instead of stalling.
		tr.Advance("P", "far away")
		if pos, _ := tr.Position(); pos != 201 {
			t.Errorf("after reset, distant match wrongly accepted: pos = %d, want unchanged 201", pos)
		}
		// A nearby match (within the reset base window) still works.
		tr.Advance("P", "next")
		if pos, _ := tr.Position(); pos != 211 {
			t.Errorf("after 'next': pos = %d, want 211", pos)
		}
	})
}

func TestProgressTrackerAdvanceToPlay(t *testing.T) {
	t.Run("bridges an entirely-skipped play (zero matching hosts) that Advance alone cannot recover from", func(t *testing.T) {
		// Reproduces the real-world case this was built for: a play whose
		// hosts: pattern matches nothing in this run's inventory is still
		// fully listed by --list-tasks, but its tasks never fire a single
		// v2_playbook_on_task_start of their own for Advance to match
		// against - so nothing but the play boundary itself can ever move
		// the cursor past it.
		skeleton := make([]progressEntry, 41)
		for i := 0; i < 40; i++ {
			skeleton[i] = progressEntry{Play: "Unreachable play", Task: "unreachable task"}
		}
		skeleton[40] = progressEntry{Play: "Real play", Task: "unique finisher"}

		tr := newProgressTracker(skeleton)
		// The unreachable play's own 40 tasks never produce any event at
		// all - nothing to call Advance with. Only its own play-start
		// event, and the real play's, ever fire.
		tr.AdvanceToPlay("Unreachable play")
		if pos, _ := tr.Position(); pos != 0 {
			t.Fatalf("after entering the first play: pos = %d, want 0 (its own first task not yet confirmed)", pos)
		}
		tr.AdvanceToPlay("Real play")
		if pos, total := tr.Position(); pos != 40 || total != 41 {
			t.Fatalf("after entering the real play: Position() = (%d, %d), want (40, 41)", pos, total)
		}
		// The real play's own one task now fires for real, and Advance
		// (tight base window, since AdvanceToPlay resets missStreak) must
		// still be able to claim it.
		tr.Advance("Real play", "unique finisher")
		if pos, total := tr.Position(); pos != 41 || total != 41 {
			t.Errorf("after the real task: Position() = (%d, %d), want (41, 41)", pos, total)
		}
	})

	t.Run("never moves backward, and a miss leaves the tracker untouched", func(t *testing.T) {
		skeleton := []progressEntry{
			{Play: "A", Task: "a1"},
			{Play: "B", Task: "b1"},
			{Play: "C", Task: "c1"},
		}
		tr := newProgressTracker(skeleton)
		tr.Advance("A", "a1")
		if pos, _ := tr.Position(); pos != 1 {
			t.Fatalf("after 'a1': pos = %d, want 1", pos)
		}
		// A play name that doesn't appear ahead of the cursor at all.
		tr.AdvanceToPlay("nonexistent play")
		if pos, _ := tr.Position(); pos != 1 {
			t.Errorf("after a miss: pos = %d, want unchanged 1", pos)
		}
		// A play name that only exists behind the cursor must not be
		// re-matched (no wraparound).
		tr.AdvanceToPlay("A")
		if pos, _ := tr.Position(); pos != 1 {
			t.Errorf("after re-requesting an already-passed play: pos = %d, want unchanged 1", pos)
		}
	})

	t.Run("nil tracker is a safe no-op", func(t *testing.T) {
		var tr *progressTracker
		tr.AdvanceToPlay("whatever")
		if pos, total := tr.Position(); pos != 0 || total != 0 {
			t.Errorf("Position() = (%d, %d), want (0, 0)", pos, total)
		}
	})
}
