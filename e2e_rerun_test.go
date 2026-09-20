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

//go:build e2e

// End-to-end smoke tests for the rerun feature, driving the real compiled
// binary inside a real tmux pane (tmux allocates an actual pty, which is
// what tview/tcell need - a plain piped/headless shell doesn't have one).
// See TESTING.md's "hard-to-test tier" for why this exists, why it's kept
// separate from the default suite (build tag e2e - run explicitly with
// `go test -tags e2e ./...`), and why it's deliberately small: these three
// scenarios target exactly the class of bug that justified writing this
// file in the first place - real, interactive-only bugs (the SetDisabled
// focus-skip quirk documented in tui.go/CLAUDE.md) that no pure-function
// unit test could ever see, not a general-purpose UI test suite.
//
// Needs `tmux` and `ansible-playbook` on PATH - skipped (not failed) if
// either is missing, since this is deliberately not part of the plain
// `go test ./...` dependency surface.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireE2ETools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found on PATH - skipping e2e test")
	}
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not found on PATH - skipping e2e test")
	}
}

// buildE2EBinary builds the tangsible binary once for the calling test,
// into that test's own t.TempDir() (so parallel/repeated runs never share
// or clobber one another's binary).
func buildE2EBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tangsible")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd() // `go test` already runs with the package
	// directory (this project's repo root) as cwd.
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	return wd
}

// shellQuote wraps s in single quotes, escaping any embedded ones - safe
// to splice into a shell command line sent via tmux send-keys, in case a
// t.TempDir() or repo path ever contains a space.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// lineContaining returns the first line of text containing substr, or ""
// if none does - used to assert on one dialog field's own line without
// the assertion accidentally matching a different field or row that
// happens to contain the same text.
func lineContaining(text, substr string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

// tmuxSession wraps one tmux session running the binary under test,
// cleaned up automatically (t.Cleanup) even if the test fails or panics.
type tmuxSession struct {
	name string
	t    *testing.T
}

func startTmuxSession(t *testing.T) *tmuxSession {
	t.Helper()
	name := fmt.Sprintf("tangsible-e2e-%d-%d", os.Getpid(), time.Now().UnixNano())
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "160", "-y", "45").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	s := &tmuxSession{name: name, t: t}
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", s.name).Run() // best-effort
	})
	return s
}

// send passes keys straight to `tmux send-keys` - each argument is either
// literal text (typed as-is, spaces included) or one of tmux's own key
// names ("Enter", "Tab", "Escape", "C-c", ...), exactly as if typed
// interactively.
func (s *tmuxSession) send(keys ...string) {
	s.t.Helper()
	args := append([]string{"send-keys", "-t", s.name}, keys...)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		s.t.Fatalf("tmux send-keys %v: %v\n%s", keys, err, out)
	}
}

