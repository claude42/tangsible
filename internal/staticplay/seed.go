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

package staticplay

import (
	"encoding/json"
	"time"

	"code.aw.net/claude/tangsible/internal/playbook"
)

// View carries what a view session knows about its seeded tree beyond what
// playbook.TaskNode holds: each task's static facts and each play's host
// pattern. Keyed by node identity, like the session's other per-node maps.
type View struct {
	Tasks map[*playbook.TaskNode]Task
	Hosts map[*playbook.PlayNode]string
}

// Seed builds the same play -> task tree a run builds from events, from the
// static playbook: no hosts, no results, every task with its source path so
// the Task definition tab finds its text. Plays without tasks are kept in
// the state but the tree shows none of them (as for a run).
func Seed(pb Playbook) (*playbook.PlaybookState, *View) {
	state := &playbook.PlaybookState{}
	v := &View{Tasks: map[*playbook.TaskNode]Task{}, Hosts: map[*playbook.PlayNode]string{}}
	for _, p := range pb.Plays {
		play := &playbook.PlayNode{Name: p.Name}
		state.Plays = append(state.Plays, play)
		v.Hosts[play] = p.Hosts
		for _, t := range p.Tasks {
			node := &playbook.TaskNode{
				Name:     t.Name,
				Path:     t.Location(),
				Hosts:    map[string]playbook.Outcome{},
				Raw:      map[string]json.RawMessage{},
				Ignored:  map[string]bool{},
				InFlight: map[string]bool{},
				Started:  map[string]time.Time{},
			}
			play.Tasks = append(play.Tasks, node)
			v.Tasks[node] = t
		}
	}
	return state, v
}

// PlayOf returns the play that owns task in state, nil if none does.
func PlayOf(state *playbook.PlaybookState, task *playbook.TaskNode) *playbook.PlayNode {
	for _, p := range state.Plays {
		for _, t := range p.Tasks {
			if t == task {
				return p
			}
		}
	}
	return nil
}
