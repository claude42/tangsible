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
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
)

func TestInheritedExpandState(t *testing.T) {
	taskA := &playbook.TaskNode{Name: "task A"}
	taskB := &playbook.TaskNode{Name: "task B"}
	taskC := &playbook.TaskNode{Name: "task C"}

	t.Run("very first task of a generation falls back to startExpanded", func(t *testing.T) {
		for _, startExpanded := range []bool{true, false} {
			got := InheritedExpandState([]*playbook.TaskNode{taskA}, map[*playbook.TaskNode]bool{}, nil, startExpanded)
			if got != startExpanded {
				t.Errorf("inheritedExpandState(single task, startExpanded=%v) = %v, want %v", startExpanded, got, startExpanded)
			}
		}
	})

	t.Run("inherits the previously-added task's current state - expanded", func(t *testing.T) {
		expanded := map[*playbook.TaskNode]bool{taskA: true}
		got := InheritedExpandState([]*playbook.TaskNode{taskA, taskB}, expanded, nil, false)
		if !got {
			t.Error("inheritedExpandState() = false, want true (inherited from taskA)")
		}
	})

	t.Run("inherits the previously-added task's current state - collapsed", func(t *testing.T) {
		expanded := map[*playbook.TaskNode]bool{taskA: false}
		got := InheritedExpandState([]*playbook.TaskNode{taskA, taskB}, expanded, nil, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (inherited from taskA)")
		}
	})

	t.Run("missing map entry for the previous task means collapsed, same as any Go zero value", func(t *testing.T) {
		got := InheritedExpandState([]*playbook.TaskNode{taskA, taskB}, map[*playbook.TaskNode]bool{}, nil, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (taskA has no explicit entry)")
		}
	})

	t.Run("looks at the second-to-last task, not the first", func(t *testing.T) {
		// taskC is the new task itself (already the last element, per
		// OnTaskAdded's own contract); taskB is "the task added right
		// before it" - taskA's own state must be ignored here.
		expanded := map[*playbook.TaskNode]bool{taskA: true, taskB: false}
		got := InheritedExpandState([]*playbook.TaskNode{taskA, taskB, taskC}, expanded, nil, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (should inherit from taskB, the second-to-last, not taskA)")
		}
	})
}

func TestInheritedExpandStateIgnoresAutomaticExpansion(t *testing.T) {
	taskA := &playbook.TaskNode{Name: "debug task"}
	taskB := &playbook.TaskNode{Name: "next"}
	all := []*playbook.TaskNode{taskA, taskB}
	expanded := map[*playbook.TaskNode]bool{taskA: true}

	if got := InheritedExpandState(all, expanded, map[*playbook.TaskNode]bool{taskA: true}, false); got {
		t.Error("a task tangsible expanded must not make its successor expanded")
	}
	// E/C handed the expansion over to the user
	if got := InheritedExpandState(all, expanded, map[*playbook.TaskNode]bool{taskA: false}, false); !got {
		t.Error("an expansion the user owns is inherited as before")
	}
	// the user collapsed it: nothing to inherit either way
	if got := InheritedExpandState(all, map[*playbook.TaskNode]bool{taskA: false}, map[*playbook.TaskNode]bool{taskA: true}, true); got {
		t.Error("collapsed by the user")
	}
}
