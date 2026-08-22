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

import "testing"

func TestNextInteractiveRow(t *testing.T) {
	interactive := func() {}
	rows := []row{
		{selected: interactive}, // 0
		{selected: interactive}, // 1
		{selected: nil},         // 2 - decorative (e.g. status divider)
		{selected: nil},         // 3 - decorative (e.g. status text)
		{selected: nil},         // 4 - decorative (recap heading)
		{selected: interactive}, // 5 - first recap host line
		{selected: interactive}, // 6
	}

	t.Run("skips a whole run of decorative rows going down", func(t *testing.T) {
		if got := nextInteractiveRow(rows, 1, 1); got != 5 {
			t.Errorf("nextInteractiveRow(1, +1) = %d, want 5", got)
		}
	})

	t.Run("skips the same run going up", func(t *testing.T) {
		if got := nextInteractiveRow(rows, 5, -1); got != 1 {
			t.Errorf("nextInteractiveRow(5, -1) = %d, want 1", got)
		}
	})

	t.Run("no wraparound past the last row", func(t *testing.T) {
		if got := nextInteractiveRow(rows, 6, 1); got != -1 {
			t.Errorf("nextInteractiveRow(6, +1) = %d, want -1", got)
		}
	})

	t.Run("no wraparound past the first row", func(t *testing.T) {
		if got := nextInteractiveRow(rows, 0, -1); got != -1 {
			t.Errorf("nextInteractiveRow(0, -1) = %d, want -1", got)
		}
	})

	t.Run("a normal one-row step when neighbors are both interactive", func(t *testing.T) {
		if got := nextInteractiveRow(rows, 0, 1); got != 1 {
			t.Errorf("nextInteractiveRow(0, +1) = %d, want 1", got)
		}
	})
}
