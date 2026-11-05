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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Password flags (internal/credfile) and debug's strategy: free handling.
// Every playbook appends a line to runs.log per execution, so "the re-run
// really ran" is checked on disk rather than guessed from a screen that looks
// the same before and after.

const e2eRunLogPlaybook = `- hosts: localhost
  gather_facts: false
  connection: local
  tasks:
    - name: record a run
      shell: echo run >> "{{ playbook_dir }}/runs.log"
`

// startRunSession starts "tangsible <verbArgs>" in a fresh work dir holding
// files, returning the tmux session, the work dir and the binary.
func startRunSession(t *testing.T, files map[string]string, verbArgs string) (*tmuxSession, string, string) {
	t.Helper()
	bin := buildE2EBinary(t)
	workDir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := startTmuxSession(t)
	s.send(fmt.Sprintf("cd %s && %s %s %s", shellQuote(workDir), e2eCallbackEnv(t), shellQuote(bin), verbArgs), "Enter")
	return s, workDir, bin
}

// rerunViaDialog opens the re-run dialog and confirms it unchanged.
func (s *tmuxSession) rerunViaDialog() {
	s.t.Helper()
	s.send("r")
	s.waitFor("Re-run (enter: run, esc: cancel, F1: help)", 5*time.Second)
	s.send("Tab", "Tab", "Tab", "Tab", "Tab", "Enter")
}

