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

package session

import (
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/runner"
	"github.com/rivo/tview"
)

// newTestRerunFieldSync builds one against a state with a named play
// ("main play") containing one genuinely failed host (web2) and one
// unreachable host (ghost) - the same shape e2e_rerun_test.go's own
// writeFailedUnreachableFixture uses, so these unit tests exercise the
// exact scenarios that harness's tmux-driven tests already cover live, at
// the wiring level rather than through a real terminal.
func newTestRerunFieldSync(t *testing.T) *rerunFieldSync {
	t.Helper()
	state := &playbook.PlaybookState{
		AllHosts: []string{"ghost", "web1", "web2"},
		Plays: []*playbook.PlayNode{{
			Name: "main play",
			Tasks: []*playbook.TaskNode{{
				Name: "ok task",
				Hosts: map[string]playbook.Outcome{
					"web1": playbook.OutcomeOK,
					"web2": playbook.OutcomeOK,
				},
			}, {
				Name: "fail on web2",
				Hosts: map[string]playbook.Outcome{
					"web2":  playbook.OutcomeFailed,
					"ghost": playbook.OutcomeUnreachable,
				},
			}},
		}},
	}
	form := tview.NewForm()
	return newRerunFieldSync(form, state, []string{"deploy"}, []string{"main play"}, nil, runner.InitialRerunDefaults{})
}

// checked reports whether label's checkbox line would render checked -
// same shape as e2e_rerun_test.go's own IsChecked assertions, but reading
// the real widget directly.
func checked(cb *tview.Checkbox) bool { return cb.IsChecked() }

func TestRerunFieldSyncRebuildOffersCheckboxesWhenThereIsSomethingToDo(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()

	if r.currentResumePlay != "main play" {
		t.Errorf("currentResumePlay = %q, want %q", r.currentResumePlay, "main play")
	}
	if got := r.currentFailedHosts; len(got) != 1 || got[0] != "web2" {
		t.Errorf("currentFailedHosts = %v, want [web2]", got)
	}
	if got := r.currentUnreachableHosts; len(got) != 1 || got[0] != "ghost" {
		t.Errorf("currentUnreachableHosts = %v, want [ghost]", got)
	}

	// Play, Resume, Tags, Skip tags, Hosts, Only failed, Only unreachable -
	// every optional item present, since this fixture has both a resumable
	// failing play and both kinds of trouble.
	if got, want := r.form.GetFormItemCount(), 7; got != want {
		t.Fatalf("form has %d items, want %d", got, want)
	}
}

func TestRerunFieldSyncRebuildOmitsResumeCheckboxForUnnamedFailingPlay(t *testing.T) {
	// resumablePlayName's own guard (rerundialog.go) - EarliestFailingPlay
	// can return a name (Ansible's own synthesized default for an unnamed
	// play) that ListTopLevelPlayNames' static scan never saw, since it
	// only lists plays with an explicit name: key. rebuild must not offer
	// "Resume where failed" for a play it can't actually resume into.
	state := &playbook.PlaybookState{
		Plays: []*playbook.PlayNode{{
			Name: "localhost", // Ansible's own synthesized name for an unnamed play
			Tasks: []*playbook.TaskNode{{
				Name:  "t",
				Hosts: map[string]playbook.Outcome{"web1": playbook.OutcomeFailed},
			}},
		}},
	}
	form := tview.NewForm()
	// knownPlayNames deliberately does NOT contain "localhost" - it was
	// never a named play in the source YAML.
	r := newRerunFieldSync(form, state, nil, nil, nil, runner.InitialRerunDefaults{})
	r.rebuild()

	if r.currentResumePlay != "" {
		t.Errorf("currentResumePlay = %q, want empty (not a resumable play)", r.currentResumePlay)
	}
	for i := 0; i < r.form.GetFormItemCount(); i++ {
		if item := r.form.GetFormItem(i); item == tview.FormItem(r.resumeCheckbox) {
			t.Fatal("Resume where failed checkbox must not appear for an unresumable play")
		}
	}
}

