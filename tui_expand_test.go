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
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
)

func TestInheritedExpandState(t *testing.T) {
	taskA := &playbook.TaskNode{Name: "task A"}
	taskB := &playbook.TaskNode{Name: "task B"}
	taskC := &playbook.TaskNode{Name: "task C"}

	t.Run("very first task of a generation falls back to startExpanded", func(t *testing.T) {
		for _, startExpanded := range []bool{true, false} {
			got := inheritedExpandState([]*playbook.TaskNode{taskA}, map[*playbook.TaskNode]bool{}, startExpanded)
			if got != startExpanded {
				t.Errorf("inheritedExpandState(single task, startExpanded=%v) = %v, want %v", startExpanded, got, startExpanded)
			}
		}
	})

	t.Run("inherits the previously-added task's current state - expanded", func(t *testing.T) {
		expanded := map[*playbook.TaskNode]bool{taskA: true}
		got := inheritedExpandState([]*playbook.TaskNode{taskA, taskB}, expanded, false)
		if !got {
			t.Error("inheritedExpandState() = false, want true (inherited from taskA)")
		}
	})

	t.Run("inherits the previously-added task's current state - collapsed", func(t *testing.T) {
		expanded := map[*playbook.TaskNode]bool{taskA: false}
		got := inheritedExpandState([]*playbook.TaskNode{taskA, taskB}, expanded, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (inherited from taskA)")
		}
	})

	t.Run("missing map entry for the previous task means collapsed, same as any Go zero value", func(t *testing.T) {
		got := inheritedExpandState([]*playbook.TaskNode{taskA, taskB}, map[*playbook.TaskNode]bool{}, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (taskA has no explicit entry)")
		}
	})

	t.Run("looks at the second-to-last task, not the first", func(t *testing.T) {
		// taskC is the new task itself (already the last element, per
		// OnTaskAdded's own contract); taskB is "the task added right
		// before it" - taskA's own state must be ignored here.
		expanded := map[*playbook.TaskNode]bool{taskA: true, taskB: false}
		got := inheritedExpandState([]*playbook.TaskNode{taskA, taskB, taskC}, expanded, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (should inherit from taskB, the second-to-last, not taskA)")
		}
	})
}
