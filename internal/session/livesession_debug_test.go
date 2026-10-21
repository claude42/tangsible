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
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"code.aw.net/claude/tangsible/internal/debugrun"
)

// requireAnsiblePlaybook skips the test if ansible-playbook isn't on
// PATH - same pattern internal/debugrun's own session_test.go already
// uses for a real-binary dependency.
func requireAnsiblePlaybook(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ansible-playbook"); err != nil {
		t.Skip("ansible-playbook not found in PATH, skipping live debug-session test")
	}
}

// TestMergeRegisteredResult_Live spawns a real debug session (a single
// command task with its own register:) and confirms
// mergeRegisteredResult pulls that same task's own register: output into
// the task_vars snapshot - the exact gap reported live: failed_when:/
// changed_when: referencing a register target (e.g. result.rc) can't
// resolve from task_vars alone, since task_vars is a pre-task snapshot
// that never reflects this same task's own effects.
func TestMergeRegisteredResult_Live(t *testing.T) {
	requireAnsiblePlaybook(t)

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGSIBLE_CALLBACK_DIR", filepath.Join(repoRoot, "callback"))

	proj := t.TempDir()
	playbookPath := filepath.Join(proj, "playbook.yml")
	playbook := `- hosts: all
  gather_facts: false
  tasks:
    - name: run something
      ansible.builtin.command: /bin/true
      register: result
      failed_when: result.rc not in [0, 1, 2]
      debugger: always
`
	if err := os.WriteFile(playbookPath, []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := debugrun.Spawn(playbookPath, []string{"-i", "localhost,", "-c", "local"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer sess.Close()

	pause, eof, err := sess.Start()
	if err != nil || eof {
		t.Fatalf("Start: pause=%+v eof=%v err=%v", pause, eof, err)
	}

	raw, err := sess.Query("task_vars")
	if err != nil {
		t.Fatalf("Query(task_vars): %v", err)
	}
	var before map[string]json.RawMessage
	if err := json.Unmarshal(raw, &before); err != nil {
		t.Fatal(err)
	}
	if _, ok := before["result"]; ok {
		t.Fatalf("task_vars already has result before merge - test fixture assumption broken: %s", raw)
	}

	merged := mergeRegisteredResult(raw, sess)
	var after map[string]json.RawMessage
	if err := json.Unmarshal(merged, &after); err != nil {
		t.Fatalf("mergeRegisteredResult produced unparseable JSON: %v", err)
	}
	resultRaw, ok := after["result"]
	if !ok {
		t.Fatalf("mergeRegisteredResult didn't add result: %s", merged)
	}
	var result struct {
		RC int `json:"rc"`
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		t.Fatalf("result isn't the expected shape: %v (%s)", err, resultRaw)
	}
	if result.RC != 0 {
		t.Errorf("result.rc = %d, want 0", result.RC)
	}
}

// TestDebugUnfakeShadowPaths covers the rewrite that keeps the shadow
// copy's own location from leaking into the watch explorer/Resolved tab
// as a confusing path (design-docs/DebugMode.md's "faked paths" follow-
// up) - pure enough (just two liveSession fields, no real debug session
// needed) to table-drive directly, unlike TestMergeRegisteredResult_Live
// above.
func TestDebugUnfakeShadowPaths(t *testing.T) {
	tests := []struct {
		name       string
		copyDir    string
		playbook   string
		raw        string
		wantResult string
	}{
		{
			name:       "rewrites every occurrence",
			copyDir:    "/proj/.tangsible/debug/run1",
			playbook:   "/proj",
			raw:        `{"role_path":"/proj/.tangsible/debug/run1/roles/foo","playbook_dir":"/proj/.tangsible/debug/run1"}`,
			wantResult: `{"role_path":"/proj/roles/foo","playbook_dir":"/proj"}`,
		},
		{
			name:       "no occurrence is a no-op",
			copyDir:    "/proj/.tangsible/debug/run1",
			playbook:   "/proj",
			raw:        `{"some_var":"unrelated value"}`,
			wantResult: `{"some_var":"unrelated value"}`,
		},
		{
			name:       "empty copyDir/playbookDir (non-debug Verb shape) is a no-op",
			copyDir:    "",
			playbook:   "",
			raw:        `{"role_path":"/proj/.tangsible/debug/run1/roles/foo"}`,
			wantResult: `{"role_path":"/proj/.tangsible/debug/run1/roles/foo"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &liveSession{debugCopyDir: tt.copyDir, debugPlaybookDir: tt.playbook}
			got := s.debugUnfakeShadowPaths(json.RawMessage(tt.raw))
			if string(got) != tt.wantResult {
				t.Errorf("debugUnfakeShadowPaths(%s) = %s, want %s", tt.raw, got, tt.wantResult)
			}
		})
	}
}
