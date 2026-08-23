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
	"encoding/json"
	"testing"
)

func namedTask(name string) *taskNode {
	return &taskNode{Name: name, Hosts: map[string]outcome{}, Raw: map[string]json.RawMessage{}}
}

func namedPlay(name string, tasks ...*taskNode) *playNode {
	return &playNode{Name: name, Tasks: tasks}
}

func TestAlignTasksIdenticalSequence(t *testing.T) {
	a, b, c := namedTask("a"), namedTask("b"), namedTask("c")
	oldPlay := namedPlay("play", a, b, c)
	a2, b2, c2 := namedTask("a"), namedTask("b"), namedTask("c")
	newPlay := namedPlay("play", a2, b2, c2)

	got := alignTasks(oldPlay, newPlay)
	if len(got) != 3 {
		t.Fatalf("alignTasks() = %d alignments, want 3", len(got))
	}
	want := []taskAlignment{{OldTask: a, NewTask: a2}, {OldTask: b, NewTask: b2}, {OldTask: c, NewTask: c2}}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("alignTasks()[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestAlignTasksInsertedTask(t *testing.T) {
	a, c := namedTask("a"), namedTask("c")
	oldPlay := namedPlay("play", a, c)
	a2, bNew, c2 := namedTask("a"), namedTask("b"), namedTask("c")
	newPlay := namedPlay("play", a2, bNew, c2)

	got := alignTasks(oldPlay, newPlay)
	want := []taskAlignment{
		{OldTask: a, NewTask: a2},
		{NewTask: bNew},
		{OldTask: c, NewTask: c2},
	}
	if len(got) != len(want) {
		t.Fatalf("alignTasks() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("alignTasks()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAlignTasksDeletedTask(t *testing.T) {
	a, bOld, c := namedTask("a"), namedTask("b"), namedTask("c")
	oldPlay := namedPlay("play", a, bOld, c)
	a2, c2 := namedTask("a"), namedTask("c")
	newPlay := namedPlay("play", a2, c2)

	got := alignTasks(oldPlay, newPlay)
	want := []taskAlignment{
		{OldTask: a, NewTask: a2},
		{OldTask: bOld},
		{OldTask: c, NewTask: c2},
	}
	if len(got) != len(want) {
		t.Fatalf("alignTasks() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("alignTasks()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestAlignTasksReplaceIsNotForcedIntoAPairing(t *testing.T) {
	// A same-position "replace" (different names at the same spot) must
	// become an old-only entry plus a new-only entry, never a matched
	// pair claiming two differently-named tasks are "the same task."
	oldOnly := namedTask("old name")
	newOnly := namedTask("new name")
	oldPlay := namedPlay("play", oldOnly)
	newPlay := namedPlay("play", newOnly)

	got := alignTasks(oldPlay, newPlay)
	if len(got) != 2 {
		t.Fatalf("alignTasks() = %+v, want 2 unmatched alignments", got)
	}
	foundOld, foundNew := false, false
	for _, a := range got {
		if a.OldTask == oldOnly && a.NewTask == nil {
			foundOld = true
		}
		if a.NewTask == newOnly && a.OldTask == nil {
			foundNew = true
		}
	}
	if !foundOld || !foundNew {
		t.Errorf("alignTasks() = %+v, want one old-only and one new-only entry, never paired", got)
	}
}

func TestAlignTasksNilPlay(t *testing.T) {
	a, b := namedTask("a"), namedTask("b")
	newPlay := namedPlay("play", a, b)

	got := alignTasks(nil, newPlay)
	if len(got) != 2 {
		t.Fatalf("alignTasks(nil, newPlay) = %+v, want 2 new-only alignments", got)
	}
	for _, al := range got {
		if al.OldTask != nil || al.NewTask == nil {
			t.Errorf("alignTasks(nil, newPlay) entry = %+v, want OldTask nil, NewTask set", al)
		}
	}

	oldPlay := namedPlay("play", a, b)
	got = alignTasks(oldPlay, nil)
	if len(got) != 2 {
		t.Fatalf("alignTasks(oldPlay, nil) = %+v, want 2 old-only alignments", got)
	}
	for _, al := range got {
		if al.NewTask != nil || al.OldTask == nil {
			t.Errorf("alignTasks(oldPlay, nil) entry = %+v, want NewTask nil, OldTask set", al)
		}
	}
}

func TestAlignPlaysAddedRemovedPlay(t *testing.T) {
	oldState := &playbookState{Plays: []*playNode{namedPlay("shared", namedTask("t"))}}
	newState := &playbookState{Plays: []*playNode{
		namedPlay("shared", namedTask("t")),
		namedPlay("brand new play", namedTask("only task")),
	}}

	got := alignPlays(oldState, newState)
	if len(got) != 2 {
		t.Fatalf("alignPlays() = %d alignments, want 2 (shared + new)", len(got))
	}
	shared, newOnly := got[0], got[1]
	if shared.OldPlay == nil || shared.NewPlay == nil {
		t.Errorf("shared play alignment = %+v, want both sides set", shared)
	}
	if newOnly.OldPlay != nil || newOnly.NewPlay == nil {
		t.Errorf("new-only play alignment = %+v, want only NewPlay set", newOnly)
	}
	// The new-only play's own task must itself be a new-only (unmatched)
	// alignment - this is what makes the whole play "contain a
	// difference" with no special-casing (see playAlignmentHasDifferences).
	if len(newOnly.Tasks) != 1 || newOnly.Tasks[0].OldTask != nil || newOnly.Tasks[0].NewTask == nil {
		t.Errorf("new-only play's own Tasks = %+v, want one new-only task alignment", newOnly.Tasks)
	}
}

func rawJSON(t *testing.T, v map[string]interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

func TestTaskDiffersUnmatchedAlwaysDiffers(t *testing.T) {
	if !taskDiffers(taskAlignment{NewTask: namedTask("only new")}) {
		t.Error("taskDiffers() for a new-only alignment = false, want true")
	}
	if !taskDiffers(taskAlignment{OldTask: namedTask("only old")}) {
		t.Error("taskDiffers() for an old-only alignment = false, want true")
	}
}

func TestTaskDiffersOutcome(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.Hosts["web1"] = outcomeOK
	newTask := namedTask("t")
	newTask.Hosts["web1"] = outcomeFailed

	if !taskDiffers(taskAlignment{OldTask: oldTask, NewTask: newTask}) {
		t.Error("taskDiffers() with a changed outcome = false, want true")
	}
}

func TestTaskDiffersOutput(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.Hosts["web1"] = outcomeOK
	oldTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hello v1"})
	newTask := namedTask("t")
	newTask.Hosts["web1"] = outcomeOK
	newTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "hello v2"})

	if !taskDiffers(taskAlignment{OldTask: oldTask, NewTask: newTask}) {
		t.Error("taskDiffers() with a changed stdout, same outcome = false, want true")
	}
}

func TestTaskDiffersIdentical(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.Hosts["web1"] = outcomeOK
	oldTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "same"})
	newTask := namedTask("t")
	newTask.Hosts["web1"] = outcomeOK
	newTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stdout": "same"})

	if taskDiffers(taskAlignment{OldTask: oldTask, NewTask: newTask}) {
		t.Error("taskDiffers() for identical outcome and output = true, want false")
	}
}

func TestTaskDiffersHostOnlyOnOneSideIsIgnored(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.Hosts["web1"] = outcomeOK
	newTask := namedTask("t")
	newTask.Hosts["web2"] = outcomeFailed // a completely different host set

	if taskDiffers(taskAlignment{OldTask: oldTask, NewTask: newTask}) {
		t.Error("taskDiffers() with disjoint host sets = true, want false (host-set differences don't count)")
	}
}

func TestHostOutputDiffersStderrAndWarnings(t *testing.T) {
	oldTask := namedTask("t")
	oldTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stderr": "err v1"})
	newTask := namedTask("t")
	newTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"stderr": "err v2"})
	if !hostOutputDiffers(oldTask, newTask, "web1") {
		t.Error("hostOutputDiffers() with changed stderr = false, want true")
	}

	oldTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"warnings": []string{"a"}})
	newTask.Raw["web1"] = rawJSON(t, map[string]interface{}{"warnings": []string{"a", "b"}})
	if !hostOutputDiffers(oldTask, newTask, "web1") {
		t.Error("hostOutputDiffers() with changed warnings = false, want true")
	}
}

func TestPlayAlignmentHasDifferences(t *testing.T) {
	same := namedTask("same")
	same.Hosts["web1"] = outcomeOK
	same2 := namedTask("same")
	same2.Hosts["web1"] = outcomeOK
	noDiff := playAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []taskAlignment{{OldTask: same, NewTask: same2}},
	}
	if playAlignmentHasDifferences(noDiff) {
		t.Error("playAlignmentHasDifferences() with an identical matched task = true, want false")
	}

	withDiff := playAlignment{
		OldPlay: namedPlay("p"), NewPlay: namedPlay("p"),
		Tasks: []taskAlignment{{OldTask: same, NewTask: same2}, {NewTask: namedTask("added")}},
	}
	if !playAlignmentHasDifferences(withDiff) {
		t.Error("playAlignmentHasDifferences() with an unmatched added task = false, want true")
	}
}
