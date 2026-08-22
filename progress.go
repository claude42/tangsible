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

// Implements a first-prototype "Task x/y" progress indicator (top bar,
// tui.go's topBarText). There is no event in the jsonl stream that tells
// us upfront how many tasks a run will execute (CLAUDE.md's own
// Aggregation section: plays/tasks are only ever discovered as they
// start), so this predicts a task sequence ahead of time from a second,
// throwaway `ansible-playbook ... --list-tasks --list-hosts` invocation
// (confirmed empirically that both flags can be given together in one
// call - ansible merges each play's pattern/hosts/tasks into a single
// block regardless of which order the two flags are given in, so this
// needs only one subprocess, not two), then matches each real task-start
// event against it live.
//
// This is deliberately approximate, not exact - confirmed empirically
// (a handful of small probe playbooks, not kept in the repo) before
// building any of this:
//   - neither flag ever lists a handler at all, even a notified one that
//     will genuinely run - so a handler's own task-start event can never
//     have a static counterpart to match.
//   - --list-tasks does not expand a dynamic `include_tasks:` (a
//     Jinja-templated path, or one used inside a `loop:`) into its real
//     children at all - it shows one opaque line for the include
//     statement itself, which then never appears as its own event at
//     runtime (only its expanded children's task-start events do; a
//     loop over include_tasks fires that many real events for what
//     --list-tasks counted as one).
//   - a `when: false` task, and ordinary --tags/--skip-tags filtering,
//     do NOT cause a mismatch: the task is still listed and still fires
//     a real task-start event either way.
//   - --list-tasks always lists every play's tasks regardless of whether
//     any host will ever run them, but --list-hosts DOES apply -l/
//     --limit and reports "hosts (0):" for a play matching nothing in
//     this run's inventory - real events never fire for such a play's
//     tasks at all (confirmed empirically: the play itself still gets a
//     v2_playbook_on_play_start, but none of its tasks ever start). Any
//     play reporting zero hosts has its tasks excluded from the
//     skeleton entirely (parseListTasksOutput below), rather than
//     inflating the total with tasks that can never execute in this run.
//
// Given that, matching by plain "have we seen this task before" is
// wrong: task names are not unique across a real playbook (the same
// role, or the same included file, commonly runs more than once), so a
// naive lookup can match a real event to the wrong occurrence and make
// the indicator jump - which is worse than not moving at all. See
// progressTracker for the bounded, forward-only matching this uses
// instead, and its own accepted failure mode (undercounting, never
// overcounting).
package main