// waitForRuns waits until runs.log in dir has n lines.
func waitForRuns(t *testing.T, dir string, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(dir, "runs.log"))
		if got := strings.Count(string(b), "run\n"); got >= n {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d executions, runs.log has %d", n, got)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// quit leaves the TUI cleanly so the password files are removed.
func (s *tmuxSession) quitCleanly() {
	s.send("q")
	time.Sleep(time.Second)
}

// TestE2E_Run_AskBecomePassSurvivesARerun is the original hang: the re-run's
// second prompt printed over the TUI and nothing could answer it. It also
// checks that history records "-K", not a generated password-file path.
func TestE2E_Run_AskBecomePassSurvivesARerun(t *testing.T) {
	requireE2ETools(t)
	s, workDir, _ := startRunSession(t, map[string]string{"site.yml": e2eRunLogPlaybook}, "run site.yml -i localhost, -K --no-dialog")
	s.waitFor("BECOME password:", 15*time.Second)
	s.send("whatever", "Enter")
	s.waitFor("Playbook completed successfully", 15*time.Second)
	waitForRuns(t, workDir, 1)

	s.rerunViaDialog()
	waitForRuns(t, workDir, 2)
	time.Sleep(time.Second)
	if got := s.capture(); strings.Contains(got, "BECOME password:") {
		t.Errorf("the re-run asked for the become password again, under the TUI:\n%s", got)
	}
	s.quitCleanly()

	state, err := os.ReadFile(filepath.Join(workDir, ".tangsible", "state.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(state), "-K") || strings.Contains(string(state), "password-file") {
		t.Errorf("history should record the original -K, not a password file:\n%s", state)
	}
}

// TestE2E_Rerun_AskBecomePassPromptsAtStartup: "tangsible rerun" replays -K
// from history, so it asks again at its own startup (nothing is stored
// between processes) and then runs without a further prompt.
func TestE2E_Rerun_AskBecomePassPromptsAtStartup(t *testing.T) {
	requireE2ETools(t)
	s, workDir, bin := startRunSession(t, map[string]string{"site.yml": e2eRunLogPlaybook}, "run site.yml -i localhost, -K --no-dialog")
	s.waitFor("BECOME password:", 15*time.Second)
	s.send("whatever", "Enter")
	s.waitFor("Playbook completed successfully", 15*time.Second)
	waitForRuns(t, workDir, 1)
	s.quitCleanly()

	// Clear first, separately: the first run's prompt is still on screen and
	// would satisfy the wait below before the second one exists.
	s.send("clear", "Enter")
	s.waitForAbsence("BECOME password:", 5*time.Second)
	s.send(fmt.Sprintf("%s %s rerun --no-dialog", e2eCallbackEnv(t), shellQuote(bin)), "Enter")
	s.waitFor("BECOME password:", 15*time.Second)
	s.send("whatever", "Enter")
	waitForRuns(t, workDir, 2)
	s.quitCleanly()
}

// TestE2E_Run_BothAskFlagsPromptInOrder: -K and --ask-vault-pass each get
// their own prompt, in command-line order, and each secret reaches the right
// flag (the vault one decrypts, the become one is accepted).
func TestE2E_Run_BothAskFlagsPromptInOrder(t *testing.T) {
	requireE2ETools(t)
	s, workDir, _ := startRunSession(t, map[string]string{
		"secret.yml": e2eVaultedSecret,
		"site.yml": `- hosts: localhost
  gather_facts: false
  connection: local
  vars_files: [secret.yml]
  tasks:
    - name: record a run
      shell: echo run >> "{{ playbook_dir }}/runs.log"
      when: secret == "sekrit"
`}, "run site.yml -i localhost, --ask-vault-pass -K --no-dialog")
	s.waitFor("Vault password:", 15*time.Second)
	s.send("pw123", "Enter")
	got := s.waitFor("BECOME password:", 15*time.Second)
	if strings.Index(got, "Vault password:") > strings.Index(got, "BECOME password:") {
		t.Errorf("prompts out of command-line order:\n%s", got)
	}
	s.send("whatever", "Enter")
	s.waitFor("Playbook completed successfully", 15*time.Second)
	waitForRuns(t, workDir, 1) // the vaulted var decrypted, so the task's when: held
	s.quitCleanly()
}

// freeRecapCheck runs a debug session over a slow-on-h1 free-strategy
// playbook (the interleaving that used to misattribute results) and checks
// both hosts' recaps.
func freeRecapCheck(t *testing.T, playbook string, extra map[string]string) {
	t.Helper()
	extra["inv.ini"] = "h1\nh2\n"
	s, _ := startDebugSession(t, playbook, extra, "-i inv.ini")
	s.waitFor("F5: run", 15*time.Second)
	s.send("F5")
	got := s.waitFor("F5/F9/F10/F11/F12 disabled", 30*time.Second)
	for _, h := range []string{"h1 ", "h2 "} {
		if recap := lineContaining(got, h+":"); !strings.Contains(recap, "changed=2") {
			t.Errorf("recap for %s should count both tasks: %q\n%s", h, recap, got)
		}
	}
	s.quitCleanly()
}

const e2eSlowFreeTasks = `  tasks:
    - name: slow on h1
      command: "sleep {{ 1.5 if inventory_hostname == 'h1' else 0 }}"

    - name: quick
      command: sleep 0.2
`

// TestE2E_Debug_GloballyConfiguredFreeStrategyRunsLinear: no strategy: in
// the playbook at all, only ansible.cfg - covered by ANSIBLE_STRATEGY=linear
// on debug's spawns, not by the in-copy rewrite.
func TestE2E_Debug_GloballyConfiguredFreeStrategyRunsLinear(t *testing.T) {
	requireE2ETools(t)
	freeRecapCheck(t, "- hosts: all\n  gather_facts: false\n  connection: local\n"+e2eSlowFreeTasks,
		map[string]string{"ansible.cfg": "[defaults]\nstrategy = free\n"})
}

// TestE2E_Debug_FreeStrategyInImportedPlaybookRunsLinear: the rewrite
// follows import_playbook. The imported playbook is roles-only (no tasks of
// its own), so it isn't among the files the debugger splice visits and only
// the import-following reaches it.
func TestE2E_Debug_FreeStrategyInImportedPlaybookRunsLinear(t *testing.T) {
	requireE2ETools(t)
	freeRecapCheck(t, "- import_playbook: other.yml\n", map[string]string{
		"other.yml":                 "- hosts: all\n  strategy: free\n  gather_facts: false\n  connection: local\n  roles: [slow]\n",
		"roles/slow/tasks/main.yml": "- name: slow on h1\n  command: \"sleep {{ 1.5 if inventory_hostname == 'h1' else 0 }}\"\n\n- name: quick\n  command: sleep 0.2\n",
	})
}
