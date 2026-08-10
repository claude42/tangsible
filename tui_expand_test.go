package main

import "testing"

func TestInheritedExpandState(t *testing.T) {
	taskA := &taskNode{Name: "task A"}
	taskB := &taskNode{Name: "task B"}
	taskC := &taskNode{Name: "task C"}

	t.Run("very first task of a generation falls back to startExpanded", func(t *testing.T) {
		for _, startExpanded := range []bool{true, false} {
			got := inheritedExpandState([]*taskNode{taskA}, map[*taskNode]bool{}, startExpanded)
			if got != startExpanded {
				t.Errorf("inheritedExpandState(single task, startExpanded=%v) = %v, want %v", startExpanded, got, startExpanded)
			}
		}
	})

	t.Run("inherits the previously-added task's current state - expanded", func(t *testing.T) {
		expanded := map[*taskNode]bool{taskA: true}
		got := inheritedExpandState([]*taskNode{taskA, taskB}, expanded, false)
		if !got {
			t.Error("inheritedExpandState() = false, want true (inherited from taskA)")
		}
	})

	t.Run("inherits the previously-added task's current state - collapsed", func(t *testing.T) {
		expanded := map[*taskNode]bool{taskA: false}
		got := inheritedExpandState([]*taskNode{taskA, taskB}, expanded, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (inherited from taskA)")
		}
	})

	t.Run("missing map entry for the previous task means collapsed, same as any Go zero value", func(t *testing.T) {
		got := inheritedExpandState([]*taskNode{taskA, taskB}, map[*taskNode]bool{}, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (taskA has no explicit entry)")
		}
	})

	t.Run("looks at the second-to-last task, not the first", func(t *testing.T) {
		// taskC is the new task itself (already the last element, per
		// OnTaskAdded's own contract); taskB is "the task added right
		// before it" - taskA's own state must be ignored here.
		expanded := map[*taskNode]bool{taskA: true, taskB: false}
		got := inheritedExpandState([]*taskNode{taskA, taskB, taskC}, expanded, true)
		if got {
			t.Error("inheritedExpandState() = true, want false (should inherit from taskB, the second-to-last, not taskA)")
		}
	})
}