func (s *tmuxSession) capture() string {
	out, err := exec.Command("tmux", "capture-pane", "-t", s.name, "-p").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// waitFor polls capture() until needle appears or timeout elapses (a
// plain poll loop, not a fixed sleep - the pane's content changes on its
// own schedule, driven by ansible-playbook/tview, not this test's), and
// fails the test with the last captured content if it never does.
func (s *tmuxSession) waitFor(needle string, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		last = s.capture()
		if strings.Contains(last, needle) {
			return last
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out after %s waiting for %q in tmux pane; last capture:\n%s", timeout, needle, last)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// waitForFieldValue polls until the first line of capture() containing
// label also contains value, or fails after timeout. Deliberately checks
// one specific line rather than the whole capture (unlike waitFor) -
// typed text can otherwise coincidentally match something already on
// screen for an unrelated reason (a static label, a tree row from a
// previous generation with the same name as what's being typed), which
// would make a plain waitFor return instantly, before the pty has
// actually finished delivering/rendering every character - exactly the
// bug this harness hit once already, in this same file.
func (s *tmuxSession) waitForFieldValue(label, value string, timeout time.Duration) string {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		last = s.capture()
		if strings.Contains(lineContaining(last, label), value) {
			return last
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out after %s waiting for %q to contain %q; last capture:\n%s", timeout, label, value, last)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// waitForAbsence polls until needle is no longer present in capture(), or
// fails after timeout - the inverse of waitFor, used to confirm a
// transition (e.g. a rerun clearing the previous generation's rows)
// actually happened before waiting for text that could otherwise match a
// stale, still-on-screen occurrence from before the transition.
func (s *tmuxSession) waitForAbsence(needle string, timeout time.Duration) {
	s.t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for {
		last = s.capture()
		if !strings.Contains(last, needle) {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out after %s waiting for %q to disappear from tmux pane; last capture:\n%s", timeout, needle, last)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestE2E_RerunDialog_TabCyclingLandsInCorrectField is a direct regression
// guard for the bug that originally justified this file: toggling the
// re-run dialog's now-removed "Start at task" checkbox used to silently
// advance Form focus by one extra step (InputField.SetDisabled's own
// finished(-1) call replaying the last real navigation key - see
// CLAUDE.md's Rerun section) - text reproducibly landed one field over
// from where it was typed. That checkbox is gone (design-docs/Rerun.md's
// "Extend rerun dialog" item 1 dropped "Start with task" outright, and its
// replacement, "Start with play," is a plain always-enabled field - see
// StartWithPlay.md), so the exact SetDisabled mechanism can't recur - but
// the dialog's field list is now built dynamically instead (rebuildRerunForm
// conditionally AddFormItems the three checkboxes based on the run's own
// data), which is its own, different way focus/tab-order could end up
// wrong. This test still pins down the thing that actually matters: typed
// text lands in the field the user was actually looking at, not a
// neighboring one.
func TestE2E_RerunDialog_TabCyclingLandsInCorrectField(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook := filepath.Join(repoRoot(t), "testdata", "outcomes.yml")

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i localhost,", shellQuote(workDir), shellQuote(bin), shellQuote(playbook)), "Enter")
	s.waitFor("Playbook completed successfully", 15*time.Second)

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)

	// The dialog opens with focus on the Play field (design-docs/
	// StartWithPlay.md's own first-in-tab-order field). outcomes.yml's
	// play has no name: key, so - per resumablePlayName's own guard -
	// "Resume where failed" is never offered here regardless of its
	// ignore_errors failure; Tab from Play lands directly on Tags.
	s.send("first")
	s.waitForFieldValue("Start with play:", "first", 3*time.Second)
	s.send("Tab")
	s.send("second")
	got := s.waitForFieldValue("Limit tags to:", "second", 3*time.Second)

	playLine := lineContaining(got, "Start with play:")
	tagsLine := lineContaining(got, "Limit tags to:")
	skipTagsLine := lineContaining(got, "Skip tags:")

	if !strings.Contains(playLine, "first") {
		t.Errorf("Play field = %q, want it to contain \"first\"", playLine)
	}
	if !strings.Contains(tagsLine, "second") {
		t.Errorf("Tags field = %q, want it to contain \"second\" (one Tab from Play)", tagsLine)
	}
	if strings.Contains(skipTagsLine, "second") {
		t.Errorf("Skip tags field = %q - \"second\" leaked past Tags into Skip tags, the exact regression this test guards against", skipTagsLine)
	}

	s.send("Escape") // cancel - don't actually start a run, this test only cares about focus/text placement
}

// TestE2E_RerunDialog_StartWithPlaySkipsEarlierPlays confirms
// design-docs/StartWithPlay.md's mechanism end to end via the dialog's own
// Play field specifically (TestE2E_CLIStartAtPlay below already covers the
// --start-at-play CLI form - a distinct code path, since the dialog spawns
// via runner.NewRequestRerun while the CLI form resolves/trims directly in
// run's own handling): confirming the dialog with a play name really drops
// every earlier play from the trimmed copy ansible-playbook actually runs,
// not just that the dialog accepts the text.
func TestE2E_RerunDialog_StartWithPlaySkipsEarlierPlays(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook := filepath.Join(workDir, "site.yml") // workDir, not testdata/ -
	// BuildTaskSourceIndex walks a playbook's whole directory tree, so a
	// fixture sharing testdata/ with every other e2e test's own playbook
	// would leak its play names into their own autocomplete candidates too
	// (same reasoning TestE2E_CLIStartAtPlay below already documents).
	playbookYAML := `- name: first play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: first play task
      command: echo hi

- name: second play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: second play task
      command: echo hi
`
	if err := os.WriteFile(playbook, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i localhost,", shellQuote(workDir), shellQuote(bin), shellQuote(playbook)), "Enter")
	s.waitFor("Playbook completed successfully", 15*time.Second)

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)
	s.send("second play") // dialog opens focused on Play - StartWithPlay.md
	s.waitForFieldValue("Start with play:", "second play", 3*time.Second)
	s.send("Enter")

	// Both plays' tasks are already on screen from the first (untrimmed)
	// run at this point, so waiting for "second play task" directly would
	// return instantly - wait for the first generation's own "first play
	// task" row to actually clear (confirming the rerun really started,
	// per requestRerun's state.Reset()) first.
	s.waitForAbsence("first play task", 5*time.Second)
	got := s.waitFor("Playbook completed successfully", 15*time.Second)
	if strings.Contains(got, "first play") {
		t.Errorf("expected \"first play\" to be dropped entirely by Start-with-play \"second play\", but it's in the rerun's tree:\n%s", got)
	}
	if !strings.Contains(got, "second play task") {
		t.Errorf("expected \"second play task\" (the Start-with-play target) to run:\n%s", got)
	}
}

// writeFailedUnreachableFixture writes a three-host playbook+inventory
// into workDir (not testdata/, for the same source-index-leakage reason
// TestE2E_RerunDialog_StartWithPlaySkipsEarlierPlays above avoids it) whose
// single named play gives PlaybookState.FailedHosts()/UnreachableHosts()
// two distinct, single-element sets to drive the re-run dialog's
// checkbox/field coupling: web1 succeeds, web2 genuinely fails "fail on
// web2" (not an ignore_errors: true one - see aggregate.go's own doc
// comment on why that distinction doesn't actually matter to this app's
// FailedHosts, but a real failure keeps this fixture's intent obvious to a
// reader), and ghost never connects at all (a short ConnectTimeout so this
// fixture doesn't hang waiting on a real network timeout).
func writeFailedUnreachableFixture(t *testing.T, workDir string) (playbookPath, inventoryPath string) {
	t.Helper()
	playbookPath = filepath.Join(workDir, "site.yml")
	playbookYAML := `- name: main play
  hosts: all
  gather_facts: false
  tasks:
    - name: ok task
      command: echo hi
    - name: fail on web2
      command: /bin/false
      when: inventory_hostname == "web2"
`
	if err := os.WriteFile(playbookPath, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	inventoryPath = filepath.Join(workDir, "inventory.ini")
	inventoryINI := `[all]
web1 ansible_connection=local
web2 ansible_connection=local
ghost ansible_host=203.0.113.1 ansible_connection=ssh ansible_ssh_common_args='-o ConnectTimeout=2 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'
`
	if err := os.WriteFile(inventoryPath, []byte(inventoryINI), 0o644); err != nil {
		t.Fatal(err)
	}
	return playbookPath, inventoryPath
}

// TestE2E_RerunDialog_ResumeWhereFailedCascadesToHostLimitedRerun is a
// direct regression guard for a real bug found live while writing this
// test: tview.Checkbox.SetChecked invokes its own changed-callback
// *before* updating its internal checked field, so resumeCheckbox's
// cascade into onlyFailedCheckbox (design-docs/Rerun.md: checking "Resume
// where failed" auto-checks "Only failed" too) originally read
// onlyFailedCheckbox.IsChecked() from inside that very cascade and saw the
// state it was leaving, not the one being set - silently computing an
// empty Hosts field. Fixed by passing each checkbox's own new value
// explicitly rather than re-querying it (see setHostsFieldFromCheckboxes's
// own comment in tui.go) - this test exercises that whole chain for real,
// through the actual tview widgets, and confirms it doesn't just look
// right in the dialog but that the resulting rerun is genuinely limited to
// the one host that actually failed.
//
// A second, independent bug was found and fixed while setting this test's
// own fixture up: EarliestFailingPlay's play name comes from the live
// jsonl stream, which Ansible always populates (synthesizing a default for
// an unnamed play), but "Start with play" can only ever target a play with
// an explicit name: key (StartWithPlay.md's own v1 scope decision) -
// confirmed live that confirming "Resume where failed" against an unnamed
// play's failure used to fail the whole rerun outright ("Playbook failed
// (exit code -1)"). resumablePlayName (rerundialog.go) is the fix; this
// fixture's play is deliberately named so this test exercises the
// happy path that fix protects, not the now-guarded-against gap itself
// (covered instead by TestResumablePlayName's own unit test).
func TestE2E_RerunDialog_ResumeWhereFailedCascadesToHostLimitedRerun(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook, inventory := writeFailedUnreachableFixture(t, workDir)

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i %s", shellQuote(workDir), shellQuote(bin), shellQuote(playbook), shellQuote(inventory)), "Enter")
	s.waitFor("Playbook completed - one or more hosts were unreachable", 20*time.Second)

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)
	s.send("Tab")   // Play -> Resume where failed
	s.send("Space") // check it

	got := s.waitForFieldValue("Start with play:", "main play", 3*time.Second)
	if !strings.Contains(lineContaining(got, "Only failed"), "X") {
		t.Errorf("expected \"Resume where failed\" to auto-check \"Only failed\" too, got:\n%s", lineContaining(got, "Only failed"))
	}
	got = s.waitForFieldValue("Limit hosts to:", "web2", 3*time.Second)
	hostsLine := lineContaining(got, "Limit hosts to:")
	if strings.Contains(hostsLine, "web1") || strings.Contains(hostsLine, "ghost") {
		t.Errorf("Hosts field = %q, want only the genuinely failed host \"web2\" - not the unreachable or unaffected ones", hostsLine)
	}

	s.send("Enter")
	s.waitForAbsence("ok=0  skipped=0  changed=0  unreachable=1", 5*time.Second) // the first generation's own ghost summary row clearing confirms the rerun really started
	// web2 is the fixture's genuinely-failing host, so the rerun (still
	// against just web2) fails again too - "Playbook failed", not
	// "completed successfully". That's expected and fine: this test's own
	// point is host scoping, not fixing the failure.
	got = s.waitFor("Playbook failed", 15*time.Second)
	if strings.Contains(got, "web1") || strings.Contains(got, "ghost") {
		t.Errorf("expected the rerun to touch only web2 (Resume where failed's own host limit), but web1/ghost appear in its tree:\n%s", got)
	}
	if !strings.Contains(got, "web2") {
		t.Errorf("expected web2 (the failed host) to appear in the rerun's tree:\n%s", got)
	}
}

// TestE2E_RerunDialog_UncheckingResumeClearsPlayAndOnlyFailed covers the
// reverse direction of the cascade above - design-docs/Rerun.md is
// explicit that unchecking "Resume where failed" (by clicking the
// checkbox itself) must clear "Start with play" and uncheck "Only failed"
// again, symmetric with checking it.
func TestE2E_RerunDialog_UncheckingResumeClearsPlayAndOnlyFailed(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook, inventory := writeFailedUnreachableFixture(t, workDir)

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i %s", shellQuote(workDir), shellQuote(bin), shellQuote(playbook), shellQuote(inventory)), "Enter")
	s.waitFor("Playbook completed - one or more hosts were unreachable", 20*time.Second)

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)
	s.send("Tab")   // Play -> Resume where failed
	s.send("Space") // check it
	s.waitForFieldValue("Start with play:", "main play", 3*time.Second)
	s.waitForFieldValue("Limit hosts to:", "web2", 3*time.Second)

	s.send("Space") // uncheck it again, same focused checkbox
	got := s.waitFor("Start with play:", 3*time.Second)
	playLine := lineContaining(got, "Start with play:")
	if strings.Contains(playLine, "main play") {
		t.Errorf("Play field = %q, want it cleared once \"Resume where failed\" is unchecked", playLine)
	}
	if strings.Contains(lineContaining(got, "Only failed"), "X") {
		t.Errorf("expected unchecking \"Resume where failed\" to also uncheck \"Only failed\", got:\n%s", lineContaining(got, "Only failed"))
	}
	hostsLine := lineContaining(got, "Limit hosts to:")
	if strings.Contains(hostsLine, "web2") {
		t.Errorf("Hosts field = %q, want it cleared along with \"Only failed\" unchecking", hostsLine)
	}

	s.send("Escape")
}

// TestE2E_RerunDialog_OnlyFailedAndOnlyUnreachableUnionIntoHosts covers
// the *other* cascade path - each of the two checkboxes' own independent
// SetChangedFunc handler (not routed through "Resume where failed" at
// all): checking both must union their host lists into "Limit hosts to",
// not overwrite one with the other.
func TestE2E_RerunDialog_OnlyFailedAndOnlyUnreachableUnionIntoHosts(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook, inventory := writeFailedUnreachableFixture(t, workDir)

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i %s", shellQuote(workDir), shellQuote(bin), shellQuote(playbook), shellQuote(inventory)), "Enter")
	s.waitFor("Playbook completed - one or more hosts were unreachable", 20*time.Second)

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)
	// Play -> Resume -> Tags -> Skip tags -> Hosts -> Only failed.
	s.send("Tab", "Tab", "Tab", "Tab", "Tab")
	s.send("Space") // check Only failed
	s.waitForFieldValue("Limit hosts to:", "web2", 3*time.Second)

	s.send("Tab")   // Only failed -> Only unreachable
	s.send("Space") // check it too
	got := s.waitForFieldValue("Limit hosts to:", "ghost,web2", 3*time.Second)

	if !strings.Contains(lineContaining(got, "Only failed"), "X") {
		t.Errorf("expected \"Only failed\" to stay checked once \"Only unreachable\" is also checked, got:\n%s", lineContaining(got, "Only failed"))
	}

	s.send("Escape")
}

// TestE2E_RerunDialog_EditingHostsFieldUnchecksBothOnlyCheckboxes covers
// Rerun.md's "don't fight the user" direction, symmetric to
// TestE2E_RerunDialog_ClearedFieldStaysCleared below but for the newer
// checkboxes rather than the -l pre-fill: editing "Limit hosts to" by hand
// after the checkboxes populated it must uncheck both, not silently
// re-overwrite what was just typed on the next unrelated redraw.
func TestE2E_RerunDialog_EditingHostsFieldUnchecksBothOnlyCheckboxes(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook, inventory := writeFailedUnreachableFixture(t, workDir)

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i %s", shellQuote(workDir), shellQuote(bin), shellQuote(playbook), shellQuote(inventory)), "Enter")
	s.waitFor("Playbook completed - one or more hosts were unreachable", 20*time.Second)

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)
	// Play -> Resume -> Tags -> Skip tags -> Hosts -> Only failed ->
	// Only unreachable.
	s.send("Tab", "Tab", "Tab", "Tab", "Tab")
	s.send("Space") // check Only failed
	s.waitForFieldValue("Limit hosts to:", "web2", 3*time.Second)
	s.send("Tab")
	s.send("Space") // check Only unreachable too
	s.waitForFieldValue("Limit hosts to:", "ghost,web2", 3*time.Second)

	s.send("BTab", "BTab") // Only unreachable -> Only failed -> Hosts
	s.send("C-u")          // clear it, same convention as
	// TestE2E_RerunDialog_ClearedFieldStaysCleared below
	s.send("manualhost")
	got := s.waitForFieldValue("Limit hosts to:", "manualhost", 3*time.Second)

	hostsLine := lineContaining(got, "Limit hosts to:")
	if strings.Contains(hostsLine, "web2") || strings.Contains(hostsLine, "ghost") {
		t.Errorf("Hosts field = %q, want the user's hand-typed value left alone, not fought by the checkboxes", hostsLine)
	}
	if strings.Contains(lineContaining(got, "Only failed"), "X") {
		t.Errorf("expected editing Hosts by hand to uncheck \"Only failed\", got:\n%s", lineContaining(got, "Only failed"))
	}
	if strings.Contains(lineContaining(got, "Only unreachable"), "X") {
		t.Errorf("expected editing Hosts by hand to uncheck \"Only unreachable\", got:\n%s", lineContaining(got, "Only unreachable"))
	}

	s.send("Escape")
}

// TestE2E_RerunDialog_ClearedFieldStaysCleared is a direct regression guard
// for a real bug reported live: the dialog's own pre-fill logic used to
// re-derive "has this field ever been touched" from whether it was
// currently empty (GetText() == "") - which can't distinguish "never
// edited" from "the user deliberately cleared it," since an empty Hosts
// field is itself a meaningful, intentional value (Reassemble's own "no
// --limit, run for every host"). Clearing the field to rerun against every
// host, then reopening the dialog, silently re-pre-filled the original
// -l value right back in - exactly the case this test pins down.
func TestE2E_RerunDialog_ClearedFieldStaysCleared(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook := filepath.Join(repoRoot(t), "testdata", "multihost.yml")
	inventory := filepath.Join(repoRoot(t), "testdata", "multihost-inventory.ini")

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s -i %s -l host1", shellQuote(workDir), shellQuote(bin), shellQuote(playbook), shellQuote(inventory)), "Enter")
	s.waitFor("Playbook completed successfully", 15*time.Second)

	s.send("r")
	got := s.waitForFieldValue("Limit hosts to:", "host1", 5*time.Second)
	if !strings.Contains(lineContaining(got, "Limit hosts to:"), "host1") {
		t.Fatalf("Hosts field on first open = %q, want it pre-filled with \"host1\" from -l", lineContaining(got, "Limit hosts to:"))
	}

	// Tab from Play -> Tags -> Skip tags -> Hosts, then clear it.
	// multihost.yml's play has no name: key, so - per resumablePlayName's
	// own guard (see CLAUDE.md's Rerun section) - "Resume where failed" is
	// never offered here despite host1's real failure, and there's no
	// "Only unreachable" candidate either; Play sits directly next to Tags.
	s.send("Tab", "Tab", "Tab", "C-u")
	s.send("Enter")

	// Both hosts' own rows must appear - confirms the clear actually took
	// effect and this generation really ran unrestricted, not just that
	// the field visually looked empty.
	s.waitFor("host2", 15*time.Second)
	got = s.waitFor("Playbook completed successfully", 15*time.Second)
	if !strings.Contains(got, "host1") || !strings.Contains(got, "host2") {
		t.Fatalf("expected the cleared Hosts field to run against every host, got:\n%s", got)
	}

	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)
	got = s.capture()
	hostsLine := lineContaining(got, "Limit hosts to:")
	if strings.Contains(hostsLine, "host1") {
		t.Errorf("Hosts field on second open = %q, want it to stay empty (as the user left it) rather than reverting to the original -l value", hostsLine)
	}
	s.send("Escape")
}

// TestE2E_CLIRerun_ShowsDialogBeforeRunning covers Phase D's own distinct
// code path (no generation started until the dialog is confirmed,
// everStarted gating the status row) with a real subprocess/file boundary
// - not just ResolveRerun's own unit tests, which never touch the TUI or
// .tangsible/state.toml at all.
func TestE2E_CLIRerun_ShowsDialogBeforeRunning(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook := filepath.Join(repoRoot(t), "testdata", "outcomes.yml")

	// Seed .tangsible/state.toml history with a first `run` invocation - `rerun`
	// (below, no playbook given) needs something to resolve to and
	// pre-fill from. The history write happens synchronously at the very
	// top of main(), long before "Playbook completed successfully" can
	// ever render, so waiting for that text is enough to know the write
	// already landed - no extra wait for the seeding process to exit.
	seed := startTmuxSession(t)
	seed.send(fmt.Sprintf("cd %s && %s run %s -i localhost, -l localhost", shellQuote(workDir), shellQuote(bin), shellQuote(playbook)), "Enter")
	seed.waitFor("Playbook completed successfully", 15*time.Second)

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s rerun", shellQuote(workDir), shellQuote(bin)), "Enter")
	got := s.waitFor("Re-run (enter: run, esc: cancel)", 5*time.Second)

	if strings.Contains(got, "Playbook completed successfully") {
		t.Errorf("status row rendered before any generation ever ran - everStarted gating regression:\n%s", got)
	}
	if hostsLine := lineContaining(got, "Limit hosts to:"); !strings.Contains(hostsLine, "localhost") {
		t.Errorf("Hosts field = %q, want it pre-filled with \"localhost\" from .tangsible/state.toml history", hostsLine)
	}

	s.send("Enter")
	got = s.waitFor("Playbook completed successfully", 15*time.Second)
	if !strings.Contains(got, "ok task") {
		t.Errorf("expected the rerun to actually execute the playbook's tasks:\n%s", got)
	}
}

// TestE2E_CLIStartAtPlay covers design-docs/StartWithPlay.md's CLI form -
// "tangsible run --start-at-play <name>" - end to end: the named play's
// own tasks run and every earlier play is dropped entirely, and (the bug
// this test would have caught) the drill-down view's own "Task
// definition" tab still finds source for a task defined directly in the
// trimmed play, not just tasks reached via roles/includes.
//
// The fixture playbook is written into this test's own isolated workDir
// rather than added to testdata/ - a real, reproduced hazard otherwise:
// BuildTaskSourceIndex walks a playbook's *whole* directory tree, so a
// fixture sharing testdata/ with every other e2e test's own playbook
// would leak its play/task names into their own autocomplete candidate
// lists too. That's exactly how TestBuildTaskSourceIndex's own unit test
// (source_test.go) already avoids this same hazard for a different
// reason - same fix, applied here for the same underlying cause.
func TestE2E_CLIStartAtPlay(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	playbook := filepath.Join(workDir, "site.yml")
	playbookYAML := `- name: first play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: first play task
      command: echo hi

- name: second play
  hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: second play task
      command: echo hi
`
	if err := os.WriteFile(playbook, []byte(playbookYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s run %s --start-at-play %s -i localhost,", shellQuote(workDir), shellQuote(bin), shellQuote(playbook), shellQuote("second play")), "Enter")
	got := s.waitFor("Playbook completed successfully", 15*time.Second)

	if strings.Contains(got, "first play") {
		t.Errorf("expected the first play to be dropped entirely by --start-at-play, but it's in the tree:\n%s", got)
	}
	if !strings.Contains(got, "second play task") {
		t.Errorf("expected the named play's own task to run:\n%s", got)
	}

	// Drill into the second play's own task and confirm its "Task
	// definition" tab is present - the regression this test guards
	// against: a task defined directly in the top-level playbook (as
	// opposed to one reached via a role) reports a RawEvent.Task.Path
	// pointing into the trimmed temp copy, not the original file the
	// session's own TaskSourceIndex was built from.
	s.send("Home")
	s.send("Down")  // onto the (only) task row
	s.send("Right") // expand it
	s.send("Down")  // onto the host row
	s.send("Enter") // open the drill-down
	got = s.waitFor("Task definition", 5*time.Second)
	if !strings.Contains(got, "Task definition") {
		t.Errorf("expected a \"Task definition\" tab for a task defined directly in the trimmed play:\n%s", got)
	}
}
