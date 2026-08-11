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
	"fmt"
	"io"
)

// Render prints a plain-text dump of the current play/task/host tree. This
// is not the TUI — it always shows every host line under every task, since
// there's no navigation/expand state yet to decide what to hide.
func Render(w io.Writer, s *playbookState) {
	for _, play := range s.Plays {
		fmt.Fprintf(w, "PLAY: %s\n", play.Name)
		for _, task := range play.Tasks {
			ok, changed, skipped, failed, unreachable := task.counts()
			fmt.Fprintf(w, "  TASK: %-40s OK: %d, Chgd: %d, Skip: %d, Fail: %d, Unrch: %d\n",
				task.Name, ok, changed, skipped, failed, unreachable)
			for _, host := range task.HostOrder {
				fmt.Fprintf(w, "    %s: %s\n", host, task.Hosts[host])
			}
		}
	}
}
