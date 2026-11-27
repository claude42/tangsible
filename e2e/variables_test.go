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

// End-to-end tests for `tangsible variables` (design-docs/Variables.md),
// sharing e2e/rerun_test.go's tmux harness.
package e2e

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copyVariablesFixture copies testdata/variables into a temp dir (a test may
// edit its files) and returns it.
func copyVariablesFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "testdata", "variables")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func (s *tmuxSession) startVariables(t *testing.T, dir, env, args string) {
	t.Helper()
	bin := buildE2EBinary(t)
	s.send(fmt.Sprintf("cd %s && %s %s %s variables %s", shellQuote(dir), env, e2eCallbackEnv(t), shellQuote(bin), args), "Enter")
}

// TestE2E_Variables_ListsDefinitionsInPrecedenceOrderAndSwitchesHosts walks
// the screen: the list, a variable's definitions in Ansible's order, the other
// host with ←/→ and the picker.
func TestE2E_Variables_ListsDefinitionsInPrecedenceOrderAndSwitchesHosts(t *testing.T) {
	requireE2ETools(t)
	dir := copyVariablesFixture(t)
	s := startTmuxSession(t)
	s.startVariables(t, dir, "", "-i inventory/hosts.ini web1 site.yml")

	list := s.waitFor("playbook_only", 30*time.Second)
	for _, want := range []string{"Variables: web1", "7 variables", "app_name", "inventory/host_vars/web1.yml:1", "enter: expand"} {
		if !strings.Contains(list, want) {
			t.Errorf("list lacks %q:\n%s", want, list)
		}
	}

	// listen_port is the third variable.
	s.send("Down", "Down", "Enter")
	open := s.waitFor("role defaults (app)", 5*time.Second)
	order := []string{"inventory host_vars (web1)", "playbook group_vars (web)", "inventory group_vars (web)", "inventory group_vars (all)", "role defaults (app)"}
	last := -1
	for _, want := range order {
		i := strings.Index(open, want)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of precedence order in:\n%s", want, open)
		}
		last = i
	}
	if strings.Contains(open, "inventory_hostname") {
		t.Errorf("ansible's own variables must not be listed:\n%s", open)
	}

	// The next host has no host_vars of its own: the playbook's group_vars win.
	s.send("Right")
	s.waitFor("Variables: web2", 10*time.Second)
	s.waitForAbsence("reading the variables", 15*time.Second)
	web2 := s.capture()
	if !strings.Contains(web2, "3000") {
		t.Errorf("web2's listen_port should come from the playbook's group_vars:\n%s", web2)
	}
	s.send("Left")
	s.waitFor("Variables: web1", 10*time.Second)

	// The picker (h) chooses a host: db1 has no role, and no web group vars.
	s.send("h")
	s.waitFor("host ═", 5*time.Second)
	s.send("Up", "Enter")
	s.waitFor("Variables: db1", 10*time.Second)
	s.waitForAbsence("reading the variables", 15*time.Second)
	db := s.capture()
	if strings.Contains(db, "app_name") || strings.Contains(db, "playbook_only") {
		t.Errorf("db1 has no role and is in no web group:\n%s", db)
	}
}

// TestE2E_Variables_EditsAFileAndReads checks e: the editor gets the
// definition's file and the list is read again when it returns.
func TestE2E_Variables_EditsAFileAndReads(t *testing.T) {
	requireE2ETools(t)
	dir := copyVariablesFixture(t)
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nsed -i 's/^listen_port: 4000/listen_port: 4444/' \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.startVariables(t, dir, fmt.Sprintf("VISUAL=%s EDITOR=%s", shellQuote(editor), shellQuote(editor)), "-i inventory/hosts.ini web1 site.yml")
	s.waitFor("playbook_only", 30*time.Second)
	s.send("Down", "Down") // listen_port, whose winner is inventory/host_vars/web1.yml
	s.send("e")
	got := s.waitFor("4444", 20*time.Second)
	if strings.Contains(got, " 4000 ") {
		t.Errorf("the old value is still shown:\n%s", got)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "inventory", "host_vars", "web1.yml"))
	if !strings.Contains(string(data), "listen_port: 4444") {
		t.Errorf("the editor was not given the winning file:\n%s", data)
	}
}