// TestRerunFieldSyncResumeCascade is the direct regression guard for the
// checkbox-ordering bug (see setHostsFieldFromCheckboxes's own doc
// comment): checking "Resume where failed" through the real
// tview.Checkbox.SetChecked call must populate the Play field and check
// "Only failed" with the *correct* hosts, not an empty list from a stale
// IsChecked() read.
func TestRerunFieldSyncResumeCascade(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()

	r.resumeCheckbox.SetChecked(true)

	if got := r.playField.GetText(); got != "main play" {
		t.Errorf("playField after checking Resume = %q, want %q", got, "main play")
	}
	if !checked(r.onlyFailedCheckbox) {
		t.Error("checking Resume where failed should auto-check Only failed")
	}
	if got := r.hostsField.GetText(); got != "web2" {
		t.Errorf("hostsField after Resume cascade = %q, want %q (the genuinely failed host, not empty)", got, "web2")
	}
}

func TestRerunFieldSyncUncheckingResumeClearsPlayAndOnlyFailed(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()
	r.resumeCheckbox.SetChecked(true)

	r.resumeCheckbox.SetChecked(false)

	if got := r.playField.GetText(); got != "" {
		t.Errorf("playField after unchecking Resume = %q, want empty", got)
	}
	if checked(r.onlyFailedCheckbox) {
		t.Error("unchecking Resume where failed should uncheck Only failed too")
	}
	if got := r.hostsField.GetText(); got != "" {
		t.Errorf("hostsField after unchecking Resume = %q, want empty (Only failed's own uncheck should have cleared it)", got)
	}
}

// TestRerunFieldSyncOnlyFailedUncheckAloneLeavesResumeChecked pins down
// Rerun.md's explicitly one-way coupling: unchecking "Only failed" by
// itself, after "Resume where failed" auto-checked it, must NOT uncheck
// Resume - only the reverse direction cascades.
func TestRerunFieldSyncOnlyFailedUncheckAloneLeavesResumeChecked(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()
	r.resumeCheckbox.SetChecked(true)

	r.onlyFailedCheckbox.SetChecked(false)

	if !checked(r.resumeCheckbox) {
		t.Error("unchecking Only failed by itself must not uncheck Resume where failed")
	}
}

func TestRerunFieldSyncOnlyFailedAndOnlyUnreachableUnion(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()

	r.onlyFailedCheckbox.SetChecked(true)
	if got := r.hostsField.GetText(); got != "web2" {
		t.Fatalf("hostsField after Only failed alone = %q, want %q", got, "web2")
	}

	r.onlyUnreachableCheckbox.SetChecked(true)
	if got := r.hostsField.GetText(); got != "ghost,web2" {
		t.Errorf("hostsField with both checked = %q, want the sorted union %q", got, "ghost,web2")
	}

	// Unchecking one of the two recomputes down to just the other, per
	// Rerun.md - not left over from the union.
	r.onlyFailedCheckbox.SetChecked(false)
	if got := r.hostsField.GetText(); got != "ghost" {
		t.Errorf("hostsField after unchecking Only failed = %q, want %q", got, "ghost")
	}
}

// TestRerunFieldSyncEditingHostsFieldByHandUnchecksBothCheckboxes covers
// the "don't fight the user's edit" direction: typing into Hosts after
// the checkboxes populated it must uncheck both, and must leave the
// user's own text alone rather than recomputing over it.
func TestRerunFieldSyncEditingHostsFieldByHandUnchecksBothCheckboxes(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()
	r.onlyFailedCheckbox.SetChecked(true)
	r.onlyUnreachableCheckbox.SetChecked(true)

	r.hostsField.SetText("manualhost") // simulates a real keystroke - not
	// r.setHostsFieldFromCheckboxes, which is exactly what a checkbox
	// cascade uses and is guarded against re-triggering here.

	if checked(r.onlyFailedCheckbox) || checked(r.onlyUnreachableCheckbox) {
		t.Error("editing Hosts by hand should uncheck both Only-failed and Only-unreachable")
	}
	if got := r.hostsField.GetText(); got != "manualhost" {
		t.Errorf("hostsField after hand-edit = %q, want the user's own text left alone", got)
	}
}

