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
	"strings"
	"testing"
)

func TestDiffColorTag(t *testing.T) {
	if got := diffColorTag(outcomeOK, ""); got != "green" {
		t.Errorf("diffColorTag(OK, \"\") = %q, want %q", got, "green")
	}
	if got := diffColorTag(outcomeFailed, "u"); got != "red::u" {
		t.Errorf("diffColorTag(Failed, u) = %q, want %q", got, "red::u")
	}
}

func TestDiffTaskRowTextUnmatchedNewOnly(t *testing.T) {
	task := namedTask("only in new run")
	task.HostOrder = []string{"web1"}
	task.Hosts["web1"] = outcomeOK

	got := diffTaskRowText(taskAlignment{NewTask: task}, false)
	if !strings.Contains(got, "only in new run") {
		t.Errorf("diffTaskRowText(new-only) = %q, want the task's own name", got)
	}
	if !strings.Contains(got, "::u]") {
		t.Errorf("diffTaskRowText(new-only) = %q, want an underline (::u) mark on the whole line", got)
	}
	if strings.Contains(got, "::s]") {
		t.Errorf("diffTaskRowText(new-only) = %q, want no strikethrough mark", got)
	}
}

func TestDiffTaskRowTextUnmatchedOldOnly(t *testing.T) {
	task := namedTask("only in old run")
	task.HostOrder = []string{"web1"}
	task.Hosts["web1"] = outcomeOK

	got := diffTaskRowText(taskAlignment{OldTask: task}, false)
	if !strings.Contains(got, "only in old run") {
		t.Errorf("diffTaskRowText(old-only) = %q, want the task's own name", got)
	}
	if !strings.Contains(got, "::s]") {
		t.Errorf("diffTaskRowText(old-only) = %q, want a strikethrough (::s) mark on the whole line", got)
	}
	if strings.Contains(got, "::u]") {
		t.Errorf("diffTaskRowText(old-only) = %q, want no underline mark", got)
	}
}

func TestDiffTaskRowTextMatchedOnlyDifferingHostsUnderlined(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1", "web2"}
	oldTask.Hosts["web1"] = outcomeOK
	oldTask.Hosts["web2"] = outcomeOK

	newTask := namedTask("t")
	newTask.HostOrder = []string{"web1", "web2"}
	newTask.Hosts["web1"] = outcomeFailed // changed
	newTask.Hosts["web2"] = outcomeOK     // unchanged

	got := diffTaskRowText(taskAlignment{OldTask: oldTask, NewTask: newTask}, false)
	if !strings.Contains(got, "[red::u]web1[-::-]") {
		t.Errorf("diffTaskRowText(matched) = %q, want web1 underlined red (it changed)", got)
	}
	if !strings.Contains(got, "[green]web2[-::-]") {
		t.Errorf("diffTaskRowText(matched) = %q, want web2 plain green, not underlined (it didn't change)", got)
	}
	if strings.Contains(got, "green::u") {
		t.Errorf("diffTaskRowText(matched) = %q, want web2 NOT underlined", got)
	}
}

func TestDiffTaskRowTextRendersFromNewOnMatch(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1"}
	oldTask.Hosts["web1"] = outcomeFailed

	newTask := namedTask("t (new title irrelevant, name must match to align)")
	newTask.Name = "t" // matched alignment always shares a name
	newTask.HostOrder = []string{"web1"}
	newTask.Hosts["web1"] = outcomeOK

	got := diffTaskRowText(taskAlignment{OldTask: oldTask, NewTask: newTask}, false)
	if !strings.Contains(got, "green") || strings.Contains(got, "red") {
		t.Errorf("diffTaskRowText(matched) = %q, want it rendered from the NEW task's own outcome (green), not the old one's (red)", got)
	}
}