// TestE2E_Variables_MasksVaultedValuesAndIgnoresOnesNoPasswordOpens pins the
// vault behavior: a vaulted value shows as <vaulted>, and one that nothing
// can decrypt (an unused leftover) neither shows nor breaks the list.
func TestE2E_Variables_MasksVaultedValuesAndIgnoresOnesNoPasswordOpens(t *testing.T) {
	requireE2ETools(t)
	dir := copyVariablesFixture(t)
	good := filepath.Join(dir, "good.pass")
	other := filepath.Join(dir, "other.pass")
	_ = os.WriteFile(good, []byte("right password\n"), 0o600)
	_ = os.WriteFile(other, []byte("some other password\n"), 0o600)
	encrypt := func(passFile, name, value string) string {
		out, err := exec.Command("ansible-vault", "encrypt_string", "--vault-password-file", passFile, "--name", name, value).Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	vars := encrypt(good, "db_password", "hunter2hunter2") + encrypt(other, "stale_secret", "leftover-secret-1")
	f, err := os.OpenFile(filepath.Join(dir, "inventory", "group_vars", "all.yml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(vars)
	_ = f.Close()

	s := startTmuxSession(t)
	s.startVariables(t, dir, "", "-i inventory/hosts.ini --vault-password-file good.pass web1 site.yml")
	got := s.waitFor("db_password", 30*time.Second)
	for _, want := range []string{"db_password", "stale_secret", "<vaulted>"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q:\n%s", want, got)
		}
	}
	for _, secret := range []string{"hunter2hunter2", "leftover-secret-1"} {
		if strings.Contains(got, secret) {
			t.Errorf("a secret is on the screen:\n%s", got)
		}
	}
}

// TestE2E_Variables_EnterOnADefinitionShowsItsFile covers the file pane: the
// two-pane layout on a wide terminal (the list stays), copy, Esc, and the
// full-screen pane on a narrow one.
func TestE2E_Variables_EnterOnADefinitionShowsItsFile(t *testing.T) {
	requireE2ETools(t)
	dir := copyVariablesFixture(t)
	s := startTmuxSession(t)
	s.startVariables(t, dir, "", "-i inventory/hosts.ini web1 site.yml")
	s.waitFor("playbook_only", 30*time.Second)

	// listen_port, expanded: winner (host_vars), then the playbook's group_vars.
	s.send("Down", "Down", "Enter", "Down", "Down", "Enter")
	pane := s.waitFor("playbook_only: yes", 10*time.Second)
	for _, want := range []string{"listen_port  -  playbook group_vars (web), group_vars/web.yml:1", "listen_port: 3000", "Variables: web1", "esc: back"} {
		if !strings.Contains(pane, want) {
			t.Errorf("file pane lacks %q (two-pane: the list stays):\n%s", want, pane)
		}
	}

	s.send("y")
	s.waitFor("copied", 5*time.Second)

	s.send("Escape")
	s.waitForAbsence("playbook_only: yes", 5*time.Second)
	if back := s.capture(); !strings.Contains(back, "enter: expand or show file") {
		t.Errorf("Esc should be back at the list:\n%s", back)
	}

	// A narrow terminal: the pane takes the whole screen.
	if out, err := exec.Command("tmux", "resize-window", "-t", s.target(), "-x", "90", "-y", "40").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v\n%s", err, out)
	}
	s.send("Enter")
	narrow := s.waitFor("playbook_only: yes", 10*time.Second)
	if strings.Contains(narrow, "Variables: web1") {
		t.Errorf("a narrow terminal shows the pane alone:\n%s", narrow)
	}
	s.send("q")
	s.waitForAbsence("playbook_only: yes", 5*time.Second)
}

// TestE2E_Variables_EditingFromTheFilePaneShowsTheNewContents checks e inside
// the pane: the file is edited and the pane shows the new contents.
func TestE2E_Variables_EditingFromTheFilePaneShowsTheNewContents(t *testing.T) {
	requireE2ETools(t)
	dir := copyVariablesFixture(t)
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nsed -i 's/^listen_port: 3000/listen_port: 3333/' \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := startTmuxSession(t)
	s.startVariables(t, dir, fmt.Sprintf("VISUAL=%s EDITOR=%s", shellQuote(editor), shellQuote(editor)), "-i inventory/hosts.ini web1 site.yml")
	s.waitFor("playbook_only", 30*time.Second)
	s.send("Down", "Down", "Enter", "Down", "Down", "Enter")
	s.waitFor("listen_port: 3000", 10*time.Second)
	s.send("e")
	got := s.waitFor("listen_port: 3333", 20*time.Second)
	if strings.Contains(got, "listen_port: 3000") {
		t.Errorf("the old contents are still shown:\n%s", got)
	}
}

// TestE2E_Variables_UnknownHostAndNoHostFailBeforeTheTUI covers the two
// argument errors.
func TestE2E_Variables_UnknownHostAndNoHostFailBeforeTheTUI(t *testing.T) {
	requireE2ETools(t)
	bin := buildE2EBinary(t)
	dir := copyVariablesFixture(t)

	cmd := exec.Command(bin, "variables", "-i", "inventory/hosts.ini", "nosuchhost", "site.yml")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), `host "nosuchhost" is not in the inventory`) {
		t.Errorf("unknown host: err=%v out=%s", err, out)
	}

	cmd = exec.Command(bin, "variables")
	cmd.Dir = dir
	out, err = cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "tangsible variables") {
		t.Errorf("no host should print the usage: err=%v out=%s", err, out)
	}
}
