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
	"encoding/json"
	"fmt"
	"testing"

	"code.aw.net/claude/tangsible/internal/playbook"
	"code.aw.net/claude/tangsible/internal/uikit"
)

// buildBenchState constructs a fully-completed state with nHosts hosts and
// nTasks tasks, each host's own result payload a realistic-size JSON blob
// (a handful of fields plus a moderately long stdout string, roughly what
// a real command/shell/setup module result looks like) - scratch, not kept
// as a real fixture.
func buildBenchState(nHosts, nTasks int) *playbook.PlaybookState {
	s := &playbook.PlaybookState{}
	s.Apply(playbook.RawEvent{Event: "v2_playbook_on_play_start", Play: &playbook.PlayRef{Name: "bench play"}})
	for t := 0; t < nTasks; t++ {
		s.Apply(playbook.RawEvent{Event: "v2_playbook_on_task_start", Task: &playbook.TaskRef{
			Name: fmt.Sprintf("task %d", t),
			Path: fmt.Sprintf("/bench.yml:%d", t+1),
		}})
		hosts := map[string]json.RawMessage{}
		for h := 0; h < nHosts; h++ {
			raw, _ := json.Marshal(map[string]interface{}{
				"changed": t%3 == 0,
				"rc":      0,
				"cmd":     []string{"echo", "hello"},
				"stdout":  fmt.Sprintf("line one\nline two\nline three for host %d task %d, padded padded padded padded padded padded", h, t),
				"stderr":  "",
				"start":   "2026-08-25 12:00:00.000000",
				"end":     "2026-08-25 12:00:00.100000",
				"delta":   "0:00:00.100000",
			})
			hosts[fmt.Sprintf("host%03d", h)] = raw
		}
		s.Apply(playbook.RawEvent{Event: "v2_runner_on_ok", Task: &playbook.TaskRef{Name: fmt.Sprintf("task %d", t)}, Hosts: hosts})
	}
	return s
}

func BenchmarkFlattenRecapRows(b *testing.B) {
	for _, n := range []int{5, 10, 20, 40} {
		state := buildBenchState(n, 30)
		hostExpanded := map[string]bool{}
		categoryExpanded := map[recapCategoryRowID]bool{}
		for _, h := range state.AllHosts {
			hostExpanded[h] = true
			for _, cat := range []string{"ok", "changed"} {
				categoryExpanded[recapCategoryRowID{host: h, label: cat}] = true
			}
		}
		noop := func(*playbook.TaskNode, string) {}
		b.Run(fmt.Sprintf("hosts=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = flattenRecapRows(state, hostExpanded, categoryExpanded, noop)
			}
		})
	}
}

func BenchmarkFlattenRowsTree(b *testing.B) {
	for _, n := range []int{5, 10, 20, 40} {
		state := buildBenchState(n, 30)
		expanded := map[*playbook.TaskNode]bool{}
		for _, play := range state.Plays {
			for _, t := range play.Tasks {
				expanded[t] = true
			}
		}
		layout := uikit.ComputeHostColumnLayout(state, state.AllHosts, 200, false)
		noop := func(*playbook.TaskNode, string) {}
		b.Run(fmt.Sprintf("hosts=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = uikit.FlattenRows(state, expanded, 200, layout, state.AllHosts, nil, ' ', uikit.FilterQuery{}, nil, noop, true)
			}
		})
	}
}

func BenchmarkHasWarningsAlone(b *testing.B) {
	raw := json.RawMessage(`{"changed":false,"rc":0,"cmd":["echo","hello"],"stdout":"line one\nline two\nline three for host 12 task 7, padded padded padded padded padded padded","stderr":"","start":"2026-08-25 12:00:00.000000","end":"2026-08-25 12:00:00.100000","delta":"0:00:00.100000"}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = uikit.HasWarnings(raw)
	}
}
