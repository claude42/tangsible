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

package diff

import (
	"strings"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

func TestStripTags(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain color tag", "[green]OK[-]", "OK"},
		{"multi-part tag", "[red::u]web1[-::-]", "web1"},
		{"bold play row tag", "[white::b]myplay[-::-]", "myplay"},
		{"no tags at all", "plain text, no tags", "plain text, no tags"},
		{"escaped literal bracket survives", "tags[[]a, b]", "tags[[]a, b]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StripTags(c.in); got != c.want {
				t.Errorf("StripTags(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestDiffTwoTextsIdenticalAfterStrippingTags(t *testing.T) {
	// Same underlying text, different color only (e.g. outcome changed
	// from OK to Failed) - must NOT be reported as a difference; the
	// tree's own underline marking already surfaces outcome changes.
	a := "[green]Status: OK[-]"
	b := "[red]Status: OK[-]"
	if got := DiffTwoTexts(a, b); got != "" {
		t.Errorf("DiffTwoTexts() for color-only differing text = %q, want \"\" (no real content difference)", got)
	}
}

func TestDiffTwoTextsRealDifference(t *testing.T) {
	got := DiffTwoTexts("[aqua]hello v1[-]", "[aqua]hello v2[-]")
	if got == "" {
		t.Fatal("DiffTwoTexts() for genuinely different text = \"\", want a real diff")
	}
	if !strings.Contains(got, "[green]") || !strings.Contains(got, "[red]") {
		t.Errorf("DiffTwoTexts() = %q, want both a [green] (+) and [red] (-) line", got)
	}
	if !strings.Contains(got, "hello v1") || !strings.Contains(got, "hello v2") {
		t.Errorf("DiffTwoTexts() = %q, want both versions represented", got)
	}
}

func TestDecodeTaskHostResult(t *testing.T) {
	task := namedTask("t")
	task.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hi"})

	decoded, ok := DecodeTaskHostResult(task, "web1")
	if !ok || decoded["stdout"] != "hi" {
		t.Errorf("DecodeTaskHostResult() = (%+v, %v), want the decoded stdout field", decoded, ok)
	}

	if _, ok := DecodeTaskHostResult(task, "missing-host"); ok {
		t.Error("DecodeTaskHostResult() for a host with no Raw entry, ok = true, want false")
	}
}

func TestSingleRunTabsDropsDiffAndResolvedTabs(t *testing.T) {
	task := namedTask("t")
	task.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hello"})
	task.Hosts["web1"] = playbook.OutcomeOK

	names, contents := SingleRunTabs(task, "web1", nil, uikit.ResolvedRender{}, "")
	if len(names) != len(contents) {
		t.Fatalf("SingleRunTabs() names/contents length mismatch: %d vs %d", len(names), len(contents))
	}
	for _, n := range names {
		if n == "Diff" {
			t.Errorf("SingleRunTabs() included the Diff tab, want it dropped: %v", names)
		}
		if n == "Resolved" {
			t.Errorf("SingleRunTabs() included the Resolved tab, want it dropped: %v", names)
		}
	}
	found := false
	for _, n := range names {
		if n == "Task" {
			found = true
		}
	}
	if !found {
		t.Errorf("SingleRunTabs() = %v, want the Task tab still present", names)
	}
}

func TestBuildDiffOutputTabsUnmatchedFallsBackToSingleRun(t *testing.T) {
	task := namedTask("only in new run")
	task.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hi"})
	task.Hosts["web1"] = playbook.OutcomeOK
	task.HostOrder = []string{"web1"}

	names, contents := BuildDiffOutputTabs(TaskAlignment{NewTask: task}, "web1", nil, nil)
	if len(names) != len(contents) {
		t.Fatalf("BuildDiffOutputTabs() names/contents length mismatch: %d vs %d", len(names), len(contents))
	}
	for _, n := range names {
		if n == "Diff" {
			t.Errorf("BuildDiffOutputTabs() included the Diff tab for an unmatched task, want it dropped: %v", names)
		}
	}
	if got := taskTabContent(t, names, contents); !strings.Contains(got, "Task only present in the new run") {
		t.Errorf("Task tab = %q, want it to note the task is only present in the new run", got)
	}
}

// taskTabContent finds the "Task" tab's own content among BuildDiffOutputTabs'
// (or SingleRunTabs') parallel names/contents slices, failing the test if
// it's missing - every one of these tab builders always includes a Task tab.
func taskTabContent(t *testing.T, names, contents []string) string {
	t.Helper()
	for i, n := range names {
		if n == "Task" {
			return contents[i]
		}
	}
	t.Fatalf("no Task tab found among %v", names)
	return ""
}

// TestBuildDiffOutputTabsOldOnlyDoesNotPanic is a regression test for a
// crash reported live, reproducible every time: expanding an old-only
// (strikethrough) task and opening one of its hosts panicked with a nil
// pointer dereference - BuildDiffOutputTabs called TaskAction(a.NewTask,
// host) unconditionally, before ever checking a.NewTask == nil, and
// a.NewTask is nil for exactly this (OldTask-only) case. The earlier
// TestBuildDiffOutputTabsUnmatchedFallsBackToSingleRun only ever
// exercised the NewTask-only side of "unmatched," which is why it didn't
// already catch this.
func TestBuildDiffOutputTabsOldOnlyDoesNotPanic(t *testing.T) {
	task := namedTask("only in old run")
	task.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hi"})
	task.Hosts["web1"] = playbook.OutcomeOK
	task.HostOrder = []string{"web1"}

	names, contents := BuildDiffOutputTabs(TaskAlignment{OldTask: task}, "web1", nil, nil)
	if len(names) != len(contents) {
		t.Fatalf("BuildDiffOutputTabs() names/contents length mismatch: %d vs %d", len(names), len(contents))
	}
	for _, n := range names {
		if n == "Diff" {
			t.Errorf("BuildDiffOutputTabs() included the Diff tab for an unmatched task, want it dropped: %v", names)
		}
	}
	if got := taskTabContent(t, names, contents); !strings.Contains(got, "Task only present in the old run") {
		t.Errorf("Task tab = %q, want it to note the task is only present in the old run", got)
	}
}

// TestBuildDiffOutputTabsDecodeFailureFallbackOmitsUnmatchedNote is a
// regression test for a subtler mistake this fix could otherwise
// introduce: the !oldOK||!newOK decode-failure fallback in
// BuildDiffOutputTabs also calls SingleRunTabs, but the task genuinely
// exists on *both* sides there (it just failed to decode) - so unlike the
// true old-only/new-only cases above, no "only present in..." note should
// be shown; it would be actively wrong.
func TestBuildDiffOutputTabsDecodeFailureFallbackOmitsUnmatchedNote(t *testing.T) {
	oldTask := namedTask("t")
	newTask := namedTask("t")
	// Deliberately no Raw entry for "web1" on the old side, so
	// DecodeTaskHostResult reports !ok for it and BuildDiffOutputTabs takes
	// its decode-failure fallback branch (falling back to the new run's own
	// SingleRunTabs, which needs a real Raw entry on the new side to still
	// produce a Task tab at all).
	oldTask.Hosts["web1"] = playbook.OutcomeOK
	oldTask.HostOrder = []string{"web1"}
	newTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hi"})
	newTask.Hosts["web1"] = playbook.OutcomeOK
	newTask.HostOrder = []string{"web1"}

	names, contents := BuildDiffOutputTabs(TaskAlignment{OldTask: oldTask, NewTask: newTask}, "web1", nil, nil)
	if got := taskTabContent(t, names, contents); strings.Contains(got, "only present in") {
		t.Errorf("Task tab = %q, want no unmatched-task note - the task exists on both sides", got)
	}
}

func TestBuildDiffOutputTabsMatchedDiffsEachTab(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hello v1"})
	oldTask.Hosts["web1"] = playbook.OutcomeOK
	oldTask.HostOrder = []string{"web1"}

	newTask := namedTask("t")
	newTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hello v2"})
	newTask.Hosts["web1"] = playbook.OutcomeOK
	newTask.HostOrder = []string{"web1"}

	names, contents := BuildDiffOutputTabs(TaskAlignment{OldTask: oldTask, NewTask: newTask}, "web1", nil, nil)
	for _, n := range names {
		if n == "Diff" {
			t.Errorf("BuildDiffOutputTabs() included the Diff tab, want it dropped: %v", names)
		}
		if n == "Resolved" {
			t.Errorf("BuildDiffOutputTabs() included the Resolved tab, want it dropped for now: %v", names)
		}
	}
	foundOutput := false
	for i, n := range names {
		if n == "Output" {
			foundOutput = true
			if !strings.Contains(contents[i], "hello v1") || !strings.Contains(contents[i], "hello v2") {
				t.Errorf("Output tab diff = %q, want both versions represented", contents[i])
			}
		}
	}
	if !foundOutput {
		t.Errorf("BuildDiffOutputTabs() = %v, want an Output tab (stdout genuinely differs)", names)
	}
}

func TestDiffColorTag(t *testing.T) {
	if got := DiffColorTag(playbook.OutcomeOK, ""); got != "green" {
		t.Errorf("DiffColorTag(OK, \"\") = %q, want %q", got, "green")
	}
	if got := DiffColorTag(playbook.OutcomeFailed, "u"); got != "red::u" {
		t.Errorf("DiffColorTag(Failed, u) = %q, want %q", got, "red::u")
	}
}

func TestDiffTaskRowTextUnmatchedNewOnly(t *testing.T) {
	task := namedTask("only in new run")
	task.HostOrder = []string{"web1"}
	task.Hosts["web1"] = playbook.OutcomeOK

	got := DiffTaskRowText(TaskAlignment{NewTask: task}, 0, false)
	if !strings.Contains(got, "only in new run") {
		t.Errorf("DiffTaskRowText(new-only) = %q, want the task's own name", got)
	}
	if !strings.Contains(got, "::u]") {
		t.Errorf("DiffTaskRowText(new-only) = %q, want an underline (::u) mark on the whole line", got)
	}
	if strings.Contains(got, "::s]") || strings.Contains(got, "::si]") {
		t.Errorf("DiffTaskRowText(new-only) = %q, want no strikethrough mark", got)
	}
	if !strings.Contains(got, "(new only)") {
		t.Errorf("DiffTaskRowText(new-only) = %q, want the plain-text \"(new only)\" marker - a fallback that doesn't depend on any terminal attribute support (see design-docs/Diff.md's mosh note)", got)
	}
}

func TestDiffTaskRowTextUnmatchedOldOnly(t *testing.T) {
	task := namedTask("only in old run")
	task.HostOrder = []string{"web1"}
	task.Hosts["web1"] = playbook.OutcomeOK

	got := DiffTaskRowText(TaskAlignment{OldTask: task}, 0, false)
	if !strings.Contains(got, "only in old run") {
		t.Errorf("DiffTaskRowText(old-only) = %q, want the task's own name", got)
	}
	// "si" (strikethrough+italic), not "s" alone - italic joined
	// strikethrough after live testing found strikethrough alone doesn't
	// render at all over mosh (design-docs/Diff.md).
	if !strings.Contains(got, "::si]") {
		t.Errorf("DiffTaskRowText(old-only) = %q, want a strikethrough+italic (::si) mark on the whole line", got)
	}
	if strings.Contains(got, "::u]") {
		t.Errorf("DiffTaskRowText(old-only) = %q, want no underline mark", got)
	}
	if !strings.Contains(got, "(old only)") {
		t.Errorf("DiffTaskRowText(old-only) = %q, want the plain-text \"(old only)\" marker - a fallback that doesn't depend on any terminal attribute support (see design-docs/Diff.md's mosh note)", got)
	}
}

func TestDiffTaskRowTextMatchedOnlyDifferingHostsUnderlined(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1", "web2"}
	oldTask.Hosts["web1"] = playbook.OutcomeOK
	oldTask.Hosts["web2"] = playbook.OutcomeOK

	newTask := namedTask("t")
	newTask.HostOrder = []string{"web1", "web2"}
	newTask.Hosts["web1"] = playbook.OutcomeFailed // changed
	newTask.Hosts["web2"] = playbook.OutcomeOK     // unchanged

	got := DiffTaskRowText(TaskAlignment{OldTask: oldTask, NewTask: newTask}, 0, false)
	if !strings.Contains(got, "[red::u]web1[-::-]") {
		t.Errorf("DiffTaskRowText(matched) = %q, want web1 underlined red (it changed)", got)
	}
	if !strings.Contains(got, "[green]web2[-::-]") {
		t.Errorf("DiffTaskRowText(matched) = %q, want web2 plain green, not underlined (it didn't change)", got)
	}
	if strings.Contains(got, "green::u") {
		t.Errorf("DiffTaskRowText(matched) = %q, want web2 NOT underlined", got)
	}
}

func TestDiffTaskRowTextRendersFromNewOnMatch(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1"}
	oldTask.Hosts["web1"] = playbook.OutcomeFailed

	newTask := namedTask("t (new title irrelevant, name must match to align)")
	newTask.Name = "t" // matched alignment always shares a name
	newTask.HostOrder = []string{"web1"}
	newTask.Hosts["web1"] = playbook.OutcomeOK

	got := DiffTaskRowText(TaskAlignment{OldTask: oldTask, NewTask: newTask}, 0, false)
	if !strings.Contains(got, "green") || strings.Contains(got, "red") {
		t.Errorf("DiffTaskRowText(matched) = %q, want it rendered from the NEW task's own outcome (green), not the old one's (red)", got)
	}
}

// TestDiffTaskRowTextIndentedAndPaddedToSharedColumn is a regression test
// for a bug reported live: task rows had no indent of their own at all
// (landing flush with play rows, at column 0), and each task row's own
// host list started right after THAT row's own title - a different
// column per row - rather than a shared one, "the formatting is totally
// off."
func TestDiffTaskRowTextIndentedAndPaddedToSharedColumn(t *testing.T) {
	short := namedTask("short")
	short.HostOrder = []string{"web1"}
	short.Hosts["web1"] = playbook.OutcomeOK

	longName := "a much, much longer task title"
	long := namedTask(longName)
	long.HostOrder = []string{"web1"}
	long.Hosts["web1"] = playbook.OutcomeOK

	// DiffTaskDisplayWidth, not a plain len(longName) - both short and long
	// are unmatched (new-only) here, so both rows also carry UnmatchedMarker's
	// own " (new only)" suffix, which the shared column has to account for
	// too (mirroring what DiffTitleColWidth itself does in production).
	titleColWidth := DiffTaskDisplayWidth(TaskAlignment{NewTask: long})
	shortText := DiffTaskRowText(TaskAlignment{NewTask: short}, titleColWidth, false)
	longText := DiffTaskRowText(TaskAlignment{NewTask: long}, titleColWidth, false)

	if !strings.HasPrefix(shortText, uikit.TaskIndent) || !strings.HasPrefix(longText, uikit.TaskIndent) {
		t.Errorf("DiffTaskRowText() = %q / %q, want both to start with TaskIndent %q", shortText, longText, uikit.TaskIndent)
	}

	// Tag markup itself varies in length (embedded text differs), so
	// comparing raw string offsets wouldn't mean anything - StripTags
	// first to compare *visible* column position instead.
	shortHostAt := strings.Index(StripTags(shortText), "web1")
	longHostAt := strings.Index(StripTags(longText), "web1")
	if shortHostAt == -1 || longHostAt == -1 {
		t.Fatalf("didn't find the hostname in either row: %q / %q", shortText, longText)
	}
	if shortHostAt != longHostAt {
		t.Errorf("host list starts at visible column %d for the short title but %d for the long one (sharing titleColWidth=%d) - want them equal, not per-row", shortHostAt, longHostAt, titleColWidth)
	}
}

func TestDiffTitleColWidth(t *testing.T) {
	shortTask := namedTask("short")
	longTask := namedTask("a much longer task name")
	pa := PlayAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []TaskAlignment{
			{NewTask: shortTask},
			{NewTask: longTask},
		},
	}
	// Both tasks are unmatched (new-only), so both carry UnmatchedMarker's
	// own " (new only)" suffix - the widest display width, not the widest
	// bare title, is what DiffTitleColWidth needs to report.
	want := DiffTaskDisplayWidth(TaskAlignment{NewTask: longTask})
	if got := DiffTitleColWidth([]PlayAlignment{pa}); got != want {
		t.Errorf("DiffTitleColWidth() = %d, want %d (the longest differing task's own display width, marker included)", got, want)
	}
}

func TestFlattenDiffRowsOnlyShowsDifferingPlaysAndTasks(t *testing.T) {
	sameTask := namedTask("unchanged")
	sameTask.HostOrder = []string{"web1"}
	sameTask.Hosts["web1"] = playbook.OutcomeOK
	sameTaskNew := namedTask("unchanged")
	sameTaskNew.HostOrder = []string{"web1"}
	sameTaskNew.Hosts["web1"] = playbook.OutcomeOK

	changedOld := namedTask("changed")
	changedOld.HostOrder = []string{"web1"}
	changedOld.Hosts["web1"] = playbook.OutcomeOK
	changedNew := namedTask("changed")
	changedNew.HostOrder = []string{"web1"}
	changedNew.Hosts["web1"] = playbook.OutcomeFailed

	quietPlay := PlayAlignment{
		OldPlay: namedPlay("quiet"), NewPlay: namedPlay("quiet"),
		Tasks: []TaskAlignment{{OldTask: sameTask, NewTask: sameTaskNew}},
	}
	noisyPlay := PlayAlignment{
		OldPlay: namedPlay("noisy"), NewPlay: namedPlay("noisy"),
		Tasks: []TaskAlignment{
			{OldTask: sameTask, NewTask: sameTaskNew},  // unchanged, shouldn't show
			{OldTask: changedOld, NewTask: changedNew}, // changed, should show
		},
	}

	rows := FlattenDiffRows([]PlayAlignment{quietPlay, noisyPlay}, map[*playbook.TaskNode]bool{}, nil, func(TaskAlignment, string) {})

	var texts []string
	for _, r := range rows {
		texts = append(texts, r.Text)
	}
	joined := strings.Join(texts, "\n")
	if strings.Contains(joined, "quiet") {
		t.Errorf("FlattenDiffRows() included the play with no real differences:\n%s", joined)
	}
	if !strings.Contains(joined, "noisy") {
		t.Errorf("FlattenDiffRows() = %v, want the play with a real difference included", texts)
	}
	if strings.Count(joined, "unchanged") > 0 {
		t.Errorf("FlattenDiffRows() included the unchanged task within a differing play:\n%s", joined)
	}
	if !strings.Contains(joined, "changed") {
		t.Errorf("FlattenDiffRows() = %v, want the changed task included", texts)
	}
}

func TestFlattenDiffRowsExpandsHostRowsWhenToggled(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1"}
	oldTask.Hosts["web1"] = playbook.OutcomeOK
	newTask := namedTask("t")
	newTask.HostOrder = []string{"web1"}
	newTask.Hosts["web1"] = playbook.OutcomeFailed

	pa := PlayAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []TaskAlignment{{OldTask: oldTask, NewTask: newTask}},
	}

	collapsed := FlattenDiffRows([]PlayAlignment{pa}, map[*playbook.TaskNode]bool{}, nil, func(TaskAlignment, string) {})
	if len(collapsed) != 2 { // play row + task row, no host row
		t.Fatalf("FlattenDiffRows() collapsed = %d rows, want 2 (play, task)", len(collapsed))
	}

	expanded := map[*playbook.TaskNode]bool{newTask: true}
	got := FlattenDiffRows([]PlayAlignment{pa}, expanded, nil, func(TaskAlignment, string) {})
	if len(got) != 3 { // play row + task row + one host row
		t.Fatalf("FlattenDiffRows() expanded = %d rows, want 3 (play, task, host)", len(got))
	}
	if !strings.Contains(got[2].Text, "web1") {
		t.Errorf("FlattenDiffRows() expanded host row = %q, want it to mention web1", got[2].Text)
	}
}

// TestFlattenDiffRowsExpandsOnlyDifferingHostRows is a regression test for
// a bug reported live: expanding a matched, differing task showed a host
// row for every host the task ran on, even ones with no difference at all
// - burying the one host that actually changed among a pile of identical
// ones. Expanded rows should include only the hosts DifferingHosts flags;
// the collapsed row (DiffHostList/DiffTaskLine) is unaffected and
// deliberately keeps showing every host - see
// TestDiffTaskRowTextMatchedOnlyDifferingHostsUnderlined.
func TestFlattenDiffRowsExpandsOnlyDifferingHostRows(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1", "web2"}
	oldTask.Hosts["web1"] = playbook.OutcomeOK
	oldTask.Hosts["web2"] = playbook.OutcomeOK
	newTask := namedTask("t")
	newTask.HostOrder = []string{"web1", "web2"}
	newTask.Hosts["web1"] = playbook.OutcomeFailed // changed
	newTask.Hosts["web2"] = playbook.OutcomeOK     // unchanged

	pa := PlayAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []TaskAlignment{{OldTask: oldTask, NewTask: newTask}},
	}

	expanded := map[*playbook.TaskNode]bool{newTask: true}
	got := FlattenDiffRows([]PlayAlignment{pa}, expanded, nil, func(TaskAlignment, string) {})
	if len(got) != 3 { // play row + task row + one host row (web1 only)
		t.Fatalf("FlattenDiffRows() expanded = %d rows, want 3 (play, task, web1 only); got %#v", len(got), got)
	}
	if !strings.Contains(got[2].Text, "web1") {
		t.Errorf("FlattenDiffRows() expanded host row = %q, want it to mention web1", got[2].Text)
	}
	if strings.Contains(got[2].Text, "web2") {
		t.Errorf("FlattenDiffRows() expanded host row = %q, want web2 (unchanged) omitted", got[2].Text)
	}
}