func TestFlattenDiffRowsOnlyShowsDifferingPlaysAndTasks(t *testing.T) {
	sameTask := namedTask("unchanged")
	sameTask.HostOrder = []string{"web1"}
	sameTask.Hosts["web1"] = outcomeOK
	sameTaskNew := namedTask("unchanged")
	sameTaskNew.HostOrder = []string{"web1"}
	sameTaskNew.Hosts["web1"] = outcomeOK

	changedOld := namedTask("changed")
	changedOld.HostOrder = []string{"web1"}
	changedOld.Hosts["web1"] = outcomeOK
	changedNew := namedTask("changed")
	changedNew.HostOrder = []string{"web1"}
	changedNew.Hosts["web1"] = outcomeFailed

	quietPlay := playAlignment{
		OldPlay: namedPlay("quiet"), NewPlay: namedPlay("quiet"),
		Tasks: []taskAlignment{{OldTask: sameTask, NewTask: sameTaskNew}},
	}
	noisyPlay := playAlignment{
		OldPlay: namedPlay("noisy"), NewPlay: namedPlay("noisy"),
		Tasks: []taskAlignment{
			{OldTask: sameTask, NewTask: sameTaskNew},  // unchanged, shouldn't show
			{OldTask: changedOld, NewTask: changedNew}, // changed, should show
		},
	}

	rows := flattenDiffRows([]playAlignment{quietPlay, noisyPlay}, map[*taskNode]bool{})

	var texts []string
	for _, r := range rows {
		texts = append(texts, r.text)
	}
	joined := strings.Join(texts, "\n")
	if strings.Contains(joined, "quiet") {
		t.Errorf("flattenDiffRows() included the play with no real differences:\n%s", joined)
	}
	if !strings.Contains(joined, "noisy") {
		t.Errorf("flattenDiffRows() = %v, want the play with a real difference included", texts)
	}
	if strings.Count(joined, "unchanged") > 0 {
		t.Errorf("flattenDiffRows() included the unchanged task within a differing play:\n%s", joined)
	}
	if !strings.Contains(joined, "changed") {
		t.Errorf("flattenDiffRows() = %v, want the changed task included", texts)
	}
}

func TestFlattenDiffRowsExpandsHostRowsWhenToggled(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.HostOrder = []string{"web1"}
	oldTask.Hosts["web1"] = outcomeOK
	newTask := namedTask("t")
	newTask.HostOrder = []string{"web1"}
	newTask.Hosts["web1"] = outcomeFailed

	pa := playAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []taskAlignment{{OldTask: oldTask, NewTask: newTask}},
	}

	collapsed := flattenDiffRows([]playAlignment{pa}, map[*taskNode]bool{})
	if len(collapsed) != 2 { // play row + task row, no host row
		t.Fatalf("flattenDiffRows() collapsed = %d rows, want 2 (play, task)", len(collapsed))
	}

	expanded := map[*taskNode]bool{newTask: true}
	got := flattenDiffRows([]playAlignment{pa}, expanded)
	if len(got) != 3 { // play row + task row + one host row
		t.Fatalf("flattenDiffRows() expanded = %d rows, want 3 (play, task, host)", len(got))
	}
	if !strings.Contains(got[2].text, "web1") {
		t.Errorf("flattenDiffRows() expanded host row = %q, want it to mention web1", got[2].text)
	}
}

func TestDiffTaskKeyAndPlayName(t *testing.T) {
	oldT, newT := namedTask("t"), namedTask("t")
	if got := diffTaskKey(taskAlignment{OldTask: oldT, NewTask: newT}); got != newT {
		t.Error("diffTaskKey() for a matched alignment should prefer NewTask")
	}
	if got := diffTaskKey(taskAlignment{OldTask: oldT}); got != oldT {
		t.Error("diffTaskKey() for an old-only alignment should return OldTask")
	}

	oldP, newP := namedPlay("p"), namedPlay("p")
	if got := diffPlayName(playAlignment{OldPlay: oldP, NewPlay: newP}); got != newP {
		t.Error("diffPlayName() for a matched alignment should prefer NewPlay")
	}
	if got := diffPlayName(playAlignment{OldPlay: oldP}); got != oldP {
		t.Error("diffPlayName() for an old-only alignment should return OldPlay")
	}
}