import (
	"bytes"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// progressEntry is one predicted task, keyed the same way a real
// v2_playbook_on_task_start event's own play.Name/task.Name pair is -
// confirmed empirically that a role-qualified task name ("myrole : task
// name") renders identically in both --list-tasks' own output and a real
// event's task.name, so no separate un-qualifying step is needed.
type progressEntry struct {
	Play string
	Task string
}

// progressPlayLine/progressTaskLine/progressHostsCountLine match
// "ansible-playbook ... --list-tasks --list-hosts"' own combined output,
// confirmed empirically against a real ansible-core install (pinned
// ANSIBLE_JSON_INDENT/callback env vars don't affect this - both flags
// bypass the callback system entirely, per CLAUDE.md, and always print
// this same plain-text format regardless). Confirmed too that both
// flags can be given in the same invocation - ansible merges each
// play's own pattern/hosts/tasks into one combined block, in either
// flag order, rather than needing two separate subprocess calls:
//
//	  play #1 (nogroup): Targets a group not in this inventory	TAGS: []
//	    pattern: ['nogroup']
//	    hosts (0):
//
//	  play #2 (all): Real play	TAGS: []
//	    pattern: ['all']
//	    hosts (1):
//	      localhost
//	    tasks:
//	      unique finisher after unreachable play	TAGS: []
//
// This is a human-readable summary, not a documented machine format, so
// treat it the same as this project's other "documented heuristic, not
// chased to 100%" text-scraping (e.g. colorizeYAML) - a line that
// doesn't match any recognized pattern (a future ansible-core version's
// reformatted output) is silently skipped, never an error.
var (
	progressPlayLine       = regexp.MustCompile(`^  play #\d+ \([^)]*\): (.+)\tTAGS: `)
	progressTaskLine       = regexp.MustCompile(`^      (.+)\tTAGS: `)
	progressHostsCountLine = regexp.MustCompile(`^    hosts \((\d+)\):`)
)

// parseListTasksOutput turns the combined "--list-tasks --list-hosts"
// stdout into a flat, execution-order sequence of progressEntry - flat
// because --list-tasks itself already merges a play's pre_tasks/roles/
// tasks/post_tasks into one single, correctly-ordered "tasks:" section
// (confirmed empirically), so no separate section-tracking is needed
// beyond which play a task line currently falls under.
//
// A play's own "hosts (0):" line - present because --list-hosts, unlike
// --list-tasks alone, DOES apply -l/--limit - drops every task under it
// from the skeleton entirely: such a play's tasks are structurally
// guaranteed to never fire a single real event in this run (confirmed
// empirically: the play itself still gets a v2_playbook_on_play_start,
// but none of its tasks ever start), so counting them would only ever
// inflate the total, never be matched.
func parseListTasksOutput(output string) []progressEntry {
	var entries []progressEntry
	var currentPlay string
	var skipCurrentPlay bool
	for _, line := range strings.Split(output, "\n") {
		if m := progressPlayLine.FindStringSubmatch(line); m != nil {
			currentPlay = m[1]
			skipCurrentPlay = false // corrected by this play's own "hosts (N):" line, below, before any "tasks:" line can follow it
			continue
		}
		if m := progressHostsCountLine.FindStringSubmatch(line); m != nil {
			if count, _ := strconv.Atoi(m[1]); count == 0 {
				skipCurrentPlay = true
			}
			continue
		}
		if skipCurrentPlay {
			continue
		}
		if m := progressTaskLine.FindStringSubmatch(line); m != nil {
			entries = append(entries, progressEntry{Play: currentPlay, Task: m[1]})
		}
	}
	return entries
}

// buildProgressSkeleton shells out to a single, throwaway
// "ansible-playbook <playbook> <passthroughArgs> --list-tasks
// --list-hosts" invocation - always best-effort: any failure
// (ansible-playbook missing, the same real playbook error the actual
// run's own pre-flight gate would separately catch, an ansible-core
// version whose output this parser doesn't recognize) just means no
// progress indicator at all, never a fatal error, since this sits
// entirely on top of a run that already works without it.
//
// passthroughArgs must be the exact args the corresponding real
// generation is itself about to run with - confirmed empirically that
// --list-tasks' own task list shrinks under --tags/--skip-tags, and
// --list-hosts' own per-play host counts shrink under -l/--limit,
// exactly like the real run's own scope does, so a mismatch here would
// make the predicted and real sequences disagree about what's even in
// scope before either one starts.
//
// Known, accepted gap for this prototype: --ask-vault-pass/
// --ask-become-pass (CLAUDE.md's own "Current scope constraints") would
// make this throwaway invocation prompt for a password on the real
// terminal too, in addition to the real run's own first generation doing
// the same - narrow enough (only playbooks actually using those flags)
// to leave as a known limitation rather than solve up front.
func buildProgressSkeleton(playbook string, passthroughArgs []string) []progressEntry {
	args := append([]string{playbook}, passthroughArgs...)
	args = append(args, "--list-tasks", "--list-hosts")
	cmd := exec.Command("ansible-playbook", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil
	}
	return parseListTasksOutput(stdout.String())
}

// progressBaseLookahead bounds how far ahead of the tracker's own cursor
// a match is trusted while things are going normally (missStreak == 0) -
// see progressTracker's doc comment for why a match has to be bounded at
// all once a task name can repeat, and for how this bound grows instead
// of staying fixed once misses start piling up.
const progressBaseLookahead = 25

// progressMaxMissShift caps how many times progressBaseLookahead doubles
// as missStreak grows (see window below) - 12 already gives a window of
// 25*4096 = 102400, far larger than any playbook this project targets,
// so this only exists to keep the shift itself well-defined rather than
// to meaningfully limit real recovery.
const progressMaxMissShift = 12

// progressTracker matches real, in-order task-start events against a
// static skeleton (see buildProgressSkeleton) to produce an approximate
// "position / total" - approximate because dynamic content (a notified
// handler, a dynamically-included task) has no static counterpart to
// match at all, in which case Advance leaves the tracker's own state
// exactly as it was, rather than treating a miss as a regression.
//
// Matching walks forward from a monotonic cursor rather than doing a
// plain (play, task) lookup, and only within a bounded window ahead of
// it, for a reason confirmed empirically, not assumed: a task name is
// not unique across a real playbook - the same included file, or the
// same role, commonly runs more than once - so a fully unbounded lookup
// can match a real event to the WRONG occurrence (one already passed, or
// one much further ahead than what's actually running) and make the
// indicator jump incorrectly.
//
// The window itself is adaptive, not fixed - a real, live-tested gap
// this design missed on its first real-world run: a single block of
// dynamic content the skeleton couldn't predict at all (a sizeable
// include_role/include_tasks tree is entirely plausible in a large
// playbook) can easily be wider than any fixed window, and once the
// cursor falls behind by more than the window, a *fixed* window can
// never recover - not even for a later, perfectly unique task name,
// since the window is measured from the stuck cursor, not from where
// that task actually sits. missStreak (consecutive Advance calls that
// found nothing) doubles the window each time, capped at
// progressMaxMissShift, and resets to zero the instant something
// matches again - so an isolated, ordinary gap stays tightly bounded
// (protecting against a coincidental collision, the original concern),
// while a long unmatched stretch progressively widens the search until
// it bridges back to the next thing that actually is in the skeleton.
// Undercounting during that widening (the bar stalls while catching up)
// is still the deliberately preferred failure mode over ever jumping
// backward or overcounting.
type progressTracker struct {
	skeleton   []progressEntry
	cursor     int // index of the first not-yet-matched skeleton entry
	matched    int // 1-based position of the most recent successful match
	missStreak int // consecutive Advance calls that found no match
}

// newProgressTracker never returns nil - an empty/nil skeleton (e.g.
// buildProgressSkeleton failed) just makes Position() always report
// (0, 0), which callers already treat as "nothing to show" identically
// to a genuinely absent tracker.
func newProgressTracker(skeleton []progressEntry) *progressTracker {
	return &progressTracker{skeleton: skeleton}
}

// Advance looks for (play, task) among the next currently-in-effect
// window of unconsumed skeleton entries (see progressTracker's own doc
// comment for how that window grows on repeated misses). On a match, the
// cursor moves just past it, that match's own 1-based position becomes
// the tracker's new Position(), and missStreak resets to zero; on a
// miss, the tracker's cursor/matched are left completely untouched and
// missStreak grows by one, widening the next call's own window. Safe to
// call on a nil *progressTracker (a no-op) - the state before this
// session's very first skeleton has ever been built, or hasn't been
// built for this particular generation yet.
func (t *progressTracker) Advance(play, task string) {
	if t == nil || len(t.skeleton) == 0 {
		return
	}
	window := progressBaseLookahead << min(t.missStreak, progressMaxMissShift)
	limit := t.cursor + window
	if limit > len(t.skeleton) {
		limit = len(t.skeleton)
	}
	want := progressEntry{Play: play, Task: task}
	for i := t.cursor; i < limit; i++ {
		if t.skeleton[i] == want {
			t.cursor = i + 1
			t.matched = i + 1
			t.missStreak = 0
			return
		}
	}
	t.missStreak++
}

// AdvanceToPlay resyncs the tracker directly to the start of playName's
// own section of the skeleton, searching the *entire* remainder rather
// than any bounded window - justified because a play boundary is a much
// stronger, less ambiguous signal than a single task name (see
// aggregate.go's OnPlayStarted: it's the one event confirmed, empirically,
// to fire even for a play whose hosts: pattern matches zero hosts in this
// run's inventory, whose tasks then never produce a single event of their
// own - a real, not hypothetical, gap Advance's own per-task matching
// structurally cannot recover from by itself, since there's nothing to
// call Advance with for a task that never starts at all).
//
// On a match, the cursor moves to that play's own first entry - not past
// it - so the play's real first task-start event can still claim it via
// a normal Advance call afterward; Position() in the meantime already
// credits everything strictly before this play as done, which is
// accurate (an earlier play, or several, were skipped over entirely to
// get here). missStreak resets to zero, same as a normal Advance hit -
// this is a confident resync, not evidence to stay cautious about. A
// miss (this exact play name never appears anywhere ahead of the
// cursor - e.g. the throwaway --list-tasks probe somehow used different
// scope than the real run) leaves the tracker completely untouched, same
// "undercount, never guess" rule Advance itself follows.
func (t *progressTracker) AdvanceToPlay(play string) {
	if t == nil || len(t.skeleton) == 0 {
		return
	}
	for i := t.cursor; i < len(t.skeleton); i++ {
		if t.skeleton[i].Play == play {
			t.cursor = i
			t.matched = i
			t.missStreak = 0
			return
		}
	}
}

// Position reports the most recent match's 1-based position and the
// skeleton's own total size - (0, 0) for a nil tracker, or one whose
// skeleton is empty, both meaning the same thing to callers: no progress
// data available, show nothing rather than a misleading "0/0".
func (t *progressTracker) Position() (position, total int) {
	if t == nil {
		return 0, 0
	}
	return t.matched, len(t.skeleton)
}