// TestRerunFieldSyncEditingPlayFieldByHandUnchecksResume mirrors the
// above for the Play field / Resume checkbox pairing.
func TestRerunFieldSyncEditingPlayFieldByHandUnchecksResume(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()
	r.resumeCheckbox.SetChecked(true)

	r.playField.SetText("some other play")

	if checked(r.resumeCheckbox) {
		t.Error("editing Play by hand should uncheck Resume where failed")
	}
	if got := r.playField.GetText(); got != "some other play" {
		t.Errorf("playField after hand-edit = %q, want the user's own text left alone", got)
	}
}

func TestRerunFieldSyncMatchHostsIncludesGroups(t *testing.T) {
	state := &playbook.PlaybookState{AllHosts: []string{"web1", "web2"}}
	form := tview.NewForm()
	r := newRerunFieldSync(form, state, nil, nil, []string{"webservers", "dbservers"}, runner.InitialRerunDefaults{})

	if got := r.matchHosts("web"); !slicesEqualUnordered(got, []string{"web1", "web2", "webservers"}) {
		t.Errorf(`matchHosts("web") = %v, want [web1 web2 webservers] in some order`, got)
	}
	if got := r.matchHosts("db"); !slicesEqualUnordered(got, []string{"dbservers"}) {
		t.Errorf(`matchHosts("db") = %v, want [dbservers]`, got)
	}
	// A literal host from live state.AllHosts is still found even with no
	// group data at all (nil knownGroups) - group support is additive, not
	// a replacement for the existing host-only behavior.
	r2 := newRerunFieldSync(form, state, nil, nil, nil, runner.InitialRerunDefaults{})
	if got := r2.matchHosts("web1"); !slicesEqualUnordered(got, []string{"web1"}) {
		t.Errorf(`matchHosts("web1") with no groups = %v, want [web1]`, got)
	}
}

// slicesEqualUnordered compares two string slices ignoring order - matchHosts'
// own candidate order (live hosts before groups) isn't a documented
// guarantee worth pinning a test to.
func slicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestRerunFieldSyncAutocompleteOpenNow(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.rebuild()
	flex := tview.NewFlex().AddItem(r.form, 0, 1, true)
	app := tview.NewApplication().SetRoot(flex, true) // not run; SetFocus below still updates HasFocus() synchronously

	if r.autocompleteOpenNow() {
		t.Error("no field focused yet - autocompleteOpenNow should be false")
	}

	r.hostsField.SetText("web")
	app.SetFocus(r.hostsField)
	if !r.autocompleteOpenNow() {
		t.Error("hostsField focused with a matching prefix (\"web\" -> web1/web2) should report a drop-down open")
	}

	r.acDismissed = true
	if r.autocompleteOpenNow() {
		t.Error("acDismissed should suppress autocompleteOpenNow even with real matches")
	}
}

func TestRerunFieldSyncResetDismissedOnTextChange(t *testing.T) {
	r := newTestRerunFieldSync(t)
	r.acDismissed = true

	r.tagsField.SetText("dep") // any real keystroke fires SetChangedFunc -> resetDismissed

	if r.acDismissed {
		t.Error("a real text change should reset acDismissed")
	}
}

func TestRerunFieldSyncInitialRerunDefaultsAppliedOnce(t *testing.T) {
	state := &playbook.PlaybookState{} // len(Plays) == 0: the "rerun" verb's very first dialog open
	form := tview.NewForm()
	r := newRerunFieldSync(form, state, nil, []string{"main play"}, nil, runner.InitialRerunDefaults{
		FailedHosts:            []string{"web2"},
		UnreachableHosts:       []string{"ghost"},
		ResumePlay:             "main play",
		CheckResumeWhereFailed: true,
	})

	r.rebuild()
	if !checked(r.resumeCheckbox) {
		t.Fatal("CheckResumeWhereFailed should pre-check Resume where failed on first rebuild")
	}

	// A later reopen (state now has real plays, simulating a generation
	// having actually run) must not re-apply the one-shot default even if
	// the user has since unchecked it by hand.
	r.resumeCheckbox.SetChecked(false)
	state.Plays = []*playbook.PlayNode{{Name: "main play", Tasks: []*playbook.TaskNode{{
		Name:  "t",
		Hosts: map[string]playbook.Outcome{"web2": playbook.OutcomeFailed},
	}}}}
	r.rebuild()
	if checked(r.resumeCheckbox) {
		t.Error("initialRerunDefaults must only ever apply once - a later rebuild must not re-check a box the user unchecked by hand")
	}
}