// TestFlattenDiffRowsRendersTheSelectedRow is a regression test for a bug
// reported live: no row was ever rendered with its own selected styling,
// leaving the cursor completely invisible - TreeList (unlike tview.List)
// has no built-in "current row" look of its own, so FlattenDiffRows
// re-rendering exactly the row matching selectedID is the ENTIRE
// highlighting mechanism, the same way it is for the live tree's own
// FlattenRows.
func TestFlattenDiffRowsRendersTheSelectedRow(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1"}
	oldTask.Hosts["web1"] = playbook.OutcomeOK
	newTask := namedTask("t")
	newTask.HostOrder = []string{"web1"}
	newTask.Hosts["web1"] = playbook.OutcomeFailed

	pa := PlayAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []TaskAlignment{{OldTask: oldTask, NewTask: newTask}},
	}

	// Selecting the task row (id == newTask, per DiffTaskKey) must render
	// THAT row with the selected (PureBlack-on-lightgray) styling, and
	// leave the play row unselected.
	rows := FlattenDiffRows([]PlayAlignment{pa}, map[*playbook.TaskNode]bool{}, newTask, func(TaskAlignment, string) {})
	if len(rows) != 2 {
		t.Fatalf("FlattenDiffRows() = %d rows, want 2 (play, task)", len(rows))
	}
	if !strings.Contains(rows[1].Text, "lightgray") {
		t.Errorf("FlattenDiffRows() task row (selected) = %q, want the selected PureBlack:lightgray styling", rows[1].Text)
	}
	if strings.Contains(rows[0].Text, "lightgray") {
		t.Errorf("FlattenDiffRows() play row (not selected) = %q, want no selected styling", rows[0].Text)
	}

	// Selecting an expanded host row instead.
	expanded := map[*playbook.TaskNode]bool{newTask: true}
	hostID := DiffHostRowID{task: newTask, host: "web1"}
	rows = FlattenDiffRows([]PlayAlignment{pa}, expanded, hostID, func(TaskAlignment, string) {})
	if len(rows) != 3 {
		t.Fatalf("FlattenDiffRows() = %d rows, want 3 (play, task, host)", len(rows))
	}
	if !strings.Contains(rows[2].Text, "lightgray") {
		t.Errorf("FlattenDiffRows() host row (selected) = %q, want the selected PureBlack:lightgray styling", rows[2].Text)
	}
	if strings.Contains(rows[1].Text, "lightgray") {
		t.Errorf("FlattenDiffRows() task row (not selected, host is) = %q, want no selected styling", rows[1].Text)
	}
}

func TestDiffTaskKeyAndPlayName(t *testing.T) {
	oldT, newT := namedTask("t"), namedTask("t")
	if got := DiffTaskKey(TaskAlignment{OldTask: oldT, NewTask: newT}); got != newT {
		t.Error("DiffTaskKey() for a matched alignment should prefer NewTask")
	}
	if got := DiffTaskKey(TaskAlignment{OldTask: oldT}); got != oldT {
		t.Error("DiffTaskKey() for an old-only alignment should return OldTask")
	}

	oldP, newP := namedPlay("p"), namedPlay("p")
	if got := DiffPlayName(PlayAlignment{OldPlay: oldP, NewPlay: newP}); got != newP {
		t.Error("DiffPlayName() for a matched alignment should prefer NewPlay")
	}
	if got := DiffPlayName(PlayAlignment{OldPlay: oldP}); got != oldP {
		t.Error("DiffPlayName() for an old-only alignment should return OldPlay")
	}
}
