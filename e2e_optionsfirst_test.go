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

//go:build e2e

// End-to-end test of design-docs/ImplicitRun.md's "Options before the
// playbook": options typed before the playbook must neither make tangsible
// run the playbook twice nor fall back to the default playbook. Needs tmux
// and ansible-playbook (see e2e_rerun_test.go).
package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestE2E_OptionsBeforePlaybook(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	playbook := filepath.Join(repoRoot(t), "testdata", "outcomes.yml")

	cases := []struct {
		name string
		args string
	}{
		{"value flag and switch", fmt.Sprintf("-i localhost, --no-ee %s", shellQuote(playbook))},
		{"after double dash", fmt.Sprintf("-i localhost, -- %s", shellQuote(playbook))},
		{"playbook first", fmt.Sprintf("%s -i localhost,", shellQuote(playbook))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := startTmuxSession(t)
			// An empty directory: with no site.yml there, falling back to the
			// default playbook would be a usage error, not a run.
			s.send(fmt.Sprintf("cd %s && %s %s run %s", shellQuote(t.TempDir()), e2eCallbackEnv(t), shellQuote(bin), c.args), "Enter")
			got := s.waitFor("Completed 5 tasks", 20*time.Second)
			if n := strings.Count(got, "ok task"); n != 1 {
				t.Errorf("task \"ok task\" shown %d times, want once (playbook run twice?):\n%s", n, got)
			}
		})
	}
}
